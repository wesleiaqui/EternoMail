package sync

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/hkdb/aerion/internal/account"
	"github.com/hkdb/aerion/internal/folder"
	"github.com/hkdb/aerion/internal/gmail"
	"github.com/hkdb/aerion/internal/message"
	"mime"
	"net/mail"
	"slices"
	"strings"
	"time"
)

func (e *Engine) GmailClient(ctx context.Context, accountID string) (*gmail.Client, error) {
	if e.gmailTokens == nil {
		return nil, gmail.ErrNotCapable
	}
	acc, err := e.accountStore.Get(accountID)
	if err != nil {
		return nil, err
	}
	if acc == nil || acc.AuthType != account.AuthOAuth2 || !strings.EqualFold(acc.IMAPHost, "imap.gmail.com") {
		return nil, gmail.ErrNotCapable
	}
	if _, err = e.gmailTokens(ctx, accountID, false); err != nil {
		if errors.Is(err, gmail.ErrNotCapable) {
			return nil, gmail.ErrReauthRequired
		}
		return nil, err
	}
	c := gmail.NewClient(func(ctx context.Context, force bool) (string, error) { return e.gmailTokens(ctx, accountID, force) })
	if e.gmailClientFactory != nil {
		c = e.gmailClientFactory(func(ctx context.Context, force bool) (string, error) { return e.gmailTokens(ctx, accountID, force) })
	}

	if e.gmailBaseURL != "" {
		c.BaseURL = e.gmailBaseURL
	}
	if c.BaseURL == gmail.DefaultBaseURL {
		e.gmailBlockMu.Lock()
		if e.gmailBudgets == nil {
			e.gmailBudgets = map[string]*gmail.Budget{}
		}
		if e.gmailBudgets[accountID] == nil {
			e.gmailBudgets[accountID] = gmail.NewBudget()
		}
		c.Budget = e.gmailBudgets[accountID]
		e.gmailBlockMu.Unlock()
	}
	return c, nil
}
func (e *Engine) GmailProviderID(ctx context.Context, c *gmail.Client, m *message.Message) (string, error) {
	id, err := e.messageStore.GmailID(m.AccountID, m.ID)
	if err != nil || id != "" {
		return id, err
	}
	if m.MessageID == "" {
		return "", fmt.Errorf("message has no provider identity")
	}
	id, err = c.FindByRFCMessageID(ctx, m.MessageID)
	if err != nil {
		return "", err
	}
	if err = e.messageStore.BindGmailID(m.AccountID, m.ID, id); err != nil {
		return "", err
	}
	return id, nil
}
func (e *Engine) GmailLabelID(ctx context.Context, c *gmail.Client, f *folder.Folder) (string, error) {
	label, err := e.folderStore.GmailLabel(f.AccountID, f.ID)
	if err != nil || label != "" {
		return label, err
	}
	switch f.Type {
	case folder.TypeInbox:
		return "INBOX", nil
	case folder.TypeSent:
		return "SENT", nil
	case folder.TypeDrafts:
		return "DRAFT", nil
	case folder.TypeTrash:
		return "TRASH", nil
	case folder.TypeSpam:
		return "SPAM", nil
	case folder.TypeStarred:
		return "STARRED", nil
	case folder.TypeAll, folder.TypeArchive:
		return "", nil
	}
	labels, err := c.Labels(ctx)
	if err != nil {
		return "", err
	}
	for _, l := range labels {
		if l.Name == f.Path {
			if err = e.folderStore.SetGmailLabel(f.AccountID, f.ID, l.ID); err != nil {
				return "", err
			}
			return l.ID, nil
		}
	}
	return "", fmt.Errorf("Gmail label not found: %s", f.Path)
}
func (e *Engine) syncGmailFolders(ctx context.Context, accountID string, c *gmail.Client) error {
	labels, err := c.Labels(ctx)
	if err != nil {
		return err
	}
	existing, err := e.folderStore.List(accountID)
	if err != nil {
		return err
	}
	types := map[string]folder.Type{"INBOX": folder.TypeInbox, "SENT": folder.TypeSent, "DRAFT": folder.TypeDrafts, "TRASH": folder.TypeTrash, "SPAM": folder.TypeSpam, "STARRED": folder.TypeStarred}
	for _, label := range labels {
		if label.ID == "UNREAD" || strings.HasPrefix(label.Name, "Postilaatikko/Triage/") {
			continue
		}
		typ := types[label.ID]
		if typ == "" {
			typ = folder.TypeFolder
		}
		var f *folder.Folder
		for _, old := range existing {
			saved, _ := e.folderStore.GmailLabel(accountID, old.ID)
			if saved == label.ID || old.Path == label.Name || (typ != folder.TypeFolder && old.Type == typ) {
				f = old
				break
			}
		}
		if f == nil {
			f = &folder.Folder{AccountID: accountID, Name: label.Name, Path: label.Name, Type: typ, Subscribed: true}
			if err = e.folderStore.Create(f); err != nil {
				return err
			}
		}
		if err = e.folderStore.SetGmailLabel(accountID, f.ID, label.ID); err != nil {
			return err
		}
	}
	current, err := e.folderStore.List(accountID)
	if err != nil {
		return err
	}
	for _, f := range current {
		if i := strings.LastIndex(f.Path, "/"); i > 0 {
			parent, err := e.folderStore.GetByPath(accountID, f.Path[:i])
			if err != nil {
				return err
			}
			if parent != nil && parent.ID != f.ID {
				f.ParentID = parent.ID
				f.Name = f.Path[i+1:]
				if err = e.folderStore.Update(f); err != nil {
					return err
				}
			}
		}
	}

	all, err := e.folderStore.GetByType(accountID, folder.TypeAll)
	if err != nil {
		return err
	}
	if all == nil {
		all, err = e.folderStore.GetByPath(accountID, "[Gmail]/All Mail")
		if err != nil {
			return err
		}
		if all == nil {
			all = &folder.Folder{AccountID: accountID, Name: "All Mail", Path: "[Gmail]/All Mail", Type: folder.TypeAll, Subscribed: true}
			if err = e.folderStore.Create(all); err != nil {
				return err
			}
		}
	}

	return nil
}
func gmailAddresses(value string) string {
	addresses, err := mail.ParseAddressList(value)
	if err != nil {
		return "[]"
	}
	out := make([]message.Address, 0, len(addresses))
	for _, a := range addresses {
		out = append(out, message.Address{Name: a.Name, Email: a.Address})
	}
	encoded, _ := json.Marshal(out)
	return string(encoded)
}
func (e *Engine) cacheGmailMetadata(accountID, folderID string, items []gmail.Message) ([]*message.Message, error) {
	out := make([]*message.Message, 0, len(items))
	for _, item := range items {
		m := &message.Message{AccountID: accountID, FolderID: folderID, MessageID: item.RFCMessageID, ThreadID: "gmail:" + accountID + ":" + item.ThreadID, InReplyTo: item.InReplyTo, Subject: item.Subject, Date: item.Date, Snippet: item.Snippet, Size: item.Size, ToList: gmailAddresses(item.To), CcList: gmailAddresses(item.Cc), BccList: gmailAddresses(item.Bcc), ReplyTo: item.ReplyTo, IsRead: !slices.Contains(item.LabelIDs, "UNREAD"), IsStarred: slices.Contains(item.LabelIDs, "STARRED"), IsDraft: slices.Contains(item.LabelIDs, "DRAFT")}
		if decoded, err := new(mime.WordDecoder).DecodeHeader(m.Subject); err == nil {
			m.Subject = decoded
		}

		if from, err := mail.ParseAddress(item.From); err == nil {
			m.FromName = from.Name
			m.FromEmail = from.Address
		} else {
			m.FromEmail = item.From
		}
		var header strings.Builder
		for name, value := range item.Headers {
			header.WriteString(name + ": " + value + "\r\n")
			if strings.EqualFold(name, "Disposition-Notification-To") {
				m.ReadReceiptTo = value
			}
		}
		header.WriteString("\r\n")
		m.InboxCategory = classifyInboxCategory([]byte(header.String()), m)

		refs, _ := json.Marshal(strings.Fields(item.References))
		m.References = string(refs)
		if err := e.messageStore.UpsertGmail(m, item.ID); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, nil
}
func (e *Engine) syncGmailMessages(ctx context.Context, c *gmail.Client, f *folder.Folder, days int) (resultErr error) {
	defer func() {
		e.recordGmailState(f.AccountID, resultErr)
		if resultErr == nil {
			resultErr = e.folderStore.MarkSynced(f.ID)
		}
	}()
	unlock, err := e.LockGmailAccount(ctx, f.AccountID)
	if err != nil {
		return err
	}
	defer unlock()

	label, err := e.GmailLabelID(ctx, c, f)
	if err != nil {
		return err
	}
	q := ""
	if f.Type == folder.TypeArchive {
		q = "-in:inbox -in:spam -in:trash"
	}
	if days > 0 {
		q += fmt.Sprintf(" after:%d", time.Now().AddDate(0, 0, -days).Unix())
	}
	req := gmail.SearchRequest{Query: strings.TrimSpace(q), Limit: 25}
	if label != "" {
		req.LabelIDs = []string{label}
	}
	// First page refreshes recent headers/flags; history is advanced in bounded
	// pages by the account scheduler, never by downloading message bodies.
	page, err := c.Search(ctx, req)
	if err != nil {
		return err
	}
	items, err := c.Metadata(ctx, page.MessageIDs)
	if err != nil {
		return err
	}
	prior, err := e.messageStore.GmailFolderMessages(f.AccountID, f.ID)
	if err != nil {
		return err
	}
	saved, err := e.cacheGmailMetadata(f.AccountID, f.ID, items)
	if err != nil {
		return err
	}
	newMessageHandler := e.newMessageHandler
	if newMessageHandler != nil {
		for index, item := range items {
			if _, existed := prior[item.ID]; existed || index >= len(saved) {
				continue
			}
			msg := saved[index]
			go func(msg *message.Message) {
				defer func() {
					if recovered := recover(); recovered != nil {
						e.log.Error().Interface("panic", recovered).Str("folder", f.ID).Msg("New Gmail message handler panicked")
					}
				}()
				newMessageHandler(f.AccountID, f.ID, msg)
			}(msg)
		}
	}
	if label != "" {
		counts, err := c.Label(ctx, label)
		if err != nil {
			return err
		}
		return e.folderStore.UpdateCounts(f.ID, counts.MessagesTotal, counts.MessagesUnread)
	}
	return nil
}
func (e *Engine) fetchGmailBody(ctx context.Context, c *gmail.Client, m *message.Message) (*message.Message, error) {
	id, err := e.GmailProviderID(ctx, c, m)
	if err != nil {
		return nil, err
	}
	parsed, err := e.gmailBody(ctx, c, m, id)
	if err != nil {
		return nil, err
	}
	err = e.messageStore.UpdateBodiesBatch([]message.BodyUpdate{{MessageID: m.ID, BodyText: parsed.BodyText, BodyHTML: parsed.BodyHTML, Snippet: generateSnippet(parsed.BodyText, 200), HasAttachments: parsed.HasAttachments, SMIMERawBody: parsed.SMIMERawBody, SMIMEEncrypted: parsed.SMIMEEncrypted, PGPRawBody: parsed.PGPRawBody, PGPEncrypted: parsed.PGPEncrypted, Reaction: parsed.Reaction}})
	if err != nil {
		return nil, err
	}
	if e.attachmentStore != nil {
		for _, a := range parsed.Attachments {
			if err = e.attachmentStore.Create(a); err != nil && !strings.Contains(err.Error(), "UNIQUE constraint") {
				return nil, err
			}
		}
	}
	return e.messageStore.Get(m.ID)
}

// CacheGmailResults makes API-only search hits actionable without an IMAP UID.
func (e *Engine) CacheGmailResults(ctx context.Context, accountID string, items []gmail.Message, preferredFolders []string) ([]*message.Message, error) {
	folders, err := e.folderStore.List(accountID)
	if err != nil {
		return nil, err
	}
	c, err := e.GmailClient(ctx, accountID)
	if err != nil {
		return nil, err
	}
	labels := map[string]string{}
	remoteLabels, err := c.Labels(ctx)
	if err != nil {
		return nil, err
	}
	for _, f := range folders {
		label, _ := e.folderStore.GmailLabel(accountID, f.ID)
		if label == "" {
			for _, r := range remoteLabels {
				if r.Name == f.Path {
					label = r.ID
					break
				}
			}
		}
		if label == "" {
			label = map[folder.Type]string{folder.TypeInbox: "INBOX", folder.TypeSent: "SENT", folder.TypeDrafts: "DRAFT", folder.TypeTrash: "TRASH", folder.TypeSpam: "SPAM", folder.TypeStarred: "STARRED"}[f.Type]
		}
		labels[f.ID] = label
	}

	var out []*message.Message
	for _, item := range items {
		var target *folder.Folder
		for _, id := range preferredFolders {
			for _, f := range folders {
				if f.ID == id && (slices.Contains(item.LabelIDs, labels[id]) || f.Type == folder.TypeAll || f.Type == folder.TypeArchive) {
					target = f
					break
				}
			}
			if target != nil {
				break
			}
		}
		if target == nil {
			for _, f := range folders {
				if label := labels[f.ID]; label != "" && slices.Contains(item.LabelIDs, label) {
					target = f
					break
				}
			}
		}
		if target == nil {
			target, err = e.folderStore.GetByType(accountID, folder.TypeAll)
			if err == nil && target == nil {
				target, err = e.folderStore.GetByPath(accountID, "[Gmail]/All Mail")
			}
			if err != nil {
				return nil, err
			}
			if target == nil {
				target = &folder.Folder{AccountID: accountID, Name: "All Mail", Path: "[Gmail]/All Mail", Type: folder.TypeAll}
				if err = e.folderStore.Create(target); err != nil {
					return nil, err
				}
			}
		}
		saved, err := e.cacheGmailMetadata(accountID, target.ID, []gmail.Message{item})
		if err != nil {
			return nil, err
		}
		out = append(out, saved...)
		for _, f := range folders {
			if f.ID != target.ID && labels[f.ID] != "" && slices.Contains(item.LabelIDs, labels[f.ID]) {
				if _, err = e.cacheGmailMetadata(accountID, f.ID, []gmail.Message{item}); err != nil {
					return nil, err
				}
			}
		}

	}
	return out, nil
}

// SyncGmailAccount advances bounded account-wide header pages and Gmail history.
// A message carrying several labels is downloaded once, then projected locally.
func (e *Engine) SyncGmailAccount(ctx context.Context, accountID string) ([]string, error) {
	c, err := e.GmailClient(ctx, accountID)
	if err != nil {
		return nil, err
	}
	unlock, err := e.folderLocks.lock(ctx, "gmail-account:"+accountID)
	if err != nil {
		return nil, err
	}
	defer func() {
		if unlock != nil {
			unlock()
		}
	}()
	history, hpage, backfill, err := e.folderStore.GmailSyncCursor(accountID)
	if err != nil {
		return nil, err
	}
	if history == "" {
		history, err = c.ProfileHistoryID(ctx)
		if err != nil {
			return nil, err
		}
	}
	req := gmail.SearchRequest{Limit: 25, IncludeSpamTrash: true}
	if backfill != "!done" {
		req.PageToken = backfill
	}
	if backfill != "!done" {
		page, err := c.Search(ctx, req)
		if err != nil {
			return nil, err
		}
		headers, err := c.Metadata(ctx, page.MessageIDs)
		if err != nil {
			return nil, err
		}
		if _, err = e.CacheGmailResults(ctx, accountID, headers, nil); err != nil {
			return nil, err
		}
		if err = e.reconcileGmailMemberships(accountID, headers); err != nil {
			return nil, err
		}
		backfill = page.NextPageToken
		if backfill == "" {
			backfill = "!done"
		}
	}
	changes, err := c.History(ctx, history, hpage)
	if errors.Is(err, gmail.ErrNotFound) {
		return nil, e.folderStore.SetGmailSyncCursor(accountID, "", "", "")
	}
	if err != nil {
		return nil, err
	}
	ids := []string{}
	deleted := []string{}
	for _, h := range changes.History {
		for _, m := range h.Messages {
			if !slices.Contains(ids, m.ID) {
				ids = append(ids, m.ID)
			}
		}
		for _, m := range h.Deleted {
			if !slices.Contains(deleted, m.Message.ID) {
				deleted = append(deleted, m.Message.ID)
			}
		}
	}
	if len(ids) > 0 {
		headers, err := c.Metadata(ctx, ids)
		if err != nil {
			return nil, err
		}
		if _, err = e.CacheGmailResults(ctx, accountID, headers, nil); err != nil {
			return nil, err
		}
		if err = e.reconcileGmailMemberships(accountID, headers); err != nil {
			return nil, err
		}
	}
	for _, id := range deleted {
		if err = e.messageStore.DeleteGmailProviderCopies(accountID, id); err != nil {
			return nil, err
		}
	}
	if changes.NextPageToken == "" && changes.HistoryID != "" {
		history = changes.HistoryID
	}
	hpage = changes.NextPageToken
	if err = e.folderStore.SetGmailSyncCursor(accountID, history, hpage, backfill); err != nil {
		return nil, err
	}
	unlock()
	unlock = nil
	fs, err := e.folderStore.List(accountID)
	if err != nil {
		return nil, err
	}
	touched := []string{}
	for _, f := range fs {
		label, _ := e.folderStore.GmailLabel(accountID, f.ID)
		if label != "" {
			counts, err := c.Label(ctx, label)
			if err != nil {
				return touched, err
			}
			if err = e.folderStore.UpdateCounts(f.ID, counts.MessagesTotal, counts.MessagesUnread); err != nil {
				return touched, err
			}
		}
		if err = e.folderStore.MarkSynced(f.ID); err != nil {
			return touched, err
		}
		touched = append(touched, f.ID)
	}
	return touched, nil
}

// SetGmailClientFactory injects transport policy before starting the engine.
func (e *Engine) SetGmailClientFactory(factory func(gmail.TokenSource) *gmail.Client) {
	e.gmailClientFactory = factory
}

func (e *Engine) LockGmailAccount(ctx context.Context, accountID string) (func(), error) {
	return e.folderLocks.lock(ctx, "gmail-account:"+accountID)
}

func (e *Engine) reconcileGmailMemberships(accountID string, items []gmail.Message) error {
	fs, err := e.folderStore.List(accountID)
	if err != nil {
		return err
	}
	labels := map[string]string{}
	for _, f := range fs {
		labels[f.ID], err = e.folderStore.GmailLabel(accountID, f.ID)
		if err != nil {
			return err
		}
	}
	for _, item := range items {
		valid := []string{}
		spamTrash := slices.Contains(item.LabelIDs, "SPAM") || slices.Contains(item.LabelIDs, "TRASH")
		for _, f := range fs {
			if (labels[f.ID] != "" && slices.Contains(item.LabelIDs, labels[f.ID])) || (f.Type == folder.TypeAll && !spamTrash) || (f.Type == folder.TypeArchive && !spamTrash && !slices.Contains(item.LabelIDs, "INBOX")) {
				valid = append(valid, f.ID)
			}
		}
		if len(valid) > 0 {
			if err = e.messageStore.PruneGmailMemberships(accountID, item.ID, valid); err != nil {
				return err
			}
		}
	}
	return nil
}

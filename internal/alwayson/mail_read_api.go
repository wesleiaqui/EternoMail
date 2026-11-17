package alwayson

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/hkdb/aerion/internal/automation/v1"
	"github.com/hkdb/aerion/internal/email/replycontent"
	"github.com/hkdb/aerion/internal/folder"
	"github.com/hkdb/aerion/internal/mailservice"
	"github.com/hkdb/aerion/internal/message"
	"github.com/hkdb/aerion/internal/replylater"
	"github.com/hkdb/aerion/internal/views"
)

type mailboxReadOperations struct{ runtime *SyncRuntime }

func (o *mailboxReadOperations) SearchMessages(ctx context.Context, scope views.Scope, query, cursor string, limit int) (*views.ThreadPage, error) {
	return o.SearchMessagesWithFilter(ctx, scope, query, "", cursor, limit)
}

func (o *mailboxReadOperations) SearchMessagesWithFilter(ctx context.Context, scope views.Scope, query, filter, cursor string, limit int) (*views.ThreadPage, error) {
	request, empty, err := o.searchRequest(scope, query, filter, cursor, limit)
	if err != nil {
		return nil, err
	}
	if empty {
		return &views.ThreadPage{Items: []views.ThreadSummary{}, Complete: true}, nil
	}
	page, err := o.runtime.mailService.Search(ctx, request)
	if err != nil {
		return nil, err
	}
	result := &views.ThreadPage{Items: make([]views.ThreadSummary, 0, len(page.Conversations)), NextCursor: page.Cursor, Complete: page.Cursor == "" && len(page.Issues) == 0}
	for _, conversation := range page.Conversations {
		if conversation == nil {
			continue
		}
		participants := make([]views.Address, 0, len(conversation.Participants))
		for _, participant := range conversation.Participants {
			participants = append(participants, views.Address{Name: participant.Name, Email: participant.Email})
		}
		matched := make([]views.MessageRef, 0, len(conversation.MessageIDs))
		for _, messageID := range conversation.MessageIDs {
			matched = append(matched, views.MessageRef{AccountID: conversation.AccountID, MessageID: messageID})
		}
		result.Items = append(result.Items, views.ThreadSummary{
			Ref:            views.ThreadRef{AccountID: conversation.AccountID, ThreadID: conversation.ThreadID},
			AccountName:    conversation.AccountName,
			FolderID:       conversation.FolderID,
			Subject:        conversation.Subject,
			Snippet:        conversation.Snippet,
			LatestDate:     conversation.LatestDate,
			Participants:   participants,
			MessageCount:   conversation.MessageCount,
			UnreadCount:    conversation.UnreadCount,
			HasAttachments: conversation.HasAttachments,
			IsStarred:      conversation.IsStarred,
			Matched:        matched,
		})
	}
	for _, issue := range page.Issues {
		code := "account_search_failed"
		if issue.Warning {
			code = "stale_source"
		}
		if issue.Reauthorize {
			code = "reauthorize_required"
		}
		result.Errors = append(result.Errors, views.PartialError{AccountID: issue.AccountID, AccountName: issue.AccountName, Code: code, Message: issue.Message, Reauthorize: issue.Reauthorize})
	}
	return result, nil
}

func (o *mailboxReadOperations) searchRequest(scope views.Scope, query, filter, cursor string, limit int) (mailservice.MailSearchRequest, bool, error) {
	if err := scope.Validate(); err != nil {
		return mailservice.MailSearchRequest{}, false, err
	}
	base := scope
	if scope.Kind == views.ScopeSearch {
		query = scope.Query
		if scope.Within != nil {
			base = *scope.Within
		}
	}
	request := mailservice.MailSearchRequest{Query: query, Filter: filter, Cursor: cursor, Limit: limit}
	switch base.Kind {
	case views.ScopeAllInboxes:
		folderIDs, err := resolveSpecialFolderIDs(o.runtime.accounts, o.runtime.folders, folder.TypeInbox)
		if err != nil {
			return mailservice.MailSearchRequest{}, false, err
		}
		request.FolderIDs = folderIDs
		if len(request.FolderIDs) == 0 {
			return request, true, nil
		}
	case views.ScopeAllAccounts:
	case views.ScopeAccount:
		request.AccountIDs = []string{base.AccountID}
	case views.ScopeFolder:
		request.AccountIDs = []string{base.AccountID}
		request.FolderIDs = []string{base.FolderID}
	case views.ScopeVirtualFolder:
		request.VirtualFolderID = base.VirtualFolderID
	default:
		return mailservice.MailSearchRequest{}, false, fmt.Errorf("unsupported search scope %q", base.Kind)
	}
	return request, false, nil
}

func (o *mailboxReadOperations) GetThread(ctx context.Context, ref views.ThreadRef, scope views.Scope) (*views.ThreadView, error) {
	if ref.AccountID == "" || ref.ThreadID == "" {
		return nil, errors.New("accountId and threadId are required")
	}
	threadID, err := o.runtime.messages.ResolveThreadID(ref.AccountID, ref.ThreadID)
	if err != nil {
		return nil, err
	}
	if threadID == "" {
		return nil, fmt.Errorf("thread %s not found", ref.ThreadID)
	}
	ref.ThreadID = threadID
	ids, err := o.runtime.messages.GetMessageIDsInThread(ref.AccountID, ref.ThreadID)
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return nil, fmt.Errorf("thread %s not found", ref.ThreadID)
	}
	messages := make([]*message.Message, 0, len(ids))
	for _, id := range ids {
		msg, err := o.runtime.messages.Get(id)
		if err != nil {
			return nil, err
		}
		if msg == nil || msg.AccountID != ref.AccountID {
			return nil, errors.New("thread message is outside the requested account")
		}
		messages = append(messages, msg)
	}

	matched, err := o.matchThreadScope(ctx, ref, scope, messages)
	if err != nil {
		return nil, err
	}
	// Match raw copies first, then count and expose only the same preferred
	// account-scoped copies that the reader displays.
	folderID := ""
	base := scope
	if scope.Kind == views.ScopeSearch && scope.Within != nil {
		base = *scope.Within
	}
	if base.Kind == views.ScopeFolder {
		folderID = base.FolderID
	}
	messages = message.DeduplicateThreadMessages(messages, folderID, matched)
	result := &views.ThreadView{Ref: ref, Messages: make([]views.ThreadMessage, 0, len(messages)), Participants: []views.Address{}}
	seenParticipants := map[string]bool{}
	for _, msg := range messages {
		if msg.Date.After(result.LatestDate) {
			result.LatestDate = msg.Date
		}
		if result.Subject == "" {
			result.Subject = msg.Subject
		}
		participantKey := strings.ToLower(msg.FromEmail)
		if participantKey != "" && !seenParticipants[participantKey] {
			seenParticipants[participantKey] = true
			result.Participants = append(result.Participants, views.Address{Name: msg.FromName, Email: msg.FromEmail})
		}
		contentState := views.ContentNotFetched
		if msg.BodyFetched {
			contentState = views.ContentCached
		} else if msg.BodyFailed {
			contentState = views.ContentFetchFailed
		}
		isMatched := matched[msg.ID]
		if isMatched {
			result.MatchedCount++
		}
		var receivedAt *time.Time
		if !msg.ReceivedAt.IsZero() {
			receivedAt = &msg.ReceivedAt
		}
		attachmentSummaries := []views.AttachmentSummary{}
		if o.runtime.attachments != nil {
			attachments, err := o.runtime.attachments.GetByMessage(msg.ID)
			if err != nil {
				return nil, fmt.Errorf("list thread attachment metadata: %w", err)
			}
			for _, attachment := range attachments {
				if attachment == nil {
					continue
				}
				attachmentSummaries = append(attachmentSummaries, views.AttachmentSummary{
					ID:          attachment.ID,
					Filename:    attachment.Filename,
					ContentType: attachment.ContentType,
					Size:        attachment.Size,
					ContentID:   attachment.ContentID,
					IsInline:    attachment.IsInline,
				})
			}
		}
		result.Messages = append(result.Messages, views.ThreadMessage{
			Ref:                views.MessageRef{AccountID: ref.AccountID, MessageID: msg.ID},
			FolderID:           msg.FolderID,
			RFCMessageID:       msg.MessageID,
			Subject:            msg.Subject,
			Snippet:            msg.Snippet,
			From:               views.Address{Name: msg.FromName, Email: msg.FromEmail},
			ToList:             msg.ToList,
			CcList:             msg.CcList,
			BccList:            msg.BccList,
			ReplyTo:            msg.ReplyTo,
			Date:               msg.Date,
			ReceivedAt:         receivedAt,
			Matched:            isMatched,
			IsRead:             msg.IsRead,
			IsStarred:          msg.IsStarred,
			IsAnswered:         msg.IsAnswered,
			IsForwarded:        msg.IsForwarded,
			IsDraft:            msg.IsDraft,
			IsDeleted:          msg.IsDeleted,
			Size:               msg.Size,
			InboxCategory:      msg.InboxCategory,
			HasAttachments:     msg.HasAttachments,
			Attachments:        attachmentSummaries,
			SMIMEStatus:        msg.SMIMEStatus,
			SMIMESignerEmail:   msg.SMIMESignerEmail,
			SMIMESignerSubject: msg.SMIMESignerSubject,
			SMIMEEncrypted:     msg.SMIMEEncrypted,
			HasSMIME:           msg.HasSMIME,
			PGPStatus:          msg.PGPStatus,
			PGPSignerEmail:     msg.PGPSignerEmail,
			PGPSignerKeyID:     msg.PGPSignerKeyID,
			PGPEncrypted:       msg.PGPEncrypted,
			HasPGP:             msg.HasPGP,
			ContentState:       contentState,
		})
	}
	result.MessageCount = len(messages)
	result.Completeness = views.CompletenessOf(result.Messages)
	return result, nil
}

func (o *mailboxReadOperations) matchThreadScope(ctx context.Context, ref views.ThreadRef, scope views.Scope, messages []*message.Message) (map[string]bool, error) {
	matched := make(map[string]bool, len(messages))
	if scope.Kind == "" {
		for _, msg := range messages {
			matched[msg.ID] = true
		}
		return matched, nil
	}
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	if scope.Kind == views.ScopeSearch || scope.Kind == views.ScopeVirtualFolder {
		request, empty, err := o.searchRequest(scope, "", "", "", 100)
		if err != nil || empty {
			return matched, err
		}
		request.AccountIDs = []string{ref.AccountID}
		for pageCount := 0; pageCount < 20; pageCount++ {
			page, err := o.runtime.mailService.Search(ctx, request)
			if err != nil {
				return nil, err
			}
			for _, conversation := range page.Conversations {
				if conversation.AccountID == ref.AccountID && conversation.ThreadID == ref.ThreadID {
					for _, messageID := range conversation.MessageIDs {
						matched[messageID] = true
					}
					return matched, nil
				}
			}
			if len(page.Issues) > 0 {
				return nil, errors.New("thread scope could not be confirmed because mailbox search was partial")
			}
			if page.Cursor == "" {
				return matched, nil
			}
			request.Cursor = page.Cursor
		}
		return nil, errors.New("thread scope could not be confirmed within the bounded search window")
	}
	for _, msg := range messages {
		inScope, err := o.messageInScope(msg, scope)
		if err != nil {
			return nil, err
		}
		if inScope {
			matched[msg.ID] = true
		}
	}
	return matched, nil
}

func (o *mailboxReadOperations) messageInScope(msg *message.Message, scope views.Scope) (bool, error) {
	switch scope.Kind {
	case views.ScopeAllInboxes:
		inbox, err := resolveSpecialFolder(o.runtime.accounts, o.runtime.folders, msg.AccountID, folder.TypeInbox)
		if err != nil {
			return false, err
		}
		return inbox != nil && inbox.ID == msg.FolderID, nil
	case views.ScopeFolder:
		return msg.AccountID == scope.AccountID && msg.FolderID == scope.FolderID, nil
	case views.ScopeUnifiedFolder:
		if scope.FolderType == "readLater" || scope.FolderType == "replyLater" {
			id := msg.ThreadID
			if id == "" {
				id = msg.ID
			}
			read, err := o.runtime.triage.IsReadLater(msg.AccountID, id)
			if err != nil {
				return false, err
			}
			reply, err := o.runtime.replyLater.Contains(replylater.Ref{AccountID: msg.AccountID, ConversationID: id})
			return read || reply, err
		}
		typeValue, err := parseUnifiedFolderType(scope.FolderType)
		if err != nil {
			return false, err
		}
		if typeValue != folder.TypeArchive {
			resolved, err := resolveSpecialFolder(o.runtime.accounts, o.runtime.folders, msg.AccountID, typeValue)
			if err != nil {
				return false, err
			}
			return resolved != nil && resolved.ID == msg.FolderID, nil
		}
		physical, err := resolveSpecialFolder(o.runtime.accounts, o.runtime.folders, msg.AccountID, folder.TypeArchive)
		if err != nil || physical != nil {
			return physical != nil && physical.ID == msg.FolderID, err
		}
		allMail, err := resolveSpecialFolder(o.runtime.accounts, o.runtime.folders, msg.AccountID, folder.TypeAll)
		return allMail != nil && allMail.ID == msg.FolderID, err
	case views.ScopeVirtualFolder:
		vf, err := o.runtime.virtualFolders.Get(scope.VirtualFolderID)
		if err != nil {
			return false, err
		}
		if vf == nil {
			return false, fmt.Errorf("virtual folder %s not found", scope.VirtualFolderID)
		}
		member := false
		for _, folderID := range vf.FolderIDs {
			if folderID == msg.FolderID {
				member = true
				break
			}
		}
		if vf.FolderMode == "exclude" {
			member = !member
		}
		if !member || vf.ReadStatus == "unread" && msg.IsRead || vf.ReadStatus == "read" && !msg.IsRead || vf.StarStatus == "starred" && !msg.IsStarred || vf.StarStatus == "unstarred" && msg.IsStarred || vf.ReplyStatus == "replied" && !msg.IsAnswered || vf.ReplyStatus == "unreplied" && msg.IsAnswered {
			return false, nil
		}
		windowDays := map[string]int{"7d": 7, "30d": 30, "90d": 90, "1y": 365}[vf.DateWindow]
		return windowDays == 0 || !msg.Date.Before(time.Now().AddDate(0, 0, -windowDays)), nil
	default:
		return false, fmt.Errorf("unsupported thread scope %q", scope.Kind)
	}
}

func (o *mailboxReadOperations) GetMessage(accountID, messageID string) (*message.Message, error) {
	return o.runtime.mailService.GetMessage(accountID, messageID)
}

func (o *mailboxReadOperations) GetReplyContent(accountID, messageID, channel string, fetch bool) (*replycontent.Reply, error) {
	return o.runtime.mailService.GetReplyContent(context.Background(), accountID, messageID, channel, fetch)
}

func (o *mailboxReadOperations) FetchMessageBody(ctx context.Context, accountID, messageID string) (*message.Message, error) {
	return o.runtime.mailService.FetchMessageBody(ctx, accountID, messageID)
}

var _ v1.SearchOperations = (*mailboxReadOperations)(nil)
var _ v1.MessageReadOperations = (*mailboxReadOperations)(nil)

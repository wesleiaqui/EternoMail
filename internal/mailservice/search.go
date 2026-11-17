package mailservice

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/hkdb/aerion/internal/account"
	"github.com/hkdb/aerion/internal/folder"
	"github.com/hkdb/aerion/internal/gmail"
	"github.com/hkdb/aerion/internal/message"
	syncre "github.com/hkdb/aerion/internal/sync"
	"github.com/hkdb/aerion/internal/virtualfolder"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
)

// Gmail scope clauses use Gmail search syntax for user labels.
// Multiple includes are ORed, never passed as labelIds (which are ANDed).
func gmailScopeClause(f *folder.Folder) string {
	switch f.Type {
	case folder.TypeInbox:
		return "in:inbox"
	case folder.TypeSent:
		return "in:sent"
	case folder.TypeDrafts:
		return "in:drafts"
	case folder.TypeTrash:
		return "in:trash"
	case folder.TypeSpam:
		return "in:spam"
	case folder.TypeStarred:
		return "is:starred"
	case folder.TypeAll:
		return "(-in:spam -in:trash)"
	case folder.TypeArchive:
		return "(-in:inbox -in:spam -in:trash)"
	}
	// Gmail search lowercases label names and represents spaces as hyphens.
	// Keep nested-label separators and Unicode characters intact.
	token := strings.NewReplacer(" ", "-", `"`, "", "(", "", ")", "", "{", "", "}", "").Replace(strings.ToLower(f.Path))
	return "label:" + token
}

// GmailViewQuery builds a Gmail query that limits a search to the selected
// folder labels while preserving the user's query.
func GmailViewQuery(query string, folders []*folder.Folder, exclude bool) string {
	parts := []string{}
	if strings.TrimSpace(query) != "" {
		parts = append(parts, "("+query+")")
	}
	clauses := []string{}
	for _, f := range folders {
		clauses = append(clauses, gmailScopeClause(f))
	}
	if len(clauses) > 0 {
		scope := "(" + strings.Join(clauses, " OR ") + ")"
		if exclude {
			scope = "-" + scope
		}
		parts = append(parts, scope)
	}
	return strings.Join(parts, " ")
}

// MatchesGmailThread reports whether Gmail thread metadata satisfies saved
// status and date criteria.
func MatchesGmailThread(items []gmail.Message, vf *virtualfolder.VirtualFolder) bool {
	if vf == nil {
		return true
	}
	unread, starred, replied := false, false, false
	var latest time.Time
	for _, m := range items {
		unread = unread || slices.Contains(m.LabelIDs, "UNREAD")
		starred = starred || slices.Contains(m.LabelIDs, "STARRED")
		replied = replied || (slices.Contains(m.LabelIDs, "SENT") && m.InReplyTo != "")
		if m.Date.After(latest) {
			latest = m.Date
		}
	}
	if vf.ReadStatus == "unread" && !unread || vf.ReadStatus == "read" && unread {
		return false
	}
	if vf.StarStatus == "starred" && !starred || vf.StarStatus == "unstarred" && starred {
		return false
	}
	if vf.ReplyStatus == "replied" && !replied || vf.ReplyStatus == "unreplied" && replied {
		return false
	}
	days := map[string]int{"7d": 7, "30d": 30, "90d": 90, "1y": 365}[vf.DateWindow]
	return days == 0 || !latest.Before(time.Now().AddDate(0, 0, -days))
}

// SearchMail searches server metadata across enabled accounts. Body content is
// never fetched here. Cursor pages keep per-account failures visible/retryable.
func (s *Service) Search(parent context.Context, req MailSearchRequest) (*MailSearchPage, error) {
	if parent == nil {
		parent = context.Background()
	}
	if err := parent.Err(); err != nil {
		return nil, err
	}
	if s.accountStore == nil || s.folderStore == nil || s.syncEngine == nil {
		return nil, errors.New("mail search service is not ready")
	}
	if req.VirtualFolderID != "" && s.virtualFolderStore == nil {
		return nil, errors.New("virtual folder service is not ready")
	}
	switch req.Filter {
	case "", "unread", "replyLater", "readLater":
	default:
		return nil, fmt.Errorf("unsupported online filter")
	}
	queued := map[string]bool{}
	if req.Filter == "replyLater" || req.Filter == "readLater" {
		if s.syncTriageQueues != nil {
			s.syncTriageQueues("reply-later")
		}
	}
	if req.Filter == "readLater" || req.Filter == "replyLater" {
		if s.syncTriageQueues != nil {
			s.syncTriageQueues("read-later")
		}
	}
	if req.Filter == "replyLater" || req.Filter == "readLater" {
		if s.replyLaterStore == nil {
			return nil, fmt.Errorf("Reply Later queue is not ready")
		}
		refs, err := s.replyLaterStore.List()
		if err != nil {
			return nil, err
		}
		for _, ref := range refs {
			queued[ref.AccountID+"\x00"+ref.ConversationID] = true
		}
	}
	if req.Filter == "readLater" || req.Filter == "replyLater" {
		refs, err := s.triageStore.ListReadLater()
		if err != nil {
			return nil, err
		}
		for _, ref := range refs {
			queued[ref.AccountID+"\x00"+ref.ConversationID] = true
		}
	}
	matchesFilter := func(conv *message.Conversation) bool {
		if req.Filter == "unread" {
			return conv.UnreadCount > 0
		}
		return (req.Filter != "replyLater" && req.Filter != "readLater") || queued[conv.AccountID+"\x00"+conv.ThreadID]
	}
	ctx, cancel := context.WithTimeout(parent, 90*time.Second)
	defer cancel()
	if len(req.SessionID) > 128 {
		return nil, fmt.Errorf("invalid search session")
	}
	if req.SessionID != "" {
		job := &mailSearchJob{cancel: cancel}
		s.mailSearchMu.Lock()
		if s.mailSearchJobs == nil {
			s.mailSearchJobs = map[string]*mailSearchJob{}
		}
		if old := s.mailSearchJobs[req.SessionID]; old != nil {
			old.cancel()
		}
		s.mailSearchJobs[req.SessionID] = job
		s.mailSearchMu.Unlock()
		defer func() {
			s.mailSearchMu.Lock()
			if s.mailSearchJobs[req.SessionID] == job {
				delete(s.mailSearchJobs, req.SessionID)
			}
			s.mailSearchMu.Unlock()
		}()
	}

	accounts, err := s.accountStore.List()
	if err != nil {
		return nil, err
	}
	selected := []*account.Account{}
	ids := []string{}
	for _, acc := range accounts {
		if acc.Enabled && (len(req.AccountIDs) == 0 || slices.Contains(req.AccountIDs, acc.ID)) {
			selected = append(selected, acc)
			ids = append(ids, acc.ID)
		}
	}
	sort.Strings(ids)
	folderIDs := append([]string(nil), req.FolderIDs...)
	sort.Strings(folderIDs)
	state := mailCursor{Query: req.Query, Virtual: req.VirtualFolderID, Filter: req.Filter, Accounts: ids, Folders: folderIDs, Pages: map[string]string{}, Seen: map[string]bool{}}
	if req.Cursor != "" {
		if len(req.Cursor) > 512<<10 {
			return nil, fmt.Errorf("search cursor too large; start a new search")
		}
		decoded, e := base64.RawURLEncoding.DecodeString(req.Cursor)
		if e != nil {
			return nil, fmt.Errorf("invalid search cursor")
		}
		var saved mailCursor
		if json.Unmarshal(decoded, &saved) != nil || saved.Query != req.Query || saved.Virtual != req.VirtualFolderID || saved.Filter != req.Filter || !slices.Equal(saved.Accounts, ids) || !slices.Equal(saved.Folders, folderIDs) || saved.Pages == nil || saved.Seen == nil {
			return nil, fmt.Errorf("search scope changed; start again")
		}
		state = saved
	}
	var vf *virtualfolder.VirtualFolder
	foldersByAccount := map[string][]*folder.Folder{}
	for _, folderID := range folderIDs {
		f, folderErr := s.folderStore.Get(folderID)
		if folderErr != nil {
			return nil, folderErr
		}
		if f == nil || !slices.Contains(ids, f.AccountID) {
			return nil, fmt.Errorf("folder %s is outside the selected account scope", folderID)
		}
		foldersByAccount[f.AccountID] = append(foldersByAccount[f.AccountID], f)
	}
	if req.VirtualFolderID != "" {
		if len(folderIDs) > 0 {
			return nil, errors.New("folder and virtual-folder scopes cannot be combined")
		}
		vf, err = s.virtualFolderStore.Get(req.VirtualFolderID)
		if err != nil {
			return nil, err
		}
		if vf == nil {
			return nil, fmt.Errorf("virtual folder not found")
		}
		for _, id := range vf.FolderIDs {
			f, err := s.folderStore.Get(id)
			if err != nil {
				return nil, err
			}
			if f != nil {
				foldersByAccount[f.AccountID] = append(foldersByAccount[f.AccountID], f)
			}
		}
		// A saved-search virtual folder carries its text query: listing the
		// folder applies it, and a typed in-folder search ANDs onto it. The
		// cursor state above tracks the raw client query, so paging stays
		// consistent while the stored query lives only in the folder.
		if combined := combineVirtualFolderQueries(vf.SearchQuery, req.Query); combined != req.Query {
			req.Query = combined
		}
	}
	result := &MailSearchPage{Conversations: []*message.Conversation{}, Issues: []MailSearchIssue{}}
	limit := req.Limit
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, 3)
	for _, acc := range selected {
		acc := acc
		scope := foldersByAccount[acc.ID]
		mu.Lock()
		token := state.Pages[acc.ID]
		if vf != nil && vf.FolderMode != "exclude" && len(scope) == 0 {
			state.Pages[acc.ID] = "!done"
			token = "!done"
		}
		mu.Unlock()
		if req.Filter == "replyLater" || req.Filter == "readLater" {
			hasQueued := false
			for ref := range queued {
				if strings.HasPrefix(ref, acc.ID+"\x00") {
					hasQueued = true
					break
				}
			}
			if !hasQueued {
				mu.Lock()
				state.Pages[acc.ID] = "!done"
				mu.Unlock()
				token = "!done"
			}
		}
		if token == "!done" {
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			issue := func(err error) {
				mu.Lock()
				defer mu.Unlock()
				for _, prior := range result.Issues {
					if prior.AccountID == acc.ID && prior.Message == err.Error() {
						return
					}
				}
				text := err.Error()
				if errors.Is(err, gmail.ErrRateLimited) {
					text = "Gmail is temporarily rate limiting this account. Requests are paused; retry shortly."
				}
				result.Issues = append(result.Issues, MailSearchIssue{AccountID: acc.ID, AccountName: acc.Name, Message: text, Reauthorize: errors.Is(err, gmail.ErrReauthRequired) || errors.Is(err, gmail.ErrInsufficientScope)})
			}
			c, err := s.syncEngine.GmailClient(ctx, acc.ID)
			if errors.Is(err, gmail.ErrNotCapable) {
				convs, next, e := s.searchIMAPAccountPage(ctx, acc, vf, scope, req.Query, token, limit)
				if e != nil {
					issue(e)
					return
				}
				mu.Lock()
				for _, conv := range convs {
					if matchesFilter(conv) {
						result.Conversations = append(result.Conversations, conv)
					}
				}
				state.Pages[acc.ID] = next
				mu.Unlock()
				return
			}
			if err != nil {
				issue(err)
				return
			}
			unlock, lockErr := s.syncEngine.LockGmailAccount(ctx, acc.ID)
			if lockErr != nil {
				issue(lockErr)
				return
			}
			defer func() {
				if unlock != nil {
					unlock()
				}
			}()

			query := GmailViewQuery(req.Query, scope, vf != nil && vf.FolderMode == "exclude")
			if req.Filter == "unread" {
				query += " is:unread"
			}
			pageLimit := min(limit, 25)
			if req.Filter == "replyLater" || req.Filter == "readLater" {
				pageLimit = 100
			}
			page, err := c.Threads(ctx, gmail.SearchRequest{Query: query, PageToken: token, Limit: pageLimit, IncludeSpamTrash: true})
			if err != nil {
				issue(err)
				return
			}
			threadIDs := page.ThreadIDs
			if req.Filter == "replyLater" || req.Filter == "readLater" {
				// Intersect provider scope with explicit local queue membership before
				// fetching metadata. Advance a bounded number of cheap ID pages so a
				// sparse queue does not hydrate unrelated mail or exhaust quota.
				for scan := 0; ; scan++ {
					threadIDs = nil
					for _, id := range page.ThreadIDs {
						if queued[acc.ID+"\x00gmail:"+acc.ID+":"+id] {
							threadIDs = append(threadIDs, id)
						}
					}
					if len(threadIDs) > 0 || page.NextPageToken == "" || scan >= 9 {
						break
					}
					page, err = c.Threads(ctx, gmail.SearchRequest{Query: query, PageToken: page.NextPageToken, Limit: pageLimit, IncludeSpamTrash: true})
					if err != nil {
						issue(err)
						return
					}
				}
			}
			preferred := []string{}
			for _, f := range scope {
				preferred = append(preferred, f.ID)
			}
			if vf != nil && vf.FolderMode == "exclude" {
				preferred = nil
			}
			threads := map[string][]gmail.Message{}
			var threadMu sync.Mutex
			var threadWG sync.WaitGroup
			threadSlots := make(chan struct{}, 4)
			var threadErr error
			threadCtx, stopThreads := context.WithCancel(ctx)
			defer stopThreads()
		enqueue:
			for _, tid := range threadIDs {
				tid := tid
				select {
				case threadSlots <- struct{}{}:
				case <-threadCtx.Done():
					break enqueue
				}
				threadWG.Add(1)
				go func() {
					defer threadWG.Done()
					defer func() { <-threadSlots }()
					ms, err := c.ThreadMetadata(threadCtx, tid)
					threadMu.Lock()
					defer threadMu.Unlock()
					if err != nil {
						if threadErr == nil {
							threadErr = err
							stopThreads()
						}
						return
					}
					threads[tid] = ms
				}()
			}
			threadWG.Wait()
			if threadErr != nil {
				issue(threadErr)
				return
			}

			headers := []gmail.Message{}
			for _, tid := range threadIDs {
				headers = append(headers, threads[tid]...)
			}
			cached, err := s.syncEngine.CacheGmailResults(ctx, acc.ID, headers, preferred)
			if err != nil {
				issue(err)
				return
			}
			unlock()
			unlock = nil
			localThreads := map[string][]*message.Message{}
			for _, m := range cached {
				tid := strings.TrimPrefix(m.ThreadID, "gmail:"+acc.ID+":")
				localThreads[tid] = append(localThreads[tid], m)
			}
			convs := []*message.Conversation{}
			newSeen := []string{}
			for _, tid := range threadIDs {
				key := acc.ID + ":" + tid
				mu.Lock()
				seen := state.Seen[key]
				mu.Unlock()
				if seen {
					continue
				}
				thread := threads[tid]
				if !MatchesGmailThread(thread, vf) {
					newSeen = append(newSeen, key)
					continue
				}
				local := localThreads[tid]
				if len(local) == 0 {
					continue
				}
				conv := &message.Conversation{ThreadID: local[0].ThreadID, Subject: local[0].Subject, AccountID: acc.ID, AccountName: acc.Name, FolderID: local[0].FolderID, MessageCount: len(thread), Participants: []message.Address{}, MessageIDs: []string{}}
				people := map[string]bool{}
				for _, m := range local {
					conv.MessageIDs = append(conv.MessageIDs, m.ID)
				}
				for _, m := range thread {
					if slices.Contains(m.LabelIDs, "UNREAD") {
						conv.UnreadCount++
					}
					conv.IsStarred = conv.IsStarred || slices.Contains(m.LabelIDs, "STARRED")
					if m.Date.After(conv.LatestDate) {
						conv.LatestDate = m.Date
						conv.Snippet = m.Snippet
					}
					for _, p := range local {
						if !people[p.FromEmail] {
							people[p.FromEmail] = true
							conv.Participants = append(conv.Participants, message.Address{Name: p.FromName, Email: p.FromEmail})
						}
					}
				}
				if matchesFilter(conv) {
					convs = append(convs, conv)
				}
				newSeen = append(newSeen, key)
			}
			mu.Lock()
			defer mu.Unlock()
			result.Conversations = append(result.Conversations, convs...)
			for _, key := range newSeen {
				state.Seen[key] = true
			}
			if len(state.Seen) > 2048 {
				state.Seen = map[string]bool{}
				for _, key := range newSeen {
					state.Seen[key] = true
				}
			}
			state.Pages[acc.ID] = page.NextPageToken
			if page.NextPageToken == "" {
				state.Pages[acc.ID] = "!done"
			}
		}()
	}
	wg.Wait()
	// A superseded or canceled view is not a provider failure. Do not return
	// account issues (or partial rows) for a request that no longer owns the UI.
	if errors.Is(ctx.Err(), context.Canceled) {
		return nil, ctx.Err()
	}
	sort.Slice(result.Conversations, func(i, j int) bool {
		return result.Conversations[i].LatestDate.After(result.Conversations[j].LatestDate)
	})
	pending := false
	for _, id := range ids {
		if state.Pages[id] != "!done" {
			pending = true
		}
	}
	if pending {
		encoded, err := json.Marshal(state)
		if err != nil {
			return nil, err
		}
		result.Cursor = base64.RawURLEncoding.EncodeToString(encoded)
	}
	return result, nil
}

type mailSearchJob struct{ cancel context.CancelFunc }

func (s *Service) Cancel(sessionID string) {
	s.mailSearchMu.Lock()
	defer s.mailSearchMu.Unlock()
	if job := s.mailSearchJobs[sessionID]; job != nil {
		job.cancel()
		delete(s.mailSearchJobs, sessionID)
	}
}

// imapAccountPage advances one folder at a time. A failed page retains its
// original cursor, so retry cannot silently skip the folder or older messages.
type imapAccountPage struct {
	Folders []string                 `json:"f"`
	Index   int                      `json:"i"`
	Cursor  *syncre.IMAPSearchCursor `json:"c,omitempty"`
}

func (s *Service) searchIMAPAccountPage(ctx context.Context, acc *account.Account, vf *virtualfolder.VirtualFolder, selectedFolders []*folder.Folder, query, token string, limit int) ([]*message.Conversation, string, error) {
	var ids []string
	if vf != nil {
		fs, err := s.folderStore.List(acc.ID)
		if err != nil {
			return nil, "", err
		}
		for _, f := range fs {
			if slices.Contains(vf.FolderIDs, f.ID) == (vf.FolderMode == "exclude") {
				continue
			}
			ids = append(ids, f.ID)
		}
	} else if len(selectedFolders) > 0 {
		for _, f := range selectedFolders {
			ids = append(ids, f.ID)
		}
	} else {
		fs, err := s.folderStore.List(acc.ID)
		if err != nil {
			return nil, "", err
		}
		for _, f := range fs {
			if vf != nil && slices.Contains(vf.FolderIDs, f.ID) == (vf.FolderMode == "exclude") {
				continue
			}
			ids = append(ids, f.ID)
		}
	}
	sort.Strings(ids)
	state := imapAccountPage{Folders: ids}
	if token != "" {
		if json.Unmarshal([]byte(token), &state) != nil || !slices.Equal(state.Folders, ids) || state.Index < 0 || state.Index >= len(ids) {
			return nil, "", fmt.Errorf("folder scope changed; restart search")
		}
	}
	if len(ids) == 0 {
		return nil, "!done", nil
	}
	page, err := s.syncEngine.SearchIMAPPage(ctx, acc.ID, ids[state.Index], query, state.Cursor, limit)
	if err != nil {
		return nil, "", err
	}
	ms, err := s.syncEngine.CacheIMAPSearchHeaders(acc.ID, ids[state.Index], page.Results)
	if err != nil {
		return nil, "", err
	}
	convs := []*message.Conversation{}
	for _, m := range ms {
		unread := 0
		if !m.IsRead {
			unread = 1
		}
		convs = append(convs, &message.Conversation{ThreadID: m.ThreadID, Subject: m.Subject, Snippet: m.Snippet, LatestDate: m.Date, MessageIDs: []string{m.ID}, MessageCount: 1, UnreadCount: unread, IsStarred: m.IsStarred, AccountID: acc.ID, AccountName: acc.Name, FolderID: m.FolderID, Participants: []message.Address{{Name: m.FromName, Email: m.FromEmail}}})
	}
	state.Cursor = page.Next
	if page.Next == nil {
		state.Index++
	}
	if state.Index == len(ids) {
		return convs, "!done", nil
	}
	encoded, err := json.Marshal(state)
	return convs, string(encoded), err
}

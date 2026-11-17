package app

import (
	"context"
	"errors"
	"fmt"
	"strings"

	v1 "github.com/hkdb/aerion/internal/automation/v1"
	"github.com/hkdb/aerion/internal/folder"
	"github.com/hkdb/aerion/internal/gmail"
	"github.com/hkdb/aerion/internal/mailservice"
	"github.com/hkdb/aerion/internal/message"
	"github.com/hkdb/aerion/internal/views"
	"github.com/hkdb/aerion/internal/virtualfolder"
)

type MailSearchRequest = mailservice.MailSearchRequest
type MailSearchIssue = mailservice.MailSearchIssue
type MailSearchPage = mailservice.MailSearchPage

func gmailViewQuery(query string, folders []*folder.Folder, exclude bool) string {
	return mailservice.GmailViewQuery(query, folders, exclude)
}

func matchesGmailThread(items []gmail.Message, vf *virtualfolder.VirtualFolder) bool {
	return mailservice.MatchesGmailThread(items, vf)
}

func (a *App) currentMailService() *mailservice.Service {
	a.mailServiceMu.Lock()
	defer a.mailServiceMu.Unlock()
	if a.mailService == nil {
		a.mailService = mailservice.New(mailservice.Options{
			Accounts:       a.accountStore,
			Folders:        a.folderStore,
			Messages:       a.messageStore,
			ReplyLater:     a.replyLaterStore,
			Triage:         a.triageStore,
			VirtualFolders: a.virtualFolderStore,
			Sync:           a.syncEngine,
			ReplyContent:   a.replyContent,
			SyncTriage:     a.syncGmailTriageQueues,
		})
	}
	return a.mailService
}

// SearchMail keeps the desktop API stable while sharing its provider-aware
// search operation with the headless service.
func (a *App) SearchMail(req MailSearchRequest) (*MailSearchPage, error) {
	if page, handled, err := a.searchMailViaService(req); handled || err != nil {
		return page, err
	}
	if a.remoteServiceMode {
		return nil, errors.New("requested search scope is not available from the remote mailbox service")
	}
	return a.currentMailService().Search(a.ctx, req)
}

// CancelMailSearch cancels a search by its UI session identifier.
func (a *App) CancelMailSearch(sessionID string) {
	if a.mailboxService != nil {
		a.mailboxService.cancelSearch(sessionID)
		if a.remoteServiceMode {
			return
		}
	}
	a.currentMailService().Cancel(sessionID)
}

func (a *App) searchMailViaService(req MailSearchRequest) (*MailSearchPage, bool, error) {
	if a.mailboxService == nil {
		return nil, false, nil
	}
	if len(req.FolderIDs) > 0 {
		if a.remoteServiceMode {
			return nil, true, errors.New("folder-ID search scopes are not available from the remote mailbox service")
		}
		return nil, false, nil
	}
	status := a.GetMailboxServiceStatus()
	if !status.Connected {
		if status.ErrorCode == "service_unavailable" {
			return nil, false, nil
		}
		return nil, true, fmt.Errorf("local mailbox service unavailable: %s", status.Message)
	}
	if req.VirtualFolderID == "" && len(req.AccountIDs) > 1 {
		return nil, true, errors.New("the mailbox service search accepts at most one selected account")
	}
	if len(req.SessionID) > 128 {
		return nil, true, errors.New("invalid search session")
	}
	query := strings.TrimSpace(req.Query)
	if query == "" && req.VirtualFolderID == "" {
		return &MailSearchPage{Conversations: []*message.Conversation{}, Issues: []MailSearchIssue{}}, true, nil
	}
	var scope views.Scope
	if req.VirtualFolderID != "" {
		within := views.Scope{Kind: views.ScopeVirtualFolder, VirtualFolderID: req.VirtualFolderID}
		if query == "" {
			scope = within
		} else {
			scope = views.Scope{Kind: views.ScopeSearch, Query: query, Within: &within}
		}
	} else {
		var within views.Scope
		if len(req.AccountIDs) == 1 {
			within = views.Scope{Kind: views.ScopeAccount, AccountID: req.AccountIDs[0]}
		} else {
			within = views.Scope{Kind: views.ScopeAllAccounts}
		}
		scope = views.Scope{Kind: views.ScopeSearch, Query: query, Within: &within}
	}
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	page, err := a.mailboxService.search(ctx, req.SessionID, v1.SearchRequest{
		Scope:  scope,
		Page:   v1.PageRequest{Cursor: req.Cursor, Limit: req.Limit},
		Filter: req.Filter,
	})
	if err != nil {
		return nil, true, err
	}
	result := &MailSearchPage{Conversations: make([]*message.Conversation, 0, len(page.Items)), Issues: make([]MailSearchIssue, 0, len(page.Errors))}
	for _, item := range page.Items {
		participants := make([]message.Address, 0, len(item.Participants))
		for _, participant := range item.Participants {
			participants = append(participants, message.Address{Name: participant.Name, Email: participant.Email})
		}
		messageIDs := make([]string, 0, len(item.Matched))
		for _, ref := range item.Matched {
			if ref.AccountID == item.Ref.AccountID {
				messageIDs = append(messageIDs, ref.MessageID)
			}
		}
		result.Conversations = append(result.Conversations, &message.Conversation{
			ThreadID:       item.Ref.ThreadID,
			AccountID:      item.Ref.AccountID,
			AccountName:    item.AccountName,
			FolderID:       item.FolderID,
			Subject:        item.Subject,
			Snippet:        item.Snippet,
			MessageCount:   item.MessageCount,
			UnreadCount:    item.UnreadCount,
			HasAttachments: item.HasAttachments,
			IsStarred:      item.IsStarred,
			LatestDate:     item.LatestDate,
			MessageIDs:     messageIDs,
			Participants:   participants,
		})
	}
	for _, issue := range page.Errors {
		result.Issues = append(result.Issues, MailSearchIssue{
			AccountID:   issue.AccountID,
			AccountName: issue.AccountName,
			Message:     issue.Message,
			Reauthorize: issue.Reauthorize,
			Warning:     issue.Code == "stale_source",
		})
	}
	result.Cursor = page.NextCursor
	return result, true, nil
}

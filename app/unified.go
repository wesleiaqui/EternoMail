package app

import (
	"errors"
	"fmt"
	"sort"

	v1 "github.com/hkdb/aerion/internal/automation/v1"
	"github.com/hkdb/aerion/internal/folder"
	"github.com/hkdb/aerion/internal/message"
	"github.com/hkdb/aerion/internal/views"
)

// unifiedSpecialFolders resolves the real folders represented by a synthetic
// unified navigation type. GetSpecialFolder is deliberately the single source
// of truth: it honors an account's configured path before folder_type.
func (a *App) unifiedSpecialFolders(folderType string) ([]*folder.Folder, error) {
	if folderType == "replyLater" || folderType == "readLater" || folderType == "snoozed" {
		accounts, err := a.accountStore.List()
		if err != nil {
			return nil, err
		}
		folders := []*folder.Folder{}
		for _, account := range accounts {
			if !account.Enabled {
				continue
			}
			items, err := a.folderStore.List(account.ID)
			if err != nil {
				return nil, err
			}
			folders = append(folders, items...)
		}
		return folders, nil
	}
	typeValue := folder.Type(folderType)
	switch typeValue {
	case folder.TypeInbox, folder.TypeDrafts, folder.TypeSent, folder.TypeTrash, folder.TypeStarred, folder.TypeArchive, folder.TypeSpam, folder.TypeAll:
	default:
		return nil, fmt.Errorf("unsupported unified folder type: %s", folderType)
	}
	accounts, err := a.accountStore.List()
	if err != nil {
		return nil, err
	}
	resolved := make([]*folder.Folder, 0, len(accounts))
	for _, account := range accounts {
		if !account.Enabled {
			continue
		}
		item, err := a.GetSpecialFolder(account.ID, typeValue)
		if err != nil {
			return nil, err
		}
		if item != nil {
			resolved = append(resolved, item)
		}
	}
	return resolved, nil
}

func (a *App) unifiedSpecialFolderIDs(folderType string) ([]string, error) {
	folders, err := a.unifiedSpecialFolders(folderType)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(folders))
	for _, item := range folders {
		ids = append(ids, item.ID)
	}
	return ids, nil
}

func (a *App) GetUnifiedFolderConversations(folderType string, offset, limit int, sortOrder, filterValue string) ([]*message.Conversation, error) {
	filterValue = replyLaterViewFilter(folderType, filterValue)
	if folderType == string(folder.TypeInbox) {
		conversations, handled, err := a.listServiceConversations(views.Scope{Kind: views.ScopeAllInboxes}, offset, limit, sortOrder, filterValue)
		if err != nil {
			return nil, err
		}
		if handled {
			go a.enqueueUnifiedInboxConversationPrefetch(conversations)
			return conversations, nil
		}
	}
	if a.remoteServiceMode {
		if !isRemoteUnifiedFolderType(folderType) {
			return nil, fmt.Errorf("unified %s conversations are not available from the remote mailbox service", folderType)
		}
		conversations, handled, err := a.listServiceConversations(views.Scope{Kind: views.ScopeUnifiedFolder, FolderType: folderType}, offset, limit, sortOrder, filterValue)
		if err != nil {
			return nil, err
		}
		if !handled {
			return nil, errors.New("remote mailbox service is not connected")
		}
		return conversations, nil
	}
	if folderType == "readLater" || folderType == "replyLater" {
		if offset == 0 {
			a.syncGmailTriageQueues("read-later")
			a.syncGmailTriageQueues("reply-later")
		}
		return a.messageStore.ListReadLaterConversations(offset, limit, sortOrder, filterValue)
	}
	if folderType == "snoozed" {
		return a.messageStore.ListSnoozedConversations(offset, limit, sortOrder, filterValue)
	}
	if folderType == string(folder.TypeArchive) {
		return a.listUnifiedArchived(offset, limit, sortOrder, filterValue)
	}
	ids, err := a.unifiedSpecialFolderIDs(folderType)
	if err != nil {
		return nil, err
	}
	return a.messageStore.ListConversationsByFolderIDs(ids, offset, limit, sortOrder, filterValue)
}

func (a *App) listUnifiedArchived(offset, limit int, sortOrder, filterValue string) ([]*message.Conversation, error) {
	accounts, err := a.accountStore.List()
	if err != nil {
		return nil, err
	}
	var physicalIDs, allMailIDs []string
	for _, account := range accounts {
		if !account.Enabled {
			continue
		}
		physical, err := a.GetSpecialFolder(account.ID, folder.TypeArchive)
		if err != nil {
			return nil, err
		}
		if physical != nil {
			physicalIDs = append(physicalIDs, physical.ID)
			continue
		}
		allMail, err := a.GetSpecialFolder(account.ID, folder.TypeAll)
		if err != nil {
			return nil, err
		}
		if allMail != nil {
			allMailIDs = append(allMailIDs, allMail.ID)
		}
	}
	// Fetch enough rows to merge physical and virtual sources globally before
	// applying pagination. Archive sources are normally small header lists.
	physical, err := a.messageStore.ListConversationsByFolderIDs(physicalIDs, 0, offset+limit, sortOrder, filterValue)
	if err != nil {
		return nil, err
	}
	virtual, err := a.messageStore.ListVirtualArchivedConversations(allMailIDs, 0, offset+limit, sortOrder, filterValue)
	if err != nil {
		return nil, err
	}
	combined := append(physical, virtual...)
	sort.SliceStable(combined, func(i, j int) bool {
		if sortOrder == "oldest" {
			return combined[i].LatestDate.Before(combined[j].LatestDate)
		}
		return combined[i].LatestDate.After(combined[j].LatestDate)
	})
	if offset >= len(combined) {
		return []*message.Conversation{}, nil
	}
	end := offset + limit
	if end > len(combined) {
		end = len(combined)
	}
	return combined[offset:end], nil
}

func (a *App) GetUnifiedFolderCount(folderType, filterValue string) (int, error) {
	filterValue = replyLaterViewFilter(folderType, filterValue)
	if folderType == string(folder.TypeInbox) {
		count, handled, err := a.serviceConversationCount(views.Scope{Kind: views.ScopeAllInboxes}, filterValue)
		if handled || err != nil {
			return count, err
		}
	}
	if a.remoteServiceMode {
		if !isRemoteUnifiedFolderType(folderType) {
			return 0, fmt.Errorf("unified %s count is not available from the remote mailbox service", folderType)
		}
		count, handled, err := a.serviceConversationCount(views.Scope{Kind: views.ScopeUnifiedFolder, FolderType: folderType}, filterValue)
		if err != nil {
			return 0, err
		}
		if !handled {
			return 0, errors.New("remote mailbox service is not connected")
		}
		return count, nil
	}
	if folderType == "readLater" || folderType == "replyLater" {
		return a.messageStore.CountReadLaterConversations(filterValue)
	}
	if folderType == "snoozed" {
		return a.messageStore.CountSnoozedConversations(filterValue)
	}
	if folderType == string(folder.TypeArchive) {
		// The list path is authoritative for mixed physical/virtual archive
		// sources; count it without exposing synthetic folder IDs.
		items, err := a.listUnifiedArchived(0, 100000, "newest", filterValue)
		return len(items), err
	}
	ids, err := a.unifiedSpecialFolderIDs(folderType)
	if err != nil {
		return 0, err
	}
	return a.messageStore.CountConversationsByFolderIDs(ids, filterValue)
}

func (a *App) SearchUnifiedFolder(folderType, query string, offset, limit int, filterValue string) ([]*message.ConversationSearchResult, error) {
	filterValue = replyLaterViewFilter(folderType, filterValue)
	if a.remoteServiceMode {
		page, err := a.searchRemoteUnifiedFolder(folderType, query, offset, limit, filterValue)
		if err != nil {
			return nil, err
		}
		return page.Results, nil
	}
	ids, err := a.unifiedSpecialFolderIDs(folderType)
	if err != nil {
		return nil, err
	}
	results, _, err := a.messageStore.SearchConversationsByFolderIDs(ids, query, offset, limit, filterValue)
	return results, err
}

func (a *App) GetSearchCountUnifiedFolder(folderType, query, filterValue string) (int, error) {
	filterValue = replyLaterViewFilter(folderType, filterValue)
	if a.remoteServiceMode {
		page, err := a.searchRemoteUnifiedFolder(folderType, query, 0, 1, filterValue)
		if err != nil {
			return 0, err
		}
		return page.TotalCount, nil
	}
	ids, err := a.unifiedSpecialFolderIDs(folderType)
	if err != nil {
		return 0, err
	}
	_, count, err := a.messageStore.SearchConversationsByFolderIDs(ids, query, 0, 0, filterValue)
	return count, err
}

func isRemoteUnifiedFolderType(folderType string) bool {
	if folderType == "readLater" || folderType == "replyLater" {
		return true
	}
	switch folder.Type(folderType) {
	case folder.TypeDrafts, folder.TypeSent, folder.TypeTrash, folder.TypeStarred, folder.TypeArchive, folder.TypeSpam:
		return true
	default:
		return false
	}
}

func (a *App) searchRemoteUnifiedFolder(folderType, query string, offset, limit int, filterValue string) (*v1.ConversationSearchPage, error) {
	if !isRemoteUnifiedFolderType(folderType) {
		return nil, fmt.Errorf("unified %s search is not available from the remote mailbox service", folderType)
	}
	if a.mailboxService == nil {
		return nil, errors.New("remote mailbox service is not connected")
	}
	ctx := a.actionContext()
	status := a.mailboxService.status(ctx)
	if !status.Connected {
		return nil, fmt.Errorf("remote mailbox service search unavailable: %s", status.Message)
	}
	return a.mailboxService.searchUnifiedFolder(ctx, v1.ConversationSearchRequest{
		Scope: views.Scope{Kind: views.ScopeUnifiedFolder, FolderType: folderType},
		Query: query, Offset: offset, Limit: limit, Filter: filterValue,
	})
}

// SyncUnifiedFolder syncs each represented real folder. It never passes a
// synthetic navigation ID to the sync engine.
func (a *App) SyncUnifiedFolder(folderType string) error {
	if a.remoteServiceMode {
		return errors.New("mailbox sync is owned by the configured remote helper")
	}
	if folderType == "replyLater" || folderType == "readLater" || folderType == "snoozed" {
		if folderType == "snoozed" {
			return nil
		}
		queue := "reply-later"
		if folderType == "readLater" {
			queue = "read-later"
		}
		a.syncGmailTriageQueues(queue)
		return nil
	}
	folders, err := a.unifiedSpecialFolders(folderType)
	if err != nil {
		return err
	}
	for _, item := range folders {
		if err := a.SyncFolder(item.AccountID, item.ID); err != nil {
			return err
		}
	}
	return nil
}

func (a *App) EmptyUnifiedTrash() error {
	folders, err := a.unifiedSpecialFolders(string(folder.TypeTrash))
	if err != nil {
		return err
	}
	for _, item := range folders {
		if err := a.EmptyTrash(item.AccountID, item.ID); err != nil {
			return err
		}
	}
	return nil
}

// replyLaterViewFilter keeps queue membership applied when quick filters or
// local search further narrow the first-class queue view.
func replyLaterViewFilter(folderType, filter string) string {
	if folderType != "replyLater" && folderType != "readLater" {
		return filter
	}
	queueFilter := folderType
	if filter == "" || filter == queueFilter {
		return queueFilter
	}
	return queueFilter + "+" + filter
}

package app

import (
	"fmt"
	"strings"

	"github.com/hkdb/aerion/internal/message"
	"github.com/hkdb/aerion/internal/virtualfolder"
)

// SyncVirtualFolder refreshes the real member folders' headers and flags. Full
// message bodies and attachments remain on demand, as with ordinary SyncFolder.
func (a *App) SyncVirtualFolder(virtualFolderID string) error {
	vf, err := a.virtualFolderStore.Get(virtualFolderID)
	if err != nil {
		return fmt.Errorf("failed to get virtual folder: %w", err)
	}
	if vf == nil {
		return fmt.Errorf("virtual folder not found: %s", virtualFolderID)
	}
	folderIDs, err := a.virtualFolderStore.QueryFolderIDs(vf)
	if err != nil {
		return fmt.Errorf("failed to resolve virtual folder members: %w", err)
	}
	var syncErrors []string
	for _, folderID := range folderIDs {
		if err := a.ctx.Err(); err != nil {
			return err
		}
		member, err := a.folderStore.Get(folderID)
		if err != nil {
			syncErrors = append(syncErrors, fmt.Sprintf("%s: %v", folderID, err))
			continue
		}
		if member == nil {
			continue
		}
		acct, err := a.accountStore.Get(member.AccountID)
		if err != nil {
			syncErrors = append(syncErrors, fmt.Sprintf("%s: %v", folderID, err))
			continue
		}
		if acct == nil || !acct.Enabled {
			continue
		}
		if err := a.SyncFolder(member.AccountID, member.ID); err != nil {
			syncErrors = append(syncErrors, fmt.Sprintf("%s: %v", member.Path, err))
		}
	}
	if len(syncErrors) > 0 {
		return fmt.Errorf("some virtual folder members failed to sync: %s", strings.Join(syncErrors, "; "))
	}
	return nil
}

// ============================================================================
// Virtual Folder API - Exposed to frontend via Wails bindings
// ============================================================================

// GetVirtualFolders returns all virtual folders
func (a *App) GetVirtualFolders() ([]*virtualfolder.VirtualFolder, error) {
	return a.virtualFolderStore.List()
}

// GetVirtualFolderUnreadCounts returns unread conversation counts for all virtual folders.
func (a *App) GetVirtualFolderUnreadCounts() (map[string]int, error) {
	return a.virtualFolderStore.UnreadCounts()
}

// CreateVirtualFolder creates a new virtual folder
func (a *App) CreateVirtualFolder(vf *virtualfolder.VirtualFolder) error {
	if vf == nil {
		return fmt.Errorf("virtual folder cannot be nil")
	}
	return a.virtualFolderStore.Create(vf)
}

// UpdateVirtualFolder updates an existing virtual folder
func (a *App) UpdateVirtualFolder(vf *virtualfolder.VirtualFolder) error {
	if vf == nil {
		return fmt.Errorf("virtual folder cannot be nil")
	}
	if vf.ID == "" {
		return fmt.Errorf("virtual folder ID cannot be empty")
	}
	return a.virtualFolderStore.Update(vf)
}

// ReorderVirtualFolders saves the sidebar order of virtual folders.
func (a *App) ReorderVirtualFolders(ids []string) error {
	return a.virtualFolderStore.Reorder(ids)
}

// DeleteVirtualFolder deletes a virtual folder
func (a *App) DeleteVirtualFolder(id string) error {
	if id == "" {
		return fmt.Errorf("virtual folder ID cannot be empty")
	}
	return a.virtualFolderStore.Delete(id)
}

// GetVirtualFolderConversations returns conversations matching a virtual folder's criteria
func (a *App) GetVirtualFolderConversations(virtualFolderID string, offset, limit int, sortOrder string) ([]*message.Conversation, error) {
	vf, err := a.virtualFolderStore.Get(virtualFolderID)
	if err != nil {
		return nil, fmt.Errorf("failed to get virtual folder: %w", err)
	}
	if vf == nil {
		return nil, fmt.Errorf("virtual folder not found: %s", virtualFolderID)
	}

	folderIDs, err := a.virtualFolderStore.QueryFolderIDs(vf)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve virtual folder members: %w", err)
	}
	if len(folderIDs) == 0 {
		return []*message.Conversation{}, nil
	}
	return a.messageStore.ListVirtualFolderConversations(folderIDs, offset, limit, sortOrder, virtualFolderMessageCriteria(vf))
}

// GetVirtualFolderCount returns the count of conversations matching a virtual folder's criteria.
func (a *App) GetVirtualFolderCount(virtualFolderID string) (int, error) {
	vf, err := a.virtualFolderStore.Get(virtualFolderID)
	if err != nil {
		return 0, fmt.Errorf("failed to get virtual folder: %w", err)
	}
	if vf == nil {
		return 0, fmt.Errorf("virtual folder not found: %s", virtualFolderID)
	}

	folderIDs, err := a.virtualFolderStore.QueryFolderIDs(vf)
	if err != nil {
		return 0, fmt.Errorf("failed to resolve virtual folder members: %w", err)
	}
	if len(folderIDs) == 0 {
		return 0, nil
	}
	return a.messageStore.CountVirtualFolderConversations(folderIDs, virtualFolderMessageCriteria(vf))
}

// SearchVirtualFolder searches messages in a virtual folder's member folders.
func (a *App) SearchVirtualFolder(virtualFolderID, query string, offset, limit int, _ string) ([]*message.ConversationSearchResult, error) {
	vf, err := a.virtualFolderStore.Get(virtualFolderID)
	if err != nil {
		return nil, fmt.Errorf("failed to get virtual folder: %w", err)
	}
	if vf == nil {
		return nil, fmt.Errorf("virtual folder not found: %s", virtualFolderID)
	}
	folderIDs, err := a.virtualFolderStore.QueryFolderIDs(vf)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve virtual folder members: %w", err)
	}
	results, _, err := a.messageStore.SearchVirtualFolderConversations(folderIDs, query, offset, limit, virtualFolderMessageCriteria(vf))
	return results, err
}

// GetVirtualFolderSearchCount returns the matching message-search count.
func (a *App) GetVirtualFolderSearchCount(virtualFolderID, query, _ string) (int, error) {
	vf, err := a.virtualFolderStore.Get(virtualFolderID)
	if err != nil {
		return 0, fmt.Errorf("failed to get virtual folder: %w", err)
	}
	if vf == nil {
		return 0, fmt.Errorf("virtual folder not found: %s", virtualFolderID)
	}
	folderIDs, err := a.virtualFolderStore.QueryFolderIDs(vf)
	if err != nil {
		return 0, fmt.Errorf("failed to resolve virtual folder members: %w", err)
	}
	_, count, err := a.messageStore.SearchVirtualFolderConversations(folderIDs, query, 0, 0, virtualFolderMessageCriteria(vf))
	return count, err
}

func virtualFolderMessageCriteria(vf *virtualfolder.VirtualFolder) message.VirtualFolderCriteria {
	return message.VirtualFolderCriteria{
		ReadStatus:  vf.ReadStatus,
		ReplyStatus: vf.ReplyStatus,
		StarStatus:  vf.StarStatus,
		DateWindow:  vf.DateWindow,
	}
}

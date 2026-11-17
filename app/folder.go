package app

import (
	"errors"
	"fmt"
	"github.com/hkdb/aerion/internal/gmail"

	"github.com/hkdb/aerion/internal/folder"
	"github.com/hkdb/aerion/internal/logging"
)

// ============================================================================
// Folder API - Exposed to frontend via Wails bindings
// ============================================================================

// GetFolders returns all folders for an account
func (a *App) GetFolders(accountID string) ([]*folder.Folder, error) {
	var folders []*folder.Folder
	var err error
	if a.mailboxService != nil {
		ctx := a.helperCommandContext()
		status := a.mailboxService.status(ctx)
		if status.Connected {
			folders, err = a.mailboxService.folders(ctx, accountID)
		} else if status.ErrorCode != "service_unavailable" || a.mailboxService.isRemote() {
			return nil, fmt.Errorf("list folders from mailbox service: %s", status.Message)
		}
	}
	if folders == nil && err == nil {
		folders, err = a.folderStore.List(accountID)
	}
	if err != nil {
		return nil, err
	}

	// Sort folders (special folders first)
	folder.SortFolders(folders)
	return folders, nil
}

// GetFolderTree returns folders as a tree structure
func (a *App) GetFolderTree(accountID string) ([]*folder.FolderTree, error) {
	folders, err := a.GetFolders(accountID)
	if err != nil {
		return nil, err
	}

	// Sort folders (INBOX first, then special folders, then alphabetically)
	folder.SortFolders(folders)
	return folder.BuildTree(folder.WithoutHidden(folders)), nil
}

// SyncFolders synchronizes the folder list with the IMAP server
func (a *App) SyncFolders(accountID string) error {
	if a.mailboxService != nil {
		ctx := a.helperCommandContext()
		status := a.mailboxService.status(ctx)
		if status.Connected {
			if !status.WriteAuthorized {
				return errDesktopMailboxWriteAuthorizationRequired
			}
			if err := a.mailboxService.syncFolders(ctx, accountID); err != nil {
				return fmt.Errorf("sync folders through mailbox service: %w", err)
			}
			return nil
		}
		if status.ErrorCode != "service_unavailable" {
			return fmt.Errorf("mailbox service is not ready for folder sync: %s", status.Message)
		}
	}
	if err := a.ensureSyncOwnership(); err != nil {
		return err
	}
	log := logging.WithComponent("app")
	err := a.syncEngine.SyncFolders(a.ctx, accountID)
	if err == nil {
		// Checkpoint WAL after heavy sync operation
		if checkpointErr := a.db.Checkpoint(); checkpointErr != nil {
			log.Warn().Err(checkpointErr).Msg("WAL checkpoint after SyncFolders failed")
		}
	}
	return err
}

// GetAccountFoldersForMapping returns all folders for an account (for folder mapping UI).
// Triggers a folder sync if no folders exist yet.
func (a *App) GetAccountFoldersForMapping(accountID string) ([]*folder.Folder, error) {
	log := logging.WithComponent("app")

	folders, err := a.GetFolders(accountID)
	if err != nil {
		return nil, fmt.Errorf("failed to list folders: %w", err)
	}

	// If no folders, trigger sync first
	if len(folders) == 0 {
		log.Info().Str("accountID", accountID).Msg("No folders found, triggering sync")
		if err := a.SyncFolders(accountID); err != nil {
			return nil, fmt.Errorf("failed to sync folders: %w", err)
		}
		folders, err = a.GetFolders(accountID)
		if err != nil {
			return nil, fmt.Errorf("failed to list folders after sync: %w", err)
		}
	}

	// Sort using existing logic
	folder.SortFolders(folders)
	return folder.WithoutHidden(folders), nil
}

// GetAutoDetectedFolders returns the auto-detected special folders for an account.
// Returns map of folder type -> folder path (e.g., {"sent": "Sent Mail", "trash": "Deleted Items"}).
func (a *App) GetAutoDetectedFolders(accountID string) (map[string]string, error) {
	folders, err := a.GetFolders(accountID)
	if err != nil {
		return nil, fmt.Errorf("failed to list folders: %w", err)
	}

	result := make(map[string]string)
	for _, f := range folders {
		if f.Type != folder.TypeFolder && f.Type != folder.TypeInbox {
			result[string(f.Type)] = f.Path
		}
	}
	return result, nil
}

func (a *App) setFolderSubscriptionThroughService(accountID, folderID string, subscribed bool) (bool, error) {
	if a.mailboxService == nil {
		return false, nil
	}
	ctx := a.helperCommandContext()
	status := a.mailboxService.status(ctx)
	if !status.Connected {
		if status.ErrorCode == "service_unavailable" {
			return false, nil
		}
		return true, fmt.Errorf("mailbox service is not ready for folder subscription: %s", status.Message)
	}
	if !status.WriteAuthorized {
		return true, errDesktopMailboxWriteAuthorizationRequired
	}
	if err := a.mailboxService.setFolderSubscription(ctx, accountID, folderID, subscribed); err != nil {
		return true, fmt.Errorf("update folder subscription through mailbox service: %w", err)
	}
	return true, nil
}

// GetSpecialFolder returns the folder for a special type, checking account mappings first.
// If no mapping is set, falls back to auto-detected folder type.
func (a *App) GetSpecialFolder(accountID string, folderType folder.Type) (*folder.Folder, error) {
	// Get account to check mappings
	acc, err := a.accountStore.Get(accountID)
	if err != nil {
		return nil, fmt.Errorf("failed to get account: %w", err)
	}
	if acc == nil {
		return nil, fmt.Errorf("account not found: %s", accountID)
	}
	folders, err := a.GetFolders(accountID)
	if err != nil {
		return nil, err
	}

	// Check if account has a manual mapping
	mappedPath := acc.GetFolderMapping(string(folderType))
	if mappedPath != "" {
		for _, f := range folders {
			if f.Path == mappedPath {
				return f, nil
			}
		}
		// Mapped folder not found, fall through to auto-detect
	}

	// Fall back to auto-detected type
	for _, f := range folders {
		if f.Type == folderType {
			return f, nil
		}
	}
	return nil, nil
}

// SubscribeFolder subscribes to an IMAP folder for automatic sync.
func (a *App) SubscribeFolder(accountID, folderID string) error {
	log := logging.WithComponent("app")
	if handled, err := a.setFolderSubscriptionThroughService(accountID, folderID, true); handled {
		return err
	}

	f, err := a.folderStore.Get(folderID)
	if err != nil {
		return fmt.Errorf("failed to get folder: %w", err)
	}
	if f == nil {
		return fmt.Errorf("folder not found: %s", folderID)
	}

	if f.AccountID != accountID {
		return fmt.Errorf("folder account mismatch")
	}
	if c, apiErr := a.syncEngine.GmailClient(a.ctx, accountID); apiErr == nil {
		label, err := a.syncEngine.GmailLabelID(a.ctx, c, f)
		if err != nil {
			return err
		}
		if label != "" {
			if err = c.SetLabelVisible(a.ctx, label, true); err != nil {
				return a.gmailMutationError(accountID, err)
			}
		}
		return a.folderStore.UpdateSubscribed(folderID, true)
	} else if !errors.Is(apiErr, gmail.ErrNotCapable) {
		return a.gmailMutationError(accountID, apiErr)
	}

	// Subscribe on IMAP server
	conn, connErr := a.syncEngine.GetPoolConnection(a.ctx, accountID)
	if connErr != nil {
		return fmt.Errorf("failed to get IMAP connection: %w", connErr)
	}
	defer a.syncEngine.ReleasePoolConnection(conn)

	if subErr := conn.Client().Subscribe(f.Path); subErr != nil {
		return fmt.Errorf("IMAP SUBSCRIBE failed: %w", subErr)
	}

	// Update local cache
	if dbErr := a.folderStore.UpdateSubscribed(folderID, true); dbErr != nil {
		log.Warn().Err(dbErr).Msg("Failed to update local subscription cache")
	}

	log.Info().Str("folder", f.Path).Msg("Subscribed to folder")
	return nil
}

// UnsubscribeFolder unsubscribes from an IMAP folder for automatic sync.
func (a *App) UnsubscribeFolder(accountID, folderID string) error {
	log := logging.WithComponent("app")
	if handled, err := a.setFolderSubscriptionThroughService(accountID, folderID, false); handled {
		return err
	}

	f, err := a.folderStore.Get(folderID)
	if err != nil {
		return fmt.Errorf("failed to get folder: %w", err)
	}
	if f == nil {
		return fmt.Errorf("folder not found: %s", folderID)
	}

	if f.AccountID != accountID {
		return fmt.Errorf("folder account mismatch")
	}
	if c, apiErr := a.syncEngine.GmailClient(a.ctx, accountID); apiErr == nil {
		label, err := a.syncEngine.GmailLabelID(a.ctx, c, f)
		if err != nil {
			return err
		}
		if label != "" {
			if err = c.SetLabelVisible(a.ctx, label, false); err != nil {
				return a.gmailMutationError(accountID, err)
			}
		}
		return a.folderStore.UpdateSubscribed(folderID, false)
	} else if !errors.Is(apiErr, gmail.ErrNotCapable) {
		return a.gmailMutationError(accountID, apiErr)
	}

	// Unsubscribe on IMAP server
	conn, connErr := a.syncEngine.GetPoolConnection(a.ctx, accountID)
	if connErr != nil {
		return fmt.Errorf("failed to get IMAP connection: %w", connErr)
	}
	defer a.syncEngine.ReleasePoolConnection(conn)

	if subErr := conn.Client().Unsubscribe(f.Path); subErr != nil {
		return fmt.Errorf("IMAP UNSUBSCRIBE failed: %w", subErr)
	}

	// Update local cache
	if dbErr := a.folderStore.UpdateSubscribed(folderID, false); dbErr != nil {
		log.Warn().Err(dbErr).Msg("Failed to update local subscription cache")
	}

	log.Info().Str("folder", f.Path).Msg("Unsubscribed from folder")
	return nil
}

// SubscribeAllFolders subscribes to all IMAP folders for an account.
func (a *App) SubscribeAllFolders(accountID string) error {
	log := logging.WithComponent("app")
	if a.mailboxService != nil {
		ctx := a.helperCommandContext()
		status := a.mailboxService.status(ctx)
		if status.Connected {
			if !status.WriteAuthorized {
				return errDesktopMailboxWriteAuthorizationRequired
			}
			if err := a.mailboxService.setAllFolderSubscriptions(ctx, accountID, true); err != nil {
				return fmt.Errorf("subscribe folders through mailbox service: %w", err)
			}
			return nil
		}
		if status.ErrorCode != "service_unavailable" {
			return fmt.Errorf("mailbox service is not ready for folder subscriptions: %s", status.Message)
		}
	}

	folders, err := a.GetFolders(accountID)
	if err != nil {
		return fmt.Errorf("failed to list folders: %w", err)
	}

	if _, apiErr := a.syncEngine.GmailClient(a.ctx, accountID); apiErr == nil {
		for _, f := range folders {
			if err = a.SubscribeFolder(accountID, f.ID); err != nil {
				return err
			}
		}
		return nil
	} else if !errors.Is(apiErr, gmail.ErrNotCapable) {
		return a.gmailMutationError(accountID, apiErr)
	}

	conn, connErr := a.syncEngine.GetPoolConnection(a.ctx, accountID)
	if connErr != nil {
		return fmt.Errorf("failed to get IMAP connection: %w", connErr)
	}
	defer a.syncEngine.ReleasePoolConnection(conn)

	for _, f := range folders {
		if subErr := conn.Client().Subscribe(f.Path); subErr != nil {
			log.Warn().Err(subErr).Str("folder", f.Path).Msg("Failed to subscribe")
			continue
		}
		if err := a.folderStore.UpdateSubscribed(f.ID, true); err != nil {
			log.Warn().Err(err).Str("folder", f.Path).Msg("Failed to update subscribed flag")
		}
	}

	log.Info().Str("accountID", accountID).Int("count", len(folders)).Msg("Subscribed to all folders")
	return nil
}

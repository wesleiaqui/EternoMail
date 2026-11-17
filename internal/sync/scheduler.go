package sync

import (
	"context"
	"errors"
	"github.com/hkdb/aerion/internal/gmail"
	"strings"
	"sync"
	"time"

	"github.com/hkdb/aerion/internal/account"
	"github.com/hkdb/aerion/internal/folder"
	"github.com/hkdb/aerion/internal/logging"
	"github.com/hkdb/aerion/internal/platform"
	"github.com/rs/zerolog"
)

// NewMailInfo contains information about newly arrived mail
type NewMailInfo struct {
	AccountID   string `json:"accountId"`
	AccountName string `json:"accountName"`
	FolderID    string `json:"folderId"`
	Subject     string `json:"subject"`
	FromName    string `json:"fromName"`
	FromEmail   string `json:"fromEmail"`
	Count       int    `json:"count"` // Number of new messages
}

// NewMailCallback is called when new mail arrives
type NewMailCallback func(info NewMailInfo)

// SyncCompletedCallback is called when a sync operation completes (success or error)
type SyncCompletedCallback func(accountID, folderID string, err error)

// Scheduler handles periodic background sync of email accounts
type Scheduler struct {
	engine       *Engine
	accountStore *account.Store
	folderStore  *folder.Store
	log          zerolog.Logger

	// Callbacks
	newMailCallback       NewMailCallback
	syncCompletedCallback SyncCompletedCallback
	isConnected           func() bool // optional: skip sync when offline

	// Control
	ctx           context.Context
	cancel        context.CancelFunc
	wg            sync.WaitGroup
	running       bool
	stopDone      chan struct{}
	runningMu     sync.Mutex
	checkInterval time.Duration

	// Track syncing accounts to prevent concurrent syncs
	syncing            map[string]bool
	syncDone           map[string]chan struct{}
	syncingMu          sync.Mutex
	hiddenInboxAttempt map[string]time.Time

	// Per-account cancellation for running syncs
	syncCancels  map[string]context.CancelFunc
	syncCancelMu sync.Mutex

	// activities keeps an in-flight sync out of App Nap (macOS); nil = no-op.
	activities *platform.ActivityManager
}

// NewScheduler creates a new sync scheduler
func NewScheduler(engine *Engine, accountStore *account.Store, folderStore *folder.Store) *Scheduler {
	return &Scheduler{
		engine:        engine,
		accountStore:  accountStore,
		folderStore:   folderStore,
		log:           logging.WithComponent("sync-scheduler"),
		checkInterval: 1 * time.Minute, // Check every minute if any account is due
		syncing:       make(map[string]bool),
		syncDone:      make(map[string]chan struct{}),
		syncCancels:   make(map[string]context.CancelFunc),
	}
}

// SetNewMailCallback sets the callback for new mail notifications
func (s *Scheduler) SetNewMailCallback(callback NewMailCallback) {
	s.newMailCallback = callback
}

// SetSyncCompletedCallback sets the callback for sync completion notifications
func (s *Scheduler) SetSyncCompletedCallback(callback SyncCompletedCallback) {
	s.syncCompletedCallback = callback
}

// SetConnectivityCheck sets a function to check network connectivity.
// When set, the scheduler skips sync ticks when offline to avoid wasted
// connection attempts and unnecessary error logging.
func (s *Scheduler) SetConnectivityCheck(check func() bool) {
	s.isConnected = check
}

// SetActivityManager lets the app share its power-assertion manager. While
// set, each in-flight account sync holds a background activity so App Nap
// cannot throttle a half-finished sync; between syncs the app stays
// nappable, which is the power saving the OS feature exists for.
func (s *Scheduler) SetActivityManager(m *platform.ActivityManager) {
	s.activities = m
}

// Start starts the background sync scheduler
func (s *Scheduler) Start(ctx context.Context) {
	s.runningMu.Lock()
	defer s.runningMu.Unlock()

	if s.running || s.stopDone != nil {
		s.log.Warn().Msg("Scheduler already running")
		return
	}

	s.ctx, s.cancel = context.WithCancel(ctx)
	s.running = true

	s.wg.Add(1)
	go s.run(s.ctx)

	s.log.Info().Msg("Email sync scheduler started")
}

// Stop stops the background sync scheduler
func (s *Scheduler) Stop() {
	s.runningMu.Lock()
	if done := s.stopDone; done != nil {
		s.runningMu.Unlock()
		<-done
		return
	}
	if !s.running {
		s.runningMu.Unlock()
		return
	}
	s.running = false
	done := make(chan struct{})
	s.stopDone = done
	s.cancel()
	s.runningMu.Unlock()
	s.wg.Wait()
	s.runningMu.Lock()
	s.stopDone = nil
	close(done)
	s.runningMu.Unlock()
	s.log.Info().Msg("Email sync scheduler stopped")
}

// startAccountSync registers work before Stop can start waiting.
func (s *Scheduler) startAccountSync(acc *account.Account) {
	s.runningMu.Lock()
	defer s.runningMu.Unlock()
	if !s.running || s.ctx.Err() != nil {
		return
	}
	ctx := s.ctx
	s.wg.Add(1)
	go func(ctx context.Context) {
		defer s.wg.Done()
		if ctx.Err() == nil {
			s.syncAccountInbox(ctx, acc)
		}
	}(ctx)
}

// run is the main scheduler loop
func (s *Scheduler) run(ctx context.Context) {
	defer s.wg.Done()

	// Initial sync on startup (after a short delay to let the app initialize)
	select {
	case <-time.After(10 * time.Second):
		s.syncDueAccounts(ctx)
	case <-ctx.Done():
		return
	}

	// Periodic check
	ticker := time.NewTicker(s.checkInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			s.syncDueAccounts(ctx)
		case <-ctx.Done():
			return
		}
	}
}

// syncDueAccounts checks all accounts and syncs those that are due
func (s *Scheduler) syncDueAccounts(ctx context.Context) {
	if ctx.Err() != nil {
		return
	}
	// Skip sync tick if we know we're offline
	if s.isConnected != nil && !s.isConnected() {
		s.log.Debug().Msg("Skipping sync tick — offline")
		return
	}

	accounts, err := s.accountStore.List()
	if err != nil {
		s.log.Error().Err(err).Msg("Failed to list accounts for sync check")
		return
	}

	for _, acc := range accounts {
		if ctx.Err() != nil {
			return
		}
		if !acc.Enabled {
			continue
		}

		// Skip manual-only accounts
		if acc.SyncInterval <= 0 {
			continue
		}

		// Check if sync is due for INBOX
		if !s.isSyncDue(acc) {
			continue
		}

		s.log.Debug().Str("account_id", acc.ID).Msg("Account is due for sync")

		// Sync in background (don't block the scheduler)
		s.startAccountSync(acc)
	}
}

// isSyncDue returns true if an account's INBOX is due for sync
func (s *Scheduler) isSyncDue(acc *account.Account) bool {
	// Get the INBOX folder for this account
	inbox, err := s.folderStore.GetByType(acc.ID, folder.TypeInbox)
	if err != nil {
		s.log.Warn().Err(err).Str("account", acc.ID).Msg("Failed to get INBOX folder")
		return true // Sync anyway to create the folder
	}
	if inbox == nil {
		return true // No INBOX yet, needs sync
	}

	if inbox.Hidden {
		s.syncingMu.Lock()
		last := s.hiddenInboxAttempt[acc.ID]
		s.syncingMu.Unlock()
		return last.IsZero() || time.Since(last) >= time.Duration(acc.SyncInterval)*time.Minute
	}

	// Never synced - definitely due
	if inbox.LastSync == nil {
		return true
	}

	// Calculate time since last sync
	elapsed := time.Since(*inbox.LastSync)
	if acc.AuthType == account.AuthOAuth2 && strings.EqualFold(acc.IMAPHost, "imap.gmail.com") {
		_, _, backfill, err := s.folderStore.GmailSyncCursor(acc.ID)
		if err == nil && backfill != "!done" {
			return elapsed >= time.Minute
		}
	}

	interval := time.Duration(acc.SyncInterval) * time.Minute

	return elapsed >= interval
}

// syncAccountInbox syncs the INBOX for an account
func (s *Scheduler) syncAccountInbox(parent context.Context, acc *account.Account) {
	// Prevent concurrent syncs for the same account
	s.syncingMu.Lock()
	if s.syncing[acc.ID] {
		s.syncingMu.Unlock()
		s.log.Debug().Str("account_id", acc.ID).Msg("Sync already in progress, skipping")
		return
	}
	s.syncing[acc.ID] = true
	done := make(chan struct{})
	s.syncDone[acc.ID] = done
	s.syncingMu.Unlock()

	// Create a cancellable context with timeout for this sync operation
	// 30 minute timeout prevents syncs from running forever if connection hangs
	ctx, cancel := context.WithTimeout(parent, 30*time.Minute)
	s.syncCancelMu.Lock()
	s.syncCancels[acc.ID] = cancel
	s.syncCancelMu.Unlock()

	defer func() {
		// Clean up cancel function
		cancel() // Always call cancel to release timeout resources
		s.syncCancelMu.Lock()
		delete(s.syncCancels, acc.ID)
		s.syncCancelMu.Unlock()

		s.syncingMu.Lock()
		delete(s.syncing, acc.ID)
		delete(s.syncDone, acc.ID)
		close(done)
		s.syncingMu.Unlock()
	}()

	// Hold the process out of App Nap for the duration of the sync itself.
	act := s.activities.BeginBackground("Syncing email")
	defer act.End()

	s.log.Info().Str("account_id", acc.ID).Msg("Starting scheduled sync for INBOX")
	// Account cadence is independent of a hidden inbox's freshness, including
	// Gmail history refresh and failed attempts. Never mark the inbox synced.
	defer func() {
		inbox, err := s.folderStore.GetByType(acc.ID, folder.TypeInbox)
		if err != nil || inbox == nil || !inbox.Hidden {
			return
		}
		s.syncingMu.Lock()
		defer s.syncingMu.Unlock()
		if s.hiddenInboxAttempt == nil {
			s.hiddenInboxAttempt = map[string]time.Time{}
		}
		s.hiddenInboxAttempt[acc.ID] = time.Now()
	}()

	// First, ensure folders are synced
	if err := s.engine.SyncFolders(ctx, acc.ID); err != nil {
		if ctx.Err() != nil {
			s.log.Info().Str("account_id", acc.ID).Msg("Sync cancelled during folder sync")
			return
		}
		s.log.Error().Err(err).Str("account_id", acc.ID).Msg("Failed to sync folders")
		return
	}

	if _, apiErr := s.engine.GmailClient(ctx, acc.ID); apiErr == nil {
		previousInbox, _ := s.folderStore.GetByType(acc.ID, folder.TypeInbox)

		touched, err := s.engine.SyncGmailAccount(ctx, acc.ID)
		if err == nil && previousInbox != nil && s.newMailCallback != nil {
			fresh, _ := s.folderStore.Get(previousInbox.ID)
			if fresh != nil && fresh.TotalCount > previousInbox.TotalCount {
				s.newMailCallback(NewMailInfo{AccountID: acc.ID, AccountName: acc.Name, FolderID: fresh.ID, Count: fresh.TotalCount - previousInbox.TotalCount})
			}
		}

		if s.syncCompletedCallback != nil {
			for _, id := range touched {
				s.syncCompletedCallback(acc.ID, id, err)
			}
		}
		if err != nil {
			s.log.Warn().Err(err).Msg("Gmail metadata/history refresh failed")
		}
		return
	} else if !errors.Is(apiErr, gmail.ErrNotCapable) {
		s.log.Warn().Err(apiErr).Msg("Gmail authorization required")
		return
	}

	// Get the INBOX folder
	inbox, err := s.folderStore.GetByType(acc.ID, folder.TypeInbox)
	if err != nil {
		s.log.Error().Err(err).Str("account_id", acc.ID).Msg("Failed to get INBOX folder")
		return
	}
	if inbox == nil {
		s.log.Warn().Str("account_id", acc.ID).Msg("INBOX folder not found")
		return
	}

	// A hidden INBOX is not refreshed: skip its message sync and only catch
	// up the account's remaining visible folders.
	if inbox.Hidden {
		s.log.Debug().Str("account_id", acc.ID).Msg("INBOX is hidden, skipping its scheduled refresh")
		s.syncAdditionalFolders(ctx, acc, inbox)
		return
	}

	// Get current message count before sync
	previousCount := inbox.TotalCount

	// Sync messages (use account's sync period setting)
	if err := s.engine.SyncMessages(ctx, acc.ID, inbox.ID, acc.SyncPeriodDays, false); err != nil {
		if ctx.Err() != nil {
			s.log.Info().Str("account_id", acc.ID).Msg("Sync cancelled during message sync")
			// Notify completion even on cancel so frontend clears progress
			if s.syncCompletedCallback != nil {
				s.syncCompletedCallback(acc.ID, inbox.ID, nil)
			}
			return
		}
		s.log.Error().Err(err).Str("account_id", acc.ID).Msg("Failed to sync messages")
		// Notify completion with error so frontend clears progress
		if s.syncCompletedCallback != nil {
			s.syncCompletedCallback(acc.ID, inbox.ID, err)
		}
		return
	}

	// Get updated folder info
	updatedInbox, err := s.folderStore.Get(inbox.ID)
	if err != nil {
		s.log.Error().Err(err).Str("account_id", acc.ID).Msg("Failed to get updated INBOX folder")
		// Notify completion with error
		if s.syncCompletedCallback != nil {
			s.syncCompletedCallback(acc.ID, inbox.ID, err)
		}
		return
	}

	// Check if there are new messages
	if updatedInbox != nil && updatedInbox.TotalCount > previousCount {
		newCount := updatedInbox.TotalCount - previousCount
		s.log.Info().
			Str("account_id", acc.ID).
			Int("newMessages", newCount).
			Msg("New messages arrived")

		// Notify about new mail
		if s.newMailCallback != nil {
			s.newMailCallback(NewMailInfo{
				AccountID:   acc.ID,
				AccountName: acc.Name,
				FolderID:    inbox.ID,
				Count:       newCount,
			})
		}
	}

	// Notify that sync completed for Inbox
	if s.syncCompletedCallback != nil && inbox != nil {
		s.syncCompletedCallback(acc.ID, inbox.ID, nil)
	}

	// Sync additional subscribed/all folders (beyond Inbox)
	s.syncAdditionalFolders(ctx, acc, inbox)

	s.log.Debug().Str("account_id", acc.ID).Msg("Scheduled sync completed")
}

// syncAdditionalFolders syncs subscribed or all folders beyond Inbox,
// based on the account's SyncAllFolders setting.
func (s *Scheduler) syncAdditionalFolders(ctx context.Context, acc *account.Account, inbox *folder.Folder) {
	folders, err := s.getAccountSyncFolders(acc)
	if err != nil {
		s.log.Warn().Err(err).Str("account_id", acc.ID).Msg("Failed to get sync folders")
		return
	}

	// Filter out Inbox (already synced) and limit to 2 concurrent syncs
	sem := make(chan struct{}, 2)
	var wg sync.WaitGroup

	for _, f := range folders {
		// Skip Inbox — already synced above with notification handling
		if inbox != nil && f.ID == inbox.ID {
			continue
		}

		// Check context
		if ctx.Err() != nil {
			break
		}

		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			wg.Wait()
			return
		}
		wg.Add(1)
		go func(ctx context.Context, f *folder.Folder) {
			defer wg.Done()
			defer func() { <-sem }()

			if syncErr := s.engine.SyncMessages(ctx, acc.ID, f.ID, acc.SyncPeriodDays, false); syncErr != nil {
				if ctx.Err() == nil {
					s.log.Warn().Err(syncErr).Str("folder", f.Path).Msg("Failed to sync additional folder")
				}
			}
			// Notify completion for this folder
			if s.syncCompletedCallback != nil {
				s.syncCompletedCallback(acc.ID, f.ID, nil)
			}
		}(ctx, f)
	}
	wg.Wait()
}

// getAccountSyncFolders returns the folders to sync for an account. Hidden
// folders are never refreshed by scheduled or bulk sync; showing a folder
// again (Show + refresh) catches it up explicitly.
func (s *Scheduler) getAccountSyncFolders(acc *account.Account) ([]*folder.Folder, error) {
	if acc.SyncAllFolders {
		folders, err := s.folderStore.List(acc.ID)
		return folder.WithoutHidden(folders), err
	}
	if acc.SyncFoldersEnabled {
		folders, err := s.folderStore.ListSubscribed(acc.ID)
		return folder.WithoutHidden(folders), err
	}
	folders, err := s.folderStore.List(acc.ID)
	return folder.WithoutHidden(folders), err
}

// TriggerSync manually triggers a sync for a specific account (non-blocking)
func (s *Scheduler) TriggerSync(accountID string) {
	acc, err := s.accountStore.Get(accountID)
	if err != nil {
		s.log.Error().Err(err).Str("accountID", accountID).Msg("Failed to get account for manual sync")
		return
	}

	s.startAccountSync(acc)
}

// CancelSync cancels any running sync for the specified account
func (s *Scheduler) CancelSync(accountID string) {
	s.syncCancelMu.Lock()
	if cancel, ok := s.syncCancels[accountID]; ok {
		s.log.Info().Str("accountID", accountID).Msg("Cancelling running sync")
		cancel()
	}
	s.syncCancelMu.Unlock()
}

// CancelSyncAndWait cancels an account sync and waits until its database work has stopped.
func (s *Scheduler) CancelSyncAndWait(ctx context.Context, accountID string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	s.CancelSync(accountID)
	for {
		s.syncingMu.Lock()
		done, syncing := s.syncDone[accountID], s.syncing[accountID]
		s.syncingMu.Unlock()
		if !syncing || done == nil {
			return nil
		}
		s.CancelSync(accountID)
		select {
		case <-done:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// TriggerSyncAll manually triggers a sync for all enabled accounts (non-blocking)
func (s *Scheduler) TriggerSyncAll() {
	accounts, err := s.accountStore.List()
	if err != nil {
		s.log.Error().Err(err).Msg("Failed to list accounts for manual sync")
		return
	}

	for _, acc := range accounts {
		if acc.Enabled {
			s.startAccountSync(acc)
		}
	}
}

// SyncAccountInboxBlocking syncs INBOX and returns new mail info (blocking)
// This is useful for IDLE-triggered syncs where we want to wait for completion
func (s *Scheduler) SyncAccountInboxBlocking(accountID string) (*NewMailInfo, error) {
	s.runningMu.Lock()
	if !s.running {
		s.runningMu.Unlock()
		return nil, context.Canceled
	}
	parent := s.ctx
	s.wg.Add(1)
	s.runningMu.Unlock()
	defer s.wg.Done()

	acc, err := s.accountStore.Get(accountID)
	if err != nil {
		return nil, err
	}

	// Prevent concurrent syncs for the same account
	s.syncingMu.Lock()
	if s.syncing[acc.ID] {
		s.syncingMu.Unlock()
		s.log.Debug().Str("account_id", acc.ID).Msg("Sync already in progress, skipping")
		return nil, nil
	}
	s.syncing[acc.ID] = true
	done := make(chan struct{})
	s.syncDone[acc.ID] = done
	s.syncingMu.Unlock()

	// Create a cancellable context with timeout for this sync operation
	// 30 minute timeout prevents syncs from running forever if connection hangs
	ctx, cancel := context.WithTimeout(parent, 30*time.Minute)
	s.syncCancelMu.Lock()
	s.syncCancels[acc.ID] = cancel
	s.syncCancelMu.Unlock()

	defer func() {
		// Clean up cancel function
		cancel() // Always call cancel to release timeout resources
		s.syncCancelMu.Lock()
		delete(s.syncCancels, acc.ID)
		s.syncCancelMu.Unlock()

		s.syncingMu.Lock()
		delete(s.syncing, acc.ID)
		delete(s.syncDone, acc.ID)
		close(done)
		s.syncingMu.Unlock()
	}()

	// Get the INBOX folder
	inbox, err := s.folderStore.GetByType(acc.ID, folder.TypeInbox)
	if err != nil {
		return nil, err
	}
	if inbox == nil {
		// Try syncing folders first
		if err := s.engine.SyncFolders(ctx, acc.ID); err != nil {
			if ctx.Err() != nil {
				s.log.Info().Str("account_id", acc.ID).Msg("Sync cancelled during folder sync")
				return nil, ctx.Err()
			}
			return nil, err
		}
		inbox, err = s.folderStore.GetByType(acc.ID, folder.TypeInbox)
		if err != nil || inbox == nil {
			return nil, err
		}
	}

	// Get current message count before sync
	previousCount := inbox.TotalCount

	// Sync messages (use account's sync period setting)
	// IDLE-triggered inbox sync (new mail + cross-client deletions) — use the
	// lightweight incremental flag path; the scheduled sync above stays the
	// authoritative full reconciliation.
	if err := s.engine.SyncMessages(ctx, acc.ID, inbox.ID, acc.SyncPeriodDays, true); err != nil {
		if ctx.Err() != nil {
			s.log.Info().Str("account_id", acc.ID).Msg("Sync cancelled during message sync")
			return nil, ctx.Err()
		}
		return nil, err
	}

	// Get updated folder info
	updatedInbox, err := s.folderStore.Get(inbox.ID)
	if err != nil {
		return nil, err
	}

	// Check if there are new messages
	if updatedInbox != nil && updatedInbox.TotalCount > previousCount {
		newCount := updatedInbox.TotalCount - previousCount
		return &NewMailInfo{
			AccountID:   acc.ID,
			AccountName: acc.Name,
			FolderID:    inbox.ID,
			Count:       newCount,
		}, nil
	}

	return nil, nil
}

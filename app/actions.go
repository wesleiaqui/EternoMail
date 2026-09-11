package app

import (
	"context"
	"errors"
	"fmt"
	stdlog "log"
	"sort"
	"time"

	goImap "github.com/emersion/go-imap/v2"
	"github.com/hkdb/aerion/internal/folder"
	"github.com/hkdb/aerion/internal/imap"
	"github.com/hkdb/aerion/internal/logging"
	"github.com/hkdb/aerion/internal/message"
	"github.com/hkdb/aerion/internal/undo"
	wailsRuntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// withIMAPRetry wraps an IMAP operation with stale-connection retry.
// If the operation fails with a connection error, the dead connection is discarded
// and the operation is retried once with a fresh connection.
func (a *App) withIMAPRetry(accountID string, op func(conn *imap.Client) error) error {
	log := logging.WithComponent("app.imapRetry")

	poolConn, err := a.imapPool.GetConnection(a.ctx, accountID)
	if err != nil {
		return fmt.Errorf("failed to get IMAP connection: %w", err)
	}

	err = op(poolConn.Client())
	if err == nil {
		a.imapPool.Release(poolConn)
		return nil
	}

	if !imap.IsConnectionError(err) {
		a.imapPool.Release(poolConn)
		return err
	}

	// Stale connection — discard and retry once with fresh connection
	log.Warn().Err(err).Str("account", accountID).Msg("IMAP connection error, retrying with fresh connection")
	a.imapPool.Discard(poolConn)

	poolConn, err = a.imapPool.GetConnection(a.ctx, accountID)
	if err != nil {
		return fmt.Errorf("failed to get IMAP connection on retry: %w", err)
	}
	defer a.imapPool.Release(poolConn)

	return op(poolConn.Client())
}

// ============================================================================
// Message Actions API - Exposed to frontend via Wails bindings
// ============================================================================

// UndoableActionResult identifies the undo operation registered by an action.
type UndoableActionResult struct {
	OperationID string `json:"operationId"`
	Coalesced   bool   `json:"coalesced"`
}

// TrashUndoResult preserves the legacy Trash outcome while identifying its undo operation.
type TrashUndoResult struct {
	MovedToTrash bool   `json:"movedToTrash"`
	OperationID  string `json:"operationId"`
	Coalesced    bool   `json:"coalesced"`
}

// SpamUndoResult preserves the legacy MarkAsSpam outcome while identifying its undo operation.
type SpamUndoResult struct {
	MovedToSpam bool   `json:"movedToSpam"`
	OperationID string `json:"operationId"`
	Coalesced   bool   `json:"coalesced"`
}

// MarkAsRead marks messages as read
func (a *App) MarkAsRead(messageIDs []string) error {
	return a.setReadStatus(messageIDs, true)
}

// MarkAllFolderMessagesAsRead marks all unread messages in a folder as read
func (a *App) MarkAllFolderMessagesAsRead(folderID string) error {
	ids, err := a.messageStore.GetUnreadMessageIDsByFolder(folderID)
	if err != nil {
		return fmt.Errorf("failed to get unread messages: %w", err)
	}
	if len(ids) == 0 {
		return nil
	}
	return a.MarkAsRead(ids)
}

// MarkAllFolderMessagesAsUnread marks all read messages in a folder as unread
func (a *App) MarkAllFolderMessagesAsUnread(folderID string) error {
	ids, err := a.messageStore.GetReadMessageIDsByFolder(folderID)
	if err != nil {
		return fmt.Errorf("failed to get read messages: %w", err)
	}
	if len(ids) == 0 {
		return nil
	}
	return a.MarkAsUnread(ids)
}

// MarkAsUnread marks messages as unread
func (a *App) MarkAsUnread(messageIDs []string) error {
	return a.setReadStatus(messageIDs, false)
}

func (a *App) setReadStatus(messageIDs []string, isRead bool) error {
	log := logging.WithComponent("app")

	if len(messageIDs) == 0 {
		return nil
	}

	// Get messages to find their UIDs and folders
	messages, err := a.messageStore.GetByIDs(messageIDs)
	if err != nil {
		return fmt.Errorf("failed to get messages: %w", err)
	}
	if len(messages) == 0 {
		return nil
	}

	// Group by folder for IMAP operations
	byFolder := make(map[string][]*message.Message)
	for _, m := range messages {
		byFolder[m.FolderID] = append(byFolder[m.FolderID], m)
	}

	// Update local DB first (local-first)
	isReadPtr := &isRead
	if err := a.messageStore.UpdateFlagsBatch(messageIDs, isReadPtr, nil); err != nil {
		return fmt.Errorf("failed to update local flags: %w", err)
	}

	// Emit event for UI update with flag state
	wailsRuntime.EventsEmit(a.ctx, "messages:readChanged", map[string]interface{}{
		"messageIds": messageIDs,
		"isRead":     isRead,
	})

	// Update folder unread counts in background to avoid blocking other DB operations
	go func() {
		defer recoverPanic("app.actions", "update folder counts")
		folderCounts := make(map[string]int)
		for folderID := range byFolder {
			unreadCount, err := a.messageStore.CountUnreadByFolder(folderID)
			if err != nil {
				log.Error().Err(err).Str("folderID", folderID).Msg("Failed to count unread messages")
				continue
			}
			folderObj, err := a.folderStore.Get(folderID)
			if err != nil || folderObj == nil {
				log.Error().Err(err).Str("folderID", folderID).Msg("Failed to get folder")
				continue
			}
			if err := a.folderStore.UpdateCounts(folderID, folderObj.TotalCount, unreadCount); err != nil {
				log.Error().Err(err).Str("folderID", folderID).Msg("Failed to update folder counts")
				continue
			}
			folderCounts[folderID] = unreadCount
		}
		if len(folderCounts) > 0 {
			wailsRuntime.EventsEmit(a.ctx, "folders:countsChanged", folderCounts)
		}
	}()

	// Sync to IMAP in background with retry
	go func() {
		defer recoverPanic("app.actions", "sync flags to IMAP")
		for folderID, msgs := range byFolder {
			var err error
			for attempt := 1; attempt <= 3; attempt++ {
				err = a.syncFlagsToIMAP(msgs, folderID, "read", isRead)
				if err == nil {
					break
				}
				log.Warn().Err(err).Int("attempt", attempt).Str("folderID", folderID).Msg("Failed to sync read flags to IMAP, retrying...")
				time.Sleep(time.Duration(attempt) * time.Second)
			}
			if err != nil {
				log.Error().Err(err).Str("folderID", folderID).Msg("Failed to sync read flags to IMAP after 3 attempts")
			}
		}
	}()

	// Read-flag changes are intentionally NOT pushed onto the undo stack.
	// The original design treated every mark-read/unread as undoable, but
	// in practice the auto-mark-as-read timer (and bulk operations) flooded
	// the stack so Cmd+Z routinely undid a background read flip instead of
	// the user's last real action (archive / move / trash). The cure is
	// simpler than per-call-site filtering: just don't make read changes
	// undoable at all — the user can re-flip manually if they need to.
	// Closes #243.

	return nil
}

// Star marks messages as starred
func (a *App) Star(messageIDs []string) error {
	return a.setStarredStatus(messageIDs, true)
}

// Unstar removes star from messages
func (a *App) Unstar(messageIDs []string) error {
	return a.setStarredStatus(messageIDs, false)
}

func (a *App) setStarredStatus(messageIDs []string, isStarred bool) error {
	log := logging.WithComponent("app")

	if len(messageIDs) == 0 {
		return nil
	}

	messages, err := a.messageStore.GetByIDs(messageIDs)
	if err != nil {
		return fmt.Errorf("failed to get messages: %w", err)
	}
	if len(messages) == 0 {
		return nil
	}

	byFolder := make(map[string][]*message.Message)
	for _, m := range messages {
		byFolder[m.FolderID] = append(byFolder[m.FolderID], m)
	}

	// Update local DB first
	isStarredPtr := &isStarred
	if err := a.messageStore.UpdateFlagsBatch(messageIDs, nil, isStarredPtr); err != nil {
		return fmt.Errorf("failed to update local flags: %w", err)
	}

	wailsRuntime.EventsEmit(a.ctx, "messages:starredChanged", map[string]interface{}{
		"messageIds": messageIDs,
		"isStarred":  isStarred,
	})

	// Sync to IMAP in background with retry
	go func() {
		defer recoverPanic("app.actions", "sync star flags to IMAP")
		for folderID, msgs := range byFolder {
			var err error
			for attempt := 1; attempt <= 3; attempt++ {
				err = a.syncFlagsToIMAP(msgs, folderID, "starred", isStarred)
				if err == nil {
					break
				}
				log.Warn().Err(err).Int("attempt", attempt).Str("folderID", folderID).Msg("Failed to sync starred flags to IMAP, retrying...")
				time.Sleep(time.Duration(attempt) * time.Second)
			}
			if err != nil {
				log.Error().Err(err).Str("folderID", folderID).Msg("Failed to sync starred flags to IMAP after 3 attempts")
			}
		}
	}()

	// Star changes are intentionally NOT pushed onto the undo stack. Same
	// rationale as setReadStatus: the cost of stack pollution outweighs
	// the value of undo for a flag flip that the user can trivially
	// reverse with another star/unstar click. Closes #243.

	return nil
}

// syncFlagsToIMAP syncs flag changes to IMAP server
func (a *App) syncFlagsToIMAP(messages []*message.Message, folderID, flagType string, flagValue bool) error {
	if len(messages) == 0 {
		return nil
	}

	folderObj, err := a.folderStore.Get(folderID)
	if err != nil || folderObj == nil {
		return fmt.Errorf("folder not found: %s", folderID)
	}

	uids := make([]goImap.UID, len(messages))
	for i, m := range messages {
		uids[i] = goImap.UID(m.UID)
	}

	var flag goImap.Flag
	switch flagType {
	case "read":
		flag = goImap.FlagSeen
	case "starred":
		flag = goImap.FlagFlagged
	}

	return a.withIMAPRetry(messages[0].AccountID, func(conn *imap.Client) error {
		if _, err := conn.SelectMailbox(a.ctx, folderObj.Path); err != nil {
			return fmt.Errorf("failed to select mailbox: %w", err)
		}

		// Stamp before the STORE so the server's IDLE echo of this change is
		// recognized as ours and doesn't trigger a self-inflicted re-sync.
		a.noteOwnFlagChange(messages[0].AccountID)
		if flagValue {
			return conn.AddMessageFlags(uids, []goImap.Flag{flag})
		}
		return conn.RemoveMessageFlags(uids, []goImap.Flag{flag})
	})
}

// MoveToFolder moves messages to a specified folder
func (a *App) MoveToFolder(messageIDs []string, destFolderID string) error {
	_, _, _, err := a.runMoveMutation(messageIDs, "move-to-folder", func() (bool, string, error) {
		operationID, moveErr := a.moveToFolder(messageIDs, destFolderID, true, "")
		return false, operationID, moveErr
	})
	return err
}

// MoveToFolderWithUndo moves messages and atomically returns its undo operation.
func (a *App) MoveToFolderWithUndo(messageIDs []string, destFolderID string) (UndoableActionResult, error) {
	_, operationID, coalesced, err := a.runMoveMutation(messageIDs, "move-to-folder", func() (bool, string, error) {
		op, moveErr := a.moveToFolder(messageIDs, destFolderID, true, "")
		return false, op, moveErr
	})
	return UndoableActionResult{OperationID: operationID, Coalesced: coalesced}, err
}

func (a *App) moveToFolder(messageIDs []string, destFolderID string, recordUndo bool, operationID string) (string, error) {
	log := logging.WithComponent("app")

	if len(messageIDs) == 0 {
		return operationID, nil
	}

	// Cross-account selections (Unified Inbox or any mixed-account multi-
	// select) route to a partition helper that recurses through this
	// same function with uniform single-account slices. The existing
	// messages[0].AccountID != destFolder.AccountID guard below then
	// fires exactly once per partition with a correct full-batch
	// classification.
	if spans, _ := a.messageStore.SpansMultipleAccounts(messageIDs); spans {
		return a.moveToFolderCrossAccount(messageIDs, destFolderID, recordUndo, operationID)
	}

	messages, err := a.messageStore.GetByIDs(messageIDs)
	if err != nil {
		return operationID, fmt.Errorf("failed to get messages: %w", err)
	}
	if len(messages) == 0 {
		return operationID, fmt.Errorf("requested messages are no longer available")
	}

	destFolder, err := a.folderStore.Get(destFolderID)
	if err != nil || destFolder == nil {
		return operationID, fmt.Errorf("destination folder not found: %s", destFolderID)
	}

	// A negative database UID is the placeholder written by MoveMessages while
	// an earlier remote move is unresolved. The guard normally makes callers
	// wait for that lease; seeing one without a lease is inconsistent state and
	// must fail before another optimistic move or undo token is created.
	for _, moved := range messages {
		if moved.UID == 0 || int32(moved.UID) < 0 {
			return operationID, fmt.Errorf("message %s has no stable server UID in source folder", moved.ID)
		}
	}

	// Cross-account move: APPEND raw bytes to destination first, then route source
	// cleanup through the existing Trash() pipeline (which handles local DB, IMAP
	// COPY+EXPUNGE within source, folder counts, events, undo, Gmail labels).
	// Strict ordering: APPEND completes synchronously before Trash runs — if
	// APPEND fails, source stays untouched.
	if messages[0].AccountID != destFolder.AccountID {
		if err := a.copyMessagesAcrossAccounts(messages, destFolder); err != nil {
			return operationID, fmt.Errorf("cross-account move: append failed: %w", err)
		}
		_, operationID, trashErr := a.trashWithUndo(messageIDs, operationID)
		// Sync destination so appended messages get correct UIDs locally.
		go func() {
			defer recoverPanic("app.actions", "cross-account move dest sync")
			_ = a.SyncFolder(destFolder.AccountID, destFolder.ID)
		}()
		return operationID, trashErr
	}

	// Group by source folder
	byFolder := make(map[string][]*message.Message)
	for _, m := range messages {
		byFolder[m.FolderID] = append(byFolder[m.FolderID], m)
	}
	for sourceFolderID := range byFolder {
		sourceFolder, sourceErr := a.folderStore.Get(sourceFolderID)
		if sourceErr != nil || sourceFolder == nil {
			return operationID, fmt.Errorf("source folder not found: %s", sourceFolderID)
		}
	}

	// Update local DB first
	if err := a.messageStore.MoveMessages(messageIDs, destFolderID); err != nil {
		return operationID, fmt.Errorf("failed to move messages locally: %w", err)
	}

	a.emitRuntimeEvent("messages:moved", map[string]interface{}{
		"messageIds":   messageIDs,
		"destFolderId": destFolderID,
	})

	// Update folder unread counts for source and destination folders
	folderCountsDone := make(chan struct{})
	go func() {
		defer close(folderCountsDone)
		defer recoverPanic("app.actions", "update folder counts after move")
		folderCounts := make(map[string]int)

		// Update source folders
		for folderID, msgs := range byFolder {
			unreadCount, err := a.messageStore.CountUnreadByFolder(folderID)
			if err != nil {
				log.Error().Err(err).Str("folderID", folderID).Msg("Failed to count unread messages")
				continue
			}
			folderObj, err := a.folderStore.Get(folderID)
			if err != nil || folderObj == nil {
				log.Error().Err(err).Str("folderID", folderID).Msg("Failed to get folder")
				continue
			}
			newTotalCount := folderObj.TotalCount - len(msgs)
			if newTotalCount < 0 {
				newTotalCount = 0
			}
			if err := a.folderStore.UpdateCounts(folderID, newTotalCount, unreadCount); err != nil {
				log.Error().Err(err).Str("folderID", folderID).Msg("Failed to update folder counts")
				continue
			}
			folderCounts[folderID] = unreadCount
		}

		// Update destination folder
		unreadCount, err := a.messageStore.CountUnreadByFolder(destFolderID)
		if err != nil {
			log.Error().Err(err).Str("folderID", destFolderID).Msg("Failed to count unread messages for destination")
		} else {
			destFolderObj, err := a.folderStore.Get(destFolderID)
			if err == nil && destFolderObj != nil {
				newTotalCount := destFolderObj.TotalCount + len(messageIDs)
				if err := a.folderStore.UpdateCounts(destFolderID, newTotalCount, unreadCount); err != nil {
					log.Error().Err(err).Str("folderID", destFolderID).Msg("Failed to update destination folder counts")
				} else {
					folderCounts[destFolderID] = unreadCount
				}
			}
		}

		if len(folderCounts) > 0 {
			a.emitRuntimeEvent("folders:countsChanged", folderCounts)
		}
	}()

	// The visible move is local-first: return to the frontend immediately.
	// Remote IMAP work and destination reconciliation stay in the background.
	// Undo receives the completion barrier below and waits only if the user
	// clicks Undo before reconciliation has finished.
	moveCompletion := undo.NewMoveCompletion()
	a.bindMoveMutationCompletion(messages, moveCompletion)

	if recordUndo {
		// Create undo commands before starting background reconciliation so every
		// diagnostic emitted by that goroutine can carry the operation ID.
		for sourceFolderID, msgs := range byFolder {
			identities := make([]undo.MoveMessageIdentity, 0, len(msgs))
			for _, m := range msgs {
				identities = append(identities, undo.MoveMessageIdentity{LocalID: m.ID, MessageID: m.MessageID})
			}

			cmd := undo.NewMoveCommand(
				a,
				msgs[0].AccountID,
				identities,
				sourceFolderID,
				destFolderID,
				fmt.Sprintf("Move to %s", destFolder.Name),
				moveCompletion,
			)
			operationID = a.undoStack.PushOperation(operationID, cmd, undo.OperationMetadata{
				Action:     "move:" + string(destFolder.Type),
				MessageIDs: messageIDs,
			})
			cmd.SetOperationID(operationID)
			cmd.SetTracedMove(func(messageIDs []string, destinationFolderID, tracedOperationID string) error {
				return a.runUndoMoveMutation(messageIDs, destinationFolderID, tracedOperationID)
			})
		}
	}
	moveCompletion.SetOperationID(operationID)

	go func() {
		defer func() {
			if r := recover(); r != nil {
				err := fmt.Errorf("panic during background move: %v", r)
				log.Error().Err(err).Msg("Background move panicked")
				stdlog.Printf("UNDO MOVE operationId=%s phase=reconcile-error destFolderId=%s error=%q", operationID, destFolderID, err)
				a.emitRuntimeEvent("folder:syncError", map[string]interface{}{
					"accountId": messages[0].AccountID,
					"folderId":  destFolderID,
					"error":     err.Error(),
				})
				moveCompletion.Complete(err)
			}
		}()

		resolvedUIDs := make(map[string]uint32, len(messages))
		uidResolutionComplete := true
		remoteStarted := false
		for sourceFolderID, msgs := range byFolder {
			copyUIDs, groupRemoteStarted, err := a.moveMessagesToIMAP(msgs, sourceFolderID, destFolder, operationID)
			if err != nil {
				if !remoteStarted && !groupRemoteStarted {
					<-folderCountsDone
					if compensateErr := a.compensateUnstartedMove(messages, destFolderID); compensateErr != nil {
						log.Error().Err(compensateErr).Str("operationID", operationID).Msg("Failed to compensate unstarted move")
					}
					if recordUndo {
						a.undoStack.DiscardOperation(operationID)
					}
				}
				log.Error().
					Err(err).
					Str("sourceFolderID", sourceFolderID).
					Str("destFolderID", destFolderID).
					Msg("Failed to move messages on IMAP")
				stdlog.Printf("UNDO MOVE operationId=%s phase=reconcile-error sourceFolderId=%s destFolderId=%s error=%q", operationID, sourceFolderID, destFolderID, err)
				a.emitRuntimeEvent("folder:syncError", map[string]interface{}{
					"accountId": messages[0].AccountID,
					"folderId":  destFolderID,
					"error":     err.Error(),
				})
				moveCompletion.Complete(err)
				return
			}
			remoteStarted = remoteStarted || groupRemoteStarted
			if len(copyUIDs) != len(msgs) {
				uidResolutionComplete = false
			}
			for messageID, uid := range copyUIDs {
				resolvedUIDs[messageID] = uid
			}
		}

		if len(messages) == 0 {
			moveCompletion.Complete(nil)
			return
		}

		// UIDPLUS gives us the destination UIDs atomically with COPY. Persisting
		// them makes an immediate Undo independent of the account's ordinary sync
		// window (an old search result may be well outside a 30-day window).
		if uidResolutionComplete {
			reconcileResults, err := a.reconcileMovedMessageUIDsWithRetry(destFolderID, resolvedUIDs)
			for _, result := range reconcileResults {
				if result.ExistingLocalID != "" {
					stdlog.Printf("UNDO MOVE operationId=%s phase=uid-owner movingLocalId=%s existingLocalId=%s destFolderId=%s destinationUID=%d messageIdMatch=%t resolution=%s", operationID, result.MovingLocalID, result.ExistingLocalID, destFolderID, result.DestinationUID, result.MessageIDMatch, result.Resolution)
				}
			}
			if err == nil {
				moveCompletion.Complete(nil)
				return
			}
			var identityConflict *message.MovedUIDConflictError
			if errors.As(err, &identityConflict) {
				stdlog.Printf("UNDO MOVE operationId=%s phase=uid-owner movingLocalId=%s existingLocalId=%s destFolderId=%s destinationUID=%d messageIdMatch=%t resolution=conflict", operationID, identityConflict.MovingLocalID, identityConflict.ExistingLocalID, destFolderID, identityConflict.DestinationUID, identityConflict.MessageIDMatch)
				stdlog.Printf("UNDO MOVE operationId=%s phase=reconcile-error destFolderId=%s uidResolution=true fallbackSync=false error=%q", operationID, destFolderID, err)
				a.emitRuntimeEvent("folder:syncError", map[string]interface{}{
					"accountId": messages[0].AccountID,
					"folderId":  destFolderID,
					"error":     err.Error(),
				})
				moveCompletion.Complete(err)
				return
			}
			log.Warn().Err(err).Str("destFolderID", destFolderID).
				Msg("COPYUID reconciliation failed; falling back to destination sync")
			stdlog.Printf("UNDO MOVE operationId=%s phase=reconcile-fallback destFolderId=%s uidResolution=true fallbackSync=true error=%q", operationID, destFolderID, err)
		}
		if !uidResolutionComplete {
			stdlog.Printf("UNDO MOVE operationId=%s phase=reconcile-fallback destFolderId=%s uidResolution=false fallbackSync=true resolved=%d expected=%d", operationID, destFolderID, len(resolvedUIDs), len(messages))
		}

		// Multiple fast moves to the same destination share one delayed
		// destination sync instead of cancelling/restarting it for every click.
		a.scheduleMoveDestinationSync(
			messages[0].AccountID,
			destFolderID,
			moveCompletion,
		)
	}()

	return operationID, nil
}

func (a *App) reconcileMovedMessageUIDsWithRetry(destFolderID string, resolvedUIDs map[string]uint32) ([]message.MovedUIDReconcileResult, error) {
	const maxAttempts = 3
	var err error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		var results []message.MovedUIDReconcileResult
		results, err = a.messageStore.ReconcileMovedMessageUIDs(destFolderID, resolvedUIDs)
		if err == nil || !isSQLiteBusy(err) {
			return results, err
		}
		if attempt+1 == maxAttempts {
			break
		}
		stdlog.Printf("UNDO MOVE phase=reconcile-busy-retry destFolderId=%s attempt=%d maxAttempts=%d error=%q", destFolderID, attempt+1, maxAttempts, err)
		time.Sleep(time.Duration(attempt+1) * 25 * time.Millisecond)
	}
	return nil, err
}

const moveDestinationSyncDelay = 750 * time.Millisecond

// scheduleMoveDestinationSync coalesces a burst of moves to the same folder.
// Example: five Done clicks in quick succession still update the UI five times
// locally, but produce only one expensive destination-folder reconciliation.
func (a *App) scheduleMoveDestinationSync(
	accountID string,
	destFolderID string,
	completion *undo.MoveCompletion,
) {
	key := accountID + ":" + destFolderID

	a.moveSyncMu.Lock()

	if a.moveSyncTimers == nil {
		a.moveSyncTimers = make(map[string]*time.Timer)
	}
	if a.moveSyncWaiters == nil {
		a.moveSyncWaiters = make(map[string][]*undo.MoveCompletion)
	}

	a.moveSyncWaiters[key] = append(a.moveSyncWaiters[key], completion)

	if timer, ok := a.moveSyncTimers[key]; ok {
		timer.Stop()
	}

	a.moveSyncTimers[key] = time.AfterFunc(moveDestinationSyncDelay, func() {
		a.flushMoveDestinationSync(key, accountID, destFolderID)
	})

	a.moveSyncMu.Unlock()
}

func (a *App) flushMoveDestinationSync(key, accountID, destFolderID string) {
	// Never let two move-triggered destination reconciliations cancel each
	// other. A later burst waits here rather than restarting the active sync.
	a.moveSyncRunMu.Lock()
	defer a.moveSyncRunMu.Unlock()

	a.moveSyncMu.Lock()

	waiters := append(
		[]*undo.MoveCompletion(nil),
		a.moveSyncWaiters[key]...,
	)

	delete(a.moveSyncWaiters, key)
	delete(a.moveSyncTimers, key)

	a.moveSyncMu.Unlock()

	var syncErr error
	// Move reconciliation must not be discarded by SyncFolder's normal
	// 500ms debounce. It runs only once after the burst has settled.
	for attempt := 0; attempt < 3; attempt++ {
		a.syncMu.Lock()
		delete(a.syncLastRequest, key)
		a.syncMu.Unlock()

		syncErr = a.SyncFolder(accountID, destFolderID)
		if syncErr != context.Canceled {
			break
		}

		time.Sleep(200 * time.Millisecond)
	}

	if syncErr == nil {
		// Header synchronization has replaced the temporary negative UIDs with
		// the actual server UIDs, so Undo can now safely reverse the move.
		if err := a.messageStore.DeleteTempUIDs(destFolderID); err != nil {
			syncErr = err
		}
	}

	for _, waiter := range waiters {
		waiter.Complete(syncErr)
		if syncErr != nil {
			stdlog.Printf("UNDO MOVE operationId=%s phase=fallback-sync-error destFolderId=%s error=%q", waiter.OperationID(), destFolderID, syncErr)
		}
	}

	if syncErr != nil {
		reconcileLog := logging.WithComponent("app.moveReconcile")
		reconcileLog.Error().
			Err(syncErr).
			Str("accountID", accountID).
			Str("destFolderID", destFolderID).
			Msg("Failed to reconcile moved messages")
		a.emitRuntimeEvent("folder:syncError", map[string]interface{}{
			"accountId": accountID,
			"folderId":  destFolderID,
			"error":     syncErr.Error(),
		})
	}
}

// isGmailAccount checks if the account uses Gmail's IMAP server.
// Gmail uses labels instead of folders — IMAP COPY adds a label rather than
// creating an independent copy, and adding the Trash/Spam label hides the
// message from all other IMAP mailbox views.
func (a *App) isGmailAccount(accountID string) bool {
	acc, err := a.accountStore.Get(accountID)
	if err != nil || acc == nil {
		return false
	}
	return acc.IMAPHost == "imap.gmail.com"
}

func (a *App) compensateUnstartedMove(messages []*message.Message, destFolderID string) error {
	states := make([]message.MoveSourceState, 0, len(messages))
	affectedFolders := map[string]bool{destFolderID: true}
	messageIDsByFolder := make(map[string][]string)
	for _, moved := range messages {
		states = append(states, message.MoveSourceState{ID: moved.ID, FolderID: moved.FolderID, UID: moved.UID})
		affectedFolders[moved.FolderID] = true
		messageIDsByFolder[moved.FolderID] = append(messageIDsByFolder[moved.FolderID], moved.ID)
	}
	if err := a.messageStore.RestoreUnstartedMove(destFolderID, states); err != nil {
		return err
	}
	for folderID, ids := range messageIDsByFolder {
		a.emitRuntimeEvent("messages:moved", map[string]interface{}{
			"messageIds":   ids,
			"destFolderId": folderID,
		})
	}
	folderCounts := make(map[string]int, len(affectedFolders))
	for folderID := range affectedFolders {
		total, totalErr := a.messageStore.CountByFolder(folderID)
		unread, unreadErr := a.messageStore.CountUnreadByFolder(folderID)
		if totalErr != nil || unreadErr != nil {
			continue
		}
		if err := a.folderStore.UpdateCounts(folderID, total, unread); err == nil {
			folderCounts[folderID] = unread
		}
	}
	if len(folderCounts) > 0 {
		a.emitRuntimeEvent("folders:countsChanged", folderCounts)
	}
	return nil
}

func (a *App) moveMessagesToIMAP(messages []*message.Message, sourceFolderID string, destFolder *folder.Folder, operationID string) (map[string]uint32, bool, error) {
	log := logging.WithComponent("app.moveMessagesToIMAP")

	if len(messages) == 0 {
		return map[string]uint32{}, false, nil
	}

	sourceFolder, err := a.folderStore.Get(sourceFolderID)
	if err != nil || sourceFolder == nil {
		return nil, false, fmt.Errorf("source folder not found")
	}
	orderedMessages := append([]*message.Message(nil), messages...)
	sort.Slice(orderedMessages, func(i, j int) bool { return orderedMessages[i].UID < orderedMessages[j].UID })

	// Collect UIDs for logging
	uidList := make([]uint32, len(orderedMessages))
	for i, m := range orderedMessages {
		if m.UID == 0 || int32(m.UID) < 0 {
			return nil, false, fmt.Errorf("message %s has no server UID in source folder", m.ID)
		}
		uidList[i] = m.UID
	}

	log.Info().
		Str("sourceFolder", sourceFolder.Path).
		Str("destFolder", destFolder.Path).
		Uints32("uids", uidList).
		Int("count", len(messages)).
		Msg("Starting IMAP move operation")

	accountID := orderedMessages[0].AccountID

	// Trash and Spam are real destination mailboxes even on Gmail. Removing
	// only the current label leaves the message in All Mail, which makes a
	// delete from Inbox behave like Archive. Always COPY to the requested
	// destination and DELETE from the current mailbox.
	moveUIDs := make([]goImap.UID, 0, len(orderedMessages))
	for _, m := range orderedMessages {
		moveUIDs = append(moveUIDs, goImap.UID(m.UID))
	}
	if len(moveUIDs) == 0 {
		return map[string]uint32{}, false, nil
	}
	resolved := make(map[string]uint32, len(orderedMessages))
	copyUIDUsed := false
	messageIDFallbackUsed := false
	destinationUIDs := make([]uint32, 0, len(orderedMessages))
	remoteStarted := false

	// Mark the IDLE echo-suppression window at start AND completion so the
	// EXISTS/EXPUNGE echoes of this op defer the inbox reconcile instead of
	// racing our own in-flight EXPUNGE (see ownExpungeEchoSuppress).
	a.noteOwnExpunge(accountID)
	defer a.noteOwnExpunge(accountID)

	err = a.withIMAPRetry(accountID, func(conn *imap.Client) error {
		// Without UIDPLUS, take the Message-ID/UID baseline from the server
		// before COPY. The local cache may be stale and RFC822 Message-ID is not
		// unique, so it cannot safely stand in for this remote snapshot.
		preCopyUIDs := make(map[string]map[uint32]bool)
		if !conn.Caps().Has(goImap.CapUIDPlus) {
			if _, err := conn.SelectMailbox(a.ctx, destFolder.Path); err != nil {
				return fmt.Errorf("failed to select destination mailbox for pre-COPY UID snapshot: %w", err)
			}
			for _, moved := range orderedMessages {
				if moved.MessageID == "" {
					continue
				}
				if _, exists := preCopyUIDs[moved.MessageID]; exists {
					continue
				}
				searchCmd := conn.RawClient().UIDSearch(&goImap.SearchCriteria{
					Header:  []goImap.SearchCriteriaHeaderField{{Key: "MESSAGE-ID", Value: moved.MessageID}},
					NotFlag: []goImap.Flag{goImap.FlagDeleted},
				}, nil)
				searchData, searchErr := searchCmd.Wait()
				if searchErr != nil {
					return fmt.Errorf("failed to snapshot destination UIDs before COPY: %w", searchErr)
				}
				baseline := make(map[uint32]bool)
				for _, uid := range searchData.AllUIDs() {
					baseline[uint32(uid)] = true
				}
				preCopyUIDs[moved.MessageID] = baseline
			}
		}

		// Select source mailbox
		log.Debug().Str("mailbox", sourceFolder.Path).Msg("Selecting source mailbox")
		if _, err := conn.SelectMailbox(a.ctx, sourceFolder.Path); err != nil {
			return fmt.Errorf("failed to select source mailbox: %w", err)
		}

		// COPY every selected message to the destination mailbox.
		if len(moveUIDs) > 0 {
			log.Debug().Str("destMailbox", destFolder.Path).Int("count", len(moveUIDs)).Msg("Copying messages to destination")
			remoteStarted = true
			destUIDs, copyErr := conn.CopyMessages(moveUIDs, destFolder.Path)
			if copyErr != nil {
				return fmt.Errorf("failed to copy messages: %w", copyErr)
			}
			if len(destUIDs) == len(orderedMessages) {
				copyUIDUsed = true
				for index, uid := range destUIDs {
					resolved[orderedMessages[index].ID] = uint32(uid)
					destinationUIDs = append(destinationUIDs, uint32(uid))
				}
			}
			log.Debug().Msg("Messages copied successfully")
		}

		// DELETE the source copies after the destination COPY succeeds.
		log.Debug().Int("count", len(moveUIDs)).Msg("Deleting messages from source (marking deleted + expunge)")
		if err := conn.DeleteMessagesByUID(moveUIDs); err != nil {
			return fmt.Errorf("failed to delete messages from source: %w", err)
		}

		// Servers without UIDPLUS do not return COPYUID. Resolve only the copied
		// messages by subtracting pre-existing destination UIDs for each RFC822
		// Message-ID. Duplicate Message-IDs are mapped as one ordered group; an
		// ambiguous cardinality is left unresolved for the safe sync fallback.
		if len(resolved) != len(orderedMessages) {
			messageIDFallbackUsed = true
			if _, err := conn.SelectMailbox(a.ctx, destFolder.Path); err != nil {
				return fmt.Errorf("failed to select destination mailbox for UID lookup: %w", err)
			}
			groups := make(map[string][]*message.Message)
			for _, moved := range orderedMessages {
				if _, alreadyResolved := resolved[moved.ID]; alreadyResolved || moved.MessageID == "" {
					continue
				}
				groups[moved.MessageID] = append(groups[moved.MessageID], moved)
			}
			for rfc822MessageID, movedGroup := range groups {
				baseline, hasBaseline := preCopyUIDs[rfc822MessageID]
				if !hasBaseline {
					stdlog.Printf("UNDO MOVE operationId=%s phase=uid-fallback-ambiguous sourceFolderId=%s destFolderId=%s groupExpected=%d candidates=unknown reason=baseline-unavailable", operationID, sourceFolderID, destFolder.ID, len(movedGroup))
					continue
				}
				searchCmd := conn.RawClient().UIDSearch(&goImap.SearchCriteria{
					Header:  []goImap.SearchCriteriaHeaderField{{Key: "MESSAGE-ID", Value: rfc822MessageID}},
					NotFlag: []goImap.Flag{goImap.FlagDeleted},
				}, nil)
				searchData, err := searchCmd.Wait()
				if err != nil {
					return fmt.Errorf("failed to resolve copied message UID: %w", err)
				}
				matching := searchData.AllUIDs()
				candidates := make([]uint32, 0, len(matching))
				for _, uid := range matching {
					if !baseline[uint32(uid)] {
						candidates = append(candidates, uint32(uid))
					}
				}
				sort.Slice(candidates, func(i, j int) bool { return candidates[i] < candidates[j] })
				if len(candidates) != len(movedGroup) {
					stdlog.Printf("UNDO MOVE operationId=%s phase=uid-fallback-ambiguous sourceFolderId=%s destFolderId=%s groupExpected=%d candidates=%d", operationID, sourceFolderID, destFolder.ID, len(movedGroup), len(candidates))
					continue
				}
				for index, moved := range movedGroup {
					resolved[moved.ID] = candidates[index]
					destinationUIDs = append(destinationUIDs, candidates[index])
				}
			}
		}

		return nil
	})

	if err != nil {
		log.Error().Err(err).Msg("IMAP move operation failed")
		stdlog.Printf("UNDO MOVE operationId=%s phase=remote-error sourceFolderId=%s destFolderId=%s sourceUIDs=%v destUIDs=%v copyUID=%t fallbackMessageId=%t error=%q", operationID, sourceFolderID, destFolder.ID, uidList, destinationUIDs, copyUIDUsed, messageIDFallbackUsed, err)
		return nil, remoteStarted, err
	}

	log.Info().
		Str("sourceFolder", sourceFolder.Path).
		Str("destFolder", destFolder.Path).
		Int("count", len(messages)).
		Msg("IMAP move operation completed successfully")
	return resolved, remoteStarted, nil
}

// CopyToFolder copies messages to a specified folder (keeps original)
// Unlike MoveToFolder, this only copies - original messages remain in place
func (a *App) CopyToFolder(messageIDs []string, destFolderID string) error {
	log := logging.WithComponent("app")

	if len(messageIDs) == 0 {
		return nil
	}

	// Cross-account selections route to a partition helper that recurses
	// through this same function with uniform single-account slices. See
	// MoveToFolder for the rationale — same shape applied to Copy (which
	// keeps originals in place rather than deleting them).
	if spans, _ := a.messageStore.SpansMultipleAccounts(messageIDs); spans {
		return a.copyToFolderCrossAccount(messageIDs, destFolderID)
	}

	messages, err := a.messageStore.GetByIDs(messageIDs)
	if err != nil {
		return fmt.Errorf("failed to get messages: %w", err)
	}
	if len(messages) == 0 {
		return nil
	}

	destFolder, err := a.folderStore.Get(destFolderID)
	if err != nil || destFolder == nil {
		return fmt.Errorf("destination folder not found: %s", destFolderID)
	}

	// Cross-account copy: APPEND raw bytes to destination. Fire-and-forget to
	// match intra-account CopyToFolder's existing async behavior.
	if messages[0].AccountID != destFolder.AccountID {
		go func() {
			defer recoverPanic("app.actions", "cross-account copy")
			if err := a.copyMessagesAcrossAccounts(messages, destFolder); err != nil {
				log.Error().Err(err).
					Str("sourceAccountID", messages[0].AccountID).
					Str("destAccountID", destFolder.AccountID).
					Str("destFolderID", destFolder.ID).
					Msg("Cross-account copy failed")
				return
			}
			// Dest folder refresh + sidebar count bump ride on folder:synced from SyncFolder.
			_ = a.SyncFolder(destFolder.AccountID, destFolder.ID)
		}()
		return nil
	}

	// Group by source folder
	byFolder := make(map[string][]*message.Message)
	for _, m := range messages {
		byFolder[m.FolderID] = append(byFolder[m.FolderID], m)
	}

	// Copy on IMAP (no local DB change - messages stay in source folder)
	go func() {
		defer recoverPanic("app.actions", "copy messages on IMAP")
		for sourceFolderID, msgs := range byFolder {
			if err := a.copyMessagesToIMAP(msgs, sourceFolderID, destFolder); err != nil {
				log.Error().Err(err).
					Str("sourceFolderID", sourceFolderID).
					Str("destFolderID", destFolderID).
					Msg("Failed to copy messages on IMAP")
			}
		}

		// Sync destination folder so copied messages appear (headers + bodies)
		// Clear debounce so this request isn't silently dropped
		if len(messages) > 0 {
			accountID := messages[0].AccountID
			syncKey := accountID + ":" + destFolderID
			a.syncMu.Lock()
			delete(a.syncLastRequest, syncKey)
			a.syncMu.Unlock()

			if err := a.SyncFolder(accountID, destFolderID); err != nil && err != context.Canceled {
				log.Warn().Err(err).Str("destFolderID", destFolderID).Msg("Failed to sync destination folder after copy")
			}
		}
		// Dest folder refresh + sidebar count bump ride on folder:synced from SyncFolder above.
	}()

	return nil
}

func (a *App) copyMessagesToIMAP(messages []*message.Message, sourceFolderID string, destFolder *folder.Folder) error {
	if len(messages) == 0 {
		return nil
	}

	sourceFolder, err := a.folderStore.Get(sourceFolderID)
	if err != nil || sourceFolder == nil {
		return fmt.Errorf("source folder not found")
	}

	uids := make([]goImap.UID, len(messages))
	for i, m := range messages {
		uids[i] = goImap.UID(m.UID)
	}

	return a.withIMAPRetry(messages[0].AccountID, func(conn *imap.Client) error {
		if _, err := conn.SelectMailbox(a.ctx, sourceFolder.Path); err != nil {
			return fmt.Errorf("failed to select source mailbox: %w", err)
		}

		// COPY to destination (no DELETE - messages stay in source)
		if _, err := conn.CopyMessages(uids, destFolder.Path); err != nil {
			return fmt.Errorf("failed to copy messages: %w", err)
		}

		return nil
	})
}

// flagsForAppend maps a local message's flag fields to the IMAP flag slice
// used by AppendMessage on cross-account copy/move.
func flagsForAppend(m *message.Message) []goImap.Flag {
	flags := []goImap.Flag{}
	if m.IsRead {
		flags = append(flags, goImap.FlagSeen)
	}
	if m.IsStarred {
		flags = append(flags, goImap.FlagFlagged)
	}
	if m.IsAnswered {
		flags = append(flags, goImap.FlagAnswered)
	}
	if m.IsDraft {
		flags = append(flags, goImap.FlagDraft)
	}
	return flags
}

// copyMessagesAcrossAccounts fetches each message's raw RFC822 bytes from the
// source account's IMAP and APPENDs them to the destination account's IMAP.
// Used by the cross-account branches of MoveToFolder and CopyToFolder.
//
// Synchronous: returns when every APPEND has succeeded. Caller is expected to
// short-circuit on error so the source side is never touched after a partial
// success/failure.
func (a *App) copyMessagesAcrossAccounts(messages []*message.Message, destFolder *folder.Folder) error {
	log := logging.WithComponent("app.copyMessagesAcrossAccounts")

	if len(messages) == 0 {
		return nil
	}

	log.Info().
		Str("sourceAccountID", messages[0].AccountID).
		Str("destAccountID", destFolder.AccountID).
		Str("destFolder", destFolder.Path).
		Int("count", len(messages)).
		Msg("Starting cross-account append")

	return a.withIMAPRetry(destFolder.AccountID, func(conn *imap.Client) error {
		if _, err := conn.SelectMailbox(a.ctx, destFolder.Path); err != nil {
			return fmt.Errorf("failed to select destination mailbox: %w", err)
		}
		for _, m := range messages {
			raw, err := a.syncEngine.FetchRawMessage(a.ctx, m.AccountID, m.FolderID, m.UID)
			if err != nil {
				return fmt.Errorf("failed to fetch raw message from source: %w", err)
			}
			if _, err := conn.AppendMessage(destFolder.Path, flagsForAppend(m), m.Date, raw); err != nil {
				return fmt.Errorf("failed to append to destination: %w", err)
			}
		}
		log.Info().
			Str("destFolder", destFolder.Path).
			Int("count", len(messages)).
			Msg("Cross-account append completed")
		return nil
	})
}

// Archive moves messages to Archive, or All Mail for Gmail.
func (a *App) Archive(messageIDs []string) error {
	_, _, _, err := a.runMoveMutation(messageIDs, "archive", func() (bool, string, error) {
		operationID, archiveErr := a.archiveWithUndo(messageIDs, "")
		return false, operationID, archiveErr
	})
	return err
}

// ArchiveWithUndo archives messages and atomically returns its undo operation.
func (a *App) ArchiveWithUndo(messageIDs []string) (UndoableActionResult, error) {
	_, operationID, coalesced, err := a.runMoveMutation(messageIDs, "archive", func() (bool, string, error) {
		op, archiveErr := a.archiveWithUndo(messageIDs, "")
		return false, op, archiveErr
	})
	return UndoableActionResult{OperationID: operationID, Coalesced: coalesced}, err
}

func (a *App) archiveWithUndo(messageIDs []string, operationID string) (string, error) {
	if len(messageIDs) == 0 {
		return operationID, nil
	}

	// Cross-account selections (Unified Inbox) route to a partition
	// helper that recurses through this same function with uniform
	// single-account slices. SpansMultipleAccounts errors fall through
	// to the fast path so a metadata hiccup never breaks what works.
	if spans, _ := a.messageStore.SpansMultipleAccounts(messageIDs); spans {
		return a.archiveCrossAccount(messageIDs, operationID)
	}

	return a.removeFromInboxWithUndo(messageIDs, operationID)
}

// RemoveFromInbox removes the Inbox label from messages. Gmail represents
// this as moving the message to its All Mail label, rather than requiring an
// Archive folder to be configured.
func (a *App) RemoveFromInbox(messageIDs []string) error {
	_, _, _, err := a.runMoveMutation(messageIDs, "remove-from-inbox", func() (bool, string, error) {
		operationID, removeErr := a.removeFromInboxWithUndo(messageIDs, "")
		return false, operationID, removeErr
	})
	return err
}

// RemoveFromInboxWithUndo removes Inbox and atomically returns its undo operation.
func (a *App) RemoveFromInboxWithUndo(messageIDs []string) (UndoableActionResult, error) {
	_, operationID, coalesced, err := a.runMoveMutation(messageIDs, "remove-from-inbox", func() (bool, string, error) {
		op, removeErr := a.removeFromInboxWithUndo(messageIDs, "")
		return false, op, removeErr
	})
	return UndoableActionResult{OperationID: operationID, Coalesced: coalesced}, err
}

func (a *App) removeFromInboxWithUndo(messageIDs []string, operationID string) (string, error) {
	if len(messageIDs) == 0 {
		return operationID, nil
	}

	messages, err := a.messageStore.GetByIDs(messageIDs[:1])
	if err != nil || len(messages) == 0 {
		return operationID, fmt.Errorf("failed to get message")
	}

	accountID := messages[0].AccountID
	destinationType := folder.TypeArchive
	if a.isGmailAccount(accountID) {
		destinationType = folder.TypeAll
	}

	destination, err := a.GetSpecialFolder(accountID, destinationType)
	if err != nil {
		return operationID, fmt.Errorf("failed to get destination folder: %w", err)
	}
	if destination == nil {
		return operationID, fmt.Errorf("no destination folder configured")
	}

	return a.moveToFolder(messageIDs, destination.ID, true, operationID)
}

// Trash moves messages to the Trash folder.
// Returns true if at least one message was moved to trash (show undo toast).
// Returns false if all messages were just label-removed on Gmail (no undo).
func (a *App) Trash(messageIDs []string) (bool, error) {
	moved, _, _, err := a.runMoveMutation(messageIDs, "trash", func() (bool, string, error) {
		return a.trashWithUndo(messageIDs, "")
	})
	return moved, err
}

// TrashWithUndo moves messages to Trash and atomically returns its undo operation.
func (a *App) TrashWithUndo(messageIDs []string) (TrashUndoResult, error) {
	moved, operationID, coalesced, err := a.runMoveMutation(messageIDs, "trash", func() (bool, string, error) {
		return a.trashWithUndo(messageIDs, "")
	})
	return TrashUndoResult{MovedToTrash: moved, OperationID: operationID, Coalesced: coalesced}, err
}

func (a *App) trashWithUndo(messageIDs []string, operationID string) (bool, string, error) {
	if len(messageIDs) == 0 {
		return false, operationID, nil
	}

	// Cross-account selections (Unified Inbox) route to a partition
	// helper that recurses through this same function with uniform
	// single-account slices. SpansMultipleAccounts errors fall through
	// to the fast path so a metadata hiccup never breaks what works.
	if spans, _ := a.messageStore.SpansMultipleAccounts(messageIDs); spans {
		return a.trashCrossAccount(messageIDs, operationID)
	}

	messages, err := a.messageStore.GetByIDs(messageIDs[:1])
	if err != nil || len(messages) == 0 {
		return false, operationID, fmt.Errorf("failed to get message")
	}

	accountID := messages[0].AccountID

	trashFolder, err := a.GetSpecialFolder(accountID, folder.TypeTrash)
	if err != nil {
		return false, operationID, fmt.Errorf("failed to get trash folder: %w", err)
	}
	if trashFolder == nil {
		return false, operationID, fmt.Errorf("no trash folder configured")
	}

	// Non-Gmail: normal move to trash for all messages
	if !a.isGmailAccount(accountID) {
		operationID, err = a.moveToFolder(messageIDs, trashFolder.ID, true, operationID)
		return true, operationID, err
	}

	// Gmail: partition messages into copies (label-remove) vs sole copies (move to trash)
	return a.gmailTrashOrSpam(messageIDs, trashFolder, operationID)
}

// gmailTrashOrSpam performs a real move to Gmail's Trash/Spam mailbox.
// Merely deleting the current IMAP label would leave the message in All Mail,
// which is archive semantics rather than delete/spam semantics.
func (a *App) gmailTrashOrSpam(messageIDs []string, destFolder *folder.Folder, operationID string) (bool, string, error) {
	if len(messageIDs) == 0 {
		return false, operationID, nil
	}

	operationID, err := a.moveToFolder(messageIDs, destFolder.ID, true, operationID)
	if err != nil {
		return false, operationID, err
	}

	return true, operationID, nil
}

// gmailRemoveLabel removes messages from their current folder (label) on Gmail.
// This deletes them locally and does IMAP DELETE from source without COPY —
// effectively removing the label while the message stays in other labels.
func (a *App) gmailRemoveLabel(messages []*message.Message) error {
	log := logging.WithComponent("app.gmailRemoveLabel")

	if len(messages) == 0 {
		return nil
	}

	// Group by source folder
	byFolder := make(map[string][]*message.Message)
	for _, m := range messages {
		byFolder[m.FolderID] = append(byFolder[m.FolderID], m)
	}

	// Collect all IDs for local delete
	ids := make([]string, len(messages))
	for i, m := range messages {
		ids[i] = m.ID
	}

	// Delete from local DB
	if err := a.messageStore.DeleteBatch(ids); err != nil {
		return fmt.Errorf("failed to delete messages locally: %w", err)
	}

	wailsRuntime.EventsEmit(a.ctx, "messages:deleted", ids)

	// Update folder counts
	go func() {
		defer recoverPanic("app.actions", "update folder counts after label removal")
		folderCounts := make(map[string]int)
		for folderID, msgs := range byFolder {
			unreadCount, countErr := a.messageStore.CountUnreadByFolder(folderID)
			if countErr != nil {
				log.Error().Err(countErr).Str("folderID", folderID).Msg("Failed to count unread messages")
				continue
			}
			folderObj, getErr := a.folderStore.Get(folderID)
			if getErr != nil || folderObj == nil {
				continue
			}
			newTotalCount := folderObj.TotalCount - len(msgs)
			if newTotalCount < 0 {
				newTotalCount = 0
			}
			if updateErr := a.folderStore.UpdateCounts(folderID, newTotalCount, unreadCount); updateErr != nil {
				log.Error().Err(updateErr).Str("folderID", folderID).Msg("Failed to update folder counts")
				continue
			}
			folderCounts[folderID] = unreadCount
		}
		if len(folderCounts) > 0 {
			wailsRuntime.EventsEmit(a.ctx, "folders:countsChanged", folderCounts)
		}
	}()

	// IMAP: DELETE from source folders (no COPY — just remove the label)
	go func() {
		defer recoverPanic("app.actions", "remove Gmail label on IMAP")
		for folderID, msgs := range byFolder {
			if err := a.removeFromIMAPFolder(msgs, folderID); err != nil {
				log.Error().Err(err).Str("folderID", folderID).Msg("Failed to remove messages from IMAP folder")
			}
		}
	}()

	return nil
}

// removeFromIMAPFolder does SELECT + DELETE by UID on the given folder.
// Unlike deleteMessagesFromIMAP, this does NOT check HasCopiesInOtherFolders —
// it unconditionally removes the messages from the folder (removes the Gmail label).
func (a *App) removeFromIMAPFolder(messages []*message.Message, folderID string) error {
	if len(messages) == 0 {
		return nil
	}

	folderObj, err := a.folderStore.Get(folderID)
	if err != nil || folderObj == nil {
		return fmt.Errorf("folder not found")
	}

	var uids []goImap.UID
	for _, m := range messages {
		if m.UID == 0 || int32(m.UID) < 0 {
			continue
		}
		uids = append(uids, goImap.UID(m.UID))
	}
	if len(uids) == 0 {
		return nil
	}

	// See ownExpungeEchoSuppress — defer the IDLE echo of our own delete.
	a.noteOwnExpunge(messages[0].AccountID)
	defer a.noteOwnExpunge(messages[0].AccountID)

	return a.withIMAPRetry(messages[0].AccountID, func(conn *imap.Client) error {
		if _, err := conn.SelectMailbox(a.ctx, folderObj.Path); err != nil {
			return fmt.Errorf("failed to select mailbox: %w", err)
		}
		return conn.DeleteMessagesByUID(uids)
	})
}

// MarkAsSpam moves messages to the Spam folder.
// Returns true if at least one message was moved to spam (show undo toast).
// Returns false if all messages were just label-removed on Gmail (no undo).
func (a *App) MarkAsSpam(messageIDs []string) (bool, error) {
	moved, _, _, err := a.runMoveMutation(messageIDs, "mark-as-spam", func() (bool, string, error) {
		return a.markAsSpamWithUndo(messageIDs, "")
	})
	return moved, err
}

// MarkAsSpamWithUndo marks messages as spam and atomically returns its undo operation.
func (a *App) MarkAsSpamWithUndo(messageIDs []string) (SpamUndoResult, error) {
	moved, operationID, coalesced, err := a.runMoveMutation(messageIDs, "mark-as-spam", func() (bool, string, error) {
		return a.markAsSpamWithUndo(messageIDs, "")
	})
	return SpamUndoResult{MovedToSpam: moved, OperationID: operationID, Coalesced: coalesced}, err
}

func (a *App) markAsSpamWithUndo(messageIDs []string, operationID string) (bool, string, error) {
	if len(messageIDs) == 0 {
		return false, operationID, nil
	}

	// Cross-account selections (Unified Inbox) route to a partition
	// helper that recurses through this same function with uniform
	// single-account slices.
	if spans, _ := a.messageStore.SpansMultipleAccounts(messageIDs); spans {
		return a.markAsSpamCrossAccount(messageIDs, operationID)
	}

	messages, err := a.messageStore.GetByIDs(messageIDs[:1])
	if err != nil || len(messages) == 0 {
		return false, operationID, fmt.Errorf("failed to get message")
	}

	accountID := messages[0].AccountID

	spamFolder, err := a.GetSpecialFolder(accountID, folder.TypeSpam)
	if err != nil {
		return false, operationID, fmt.Errorf("failed to get spam folder: %w", err)
	}
	if spamFolder == nil {
		return false, operationID, fmt.Errorf("no spam folder configured")
	}

	// Non-Gmail: normal move to spam
	if !a.isGmailAccount(accountID) {
		operationID, err = a.moveToFolder(messageIDs, spamFolder.ID, true, operationID)
		return true, operationID, err
	}

	// Gmail: partition messages into copies (label-remove) vs sole copies (move to spam)
	return a.gmailTrashOrSpam(messageIDs, spamFolder, operationID)
}

// MarkAsNotSpam moves messages from Spam to Inbox
func (a *App) MarkAsNotSpam(messageIDs []string) error {
	_, _, _, err := a.runMoveMutation(messageIDs, "mark-as-not-spam", func() (bool, string, error) {
		operationID, notSpamErr := a.markAsNotSpamWithUndo(messageIDs, "")
		return false, operationID, notSpamErr
	})
	return err
}

// MarkAsNotSpamWithUndo moves messages to Inbox and atomically returns its undo operation.
func (a *App) MarkAsNotSpamWithUndo(messageIDs []string) (UndoableActionResult, error) {
	_, operationID, coalesced, err := a.runMoveMutation(messageIDs, "mark-as-not-spam", func() (bool, string, error) {
		op, notSpamErr := a.markAsNotSpamWithUndo(messageIDs, "")
		return false, op, notSpamErr
	})
	return UndoableActionResult{OperationID: operationID, Coalesced: coalesced}, err
}

func (a *App) markAsNotSpamWithUndo(messageIDs []string, operationID string) (string, error) {
	return a.moveToInboxWithUndo(messageIDs, operationID)
}

// MoveToInbox restores messages to the Inbox of their respective accounts.
func (a *App) MoveToInbox(messageIDs []string) error {
	_, _, _, err := a.runMoveMutation(messageIDs, "move-to-inbox", func() (bool, string, error) {
		operationID, inboxErr := a.moveToInboxWithUndo(messageIDs, "")
		return false, operationID, inboxErr
	})
	return err
}

// MoveToInboxWithUndo moves messages to Inbox and atomically returns its undo operation.
func (a *App) MoveToInboxWithUndo(messageIDs []string) (UndoableActionResult, error) {
	_, operationID, coalesced, err := a.runMoveMutation(messageIDs, "move-to-inbox", func() (bool, string, error) {
		op, inboxErr := a.moveToInboxWithUndo(messageIDs, "")
		return false, op, inboxErr
	})
	return UndoableActionResult{OperationID: operationID, Coalesced: coalesced}, err
}

func (a *App) moveToInboxWithUndo(messageIDs []string, operationID string) (string, error) {
	if len(messageIDs) == 0 {
		return operationID, nil
	}

	// Cross-account selections (Unified Inbox) route to a partition
	// helper that recurses through this same function with uniform
	// single-account slices.
	if spans, _ := a.messageStore.SpansMultipleAccounts(messageIDs); spans {
		return a.moveToInboxCrossAccount(messageIDs, operationID)
	}

	messages, err := a.messageStore.GetByIDs(messageIDs[:1])
	if err != nil || len(messages) == 0 {
		return operationID, fmt.Errorf("failed to get message")
	}

	inboxFolder, err := a.GetSpecialFolder(messages[0].AccountID, folder.TypeInbox)
	if err != nil {
		return operationID, fmt.Errorf("failed to get inbox folder: %w", err)
	}
	if inboxFolder == nil {
		return operationID, fmt.Errorf("no inbox folder found")
	}

	return a.moveToFolder(messageIDs, inboxFolder.ID, true, operationID)
}

// EmptyTrash permanently deletes all messages in a trash folder
func (a *App) EmptyTrash(accountID, folderID string) error {
	ids, err := a.messageStore.GetAllIDsByFolder(folderID)
	if err != nil {
		return fmt.Errorf("failed to get messages in trash: %w", err)
	}
	if len(ids) == 0 {
		return nil
	}
	return a.DeletePermanently(ids)
}

// DeletePermanently permanently deletes messages
func (a *App) DeletePermanently(messageIDs []string) error {
	log := logging.WithComponent("app")

	if len(messageIDs) == 0 {
		return nil
	}

	messages, err := a.messageStore.GetByIDs(messageIDs)
	if err != nil {
		return fmt.Errorf("failed to get messages: %w", err)
	}
	if len(messages) == 0 {
		return nil
	}

	// Group by folder
	byFolder := make(map[string][]*message.Message)
	for _, m := range messages {
		byFolder[m.FolderID] = append(byFolder[m.FolderID], m)
	}

	// Delete from local DB first
	if err := a.messageStore.DeleteBatch(messageIDs); err != nil {
		return fmt.Errorf("failed to delete messages locally: %w", err)
	}

	wailsRuntime.EventsEmit(a.ctx, "messages:deleted", messageIDs)

	// Update folder unread counts
	go func() {
		defer recoverPanic("app.actions", "update folder counts after delete")
		folderCounts := make(map[string]int)
		for folderID, msgs := range byFolder {
			unreadCount, err := a.messageStore.CountUnreadByFolder(folderID)
			if err != nil {
				log.Error().Err(err).Str("folderID", folderID).Msg("Failed to count unread messages")
				continue
			}
			folderObj, err := a.folderStore.Get(folderID)
			if err != nil || folderObj == nil {
				log.Error().Err(err).Str("folderID", folderID).Msg("Failed to get folder")
				continue
			}
			newTotalCount := folderObj.TotalCount - len(msgs)
			if newTotalCount < 0 {
				newTotalCount = 0
			}
			if err := a.folderStore.UpdateCounts(folderID, newTotalCount, unreadCount); err != nil {
				log.Error().Err(err).Str("folderID", folderID).Msg("Failed to update folder counts")
				continue
			}
			folderCounts[folderID] = unreadCount
		}
		if len(folderCounts) > 0 {
			wailsRuntime.EventsEmit(a.ctx, "folders:countsChanged", folderCounts)
		}
	}()

	// Delete from IMAP in background
	go func() {
		defer recoverPanic("app.actions", "delete from IMAP")
		for folderID, msgs := range byFolder {
			if err := a.deleteMessagesFromIMAP(msgs, folderID); err != nil {
				log.Error().Err(err).Str("folderID", folderID).Msg("Failed to delete messages from IMAP")
			}
		}
	}()

	// Note: Permanent delete undo is complex - would need to store full message content
	// For now, we don't add to undo stack for permanent deletes

	return nil
}

func (a *App) deleteMessagesFromIMAP(messages []*message.Message, folderID string) error {
	if len(messages) == 0 {
		return nil
	}

	log := logging.WithComponent("app.deleteMessagesFromIMAP")

	folderObj, err := a.folderStore.Get(folderID)
	if err != nil || folderObj == nil {
		return fmt.Errorf("folder not found")
	}

	accountID := messages[0].AccountID
	isGmail := a.isGmailAccount(accountID)

	var uids []goImap.UID
	for _, m := range messages {
		// Skip messages with temp UIDs (negative values from local move operations)
		// that haven't been reconciled with the IMAP server yet
		if m.UID == 0 || int32(m.UID) < 0 {
			continue
		}

		// For Gmail: skip IMAP delete if the same RFC 822 Message-ID exists in
		// other local folders. On Gmail there's only ONE underlying message — an
		// IMAP EXPUNGE here would destroy it across ALL labels (Inbox, etc.).
		if isGmail && m.MessageID != "" {
			hasCopies, copyErr := a.messageStore.HasCopiesInOtherFolders(m.MessageID, folderID, accountID)
			if copyErr != nil {
				log.Warn().Err(copyErr).Str("message_ref", logging.ShortHash(m.MessageID)).Msg("Failed to check for copies in other folders")
			}
			if hasCopies {
				log.Debug().Str("message_ref", logging.ShortHash(m.MessageID)).Msg("Gmail: skipping IMAP delete — message exists in other folders")
				continue
			}
		}

		uids = append(uids, goImap.UID(m.UID))
	}
	if len(uids) == 0 {
		return nil
	}

	// See ownExpungeEchoSuppress — defer the IDLE echo of our own delete.
	a.noteOwnExpunge(accountID)
	defer a.noteOwnExpunge(accountID)

	return a.withIMAPRetry(accountID, func(conn *imap.Client) error {
		if _, err := conn.SelectMailbox(a.ctx, folderObj.Path); err != nil {
			return fmt.Errorf("failed to select mailbox: %w", err)
		}

		return conn.DeleteMessagesByUID(uids)
	})
}

// ============================================================================
// Cross-account dispatch helpers
//
// Bulk actions invoked from Unified Inbox may carry a selection that spans
// multiple accounts. The public entry points (Trash, Archive, MarkAsSpam,
// MarkAsNotSpam, MoveToFolder, CopyToFolder) detect this up front via
// messageStore.SpansMultipleAccounts and route here. Each helper partitions
// the IDs by AccountID, then loops by re-invoking the public function with
// each partition's single-account slice. The recursive call takes the
// untouched single-account fast path. Recursion depth is bounded at 1 by
// construction (partition slices are uniform-account by definition).
// ============================================================================

// partitionByAccount groups message IDs by their owning account. Invalid
// IDs (no matching row in the store) are silently dropped — matching the
// behavior of every other call site that consumes GetByIDs results.
func (a *App) partitionByAccount(messageIDs []string) (map[string][]string, error) {
	msgs, err := a.messageStore.GetByIDs(messageIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to get messages: %w", err)
	}
	byAccount := make(map[string][]string)
	for _, m := range msgs {
		byAccount[m.AccountID] = append(byAccount[m.AccountID], m.ID)
	}
	return byAccount, nil
}

// trashCrossAccount fan-outs Trash() per account partition. Aggregates the
// (bool, error) returns: anyMoved is OR'd across partitions, firstErr wins.
func (a *App) trashCrossAccount(messageIDs []string, operationID string) (bool, string, error) {
	byAccount, err := a.partitionByAccount(messageIDs)
	if err != nil {
		return false, operationID, err
	}
	var anyMoved bool
	var firstErr error
	for _, ids := range byAccount {
		moved, nextOperationID, err := a.trashWithUndo(ids, operationID)
		operationID = nextOperationID
		if moved {
			anyMoved = true
		}
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return anyMoved, operationID, firstErr
}

// archiveCrossAccount fan-outs Archive() per account partition. Aggregates
// first error encountered.
func (a *App) archiveCrossAccount(messageIDs []string, operationID string) (string, error) {
	byAccount, err := a.partitionByAccount(messageIDs)
	if err != nil {
		return operationID, err
	}
	var firstErr error
	for _, ids := range byAccount {
		var actionErr error
		operationID, actionErr = a.archiveWithUndo(ids, operationID)
		if actionErr != nil && firstErr == nil {
			firstErr = actionErr
		}
	}
	return operationID, firstErr
}

// markAsSpamCrossAccount fan-outs MarkAsSpam() per account partition.
func (a *App) markAsSpamCrossAccount(messageIDs []string, operationID string) (bool, string, error) {
	byAccount, err := a.partitionByAccount(messageIDs)
	if err != nil {
		return false, operationID, err
	}
	var anyMoved bool
	var firstErr error
	for _, ids := range byAccount {
		moved, nextOperationID, err := a.markAsSpamWithUndo(ids, operationID)
		operationID = nextOperationID
		if moved {
			anyMoved = true
		}
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return anyMoved, operationID, firstErr
}

// moveToInboxCrossAccount fan-outs MoveToInbox() per account partition.
func (a *App) moveToInboxCrossAccount(messageIDs []string, operationID string) (string, error) {
	byAccount, err := a.partitionByAccount(messageIDs)
	if err != nil {
		return operationID, err
	}
	var firstErr error
	for _, ids := range byAccount {
		var actionErr error
		operationID, actionErr = a.moveToInboxWithUndo(ids, operationID)
		if actionErr != nil && firstErr == nil {
			firstErr = actionErr
		}
	}
	return operationID, firstErr
}

// moveToFolderCrossAccount fan-outs MoveToFolder() per source-account
// partition. Each recursive call hits the public MoveToFolder with a
// uniform-source-account slice; the existing
// `messages[0].AccountID != destFolder.AccountID` guard in that function
// then classifies the WHOLE partition correctly:
//   - partition's account == dest's account → same-account move path.
//   - partition's account != dest's account → existing cross-account
//     APPEND-then-Trash path (copyMessagesAcrossAccounts + recursive
//     Trash on the partition's IDs).
//
// Each partition's outcome is independent — a Gmail partition's failure
// doesn't block an IMAP partition's success.
func (a *App) moveToFolderCrossAccount(messageIDs []string, destFolderID string, recordUndo bool, operationID string) (string, error) {
	byAccount, err := a.partitionByAccount(messageIDs)
	if err != nil {
		return operationID, err
	}
	var firstErr error
	for _, ids := range byAccount {
		var actionErr error
		operationID, actionErr = a.moveToFolder(ids, destFolderID, recordUndo, operationID)
		if actionErr != nil && firstErr == nil {
			firstErr = actionErr
		}
	}
	return operationID, firstErr
}

// copyToFolderCrossAccount fan-outs CopyToFolder() per source-account
// partition. Mirrors moveToFolderCrossAccount but Copy keeps originals.
func (a *App) copyToFolderCrossAccount(messageIDs []string, destFolderID string) error {
	byAccount, err := a.partitionByAccount(messageIDs)
	if err != nil {
		return err
	}
	var firstErr error
	for _, ids := range byAccount {
		if err := a.CopyToFolder(ids, destFolderID); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

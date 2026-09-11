package app

import (
	"context"
	"fmt"

	"github.com/hkdb/aerion/internal/imap"
	"github.com/hkdb/aerion/internal/logging"
	undoPkg "github.com/hkdb/aerion/internal/undo"
	wailsRuntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// ============================================================================
// Undo API - Exposed to frontend via Wails bindings
// ============================================================================

func (a *App) emitRuntimeEvent(eventName string, data ...interface{}) {
	if a.runtimeEventEmitter != nil {
		a.runtimeEventEmitter(eventName, data...)
		return
	}
	wailsRuntime.EventsEmit(a.ctx, eventName, data...)
}

// UndoLatestResult identifies the operation completed by global/LIFO Undo.
type UndoLatestResult struct {
	OperationID string `json:"operationId"`
	Description string `json:"description"`
}

type undoFolderDiagnostics interface {
	DiagnosticFolderIDs() (sourceFolderID, destFolderID string)
}

func undoFolderFields(cmd undoPkg.Command) (string, string) {
	if diagnostic, ok := cmd.(undoFolderDiagnostics); ok {
		return diagnostic.DiagnosticFolderIDs()
	}
	return "", ""
}

// undoLatest reverses the newest complete user action and retains the claim on
// failure so an execution error does not silently consume the operation.
func (a *App) undoLatest() (UndoLatestResult, error) {
	claim := a.undoStack.PopLatestOperation()
	if claim == nil {
		return UndoLatestResult{}, fmt.Errorf("nothing to undo")
	}
	commands := claim.Commands()
	description := commands[0].Description()
	log := logging.WithComponent("undo")
	for index, cmd := range commands {
		sourceFolderID, destFolderID := undoFolderFields(cmd)
		log.Info().
			Str("operationId", claim.OperationID).
			Str("action", cmd.Description()).
			Str("sourceFolderID", sourceFolderID).
			Str("destFolderID", destFolderID).
			Int("command", index+1).
			Int("commands", len(commands)).
			Msg("UNDO EXEC")
		if err := cmd.Undo(); err != nil {
			a.undoStack.RestoreOperation(claim, index)
			log.Error().
				Err(err).
				Str("operationId", claim.OperationID).
				Str("action", cmd.Description()).
				Str("sourceFolderID", sourceFolderID).
				Str("destFolderID", destFolderID).
				Int("command", index+1).
				Int("commands", len(commands)).
				Msg("UNDO ERROR")
			return UndoLatestResult{}, fmt.Errorf("undo failed: %w", err)
		}
	}
	log.Info().Str("operationId", claim.OperationID).Str("action", description).Msg("UNDO DONE")

	// Emit event to refresh UI
	a.emitRuntimeEvent("undo:completed", description)
	return UndoLatestResult{OperationID: claim.OperationID, Description: description}, nil
}

// Undo reverses the most recent undoable action and preserves the legacy
// description-only result for existing consumers.
func (a *App) Undo() (string, error) {
	result, err := a.undoLatest()
	return result.Description, err
}

// UndoLatestWithResult is the additive global Undo API used by Ctrl+Z so the
// frontend can reconcile the exact search snapshot that was undone.
func (a *App) UndoLatestWithResult() (UndoLatestResult, error) {
	return a.undoLatest()
}

// UndoOperation reverses only commands created by the matching user action.
// Claiming the operation is atomic, so a second click cannot run it twice.
func (a *App) UndoOperation(operationID string) (string, error) {
	log := logging.WithComponent("undo")
	if operationID == "" {
		log.Warn().Msg("UNDO MISS")
		return "", fmt.Errorf("missing undo operation")
	}
	commands := a.undoStack.PopOperation(operationID)
	if len(commands) == 0 {
		return "", fmt.Errorf("undo operation is unavailable")
	}
	description := commands[0].Description()
	for index, cmd := range commands {
		log.Info().
			Str("operationId", operationID).
			Str("action", cmd.Description()).
			Int("command", index+1).
			Int("commands", len(commands)).
			Msg("UNDO EXEC")
		if err := cmd.Undo(); err != nil {
			log.Error().
				Err(err).
				Str("operationId", operationID).
				Str("action", cmd.Description()).
				Int("command", index+1).
				Int("commands", len(commands)).
				Msg("UNDO ERROR")
			return "", fmt.Errorf("undo failed: %w", err)
		}
		log.Info().
			Str("operationId", operationID).
			Str("action", cmd.Description()).
			Int("command", index+1).
			Int("commands", len(commands)).
			Msg("UNDO COMMAND DONE")
	}
	a.emitRuntimeEvent("undo:completed", description)
	log.Info().Str("operationId", operationID).Str("action", description).Msg("UNDO DONE")
	return description, nil
}

// CanUndo returns true if there's an action that can be undone
func (a *App) CanUndo() bool {
	return a.undoStack.CanUndo()
}

// GetUndoDescription returns the description of what would be undone
func (a *App) GetUndoDescription() string {
	cmd := a.undoStack.Peek()
	if cmd == nil {
		return ""
	}
	return cmd.Description()
}

// ============================================================================
// UndoContext Implementation - Required for undo.Command operations
// ============================================================================

// GetIMAPConnectionForUndo implements undo.UndoContext
func (a *App) GetIMAPConnectionForUndo(ctx context.Context, accountID string) (*imap.Client, func(), error) {
	poolConn, err := a.imapPool.GetConnection(ctx, accountID)
	if err != nil {
		return nil, nil, err
	}
	return poolConn.Client(), func() { a.imapPool.Release(poolConn) }, nil
}

// UpdateLocalFlags implements undo.UndoContext
func (a *App) UpdateLocalFlags(messageIDs []string, isRead, isStarred *bool) error {
	err := a.messageStore.UpdateFlagsBatch(messageIDs, isRead, isStarred)
	if err != nil {
		return err
	}
	// Emit a per-flag event so each listener sees the typed payload it
	// expects. UpdateLocalFlags is called by undo commands; today each
	// command flips exactly one flag (either read or starred), so in
	// practice only one branch fires. The if/if (not if/else) shape
	// covers a hypothetical future undo that combines both without
	// changing this call site.
	if isRead != nil {
		wailsRuntime.EventsEmit(a.ctx, "messages:readChanged", map[string]interface{}{
			"messageIds": messageIDs,
			"isRead":     *isRead,
		})
	}
	if isStarred != nil {
		wailsRuntime.EventsEmit(a.ctx, "messages:starredChanged", map[string]interface{}{
			"messageIds": messageIDs,
			"isStarred":  *isStarred,
		})
	}
	return nil
}

// MoveLocalMessages implements undo.UndoContext
func (a *App) MoveLocalMessages(messageIDs []string, folderID string) error {
	// Get the source folder IDs before moving (for count updates)
	messages, err := a.messageStore.GetByIDs(messageIDs)
	if err != nil {
		return fmt.Errorf("failed to get messages: %w", err)
	}

	// Group by source folder
	sourceFolderIDs := make(map[string]bool)
	for _, msg := range messages {
		sourceFolderIDs[msg.FolderID] = true
	}

	// Move messages in database
	err = a.messageStore.MoveMessages(messageIDs, folderID)
	if err != nil {
		return err
	}

	// Emit messages:moved event
	wailsRuntime.EventsEmit(a.ctx, "messages:moved", map[string]interface{}{
		"messageIds":   messageIDs,
		"destFolderId": folderID,
	})

	// Update folder counts for all affected folders (source + destination)
	go func() {
		defer recoverPanic("app.undo", "update folder counts")
		folderCounts := make(map[string]int)

		// Update source folders
		for sourceFolderID := range sourceFolderIDs {
			unreadCount, err := a.messageStore.CountUnreadByFolder(sourceFolderID)
			if err == nil {
				folderObj, err := a.folderStore.Get(sourceFolderID)
				if err == nil && folderObj != nil {
					totalCount, _ := a.messageStore.CountByFolder(sourceFolderID)
					_ = a.folderStore.UpdateCounts(sourceFolderID, totalCount, unreadCount)
					folderCounts[sourceFolderID] = unreadCount
				}
			}
		}

		// Update destination folder
		unreadCount, err := a.messageStore.CountUnreadByFolder(folderID)
		if err == nil {
			folderObj, err := a.folderStore.Get(folderID)
			if err == nil && folderObj != nil {
				totalCount, _ := a.messageStore.CountByFolder(folderID)
				_ = a.folderStore.UpdateCounts(folderID, totalCount, unreadCount)
				folderCounts[folderID] = unreadCount
			}
		}

		if len(folderCounts) > 0 {
			wailsRuntime.EventsEmit(a.ctx, "folders:countsChanged", folderCounts)
		}
	}()

	return nil
}

// DeleteLocalMessages implements undo.UndoContext
func (a *App) DeleteLocalMessages(messageIDs []string) error {
	err := a.messageStore.DeleteBatch(messageIDs)
	if err == nil {
		wailsRuntime.EventsEmit(a.ctx, "messages:deleted", messageIDs)
	}
	return err
}

// FindLocalMessageIDs implements undo.UndoContext
// Finds current local DB message IDs by RFC822 Message-ID header and folder
func (a *App) FindLocalMessageIDs(accountID, folderID string, rfc822MessageIDs []string) ([]string, error) {
	return a.messageStore.GetIDsByMessageIDs(accountID, folderID, rfc822MessageIDs)
}

// FindLocalMessageIDsByLocalID is the primary exact lookup for Move Undo.
// Folder/account predicates prevent matching a row that is no longer in the
// move destination.
func (a *App) FindLocalMessageIDsByLocalID(accountID, folderID string, localMessageIDs []string) ([]string, error) {
	return a.messageStore.GetExistingIDsInFolder(accountID, folderID, localMessageIDs)
}

// MoveMessagesToFolder implements undo.UndoContext
// Delegates to the standard MoveToFolder pipeline (IMAP + local DB + events)
func (a *App) MoveMessagesToFolder(messageIDs []string, destFolderID string) error {
	// This call is itself an Undo operation. Reuse the complete local-first
	// + IMAP reconciliation pipeline, but never put the reverse move back
	// onto the undo stack.
	return a.runUndoMoveMutation(messageIDs, destFolderID, "")
}

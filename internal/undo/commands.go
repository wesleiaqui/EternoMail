package undo

import (
	"context"
	"fmt"
	stdlog "log"
	"sync"

	"github.com/emersion/go-imap/v2"
	imapPkg "github.com/hkdb/aerion/internal/imap"
)

// UndoContext provides dependencies for undo operations
type UndoContext interface {
	// GetIMAPConnectionForUndo returns an IMAP client for the account
	GetIMAPConnectionForUndo(ctx context.Context, accountID string) (*imapPkg.Client, func(), error)
	// UpdateLocalFlags updates flags in local database
	UpdateLocalFlags(messageIDs []string, isRead, isStarred *bool) error
	// MoveLocalMessages moves messages in local database
	MoveLocalMessages(messageIDs []string, folderID string) error
	// DeleteLocalMessages deletes messages from local database
	DeleteLocalMessages(messageIDs []string) error
	// FindLocalMessageIDs finds current local DB message IDs by RFC822 Message-ID and folder
	FindLocalMessageIDs(accountID, folderID string, rfc822MessageIDs []string) ([]string, error)
	// MoveMessagesToFolder moves messages using the full move pipeline (IMAP + local DB)
	MoveMessagesToFolder(messageIDs []string, destFolderID string) error
}

// FlagChangeCommand handles read/star flag changes
type FlagChangeCommand struct {
	BaseCommand
	ctx           context.Context
	undoCtx       UndoContext
	accountID     string
	folderPath    string
	messageIDs    []string
	uids          []uint32
	flagType      string // "read" or "starred"
	previousState bool   // What was the state before
}

// NewFlagChangeCommand creates a new FlagChangeCommand
func NewFlagChangeCommand(
	ctx context.Context,
	undoCtx UndoContext,
	accountID, folderPath string,
	messageIDs []string,
	uids []uint32,
	flagType string,
	previousState bool,
	description string,
) *FlagChangeCommand {
	return &FlagChangeCommand{
		BaseCommand:   NewBaseCommand(description),
		ctx:           ctx,
		undoCtx:       undoCtx,
		accountID:     accountID,
		folderPath:    folderPath,
		messageIDs:    messageIDs,
		uids:          uids,
		flagType:      flagType,
		previousState: previousState,
	}
}

// Execute performs the action (already done at creation time)
func (c *FlagChangeCommand) Execute() error { return nil }

// Undo reverses the flag change
func (c *FlagChangeCommand) Undo() error {
	// Get IMAP connection
	client, release, err := c.undoCtx.GetIMAPConnectionForUndo(c.ctx, c.accountID)
	if err != nil {
		return fmt.Errorf("failed to get IMAP connection: %w", err)
	}
	defer release()

	// Select mailbox
	if _, err := client.SelectMailbox(c.ctx, c.folderPath); err != nil {
		return fmt.Errorf("failed to select mailbox: %w", err)
	}

	// Convert UIDs
	imapUIDs := make([]imap.UID, len(c.uids))
	for i, uid := range c.uids {
		imapUIDs[i] = imap.UID(uid)
	}

	// Determine flag
	var flag imap.Flag
	switch c.flagType {
	case "read":
		flag = imap.FlagSeen
	case "starred":
		flag = imap.FlagFlagged
	default:
		return fmt.Errorf("unknown flag type: %s", c.flagType)
	}

	// Restore previous state on IMAP
	if c.previousState {
		if err := client.AddMessageFlags(imapUIDs, []imap.Flag{flag}); err != nil {
			return fmt.Errorf("failed to add flags: %w", err)
		}
	} else {
		if err := client.RemoveMessageFlags(imapUIDs, []imap.Flag{flag}); err != nil {
			return fmt.Errorf("failed to remove flags: %w", err)
		}
	}

	// Update local database
	var isRead, isStarred *bool
	switch c.flagType {
	case "read":
		isRead = &c.previousState
	case "starred":
		isStarred = &c.previousState
	}
	if err := c.undoCtx.UpdateLocalFlags(c.messageIDs, isRead, isStarred); err != nil {
		return fmt.Errorf("failed to update local flags: %w", err)
	}

	return nil
}

// MoveCompletion lets the UI-facing move return immediately while Undo
// waits only when the remote IMAP move is still being reconciled.
type MoveCompletion struct {
	done        chan struct{}
	once        sync.Once
	mu          sync.RWMutex
	err         error
	operationID string
}

// SetOperationID associates temporary reconciliation diagnostics with the
// user action without exposing message content.
func (c *MoveCompletion) SetOperationID(operationID string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.operationID = operationID
	c.mu.Unlock()
}

// OperationID returns the user action associated with this completion.
func (c *MoveCompletion) OperationID() string {
	if c == nil {
		return ""
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.operationID
}

func NewMoveCompletion() *MoveCompletion {
	return &MoveCompletion{done: make(chan struct{})}
}

func (c *MoveCompletion) Complete(err error) {
	if c == nil {
		return
	}

	c.once.Do(func() {
		c.mu.Lock()
		c.err = err
		c.mu.Unlock()
		close(c.done)
	})
}

func (c *MoveCompletion) Wait() error {
	if c == nil {
		return nil
	}

	<-c.done

	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.err
}

// MoveCommand handles moving messages between folders
type MoveCommand struct {
	BaseCommand
	undoCtx        UndoContext
	accountID      string
	identities     []MoveMessageIdentity
	operationID    string
	sourceFolderID string
	destFolderID   string
	completion     *MoveCompletion
	tracedMove     func(messageIDs []string, destFolderID, operationID string) error
}

// MoveMessageIdentity keeps the immutable local row identity paired with its
// optional RFC822 Message-ID fallback. Message-ID is not a unique local key.
type MoveMessageIdentity struct {
	LocalID   string
	MessageID string
}

// NewMoveCommand creates a new MoveCommand
func NewMoveCommand(
	undoCtx UndoContext,
	accountID string,
	identities []MoveMessageIdentity,
	sourceFolderID string,
	destFolderID string,
	description string,
	completion ...*MoveCompletion,
) *MoveCommand {
	var moveCompletion *MoveCompletion
	if len(completion) > 0 {
		moveCompletion = completion[0]
	}

	return &MoveCommand{
		BaseCommand:    NewBaseCommand(description),
		undoCtx:        undoCtx,
		accountID:      accountID,
		identities:     append([]MoveMessageIdentity(nil), identities...),
		sourceFolderID: sourceFolderID,
		destFolderID:   destFolderID,
		completion:     moveCompletion,
	}
}

// Execute performs the action (already done at creation time)
func (c *MoveCommand) Execute() error { return nil }

// DiagnosticFolderIDs exposes only safe identifiers for temporary Undo
// instrumentation. It deliberately excludes RFC822 headers and message data.
func (c *MoveCommand) DiagnosticFolderIDs() (string, string) {
	return c.sourceFolderID, c.destFolderID
}

// SetOperationID attaches the stack token before background reconciliation can
// emit diagnostics for this command.
func (c *MoveCommand) SetOperationID(operationID string) {
	c.operationID = operationID
}

// SetTracedMove installs the app's normal move pipeline while allowing its
// temporary diagnostics to retain this command's operation ID.
func (c *MoveCommand) SetTracedMove(move func(messageIDs []string, destFolderID, operationID string) error) {
	c.tracedMove = move
}

// Undo reverses the move by finding current messages in the destination folder
// and moving them back using the standard move pipeline.
func (c *MoveCommand) Undo() error {
	// The local-first move returns before IMAP reconciliation finishes.
	// If Undo is clicked immediately, wait here rather than blocking the
	// original user action and its success toast.
	if c.completion != nil {
		if err := c.completion.Wait(); err != nil {
			return fmt.Errorf("original move did not complete: %w", err)
		}
	}

	expectedLocalIDs := make([]string, 0, len(c.identities))
	for _, identity := range c.identities {
		expectedLocalIDs = append(expectedLocalIDs, identity.LocalID)
	}
	exactFinder, ok := c.undoCtx.(interface {
		FindLocalMessageIDsByLocalID(accountID, folderID string, localMessageIDs []string) ([]string, error)
	})
	if !ok {
		return fmt.Errorf("exact local-ID lookup is unavailable")
	}
	exactIDs, err := exactFinder.FindLocalMessageIDsByLocalID(c.accountID, c.destFolderID, expectedLocalIDs)
	if err != nil {
		return fmt.Errorf("failed exact local-ID lookup: %w", err)
	}
	exactSet := make(map[string]bool, len(exactIDs))
	for _, id := range exactIDs {
		exactSet[id] = true
	}
	usedResolvedIDs := make(map[string]bool, len(c.identities))

	localMsgIDs := make([]string, 0, len(c.identities))
	for _, identity := range c.identities {
		if exactSet[identity.LocalID] {
			localMsgIDs = append(localMsgIDs, identity.LocalID)
			usedResolvedIDs[identity.LocalID] = true
			continue
		}
		if identity.MessageID == "" {
			stdlog.Printf("UNDO LOOKUP operationId=%s sourceFolderId=%s destFolderId=%s exactFound=%d found=%d expected=%d messageIdFallback=true ambiguous=false error=%q", c.operationID, c.sourceFolderID, c.destFolderID, len(exactIDs), len(localMsgIDs), len(c.identities), "missing exact row and RFC822 Message-ID")
			return fmt.Errorf("message %s is missing from destination and has no safe fallback identity", identity.LocalID)
		}
		candidates, findErr := c.undoCtx.FindLocalMessageIDs(c.accountID, c.destFolderID, []string{identity.MessageID})
		if findErr != nil {
			return fmt.Errorf("failed Message-ID fallback lookup: %w", findErr)
		}
		unused := make([]string, 0, len(candidates))
		for _, candidate := range candidates {
			if !usedResolvedIDs[candidate] {
				unused = append(unused, candidate)
			}
		}
		if len(unused) != 1 {
			stdlog.Printf("UNDO LOOKUP operationId=%s sourceFolderId=%s destFolderId=%s exactFound=%d found=%d expected=%d messageIdFallback=true ambiguous=%t candidates=%d", c.operationID, c.sourceFolderID, c.destFolderID, len(exactIDs), len(localMsgIDs), len(c.identities), len(unused) > 1, len(unused))
			return fmt.Errorf("ambiguous Message-ID fallback in destination folder %s: got %d candidates for one moved row", c.destFolderID, len(unused))
		}
		localMsgIDs = append(localMsgIDs, unused[0])
		usedResolvedIDs[unused[0]] = true
	}
	// Reuse the full move pipeline (IMAP + local DB + events)
	if c.tracedMove != nil {
		return c.tracedMove(localMsgIDs, c.sourceFolderID, c.operationID)
	}
	return c.undoCtx.MoveMessagesToFolder(localMsgIDs, c.sourceFolderID)
}

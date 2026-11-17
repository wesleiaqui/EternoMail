package undo

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/hkdb/aerion/internal/replylater"
	"github.com/hkdb/aerion/internal/triage"
)

// Durable command types stored in mailbox_undo_commands.type.
const (
	commandTypeFlag       = "flag"
	commandTypeMove       = "move"
	commandTypeReplyLater = "reply-later"
	commandTypeLater      = "later"
)

type flagPayload struct {
	AccountID     string    `json:"accountId"`
	FolderPath    string    `json:"folderPath"`
	MessageIDs    []string  `json:"messageIds"`
	UIDs          []uint32  `json:"uids"`
	FlagType      string    `json:"flagType"`
	PreviousState bool      `json:"previousState"`
	Description   string    `json:"description"`
	CreatedAt     time.Time `json:"createdAt"`
}

type movePayload struct {
	AccountID      string                `json:"accountId"`
	Identities     []MoveMessageIdentity `json:"identities"`
	SourceFolderID string                `json:"sourceFolderId"`
	DestFolderID   string                `json:"destFolderId"`
	Description    string                `json:"description"`
	CreatedAt      time.Time             `json:"createdAt"`
}

type replyLaterPayload struct {
	Ref         replylater.Ref `json:"ref"`
	Previous    bool           `json:"previous"`
	Description string         `json:"description"`
	CreatedAt   time.Time      `json:"createdAt"`
}

type laterPayload struct {
	Ref         triage.Ref        `json:"ref"`
	Before      triage.LaterState `json:"before"`
	After       triage.LaterState `json:"after"`
	Description string            `json:"description"`
	CreatedAt   time.Time         `json:"createdAt"`
}

// marshalCommand converts a command to its durable type tag + JSON payload.
// The boolean is false for command kinds that are intentionally not durable.
func marshalCommand(cmd Command) (string, []byte, bool) {
	switch c := cmd.(type) {
	case *FlagChangeCommand:
		payload, err := json.Marshal(flagPayload{
			AccountID:     c.accountID,
			FolderPath:    c.folderPath,
			MessageIDs:    c.messageIDs,
			UIDs:          c.uids,
			FlagType:      c.flagType,
			PreviousState: c.previousState,
			Description:   c.description,
			CreatedAt:     c.createdAt,
		})
		if err != nil {
			return "", nil, false
		}
		return commandTypeFlag, payload, true
	case *MoveCommand:
		payload, err := json.Marshal(movePayload{
			AccountID:      c.accountID,
			Identities:     c.identities,
			SourceFolderID: c.sourceFolderID,
			DestFolderID:   c.destFolderID,
			Description:    c.description,
			CreatedAt:      c.createdAt,
		})
		if err != nil {
			return "", nil, false
		}
		return commandTypeMove, payload, true
	case *LaterChangeCommand:
		payload, err := json.Marshal(laterPayload{Ref: c.ref, Before: c.before, After: c.after, Description: c.description, CreatedAt: c.createdAt})
		if err != nil {
			return "", nil, false
		}
		return commandTypeLater, payload, true
	case *ReplyLaterChangeCommand:
		payload, err := json.Marshal(replyLaterPayload{
			Ref: c.ref, Previous: c.inQueue, Description: c.description, CreatedAt: c.createdAt,
		})
		if err != nil {
			return "", nil, false
		}
		return commandTypeReplyLater, payload, true
	default:
		return "", nil, false
	}
}

// rehydrateCommand rebuilds a persisted command with fresh runtime
// dependencies. A restored MoveCommand has no in-flight completion or traced
// move: the original action finished long ago, and Undo falls back to the
// standard move pipeline.
func rehydrateCommand(cmdType string, payload []byte, ctx context.Context, undoCtx UndoContext) (Command, error) {
	switch cmdType {
	case commandTypeFlag:
		var p flagPayload
		if err := json.Unmarshal(payload, &p); err != nil {
			return nil, fmt.Errorf("decode flag command: %w", err)
		}
		cmd := NewFlagChangeCommand(ctx, undoCtx, p.AccountID, p.FolderPath, p.MessageIDs, p.UIDs, p.FlagType, p.PreviousState, p.Description)
		cmd.createdAt = p.CreatedAt
		return cmd, nil
	case commandTypeMove:
		var p movePayload
		if err := json.Unmarshal(payload, &p); err != nil {
			return nil, fmt.Errorf("decode move command: %w", err)
		}
		cmd := NewMoveCommand(undoCtx, p.AccountID, p.Identities, p.SourceFolderID, p.DestFolderID, p.Description)
		cmd.createdAt = p.CreatedAt
		return cmd, nil
	case commandTypeLater:
		var p laterPayload
		if err := json.Unmarshal(payload, &p); err != nil {
			return nil, err
		}
		laterCtx, ok := undoCtx.(LaterContext)
		if !ok {
			return nil, fmt.Errorf("undo context does not support Later")
		}
		cmd := NewLaterChangeCommand(laterCtx, p.Ref, p.Before, p.After, p.Description)
		cmd.createdAt = p.CreatedAt
		return cmd, nil
	case commandTypeReplyLater:
		var p replyLaterPayload
		if err := json.Unmarshal(payload, &p); err != nil {
			return nil, fmt.Errorf("decode Reply Later command: %w", err)
		}
		ctx, ok := undoCtx.(ReplyLaterContext)
		if !ok {
			return nil, fmt.Errorf("undo context does not support Reply Later")
		}
		cmd := NewReplyLaterChangeCommand(ctx, p.Ref, p.Previous, p.Description)
		cmd.createdAt = p.CreatedAt
		return cmd, nil
	default:
		return nil, fmt.Errorf("unknown durable command type %q", cmdType)
	}
}

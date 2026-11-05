package app

import (
	"fmt"

	agentapi "github.com/hkdb/aerion/internal/automation/v1"
	"github.com/hkdb/aerion/internal/replylater"
	"github.com/hkdb/aerion/internal/undo"
)

// newReplyLaterAutomationAPI builds the transport-independent API adapter for
// an already authenticated automation caller. The local API transport owns
// authentication and capability grants; this method never creates a listener.
func (a *App) newReplyLaterAutomationAPI(authorizer agentapi.Authorizer) *agentapi.ReplyLaterAdapter {
	return agentapi.NewReplyLaterAdapter(replyLaterAutomationOperations{app: a}, authorizer)
}

type replyLaterAutomationOperations struct{ app *App }

var _ agentapi.ReplyLaterOperations = replyLaterAutomationOperations{}

func (ops replyLaterAutomationOperations) GetReplyLaterQueue() ([]replylater.Ref, error) {
	return ops.app.GetReplyLaterQueue()
}

func (ops replyLaterAutomationOperations) ChangeReplyLaterWithUndo(accountID, conversationID string, inQueue bool) (bool, string, error) {
	result, err := ops.app.ChangeReplyLaterWithUndo(accountID, conversationID, inQueue)
	return result.Changed, result.OperationID, err
}

// GetReplyLaterQueue returns explicit triage references, including references
// whose conversation is not currently present in the local message index.
func (a *App) GetReplyLaterQueue() ([]replylater.Ref, error) {
	if a.replyLaterStore == nil {
		return nil, fmt.Errorf("Reply Later queue is not ready")
	}
	return a.replyLaterStore.List()
}

// GetReplyLaterCount returns the number of explicit queued conversations.
func (a *App) GetReplyLaterCount() (int, error) {
	if a.replyLaterStore == nil {
		return 0, fmt.Errorf("Reply Later queue is not ready")
	}
	return a.replyLaterStore.Count()
}

// IsReplyLater reports explicit queue membership for an account conversation.
func (a *App) IsReplyLater(accountID, conversationID string) (bool, error) {
	if a.replyLaterStore == nil {
		return false, fmt.Errorf("Reply Later queue is not ready")
	}
	return a.replyLaterStore.Contains(replylater.Ref{AccountID: accountID, ConversationID: conversationID})
}

// ReplyLaterChangeResult exposes a targeted undo token for one queue change.
type ReplyLaterChangeResult struct {
	Changed     bool   `json:"changed"`
	OperationID string `json:"operationId"`
}

// AddReplyLater adds a conversation to the personal triage queue.
func (a *App) AddReplyLater(accountID, conversationID string) (bool, error) {
	result, err := a.ChangeReplyLaterWithUndo(accountID, conversationID, true)
	return result.Changed, err
}

// RemoveReplyLater removes a conversation without changing provider flags.
func (a *App) RemoveReplyLater(accountID, conversationID string) (bool, error) {
	result, err := a.ChangeReplyLaterWithUndo(accountID, conversationID, false)
	return result.Changed, err
}

// ChangeReplyLaterWithUndo sets explicit membership and returns the normal
// operation-scoped undo token. Duplicate calls do not create duplicate history.
func (a *App) ChangeReplyLaterWithUndo(accountID, conversationID string, inQueue bool) (ReplyLaterChangeResult, error) {
	if a.replyLaterStore == nil {
		return ReplyLaterChangeResult{}, fmt.Errorf("Reply Later queue is not ready")
	}
	ref := replylater.Ref{AccountID: accountID, ConversationID: conversationID}
	if err := a.changeReplyLaterProvider(accountID, conversationID, inQueue); err != nil {
		return ReplyLaterChangeResult{}, err
	}
	var changed bool
	var err error
	if inQueue {
		changed, err = a.replyLaterStore.Add(ref)
	} else {
		changed, err = a.replyLaterStore.Remove(ref)
	}
	if err != nil || !changed {
		return ReplyLaterChangeResult{Changed: changed}, err
	}
	description, action := "Added to Reply Later", "reply-later-add"
	if !inQueue {
		description, action = "Removed from Reply Later", "reply-later-remove"
	}
	operationID := a.undoStack.PushOperation("", undo.NewReplyLaterChangeCommand(a, ref, !inQueue, description), undo.OperationMetadata{Action: action})
	a.emitReplyLaterChanged(ref, inQueue)
	a.emitRuntimeEvent("undo:history-changed")
	return ReplyLaterChangeResult{Changed: true, OperationID: operationID}, nil
}

// SetReplyLaterMembership is the undo-context operation for restoring a
// previous explicit membership state.
func (a *App) SetReplyLaterMembership(ref replylater.Ref, inQueue bool) error {
	if a.replyLaterStore == nil {
		return fmt.Errorf("Reply Later queue is not ready")
	}
	if err := a.changeReplyLaterProvider(ref.AccountID, ref.ConversationID, inQueue); err != nil {
		return err
	}
	var changed bool
	var err error
	if inQueue {
		changed, err = a.replyLaterStore.Add(ref)
	} else {
		changed, err = a.replyLaterStore.Remove(ref)
	}
	if err != nil {
		return err
	}
	if changed {
		a.emitReplyLaterChanged(ref, inQueue)
	}
	return nil
}

func (a *App) emitReplyLaterChanged(ref replylater.Ref, inQueue bool) {
	a.emitRuntimeEvent("replyLater:changed", map[string]interface{}{
		"accountId":      ref.AccountID,
		"conversationId": ref.ConversationID,
		"inQueue":        inQueue,
	})
}

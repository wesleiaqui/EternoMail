package app

import (
	"fmt"
	"github.com/hkdb/aerion/internal/triage"
	"github.com/hkdb/aerion/internal/undo"
)

// ChangeLaterWithUndo is one logical operation across both legacy memberships.
func (a *App) ChangeLaterWithUndo(accountID, conversationID string, inQueue bool) (TriageChangeResult, error) {
	ref := triage.Ref{AccountID: accountID, ConversationID: conversationID}
	ctx := a.helperCommandContext()
	service, handled, err := a.undoService(ctx, true)
	if handled {
		if err != nil {
			return TriageChangeResult{}, err
		}
		response, err := service.setLaterWithReceipt(ctx, ref, inQueue)
		if err != nil {
			return TriageChangeResult{}, err
		}
		if response == nil {
			return TriageChangeResult{}, fmt.Errorf("mailbox service returned no Later receipt")
		}
		if response.Changed {
			a.emitLaterChanged()
		}
		return TriageChangeResult{Changed: response.Changed, OperationID: response.Receipt.OperationID}, nil
	}
	a.laterMutationMu.Lock()
	defer a.laterMutationMu.Unlock()
	if a.triageStore == nil || a.undoStack == nil {
		return TriageChangeResult{}, fmt.Errorf("Later queue is not ready")
	}
	before, err := a.triageStore.LaterState(ref)
	if err != nil {
		return TriageChangeResult{}, err
	}
	after := triage.LaterState{}
	if inQueue {
		after = before
		if !before.Read && !before.Reply {
			after.Read = true
		}
	}
	if before == after {
		return TriageChangeResult{}, nil
	}
	if err := a.setLaterMembership(ref, before, after); err != nil {
		return TriageChangeResult{}, err
	}
	description, action := "Added to Later", "read-later-add"
	if !inQueue {
		description, action = "Removed from Later", "read-later-remove"
	}
	id := a.undoStack.PushOperation("", undo.NewLaterChangeCommand(a, ref, before, after, description), undo.OperationMetadata{Action: action})
	a.emitLaterChanged()
	return TriageChangeResult{Changed: true, OperationID: id}, nil
}
func (a *App) setLaterMembership(ref triage.Ref, before, after triage.LaterState) error {
	if a.triageStore == nil {
		return fmt.Errorf("Later queue is not ready")
	}
	return a.triageStore.SetLaterState(a.helperCommandContext(), ref, before, after, func(queue string, on bool) error {
		return a.setGmailTriageLabel(ref.AccountID, ref.ConversationID, queue, on)
	})
}
func (a *App) RestoreLaterMembership(ref triage.Ref, expected, prior triage.LaterState) error {
	a.laterMutationMu.Lock()
	defer a.laterMutationMu.Unlock()
	if err := a.setLaterMembership(ref, expected, prior); err != nil {
		return err
	}
	a.emitLaterChanged()
	return nil
}
func (a *App) emitLaterChanged() {
	a.emitRuntimeEvent("readLater:refresh", map[string]interface{}{})
	a.emitRuntimeEvent("replyLater:refresh", map[string]interface{}{})
	a.emitRuntimeEvent("undo:history-changed")
}

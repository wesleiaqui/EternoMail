package alwayson

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	v1 "github.com/hkdb/aerion/internal/automation/v1"
	"github.com/hkdb/aerion/internal/replylater"
	"github.com/hkdb/aerion/internal/triage"
)

type triageActionUndoSnapshot struct {
	Version int                             `json:"version"`
	Action  v1.ActionKind                   `json:"action"`
	Entries []triageActionUndoSnapshotEntry `json:"entries"`
}

type triageActionUndoSnapshotEntry struct {
	PriorLater      *triage.LaterState `json:"priorLater,omitempty"`
	ExpectedLater   *triage.LaterState `json:"expectedLater,omitempty"`
	AccountID       string             `json:"accountId"`
	ConversationID  string             `json:"conversationId"`
	PriorInQueue    bool               `json:"priorInQueue,omitempty"`
	ExpectedInQueue bool               `json:"expectedInQueue,omitempty"`
	PriorWakeAt     string             `json:"priorWakeAt,omitempty"`
	ExpectedWakeAt  string             `json:"expectedWakeAt,omitempty"`
	PriorSnoozed    bool               `json:"priorSnoozed,omitempty"`
	ExpectedSnoozed bool               `json:"expectedSnoozed,omitempty"`
}

func (r *SyncRuntime) ApplyTriageMutation(ctx context.Context, request v1.TriageMutationRequest, reserve func([]byte) error) (bool, []byte, error) {
	if r == nil || r.replyLater == nil || r.triage == nil {
		return false, nil, errors.New("always-on triage stores are not open")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return false, nil, err
	}
	if reserve == nil {
		return false, nil, errors.New("triage mutation receipt reservation is required")
	}
	if request.AccountID == "" || request.ConversationID == "" {
		return false, nil, errors.New("account and conversation IDs are required")
	}

	r.triageMutationMu.Lock()
	defer r.triageMutationMu.Unlock()

	snapshot, changed, err := r.captureTriageMutation(ctx, request)
	if err != nil {
		return false, nil, err
	}
	if err := reserve(snapshot); err != nil {
		return false, nil, err
	}
	if !changed {
		return false, snapshot, nil
	}

	if request.UnifiedLater {
		var state triageActionUndoSnapshot
		if err := json.Unmarshal(snapshot, &state); err != nil {
			return false, nil, err
		}
		e := state.Entries[0]
		err := r.triage.SetLaterState(ctx, triage.Ref{AccountID: request.AccountID, ConversationID: request.ConversationID}, *e.PriorLater, *e.ExpectedLater, func(queue string, on bool) error {
			return r.setGmailTriageLabel(ctx, request.AccountID, request.ConversationID, queue, on)
		})
		return err == nil, snapshot, err
	}
	if err := r.applyTriageState(ctx, request); err != nil {
		return false, nil, err
	}
	return true, snapshot, nil
}

func (r *SyncRuntime) captureTriageMutation(ctx context.Context, request v1.TriageMutationRequest) ([]byte, bool, error) {
	entry := triageActionUndoSnapshotEntry{AccountID: request.AccountID, ConversationID: request.ConversationID}
	changed := false
	if request.UnifiedLater {
		ref := triage.Ref{AccountID: request.AccountID, ConversationID: request.ConversationID}
		prior, err := r.triage.LaterState(ref)
		if err != nil {
			return nil, false, err
		}
		expected := triage.LaterState{}
		if request.InQueue {
			expected = prior
			if !prior.Read && !prior.Reply {
				expected.Read = true
			}
		}
		entry.PriorLater = &prior
		entry.ExpectedLater = &expected
		payload, err := json.Marshal(triageActionUndoSnapshot{Version: 2, Action: request.Action, Entries: []triageActionUndoSnapshotEntry{entry}})
		return payload, prior != expected, err
	}
	switch request.Action {
	case v1.ActionReplyLaterAdd, v1.ActionReplyLaterRemove:
		ref := replylater.Ref{AccountID: request.AccountID, ConversationID: request.ConversationID}
		prior, err := r.replyLater.Contains(ref)
		if err != nil {
			return nil, false, err
		}
		entry.PriorInQueue = prior
		entry.ExpectedInQueue = request.InQueue
		changed = prior != request.InQueue
	case v1.ActionReadLaterAdd, v1.ActionReadLaterRemove:
		prior, err := r.triage.IsReadLater(request.AccountID, request.ConversationID)
		if err != nil {
			return nil, false, err
		}
		entry.PriorInQueue = prior
		entry.ExpectedInQueue = request.InQueue
		changed = prior != request.InQueue
	case v1.ActionSnooze:
		if !request.WakeAt.After(time.Now()) {
			return nil, false, errors.New("snooze deadline must be in the future")
		}
		prior, exists, err := r.getSnooze(request.AccountID, request.ConversationID)
		if err != nil {
			return nil, false, err
		}
		entry.PriorSnoozed = exists
		if exists {
			entry.PriorWakeAt = prior.UTC().Format(time.RFC3339Nano)
		}
		entry.ExpectedSnoozed = true
		entry.ExpectedWakeAt = request.WakeAt.UTC().Format(time.RFC3339Nano)
		changed = !exists || !prior.Equal(request.WakeAt)
	case v1.ActionUnsnooze:
		prior, exists, err := r.getSnooze(request.AccountID, request.ConversationID)
		if err != nil {
			return nil, false, err
		}
		entry.PriorSnoozed = exists
		if exists {
			entry.PriorWakeAt = prior.UTC().Format(time.RFC3339Nano)
		}
		entry.ExpectedSnoozed = false
		changed = exists
	default:
		return nil, false, fmt.Errorf("unsupported triage action %q", request.Action)
	}
	snapshot, err := json.Marshal(triageActionUndoSnapshot{Version: 1, Action: request.Action, Entries: []triageActionUndoSnapshotEntry{entry}})
	if err != nil {
		return nil, false, fmt.Errorf("encode triage undo snapshot: %w", err)
	}
	return snapshot, changed, nil
}

func (r *SyncRuntime) applyTriageState(ctx context.Context, request v1.TriageMutationRequest) error {
	switch request.Action {
	case v1.ActionReplyLaterAdd, v1.ActionReplyLaterRemove:
		if err := r.setGmailTriageLabel(ctx, request.AccountID, request.ConversationID, "reply-later", request.InQueue); err != nil {
			return fmt.Errorf("update Reply Later provider label: %w", err)
		}
		ref := replylater.Ref{AccountID: request.AccountID, ConversationID: request.ConversationID}
		if request.InQueue {
			_, err := r.replyLater.Add(ref)
			return err
		}
		_, err := r.replyLater.Remove(ref)
		return err
	case v1.ActionReadLaterAdd, v1.ActionReadLaterRemove:
		if err := r.setGmailTriageLabel(ctx, request.AccountID, request.ConversationID, "read-later", request.InQueue); err != nil {
			return fmt.Errorf("update Read Later provider label: %w", err)
		}
		_, err := r.triage.SetReadLater(triage.Ref{AccountID: request.AccountID, ConversationID: request.ConversationID}, request.InQueue)
		return err
	case v1.ActionSnooze:
		return r.triage.Snooze(triage.Ref{AccountID: request.AccountID, ConversationID: request.ConversationID}, request.WakeAt)
	case v1.ActionUnsnooze:
		_, err := r.triage.Unsnooze(request.AccountID, request.ConversationID)
		return err
	default:
		return fmt.Errorf("unsupported triage action %q", request.Action)
	}
}

func (r *SyncRuntime) setGmailTriageLabel(ctx context.Context, accountID, conversationID, label string, enabled bool) error {
	r.ctxMu.RLock()
	labeler := r.gmailTriage
	r.ctxMu.RUnlock()
	if labeler == nil {
		return nil
	}
	return labeler(ctx, accountID, conversationID, label, enabled)
}

func (r *SyncRuntime) getSnooze(accountID, conversationID string) (time.Time, bool, error) {
	wakeAt, exists, err := r.triage.GetSnooze(accountID, conversationID)
	if err != nil {
		return time.Time{}, false, err
	}
	if !exists || !wakeAt.After(time.Now()) {
		return time.Time{}, false, nil
	}
	return wakeAt, true, nil
}

func (o *mailboxActionOperations) ActionUndoTriageRefs(ctx context.Context, payload []byte) ([]v1.AffectedTriageRef, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var snapshot triageActionUndoSnapshot
	if err := json.Unmarshal(payload, &snapshot); err != nil {
		return nil, fmt.Errorf("decode triage undo snapshot: %w", err)
	}
	if !validTriageSnapshot(snapshot) {
		return nil, errors.New("triage undo snapshot does not match operation")
	}
	entry := snapshot.Entries[0]
	ref := v1.AffectedTriageRef{AccountID: entry.AccountID, ConversationID: entry.ConversationID, Known: true}
	if entry.PriorLater != nil && entry.ExpectedLater != nil {
		return laterAffectedRefs(entry.AccountID, entry.ConversationID, *entry.PriorLater, true), nil
	}
	switch snapshot.Action {
	case v1.ActionReplyLaterAdd, v1.ActionReplyLaterRemove:
		ref.Queue = "replyLater"
		ref.InQueue = entry.PriorInQueue
	case v1.ActionReadLaterAdd, v1.ActionReadLaterRemove:
		ref.Queue = "readLater"
		ref.InQueue = entry.PriorInQueue
	case v1.ActionSnooze, v1.ActionUnsnooze:
		ref.Queue = "snoozed"
		ref.InQueue = entry.PriorSnoozed
		ref.WakeAt = entry.PriorWakeAt
	default:
		return []v1.AffectedTriageRef{}, nil
	}
	if ref.AccountID == "" || ref.ConversationID == "" {
		return nil, errors.New("triage undo snapshot has incomplete identity")
	}
	return []v1.AffectedTriageRef{ref}, nil
}

func (o *mailboxActionOperations) undoTriageAction(ctx context.Context, action v1.ActionKind, payload []byte) error {
	r := o.runtime
	if r == nil || r.replyLater == nil || r.triage == nil {
		return errors.New("always-on triage stores are not open")
	}
	r.triageMutationMu.Lock()
	defer r.triageMutationMu.Unlock()
	var snapshot triageActionUndoSnapshot
	if err := json.Unmarshal(payload, &snapshot); err != nil {
		return fmt.Errorf("decode triage undo snapshot: %w", err)
	}
	if !validTriageSnapshot(snapshot) || snapshot.Action != action {
		return errors.New("triage undo snapshot does not match operation")
	}
	entry := snapshot.Entries[0]
	if entry.AccountID == "" || entry.ConversationID == "" {
		return errors.New("triage undo snapshot has incomplete identity")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if entry.PriorLater != nil && entry.ExpectedLater != nil {
		return r.triage.SetLaterState(ctx, triage.Ref{AccountID: entry.AccountID, ConversationID: entry.ConversationID}, *entry.ExpectedLater, *entry.PriorLater, func(queue string, on bool) error {
			return r.setGmailTriageLabel(ctx, entry.AccountID, entry.ConversationID, queue, on)
		})
	}
	switch action {
	case v1.ActionReplyLaterAdd, v1.ActionReplyLaterRemove:
		current, err := r.replyLater.Contains(replylater.Ref{AccountID: entry.AccountID, ConversationID: entry.ConversationID})
		if err != nil {
			return err
		}
		if current != entry.ExpectedInQueue {
			return errors.New("Reply Later membership changed since the operation")
		}
		if err := r.setGmailTriageLabel(ctx, entry.AccountID, entry.ConversationID, "reply-later", entry.PriorInQueue); err != nil {
			return fmt.Errorf("restore Reply Later provider label: %w", err)
		}
		if entry.PriorInQueue {
			_, err = r.replyLater.Add(replylater.Ref{AccountID: entry.AccountID, ConversationID: entry.ConversationID})
			return err
		}
		_, err = r.replyLater.Remove(replylater.Ref{AccountID: entry.AccountID, ConversationID: entry.ConversationID})
		return err
	case v1.ActionReadLaterAdd, v1.ActionReadLaterRemove:
		current, err := r.triage.IsReadLater(entry.AccountID, entry.ConversationID)
		if err != nil {
			return err
		}
		if current != entry.ExpectedInQueue {
			return errors.New("Read Later membership changed since the operation")
		}
		if err := r.setGmailTriageLabel(ctx, entry.AccountID, entry.ConversationID, "read-later", entry.PriorInQueue); err != nil {
			return fmt.Errorf("restore Read Later provider label: %w", err)
		}
		_, err = r.triage.SetReadLater(triage.Ref{AccountID: entry.AccountID, ConversationID: entry.ConversationID}, entry.PriorInQueue)
		return err
	case v1.ActionSnooze, v1.ActionUnsnooze:
		current, exists, err := r.getSnooze(entry.AccountID, entry.ConversationID)
		if err != nil {
			return err
		}
		if exists != entry.ExpectedSnoozed {
			return errors.New("Snooze state changed since the operation")
		}
		if exists {
			expected, err := time.Parse(time.RFC3339Nano, entry.ExpectedWakeAt)
			if err != nil || !current.Equal(expected) {
				return errors.New("Snooze deadline changed since the operation")
			}
		}
		if entry.PriorSnoozed {
			prior, err := time.Parse(time.RFC3339Nano, entry.PriorWakeAt)
			if err != nil || !prior.After(time.Now()) {
				return errors.New("previous Snooze deadline has expired")
			}
			return r.triage.Snooze(triage.Ref{AccountID: entry.AccountID, ConversationID: entry.ConversationID}, prior)
		}
		_, err = r.triage.Unsnooze(entry.AccountID, entry.ConversationID)
		return err
	default:
		return fmt.Errorf("undo is unsupported for action %q", action)
	}
}

var _ v1.TriageMutationOperations = (*SyncRuntime)(nil)
var _ v1.ActionUndoTriageRefReader = (*mailboxActionOperations)(nil)

func laterAffectedRefs(accountID, conversationID string, state triage.LaterState, known bool) []v1.AffectedTriageRef {
	return []v1.AffectedTriageRef{{AccountID: accountID, ConversationID: conversationID, Queue: "readLater", InQueue: state.Read, Known: known}, {AccountID: accountID, ConversationID: conversationID, Queue: "replyLater", InQueue: state.Reply, Known: known}}
}

func validTriageSnapshot(s triageActionUndoSnapshot) bool {
	if len(s.Entries) != 1 {
		return false
	}
	e := s.Entries[0]
	if s.Version == 1 {
		return e.PriorLater == nil && e.ExpectedLater == nil
	}
	return s.Version == 2 && e.PriorLater != nil && e.ExpectedLater != nil && (s.Action == v1.ActionReadLaterAdd || s.Action == v1.ActionReadLaterRemove)
}

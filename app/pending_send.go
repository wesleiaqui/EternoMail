package app

// 30-second undo-send queue (user-approved design):
//
//   - Clicking send queues the message here with a dispatch time ~30s out
//     and emits "pending-send:queued" so the main window shows the
//     countdown. The composer closes as usual; the DRAFT STAYS in the
//     local store until the send actually succeeds (the normal send path
//     deletes it), which is what makes cancel = restore-trivially-safe.
//   - The frontend main window owns the timer and dispatches through the
//     identical SendMessageWithDraftGuard call a direct send would use.
//   - Cancel deletes the pending row; the draft is then reopened.
//   - An explicit app quit dispatches queued sends immediately and waits for
//     them; a failed send keeps its draft and cancels the quit request.
//   - A restart never auto-sends: ExpirePendingSends marks leftover rows
//     expired at startup and the draft simply reappears in Drafts.
//   - The countdown timer lives in the WKWebView, which macOS throttles
//     together with the rest of the app under App Nap. Each queued row
//     holds a short user-initiated activity so the dispatch fires on
//     time even when the window is hidden; the assertion ends with the
//     row (dispatch, cancel or expiry).
//
// Concurrency: QueuePendingSend rejects a second pending send for the same
// draft (Cmd-Enter pressed twice), so the safety window can never be
// bypassed by re-sending. Dispatch-now (Cmd-Shift-Enter) only shortens the
// wait for the EXISTING row.

import (
	"fmt"
	"time"

	"github.com/google/uuid"
	wailsRuntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/hkdb/aerion/internal/ipc"
	"github.com/hkdb/aerion/internal/platform"
)

// PendingSend is the durable record of one queued send.
type PendingSend struct {
	ID         string `json:"id"`
	AccountID  string `json:"accountId"`
	DraftID    string `json:"draftId"`
	Payload    string `json:"payload"` // serialized smtp.ComposeMessage
	DispatchAt string `json:"dispatchAt"`
	State      string `json:"state"` // pending | expired
	CreatedAt  string `json:"createdAt"`
}

// PendingSendDelay is the user-approved safety window.
const PendingSendDelay = 30 * time.Second

// QueuePendingSend stores one pending send and notifies the main window.
// payload is the serialized smtp.ComposeMessage exactly as SendMessage
// would receive it. Returns the pending-send ID.
func (a *App) QueuePendingSend(accountID, draftID, payload string) (string, error) {
	if accountID == "" || payload == "" {
		return "", fmt.Errorf("pending send: account and payload are required")
	}
	id := uuid.NewString()
	now := time.Now().UTC()
	dispatchAt := now.Add(PendingSendDelay)

	tx, err := a.db.Begin()
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback() }()
	if draftID != "" {
		var n int
		if err := tx.QueryRow(
			`SELECT COUNT(*) FROM pending_sends WHERE draft_id = ? AND state = 'pending'`, draftID).Scan(&n); err != nil {
			return "", err
		}
		if n > 0 {
			return "", fmt.Errorf("pending send: this draft is already queued to send")
		}
	}
	if _, err := tx.Exec(
		`INSERT INTO pending_sends (id, account_id, draft_id, payload, dispatch_at, state, created_at)
		 VALUES (?,?,?,?,?,'pending',?)`,
		id, accountID, draftID, payload,
		dispatchAt.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano)); err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	a.holdPendingSendActivity(id)
	wailsRuntime.EventsEmit(a.ctx, "pending-send:queued", PendingSend{
		ID: id, AccountID: accountID, DraftID: draftID, Payload: payload,
		DispatchAt: dispatchAt.Format(time.RFC3339Nano), State: "pending",
		CreatedAt: now.Format(time.RFC3339Nano),
	})
	return id, nil
}

// holdPendingSendActivity keeps the process out of App Nap until the row
// dispatches, so the frontend-owned countdown timer is not coalesced by
// the OS into firing late.
func (a *App) holdPendingSendActivity(id string) {
	if a.activities == nil {
		return
	}
	act := a.activities.BeginUserInitiated("Sending email after Undo Send countdown")
	a.pendingSendActivitiesMu.Lock()
	if old := a.pendingSendActivities[id]; old != nil {
		old.End()
	}
	a.pendingSendActivities[id] = act
	a.pendingSendActivitiesMu.Unlock()
}

// releasePendingSendActivity ends the row's countdown assertion, if any.
func (a *App) releasePendingSendActivity(id string) {
	a.pendingSendActivitiesMu.Lock()
	act := a.pendingSendActivities[id]
	delete(a.pendingSendActivities, id)
	a.pendingSendActivitiesMu.Unlock()
	if act != nil {
		act.End()
	}
}

// releaseAllPendingSendActivities ends every countdown assertion (startup
// expiry and shutdown).
func (a *App) releaseAllPendingSendActivities() {
	a.pendingSendActivitiesMu.Lock()
	acts := a.pendingSendActivities
	a.pendingSendActivities = make(map[string]*platform.Activity)
	a.pendingSendActivitiesMu.Unlock()
	for _, act := range acts {
		act.End()
	}
}

// CancelPendingSend removes a pending send that has not dispatched. The
// frontend only calls this before its timer fires; after dispatch begins
// the row is already gone and cancel reports an error the UI must show
// truthfully ("already sending").
func (a *App) CancelPendingSend(id string) error {
	res, err := a.db.Exec(`DELETE FROM pending_sends WHERE id = ? AND state = 'pending'`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("pending send: %q is not pending (already sent or cancelled)", id)
	}
	wailsRuntime.EventsEmit(a.ctx, "pending-send:cancelled", id)
	return nil
}

// CompletePendingSend removes the row after a successful dispatch. Called
// by the frontend only after the send call returned without error.
func (a *App) CompletePendingSend(id string) error {
	a.releasePendingSendActivity(id)
	_, err := a.db.Exec(`DELETE FROM pending_sends WHERE id = ?`, id)
	return err
}

// ListPendingSends returns all pending rows (main-window recovery on focus
// or startup before expiry).
func (a *App) ListPendingSends() ([]*PendingSend, error) {
	rows, err := a.db.Query(
		`SELECT id, account_id, draft_id, payload, dispatch_at, state, created_at
		 FROM pending_sends WHERE state = 'pending' ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*PendingSend{}
	for rows.Next() {
		p := &PendingSend{}
		if err := rows.Scan(&p.ID, &p.AccountID, &p.DraftID, &p.Payload, &p.DispatchAt, &p.State, &p.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// ExpirePendingSends marks every leftover pending row expired. Called at
// startup: a restart NEVER auto-sends; the draft reappears in Drafts.
func (a *App) ExpirePendingSends() error {
	a.releaseAllPendingSendActivities()
	_, err := a.db.Exec(`UPDATE pending_sends SET state = 'expired' WHERE state = 'pending'`)
	return err
}

// handlePendingSendQueued relays a detached composer's queued send to the
// main window so its countdown bar can track it. The row is already in the
// shared database; the main window reads it via ListPendingSends.
func (a *App) handlePendingSendQueued(p ipc.PendingSendQueuedPayload) {
	row, err := a.db.Query(
		`SELECT id, account_id, draft_id, payload, dispatch_at, state, created_at
		 FROM pending_sends WHERE id = ? AND state = 'pending'`, p.ID)
	if err != nil {
		return
	}
	defer row.Close()
	if row.Next() {
		ps := &PendingSend{}
		if err := row.Scan(&ps.ID, &ps.AccountID, &ps.DraftID, &ps.Payload, &ps.DispatchAt, &ps.State, &ps.CreatedAt); err != nil {
			return
		}
		wailsRuntime.EventsEmit(a.ctx, "pending-send:queued", ps)
	}
}

// QueuePendingSend is the detached composer's twin of App.QueuePendingSend:
// it writes the same shared table and notifies the main window over IPC,
// which relays it to the countdown UI. The countdown and dispatch still
// happen in the main process, so closing the composer window mid-countdown
// is safe.
func (c *ComposerApp) QueuePendingSend(accountID, draftID, payload string) (string, error) {
	if accountID == "" || payload == "" {
		return "", fmt.Errorf("pending send: account and payload are required")
	}
	id := uuid.NewString()
	now := time.Now().UTC()
	dispatchAt := now.Add(PendingSendDelay)

	tx, err := c.db.Begin()
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback() }()
	if draftID != "" {
		var n int
		if err := tx.QueryRow(
			`SELECT COUNT(*) FROM pending_sends WHERE draft_id = ? AND state = 'pending'`, draftID).Scan(&n); err != nil {
			return "", err
		}
		if n > 0 {
			return "", fmt.Errorf("pending send: this draft is already queued to send")
		}
	}
	if _, err := tx.Exec(
		`INSERT INTO pending_sends (id, account_id, draft_id, payload, dispatch_at, state, created_at)
		 VALUES (?,?,?,?,?,'pending',?)`,
		id, accountID, draftID, payload,
		dispatchAt.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano)); err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	if c.ipcClient != nil {
		msg, err := ipc.NewMessage(ipc.TypePendingSendQueued, ipc.PendingSendQueuedPayload{
			ID: id, AccountID: accountID, DraftID: draftID,
			DispatchAt: dispatchAt.Format(time.RFC3339Nano),
		})
		if err == nil {
			_ = c.ipcClient.Send(msg)
		}
	}
	return id, nil
}

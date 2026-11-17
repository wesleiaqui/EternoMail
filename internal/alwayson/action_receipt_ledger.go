package alwayson

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	v1 "github.com/hkdb/aerion/internal/automation/v1"
	"github.com/hkdb/aerion/internal/database"
	"github.com/hkdb/aerion/internal/triage"
)

type actionReceiptLedger struct{ db *database.DB }

func newActionReceiptLedger(db *database.DB) *actionReceiptLedger {
	return &actionReceiptLedger{db: db}
}

func (l *actionReceiptLedger) digestKey(key string, fingerprint func([]byte) []byte) string {
	if fingerprint != nil {
		return hex.EncodeToString(fingerprint([]byte(key)))
	}
	digest := sha256.Sum256([]byte(key))
	return hex.EncodeToString(digest[:])
}

func (l *actionReceiptLedger) RecoverInterrupted(ctx context.Context) error {
	if l == nil || l.db == nil {
		return errors.New("action receipt database is unavailable")
	}
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin interrupted mail action recovery: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // A committed transaction has nothing left to roll back.
	rows, err := tx.QueryContext(ctx, `SELECT operation_id, affected_messages_json, affected_triage_json FROM mail_action_operations WHERE state = 'processing'`)
	if err != nil {
		return fmt.Errorf("list interrupted mail actions: %w", err)
	}
	var interrupted []struct {
		operationID      string
		affectedMessages []v1.AffectedMessageRef
		affected         []v1.AffectedTriageRef
	}
	for rows.Next() {
		var operationID string
		var messagesPayload, triagePayload []byte
		if err := rows.Scan(&operationID, &messagesPayload, &triagePayload); err != nil {
			rows.Close()
			return err
		}
		var affectedMessages []v1.AffectedMessageRef
		if err := json.Unmarshal(messagesPayload, &affectedMessages); err != nil {
			rows.Close()
			return err
		}
		var affected []v1.AffectedTriageRef
		if err := json.Unmarshal(triagePayload, &affected); err != nil {
			rows.Close()
			return err
		}
		for i := range affected {
			affected[i].Known = false
		}
		interrupted = append(interrupted, struct {
			operationID      string
			affectedMessages []v1.AffectedMessageRef
			affected         []v1.AffectedTriageRef
		}{operationID, affectedMessages, affected})
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, item := range interrupted {
		groups, err := loadActionGroupOutcomes(ctx, tx, item.operationID)
		if err != nil {
			return fmt.Errorf("load interrupted provider groups: %w", err)
		}
		if len(groups) > 0 {
			if _, err := tx.ExecContext(ctx, `UPDATE mail_action_groups SET state='outcome_unknown', undo_payload=NULL, updated_at=datetime('now') WHERE operation_id=? AND state='processing'`, item.operationID); err != nil {
				return fmt.Errorf("mark interrupted provider groups uncertain: %w", err)
			}
			if _, err := tx.ExecContext(ctx, `UPDATE mail_action_groups SET state='not_started', undo_payload=NULL, updated_at=datetime('now') WHERE operation_id=? AND state='planned'`, item.operationID); err != nil {
				return fmt.Errorf("mark unstarted provider groups: %w", err)
			}
			groups, err = loadActionGroupOutcomes(ctx, tx, item.operationID)
			if err != nil {
				return fmt.Errorf("reload interrupted provider groups: %w", err)
			}
			payloads, err := loadConfirmedActionGroupUndoPayloads(ctx, tx, item.operationID)
			if err != nil {
				return fmt.Errorf("load confirmed provider group Undo state: %w", err)
			}
			undoPayload, err := combineActionUndoSnapshots(payloads)
			if err != nil {
				return fmt.Errorf("combine confirmed provider group Undo state: %w", err)
			}
			var expiresAt string
			if len(undoPayload) > 0 {
				if err := tx.QueryRowContext(ctx, `SELECT strftime('%Y-%m-%dT%H:%M:%SZ', created_at, '+24 hours')
					FROM mail_action_operations WHERE operation_id=? AND datetime(created_at, '+24 hours') > datetime('now')`, item.operationID).Scan(&expiresAt); err != nil && !errors.Is(err, sql.ErrNoRows) {
					return fmt.Errorf("load recovered Undo expiry: %w", err)
				}
			}
			undoAvailable := len(undoPayload) > 0 && expiresAt != ""
			if !undoAvailable {
				undoPayload = nil
				if _, err := tx.ExecContext(ctx, `UPDATE mail_action_groups SET undo_payload = NULL WHERE operation_id=?`, item.operationID); err != nil {
					return fmt.Errorf("discard unavailable provider group Undo state: %w", err)
				}
			}
			undoState := "unavailable"
			var undoExpiry any
			if undoAvailable {
				undoState = "available"
				undoExpiry = expiresAt
			}
			hasConfirmed, hasUnconfirmed := false, false
			for _, group := range groups {
				if group.State == v1.ActionGroupConfirmed {
					hasConfirmed = true
					continue
				}
				hasUnconfirmed = true
			}
			actionState := v1.OperationUnknown
			eventState := "outcome_unknown"
			if hasConfirmed && !hasUnconfirmed {
				actionState = v1.OperationSent
				eventState = "sent"
			}
			response := v1.ActionResponse{
				Receipt:       v1.OperationReceipt{OperationID: item.operationID, State: actionState},
				MessageIDs:    []string{},
				Affected:      item.affectedMessages,
				Partial:       hasConfirmed && hasUnconfirmed,
				Groups:        groups,
				UndoAvailable: undoAvailable,
				UndoExpiresAt: expiresAt,
			}
			response.Affected, err = loadActionGroupDestinationRefs(ctx, tx, item.operationID, response.Affected)
			if err != nil {
				return fmt.Errorf("load provider group reconciliation refs: %w", err)
			}
			responseJSON, err := json.Marshal(response)
			if err != nil {
				return fmt.Errorf("encode recovered provider group receipt: %w", err)
			}
			if _, err := tx.ExecContext(ctx, `UPDATE mail_action_operations SET state=?, response_status=200, response_json=?,
				undo_payload=?, undo_state=?, undo_expires_at=?, undo_message_count=?, updated_at=datetime('now')
				WHERE operation_id=? AND state='processing'`, eventState, responseJSON, undoPayload, undoState, undoExpiry, undoMessageCount(undoPayload), item.operationID); err != nil {
				return fmt.Errorf("recover interrupted provider group action: %w", err)
			}
			if err := insertActionEventWithGroups(ctx, tx, item.operationID, eventState, response.Affected, item.affected, groups, response.Partial, response.UndoAvailable); err != nil {
				return fmt.Errorf("record interrupted provider group event: %w", err)
			}
			continue
		}
		if _, err := tx.ExecContext(ctx, `UPDATE mail_action_operations SET state='outcome_unknown', updated_at=datetime('now') WHERE operation_id=? AND state='processing'`, item.operationID); err != nil {
			return fmt.Errorf("recover interrupted mail action: %w", err)
		}
		if err := insertActionEvent(ctx, tx, item.operationID, "outcome_unknown", item.affectedMessages, item.affected); err != nil {
			return fmt.Errorf("record interrupted mail action event: %w", err)
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE mail_action_operations
		SET undo_state = 'outcome_unknown', updated_at = datetime('now') WHERE undo_state = 'undoing'`); err != nil {
		return fmt.Errorf("recover interrupted mail undo: %w", err)
	}
	var interruptedUndos []struct {
		operationID string
		payload     []byte
		affected    []v1.AffectedTriageRef
	}
	undoRows, err := tx.QueryContext(ctx, `SELECT operation_id, undo_payload, affected_triage_json FROM mail_action_operations WHERE undo_state = 'outcome_unknown' AND operation_id IN (SELECT operation_id FROM mail_action_undo_requests WHERE state = 'reserved')`)
	if err != nil {
		return fmt.Errorf("list interrupted mail undos: %w", err)
	}
	for undoRows.Next() {
		var operationID string
		var payload, affectedJSON []byte
		if err := undoRows.Scan(&operationID, &payload, &affectedJSON); err != nil {
			undoRows.Close()
			return err
		}
		var affected []v1.AffectedTriageRef
		if err := json.Unmarshal(affectedJSON, &affected); err != nil {
			undoRows.Close()
			return err
		}
		for i := range affected {
			affected[i].Known = false
		}
		interruptedUndos = append(interruptedUndos, struct {
			operationID string
			payload     []byte
			affected    []v1.AffectedTriageRef
		}{operationID, payload, affected})
	}
	if err := undoRows.Err(); err != nil {
		undoRows.Close()
		return err
	}
	if err := undoRows.Close(); err != nil {
		return err
	}
	for _, item := range interruptedUndos {
		if err := insertActionEvent(ctx, tx, item.operationID, "outcome_unknown", nil, item.affected); err != nil {
			return fmt.Errorf("record interrupted mail undo event: %w", err)
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE mail_action_undo_requests SET state = 'outcome_unknown', updated_at = datetime('now')
		WHERE state = 'reserved' AND operation_id IN (SELECT operation_id FROM mail_action_operations WHERE undo_state = 'outcome_unknown')`); err != nil {
		return fmt.Errorf("recover interrupted undo receipts: %w", err)
	}
	return tx.Commit()
}

func (l *actionReceiptLedger) Lookup(ctx context.Context, keyHash string) (v1.ActionReceiptRecord, bool, error) {
	var record v1.ActionReceiptRecord
	var state string
	var affectedJSON []byte
	err := l.db.QueryRowContext(ctx, `SELECT operation_id, request_hash, state, response_status, response_json, affected_messages_json
		FROM mail_action_operations WHERE idempotency_key_hash = ?`, keyHash).
		Scan(&record.OperationID, &record.RequestHash, &state, &record.Status, &record.Response, &affectedJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return v1.ActionReceiptRecord{}, false, nil
	}
	if err != nil {
		return v1.ActionReceiptRecord{}, false, err
	}
	record.State = v1.OperationState(state)
	if err := json.Unmarshal(affectedJSON, &record.Affected); err != nil {
		return v1.ActionReceiptRecord{}, false, err
	}
	record.Groups, err = loadActionGroupOutcomes(ctx, l.db.DB, record.OperationID)
	if err != nil {
		return v1.ActionReceiptRecord{}, false, err
	}
	return record, true, nil
}

func (l *actionReceiptLedger) Reserve(ctx context.Context, keyHash, requestHash, operationID string, action v1.ActionKind, messageCount int) (v1.ActionReceiptRecord, bool, error) {
	return l.reserve(ctx, keyHash, requestHash, operationID, action, messageCount, nil, nil, nil)
}

func (l *actionReceiptLedger) ReserveTriage(ctx context.Context, keyHash, requestHash, operationID string, action v1.ActionKind, messageCount int, affected []v1.AffectedTriageRef) (v1.ActionReceiptRecord, bool, error) {
	return l.reserve(ctx, keyHash, requestHash, operationID, action, messageCount, nil, affected, nil)
}

func (l *actionReceiptLedger) ReserveAction(ctx context.Context, keyHash, requestHash, operationID string, action v1.ActionKind, messageCount int, affected []v1.AffectedMessageRef) (v1.ActionReceiptRecord, bool, error) {
	return l.reserve(ctx, keyHash, requestHash, operationID, action, messageCount, affected, nil, nil)
}

func (l *actionReceiptLedger) ReserveActionGroups(ctx context.Context, keyHash, requestHash, operationID string, action v1.ActionKind, messageCount int, affected []v1.AffectedMessageRef, groups []v1.ActionProviderGroup) (v1.ActionReceiptRecord, bool, error) {
	if len(groups) == 0 {
		return v1.ActionReceiptRecord{}, false, errors.New("action provider groups are required")
	}
	return l.reserve(ctx, keyHash, requestHash, operationID, action, messageCount, affected, nil, groups)
}

func (l *actionReceiptLedger) reserve(ctx context.Context, keyHash, requestHash, operationID string, action v1.ActionKind, messageCount int, affectedMessages []v1.AffectedMessageRef, affectedTriage []v1.AffectedTriageRef, groups []v1.ActionProviderGroup) (v1.ActionReceiptRecord, bool, error) {
	if err := ctx.Err(); err != nil {
		return v1.ActionReceiptRecord{}, false, err
	}
	if affectedMessages == nil {
		affectedMessages = []v1.AffectedMessageRef{}
	}
	if affectedTriage == nil {
		affectedTriage = []v1.AffectedTriageRef{}
	}
	affectedJSON, err := json.Marshal(affectedMessages)
	if err != nil {
		return v1.ActionReceiptRecord{}, false, err
	}
	affectedTriageJSON, err := json.Marshal(affectedTriage)
	if err != nil {
		return v1.ActionReceiptRecord{}, false, err
	}
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return v1.ActionReceiptRecord{}, false, err
	}
	defer func() { _ = tx.Rollback() }() // A committed transaction has nothing left to roll back.
	_, err = tx.ExecContext(ctx, `INSERT INTO mail_action_operations
		(idempotency_key_hash, request_hash, operation_id, state, action_kind, undo_message_count, affected_messages_json, affected_triage_json)
		VALUES (?, ?, ?, 'processing', ?, ?, ?, ?) ON CONFLICT(idempotency_key_hash) DO NOTHING`, keyHash, requestHash, operationID, action, messageCount, affectedJSON, affectedTriageJSON)
	if err != nil {
		return v1.ActionReceiptRecord{}, false, err
	}
	var record v1.ActionReceiptRecord
	var state string
	var storedAffectedJSON []byte
	err = tx.QueryRowContext(ctx, `SELECT operation_id, request_hash, state, response_status, response_json, affected_messages_json
		FROM mail_action_operations WHERE idempotency_key_hash = ?`, keyHash).
		Scan(&record.OperationID, &record.RequestHash, &state, &record.Status, &record.Response, &storedAffectedJSON)
	if err != nil {
		return v1.ActionReceiptRecord{}, false, err
	}
	if err := json.Unmarshal(storedAffectedJSON, &record.Affected); err != nil {
		return v1.ActionReceiptRecord{}, false, err
	}
	if record.RequestHash != requestHash {
		return record, false, v1.ErrActionReceiptConflict
	}
	record.State = v1.OperationState(state)
	owner := record.OperationID == operationID && record.State == "processing"
	if owner {
		for _, group := range groups {
			if group.ID == "" || group.AccountID == "" || group.SourceFolderID == "" || len(group.MessageIDs) == 0 {
				return v1.ActionReceiptRecord{}, false, errors.New("action provider group has incomplete identity")
			}
			messageIDsJSON, err := json.Marshal(group.MessageIDs)
			if err != nil {
				return v1.ActionReceiptRecord{}, false, err
			}
			_, err = tx.ExecContext(ctx, `INSERT INTO mail_action_groups
				(operation_id, group_id, account_id, source_folder_id, destination_account_id, destination_folder_id, message_ids_json, undo_payload, state)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'planned')`, record.OperationID, group.ID, group.AccountID, group.SourceFolderID, group.DestinationAccountID, group.DestinationFolderID, messageIDsJSON, group.UndoPayload)
			if err != nil {
				return v1.ActionReceiptRecord{}, false, err
			}
		}
	}
	record.Groups, err = loadActionGroupOutcomes(ctx, tx, record.OperationID)
	if err != nil {
		return v1.ActionReceiptRecord{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return v1.ActionReceiptRecord{}, false, err
	}
	return record, owner, nil
}

func (l *actionReceiptLedger) SetActionGroupState(ctx context.Context, operationID, groupID, state string) error {
	if state != v1.ActionGroupProcessing && state != v1.ActionGroupConfirmed && state != v1.ActionGroupOutcomeUnknown {
		return fmt.Errorf("invalid action provider group state %q", state)
	}
	from := v1.ActionGroupPlanned
	if state != v1.ActionGroupProcessing {
		from = v1.ActionGroupProcessing
	}
	result, err := l.db.ExecContext(ctx, `UPDATE mail_action_groups SET state = ?, undo_payload = CASE WHEN ? = 'outcome_unknown' THEN NULL ELSE undo_payload END, updated_at = datetime('now')
		WHERE operation_id = ? AND group_id = ? AND state = ?`, state, state, operationID, groupID, from)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows != 1 {
		return errors.New("action provider group is no longer in the expected state")
	}
	return nil
}

type actionGroupQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func loadActionGroupOutcomes(ctx context.Context, queryer actionGroupQueryer, operationID string) ([]v1.ActionGroupOutcome, error) {
	rows, err := queryer.QueryContext(ctx, `SELECT group_id, account_id, source_folder_id, destination_account_id, destination_folder_id, state, message_ids_json
		FROM mail_action_groups WHERE operation_id = ? ORDER BY account_id, source_folder_id, group_id`, operationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var outcomes []v1.ActionGroupOutcome
	for rows.Next() {
		var outcome v1.ActionGroupOutcome
		var idsJSON []byte
		if err := rows.Scan(&outcome.ID, &outcome.AccountID, &outcome.SourceFolderID, &outcome.DestinationAccountID, &outcome.DestinationFolderID, &outcome.State, &idsJSON); err != nil {
			return nil, err
		}
		var ids []string
		if err := json.Unmarshal(idsJSON, &ids); err != nil {
			return nil, err
		}
		outcome.MessageCount = len(ids)
		outcomes = append(outcomes, outcome)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return outcomes, nil
}

func loadConfirmedActionGroupUndoPayloads(ctx context.Context, queryer actionGroupQueryer, operationID string) ([][]byte, error) {
	rows, err := queryer.QueryContext(ctx, `SELECT undo_payload FROM mail_action_groups
		WHERE operation_id = ? AND state = 'confirmed' AND length(undo_payload) > 0
		ORDER BY account_id, source_folder_id, group_id`, operationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var payloads [][]byte
	for rows.Next() {
		var payload []byte
		if err := rows.Scan(&payload); err != nil {
			return nil, err
		}
		payloads = append(payloads, payload)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return payloads, nil
}

func loadActionGroupDestinationRefs(ctx context.Context, queryer actionGroupQueryer, operationID string, refs []v1.AffectedMessageRef) ([]v1.AffectedMessageRef, error) {
	rows, err := queryer.QueryContext(ctx, `SELECT destination_account_id, destination_folder_id, message_ids_json
		FROM mail_action_groups WHERE operation_id = ? AND destination_account_id != '' AND destination_folder_id != ''`, operationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	seen := make(map[v1.AffectedMessageRef]struct{}, len(refs))
	unique := make([]v1.AffectedMessageRef, 0, len(refs))
	for _, ref := range refs {
		if _, exists := seen[ref]; exists {
			continue
		}
		seen[ref] = struct{}{}
		unique = append(unique, ref)
	}
	for rows.Next() {
		var accountID, folderID string
		var idsJSON []byte
		if err := rows.Scan(&accountID, &folderID, &idsJSON); err != nil {
			return nil, err
		}
		var ids []string
		if err := json.Unmarshal(idsJSON, &ids); err != nil {
			return nil, err
		}
		for _, messageID := range ids {
			ref := v1.AffectedMessageRef{AccountID: accountID, MessageID: messageID, FolderID: folderID}
			if _, exists := seen[ref]; exists {
				continue
			}
			seen[ref] = struct{}{}
			unique = append(unique, ref)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return unique, nil
}

func (l *actionReceiptLedger) Complete(ctx context.Context, keyHash, requestHash string, action v1.ActionKind, state v1.OperationState, status int, response, undoPayload []byte) error {
	if state != v1.OperationSent && state != v1.OperationUnknown {
		return fmt.Errorf("invalid terminal action state %q", state)
	}
	var responseData v1.ActionResponse
	if err := json.Unmarshal(response, &responseData); err != nil {
		return fmt.Errorf("decode action response for event: %w", err)
	}
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }() // A committed transaction has nothing left to roll back.
	result, err := tx.ExecContext(ctx, `UPDATE mail_action_operations
		SET state = ?, response_status = ?, response_json = ?, action_kind = ?,
			undo_payload = ?, undo_state = CASE WHEN length(?) > 0 AND ? THEN 'available' ELSE 'unavailable' END,
			undo_expires_at = CASE WHEN length(?) > 0 AND ? THEN datetime('now', '+24 hours') ELSE NULL END,
			undo_message_count = CASE WHEN length(?) > 0 AND ? THEN ? ELSE undo_message_count END,
			updated_at = datetime('now')
		WHERE idempotency_key_hash = ? AND request_hash = ? AND state = 'processing'`,
		state, status, response, action, undoPayload, undoPayload, responseData.UndoAvailable, undoPayload, responseData.UndoAvailable,
		undoPayload, responseData.UndoAvailable, undoMessageCount(undoPayload), keyHash, requestHash)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows != 1 {
		return errors.New("action receipt is no longer processing")
	}
	if action == "" {
		return tx.Commit()
	}
	if err := insertActionEventWithGroups(ctx, tx, responseData.Receipt.OperationID, string(state), responseData.Affected, responseData.AffectedTriage, responseData.Groups, responseData.Partial, responseData.UndoAvailable); err != nil {
		return err
	}
	return tx.Commit()
}

func insertActionEvent(ctx context.Context, tx *sql.Tx, operationID, state string, affected []v1.AffectedMessageRef, affectedTriage []v1.AffectedTriageRef) error {
	return insertActionEventWithGroups(ctx, tx, operationID, state, affected, affectedTriage, nil, false, false)
}

func insertActionEventWithGroups(ctx context.Context, tx *sql.Tx, operationID, state string, affected []v1.AffectedMessageRef, affectedTriage []v1.AffectedTriageRef, groups []v1.ActionGroupOutcome, partial, undoAvailable bool) error {
	if state != "sent" && state != "undone" && state != "outcome_unknown" {
		return fmt.Errorf("invalid action event state %q", state)
	}
	if affected == nil {
		affected = []v1.AffectedMessageRef{}
	}
	payload, err := json.Marshal(affected)
	if err != nil {
		return fmt.Errorf("encode affected action references: %w", err)
	}
	if affectedTriage == nil {
		affectedTriage = []v1.AffectedTriageRef{}
	}
	triagePayload, err := json.Marshal(affectedTriage)
	if err != nil {
		return fmt.Errorf("encode affected triage references: %w", err)
	}
	if groups == nil {
		groups = []v1.ActionGroupOutcome{}
	}
	groupsPayload, err := json.Marshal(groups)
	if err != nil {
		return fmt.Errorf("encode action provider group outcomes: %w", err)
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO mail_action_events (operation_id, state, affected_json, affected_triage_json, affected_groups_json, partial, undo_available)
		VALUES (?, ?, ?, ?, ?, ?, ?) ON CONFLICT(operation_id, state) DO NOTHING`, operationID, state, payload, triagePayload, groupsPayload, partial, undoAvailable)
	return err
}

func (l *actionReceiptLedger) ListActionEvents(ctx context.Context, clientID string, limit int) (v1.ActionEventPage, error) {
	if clientID == "" || limit < 1 || limit > 100 {
		return v1.ActionEventPage{}, errors.New("client ID and bounded event limit are required")
	}
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return v1.ActionEventPage{}, err
	}
	defer func() { _ = tx.Rollback() }() // A committed transaction has nothing left to roll back.
	if _, err := tx.ExecContext(ctx, `DELETE FROM mail_action_events WHERE created_at < datetime('now', '-90 days')
		AND sequence < (SELECT COALESCE(MAX(sequence),0) FROM mail_action_events)`); err != nil {
		return v1.ActionEventPage{}, err
	}
	var cursor int64
	err = tx.QueryRowContext(ctx, `SELECT last_sequence FROM mail_action_event_cursors WHERE client_id=?`, clientID).Scan(&cursor)
	if errors.Is(err, sql.ErrNoRows) {
		err = nil
	}
	if err != nil {
		return v1.ActionEventPage{}, err
	}
	var oldest, newest sql.NullInt64
	if err := tx.QueryRowContext(ctx, `SELECT MIN(sequence), MAX(sequence) FROM mail_action_events`).Scan(&oldest, &newest); err != nil {
		return v1.ActionEventPage{}, err
	}
	page := v1.ActionEventPage{Events: []v1.ActionEvent{}, Cursor: cursor}
	if oldest.Valid && cursor < oldest.Int64-1 {
		page.ResetRequired = true
		page.Cursor = newest.Int64
		return page, tx.Commit()
	}
	rows, err := tx.QueryContext(ctx, `SELECT sequence, operation_id, state, affected_json, affected_triage_json, affected_groups_json, partial, undo_available, created_at
		FROM mail_action_events WHERE sequence > ? ORDER BY sequence LIMIT ?`, cursor, limit)
	if err != nil {
		return v1.ActionEventPage{}, err
	}
	for rows.Next() {
		var item v1.ActionEvent
		var affected, affectedTriage, affectedGroups []byte
		if err := rows.Scan(&item.Sequence, &item.OperationID, &item.State, &affected, &affectedTriage, &affectedGroups, &item.Partial, &item.UndoAvailable, &item.CreatedAt); err != nil {
			rows.Close()
			return v1.ActionEventPage{}, err
		}
		if err := json.Unmarshal(affected, &item.Affected); err != nil {
			rows.Close()
			return v1.ActionEventPage{}, err
		}
		if err := json.Unmarshal(affectedTriage, &item.AffectedTriage); err != nil {
			rows.Close()
			return v1.ActionEventPage{}, err
		}
		if err := json.Unmarshal(affectedGroups, &item.Groups); err != nil {
			rows.Close()
			return v1.ActionEventPage{}, err
		}
		page.Events = append(page.Events, item)
		page.Cursor = item.Sequence
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return v1.ActionEventPage{}, err
	}
	if err := rows.Close(); err != nil {
		return v1.ActionEventPage{}, err
	}
	return page, tx.Commit()
}

func (l *actionReceiptLedger) AcknowledgeActionEvents(ctx context.Context, clientID string, sequence int64) error {
	if clientID == "" || sequence < 0 {
		return errors.New("client ID and non-negative sequence are required")
	}
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }() // A committed transaction has nothing left to roll back.
	var current int64
	err = tx.QueryRowContext(ctx, `SELECT last_sequence FROM mail_action_event_cursors WHERE client_id=?`, clientID).Scan(&current)
	if errors.Is(err, sql.ErrNoRows) {
		err = nil
	}
	if err != nil {
		return err
	}
	if sequence < current {
		return errors.New("action-event cursor cannot move backwards")
	}
	var maximum int64
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(sequence),0) FROM mail_action_events`).Scan(&maximum); err != nil {
		return err
	}
	if sequence > maximum {
		return errors.New("action-event cursor exceeds latest sequence")
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO mail_action_event_cursors(client_id,last_sequence) VALUES(?,?)
		ON CONFLICT(client_id) DO UPDATE SET last_sequence=excluded.last_sequence, updated_at=datetime('now')`, clientID, sequence); err != nil {
		return err
	}
	return tx.Commit()
}

func undoMessageCount(payload []byte) int {
	var snapshot struct {
		Entries []json.RawMessage `json:"entries"`
	}
	if len(payload) == 0 || json.Unmarshal(payload, &snapshot) != nil {
		return 0
	}
	return len(snapshot.Entries)
}

func actionUndoTriageRefs(payload []byte, known bool) ([]v1.AffectedTriageRef, error) {
	if len(payload) == 0 {
		return []v1.AffectedTriageRef{}, nil
	}
	var snapshot struct {
		Action  v1.ActionKind `json:"action"`
		Entries []struct {
			AccountID      string             `json:"accountId"`
			ConversationID string             `json:"conversationId"`
			PriorLater     *triage.LaterState `json:"priorLater,omitempty"`
			PriorInQueue   bool               `json:"priorInQueue"`
			PriorWakeAt    string             `json:"priorWakeAt"`
			PriorSnoozed   bool               `json:"priorSnoozed"`
		} `json:"entries"`
	}
	if err := json.Unmarshal(payload, &snapshot); err != nil {
		return nil, err
	}
	if len(snapshot.Entries) == 0 {
		return []v1.AffectedTriageRef{}, nil
	}
	entry := snapshot.Entries[0]
	ref := v1.AffectedTriageRef{AccountID: entry.AccountID, ConversationID: entry.ConversationID, Known: known}
	if entry.PriorLater != nil {
		return laterAffectedRefs(entry.AccountID, entry.ConversationID, *entry.PriorLater, known), nil
	}
	switch snapshot.Action {
	case v1.ActionReplyLaterAdd, v1.ActionReplyLaterRemove:
		ref.Queue, ref.InQueue = "replyLater", entry.PriorInQueue
	case v1.ActionReadLaterAdd, v1.ActionReadLaterRemove:
		ref.Queue, ref.InQueue = "readLater", entry.PriorInQueue
	case v1.ActionSnooze, v1.ActionUnsnooze:
		ref.Queue, ref.InQueue, ref.WakeAt = "snoozed", entry.PriorSnoozed, entry.PriorWakeAt
	default:
		return []v1.AffectedTriageRef{}, nil
	}
	if ref.AccountID == "" || ref.ConversationID == "" {
		return nil, errors.New("triage undo snapshot has incomplete identity")
	}
	return []v1.AffectedTriageRef{ref}, nil
}

func (l *actionReceiptLedger) ListUndoHistory(ctx context.Context, limit int) ([]v1.ActionUndoSummary, error) {
	if err := l.ExpireUndoSnapshots(ctx); err != nil {
		return nil, err
	}
	rows, err := l.db.QueryContext(ctx, `SELECT operation_id, action_kind, created_at, COALESCE(undo_expires_at,''),
		undo_message_count, state, undo_state FROM mail_action_operations
		WHERE action_kind != '' ORDER BY created_at DESC, operation_id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]v1.ActionUndoSummary, 0, limit)
	for rows.Next() {
		var item v1.ActionUndoSummary
		if err := rows.Scan(&item.OperationID, &item.Action, &item.CreatedAt, &item.UndoExpiresAt, &item.MessageCount, &item.State, &item.UndoState); err != nil {
			return nil, err
		}
		item.UndoAvailable = item.UndoState == "available"
		items = append(items, item)
	}
	return items, rows.Err()
}

func (l *actionReceiptLedger) ExpireUndoSnapshots(ctx context.Context) error {
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }() // A committed transaction has nothing left to roll back.
	if _, err := tx.ExecContext(ctx, `UPDATE mail_action_operations SET undo_state = 'expired', undo_payload = NULL
		WHERE undo_state = 'available' AND undo_expires_at <= datetime('now')`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE mail_action_groups SET undo_payload = NULL
		WHERE operation_id IN (SELECT operation_id FROM mail_action_operations WHERE undo_state = 'expired')`); err != nil {
		return err
	}
	return tx.Commit()
}

func (l *actionReceiptLedger) ClaimUndo(ctx context.Context, operationID, keyHash, requestHash string) (v1.ActionUndoRecord, error) {
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return v1.ActionUndoRecord{}, err
	}
	defer func() { _ = tx.Rollback() }() // A committed transaction has nothing left to roll back.
	var mappedOperationID, existingRequestHash string
	err = tx.QueryRowContext(ctx, `SELECT operation_id, request_hash FROM mail_action_undo_requests WHERE idempotency_key_hash = ?`, keyHash).
		Scan(&mappedOperationID, &existingRequestHash)
	existingRequest := err == nil
	if errors.Is(err, sql.ErrNoRows) {
		err = nil
	}
	if err != nil {
		return v1.ActionUndoRecord{}, err
	}
	if existingRequest {
		if existingRequestHash != requestHash {
			return v1.ActionUndoRecord{}, v1.ErrActionUndoConflict
		}
		operationID = mappedOperationID
	}
	query := `SELECT operation_id, action_kind, state, undo_state, created_at, COALESCE(undo_expires_at,''), undo_message_count, undo_payload
		FROM mail_action_operations WHERE operation_id = ?`
	if operationID == "" {
		query = `SELECT operation_id, action_kind, state, undo_state, created_at, COALESCE(undo_expires_at,''), undo_message_count, undo_payload
			FROM mail_action_operations WHERE undo_state = 'available' AND undo_expires_at > datetime('now')
			ORDER BY created_at DESC, operation_id DESC LIMIT 1`
	}
	var record v1.ActionUndoRecord
	var operationState string
	var row *sql.Row
	if operationID == "" {
		row = tx.QueryRowContext(ctx, query)
	} else {
		row = tx.QueryRowContext(ctx, query, operationID)
	}
	err = row.Scan(&record.OperationID, &record.Action, &operationState, &record.UndoState,
		&record.CreatedAt, &record.ExpiresAt, &record.MessageCount, &record.Payload)
	if errors.Is(err, sql.ErrNoRows) {
		return v1.ActionUndoRecord{}, v1.ErrActionUndoUnavailable
	}
	if err != nil {
		return v1.ActionUndoRecord{}, err
	}
	record.State = v1.OperationState(operationState)
	if record.UndoState == "undone" || record.UndoState == "outcome_unknown" {
		if err := tx.Commit(); err != nil {
			return v1.ActionUndoRecord{}, err
		}
		return record, nil
	}
	if existingRequest {
		return v1.ActionUndoRecord{}, v1.ErrActionUndoBusy
	}
	if record.UndoState == "undoing" {
		return v1.ActionUndoRecord{}, v1.ErrActionUndoBusy
	}
	if record.UndoState != "available" || (record.State != v1.OperationSent && record.State != v1.OperationUnknown) {
		return v1.ActionUndoRecord{}, v1.ErrActionUndoUnavailable
	}
	result, err := tx.ExecContext(ctx, `UPDATE mail_action_operations SET undo_state = 'undoing', updated_at = datetime('now')
		WHERE operation_id = ? AND undo_state = 'available' AND undo_expires_at > datetime('now') AND state IN ('sent','outcome_unknown')`, record.OperationID)
	if err != nil {
		return v1.ActionUndoRecord{}, err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return v1.ActionUndoRecord{}, err
	}
	if rows != 1 {
		return v1.ActionUndoRecord{}, v1.ErrActionUndoUnavailable
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO mail_action_undo_requests
		(idempotency_key_hash, request_hash, operation_id, state) VALUES (?, ?, ?, 'reserved')`, keyHash, requestHash, record.OperationID); err != nil {
		return v1.ActionUndoRecord{}, err
	}
	record.UndoState = "undoing"
	if err := tx.Commit(); err != nil {
		return v1.ActionUndoRecord{}, err
	}
	return record, nil
}

func (l *actionReceiptLedger) CompleteUndo(ctx context.Context, operationID, state string) error {
	if state != "undone" && state != "outcome_unknown" {
		return fmt.Errorf("invalid undo state %q", state)
	}
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }() // A committed transaction has nothing left to roll back.
	var payload []byte
	if err := tx.QueryRowContext(ctx, `SELECT undo_payload FROM mail_action_operations WHERE operation_id=?`, operationID).Scan(&payload); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `UPDATE mail_action_operations
		SET undo_state = ?, undo_payload = CASE WHEN ? = 'undone' THEN NULL ELSE undo_payload END,
			updated_at = datetime('now') WHERE operation_id = ? AND undo_state = 'undoing'`, state, state, operationID)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows != 1 {
		return errors.New("action undo is no longer in progress")
	}
	if _, err = tx.ExecContext(ctx, `UPDATE mail_action_undo_requests SET state = ?, updated_at = datetime('now')
		WHERE operation_id = ? AND state = 'reserved'`, state, operationID); err != nil {
		return err
	}
	if state == "undone" {
		if _, err := tx.ExecContext(ctx, `UPDATE mail_action_groups SET undo_payload = NULL WHERE operation_id = ?`, operationID); err != nil {
			return err
		}
	}
	var affected []v1.AffectedMessageRef
	if state == "undone" {
		var snapshot struct {
			Entries []struct {
				AccountID string `json:"accountId"`
				MessageID string `json:"messageId"`
				FolderID  string `json:"folderId"`
			} `json:"entries"`
		}
		if err := json.Unmarshal(payload, &snapshot); err != nil {
			return err
		}
		for _, entry := range snapshot.Entries {
			affected = append(affected, v1.AffectedMessageRef{AccountID: entry.AccountID, MessageID: entry.MessageID, FolderID: entry.FolderID})
		}
	}
	var affectedTriage []v1.AffectedTriageRef
	if triageRefs, err := actionUndoTriageRefs(payload, state == "undone"); err != nil {
		return err
	} else {
		affectedTriage = triageRefs
	}
	if err := insertActionEvent(ctx, tx, operationID, state, affected, affectedTriage); err != nil {
		return err
	}
	return tx.Commit()
}

var _ v1.ActionReceiptStore = (*actionReceiptLedger)(nil)
var _ v1.ActionTargetReceiptStore = (*actionReceiptLedger)(nil)
var _ v1.ActionProviderGroupReceiptStore = (*actionReceiptLedger)(nil)
var _ v1.ActionUndoStore = (*actionReceiptLedger)(nil)
var _ v1.ActionEventStore = (*actionReceiptLedger)(nil)

package mailfilter

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/hkdb/aerion/internal/database"
	"github.com/hkdb/aerion/internal/message"
)

// RuleActions is the snapshot of a rule's non-move actions persisted with a
// durable job. Execution compares it with the current rule and skips jobs
// whose actions were edited, rather than combining new and old actions.
type RuleActions struct {
	ApplyExisting bool `json:"applyExisting,omitempty"`
	MarkRead      bool `json:"markRead,omitempty"`
	Archive       bool `json:"archive,omitempty"`
	Trash         bool `json:"trash,omitempty"`
}

// actionsOf extracts the action snapshot from a rule.
func actionsOf(rule *Rule) RuleActions {
	if rule == nil {
		return RuleActions{}
	}
	return RuleActions{MarkRead: rule.ActionMarkRead, Archive: rule.ActionArchive, Trash: rule.ActionTrash}
}

// FilterJob is the durable input needed to resume automatic filter actions
// that had not started before the service stopped.
type FilterJob struct {
	ID                  string
	RuleID              string
	DestinationFolderID string
	MessageID           string
	SourceAccountID     string
	SourceFolderID      string
	SourceUID           uint32
	Actions             RuleActions
}

// FilterJobInspection contains the metadata needed to reconcile an uncertain
// move without exposing message content or allowing the job to be retried.
type FilterJobInspection struct {
	ID                   string `json:"id"`
	RuleID               string `json:"ruleId"`
	MessageID            string `json:"messageId"`
	SourceAccountID      string `json:"sourceAccountId"`
	SourceFolderID       string `json:"sourceFolderId"`
	SourceUID            uint32 `json:"sourceUid"`
	DestinationAccountID string `json:"destinationAccountId,omitempty"`
	DestinationFolderID  string `json:"destinationFolderId"`
	State                string `json:"state"`
	LastError            string `json:"lastError,omitempty"`
	CreatedAt            string `json:"createdAt"`
	UpdatedAt            string `json:"updatedAt"`
}

// JobStore persists automatic filter work in the mailbox database.
type JobStore struct{ db *database.DB }

// NewJobStore creates a durable filter job store. The schema is installed by
// the database migration run before the service starts.
func NewJobStore(db *database.DB) *JobStore { return &JobStore{db: db} }

// Enqueue records a matched rule application. Duplicate sync callbacks for
// the same rule, message, source location and destination collapse to one
// job. destinationFolderID may be empty when the rule has no move action.
func (s *JobStore) Enqueue(rule *Rule, destinationFolderID string, msg *message.Message, applyExisting ...bool) error {
	return s.EnqueueBatch(rule, destinationFolderID, []*message.Message{msg}, len(applyExisting) > 0 && applyExisting[0])
}

// EnqueueBatch commits the entire explicit selection or none of it, so an HTTP
// error can never leave an undisclosed partial batch behind.
func (s *JobStore) EnqueueBatch(rule *Rule, destinationFolderID string, messages []*message.Message, applyExisting bool) error {
	if s == nil || s.db == nil || rule == nil || rule.ID == "" {
		return errors.New("automatic filter job is missing its rule")
	}
	for _, msg := range messages {
		if msg == nil || msg.ID == "" || msg.AccountID == "" || msg.FolderID == "" || msg.UID == 0 {
			return errors.New("automatic filter job is missing required message identifiers")
		}
	}
	snapshot := actionsOf(rule)
	snapshot.ApplyExisting = applyExisting
	actions, err := json.Marshal(snapshot)
	if err != nil {
		return err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if applyExisting {
		for _, msg := range messages {
			var processing int
			if err := tx.QueryRow(`SELECT COUNT(*) FROM automatic_filter_jobs WHERE rule_id=? AND message_id=? AND source_folder_id=? AND destination_folder_id=? AND state IN ('processing','outcome_unknown')`, rule.ID, msg.ID, msg.FolderID, destinationFolderID).Scan(&processing); err != nil {
				return err
			}
			if processing > 0 {
				return errors.New("filtering is in progress or has an uncertain outcome for this selection; inspect its status before applying again")
			}
		}
	}
	for _, msg := range messages {
		_, err = tx.Exec(`INSERT INTO automatic_filter_jobs
   (id,rule_id,destination_folder_id,message_id,source_account_id,source_folder_id,source_uid,state,actions)
   VALUES (?,?,?,?,?,?,?,'pending',?) ON CONFLICT(rule_id,message_id,source_folder_id,destination_folder_id) DO UPDATE SET actions=excluded.actions,source_uid=excluded.source_uid,source_account_id=excluded.source_account_id,updated_at=datetime('now') WHERE automatic_filter_jobs.state='pending' AND ?`, uuid.NewString(), rule.ID, destinationFolderID, msg.ID, msg.AccountID, msg.FolderID, msg.UID, string(actions), applyExisting)
		if err != nil {
			return fmt.Errorf("persist automatic filter job: %w", err)
		}
	}
	return tx.Commit()
}

// ClaimNext atomically changes the oldest pending job to processing.
func (s *JobStore) ClaimNext() (*FilterJob, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("begin automatic filter claim: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	job := &FilterJob{}
	var actionsJSON string
	err = tx.QueryRow(`
		SELECT id, rule_id, destination_folder_id, message_id, source_account_id, source_folder_id, source_uid, actions
		FROM automatic_filter_jobs WHERE state='pending' AND updated_at<=datetime('now') ORDER BY created_at, id LIMIT 1
	`).Scan(&job.ID, &job.RuleID, &job.DestinationFolderID, &job.MessageID, &job.SourceAccountID, &job.SourceFolderID, &job.SourceUID, &actionsJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read pending automatic filter job: %w", err)
	}
	if _, err := tx.Exec(`UPDATE automatic_filter_jobs SET state='processing', updated_at=datetime('now') WHERE id=? AND state='pending'`, job.ID); err != nil {
		return nil, fmt.Errorf("claim automatic filter job: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit automatic filter claim: %w", err)
	}
	if err := json.Unmarshal([]byte(actionsJSON), &job.Actions); err != nil {
		_ = s.MarkOutcomeUnknown(job.ID, "invalid action snapshot")
		return nil, fmt.Errorf("decode filter actions: %w", err)
	}
	return job, nil
}

// DeferUnstarted keeps an unattempted hidden-destination job pending. The
// future eligibility time avoids repeatedly claiming it in the hot queue loop;
// after Show it resumes within a minute, without losing the original snapshot.
func (s *JobStore) DeferUnstarted(id string) error {
	_, err := s.db.Exec(`UPDATE automatic_filter_jobs SET state='pending',updated_at=datetime('now','+60 seconds') WHERE id=? AND state='processing'`, id)
	return err
}

// Complete removes a successfully handled job from the live queue.
func (s *JobStore) Complete(id string) error {
	_, err := s.db.Exec(`DELETE FROM automatic_filter_jobs WHERE id=?`, id)
	if err != nil {
		return fmt.Errorf("remove completed automatic filter job: %w", err)
	}
	return nil
}

// MarkOutcomeUnknown keeps a failed or interrupted move for later inspection.
// The service does not retry it automatically because destination APPEND may
// have succeeded even when its acknowledgement was lost.
func (s *JobStore) MarkOutcomeUnknown(id, reason string) error {
	if len(reason) > 2000 {
		reason = reason[:2000]
	}
	_, err := s.db.Exec(`UPDATE automatic_filter_jobs SET state='outcome_unknown', last_error=?, updated_at=datetime('now') WHERE id=?`, reason, id)
	if err != nil {
		return fmt.Errorf("record uncertain automatic filter outcome: %w", err)
	}
	return nil
}

// RecoverInterrupted marks jobs claimed by a previous process as uncertain.
// Pending jobs remain eligible for automatic execution.
func (s *JobStore) RecoverInterrupted() (int64, error) {
	result, err := s.db.Exec(`UPDATE automatic_filter_jobs SET state='outcome_unknown', last_error='service stopped during move; destination status requires inspection', updated_at=datetime('now') WHERE state='processing'`)
	if err != nil {
		return 0, fmt.Errorf("recover interrupted automatic filter jobs: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("count interrupted automatic filter jobs: %w", err)
	}
	return count, nil
}

// ListOutcomeUnknown returns a stable bounded page of jobs whose move result
// must be inspected. Reading these rows never changes their state.
func (s *JobStore) ListOutcomeUnknown(offset, limit int) ([]FilterJobInspection, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("automatic filter job store is not open")
	}
	if offset < 0 || limit < 1 || limit > 101 {
		return nil, errors.New("automatic filter job page is invalid")
	}
	rows, err := s.db.Query(`
		SELECT j.id, j.rule_id, j.message_id, j.source_account_id, j.source_folder_id,
		       j.source_uid, COALESCE(f.account_id, ''), j.destination_folder_id,
		       j.state, j.last_error, j.created_at, j.updated_at
		FROM automatic_filter_jobs AS j
		LEFT JOIN folders AS f ON f.id = j.destination_folder_id
		WHERE j.state = 'outcome_unknown'
		ORDER BY j.updated_at, j.id
		LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("list uncertain automatic filter jobs: %w", err)
	}
	defer rows.Close()

	jobs := make([]FilterJobInspection, 0, limit)
	for rows.Next() {
		var job FilterJobInspection
		if err := rows.Scan(
			&job.ID, &job.RuleID, &job.MessageID, &job.SourceAccountID, &job.SourceFolderID,
			&job.SourceUID, &job.DestinationAccountID, &job.DestinationFolderID,
			&job.State, &job.LastError, &job.CreatedAt, &job.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("read uncertain automatic filter job: %w", err)
		}
		jobs = append(jobs, job)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate uncertain automatic filter jobs: %w", err)
	}
	return jobs, nil
}

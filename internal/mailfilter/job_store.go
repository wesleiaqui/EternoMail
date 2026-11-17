package mailfilter

import (
	"database/sql"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/hkdb/aerion/internal/database"
	"github.com/hkdb/aerion/internal/message"
)

// FilterJob is the durable input needed to resume an automatic filter move
// that had not started before the service stopped.
type FilterJob struct {
	ID                  string
	RuleID              string
	DestinationFolderID string
	MessageID           string
	SourceAccountID     string
	SourceFolderID      string
	SourceUID           uint32
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

// Enqueue records a matched automatic move. Duplicate sync callbacks for the
// same rule, message and source location collapse to one job.
func (s *JobStore) Enqueue(rule *Rule, destinationFolderID string, msg *message.Message) error {
	if s == nil || s.db == nil || rule == nil || msg == nil || rule.ID == "" || destinationFolderID == "" || msg.ID == "" || msg.AccountID == "" || msg.FolderID == "" || msg.UID == 0 {
		return errors.New("automatic filter job is missing required identifiers")
	}
	_, err := s.db.Exec(`
		INSERT INTO automatic_filter_jobs
		(id, rule_id, destination_folder_id, message_id, source_account_id, source_folder_id, source_uid, state)
		VALUES (?, ?, ?, ?, ?, ?, ?, 'pending')
		ON CONFLICT(rule_id, message_id, source_folder_id, destination_folder_id) DO NOTHING
	`, uuid.NewString(), rule.ID, destinationFolderID, msg.ID, msg.AccountID, msg.FolderID, msg.UID)
	if err != nil {
		return fmt.Errorf("persist automatic filter job: %w", err)
	}
	return nil
}

// ClaimNext atomically changes the oldest pending job to processing.
func (s *JobStore) ClaimNext() (*FilterJob, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("begin automatic filter claim: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	job := &FilterJob{}
	err = tx.QueryRow(`
		SELECT id, rule_id, destination_folder_id, message_id, source_account_id, source_folder_id, source_uid
		FROM automatic_filter_jobs WHERE state='pending' AND updated_at<=datetime('now') ORDER BY created_at, id LIMIT 1
	`).Scan(&job.ID, &job.RuleID, &job.DestinationFolderID, &job.MessageID, &job.SourceAccountID, &job.SourceFolderID, &job.SourceUID)
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

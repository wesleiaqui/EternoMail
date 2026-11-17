// Package triage stores app-local read-later membership and snooze deadlines.
package triage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/hkdb/aerion/internal/database"
)

type Ref struct {
	AccountID      string    `json:"accountId"`
	ConversationID string    `json:"conversationId"`
	WakeAt         time.Time `json:"wakeAt,omitempty"`
	Indexed        *bool     `json:"indexed,omitempty"`
}

type Store struct{ db *database.DB }

func NewStore(db *database.DB) *Store { return &Store{db: db} }

func validate(accountID, conversationID string) error {
	if strings.TrimSpace(accountID) == "" || strings.TrimSpace(conversationID) == "" {
		return fmt.Errorf("account and conversation IDs are required")
	}
	return nil
}

func (s *Store) SetReadLater(ref Ref, enabled bool) (bool, error) {
	if err := validate(ref.AccountID, ref.ConversationID); err != nil {
		return false, err
	}
	var result interface{ RowsAffected() (int64, error) }
	var err error
	if enabled {
		result, err = s.db.ExecContext(context.Background(), `INSERT INTO read_later_queue(account_id,conversation_id) VALUES(?,?) ON CONFLICT DO NOTHING`, ref.AccountID, ref.ConversationID)
	} else {
		result, err = s.db.ExecContext(context.Background(), `DELETE FROM read_later_queue WHERE account_id=? AND conversation_id=?`, ref.AccountID, ref.ConversationID)
	}
	if err != nil {
		return false, fmt.Errorf("change Read Later membership: %w", err)
	}
	n, err := result.RowsAffected()
	return n > 0, err
}

func (s *Store) IsReadLater(accountID, conversationID string) (bool, error) {
	if err := validate(accountID, conversationID); err != nil {
		return false, err
	}
	var exists int
	err := s.db.QueryRowContext(context.Background(), `SELECT EXISTS(SELECT 1 FROM read_later_queue WHERE account_id=? AND conversation_id=?)`, accountID, conversationID).Scan(&exists)
	return exists == 1, err
}

func (s *Store) ListReadLater() ([]Ref, error) {
	rows, err := s.db.QueryContext(context.Background(), `SELECT account_id,conversation_id, EXISTS(SELECT 1 FROM messages m WHERE m.account_id=rl.account_id AND COALESCE(m.thread_id,m.id)=rl.conversation_id) FROM read_later_queue rl ORDER BY added_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var refs []Ref
	for rows.Next() {
		var ref Ref
		if err := rows.Scan(&ref.AccountID, &ref.ConversationID, &ref.Indexed); err != nil {
			return nil, err
		}
		refs = append(refs, ref)
	}
	return refs, rows.Err()
}

func (s *Store) CountReadLater() (int, error) {
	var count int
	err := s.db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM read_later_queue`).Scan(&count)
	return count, err
}

func (s *Store) Snooze(ref Ref, until time.Time) error {
	if err := validate(ref.AccountID, ref.ConversationID); err != nil {
		return err
	}
	if !until.After(time.Now()) {
		return fmt.Errorf("snooze deadline must be in the future")
	}
	_, err := s.db.ExecContext(context.Background(), `INSERT INTO snoozed_conversations(account_id,conversation_id,wake_at) VALUES(?,?,?) ON CONFLICT(account_id,conversation_id) DO UPDATE SET wake_at=excluded.wake_at, added_at=datetime('now')`, ref.AccountID, ref.ConversationID, until.UTC().Format(time.RFC3339Nano))
	return err
}

func (s *Store) Unsnooze(accountID, conversationID string) (bool, error) {
	if err := validate(accountID, conversationID); err != nil {
		return false, err
	}
	r, err := s.db.ExecContext(context.Background(), `DELETE FROM snoozed_conversations WHERE account_id=? AND conversation_id=?`, accountID, conversationID)
	if err != nil {
		return false, err
	}
	n, err := r.RowsAffected()
	return n > 0, err
}

// GetSnooze returns one account-qualified Snooze without scanning the full queue.
func (s *Store) GetSnooze(accountID, conversationID string) (time.Time, bool, error) {
	if err := validate(accountID, conversationID); err != nil {
		return time.Time{}, false, err
	}
	var wakeAt string
	err := s.db.QueryRowContext(context.Background(), `SELECT wake_at FROM snoozed_conversations WHERE account_id=? AND conversation_id=?`, accountID, conversationID).Scan(&wakeAt)
	if errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, false, err
	}
	deadline, err := time.Parse(time.RFC3339Nano, wakeAt)
	if err != nil {
		return time.Time{}, false, fmt.Errorf("parse Snooze deadline: %w", err)
	}
	return deadline, true, nil
}

func (s *Store) ExpireSnoozes(now time.Time) (int64, error) {
	r, err := s.db.ExecContext(context.Background(), `DELETE FROM snoozed_conversations WHERE julianday(wake_at)<=julianday(?)`, now.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return 0, err
	}
	return r.RowsAffected()
}

func (s *Store) ListSnoozed() ([]Ref, error) {
	rows, err := s.db.QueryContext(context.Background(), `SELECT account_id,conversation_id,wake_at FROM snoozed_conversations ORDER BY wake_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var refs []Ref
	for rows.Next() {
		var ref Ref
		var wake string
		if err := rows.Scan(&ref.AccountID, &ref.ConversationID, &wake); err != nil {
			return nil, err
		}
		ref.WakeAt, _ = time.Parse(time.RFC3339Nano, wake)
		refs = append(refs, ref)
	}
	return refs, rows.Err()
}

func (s *Store) CountSnoozed() (int, error) {
	var count int
	err := s.db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM snoozed_conversations WHERE julianday(wake_at)>julianday(?)`, time.Now().UTC().Format(time.RFC3339Nano)).Scan(&count)
	return count, err
}

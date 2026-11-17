package triage

import (
	"context"
	"errors"
	"fmt"
)

// LaterState preserves the two legacy memberships without migrating either.
type LaterState struct {
	Read  bool `json:"read"`
	Reply bool `json:"reply"`
}

func (s *Store) LaterState(ref Ref) (state LaterState, err error) {
	if err = validate(ref.AccountID, ref.ConversationID); err != nil {
		return
	}
	err = s.db.QueryRow(`SELECT EXISTS(SELECT 1 FROM read_later_queue WHERE account_id=? AND conversation_id=?), EXISTS(SELECT 1 FROM reply_later_queue WHERE account_id=? AND conversation_id=?)`, ref.AccountID, ref.ConversationID, ref.AccountID, ref.ConversationID).Scan(&state.Read, &state.Reply)
	return
}

// SetLaterState applies one logical operation. Provider effects are compensated
// on failure; local memberships commit together and only if still as captured.
// A returned error must never be presented as a successful/undoable operation.
func (s *Store) SetLaterState(ctx context.Context, ref Ref, before, after LaterState, label func(string, bool) error) (err error) {
	if err = validate(ref.AccountID, ref.ConversationID); err != nil {
		return err
	}
	current, checkErr := s.LaterState(ref)
	if checkErr != nil {
		return checkErr
	}
	if current != before {
		return errors.New("Later membership changed since the operation")
	}
	type change struct {
		queue    string
		old, new bool
	}
	changes := []change{}
	if before.Read != after.Read {
		changes = append(changes, change{"read-later", before.Read, after.Read})
	}
	if before.Reply != after.Reply {
		changes = append(changes, change{"reply-later", before.Reply, after.Reply})
	}
	applied := []change{}
	defer func() {
		if err != nil && label != nil {
			for i := len(applied) - 1; i >= 0; i-- {
				c := applied[i]
				if restoreErr := label(c.queue, c.old); restoreErr != nil {
					err = errors.Join(err, fmt.Errorf("restore %s provider state: %w", c.queue, restoreErr))
				}
			}
		}
	}()
	for _, c := range changes {
		if label != nil { // Include the failed request: its provider outcome may be uncertain.
			applied = append(applied, c)
			if err = label(c.queue, c.new); err != nil {
				return err
			}
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	current = LaterState{}
	err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM read_later_queue WHERE account_id=? AND conversation_id=?), EXISTS(SELECT 1 FROM reply_later_queue WHERE account_id=? AND conversation_id=?)`, ref.AccountID, ref.ConversationID, ref.AccountID, ref.ConversationID).Scan(&current.Read, &current.Reply)
	if err != nil {
		return err
	}
	if current != before {
		return errors.New("Later membership changed since the operation")
	}
	for _, item := range []struct {
		table string
		on    bool
	}{{"read_later_queue", after.Read}, {"reply_later_queue", after.Reply}} {
		query := `DELETE FROM ` + item.table + ` WHERE account_id=? AND conversation_id=?`
		if item.on {
			query = `INSERT INTO ` + item.table + `(account_id,conversation_id) VALUES(?,?) ON CONFLICT DO NOTHING`
		}
		if _, err = tx.ExecContext(ctx, query, ref.AccountID, ref.ConversationID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

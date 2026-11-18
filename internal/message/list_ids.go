package message

import (
	"database/sql"
	"fmt"
	"strings"
)

// SetListID persists a message's List-ID header value for filter matching.
// An empty value removes any stored row: a message known to have no List-ID
// must not keep a stale one from an earlier sync. The table is installed by
// migration 82; writes are best-effort there and after.
func (s *Store) SetListID(messageID, listID string) error {
	listID = strings.TrimSpace(listID)
	if listID == "" {
		if _, err := s.db.Exec(`DELETE FROM message_list_ids WHERE message_id = ?`, messageID); err != nil {
			return fmt.Errorf("clear list id: %w", err)
		}
		return nil
	}
	_, err := s.db.Exec(`
		INSERT INTO message_list_ids (message_id, list_id) VALUES (?, ?)
		ON CONFLICT(message_id) DO UPDATE SET list_id = excluded.list_id
	`, messageID, listID)
	if err != nil {
		return fmt.Errorf("store list id: %w", err)
	}
	return nil
}

// GetListID returns the stored List-ID header value for a message, or an
// empty string when none was captured (including messages synced before the
// header table existed).
func (s *Store) GetListID(messageID string) (string, error) {
	var listID string
	err := s.db.QueryRow(`SELECT list_id FROM message_list_ids WHERE message_id = ?`, messageID).Scan(&listID)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("load list id: %w", err)
	}
	return listID, nil
}

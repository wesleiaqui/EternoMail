package message

import (
	"database/sql"
	"fmt"
	"github.com/google/uuid"
	"strings"
	"time"
)

// GmailID is account-qualified. Cache row identifiers are never provider identifiers.
func (s *Store) GmailID(accountID, localID string) (string, error) {
	var id string
	err := s.db.QueryRow(`SELECT provider_id FROM gmail_message_refs WHERE account_id=? AND message_id=?`, accountID, localID).Scan(&id)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return id, err
}

// UpsertGmail stores metadata without IMAP. The legacy schema requires a numeric
// unique UID; the reserved high-half cache key is a LOCAL surrogate, not an IMAP
// UID. Every Gmail transport resolves gmail_message_refs instead. No fallback
// may send this key to IMAP (native cache keys have int32(uid)<0).
func (s *Store) UpsertGmail(m *Message, providerID string) error {
	if providerID == "" || m.AccountID == "" || m.FolderID == "" {
		return fmt.Errorf("incomplete Gmail identity")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }() // no-op after a successful Commit
	var key int64
	if err = tx.QueryRow(`UPDATE gmail_cache_sequence SET next=next+1 WHERE id=1 AND next<4294967295 RETURNING next`).Scan(&key); err != nil {
		return err
	}
	var id string
	err = tx.QueryRow(`SELECT message_id FROM gmail_message_refs WHERE account_id=? AND folder_id=? AND provider_id=?`, m.AccountID, m.FolderID, providerID).Scan(&id)
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	if id == "" && m.MessageID != "" {
		var count int
		if err = tx.QueryRow(`SELECT COUNT(*),COALESCE(MIN(id),'') FROM messages WHERE account_id=? AND folder_id=? AND message_id IN (?,?,?) AND NOT EXISTS (SELECT 1 FROM gmail_message_refs bound WHERE bound.message_id=messages.id)`, m.AccountID, m.FolderID, m.MessageID, strings.Trim(m.MessageID, "<>"), "<"+strings.Trim(m.MessageID, "<>")+">").Scan(&count, &id); err != nil {
			return err
		}
		if count != 1 {
			id = ""
		}
	}
	if id == "" {
		id = uuid.NewString()
	}
	_, err = tx.Exec(`INSERT INTO messages(id,account_id,folder_id,uid,message_id,in_reply_to,references_list,thread_id,subject,from_name,from_email,to_list,cc_list,bcc_list,reply_to,read_receipt_to,inbox_category,date,snippet,is_read,is_starred,is_answered,is_draft,size,received_at,body_fetched)
 VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,0) ON CONFLICT(id) DO UPDATE SET
 thread_id=excluded.thread_id,subject=excluded.subject,from_name=excluded.from_name,from_email=excluded.from_email,to_list=excluded.to_list,cc_list=excluded.cc_list,bcc_list=excluded.bcc_list,reply_to=excluded.reply_to,read_receipt_to=excluded.read_receipt_to,inbox_category=excluded.inbox_category,in_reply_to=excluded.in_reply_to,references_list=excluded.references_list,date=excluded.date,snippet=excluded.snippet,is_read=excluded.is_read,is_starred=excluded.is_starred,is_draft=excluded.is_draft,size=excluded.size`, id, m.AccountID, m.FolderID, key, m.MessageID, m.InReplyTo, m.References, m.ThreadID, m.Subject, m.FromName, m.FromEmail, m.ToList, m.CcList, m.BccList, m.ReplyTo, m.ReadReceiptTo, m.InboxCategory, m.Date.UTC(), m.Snippet, m.IsRead, m.IsStarred, m.IsAnswered, m.IsDraft, m.Size, time.Now().UTC())
	if err != nil {
		return err
	}
	_, err = tx.Exec(`INSERT INTO gmail_message_refs(account_id,folder_id,provider_id,message_id) VALUES(?,?,?,?) ON CONFLICT(message_id) DO UPDATE SET folder_id=excluded.folder_id,provider_id=excluded.provider_id`, m.AccountID, m.FolderID, providerID, id)
	if err != nil {
		return err
	}
	var storedUID int64
	if err = tx.QueryRow(`SELECT uid FROM messages WHERE id=?`, id).Scan(&storedUID); err != nil {
		return err
	}
	m.UID = uint32(storedUID)
	if err = tx.Commit(); err != nil {
		return err
	}
	m.ID = id
	return nil
}
func (s *Store) GmailFolderMessages(accountID, folderID string) (map[string]string, error) {
	rows, err := s.db.Query(`SELECT r.provider_id,r.message_id FROM gmail_message_refs r JOIN messages m ON m.id=r.message_id WHERE r.account_id=? AND m.folder_id=?`, accountID, folderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := map[string]string{}
	for rows.Next() {
		var provider, id string
		if err = rows.Scan(&provider, &id); err != nil {
			return nil, err
		}
		result[provider] = id
	}
	return result, rows.Err()
}

func (s *Store) GmailMessageByCacheKey(accountID, folderID string, key uint32) (*Message, error) {
	var id string
	err := s.db.QueryRow(`SELECT m.id FROM messages m JOIN gmail_message_refs r ON r.message_id=m.id WHERE m.account_id=? AND m.folder_id=? AND (m.uid=? OR m.uid=?)`, accountID, folderID, key, int32(key)).Scan(&id)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return s.Get(id)
}

func (s *Store) DeleteGmailProviderCopies(accountID, providerID string) error {
	_, err := s.db.Exec(`DELETE FROM messages WHERE account_id=? AND id IN (SELECT message_id FROM gmail_message_refs WHERE account_id=? AND provider_id=?)`, accountID, accountID, providerID)
	return err
}
func (s *Store) PruneGmailMemberships(accountID, providerID string, folderIDs []string) error {
	placeholders := make([]string, len(folderIDs))
	args := []any{accountID, accountID, providerID}
	for i, id := range folderIDs {
		placeholders[i] = "?"
		args = append(args, id)
	}
	if len(folderIDs) == 0 {
		return nil
	}
	_, err := s.db.Exec(`DELETE FROM messages WHERE account_id=? AND id IN (SELECT message_id FROM gmail_message_refs WHERE account_id=? AND provider_id=?) AND folder_id NOT IN (`+strings.Join(placeholders, ",")+`)`, args...)
	return err
}

// MoveGmailMessages keeps the action's local IDs while reconciling any cached
// destination copies of the same provider message. No server UID is involved.
func (s *Store) MoveGmailMessages(ids []string, destination string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }() // no-op after a successful Commit
	if _, err = tx.Exec(`UPDATE gmail_cache_sequence SET next=next WHERE id=1`); err != nil {
		return err
	}

	for _, id := range ids {
		var accountID, provider string
		if err = tx.QueryRow(`SELECT account_id,provider_id FROM gmail_message_refs WHERE message_id=?`, id).Scan(&accountID, &provider); err != nil {
			return err
		}
		var destinationAccount string
		if err = tx.QueryRow(`SELECT account_id FROM folders WHERE id=?`, destination).Scan(&destinationAccount); err != nil {
			return err
		}
		if destinationAccount != accountID {
			return fmt.Errorf("Gmail local move crosses accounts")
		}
		var key int64
		if err = tx.QueryRow(`UPDATE gmail_cache_sequence SET next=next+1 WHERE id=1 AND next<4294967295 RETURNING next`).Scan(&key); err != nil {
			return err
		}
		if _, err = tx.Exec(`DELETE FROM messages WHERE id IN (SELECT message_id FROM gmail_message_refs WHERE account_id=? AND folder_id=? AND provider_id=? AND message_id!=?)`, accountID, destination, provider, id); err != nil {
			return err
		}
		if _, err = tx.Exec(`UPDATE messages SET folder_id=?,uid=? WHERE id=? AND account_id=?`, destination, key, id, accountID); err != nil {
			return err
		}
		if _, err = tx.Exec(`UPDATE gmail_message_refs SET folder_id=? WHERE message_id=?`, destination, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}
func (s *Store) BindGmailID(accountID, localID, providerID string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }() // no-op after a successful Commit
	// Take the write lock before resolving legacy cache rows.
	if _, err = tx.Exec(`UPDATE gmail_cache_sequence SET next=next WHERE id=1`); err != nil {
		return err
	}
	var folderID string
	if err = tx.QueryRow(`SELECT folder_id FROM messages WHERE id=? AND account_id=?`, localID, accountID).Scan(&folderID); err != nil {
		return err
	}
	if _, err = tx.Exec(`DELETE FROM messages WHERE id IN (SELECT message_id FROM gmail_message_refs WHERE account_id=? AND folder_id=? AND provider_id=? AND message_id!=?)`, accountID, folderID, providerID, localID); err != nil {
		return err
	}
	if _, err = tx.Exec(`INSERT INTO gmail_message_refs(account_id,folder_id,provider_id,message_id) VALUES(?,?,?,?) ON CONFLICT(message_id) DO UPDATE SET folder_id=excluded.folder_id,provider_id=excluded.provider_id`, accountID, folderID, providerID, localID); err != nil {
		return err
	}
	return tx.Commit()
}

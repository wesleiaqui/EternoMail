package message

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
)

// RepairPart is produced only by the strict, complete authoritative MIME parser.
// Every occurrence stays distinct; payload is retained for exact downloads.
type RepairSource struct {
	FolderID  string
	UID       uint32
	MessageID string
}

type RepairPart struct {
	Metadata Attachment
	Payload  []byte
}
type attachmentBackup struct {
	Metadata        Attachment
	Payload         []byte
	OriginalContent []byte
}

// RepairFromMIME replaces one IMAP message's metadata transactionally and keeps
// a full undo snapshot. No files are deleted. Caller verifies original MIME
// identity and completeness; this method verifies account and DB ownership.
func (s *AttachmentStore) RepairFromMIME(accountID, messageID, snapshot string, parts []RepairPart, cacheRoot string, expected ...RepairSource) (string, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback() }()
	var owner string
	var gmail int
	if err = tx.QueryRow(`SELECT account_id FROM messages WHERE id=?`, messageID).Scan(&owner); err != nil {
		return "", err
	}
	if len(expected) > 0 {
		var current RepairSource
		if err = tx.QueryRow("SELECT folder_id,uid,COALESCE(message_id,'') FROM messages WHERE id=?", messageID).Scan(&current.FolderID, &current.UID, &current.MessageID); err != nil {
			return "", err
		}
		if current != expected[0] {
			return "", fmt.Errorf("message source changed during attachment repair")
		}
	}
	if accountID == "" || messageID == "" || snapshot == "" {
		return "", fmt.Errorf("missing repair identity")
	}
	if owner != accountID {
		return "", fmt.Errorf("attachment repair account mismatch")
	}
	if err = tx.QueryRow(`SELECT (SELECT COUNT(*) FROM gmail_message_refs WHERE message_id=?) + (SELECT COUNT(*) FROM gmail_attachment_refs g JOIN attachments a ON a.id=g.attachment_id WHERE a.message_id=?)`, messageID, messageID).Scan(&gmail); err != nil {
		return "", err
	}
	if gmail != 0 {
		return "", fmt.Errorf("Gmail-native repair unsupported")
	}
	var existing string
	err = tx.QueryRow(`SELECT backup_id FROM attachment_repairs WHERE message_id=?`, messageID).Scan(&existing)
	if err == nil {
		return existing, nil
	}
	if err != sql.ErrNoRows {
		return "", err
	}
	old, err := readAttachmentBackup(tx, messageID)
	if err != nil {
		return "", err
	}
	encoded, err := json.Marshal(old)
	if err != nil {
		return "", err
	}
	backupID := uuid.NewString()
	if _, err = tx.Exec(`INSERT INTO attachment_repair_backups(id,message_id,rows_json) VALUES(?,?,?)`, backupID, messageID, encoded); err != nil {
		return "", err
	}
	if _, err = tx.Exec(`DELETE FROM attachments WHERE message_id=?`, messageID); err != nil {
		return "", err
	}
	for _, part := range parts {
		a := part.Metadata
		if a.MessageID != messageID || a.ID == "" || len(part.Payload) != a.Size {
			return "", fmt.Errorf("invalid repair part")
		}
		// Never trust a filename-only cached path. Compare the actual bounded bytes
		// inside the application's attachment cache. Preserve every old path in undo.
		a.LocalPath = ""
		for _, prior := range old {
			if verifiedCachePayload(cacheRoot, prior.Metadata.LocalPath, part.Payload) {
				a.LocalPath = prior.Metadata.LocalPath
				break
			}
		}
		var inline []byte
		if a.IsInline {
			inline = part.Payload
		}
		if _, err = tx.Exec(`INSERT INTO attachments(id,message_id,filename,content_type,size,content_id,is_inline,local_path,content) VALUES(?,?,?,?,?,?,?,?,?)`, a.ID, messageID, a.Filename, a.ContentType, a.Size, nullString(a.ContentID), boolToInt(a.IsInline), nullString(a.LocalPath), inline); err != nil {
			return "", err
		}
		payload := part.Payload
		if payload == nil {
			payload = []byte{}
		}
		if _, err = tx.Exec(`INSERT INTO attachment_repair_payloads(attachment_id,content) VALUES(?,?)`, a.ID, payload); err != nil {
			return "", err
		}
	}
	if _, err = tx.Exec(`INSERT INTO attachment_repairs(message_id,backup_id,snapshot_digest) VALUES(?,?,?)`, messageID, backupID, snapshot); err != nil {
		return "", err
	}
	if err = tx.Commit(); err != nil {
		return "", err
	}
	return backupID, nil
}

func readAttachmentBackup(tx *sql.Tx, messageID string) ([]attachmentBackup, error) {
	rows, err := tx.Query(`SELECT a.id,a.filename,COALESCE(a.content_type,''),COALESCE(a.size,0),COALESCE(a.content_id,''),COALESCE(a.is_inline,0),COALESCE(a.local_path,''),a.content,p.content FROM attachments a LEFT JOIN attachment_repair_payloads p ON p.attachment_id=a.id WHERE a.message_id=?`, messageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []attachmentBackup{}
	for rows.Next() {
		var b attachmentBackup
		b.Metadata.MessageID = messageID
		if err = rows.Scan(&b.Metadata.ID, &b.Metadata.Filename, &b.Metadata.ContentType, &b.Metadata.Size, &b.Metadata.ContentID, &b.Metadata.IsInline, &b.Metadata.LocalPath, &b.OriginalContent, &b.Payload); err != nil {
			return nil, err
		}
		result = append(result, b)
	}
	return result, rows.Err()
}

// RestoreRepair is generation-guarded: it never overwrites a different repair.
// Cached files are not removed by either direction. Backup is retained for audit.
func (s *AttachmentStore) RestoreRepair(accountID, messageID, backupID string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var current, owner string
	var encoded []byte
	if err = tx.QueryRow(`SELECT m.account_id,r.backup_id,b.rows_json FROM messages m JOIN attachment_repairs r ON r.message_id=m.id JOIN attachment_repair_backups b ON b.id=r.backup_id WHERE m.id=?`, messageID).Scan(&owner, &current, &encoded); err != nil {
		return err
	}
	if owner != accountID || current != backupID {
		return fmt.Errorf("stale or unauthorized attachment repair restore")
	}
	var old []attachmentBackup
	if err = json.Unmarshal(encoded, &old); err != nil {
		return err
	}
	// Preserve the current state as the next undo generation before restoring.
	// This includes downloads/path changes made after the original repair.
	present, err := readAttachmentBackup(tx, messageID)
	if err != nil {
		return err
	}
	saved, err := json.Marshal(present)
	if err != nil {
		return err
	}
	nextBackup := uuid.NewString()
	if _, err = tx.Exec(`INSERT INTO attachment_repair_backups(id,message_id,rows_json) VALUES(?,?,?)`, nextBackup, messageID, saved); err != nil {
		return err
	}
	if _, err = tx.Exec(`DELETE FROM attachments WHERE message_id=?`, messageID); err != nil {
		return err
	}
	for _, b := range old {
		a := b.Metadata
		if a.MessageID != messageID {
			return fmt.Errorf("backup message mismatch")
		}
		if _, err = tx.Exec(`INSERT INTO attachments(id,message_id,filename,content_type,size,content_id,is_inline,local_path,content) VALUES(?,?,?,?,?,?,?,?,?)`, a.ID, messageID, a.Filename, a.ContentType, a.Size, nullString(a.ContentID), boolToInt(a.IsInline), nullString(a.LocalPath), b.OriginalContent); err != nil {
			return err
		}
		if b.Payload != nil {
			if _, err = tx.Exec(`INSERT INTO attachment_repair_payloads(attachment_id,content) VALUES(?,?)`, a.ID, b.Payload); err != nil {
				return err
			}
		}
	}
	if _, err = tx.Exec(`UPDATE attachment_repairs SET backup_id=?,snapshot_digest='restored' WHERE message_id=? AND backup_id=?`, nextBackup, messageID, backupID); err != nil {
		return err
	}
	return tx.Commit()
}

// RepairedContent returns exact part bytes, never a filename-based fallback.
func (s *AttachmentStore) RepairedContent(id string) ([]byte, bool, error) {
	var content []byte
	err := s.db.QueryRow(`SELECT content FROM attachment_repair_payloads WHERE attachment_id=?`, id).Scan(&content)
	if err == sql.ErrNoRows {
		return nil, false, nil
	}
	return content, err == nil, err
}
func (s *AttachmentStore) RepairBackupID(messageID string) (string, error) {
	var id string
	err := s.db.QueryRow(`SELECT backup_id FROM attachment_repairs WHERE message_id=?`, messageID).Scan(&id)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return id, err
}

func verifiedCachePayload(root, path string, want []byte) bool {
	if root == "" || path == "" {
		return false
	}
	canonicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return false
	}
	canonicalPath, err := filepath.EvalSymlinks(path)
	if err != nil {
		return false
	}
	relative, err := filepath.Rel(canonicalRoot, canonicalPath)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return false
	}
	f, err := os.Open(canonicalPath)
	if err != nil {
		return false
	}
	defer f.Close()
	stat, err := f.Stat()
	if err != nil || !stat.Mode().IsRegular() || stat.Size() != int64(len(want)) {
		return false
	}
	got, err := io.ReadAll(io.LimitReader(f, int64(len(want))+1))
	if err != nil {
		return false
	}
	a, b := sha256.Sum256(got), sha256.Sum256(want)
	return bytes.Equal(a[:], b[:])
}

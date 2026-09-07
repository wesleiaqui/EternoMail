package credentials

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/hkdb/aerion/internal/crypto"
)

// These identifiers are internal constants, never user-supplied SQL.
var credentialColumns = []struct {
	table, column string
	keys          []string
}{
	{"accounts", "encrypted_password", []string{"id"}},
	{"accounts", "encrypted_smtp_password", []string{"id"}},
	{"smime_certificates", "encrypted_private_key", []string{"id"}},
	{"pgp_keys", "encrypted_private_key", []string{"id"}},
	{"extension_secrets", "encrypted_value", []string{"extension", "key"}},
	{"contact_sources", "encrypted_password", []string{"id"}},
	{"accounts", "encrypted_access_token", []string{"id"}},
	{"accounts", "encrypted_refresh_token", []string{"id"}},
	{"contact_sources", "encrypted_access_token", []string{"id"}},
	{"contact_sources", "encrypted_refresh_token", []string{"id"}},
	{"oauth_tokens", "encrypted_access_token", []string{"account_id", "client_config_id"}},
	{"oauth_tokens", "encrypted_refresh_token", []string{"account_id", "client_config_id"}},
	{"user_oauth_clients", "encrypted", []string{"config_id"}},
	{"oauth_custom_providers", "encrypted", []string{"account_id"}},
}

// reencryptAllCredentials commits every column together. Recovery material is
// retained by the caller on any failure, so startup can retry after rollback.
func (s *Store) reencryptAllCredentials(legacyEnc, newEnc *crypto.Encryptor) error {
	// FULL is required on this connection: NORMAL may acknowledge a WAL
	// commit before it reaches stable storage, while recovery-key deletion is
	// already durable. Keep the normal pool policy for all other operations.
	conn, err := s.db.Conn(context.Background())
	if err != nil {
		return err
	}
	defer conn.Close()
	var synchronous int
	if err := conn.QueryRowContext(context.Background(), "PRAGMA synchronous").Scan(&synchronous); err != nil {
		return err
	}
	if _, err := conn.ExecContext(context.Background(), "PRAGMA synchronous=FULL"); err != nil {
		return err
	}
	defer func() {
		if _, err := conn.ExecContext(context.Background(), fmt.Sprintf("PRAGMA synchronous=%d", synchronous)); err != nil {
			s.log.Warn().Err(err).Msg("restore database synchronous mode failed")
		}
	}()
	tx, err := conn.BeginTx(context.Background(), nil)
	if err != nil {
		return fmt.Errorf("begin re-encryption transaction: %w", err)
	}
	defer func() {
		if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
			s.log.Error().Err(err).Msg("re-encryption rollback failed")
		}
	}()
	for _, col := range credentialColumns {
		// These two tables are created lazily by their first credential write.
		if col.table == "user_oauth_clients" || col.table == "oauth_custom_providers" {
			var exists int
			if err := tx.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?", col.table).Scan(&exists); err != nil {
				return err
			}
			if exists == 0 {
				continue
			}
		}
		predicates := make([]string, len(col.keys))
		for i, key := range col.keys {
			predicates[i] = key + " = ?"
		}
		selectSQL := fmt.Sprintf("SELECT %s, %s FROM %s WHERE %s IS NOT NULL AND %s != ''",
			strings.Join(col.keys, ", "), col.column, col.table, col.column, col.column)
		updateSQL := fmt.Sprintf("UPDATE %s SET %s = ? WHERE %s",
			col.table, col.column, strings.Join(predicates, " AND "))
		if err := reencryptColumn(tx, legacyEnc, newEnc, selectSQL, updateSQL, len(col.keys)); err != nil {
			return fmt.Errorf("%s.%s: %w", col.table, col.column, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit re-encryption: %w", err)
	}
	return nil
}

// Materialize and close the cursor before updates, including composite keys.
func reencryptColumn(tx *sql.Tx, legacyEnc, newEnc *crypto.Encryptor, selectSQL, updateSQL string, keyCount int) error {
	rows, err := tx.Query(selectSQL)
	if err != nil {
		return err
	}
	defer rows.Close()
	type row struct {
		keys       []string
		ciphertext string
	}
	var pending []row
	for rows.Next() {
		r := row{keys: make([]string, keyCount)}
		dest := make([]any, keyCount+1)
		for i := range r.keys {
			dest[i] = &r.keys[i]
		}
		dest[keyCount] = &r.ciphertext
		if err := rows.Scan(dest...); err != nil {
			return err
		}
		pending = append(pending, r)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, r := range pending {
		// A previous commit may have succeeded just before recovery-file cleanup
		// was interrupted. Authenticate with v2 to safely skip those rows.
		if _, err := newEnc.Decrypt(r.ciphertext); err == nil {
			continue
		}
		plaintext, err := legacyEnc.Decrypt(r.ciphertext)
		if err != nil {
			return fmt.Errorf("decrypt credential: %w", err)
		}
		ciphertext, err := newEnc.Encrypt(plaintext)
		if err != nil {
			return fmt.Errorf("encrypt credential: %w", err)
		}
		args := []any{ciphertext}
		for _, key := range r.keys {
			args = append(args, key)
		}
		if _, err := tx.Exec(updateSQL, args...); err != nil {
			return fmt.Errorf("update credential: %w", err)
		}
	}
	return nil
}

// rejectMissingCredentialKey checks only Encryptor-managed values; keyring-only
// accounts and an empty new database still allow first-time key creation.
func rejectMissingCredentialKey(db *sql.DB) error {
	for _, col := range credentialColumns {
		var exists int
		if err := db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?", col.table).Scan(&exists); err != nil {
			return err
		}
		if exists == 0 {
			continue
		}
		var populated int
		query := fmt.Sprintf("SELECT EXISTS(SELECT 1 FROM %s WHERE %s IS NOT NULL AND %s != '')", col.table, col.column, col.column)
		if err := db.QueryRow(query).Scan(&populated); err != nil {
			return err
		}
		if populated != 0 {
			return fmt.Errorf("device key missing with encrypted credentials in %s.%s; restore the original key", col.table, col.column)
		}
	}
	return nil
}

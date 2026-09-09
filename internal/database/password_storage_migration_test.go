package database

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestMigrationV49PasswordStorageRollback(t *testing.T) {
	db := openUnmigratedTestDB(t)
	applyMigrationsThrough(t, db, 48)

	if _, err := db.Exec(`
		INSERT INTO accounts (
			id, name, email, imap_host, smtp_host, username,
			encrypted_password, encrypted_smtp_password
		) VALUES (
			'account-1', 'Account', 'account@example.test',
			'imap.example.test', 'smtp.example.test', 'account-1',
			'imap-ciphertext', 'smtp-ciphertext'
		)
	`); err != nil {
		t.Fatalf("seed v48 account: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO contact_sources (id, name, type, url, encrypted_password)
		VALUES ('source-1', 'Contacts', 'carddav', 'https://dav.example.test', 'dav-ciphertext')
	`); err != nil {
		t.Fatalf("seed v48 contact source: %v", err)
	}
	v48Schema := schemaSignature(t, db)

	if err := db.Migrate(); err != nil {
		t.Fatalf("migrate v48 to v49: %v", err)
	}
	for _, column := range []struct {
		table string
		name  string
	}{
		{"accounts", "password_storage"},
		{"accounts", "smtp_password_storage"},
		{"contact_sources", "password_storage"},
	} {
		if !tableHasColumn(t, db, column.table, column.name) {
			t.Fatalf("v49 missing %s.%s", column.table, column.name)
		}
	}

	var imapStorage, smtpStorage, davStorage string
	if err := db.QueryRow(`
		SELECT password_storage, smtp_password_storage FROM accounts WHERE id = 'account-1'
	`).Scan(&imapStorage, &smtpStorage); err != nil {
		t.Fatalf("read account markers: %v", err)
	}
	if err := db.QueryRow(`
		SELECT password_storage FROM contact_sources WHERE id = 'source-1'
	`).Scan(&davStorage); err != nil {
		t.Fatalf("read contact source marker: %v", err)
	}
	if imapStorage != "fallback" || smtpStorage != "fallback" || davStorage != "fallback" {
		t.Fatalf("v49 markers = %q, %q, %q; want fallback", imapStorage, smtpStorage, davStorage)
	}

	rollback, err := os.ReadFile(filepath.Join("..", "..", "tools", "db", "rollback-v49-to-v48.sql"))
	if err != nil {
		t.Fatalf("read rollback script: %v", err)
	}
	if _, err := db.Exec(string(rollback)); err != nil {
		t.Fatalf("run v49 rollback: %v", err)
	}

	for _, column := range []struct {
		table string
		name  string
	}{
		{"accounts", "password_storage"},
		{"accounts", "smtp_password_storage"},
		{"contact_sources", "password_storage"},
	} {
		if tableHasColumn(t, db, column.table, column.name) {
			t.Fatalf("rollback retained %s.%s", column.table, column.name)
		}
	}
	if got := schemaSignature(t, db); !reflect.DeepEqual(got, v48Schema) {
		t.Fatalf("rollback schema differs from v48: %s", firstSchemaDifference(v48Schema, got))
	}

	var accountName, imapCiphertext, smtpCiphertext, sourceName, davCiphertext string
	if err := db.QueryRow(`
		SELECT name, encrypted_password, encrypted_smtp_password FROM accounts WHERE id = 'account-1'
	`).Scan(&accountName, &imapCiphertext, &smtpCiphertext); err != nil {
		t.Fatalf("read rolled-back account: %v", err)
	}
	if err := db.QueryRow(`
		SELECT name, encrypted_password FROM contact_sources WHERE id = 'source-1'
	`).Scan(&sourceName, &davCiphertext); err != nil {
		t.Fatalf("read rolled-back contact source: %v", err)
	}
	if accountName != "Account" || imapCiphertext != "imap-ciphertext" || smtpCiphertext != "smtp-ciphertext" || sourceName != "Contacts" || davCiphertext != "dav-ciphertext" {
		t.Fatalf("v48 data changed by rollback: account=%q/%q/%q source=%q/%q", accountName, imapCiphertext, smtpCiphertext, sourceName, davCiphertext)
	}

	var version int
	if err := db.QueryRow(`SELECT MAX(version) FROM migrations`).Scan(&version); err != nil {
		t.Fatalf("read migration version after rollback: %v", err)
	}
	if version != 48 {
		t.Fatalf("migration version after rollback = %d, want 48", version)
	}

	if err := db.Migrate(); err != nil {
		t.Fatalf("migrate v48 to v49 after rollback: %v", err)
	}
	if !tableHasColumn(t, db, "accounts", "password_storage") ||
		!tableHasColumn(t, db, "accounts", "smtp_password_storage") ||
		!tableHasColumn(t, db, "contact_sources", "password_storage") {
		t.Fatal("v49 columns missing after re-upgrade")
	}
	if err := db.QueryRow(`
		SELECT password_storage, smtp_password_storage FROM accounts WHERE id = 'account-1'
	`).Scan(&imapStorage, &smtpStorage); err != nil {
		t.Fatalf("read account markers after re-upgrade: %v", err)
	}
	if err := db.QueryRow(`
		SELECT password_storage FROM contact_sources WHERE id = 'source-1'
	`).Scan(&davStorage); err != nil {
		t.Fatalf("read contact source marker after re-upgrade: %v", err)
	}
	if imapStorage != "fallback" || smtpStorage != "fallback" || davStorage != "fallback" {
		t.Fatalf("re-upgraded markers = %q, %q, %q; want fallback", imapStorage, smtpStorage, davStorage)
	}
	assertDatabaseIntegrity(t, db)
}

func tableHasColumn(t *testing.T, db *DB, table, want string) bool {
	t.Helper()
	rows, err := db.Query("PRAGMA table_info(" + table + ")")
	if err != nil {
		t.Fatalf("table_info %s: %v", table, err)
	}
	defer rows.Close()

	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, dataType string
		var defaultValue any
		if err := rows.Scan(&cid, &name, &dataType, &notNull, &defaultValue, &primaryKey); err != nil {
			t.Fatalf("scan table_info %s: %v", table, err)
		}
		if name == want {
			return true
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("table_info %s: %v", table, err)
	}
	return false
}

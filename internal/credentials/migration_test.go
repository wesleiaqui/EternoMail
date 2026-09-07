package credentials

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/hkdb/aerion/internal/crypto"
	"github.com/hkdb/aerion/internal/database"
	gokeyring "github.com/zalando/go-keyring"
	"golang.org/x/crypto/pbkdf2"
)

func legacyStoreFixture(t *testing.T) (*database.DB, string, string) {
	t.Helper()
	t.Setenv("USER", "migration-user")
	gokeyring.MockInit()
	dir := t.TempDir()
	salt := make([]byte, 32)
	if _, err := rand.Read(salt); err != nil {
		t.Fatal(err)
	}
	hostname, err := os.Hostname()
	if err != nil {
		t.Fatal(err)
	}
	key := pbkdf2.Key([]byte(fmt.Sprintf("aerion:%s:%s:%d", hostname, os.Getenv("USER"), os.Getuid())), salt, 100000, 32, sha256.New)
	if err := os.WriteFile(filepath.Join(dir, "device.key"), append(salt, key...), 0600); err != nil {
		t.Fatal(err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatal(err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		t.Fatal(err)
	}
	ciphertext := base64.StdEncoding.EncodeToString(gcm.Seal(nonce, nonce, []byte("legacy secret"), nil))
	db, err := database.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.Migrate(); err != nil {
		t.Fatal(err)
	}
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := db.Exec(query, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec("INSERT INTO accounts (id,name,email,imap_host,smtp_host,username) VALUES ('a','test','a@test','imap','smtp','a')")
	exec("INSERT INTO smime_certificates (id,account_id,email,subject,issuer,serial_number,fingerprint,not_before,not_after,cert_chain_pem) VALUES ('s','a','a@test','','','','s','2020-01-01','2030-01-01','')")
	exec("INSERT INTO pgp_keys (id,account_id,email,key_id,fingerprint,user_id,algorithm,public_key_armored) VALUES ('p','a','a@test','','p','','','')")
	exec("INSERT INTO contact_sources (id,name,type,url) VALUES ('c','test','carddav','https://example.test')")
	exec("INSERT INTO extension_secrets (extension,key,encrypted_value,created_at) VALUES ('e','one',?,0),('e','two',?,0),('other','one',?,0),('empty','empty','',0)", ciphertext, ciphertext, ciphertext)
	exec("INSERT INTO oauth_tokens (account_id,client_config_id,provider) VALUES ('a','one','google'),('a','two','google')")
	s := &Store{db: db.DB}
	if err := s.ensureUserClientsTable(); err != nil {
		t.Fatal(err)
	}
	if err := s.ensureCustomProvidersTable(); err != nil {
		t.Fatal(err)
	}
	exec("INSERT INTO user_oauth_clients (config_id,encrypted) VALUES ('one',?)", ciphertext)
	exec("INSERT INTO oauth_custom_providers (account_id,encrypted) VALUES ('a',?)", ciphertext)
	// Explicit list guards against forgetting OAuth fields in the implementation.
	for table, columns := range map[string][]string{
		"accounts":           {"encrypted_password", "encrypted_smtp_password", "encrypted_access_token", "encrypted_refresh_token"},
		"contact_sources":    {"encrypted_password", "encrypted_access_token", "encrypted_refresh_token"},
		"smime_certificates": {"encrypted_private_key"}, "pgp_keys": {"encrypted_private_key"},
		"oauth_tokens": {"encrypted_access_token", "encrypted_refresh_token"},
	} {
		for _, column := range columns {
			exec("UPDATE "+table+" SET "+column+" = ?", ciphertext)
		}
	}
	return db, dir, ciphertext
}

func credentialSnapshot(t *testing.T, db *database.DB) []string {
	t.Helper()
	var values []string
	for _, query := range []string{
		"SELECT encrypted_password, encrypted_smtp_password, encrypted_access_token, encrypted_refresh_token FROM accounts ORDER BY id",
		"SELECT encrypted_private_key FROM smime_certificates ORDER BY id",
		"SELECT encrypted_private_key FROM pgp_keys ORDER BY id",
		"SELECT encrypted_value FROM extension_secrets WHERE encrypted_value != '' ORDER BY extension,key",
		"SELECT encrypted_password, encrypted_access_token, encrypted_refresh_token FROM contact_sources ORDER BY id",
		"SELECT encrypted_access_token, encrypted_refresh_token FROM oauth_tokens ORDER BY account_id,client_config_id",
		"SELECT encrypted FROM user_oauth_clients ORDER BY config_id",
		"SELECT encrypted FROM oauth_custom_providers ORDER BY account_id",
	} {
		rows, err := db.Query(query)
		if err != nil {
			t.Fatal(err)
		}
		cols, err := rows.Columns()
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			row := make([]string, len(cols))
			dest := make([]any, len(cols))
			for i := range row {
				dest[i] = &row[i]
			}
			if err := rows.Scan(dest...); err != nil {
				t.Fatal(err)
			}
			values = append(values, row...)
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		if err := rows.Close(); err != nil {
			t.Fatal(err)
		}
	}
	return values
}

func TestCredentialMigrationCompleteAndIdempotent(t *testing.T) {
	db, dir, original := legacyStoreFixture(t)
	s, err := NewStore(db.DB, dir)
	if err != nil {
		t.Fatal(err)
	}
	first := credentialSnapshot(t, db)
	for _, ciphertext := range first {
		plaintext, err := s.encryptor.Decrypt(ciphertext)
		if err != nil || plaintext != "legacy secret" || ciphertext == original {
			t.Fatalf("credential not migrated: %v", err)
		}
	}
	var empty string
	if err := db.QueryRow("SELECT encrypted_value FROM extension_secrets WHERE extension = 'empty'").Scan(&empty); err != nil || empty != "" {
		t.Fatal("empty credential changed", err)
	}
	_, legacy, version, err := crypto.NewEncryptor(dir)
	if err != nil || legacy != nil || version != crypto.KeyVersionNew {
		t.Fatal("migration not finalized", err)
	}
	if _, err := NewStore(db.DB, dir); err != nil {
		t.Fatal(err)
	}
	second := credentialSnapshot(t, db)
	for i := range first {
		if first[i] != second[i] {
			t.Fatal("second startup rewrote ciphertext")
		}
	}
}

func TestCredentialMigrationRollbackAndRestart(t *testing.T) {
	db, dir, original := legacyStoreFixture(t)
	// Fail after three columns have already been updated in the transaction.
	if _, err := db.Exec("CREATE TRIGGER fail_migration BEFORE UPDATE OF encrypted_private_key ON pgp_keys BEGIN SELECT RAISE(ABORT, 'injected failure'); END"); err != nil {
		t.Fatal(err)
	}
	if _, err := NewStore(db.DB, dir); err == nil {
		t.Fatal("expected migration failure")
	}
	for _, ciphertext := range credentialSnapshot(t, db) {
		if ciphertext != original {
			t.Fatal("partial update survived rollback")
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "device.key.legacy")); err != nil {
		t.Fatal("recovery material lost", err)
	}
	if _, err := db.Exec("DROP TRIGGER fail_migration"); err != nil {
		t.Fatal(err)
	}
	// Reopen both disk resources, without relying on the first encryptor.
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := database.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	s, err := NewStore(reopened.DB, dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, ciphertext := range credentialSnapshot(t, reopened) {
		plaintext, err := s.encryptor.Decrypt(ciphertext)
		if err != nil || plaintext != "legacy secret" {
			t.Fatal("restart failed to recover", err)
		}
	}
}

func TestCredentialMigrationInterruptedAfterCommit(t *testing.T) {
	db, dir, _ := legacyStoreFixture(t)
	enc, legacy, _, err := crypto.NewEncryptor(dir)
	if err != nil {
		t.Fatal(err)
	}
	s := &Store{db: db.DB, encryptor: enc}
	if err := s.reencryptAllCredentials(legacy, enc); err != nil {
		t.Fatal(err)
	}
	before := credentialSnapshot(t, db)
	// Simulate exit after commit but before CompleteKeyMigration.
	if _, err := NewStore(db.DB, dir); err != nil {
		t.Fatal(err)
	}
	after := credentialSnapshot(t, db)
	for i := range before {
		if before[i] != after[i] {
			t.Fatal("committed data rewritten")
		}
	}
}

func TestCredentialMigrationWithoutOptionalTables(t *testing.T) {
	db, dir, _ := legacyStoreFixture(t)
	for _, table := range []string{"user_oauth_clients", "oauth_custom_providers"} {
		if _, err := db.Exec("DROP TABLE " + table); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := NewStore(db.DB, dir); err != nil {
		t.Fatal(err)
	}
}

func TestCredentialMigrationRejectsCorruptCiphertext(t *testing.T) {
	db, dir, original := legacyStoreFixture(t)
	if _, err := db.Exec("UPDATE oauth_custom_providers SET encrypted = 'invalid-ciphertext'"); err != nil {
		t.Fatal(err)
	}
	if _, err := NewStore(db.DB, dir); err == nil {
		t.Fatal("accepted corrupt ciphertext")
	}
	values := credentialSnapshot(t, db)
	for _, value := range values[:len(values)-1] {
		if value != original {
			t.Fatal("earlier credentials changed despite rollback")
		}
	}
	if values[len(values)-1] != "invalid-ciphertext" {
		t.Fatal("corrupt value was overwritten")
	}
	if _, err := db.Exec("UPDATE oauth_custom_providers SET encrypted = ?", original); err != nil {
		t.Fatal(err)
	}
	if _, err := NewStore(db.DB, dir); err != nil {
		t.Fatal("retry after repair failed", err)
	}
}

func TestMissingKeyPreservesExistingCredentials(t *testing.T) {
	db, dir, original := legacyStoreFixture(t)
	if err := os.Remove(filepath.Join(dir, "device.key")); err != nil {
		t.Fatal(err)
	}
	if _, err := NewStore(db.DB, dir); err == nil {
		t.Fatal("created unrelated replacement key")
	}
	if _, err := os.Lstat(filepath.Join(dir, "device.key")); !os.IsNotExist(err) {
		t.Fatal("replacement exists", err)
	}
	for _, value := range credentialSnapshot(t, db) {
		if value != original {
			t.Fatal("credential changed")
		}
	}
}

func TestLegacyUpgradeAcrossClosedDatabase(t *testing.T) {
	db, dir, _ := legacyStoreFixture(t)
	keyFile, err := os.ReadFile(filepath.Join(dir, "device.key"))
	if err != nil {
		t.Fatal(err)
	}
	expected := map[string]bool{}
	for _, col := range credentialColumns {
		value := col.table + "." + col.column + ": synthetic credential"
		if col.column == "encrypted" {
			value = `{"clientId":"synthetic-client","clientSecret":"` + col.table + `-secret"}`
		}
		expected[value] = true
		block, err := aes.NewCipher(keyFile[32:])
		if err != nil {
			t.Fatal(err)
		}
		gcm, err := cipher.NewGCM(block)
		if err != nil {
			t.Fatal(err)
		}
		nonce := make([]byte, gcm.NonceSize())
		if _, err := rand.Read(nonce); err != nil {
			t.Fatal(err)
		}
		ct := base64.StdEncoding.EncodeToString(gcm.Seal(nonce, nonce, []byte(value), nil))
		query := fmt.Sprintf("UPDATE %s SET %s=? WHERE %s IS NOT NULL AND %s!=''", col.table, col.column, col.column, col.column)
		if _, err := db.Exec(query, ct); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := db.Exec("INSERT INTO accounts (id,name,email,imap_host,smtp_host,username,encrypted_password,encrypted_smtp_password) VALUES ('empty','empty','empty@test','imap','smtp','empty',NULL,'')"); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := database.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	s, err := NewStore(reopened.DB, dir)
	if err != nil {
		t.Fatal(err)
	}
	var password, smtp sql.NullString
	if err := reopened.QueryRow("SELECT encrypted_password,encrypted_smtp_password FROM accounts WHERE id='empty'").Scan(&password, &smtp); err != nil || password.Valid || !smtp.Valid || smtp.String != "" {
		t.Fatal("NULL/empty changed", err)
	}
	// Keep the existing snapshot's string-only scans focused on populated rows.
	if _, err := reopened.Exec("DELETE FROM accounts WHERE id='empty'"); err != nil {
		t.Fatal(err)
	}
	first := credentialSnapshot(t, reopened)
	for _, ct := range first {
		if value, err := s.encryptor.Decrypt(ct); err != nil || !expected[value] {
			t.Fatal("upgrade changed plaintext", err)
		}
	}
	if err := reopened.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err = database.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	s, err = NewStore(reopened.DB, dir)
	if err != nil {
		t.Fatal(err)
	}
	second := credentialSnapshot(t, reopened)
	for i := range first {
		if first[i] != second[i] {
			t.Fatal("second startup migrated again")
		}
	}
	s.keyringEnabled = false
	if err := s.SetPassword("a", "new v2 password"); err != nil {
		t.Fatal(err)
	}
	enc, legacy, version, err := crypto.NewEncryptor(dir)
	if err != nil || legacy != nil || version != crypto.KeyVersionNew {
		t.Fatal("unexpected legacy state", err)
	}
	var ct string
	if err := reopened.QueryRow("SELECT encrypted_password FROM accounts WHERE id='a'").Scan(&ct); err != nil {
		t.Fatal(err)
	}
	if plain, err := enc.Decrypt(ct); err != nil || plain != "new v2 password" {
		t.Fatal("new credential not v2", err)
	}
}

package credentials

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hkdb/aerion/internal/crypto"
	"github.com/hkdb/aerion/internal/database"
	gokeyring "github.com/zalando/go-keyring"
)

// Independently derive the persistent-column inventory from the actual upgraded
// SQLite schema, including the lazily created OAuth configuration tables.
func TestMigrationCoversEveryEncryptedSchemaColumn(t *testing.T) {
	db, _, _ := legacyStoreFixture(t)
	rows, err := db.Query(`SELECT m.name,p.name FROM sqlite_master AS m JOIN pragma_table_info(m.name) AS p WHERE m.type='table' AND p.name LIKE '%encrypted%'`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	expected := map[string]bool{}
	for _, col := range credentialColumns {
		key := col.table + "." + col.column
		if expected[key] {
			t.Fatal("duplicate migration column", key)
		}
		expected[key] = true
	}
	// These columns are protocol blobs or boolean flags, not device encryption.
	excluded := map[string]bool{"drafts.encrypted": true, "drafts.encrypted_body": true, "drafts.pgp_encrypted": true, "drafts.pgp_encrypted_body": true, "messages.smime_encrypted": true, "messages.pgp_encrypted": true}
	count := 0
	for rows.Next() {
		var table, column string
		if err := rows.Scan(&table, &column); err != nil {
			t.Fatal(err)
		}
		key := table + "." + column
		if excluded[key] {
			delete(excluded, key)
			continue
		}
		if !expected[key] {
			t.Error("encrypted schema column omitted", key)
		}
		delete(expected, key)
		count++
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if count != 14 || len(expected) != 0 || len(excluded) != 0 {
		t.Fatalf("schema=%d missing=%v", count, expected)
	}
}

// This helper runs in independent OS processes. os.Exit deliberately bypasses
// defers, exercising SQLite rollback and durable recovery through kernel close.
func TestMigrationProcessHelper(t *testing.T) {
	dir := os.Getenv("ANCLO_ROUND3_PROCESS_DIR")
	if dir == "" {
		return
	}
	gokeyring.MockInit()
	db, err := database.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	mode := os.Getenv("ANCLO_ROUND3_PROCESS_MODE")
	if mode == "startup" {
		s, err := NewStore(db.DB, dir)
		if err != nil {
			t.Fatal(err)
		}
		var ct string
		if err := db.QueryRow("SELECT encrypted_password FROM accounts WHERE id='a'").Scan(&ct); err != nil {
			t.Fatal(err)
		}
		if value, err := s.encryptor.Decrypt(ct); err != nil || value != "legacy secret" {
			t.Fatal("concurrent process cannot read credentials", err)
		}
		db.Close()
		return
	}
	lock, err := crypto.LockCredentialMigration(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	enc, legacy, _, err := crypto.NewEncryptor(dir)
	if err != nil {
		t.Fatal(err)
	}
	switch mode {
	case "before-transaction":
	case "during-transaction":
		tx, err := db.Begin()
		if err != nil {
			t.Fatal(err)
		}
		ct, err := enc.Encrypt("uncommitted change")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec("UPDATE accounts SET encrypted_password=?", ct); err != nil {
			t.Fatal(err)
		}
	case "after-commit":
		s := &Store{db: db.DB, encryptor: enc}
		if err := s.reencryptAllCredentials(legacy, enc); err != nil {
			t.Fatal(err)
		}
	default:
		t.Fatal("unknown helper mode")
	}
	os.Exit(0)
}

func migrationProcess(t *testing.T, dir, mode string) *exec.Cmd {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(exe, "-test.run=^TestMigrationProcessHelper$", "-test.timeout=90s")
	cmd.Env = append(os.Environ(), "ANCLO_ROUND3_PROCESS_DIR="+dir, "ANCLO_ROUND3_PROCESS_MODE="+mode)
	return cmd
}

func TestMigrationAcrossProcessExit(t *testing.T) {
	for _, mode := range []string{"before-transaction", "during-transaction", "after-commit"} {
		t.Run(mode, func(t *testing.T) {
			db, dir, original := legacyStoreFixture(t)
			if err := db.Close(); err != nil {
				t.Fatal(err)
			}
			if output, err := migrationProcess(t, dir, mode).CombinedOutput(); err != nil {
				t.Fatalf("helper: %v %s", err, output)
			}
			reopened, err := database.Open(filepath.Join(dir, "test.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer reopened.Close()
			before := credentialSnapshot(t, reopened)
			if mode != "after-commit" {
				for _, ct := range before {
					if ct != original {
						t.Fatal("uncommitted data survived process exit")
					}
				}
			}
			if _, err := os.Stat(filepath.Join(dir, "device.key.legacy")); err != nil {
				t.Fatal("recovery material missing", err)
			}
			s, err := NewStore(reopened.DB, dir)
			if err != nil {
				t.Fatal(err)
			}
			after := credentialSnapshot(t, reopened)
			for i, ct := range after {
				if value, err := s.encryptor.Decrypt(ct); err != nil || value != "legacy secret" {
					t.Fatal("recovery changed plaintext", err)
				}
				if mode == "after-commit" && ct != before[i] {
					t.Fatal("committed v2 data unnecessarily rewritten")
				}
			}
		})
	}
}

func TestConcurrentStoreProcesses(t *testing.T) {
	db, dir, _ := legacyStoreFixture(t)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	const count = 3
	commands := make([]*exec.Cmd, count)
	outputs := make([]bytes.Buffer, count)
	for i := range commands {
		commands[i] = migrationProcess(t, dir, "startup")
		commands[i].Stdout = &outputs[i]
		commands[i].Stderr = &outputs[i]
		if err := commands[i].Start(); err != nil {
			t.Fatal(err)
		}
	}
	for i, cmd := range commands {
		if err := cmd.Wait(); err != nil {
			t.Fatalf("process %d: %v %s", i, err, outputs[i].String())
		}
	}
	if _, legacy, version, err := crypto.NewEncryptor(dir); err != nil || legacy != nil || version != crypto.KeyVersionNew {
		t.Fatal("concurrent migration incomplete", err)
	}
}

// Document and guard every production source file that calls the device
// Encryptor. Schema inventory above covers the concrete persistent destinations.
func TestEncryptorWriterInventory(t *testing.T) {
	want := map[string]bool{"store.go": true, "oauth.go": true, "oauth_clientconfig.go": true, "oauth_user_creds.go": true, "oauth_custom_provider.go": true}
	err := filepath.WalkDir("..", func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if !bytes.Contains(b, []byte(".encryptor.Encrypt(")) {
			return nil
		}
		if filepath.Dir(path) != filepath.Join("..", "credentials") || !want[filepath.Base(path)] {
			return fmt.Errorf("new Encryptor writer requires migration review: %s", path)
		}
		delete(want, filepath.Base(path))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(want) != 0 {
		t.Fatal("writer inventory stale", want)
	}
}

func TestMigrationDoesNotReencryptProtocolBlobs(t *testing.T) {
	db, dir, _ := legacyStoreFixture(t)
	smime := []byte{0x30, 0x03, 0x02, 0x01, 0x01}
	pgp := []byte("synthetic PGP protocol blob")
	if _, err := db.Exec("INSERT INTO drafts (id,account_id,encrypted,encrypted_body,pgp_encrypted,pgp_encrypted_body) VALUES ('d','a',1,?,1,?)", smime, pgp); err != nil {
		t.Fatal(err)
	}
	if _, err := NewStore(db.DB, dir); err != nil {
		t.Fatal(err)
	}
	var gotSMIME, gotPGP []byte
	var smimeFlag, pgpFlag int
	if err := db.QueryRow("SELECT encrypted,encrypted_body,pgp_encrypted,pgp_encrypted_body FROM drafts WHERE id='d'").Scan(&smimeFlag, &gotSMIME, &pgpFlag, &gotPGP); err != nil {
		t.Fatal(err)
	}
	if smimeFlag != 1 || pgpFlag != 1 || !bytes.Equal(smime, gotSMIME) || !bytes.Equal(pgp, gotPGP) {
		t.Fatal("protocol encryption was changed")
	}
}

package credentials

import (
	"errors"
	"strings"
	"testing"

	"github.com/hkdb/aerion/internal/database"
	gokeyring "github.com/zalando/go-keyring"
)

func TestPasswordStorageSelectsCurrentSource(t *testing.T) {
	gokeyring.MockInit()
	s, db := newTestStore(t)
	insertTestAccount(t, db, "account")
	s.keyringEnabled = true

	if err := s.SetPassword("account", "old-password"); err != nil {
		t.Fatal(err)
	}
	if got, err := s.GetPassword("account"); err != nil || got != "old-password" {
		t.Fatalf("keyring password = %q, %v", got, err)
	}

	// Simulate a write while the keyring is unavailable, leaving its old entry
	// readable when it returns.
	s.keyringEnabled = false
	if err := s.SetPassword("account", "new-password"); err != nil {
		t.Fatal(err)
	}
	s.keyringEnabled = true
	if got, err := s.GetPassword("account"); err != nil || got != "new-password" {
		t.Fatalf("fallback password lost to stale keyring entry: %q, %v", got, err)
	}

	// A repeated fallback write remains authoritative.
	s.keyringEnabled = false
	if err := s.SetPassword("account", "newest-password"); err != nil {
		t.Fatal(err)
	}
	s.keyringEnabled = true
	if got, err := s.GetPassword("account"); err != nil || got != "newest-password" {
		t.Fatalf("newest fallback password = %q, %v", got, err)
	}

	// Once the keyring works again, a successful new write becomes current and
	// clears the fallback ciphertext.
	if err := s.SetPassword("account", "keyring-password"); err != nil {
		t.Fatal(err)
	}
	if got, err := s.GetPassword("account"); err != nil || got != "keyring-password" {
		t.Fatalf("keyring password after recovery = %q, %v", got, err)
	}
	var storage string
	if err := db.QueryRow("SELECT password_storage FROM accounts WHERE id = 'account'").Scan(&storage); err != nil || storage != credentialStorageKeyring {
		t.Fatalf("password storage = %q, %v", storage, err)
	}
}

func TestPasswordStorageFallsBackWhenKeyringWriteFails(t *testing.T) {
	gokeyring.MockInitWithError(errors.New("keyring unavailable"))
	s, db := newTestStore(t)
	insertTestAccount(t, db, "account")
	s.keyringEnabled = true

	if err := s.SetPassword("account", "fallback-password"); err != nil {
		t.Fatal(err)
	}
	if got, err := s.GetPassword("account"); err != nil || got != "fallback-password" {
		t.Fatalf("fallback password = %q, %v", got, err)
	}
}

func TestPasswordStorageDeletionSuppressesStaleKeyring(t *testing.T) {
	gokeyring.MockInit()
	s, db := newTestStore(t)
	insertTestAccount(t, db, "account")
	s.keyringEnabled = true
	if err := s.SetPassword("account", "old-password"); err != nil {
		t.Fatal(err)
	}

	// Deletion while the keyring is unavailable cannot remove the old physical
	// entry, so the persisted marker must still prevent its resurrection.
	s.keyringEnabled = false
	if err := s.DeletePassword("account"); err != nil {
		t.Fatal(err)
	}
	s.keyringEnabled = true
	if _, err := s.GetPassword("account"); !errors.Is(err, ErrCredentialNotFound) {
		t.Fatalf("deleted password returned %v, want ErrCredentialNotFound", err)
	}
}

func TestSMTPAndCardDAVPasswordStorageUseCurrentSource(t *testing.T) {
	gokeyring.MockInit()
	s, db := newTestStore(t)
	insertTestAccount(t, db, "account")
	if _, err := db.Exec(`INSERT INTO contact_sources (id, name, type, url) VALUES ('source', 'Source', 'carddav', 'https://dav.example.test')`); err != nil {
		t.Fatal(err)
	}
	s.keyringEnabled = true

	if err := s.SetSMTPPassword("account", "smtp-old"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetCardDAVPassword("source", "dav-old"); err != nil {
		t.Fatal(err)
	}
	s.keyringEnabled = false
	if err := s.SetSMTPPassword("account", "smtp-new"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetCardDAVPassword("source", "dav-new"); err != nil {
		t.Fatal(err)
	}
	s.keyringEnabled = true

	if got, err := s.GetSMTPPassword("account"); err != nil || got != "smtp-new" {
		t.Fatalf("SMTP password = %q, %v", got, err)
	}
	if got, err := s.GetCardDAVPassword("source"); err != nil || got != "dav-new" {
		t.Fatalf("CardDAV password = %q, %v", got, err)
	}
}

func TestPasswordStorageReturnsErrorWhenFallbackFails(t *testing.T) {
	gokeyring.MockInit()
	s, db := newTestStore(t)
	insertTestAccount(t, db, "account")
	s.keyringEnabled = false
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	err := s.SetPassword("account", "secret-password")
	if err == nil {
		t.Fatal("SetPassword succeeded with both stores unavailable")
	}
	if strings.Contains(err.Error(), "secret-password") {
		t.Fatalf("password leaked through error: %v", err)
	}
}

func TestCredentialSnapshotRestoresExactSource(t *testing.T) {
	gokeyring.MockInit()
	s, db := newTestStore(t)
	insertTestAccount(t, db, "account")
	s.keyringEnabled = true

	// A fallback snapshot remains fallback even when the keyring is available.
	s.keyringEnabled = false
	if err := s.SetPassword("account", "fallback-password"); err != nil {
		t.Fatal(err)
	}
	s.keyringEnabled = true
	fallback, err := s.CapturePassword("account")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetPassword("account", "new-password"); err != nil {
		t.Fatal(err)
	}
	if err := s.RestorePassword("account", fallback); err != nil {
		t.Fatal(err)
	}
	assertPasswordStorage(t, db, "password_storage", credentialStorageFallback)
	if got, err := s.GetPassword("account"); err != nil || got != "fallback-password" {
		t.Fatalf("fallback restore = %q, %v", got, err)
	}

	// A keyring snapshot remains keyring.
	// Capture the actual keyring representation after making it current.
	if err := s.SetPassword("account", "keyring-password"); err != nil {
		t.Fatal(err)
	}
	keyring, err := s.CapturePassword("account")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetPassword("account", "another-password"); err != nil {
		t.Fatal(err)
	}
	if err := s.RestorePassword("account", keyring); err != nil {
		t.Fatal(err)
	}
	assertPasswordStorage(t, db, "password_storage", credentialStorageKeyring)
	if got, err := s.GetPassword("account"); err != nil || got != "keyring-password" {
		t.Fatalf("keyring restore = %q, %v", got, err)
	}

	// Deleted must suppress any residual keyring value after restoration.
	s.keyringEnabled = false
	if err := s.DeletePassword("account"); err != nil {
		t.Fatal(err)
	}
	s.keyringEnabled = true
	deleted, err := s.CapturePassword("account")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetPassword("account", "residual-password"); err != nil {
		t.Fatal(err)
	}
	if err := s.RestorePassword("account", deleted); err != nil {
		t.Fatal(err)
	}
	assertPasswordStorage(t, db, "password_storage", credentialStorageDeleted)
	if _, err := s.GetPassword("account"); !errors.Is(err, ErrCredentialNotFound) {
		t.Fatalf("deleted restore returned %v", err)
	}
}

func TestCredentialSnapshotRestoresLegacyAbsenceAndRejectsEmptyKeyringValue(t *testing.T) {
	gokeyring.MockInit()
	s, db := newTestStore(t)
	insertTestAccount(t, db, "account")
	s.keyringEnabled = true

	legacy, err := s.CapturePassword("account")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetPassword("account", "new-password"); err != nil {
		t.Fatal(err)
	}
	if err := s.RestorePassword("account", legacy); err != nil {
		t.Fatal(err)
	}
	assertPasswordStorage(t, db, "password_storage", "")
	if _, err := s.GetPassword("account"); !errors.Is(err, ErrCredentialNotFound) {
		t.Fatalf("legacy absence returned %v", err)
	}

	if err := gokeyring.Set(serviceName, "account", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("UPDATE accounts SET password_storage = ? WHERE id = ?", credentialStorageKeyring, "account"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetPassword("account"); !errors.Is(err, ErrCredentialNotFound) {
		t.Fatalf("empty keyring value returned %v", err)
	}
}

func assertPasswordStorage(t *testing.T, db *database.DB, column, want string) {
	t.Helper()
	var got string
	if err := db.QueryRow("SELECT " + column + " FROM accounts WHERE id = 'account'").Scan(&got); err != nil || got != want {
		t.Fatalf("%s = %q, %v; want %q", column, got, err, want)
	}
}

package crypto

import (
	"bytes"
	"crypto/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func keyFixture(t *testing.T, legacy bool, versioned bool) (string, []byte, *Encryptor) {
	t.Helper()
	salt := make([]byte, saltSize)
	if _, err := rand.Read(salt); err != nil {
		t.Fatal(err)
	}
	key := deriveKeyV2(salt)
	if legacy {
		key = deriveKeyV1(salt)
	}
	data := append(append([]byte{}, salt...), key...)
	if versioned {
		data = append([]byte{keyFormatVersion2}, data...)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, keyFileName), data, 0600); err != nil {
		t.Fatal(err)
	}
	return dir, data, &Encryptor{key: key}
}

func TestVersionedKeyAndUnversionedV2Upgrade(t *testing.T) {
	for _, versioned := range []bool{true, false} {
		name := "upgrade-unversioned-v2"
		if versioned {
			name = "existing-v2"
		}
		t.Run(name, func(t *testing.T) {
			dir, original, old := keyFixture(t, false, versioned)
			before, err := old.Encrypt("existing credential")
			if err != nil {
				t.Fatal(err)
			}
			enc, legacy, version, err := NewEncryptor(dir)
			if err != nil {
				t.Fatal(err)
			}
			if legacy != nil || version != KeyVersionNew {
				t.Fatal("unexpected legacy migration")
			}
			data, err := os.ReadFile(filepath.Join(dir, keyFileName))
			if err != nil {
				t.Fatal(err)
			}
			if len(data) != keyFileSizeV2 || data[0] != keyFormatVersion2 {
				t.Fatalf("invalid v2 file: %x", data[:1])
			}
			want := original
			if !versioned {
				want = append([]byte{keyFormatVersion2}, original...)
			}
			if !bytes.Equal(data, want) {
				t.Fatal("key or salt changed during format upgrade")
			}
			plaintext, err := enc.Decrypt(before)
			if err != nil || plaintext != "existing credential" {
				t.Fatalf("old ciphertext unreadable: %v", err)
			}
			after, err := enc.Encrypt("new credential")
			if err != nil {
				t.Fatal(err)
			}
			// Reopening checks persisted state, not just an in-memory round trip.
			reopened, _, _, err := NewEncryptor(dir)
			if err != nil {
				t.Fatal(err)
			}
			plaintext, err = reopened.Decrypt(after)
			if err != nil || plaintext != "new credential" {
				t.Fatalf("new ciphertext unreadable after restart: %v", err)
			}
		})
	}
}

func TestLegacyKeyMigrationAndRecovery(t *testing.T) {
	t.Setenv("USER", "legacy-user")
	dir, _, old := keyFixture(t, true, false)
	ciphertext, err := old.Encrypt("existing legacy credential")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		enc, legacy, version, err := NewEncryptor(dir)
		if err != nil || enc == nil || legacy == nil || version != KeyVersionLegacy {
			t.Fatalf("migration/recovery: %v, version %v", err, version)
		}
		got, err := os.ReadFile(filepath.Join(dir, keyFileName))
		if err != nil || len(got) != keyFileSizeV2 || got[0] != keyFormatVersion2 {
			t.Fatal("invalid migrated key")
		}
		plaintext, err := legacy.Decrypt(ciphertext)
		if err != nil || plaintext != "existing legacy credential" {
			t.Fatalf("legacy data lost: %v", err)
		}
		if _, err := enc.Decrypt(ciphertext); err == nil {
			t.Fatal("legacy ciphertext accepted by v2")
		}
	}
	if err := CompleteKeyMigration(dir); err != nil {
		t.Fatal(err)
	}
	_, legacy, version, err := NewEncryptor(dir)
	if err != nil || legacy != nil || version != KeyVersionNew {
		t.Fatalf("completion: %v", err)
	}
}

func TestInvalidKeyFormatsPreserved(t *testing.T) {
	for _, tc := range []struct {
		name    string
		data    []byte
		message string
	}{
		{"size", bytes.Repeat([]byte{1}, 10), "unexpected size"},
		{"version", append([]byte{0x99}, make([]byte, keyFileSizeV1)...), "unexpected version byte 0x99"},
		{"unrecognized-legacy", bytes.Repeat([]byte{42}, keyFileSizeV1), "cannot be unlocked"},
		{"corrupt-v2", append([]byte{keyFormatVersion2}, make([]byte, keyFileSizeV1)...), "cannot be unlocked"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, keyFileName)
			if err := os.WriteFile(path, tc.data, 0600); err != nil {
				t.Fatal(err)
			}
			if _, _, _, err := NewEncryptor(dir); err == nil || !strings.Contains(err.Error(), tc.message) {
				t.Fatalf("unexpected error: %v", err)
			}
			got, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(got, tc.data) {
				t.Fatal("invalid key overwritten")
			}
		})
	}
}

func TestNewV2KeyWithOrphanTemporaryFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, keyFileName)
	// Stale temporary files must not affect creating a valid private key.
	if err := os.WriteFile(path+".tmp", []byte("interrupted write"), 0644); err != nil {
		t.Fatal(err)
	}
	enc, _, _, err := NewEncryptor(dir)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() != keyFileSizeV2 || info.Mode().Perm() != 0600 {
		t.Fatalf("size/mode: %d %o", info.Size(), info.Mode().Perm())
	}
	ct, err := enc.Encrypt("secret")
	if err != nil {
		t.Fatal(err)
	}
	reopened, _, _, err := NewEncryptor(dir)
	if err != nil {
		t.Fatal(err)
	}
	plaintext, err := reopened.Decrypt(ct)
	if err != nil || plaintext != "secret" {
		t.Fatalf("reopen: %v", err)
	}
	matches, err := filepath.Glob(filepath.Join(dir, ".device-key-*"))
	if err != nil || len(matches) != 0 {
		t.Fatalf("temporary files leaked: %v %v", matches, err)
	}
}

func TestAtomicWriteFailurePreservesDestination(t *testing.T) {
	path := filepath.Join(t.TempDir(), keyFileName)
	// A nonempty directory forces rename to fail even when running as root.
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(path, "original")
	if err := os.WriteFile(marker, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := writeKeyV2(path, make([]byte, saltSize), make([]byte, keySize)); err == nil {
		t.Fatal("expected rename failure")
	}
	got, err := os.ReadFile(marker)
	if err != nil || string(got) != "original" {
		t.Fatal("destination changed")
	}
	matches, err := filepath.Glob(filepath.Join(filepath.Dir(path), ".device-key-*"))
	if err != nil || len(matches) != 0 {
		t.Fatalf("temporary files leaked: %v %v", matches, err)
	}
}

func TestPendingMigrationSurvivesEnvironmentChange(t *testing.T) {
	t.Setenv("USER", "original-user")
	dir, _, old := keyFixture(t, true, false)
	ciphertext, err := old.Encrypt("secret")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := NewEncryptor(dir); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(dir, keyFileName) + ".legacy")
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("recovery key permissions", err)
	}
	t.Setenv("USER", "changed-user")
	_, legacy, version, err := NewEncryptor(dir)
	if err != nil || version != KeyVersionLegacy || legacy == nil {
		t.Fatal("could not resume", err)
	}
	plaintext, err := legacy.Decrypt(ciphertext)
	if err != nil || plaintext != "secret" {
		t.Fatal("legacy key lost after environment change", err)
	}
}

func TestMissingDeviceKeyWithPendingRecoveryIsNotRecreated(t *testing.T) {
	dir, _, _ := keyFixture(t, true, false)
	if _, _, _, err := NewEncryptor(dir); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, keyFileName)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := NewEncryptor(dir); err == nil {
		t.Fatal("replaced missing migration key")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("unrelated key created")
	}
}

func TestDanglingRecoverySymlinkDoesNotCreateKey(t *testing.T) {
	dir := t.TempDir()
	if err := os.Symlink(filepath.Join(dir, "missing"), filepath.Join(dir, keyFileName)+".legacy"); err != nil {
		t.Skip(err)
	}
	if _, _, _, err := NewEncryptor(dir); err == nil {
		t.Fatal("accepted dangling recovery link")
	}
	if _, err := os.Lstat(filepath.Join(dir, keyFileName)); !os.IsNotExist(err) {
		t.Fatal("created unrelated key", err)
	}
}

func TestConcurrentEncryptorsUseSameKey(t *testing.T) {
	dir := t.TempDir()
	const count = 8
	results := make(chan *Encryptor, count)
	errs := make(chan error, count)
	for i := 0; i < count; i++ {
		go func() { enc, _, _, err := NewEncryptor(dir); results <- enc; errs <- err }()
	}
	var first *Encryptor
	for i := 0; i < count; i++ {
		enc := <-results
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
		if enc == nil {
			t.Fatal("nil encryptor")
		}
		if first == nil {
			first = enc
		}
		if !bytes.Equal(first.key, enc.key) {
			t.Fatal("concurrent startup generated different keys")
		}
	}
}

func TestKeyLockRejectsSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	if err := os.WriteFile(target, []byte("original"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(dir, ".device-key.lock")); err != nil {
		t.Skip(err)
	}
	if _, _, _, err := NewEncryptor(dir); err == nil {
		t.Fatal("accepted lock symlink")
	}
	data, err := os.ReadFile(target)
	if err != nil || string(data) != "original" {
		t.Fatal("changed target", err)
	}
}

func TestRecoveryBeforeKeyReplacement(t *testing.T) {
	t.Setenv("USER", "before-interruption")
	dir, original, old := keyFixture(t, true, false)
	// Simulate the durable recovery write followed by exit before device.key rename.
	if err := writeKeyV2(filepath.Join(dir, keyFileName)+".legacy", original[:saltSize], original[saltSize:]); err != nil {
		t.Fatal(err)
	}
	t.Setenv("USER", "after-interruption")
	enc, legacy, version, err := NewEncryptor(dir)
	if err != nil || enc == nil || legacy == nil || version != KeyVersionLegacy {
		t.Fatal("could not resume before rename", err)
	}
	if !bytes.Equal(legacy.key, old.key) {
		t.Fatal("legacy material changed")
	}
}

func TestInvalidRecoveryPreservesBothFiles(t *testing.T) {
	for _, v2 := range []bool{false, true} {
		dir, original, _ := keyFixture(t, !v2, v2)
		bad := []byte("invalid recovery envelope")
		recovery := filepath.Join(dir, keyFileName) + ".legacy"
		if err := os.WriteFile(recovery, bad, 0600); err != nil {
			t.Fatal(err)
		}
		if _, _, _, err := NewEncryptor(dir); err == nil {
			t.Fatal("accepted invalid recovery")
		}
		got, err := os.ReadFile(filepath.Join(dir, keyFileName))
		if err != nil || !bytes.Equal(got, original) {
			t.Fatal("device key changed", err)
		}
		got, err = os.ReadFile(recovery)
		if err != nil || !bytes.Equal(got, bad) {
			t.Fatal("recovery changed", err)
		}
	}
}

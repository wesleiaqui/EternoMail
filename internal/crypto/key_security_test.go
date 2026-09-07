package crypto

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestDeriveKeyIndependentOfEnvironment(t *testing.T) {
	salt := bytes.Repeat([]byte{1}, saltSize)
	t.Setenv("USER", "first")
	t.Setenv("USERNAME", "first")
	first := deriveKeyV2(salt)
	t.Setenv("USER", "second")
	t.Setenv("USERNAME", "second")
	if !bytes.Equal(first, deriveKeyV2(salt)) {
		t.Fatal("key depends on environment")
	}
}
func TestCorruptKeyIsPreserved(t *testing.T) {
	for _, size := range []int{0, 1, keySize + saltSize - 1, keySize + saltSize + 1} {
		path := filepath.Join(t.TempDir(), keyFileName)
		original := bytes.Repeat([]byte{42}, size)
		if err := os.WriteFile(path, original, 0600); err != nil {
			t.Fatal(err)
		}
		if _, _, err := loadOrCreateKey(path); err == nil {
			t.Fatalf("accepted size %d", size)
		}
		got, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(got, original) {
			t.Fatal("corrupt key overwritten")
		}
	}
}
func TestUnreadableKeyIsNotReplaced(t *testing.T) {
	path := filepath.Join(t.TempDir(), keyFileName)
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	if _, _, err := loadOrCreateKey(path); err == nil {
		t.Fatal("ignored IO error")
	}
}

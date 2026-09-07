// Package crypto provides encryption utilities for secure credential storage
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"

	"golang.org/x/crypto/pbkdf2"
)

const (
	// keyFileName is the name of the file storing the encryption key
	keyFileName = "device.key"

	// saltSize is the size of the salt in bytes
	saltSize = 32

	// keySize is the size of the derived key in bytes (AES-256)
	keySize = 32

	// pbkdf2Iterations is the number of PBKDF2 iterations
	pbkdf2Iterations = 100000

	// v1 has no version byte: [salt 32B][key 32B].
	// v2 is [0x02][salt 32B][key 32B]. Length disambiguates random salts.
	keyFormatVersion2 = byte(0x02)
	keyFileSizeV1     = saltSize + keySize
	keyFileSizeV2     = 1 + saltSize + keySize
	// Planned release for removal of legacy derivation support.
	keyFormatDeprecationVersion = "0.5.0"
)

// KeyVersion identifies whether database credential migration is pending.
type KeyVersion int

const (
	KeyVersionNew    KeyVersion = 2 // Current format, no pending credential migration.
	KeyVersionLegacy KeyVersion = 1 // Legacy migration, including interrupted attempts.
)

// Encryptor provides AES-256-GCM encryption/decryption
type Encryptor struct {
	key []byte
}

// NewEncryptor creates a new Encryptor using a device-specific key
// The key is stored in the data directory and generated if it doesn't exist.
// When migration is pending, both encryptors are returned. The caller must
// commit re-encryption of all database credentials before CompleteKeyMigration.
func NewEncryptor(dataDir string) (*Encryptor, *Encryptor, KeyVersion, error) {
	lock, err := acquireKeyLock(dataDir, ".device-key.lock")
	if err != nil {
		return nil, nil, 0, err
	}
	defer lock.Close()
	keyPath := filepath.Join(dataDir, keyFileName)
	key, _, err := loadOrCreateKey(keyPath)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("failed to load or create key: %w", err)
	}
	enc := &Encryptor{key: key}
	// A durable recovery file bridges the filesystem write and SQLite commit.
	// It remains available after rollback and after an interrupted startup.
	pending, err := readKeyFile(keyPath + ".legacy")
	if os.IsNotExist(err) {
		return enc, nil, KeyVersionNew, nil
	}
	if err != nil {
		return nil, nil, 0, fmt.Errorf("read migration recovery key: %w", err)
	}
	if len(pending) != keyFileSizeV2 || pending[0] != keyFormatVersion2 ||
		!bytesEqual(deriveKeyV2(pending[1:1+saltSize]), key) {
		return nil, nil, 0, fmt.Errorf("invalid migration recovery key")
	}
	return enc, &Encryptor{key: pending[1+saltSize:]}, KeyVersionLegacy, nil
}

// CompleteKeyMigration removes recovery material only after the database commits.
func CompleteKeyMigration(dataDir string) error {
	lock, err := acquireKeyLock(dataDir, ".device-key.lock")
	if err != nil {
		return err
	}
	defer lock.Close()
	err = os.Remove(filepath.Join(dataDir, keyFileName) + ".legacy")
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	return syncKeyDirectory(filepath.Join(dataDir, keyFileName))
}

// loadOrCreateKey validates existing keys before making changes.
func loadOrCreateKey(keyPath string) ([]byte, KeyVersion, error) {
	data, err := readKeyFile(keyPath)
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, 0, fmt.Errorf("failed to read device key: %w", err)
		}
		// Never create a new unrelated key when recovery material exists.
		if _, recoveryErr := os.Lstat(keyPath + ".legacy"); !os.IsNotExist(recoveryErr) {
			return nil, 0, fmt.Errorf("device key missing while migration recovery file exists or is unreadable")
		}
		key, err := createKeyV2(keyPath)
		if err != nil {
			return nil, 0, err
		}
		return key, KeyVersionNew, nil
	}
	switch len(data) {
	case keyFileSizeV2:
		if data[0] != keyFormatVersion2 {
			return nil, 0, fmt.Errorf("device key has unexpected version byte 0x%02x (expected 0x%02x); file may be corrupted", data[0], keyFormatVersion2)
		}
		key := deriveKeyV2(data[1 : 1+saltSize])
		if !bytesEqual(key, data[1+saltSize:]) {
			return nil, 0, fmt.Errorf("device key cannot be unlocked; file may be corrupted or belong to another machine or user account")
		}
		return key, KeyVersionNew, nil
	case keyFileSizeV1:
		return migrateV1ToV2(keyPath, data)
	default:
		return nil, 0, fmt.Errorf("device key has unexpected size %d (expected %d for v2 or %d for v1 legacy); file may be corrupted", len(data), keyFileSizeV2, keyFileSizeV1)
	}
}

func migrateV1ToV2(keyPath string, data []byte) ([]byte, KeyVersion, error) {
	if len(data) != keyFileSizeV1 {
		return nil, 0, fmt.Errorf("invalid legacy device key size %d", len(data))
	}
	salt, storedKey := data[:saltSize], data[saltSize:]
	keyV2 := deriveKeyV2(salt)
	version := KeyVersionNew
	if !bytesEqual(keyV2, storedKey) {
		// Persist the old key before replacing device.key. The recovery file uses
		// the same envelope, but stores the legacy key for this salt.
		pending, err := readKeyFile(keyPath + ".legacy")
		if err == nil && (len(pending) != keyFileSizeV2 || pending[0] != keyFormatVersion2 || !bytesEqual(pending[1:1+saltSize], salt) || !bytesEqual(pending[1+saltSize:], storedKey)) {
			return nil, 0, fmt.Errorf("invalid migration recovery key")
		}
		if err != nil && !os.IsNotExist(err) {
			return nil, 0, err
		}
		// A matching recovery envelope was durably written by a prior
		// migration before replacing device.key. Resume it even if USER changed.
		if os.IsNotExist(err) && !bytesEqual(deriveKeyV1(salt), storedKey) {
			return nil, 0, fmt.Errorf("device key at %q cannot be unlocked by any known derivation formula", keyPath)
		}
		if err := writeKeyV2(keyPath+".legacy", salt, storedKey); err != nil {
			return nil, 0, err
		}
		version = KeyVersionLegacy
	}
	if err := writeKeyV2(keyPath, salt, keyV2); err != nil {
		return nil, 0, err
	}
	return keyV2, version, nil
}

// deriveKeyV2 uses hostname + UID, independent of mutable environment variables.
func deriveKeyV2(salt []byte) []byte {
	hostname, _ := os.Hostname()
	machineData := fmt.Sprintf("aerion:%s:%d", hostname, os.Getuid())
	return pbkdf2.Key([]byte(machineData), salt, pbkdf2Iterations, keySize, sha256.New)
}

// deriveKeyV1 is only for migration of legacy keys.
// Deprecated: use deriveKeyV2. Removal planned for 0.5.0.
func deriveKeyV1(salt []byte) []byte {
	hostname, _ := os.Hostname()
	username := os.Getenv("USER")
	if username == "" {
		username = os.Getenv("USERNAME")
	}
	machineData := fmt.Sprintf("aerion:%s:%s:%d", hostname, username, os.Getuid())
	return pbkdf2.Key([]byte(machineData), salt, pbkdf2Iterations, keySize, sha256.New)
}

func createKeyV2(keyPath string) ([]byte, error) {
	salt := make([]byte, saltSize)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return nil, fmt.Errorf("failed to generate salt: %w", err)
	}
	key := deriveKeyV2(salt)
	if err := os.MkdirAll(filepath.Dir(keyPath), 0700); err != nil {
		return nil, fmt.Errorf("failed to create key directory: %w", err)
	}
	if err := writeKeyV2(keyPath, salt, key); err != nil {
		return nil, err
	}
	return key, nil
}

// writeKeyV2 writes a private temporary file and atomically replaces device.key.
// A unique temporary name avoids following stale .tmp symlinks or reusing
// permissive files left by an interrupted write.
func writeKeyV2(keyPath string, salt, key []byte) error {
	if len(salt) != saltSize || len(key) != keySize {
		return fmt.Errorf("invalid device key salt or key size")
	}
	data := make([]byte, keyFileSizeV2)
	data[0] = keyFormatVersion2
	copy(data[1:], salt)
	copy(data[1+saltSize:], key)
	tmp, err := os.CreateTemp(filepath.Dir(keyPath), ".device-key-*")
	if err != nil {
		return fmt.Errorf("failed to create temporary key file: %w", err)
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("failed to write key file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("failed to sync key file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("failed to close key file: %w", err)
	}
	if err := renameKeyFile(tmp.Name(), keyPath); err != nil {
		return fmt.Errorf("failed to finalize key file: %w", err)
	}
	return syncKeyDirectory(keyPath)
}

// Persist rename ordering as well as file contents on Unix, so the recovery
// entry reaches disk before device.key is replaced. Windows cannot sync
// directory handles through os.File; renameKeyFile requests write-through there.
func syncKeyDirectory(keyPath string) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	dir, err := os.Open(filepath.Dir(keyPath))
	if err != nil {
		return fmt.Errorf("open key directory: %w", err)
	}
	defer dir.Close()
	if err := dir.Sync(); err != nil {
		return fmt.Errorf("sync key directory: %w", err)
	}
	return nil
}

func bytesEqual(a, b []byte) bool {
	return subtle.ConstantTimeCompare(a, b) == 1
}

// Encrypt encrypts plaintext using AES-256-GCM
// Returns base64-encoded ciphertext (nonce prepended)
func (e *Encryptor) Encrypt(plaintext string) (string, error) {
	if plaintext == "" {
		return "", nil
	}

	block, err := aes.NewCipher(e.key)
	if err != nil {
		return "", fmt.Errorf("failed to create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("failed to create GCM: %w", err)
	}

	// Generate random nonce
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("failed to generate nonce: %w", err)
	}

	// Encrypt and prepend nonce
	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)

	// Return base64-encoded result
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// Decrypt decrypts base64-encoded ciphertext (with prepended nonce)
func (e *Encryptor) Decrypt(ciphertext string) (string, error) {
	if ciphertext == "" {
		return "", nil
	}

	// Decode base64
	data, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return "", fmt.Errorf("failed to decode ciphertext: %w", err)
	}

	block, err := aes.NewCipher(e.key)
	if err != nil {
		return "", fmt.Errorf("failed to create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("failed to create GCM: %w", err)
	}

	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return "", fmt.Errorf("ciphertext too short")
	}

	// Extract nonce and ciphertext
	nonce, ciphertextBytes := data[:nonceSize], data[nonceSize:]

	// Decrypt
	plaintext, err := gcm.Open(nil, nonce, ciphertextBytes, nil)
	if err != nil {
		return "", fmt.Errorf("failed to decrypt: %w", err)
	}

	return string(plaintext), nil
}

// Bound reads and reject symlinks/nonregular files before interpreting key material.
func readKeyFile(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("device key is not a regular file")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !os.SameFile(info, opened) {
		return nil, fmt.Errorf("device key changed while opening")
	}
	if err := f.Chmod(0600); err != nil {
		return nil, fmt.Errorf("secure device key permissions: %w", err)
	}
	return io.ReadAll(io.LimitReader(f, keyFileSizeV2+1))
}

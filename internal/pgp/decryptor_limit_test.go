package pgp

import (
	"bytes"
	"errors"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/packet"
	"github.com/hkdb/aerion/internal/credentials"
	"github.com/hkdb/aerion/internal/database"
	"github.com/rs/zerolog"
)

func TestDecryptBytesLimitsDecompressedPlaintext(t *testing.T) {
	decryptor, entity := newTestDecryptor(t)

	exact := bytes.Repeat([]byte("a"), maxDecryptedMessageBytes)
	encryptedExact := encryptCompressed(t, entity, exact)
	decrypted, err := decryptor.DecryptBytes("account", "test@example.com", encryptedExact)
	if err != nil {
		t.Fatalf("DecryptBytes at limit: %v", err)
	}
	if !bytes.Equal(decrypted, exact) {
		t.Fatal("DecryptBytes changed plaintext at limit")
	}

	over := bytes.Repeat([]byte("b"), maxDecryptedMessageBytes+1)
	encryptedOver := encryptCompressed(t, entity, over)
	decrypted, err = decryptor.DecryptBytes("account", "test@example.com", encryptedOver)
	if !errors.Is(err, ErrDecryptedMessageTooLarge) {
		t.Fatalf("DecryptBytes over limit error = %v, want ErrDecryptedMessageTooLarge", err)
	}
	if decrypted != nil {
		t.Fatal("DecryptBytes returned partial plaintext after exceeding the limit")
	}
}

func TestDecryptMessageCompressedPlaintext(t *testing.T) {
	decryptor, entity := newTestDecryptor(t)
	plaintext := []byte("Content-Type: text/plain\r\n\r\ncompressed PGP/MIME plaintext")
	encrypted := encryptCompressed(t, entity, plaintext)
	raw := pgpMIMEMessage(encrypted)

	decrypted, encryptedMessage, err := decryptor.DecryptMessage("account", "test@example.com", raw)
	if err != nil {
		t.Fatalf("DecryptMessage: %v", err)
	}
	if !encryptedMessage {
		t.Fatal("DecryptMessage did not recognize PGP/MIME content")
	}
	if !bytes.Equal(decrypted, plaintext) {
		t.Fatalf("DecryptMessage plaintext = %q, want %q", decrypted, plaintext)
	}
}

func TestDecryptBytesInvalidMessageReturnsPGPError(t *testing.T) {
	decryptor, _ := newTestDecryptor(t)
	decrypted, err := decryptor.DecryptBytes("account", "test@example.com", []byte("not an OpenPGP message"))
	if err == nil {
		t.Fatal("DecryptBytes accepted invalid OpenPGP data")
	}
	if errors.Is(err, ErrDecryptedMessageTooLarge) {
		t.Fatalf("DecryptBytes invalid message returned size error: %v", err)
	}
	if decrypted != nil {
		t.Fatal("DecryptBytes returned plaintext for invalid OpenPGP data")
	}
}

func newTestDecryptor(t *testing.T) (*Decryptor, *openpgp.Entity) {
	t.Helper()
	dir := t.TempDir()
	db, err := database.Open(filepath.Join(dir, "pgp.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Migrate(); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO accounts (id, name, email, imap_host, smtp_host, username) VALUES ('account', 'Test', 'test@example.com', 'imap.example.com', 'smtp.example.com', 'test@example.com')`); err != nil {
		t.Fatal(err)
	}

	credStore, err := credentials.NewStore(db.DB, dir)
	if err != nil {
		t.Fatal(err)
	}
	entity := generateTestKey(t)
	publicKey, err := ArmorPublicKey(entity)
	if err != nil {
		t.Fatal(err)
	}
	privateKey, err := ArmorPrivateKey(entity)
	if err != nil {
		t.Fatal(err)
	}
	key := ExtractKeyMetadata(entity)
	key.AccountID = "account"
	key.IsDefault = true
	store := NewStore(db.DB, zerolog.Nop())
	if err := store.SaveKey(key, publicKey); err != nil {
		t.Fatal(err)
	}
	if err := credStore.SetPGPPrivateKey(key.ID, []byte(privateKey)); err != nil {
		t.Fatal(err)
	}
	return NewDecryptor(store, credStore, zerolog.Nop()), entity
}

func encryptCompressed(t *testing.T, entity *openpgp.Entity, plaintext []byte) []byte {
	t.Helper()
	var encrypted bytes.Buffer
	w, err := openpgp.Encrypt(&encrypted, openpgp.EntityList{entity}, nil, nil, &packet.Config{
		DefaultCompressionAlgo: packet.CompressionZLIB,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(plaintext); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return encrypted.Bytes()
}

func pgpMIMEMessage(encrypted []byte) []byte {
	const boundary = "pgp-limit-test"
	return []byte(fmt.Sprintf("Content-Type: multipart/encrypted; protocol=\"application/pgp-encrypted\"; boundary=%q\r\n\r\n--%s\r\nContent-Type: application/pgp-encrypted\r\n\r\nVersion: 1\r\n--%s\r\nContent-Type: application/octet-stream\r\n\r\n%s\r\n--%s--\r\n", boundary, boundary, boundary, encrypted, boundary))
}

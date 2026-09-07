package smime

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"path/filepath"
	"testing"
	"time"

	"github.com/hkdb/aerion/internal/database"
	"github.com/rs/zerolog"
	"go.mozilla.org/pkcs7"
)

func TestVerifyPKCS7TrustAndCache(t *testing.T) {
	for _, tc := range []struct {
		name                         string
		selfSigned, corrupt, expired bool
		want                         SignatureStatus
		cached                       int
	}{
		{name: "unknown CA", want: StatusUnknownSigner},
		{name: "self signed", selfSigned: true, want: StatusSelfSigned, cached: 1},
		{name: "expired signature", expired: true, want: StatusExpiredCert, cached: 1},
		{name: "invalid unknown CA signature", corrupt: true, want: StatusInvalid},
		{name: "invalid self signed signature", selfSigned: true, corrupt: true, want: StatusInvalid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, err := database.Open(filepath.Join(t.TempDir(), "certs.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			if err := db.Migrate(); err != nil {
				t.Fatal(err)
			}
			store := NewStore(db.DB, zerolog.Nop())
			p7 := signedTestMessage(t, tc.selfSigned, tc.expired)
			if tc.corrupt {
				p7.Signers[0].EncryptedDigest[0] ^= 0xff
			}
			result := NewVerifier(store, zerolog.Nop()).verifyPKCS7(p7)
			if result.Status != tc.want {
				t.Fatalf("status = %s, want %s: %s", result.Status, tc.want, result.ErrorMessage)
			}
			certs, err := store.GetSenderCerts("sender@example.com")
			if err != nil {
				t.Fatal(err)
			}
			if len(certs) != tc.cached {
				t.Fatalf("cached %d certificates, want %d", len(certs), tc.cached)
			}
		})
	}
}

func signedTestMessage(t *testing.T, selfSigned, expired bool) *pkcs7.PKCS7 {
	t.Helper()
	rootKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	root := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "Untrusted test CA"},
		NotBefore: time.Now().Add(-48 * time.Hour), NotAfter: time.Now().Add(24 * time.Hour),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign,
	}
	rootDER, err := x509.CreateCertificate(rand.Reader, root, root, &rootKey.PublicKey, rootKey)
	if err != nil {
		t.Fatal(err)
	}
	root, err = x509.ParseCertificate(rootDER)
	if err != nil {
		t.Fatal(err)
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	leaf := &x509.Certificate{
		SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: "Sender"},
		EmailAddresses: []string{"sender@example.com"},
		NotBefore:      time.Now().Add(-24 * time.Hour), NotAfter: time.Now().Add(24 * time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageEmailProtection},
	}
	if expired {
		leaf.NotAfter = time.Now().Add(-time.Hour)
	}
	issuer, signingKey := root, rootKey
	if selfSigned {
		issuer, signingKey = leaf, key
	}
	leafDER, err := x509.CreateCertificate(rand.Reader, leaf, issuer, &key.PublicKey, signingKey)
	if err != nil {
		t.Fatal(err)
	}
	leaf, err = x509.ParseCertificate(leafDER)
	if err != nil {
		t.Fatal(err)
	}
	signed, err := pkcs7.NewSignedData([]byte("signed message"))
	if err != nil {
		t.Fatal(err)
	}
	signed.SetDigestAlgorithm(pkcs7.OIDDigestAlgorithmSHA256)
	// Omit signing-time attributes so expiration is evaluated at the current time.
	if err := signed.SignWithoutAttr(leaf, key, pkcs7.SignerInfoConfig{}); err != nil {
		t.Fatal(err)
	}
	if !selfSigned {
		signed.AddCertificate(root)
	}
	der, err := signed.Finish()
	if err != nil {
		t.Fatal(err)
	}
	p7, err := pkcs7.Parse(der)
	if err != nil {
		t.Fatal(err)
	}
	return p7
}

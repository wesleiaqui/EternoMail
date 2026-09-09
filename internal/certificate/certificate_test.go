package certificate

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"database/sql"
	"encoding/pem"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func generateTestCert(t *testing.T) []byte {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "test.example.com", Organization: []string{"Test Org"}},
		NotBefore:    time.Now().Add(-1 * time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		DNSNames:     []string{"test.example.com"},
	}
	derBytes, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("failed to create certificate: %v", err)
	}
	return derBytes
}

func generateSystemTrustedTestChain(t *testing.T, host string) ([]byte, []byte, []byte) {
	t.Helper()
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	caTemplate := &x509.Certificate{
		SerialNumber:          big.NewInt(10),
		Subject:               pkix.Name{CommonName: "Test Root"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	leafKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	leafTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(11),
		Subject:      pkix.Name{CommonName: host},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		DNSNames:     []string{host},
		KeyUsage:     x509.KeyUsageDigitalSignature,
	}
	leafDER, err := x509.CreateCertificate(rand.Reader, leafTemplate, caTemplate, &leafKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	return caDER, leafDER, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER})
}

func openTestStore(t *testing.T) *Store {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("failed to open in-memory DB: %v", err)
	}
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS trusted_certificates (
		id TEXT PRIMARY KEY,
		fingerprint TEXT NOT NULL,
		host TEXT NOT NULL,
		subject TEXT,
		issuer TEXT,
		not_before TEXT,
		not_after TEXT,
		accepted_at DATETIME,
		UNIQUE(host, fingerprint)
	)`)
	if err != nil {
		t.Fatalf("failed to create table: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return NewStore(db)
}

func TestFingerprint(t *testing.T) {
	der := generateTestCert(t)
	fp := Fingerprint(der)

	if len(fp) != 64 {
		t.Fatalf("Fingerprint length = %d, want 64", len(fp))
	}

	// Verify it's hex
	for _, c := range fp {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			t.Fatalf("Fingerprint contains non-hex char: %c", c)
		}
	}
}

func TestFormatFingerprint(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"abcd1234", "AB:CD:12:34"},
		{"aa", "AA"},
		{"aabb", "AA:BB"},
		{"a", "A"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := FormatFingerprint(tt.input)
			if got != tt.want {
				t.Fatalf("FormatFingerprint(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestExtractCertInfo(t *testing.T) {
	der := generateTestCert(t)
	info := ExtractCertInfo(der, errors.New("test error"))

	if info.Subject == "" {
		t.Fatal("Subject should not be empty")
	}
	if info.Issuer == "" {
		t.Fatal("Issuer should not be empty")
	}
	if len(info.DNSNames) == 0 {
		t.Fatal("DNSNames should not be empty")
	}
	if info.DNSNames[0] != "test.example.com" {
		t.Fatalf("DNSNames[0] = %q, want %q", info.DNSNames[0], "test.example.com")
	}
	if info.NotBefore == "" {
		t.Fatal("NotBefore should not be empty")
	}
	if info.NotAfter == "" {
		t.Fatal("NotAfter should not be empty")
	}
	if info.Fingerprint == "" {
		t.Fatal("Fingerprint should not be empty")
	}
	if info.IsExpired {
		t.Fatal("IsExpired = true, want false (cert valid for 24h)")
	}
}

func TestFormatDN(t *testing.T) {
	tests := []struct {
		name string
		cn   string
		org  []string
		want string
	}{
		{"cn and org", "example.com", []string{"Org"}, "example.com (Org)"},
		{"cn only", "example.com", nil, "example.com"},
		{"org only", "", []string{"Org"}, "Org"},
		{"neither", "", nil, "(unknown)"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatDN(tt.cn, tt.org)
			if got != tt.want {
				t.Fatalf("formatDN(%q, %v) = %q, want %q", tt.cn, tt.org, got, tt.want)
			}
		})
	}
}

func TestClassifyError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{"nil error", nil, "unknown error"},
		{"unknown authority", errors.New("x509: certificate signed by unknown authority"), "self-signed or unknown certificate authority"},
		{"expired", errors.New("x509: certificate has expired or is not yet valid"), "certificate has expired"},
		{"random error", errors.New("something went wrong"), "something went wrong"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := classifyError(tt.err)
			if got != tt.want {
				t.Fatalf("classifyError(%v) = %q, want %q", tt.err, got, tt.want)
			}
		})
	}
}

func TestAcceptSession(t *testing.T) {
	store := openTestStore(t)

	fp := "aabbccdd11223344aabbccdd11223344aabbccdd11223344aabbccdd11223344"
	if err := store.AcceptSession("Mail.Example.COM:993", fp); err != nil {
		t.Fatal(err)
	}

	if !store.IsTrusted("mail.example.com", fp) {
		t.Fatal("IsTrusted = false after AcceptSession, want true")
	}
	if store.IsTrusted("other.example.com", fp) {
		t.Fatal("session trust leaked to another host")
	}
}

func TestIsTrustedDefault(t *testing.T) {
	store := openTestStore(t)

	fp := "0000000000000000000000000000000000000000000000000000000000000000"
	if store.IsTrusted("test.example.com", fp) {
		t.Fatal("IsTrusted = true for unknown fingerprint, want false")
	}
}

func TestBuildTLSConfigAcceptsNormallyTrustedCertificate(t *testing.T) {
	const host = "valid.example.test"
	caDER, leafDER, caPEM := generateSystemTrustedTestChain(t, host)
	path := filepath.Join(t.TempDir(), "test-ca.pem")
	if err := os.WriteFile(path, caPEM, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SSL_CERT_FILE", path)

	cfg := BuildTLSConfig(host, openTestStore(t))
	if err := cfg.VerifyPeerCertificate([][]byte{leafDER, caDER}, nil); err != nil {
		t.Fatalf("normally trusted certificate was rejected: %v", err)
	}
}

// TestBuildTLSConfigDynamic exercises the host-agnostic TOFU verifier used by
// the DAV transports. Drives VerifyConnection directly (no TLS server needed):
// an untrusted self-signed cert is rejected with a structured *Error; once its
// fingerprint is trusted it passes; an empty chain errors.
func TestBuildTLSConfigDynamic(t *testing.T) {
	store := openTestStore(t)
	der := generateTestCert(t)
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse cert: %v", err)
	}

	cfg := BuildTLSConfigDynamic(store)
	if cfg.VerifyConnection == nil {
		t.Fatal("VerifyConnection is nil")
	}
	if !cfg.InsecureSkipVerify {
		t.Fatal("InsecureSkipVerify must be true (the callback does the real verification)")
	}

	cs := tls.ConnectionState{
		ServerName:       "test.example.com",
		PeerCertificates: []*x509.Certificate{cert},
	}

	// Untrusted self-signed → structured *Error.
	err = cfg.VerifyConnection(cs)
	if err == nil {
		t.Fatal("expected error for untrusted self-signed cert")
	}
	var ce *Error
	if !errors.As(err, &ce) {
		t.Fatalf("expected *Error, got %T: %v", err, err)
	}

	// Trust the fingerprint → now accepted.
	if err := store.AcceptSession("test.example.com", Fingerprint(der)); err != nil {
		t.Fatal(err)
	}
	if err := cfg.VerifyConnection(cs); err != nil {
		t.Fatalf("expected store-trusted cert to pass, got %v", err)
	}
	if err := cfg.VerifyConnection(tls.ConnectionState{ServerName: "other.example.com", PeerCertificates: []*x509.Certificate{cert}}); err == nil {
		t.Fatal("session trust for test.example.com accepted the same certificate for another host")
	}
	differentDER := generateTestCert(t)
	differentCert, err := x509.ParseCertificate(differentDER)
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.VerifyConnection(tls.ConnectionState{ServerName: "test.example.com", PeerCertificates: []*x509.Certificate{differentCert}}); err == nil {
		t.Fatal("a different fingerprint was accepted for the trusted host")
	}

	// Empty chain → error.
	if err := cfg.VerifyConnection(tls.ConnectionState{ServerName: "test.example.com"}); err == nil {
		t.Fatal("expected error for empty PeerCertificates")
	}
}

func TestPermanentTrustIsHostBoundAndRevocable(t *testing.T) {
	store := openTestStore(t)
	info := &CertificateInfo{Fingerprint: "aabbccdd", Subject: "subject", Issuer: "issuer"}
	if err := store.AcceptPermanently("MAIL.EXAMPLE.COM:993", info); err != nil {
		t.Fatal(err)
	}
	if !store.IsTrusted("mail.example.com", info.Fingerprint) {
		t.Fatal("permanent trust was not found for its host")
	}
	if store.IsTrusted("other.example.com", info.Fingerprint) {
		t.Fatal("permanent trust leaked to another host")
	}
	if err := store.AcceptPermanently("other.example.com", info); err != nil {
		t.Fatal(err)
	}
	if !store.IsTrusted("other.example.com", info.Fingerprint) {
		t.Fatal("same certificate could not be trusted independently for another host")
	}
	if err := store.AcceptSession("third.example.com", info.Fingerprint); err != nil {
		t.Fatal(err)
	}
	if err := store.Remove(info.Fingerprint); err != nil {
		t.Fatal(err)
	}
	if store.IsTrusted("mail.example.com", info.Fingerprint) || store.IsTrusted("other.example.com", info.Fingerprint) || store.IsTrusted("third.example.com", info.Fingerprint) {
		t.Fatal("removal did not revoke every host-bound grant")
	}
}

func TestHostNormalization(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"MAIL.Example.COM.:993", "mail.example.com"},
		{"127.0.0.1:993", "127.0.0.1"},
		{"[2001:0db8:0:0:0:0:0:1]:993", "2001:db8::1"},
		{"[2001:db8::1]", "2001:db8::1"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := normalizeHost(tt.input)
			if err != nil || got != tt.want {
				t.Fatalf("normalizeHost(%q) = %q, %v; want %q", tt.input, got, err, tt.want)
			}
		})
	}
}

func TestSessionTrustConcurrent(t *testing.T) {
	store := openTestStore(t)
	const host = "mail.example.com"
	const fingerprint = "concurrent-fingerprint"
	var wg sync.WaitGroup
	for range 32 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := store.AcceptSession(host, fingerprint); err != nil {
				t.Error(err)
			}
			if !store.IsTrusted(host, fingerprint) {
				t.Error("concurrent session trust was not visible")
			}
		}()
	}
	wg.Wait()
}

func TestErrorInterface(t *testing.T) {
	info := &CertificateInfo{
		Fingerprint: "abcd1234",
	}
	certErr := &Error{
		Info:   info,
		Reason: "test reason",
	}

	// Verify it implements the error interface
	var err error = certErr
	if err.Error() == "" {
		t.Fatal("Error() should return non-empty string")
	}

	expected := "untrusted certificate: test reason (fingerprint: abcd1234)"
	if err.Error() != expected {
		t.Fatalf("Error() = %q, want %q", err.Error(), expected)
	}
}

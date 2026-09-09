package certificate

import (
	"database/sql"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Store manages trusted certificates in the database and session memory
type Store struct {
	db      *sql.DB
	mu      sync.RWMutex
	session map[string]bool // normalized host + fingerprint -> trusted
}

// NewStore creates a new certificate trust store
func NewStore(db *sql.DB) *Store {
	return &Store{
		db:      db,
		session: make(map[string]bool),
	}
}

// IsTrusted checks whether a certificate fingerprint is trusted for host.
func (s *Store) IsTrusted(host, fingerprint string) bool {
	host, err := normalizeHost(host)
	if err != nil || fingerprint == "" {
		return false
	}
	key := trustKey(host, fingerprint)
	// Check session memory first (fast path)
	s.mu.RLock()
	if s.session[key] {
		s.mu.RUnlock()
		return true
	}
	s.mu.RUnlock()

	// Check database
	var count int
	err = s.db.QueryRow(
		"SELECT COUNT(*) FROM trusted_certificates WHERE host = ? AND fingerprint = ?",
		host, fingerprint,
	).Scan(&count)
	if err != nil {
		return false
	}
	return count > 0
}

// AcceptPermanently stores a certificate in the database
func (s *Store) AcceptPermanently(host string, info *CertificateInfo) error {
	if info == nil || info.Fingerprint == "" {
		return fmt.Errorf("certificate fingerprint is required")
	}
	host, err := normalizeHost(host)
	if err != nil {
		return err
	}
	id := uuid.New().String()
	_, err = s.db.Exec(
		`INSERT INTO trusted_certificates (id, fingerprint, host, subject, issuer, not_before, not_after, accepted_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(host, fingerprint) DO UPDATE SET
			 subject = excluded.subject, issuer = excluded.issuer,
			 not_before = excluded.not_before, not_after = excluded.not_after,
			 accepted_at = excluded.accepted_at`,
		id, info.Fingerprint, host, info.Subject, info.Issuer, info.NotBefore, info.NotAfter, time.Now(),
	)
	return err
}

// AcceptSession stores a certificate fingerprint for host in session memory only.
func (s *Store) AcceptSession(host, fingerprint string) error {
	host, err := normalizeHost(host)
	if err != nil {
		return err
	}
	if fingerprint == "" {
		return fmt.Errorf("certificate fingerprint is required")
	}
	s.mu.Lock()
	s.session[trustKey(host, fingerprint)] = true
	s.mu.Unlock()
	return nil
}

// GetByHosts returns permanently trusted certificates for the given hosts
func (s *Store) GetByHosts(hosts []string) ([]*CertificateInfo, error) {
	if len(hosts) == 0 {
		return nil, nil
	}

	args := make([]interface{}, 0, len(hosts))
	for _, h := range hosts {
		normalized, err := normalizeHost(h)
		if err != nil {
			continue
		}
		args = append(args, normalized)
	}
	if len(args) == 0 {
		return nil, nil
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(args)), ",")
	query := "SELECT fingerprint, host, subject, issuer, not_before, not_after FROM trusted_certificates WHERE host IN (" + placeholders + ") ORDER BY accepted_at DESC"

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var certs []*CertificateInfo
	for rows.Next() {
		var ci CertificateInfo
		var host string
		if err := rows.Scan(&ci.Fingerprint, &host, &ci.Subject, &ci.Issuer, &ci.NotBefore, &ci.NotAfter); err != nil {
			return nil, err
		}
		certs = append(certs, &ci)
	}
	return certs, rows.Err()
}

// Remove deletes a trusted certificate from the database by fingerprint
func (s *Store) Remove(fingerprint string) error {
	_, err := s.db.Exec("DELETE FROM trusted_certificates WHERE fingerprint = ?", fingerprint)
	if err != nil {
		return err
	}

	// Also remove from session
	s.mu.Lock()
	for key := range s.session {
		if strings.HasSuffix(key, "\x00"+fingerprint) {
			delete(s.session, key)
		}
	}
	s.mu.Unlock()

	return nil
}

func trustKey(host, fingerprint string) string {
	return host + "\x00" + fingerprint
}

// normalizeHost produces the host identity used by TLS verification. Ports
// are excluded because TLS ServerName identifies the endpoint name, not its
// service port. DNS names are case-insensitive; IP addresses use net.IP's
// canonical form so equivalent IPv4 and IPv6 spellings share one identity.
func normalizeHost(host string) (string, error) {
	host = strings.TrimSpace(host)
	if splitHost, _, err := net.SplitHostPort(host); err == nil {
		host = splitHost
	}
	host = strings.TrimPrefix(strings.TrimSuffix(host, "]"), "[")
	host = strings.TrimSuffix(host, ".")
	if host == "" || strings.ContainsAny(host, "\t\n\r /\\") {
		return "", fmt.Errorf("invalid certificate host %q", host)
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.String(), nil
	}
	if strings.Contains(host, ":") {
		return "", fmt.Errorf("invalid certificate host %q", host)
	}
	return strings.ToLower(host), nil
}

package database

import "testing"

func TestMigrationV48BindsTrustedCertificatesToHosts(t *testing.T) {
	db := openUnmigratedTestDB(t)
	applyMigrationsThrough(t, db, 47)
	if _, err := db.Exec(`
		INSERT INTO trusted_certificates
			(id, fingerprint, host, subject, issuer, accepted_at)
		VALUES
			('valid', 'fingerprint-a', 'MAIL.EXAMPLE.COM', 'subject', 'issuer', CURRENT_TIMESTAMP),
			('legacy-empty', 'fingerprint-b', '', 'subject', 'issuer', CURRENT_TIMESTAMP)
	`); err != nil {
		t.Fatalf("seed legacy trusted certificates: %v", err)
	}
	if err := db.Migrate(); err != nil {
		t.Fatalf("apply v48: %v", err)
	}
	var host string
	if err := db.QueryRow(`SELECT host FROM trusted_certificates WHERE id = 'valid'`).Scan(&host); err != nil {
		t.Fatalf("read migrated certificate: %v", err)
	}
	if host != "mail.example.com" {
		t.Fatalf("migrated host = %q, want mail.example.com", host)
	}
	var legacyCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM trusted_certificates WHERE id = 'legacy-empty'`).Scan(&legacyCount); err != nil {
		t.Fatal(err)
	}
	if legacyCount != 0 {
		t.Fatal("empty legacy host survived migration and could become global trust")
	}
	if _, err := db.Exec(`
		INSERT INTO trusted_certificates (id, fingerprint, host, subject, issuer)
		VALUES ('second-host', 'fingerprint-a', 'other.example.com', 'subject', 'issuer')
	`); err != nil {
		t.Fatalf("host-scoped uniqueness rejected same fingerprint at another host: %v", err)
	}
}

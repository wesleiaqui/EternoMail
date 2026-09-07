package smime

import (
	"database/sql"
	"github.com/rs/zerolog"
	_ "modernc.org/sqlite"
	"testing"
)

func TestDefaultCredentialRejectsOtherAccountAndPreservesDefault(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	for _, q := range []string{
		"CREATE TABLE accounts (id TEXT PRIMARY KEY, smime_default_cert_id TEXT)",
		"CREATE TABLE smime_certificates (id TEXT PRIMARY KEY, account_id TEXT, is_default INTEGER)",
		"INSERT INTO accounts VALUES ('a','own'),('b','foreign')",
		"INSERT INTO smime_certificates VALUES ('own','a',1),('foreign','b',1),('replacement','a',0)",
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	s := NewStore(db, zerolog.Nop())
	for _, id := range []string{"foreign", "missing"} {
		if err := s.SetDefaultCertificate("a", id); err == nil {
			t.Fatalf("accepted %s", id)
		}
		var current string
		if err := db.QueryRow("SELECT smime_default_cert_id FROM accounts WHERE id='a'").Scan(&current); err != nil || current != "own" {
			t.Fatal("default changed", err)
		}
		var flag int
		if err := db.QueryRow("SELECT is_default FROM smime_certificates WHERE id='own'").Scan(&flag); err != nil || flag != 1 {
			t.Fatal("default flag changed", err)
		}
	}
	if err := s.SetDefaultCertificate("a", "replacement"); err != nil {
		t.Fatal(err)
	}
}

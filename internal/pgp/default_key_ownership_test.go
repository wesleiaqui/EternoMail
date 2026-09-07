package pgp

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
		"CREATE TABLE accounts (id TEXT PRIMARY KEY, pgp_default_key_id TEXT)",
		"CREATE TABLE pgp_keys (id TEXT PRIMARY KEY, account_id TEXT, is_default INTEGER)",
		"INSERT INTO accounts VALUES ('a','own'),('b','foreign')",
		"INSERT INTO pgp_keys VALUES ('own','a',1),('foreign','b',1),('replacement','a',0)",
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	s := NewStore(db, zerolog.Nop())
	for _, id := range []string{"foreign", "missing"} {
		if err := s.SetDefaultKey("a", id); err == nil {
			t.Fatalf("accepted %s", id)
		}
		var current string
		if err := db.QueryRow("SELECT pgp_default_key_id FROM accounts WHERE id='a'").Scan(&current); err != nil || current != "own" {
			t.Fatal("default changed", err)
		}
		var flag int
		if err := db.QueryRow("SELECT is_default FROM pgp_keys WHERE id='own'").Scan(&flag); err != nil || flag != 1 {
			t.Fatal("default flag changed", err)
		}
	}
	if err := s.SetDefaultKey("a", "replacement"); err != nil {
		t.Fatal(err)
	}
}

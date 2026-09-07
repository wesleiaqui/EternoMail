package database

import (
	"path/filepath"
	"testing"
)

func TestOpenRejectsDSNInjection(t *testing.T) {
	for _, name := range []string{"mail?mode=memory", "mail&mode=memory", "mail#fragment"} {
		if db, err := Open(filepath.Join(t.TempDir(), name)); err == nil {
			db.Close()
			t.Fatalf("accepted %s", name)
		}
	}
}
func TestOpenLiteralPercentPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mail%3F.db")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if db.Path() != path {
		t.Fatal(db.Path())
	}
	if err := db.Migrate(); err != nil {
		t.Fatal(err)
	}
}

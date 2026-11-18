package mailfilter

import (
	"github.com/hkdb/aerion/internal/account"
	"github.com/hkdb/aerion/internal/folder"
	"strings"
	"testing"
)

func TestValidationRejectsEmptyDestructiveCriteria(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	accounts := account.NewStore(db)
	folders := folder.NewStore(db)
	for _, blank := range [][]string{nil, {}, {""}, {" ", "\t\n"}} {
		for _, action := range []string{"trash", "archive", "read"} {
			rule := &Rule{Name: "Unsafe", Enabled: true, FromContains: blank, ToContains: blank, SubjectContains: blank, ListIDContains: blank, ActionTrash: action == "trash", ActionArchive: action == "archive", ActionMarkRead: action == "read"}
			if err := ValidateRule(rule, accounts, folders); err == nil || !strings.Contains(err.Error(), "non-blank") {
				t.Fatalf("accepted %s criteria %q: %v", action, blank, err)
			}
		}
	}
	rule := &Rule{Name: "Safe", Enabled: true, SubjectContains: []string{" ", " invoice ", "\t"}, ActionTrash: true}
	if err := ValidateRule(rule, accounts, folders); err != nil {
		t.Fatal(err)
	}
	if len(rule.SubjectContains) != 1 || rule.SubjectContains[0] != "invoice" {
		t.Fatalf("patterns not normalized: %q", rule.SubjectContains)
	}
	if Match(&Rule{Enabled: true, FromContains: []string{" "}}, &Message{FromName: "Any Person"}) {
		t.Fatal("whitespace-only pattern matched incoming mail")
	}
}

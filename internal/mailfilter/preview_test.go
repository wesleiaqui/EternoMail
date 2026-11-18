package mailfilter

import (
	"github.com/hkdb/aerion/internal/folder"
	"github.com/hkdb/aerion/internal/message"
	"reflect"
	"testing"
	"time"
)

func TestPreviewMatchesIncomingSemantics(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	messages := message.NewStore(db)
	inputs := []*message.Message{
		{ID: "a", AccountID: "account1", FolderID: "folder1", UID: 1, FromName: "Ääkkönen", FromEmail: "sender@example.com", ToList: `[{"name":"Matti, Meikäläinen","email":"to@example.com"}]`, Subject: "Hello, world", Date: time.Now()},
		{ID: "b", AccountID: "account2", FolderID: "folder3", UID: 2, FromEmail: "other@example.com", ToList: `[{"email":"elsewhere@example.com"}]`, Subject: "other", Date: time.Now()},
	}
	for _, m := range inputs {
		if err := messages.Create(m); err != nil {
			t.Fatal(err)
		}
	}
	if err := messages.SetListID("a", "news.example.com"); err != nil {
		t.Fatal(err)
	}
	inputs[0].ListID = "news.example.com"
	cases := []Rule{
		{FromContains: []string{"ÄÄKKÖNEN"}},
		{ToContains: []string{"email"}}, // JSON property names must not match.
		{ToContains: []string{"MEIKÄLÄINEN"}},
		{SubjectContains: []string{"Hello, world"}},
		{ListIDContains: []string{"NEWS.EXAMPLE"}},
		{AccountIDs: []string{"account1", "account2"}, FromContains: []string{"sender"}, ListIDContains: []string{"news"}},
		{AccountIDs: []string{"account2"}, ListIDContains: []string{"news"}},
		{SubjectContains: []string{""}},
		{SubjectContains: []string{"%"}},
	}
	store := NewStore(db)
	for _, rule := range cases {
		rule := rule
		t.Run(rule.Name+"match", func(t *testing.T) {
			var want []string
			rule.Enabled = true
			for _, m := range inputs {
				if Match(&rule, FromMessage(m)) {
					want = append(want, m.ID)
				}
			}
			rule.Enabled = false // Preview intentionally ignores enabled state.
			got, err := store.MatchingMessageIDs(&rule, 0)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("rule %+v: got %v want %v", rule, got, want)
			}
			n, err := store.CountMatching(&rule)
			if err != nil || n != len(want) {
				t.Fatalf("count %d error %v", n, err)
			}
		})
	}
	if _, err := db.Exec(`UPDATE messages SET is_deleted=1 WHERE id='a'`); err != nil {
		t.Fatal(err)
	}
	n, err := store.CountMatching(&Rule{ListIDContains: []string{"news"}})
	if err != nil || n != 0 {
		t.Fatalf("deleted mail counted: %d %v", n, err)
	}
}

type routerRules struct{ rules []*Rule }

func (r routerRules) ListEnabled() ([]*Rule, error) { return r.rules, nil }

func TestRouterActionOnlyAndHiddenDestination(t *testing.T) {
	rules := routerRules{[]*Rule{{ID: "hidden", Enabled: true, DestFolderID: "hidden"}, {ID: "read", Enabled: true, ActionMarkRead: true}}}
	targets := routerFolders{}
	var applied string
	router := NewAutomaticRouter(rules, targets, func(rule *Rule, _ *folder.Folder, _ *message.Message) { applied = rule.ID })
	router.Route(&message.Message{ID: "m", AccountID: "account1"})
	if applied != "read" {
		t.Fatalf("hidden rule blocked read-only rule: %s", applied)
	}
}

type routerFolders struct{}

func (routerFolders) Get(id string) (*folder.Folder, error) {
	return &folder.Folder{ID: id, Hidden: true}, nil
}

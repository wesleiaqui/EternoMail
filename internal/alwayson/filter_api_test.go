package alwayson

import (
	"fmt"
	"github.com/hkdb/aerion/internal/account"
	"github.com/hkdb/aerion/internal/database"
	"github.com/hkdb/aerion/internal/folder"
	"github.com/hkdb/aerion/internal/mailfilter"
	"github.com/hkdb/aerion/internal/message"
	"path/filepath"
	"testing"
	"time"
)

func TestExistingFilterSafetyLimitAndCapturedListID(t *testing.T) {
	db, err := database.Open(filepath.Join(t.TempDir(), "filter.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{`INSERT INTO accounts(id,name,email,imap_host,smtp_host,username) VALUES ('a','A','me@example.com','imap','smtp','a')`, `INSERT INTO folders(id,account_id,name,path,folder_type) VALUES ('inbox','a','Inbox','INBOX','inbox')`} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	r := &SyncRuntime{db: db, messages: message.NewStore(db), rules: mailfilter.NewStore(db), accounts: account.NewStore(db), folders: folder.NewStore(db), filterJobs: mailfilter.NewJobStore(db)}
	for i := 0; i < 501; i++ {
		if err := r.messages.Create(&message.Message{ID: fmt.Sprint(i), AccountID: "a", FolderID: "inbox", UID: uint32(i + 1), FromEmail: "sender@example.com", Date: time.Now()}); err != nil {
			t.Fatal(err)
		}
	}
	rule := &mailfilter.Rule{Name: "Explicit only", ActionMarkRead: true, FromContains: []string{"sender@"}}
	if err := r.CreateFilterRule(rule); err != nil {
		t.Fatal(err)
	}
	// Neither creating a rule nor previewing it is permission to change old mail.
	n, err := r.PreviewFilterRule(rule)
	if err != nil || n != 501 {
		t.Fatalf("preview %d: %v", n, err)
	}
	if n, err := r.ApplyFilterRuleToExisting(rule.ID); err == nil || n != 0 {
		t.Fatalf("broad apply did not fail before provider access: %d %v", n, err)
	}
	var read int
	if err := db.QueryRow(`SELECT COUNT(*) FROM messages WHERE is_read=1`).Scan(&read); err != nil || read != 0 {
		t.Fatalf("mail changed during rejected apply: %d %v", read, err)
	}
	if err := r.messages.SetListID("0", "news.example.com"); err != nil {
		t.Fatal(err)
	}
	value, err := r.GetMessageListID("0")
	if err != nil || value != "news.example.com" {
		t.Fatalf("captured List-ID unavailable: %q %v", value, err)
	}
	rule.ListIDContains = []string{"nonexistent"}
	if err := r.UpdateFilterRule(rule); err != nil {
		t.Fatal(err)
	}
	n, err = r.ApplyFilterRuleToExisting(rule.ID)
	if err != nil || n != 0 {
		t.Fatalf("nonmatching existing mail touched provider: %d %v", n, err)
	}
	rule.ListIDContains = []string{"news"}
	if err := r.UpdateFilterRule(rule); err != nil {
		t.Fatal(err)
	}
	n, err = r.ApplyFilterRuleToExisting(rule.ID)
	if err != nil || n != 1 {
		t.Fatalf("narrow apply not queued: %d %v", n, err)
	}
	job, err := r.filterJobs.ClaimNext()
	if err != nil || job == nil || !job.Actions.ApplyExisting || job.MessageID != "0" {
		t.Fatalf("explicit disabled-rule application lost its identity: %+v %v", job, err)
	}

}

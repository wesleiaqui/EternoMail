package sync

import (
	"context"
	"github.com/hkdb/aerion/internal/account"
	"github.com/hkdb/aerion/internal/database"
	"github.com/hkdb/aerion/internal/folder"
	"github.com/hkdb/aerion/internal/message"
	"path/filepath"
	"testing"
	"time"
)

func TestHiddenFolderSkipsProviderAndVisibilityWaitsForReconciliation(t *testing.T) {
	db, err := database.Open(filepath.Join(t.TempDir(), "mail.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = db.Migrate(); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO accounts(id,name,email,imap_host,smtp_host,username) VALUES ('a','A','a@example.org','imap.example.org','smtp.example.org','a')`); err != nil {
		t.Fatal(err)
	}
	folders := folder.NewStore(db)
	f := &folder.Folder{ID: "old", AccountID: "a", Name: "Old", Path: "Old", Type: folder.TypeFolder}
	if err = folders.Create(f); err != nil {
		t.Fatal(err)
	}
	engine := NewEngine(nil, account.NewStore(db), folders, message.NewStore(db), message.NewAttachmentStore(db))
	unlock, err := engine.folderLocks.lock(context.Background(), "old")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	if err := engine.SetFolderVisibility(ctx, "a", []string{"old"}, true); err == nil {
		t.Fatal("visibility raced active reconcile")
	}
	cancel()
	if fresh, _ := folders.Get("old"); fresh.Hidden {
		t.Fatal("preference changed before reconcile settled")
	}
	unlock()
	if err := engine.SetFolderVisibility(context.Background(), "a", []string{"old", "old"}, true); err != nil {
		t.Fatal(err)
	}
	// Pool is nil: any provider access would panic. Both header/flag paths must
	// stop on persisted Hidden, before Gmail capability checks or IMAP access.
	if err := engine.SyncMessages(context.Background(), "a", "old", 0, false); err != nil {
		t.Fatal(err)
	}
	if err := engine.SyncFolderFlags(context.Background(), "a", "old"); err != nil {
		t.Fatal(err)
	}
	scheduler := NewScheduler(engine, account.NewStore(db), folders)
	for _, acc := range []*account.Account{{ID: "a", SyncAllFolders: true}, {ID: "a", SyncFoldersEnabled: true}, {ID: "a"}} {
		fs, err := scheduler.getAccountSyncFolders(acc)
		if err != nil || len(fs) != 0 {
			t.Fatalf("hidden included in schedule: %#v %v", fs, err)
		}
	}
}

func TestHiddenInboxSchedulerRespectsAccountInterval(t *testing.T) {
	db, err := database.Open(filepath.Join(t.TempDir(), "interval.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = db.Migrate(); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO accounts(id,name,email,imap_host,smtp_host,username) VALUES ('a','A','a@example.org','imap','smtp','a')`); err != nil {
		t.Fatal(err)
	}
	fs := folder.NewStore(db)
	if err = fs.Create(&folder.Folder{ID: "i", AccountID: "a", Name: "Inbox", Path: "INBOX", Type: folder.TypeInbox}); err != nil {
		t.Fatal(err)
	}
	if err = fs.SetHiddenBatch("a", []string{"i"}, true); err != nil {
		t.Fatal(err)
	}
	s := NewScheduler(nil, account.NewStore(db), fs)
	acc := &account.Account{ID: "a", SyncInterval: 30}
	if !s.isSyncDue(acc) {
		t.Fatal("initial hidden account refresh not due")
	}
	s.hiddenInboxAttempt = map[string]time.Time{"a": time.Now()}
	if s.isSyncDue(acc) {
		t.Fatal("repeat tick ignored interval")
	}
	s.hiddenInboxAttempt["a"] = time.Now().Add(-31 * time.Minute)
	if !s.isSyncDue(acc) {
		t.Fatal("interval did not expire")
	}
	f, _ := fs.Get("i")
	if f.LastSync != nil {
		t.Fatal("account attempt changed hidden inbox freshness")
	}
}

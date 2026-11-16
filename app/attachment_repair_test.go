package app

import (
	"github.com/hkdb/aerion/internal/database"
	"github.com/hkdb/aerion/internal/email"
	"github.com/hkdb/aerion/internal/message"
	"path/filepath"
	"testing"
	"time"
)

func TestAttachmentRepairDownloadUsesExactPartNotFilename(t *testing.T) {
	db, err := database.Open(filepath.Join(t.TempDir(), "repair.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err = db.Migrate(); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO accounts(id,name,email,imap_host,smtp_host,username) VALUES('a','A','a@example.test','imap','smtp','a')`); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO folders(id,account_id,name,path,folder_type) VALUES('f','a','Inbox','Inbox','inbox')`); err != nil {
		t.Fatal(err)
	}
	ms := message.NewStore(db)
	if err = ms.Create(&message.Message{ID: "mail", AccountID: "a", FolderID: "f", UID: 1, ThreadID: "mail", Date: time.Now(), ReceivedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	store := message.NewAttachmentStore(db)
	parts := []message.RepairPart{{Metadata: message.Attachment{ID: "part1", MessageID: "mail", Filename: "same.png", ContentType: "image/png", Size: 3}, Payload: []byte("one")}, {Metadata: message.Attachment{ID: "part2", MessageID: "mail", Filename: "same.png", ContentType: "image/png", Size: 3}, Payload: []byte("two")}}
	if _, err = store.RepairFromMIME("a", "mail", "digest", parts, ""); err != nil {
		t.Fatal(err)
	}
	app := &App{attachmentStore: store}
	downloader := email.NewAttachmentDownloader(t.TempDir())
	for i, want := range []string{"one", "two"} {
		got, err := app.storedAttachmentContent(downloader, []byte("not even parseable MIME"), &parts[i].Metadata)
		if err != nil || string(got) != want {
			t.Fatalf("part %d: %q %v", i, got, err)
		}
	}
	if _, err = app.RepairMessageAttachments("a", "mail"); err == nil {
		t.Fatal("repair accepted missing live owner engine")
	}
}

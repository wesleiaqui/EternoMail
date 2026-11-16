package main

import (
	"github.com/hkdb/aerion/internal/database"
	"github.com/hkdb/aerion/internal/message"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRollbackCommandRefusesMissingOrWrongDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "typo.db")
	if err := run([]string{"--db", path, "--account", "a", "--message", "m"}); err == nil {
		t.Fatal("created missing database")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("typo created a file")
	}
	if err := run(nil); err == nil {
		t.Fatal("missing ownership accepted")
	}
}

func TestRollbackCommandRestoresBackedUpCache(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mail.db")
	db, err := database.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.Migrate(); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO accounts(id,name,email,imap_host,smtp_host,username) VALUES('a','A','a@example.test','imap','smtp','a')`); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO folders(id,account_id,name,path,folder_type) VALUES('f','a','Inbox','Inbox','inbox')`); err != nil {
		t.Fatal(err)
	}
	if err = message.NewStore(db).Create(&message.Message{ID: "mail", AccountID: "a", FolderID: "f", UID: 1, ThreadID: "mail", Date: time.Now(), ReceivedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	store := message.NewAttachmentStore(db)
	if err = store.Create(&message.Attachment{ID: "legacy", MessageID: "mail", Filename: "old", Size: 3}); err != nil {
		t.Fatal(err)
	}
	backup, err := store.RepairFromMIME("a", "mail", "digest", []message.RepairPart{{Metadata: message.Attachment{ID: "new", MessageID: "mail", Filename: "new", Size: 3}, Payload: []byte("one")}}, "")
	if err != nil {
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	args := []string{"--db", path, "--account", "a", "--message", "mail"}
	if err = run(args); err != nil {
		t.Fatal(err)
	}
	if err = run(append(args, "--restore", backup)); err != nil {
		t.Fatal(err)
	}
	db, err = database.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	got, err := message.NewAttachmentStore(db).GetByMessage("mail")
	if err != nil || len(got) != 1 || got[0].ID != "legacy" {
		t.Fatalf("CLI restore lost original: %v %v", got, err)
	}
}

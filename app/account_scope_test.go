package app

import (
	"github.com/hkdb/aerion/internal/database"
	"github.com/hkdb/aerion/internal/folder"
	"github.com/hkdb/aerion/internal/message"
	"path/filepath"
	"testing"
)

func TestSearchAndReceiptsRejectMismatchedAccount(t *testing.T) {
	db, err := database.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		"INSERT INTO accounts (id,name,email,imap_host,smtp_host,username) VALUES ('a','a','a@test','imap','smtp','a'),('b','b','b@test','imap','smtp','b')",
		"INSERT INTO folders (id,account_id,name,path,folder_type) VALUES ('fb','b','INBOX','INBOX','inbox')",
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	a := &App{folderStore: folder.NewStore(db), messageStore: message.NewStore(db)}
	if err := a.messageStore.Create(&message.Message{ID: "mb", AccountID: "b", FolderID: "fb", UID: 1, Subject: "private", ReadReceiptTo: "b@test"}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.SearchConversations("a", "fb", "", 0, 10, ""); err == nil {
		t.Fatal("cross-account search accepted")
	}
	if _, err := a.GetSearchCount("a", "fb", "", ""); err == nil {
		t.Fatal("cross-account count accepted")
	}
	if err := a.SendReadReceipt("a", "mb"); err == nil {
		t.Fatal("cross-account receipt accepted")
	}
	if err := a.IgnoreReadReceipt("a", "mb"); err == nil {
		t.Fatal("cross-account ignore accepted")
	}
	var handled bool
	if err := db.QueryRow("SELECT read_receipt_handled FROM messages WHERE id='mb'").Scan(&handled); err != nil || handled {
		t.Fatal("foreign message changed", err)
	}
	if err := a.IgnoreReadReceipt("b", "mb"); err != nil {
		t.Fatal("own account rejected", err)
	}
}

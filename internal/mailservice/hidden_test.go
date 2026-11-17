package mailservice

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/hkdb/aerion/internal/account"
	"github.com/hkdb/aerion/internal/database"
	"github.com/hkdb/aerion/internal/folder"
	"github.com/hkdb/aerion/internal/message"
	mailSync "github.com/hkdb/aerion/internal/sync"
	"github.com/hkdb/aerion/internal/virtualfolder"
)

func TestHiddenVirtualSourceKeepsCachedConversationsWithoutProvider(t *testing.T) {
	db, err := database.Open(filepath.Join(t.TempDir(), "mail.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = db.Migrate(); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO accounts(id,name,email,imap_host,smtp_host,username,enabled) VALUES ('a','A','a@example.org','imap.example.org','smtp.example.org','a',1)`); err != nil {
		t.Fatal(err)
	}
	fs := folder.NewStore(db)
	if err = fs.Create(&folder.Folder{ID: "old", AccountID: "a", Name: "Old", Path: "Old", Type: folder.TypeFolder}); err != nil {
		t.Fatal(err)
	}
	if err = fs.SetHiddenBatch("a", []string{"old"}, true); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO messages(id,account_id,folder_id,uid,subject,date,thread_id) VALUES ('m','a','old',1,'Retained mail',CURRENT_TIMESTAMP,'thread')`); err != nil {
		t.Fatal(err)
	}
	vs := virtualfolder.NewStore(db)
	vf := &virtualfolder.VirtualFolder{ID: "v", Name: "Saved", FolderIDs: []string{"old"}}
	if err = vs.Create(vf); err != nil {
		t.Fatal(err)
	}
	as := account.NewStore(db)
	ms := message.NewStore(db)
	// A nil IMAP pool guarantees accidental provider access fails loudly.
	svc := New(Options{Accounts: as, Folders: fs, Messages: ms, VirtualFolders: vs, Sync: mailSync.NewEngine(nil, as, fs, ms, message.NewAttachmentStore(db))})
	page, err := svc.Search(context.Background(), MailSearchRequest{VirtualFolderID: "v", Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Conversations) != 1 || page.Conversations[0].Subject != "Retained mail" {
		t.Fatalf("cached mail missing: %#v", page)
	}
	if len(page.Issues) != 1 || !page.Issues[0].Warning {
		t.Fatalf("expected stale source is not a provider failure: %#v", page.Issues)
	}
	if page.Cursor == "" {
		t.Fatal("cached pagination lost")
	}
	page, err = svc.Search(context.Background(), MailSearchRequest{VirtualFolderID: "v", Limit: 1, Cursor: page.Cursor})
	if err != nil || len(page.Conversations) != 0 || page.Cursor != "" {
		t.Fatalf("pagination %#v %v", page, err)
	}
	if _, err = db.Exec(`UPDATE messages SET is_read=1,date=datetime('now') WHERE id='m'`); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO messages(id,account_id,folder_id,uid,subject,date,thread_id,is_read) VALUES ('older','a','old',2,'Older unread',datetime('now','-1 day'),'other-thread',0)`); err != nil {
		t.Fatal(err)
	}
	unread, err := svc.Search(context.Background(), MailSearchRequest{VirtualFolderID: "v", Limit: 1, Filter: "unread"})
	if err != nil || len(unread.Conversations) != 1 || unread.Conversations[0].Subject != "Older unread" || len(unread.Issues) != 1 || !unread.Issues[0].Warning {
		t.Fatalf("filtered cached page inaccessible: %#v %v", unread, err)
	}

	vf.SearchQuery = "from:alice"
	if err = vs.Update(vf); err != nil {
		t.Fatal(err)
	}
	if err = vs.SaveSearchCachePage(vf, "a", []virtualfolder.CachedConversation{{AccountID: "a", ConversationID: "thread"}}, true); err != nil {
		t.Fatal(err)
	}
	saved, err := svc.Search(context.Background(), MailSearchRequest{VirtualFolderID: "v", Limit: 10})
	if err != nil || len(saved.Conversations) != 1 || saved.Conversations[0].Subject != "Retained mail" {
		t.Fatalf("provider operator lost retained results: %#v %v", saved, err)
	}

	retained, err := vs.Get("v")
	if err != nil || len(retained.FolderIDs) != 1 || retained.FolderIDs[0] != "old" {
		t.Fatal("membership changed", err)
	}
}

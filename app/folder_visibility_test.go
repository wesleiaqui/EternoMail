package app

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/hkdb/aerion/internal/database"
	"github.com/hkdb/aerion/internal/folder"
)

func TestHiddenFolderLeavesTreeAndDisablesPrefetchWithoutChangingOverride(t *testing.T) {
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
	fs := folder.NewStore(db)
	for _, f := range []*folder.Folder{{ID: "parent", AccountID: "a", Name: "Old", Path: "Old", Type: folder.TypeFolder}, {ID: "child", AccountID: "a", Name: "Child", Path: "Old/Child", Type: folder.TypeFolder, ParentID: "parent"}} {
		if err = fs.Create(f); err != nil {
			t.Fatal(err)
		}
	}
	policy := folder.NewContentPolicyStore(db)
	if err = policy.Set("a", "parent", true); err != nil {
		t.Fatal(err)
	}
	a := &App{ctx: context.Background(), folderStore: fs, contentPolicyStore: policy}
	if err = fs.SetHiddenBatch("a", []string{"parent"}, true); err != nil {
		t.Fatal(err)
	}
	tree, err := a.GetFolderTree("a")
	if err != nil || len(tree) != 1 || tree[0].Folder.ID != "child" {
		t.Fatalf("visible child lost or hidden parent returned: %#v %v", tree, err)
	}
	catalog, err := a.GetFolders("a")
	if err != nil || len(catalog) != 2 {
		t.Fatal("hidden catalog not retained", err)
	}
	effective, err := a.GetFolderContentPolicy("a", "parent")
	if err != nil || effective.BodyPrefetch || effective.Source != "hidden" {
		t.Fatalf("hidden prefetch allowed: %#v %v", effective, err)
	}
	if err = fs.SetHiddenBatch("a", []string{"parent"}, false); err != nil {
		t.Fatal(err)
	}
	effective, err = a.GetFolderContentPolicy("a", "parent")
	if err != nil || !effective.BodyPrefetch || !effective.Explicit {
		t.Fatalf("override not restored: %#v %v", effective, err)
	}
	tree, err = a.GetFolderTree("a")
	if err != nil || len(tree) != 1 || tree[0].Folder.ID != "parent" || len(tree[0].Children) != 1 {
		t.Fatalf("tree not restored %#v %v", tree, err)
	}
}

func TestForceRefreshHiddenFolderKeepsCachedContent(t *testing.T) {
	db, err := database.Open(filepath.Join(t.TempDir(), "force.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = db.Migrate(); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO accounts(id,name,email,imap_host,smtp_host,username) VALUES ('a','A','a@example.org','imap','smtp','a'); INSERT INTO folders(id,account_id,name,path,folder_type,hidden) VALUES ('f','a','Old','Old','folder',1); INSERT INTO messages(id,account_id,folder_id,uid,body_text,body_fetched) VALUES ('m','a','f',1,'Keep body',1);`); err != nil {
		t.Fatal(err)
	}
	a := &App{folderStore: folder.NewStore(db)}
	if err = a.ForceSyncFolder("a", "f"); err == nil {
		t.Fatal("hidden force refresh accepted")
	}
	var body string
	if err = db.QueryRow(`SELECT body_text FROM messages WHERE id='m'`).Scan(&body); err != nil || body != "Keep body" {
		t.Fatal("content lost", body, err)
	}
}

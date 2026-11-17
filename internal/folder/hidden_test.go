package folder

import "testing"

func TestHiddenBatchPreservesMailAndDiscoveryAndRejectsCrossAccount(t *testing.T) {
	db := openTestDB(t)
	createTestAccount(t, db, "a")
	createTestAccount(t, db, "b")
	store := NewStore(db)
	old := &Folder{ID: "old", AccountID: "a", Path: "Archive/2016", Name: "2016", Type: TypeFolder, Subscribed: true}
	other := &Folder{ID: "other", AccountID: "b", Path: "Archive/2016", Name: "2016", Type: TypeFolder, Subscribed: true}
	for _, f := range []*Folder{old, other} {
		if err := store.Create(f); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`INSERT INTO messages (id,account_id,folder_id,uid,subject) VALUES ('mail','a','old',1,'Retain me')`); err != nil {
		t.Fatal(err)
	}
	if err := store.SetHiddenBatch("a", []string{"old", "other"}, true); err == nil {
		t.Fatal("cross-account batch accepted")
	}
	if f, _ := store.Get("old"); f.Hidden {
		t.Fatal("partial batch was committed")
	}
	if err := store.SetHiddenBatch("a", []string{"old"}, true); err != nil {
		t.Fatal(err)
	}
	if err := store.Upsert(&Folder{AccountID: "a", Path: old.Path, Name: "Renamed", Type: TypeFolder, Subscribed: true}); err != nil {
		t.Fatal(err)
	}
	got, err := store.GetByPath("a", old.Path)
	if err != nil || !got.Hidden || !got.Subscribed || got.ID != "old" {
		t.Fatalf("discovery lost preference/subscription/identity: %#v %v", got, err)
	}
	listed, _ := store.ListSubscribed("a")
	if len(listed) != 1 || !listed[0].Hidden {
		t.Fatal("catalog must retain hidden folder")
	}
	if visible := WithoutHidden(listed); len(visible) != 0 {
		t.Fatal("refresh list contains hidden folder")
	}
	var count int
	if err := db.QueryRow("SELECT count(*) FROM messages WHERE id='mail'").Scan(&count); err != nil || count != 1 {
		t.Fatal("mail deleted", err)
	}
	if err := store.SetHiddenBatch("a", []string{"old"}, false); err != nil {
		t.Fatal(err)
	}
	listed, _ = store.List("a")
	if len(WithoutHidden(listed)) != 1 {
		t.Fatal("show did not restore visibility")
	}
}

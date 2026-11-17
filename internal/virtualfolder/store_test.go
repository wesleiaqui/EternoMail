package virtualfolder

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/hkdb/aerion/internal/database"
)

func setupTestDB(t *testing.T) *database.DB {
	t.Helper()
	db, err := database.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Failed to create in-memory database: %v", err)
	}

	if err := db.Migrate(); err != nil {
		t.Fatalf("Failed to migrate database: %v", err)
	}

	// Create test accounts and folders to satisfy foreign key constraints
	_, err = db.Exec(`
		INSERT INTO accounts (id, name, email, imap_host, smtp_host, username)
		VALUES
			('account1', 'Account 1', 'user1@example.com', 'imap.example.com', 'smtp.example.com', 'user1'),
			('account2', 'Account 2', 'user2@example.com', 'imap.example.com', 'smtp.example.com', 'user2')
	`)
	if err != nil {
		t.Fatalf("Failed to insert test accounts: %v", err)
	}

	_, err = db.Exec(`
		INSERT INTO folders (id, account_id, name, path, folder_type)
		VALUES
			('folder1', 'account1', 'Inbox', 'INBOX', 'inbox'),
			('folder2', 'account1', 'Work', 'Work', 'folder'),
			('folder3', 'account2', 'Inbox', 'INBOX', 'inbox')
	`)
	if err != nil {
		t.Fatalf("Failed to insert test folders: %v", err)
	}

	return db
}

func TestStore_CreateGetList(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	store := NewStore(db)

	vFolders := []*VirtualFolder{
		{
			Name:       "Needs Action",
			OrderIndex: 0,
			FolderIDs:  []string{"folder1", "folder2"},
		},
		{
			Name:       "All Inboxes",
			OrderIndex: 1,
			FolderIDs:  []string{"folder1", "folder3"},
		},
	}

	for i, vf := range vFolders {
		err := store.Create(vf)
		if err != nil {
			t.Fatalf("Failed to create virtual folder %d: %v", i, err)
		}
		if vf.ID == "" {
			t.Fatalf("Virtual folder %d missing ID after Create", i)
		}
		if vf.CreatedAt.IsZero() || vf.UpdatedAt.IsZero() {
			t.Fatalf("Virtual folder %d missing timestamps after Create", i)
		}
	}

	// Test Get
	got, err := store.Get(vFolders[0].ID)
	if err != nil {
		t.Fatalf("Failed to get virtual folder: %v", err)
	}
	if got.Name != vFolders[0].Name {
		t.Errorf("Expected Name %q, got %q", vFolders[0].Name, got.Name)
	}
	if len(got.FolderIDs) != len(vFolders[0].FolderIDs) {
		t.Errorf("Expected %d folder IDs, got %d", len(vFolders[0].FolderIDs), len(got.FolderIDs))
	}

	// Test List
	list, err := store.List()
	if err != nil {
		t.Fatalf("Failed to list virtual folders: %v", err)
	}
	if len(list) != len(vFolders) {
		t.Errorf("Expected %d virtual folders, got %d", len(vFolders), len(list))
	}

	// Ensure sorted by OrderIndex
	if list[0].Name != "Needs Action" || list[1].Name != "All Inboxes" {
		t.Errorf("List returned in unexpected order")
	}
}

func TestStore_Update(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	store := NewStore(db)

	vf := &VirtualFolder{
		Name:       "Initial",
		OrderIndex: 0,
		FolderIDs:  []string{"folder1"},
	}
	err := store.Create(vf)
	if err != nil {
		t.Fatalf("Failed to create: %v", err)
	}

	// Update Name and FolderIDs
	vf.Name = "Updated"
	vf.FolderIDs = []string{"folder2", "folder3"}

	err = store.Update(vf)
	if err != nil {
		t.Fatalf("Failed to update: %v", err)
	}

	got, err := store.Get(vf.ID)
	if err != nil {
		t.Fatalf("Failed to get after update: %v", err)
	}

	if got.Name != "Updated" {
		t.Errorf("Expected Name 'Updated', got %q", got.Name)
	}
	if len(got.FolderIDs) != 2 || got.FolderIDs[0] != "folder2" || got.FolderIDs[1] != "folder3" {
		t.Errorf("Expected FolderIDs [folder2, folder3], got %v", got.FolderIDs)
	}
	if got.UpdatedAt.Before(got.CreatedAt) || got.UpdatedAt.Equal(got.CreatedAt) {
		t.Errorf("UpdatedAt should be after CreatedAt")
	}
}

func TestStore_Delete(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	store := NewStore(db)

	vf := &VirtualFolder{
		Name:       "To Delete",
		OrderIndex: 0,
		FolderIDs:  []string{"folder1"},
	}
	if err := store.Create(vf); err != nil {
		t.Fatal(err)
	}

	err := store.Delete(vf.ID)
	if err != nil {
		t.Fatalf("Failed to delete: %v", err)
	}

	got, err := store.Get(vf.ID)
	if err != nil {
		t.Fatalf("Get after delete returned error: %v", err)
	}
	if got != nil {
		t.Errorf("Expected Get to return nil after delete, got %v", got)
	}
}

func TestStore_CascadingDelete(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	store := NewStore(db)

	vf := &VirtualFolder{
		Name:       "Cascade Test",
		OrderIndex: 0,
		FolderIDs:  []string{"folder1", "folder2"},
	}
	if err := store.Create(vf); err != nil {
		t.Fatal(err)
	}

	// Delete physical folder 1
	_, err := db.Exec(`DELETE FROM folders WHERE id = 'folder1'`)
	if err != nil {
		t.Fatalf("Failed to delete physical folder: %v", err)
	}

	got, err := store.Get(vf.ID)
	if err != nil {
		t.Fatalf("Failed to get virtual folder: %v", err)
	}

	// Should only have folder2 now
	if len(got.FolderIDs) != 1 || got.FolderIDs[0] != "folder2" {
		t.Errorf("Expected FolderIDs to just have folder2, got %v", got.FolderIDs)
	}
}

// Unread counts for a view with a date window must include messages whose date
// the app stored as a Go time.Time; unixepoch() alone is NULL for that format.
func TestStore_UnreadCountsWithDateWindowForGoTimes(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	store := NewStore(db)

	vf := &VirtualFolder{Name: "Last week", FolderIDs: []string{"folder1"}, DateWindow: DateWindow7Day}
	if err := store.Create(vf); err != nil {
		t.Fatalf("Failed to create virtual folder: %v", err)
	}

	eest := time.FixedZone("EEST", 3*3600)
	for i, date := range []time.Time{
		time.Now().Add(-24 * time.Hour).In(eest),
		time.Now().Add(-400 * 24 * time.Hour).In(eest),
	} {
		id := fmt.Sprintf("m%d", i)
		if _, err := db.Exec(
			`INSERT INTO messages (id, account_id, folder_id, uid, thread_id, subject, date, is_read, received_at)
			 VALUES (?, 'account1', 'folder1', ?, ?, ?, ?, 0, ?)`,
			id, i+1, id, id, date, time.Now()); err != nil {
			t.Fatalf("Failed to insert message %s: %v", id, err)
		}
	}

	counts, err := store.UnreadCounts()
	if err != nil {
		t.Fatalf("UnreadCounts: %v", err)
	}
	if got := counts[vf.ID]; got != 1 {
		t.Errorf("unread count = %d, want 1 (only the message inside the window)", got)
	}
}

func TestStore_SearchQueryRoundtrip(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	store := NewStore(db)

	vf := &VirtualFolder{
		Name:        "Invoices",
		FolderIDs:   []string{"folder1"},
		SearchQuery: "  invoice  ",
	}
	if err := store.Create(vf); err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, err := store.Get(vf.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.SearchQuery != "invoice" {
		t.Errorf("Get searchQuery = %q, want trimmed %q", got.SearchQuery, "invoice")
	}
	list, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 || list[0].SearchQuery != "invoice" {
		t.Errorf("List searchQuery = %+v, want one folder with query %q", list, "invoice")
	}

	vf.SearchQuery = "receipt"
	if err := store.Update(vf); err != nil {
		t.Fatalf("Update: %v", err)
	}
	got, err = store.Get(vf.ID)
	if err != nil {
		t.Fatalf("Get after update: %v", err)
	}
	if got.SearchQuery != "receipt" {
		t.Errorf("updated searchQuery = %q, want %q", got.SearchQuery, "receipt")
	}
}

func TestStore_CacheSaveLoadDelete(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	store := NewStore(db)

	vf := &VirtualFolder{Name: "Cached", FolderIDs: []string{"folder1"}}
	if err := store.Create(vf); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if _, _, found, err := store.LoadCache(vf.ID); err != nil || found {
		t.Errorf("LoadCache before save: found=%v err=%v, want false/nil", found, err)
	}

	entries := []CachedConversation{
		{AccountID: "account1", ConversationID: "conv-b"},
		{AccountID: "account1", ConversationID: "conv-a"},
	}
	if err := store.SaveCache(vf.ID, entries); err != nil {
		t.Fatalf("SaveCache: %v", err)
	}
	got, refreshedAt, found, err := store.LoadCache(vf.ID)
	if err != nil || !found {
		t.Fatalf("LoadCache: found=%v err=%v", found, err)
	}
	if len(got) != 2 || got[0].ConversationID != "conv-b" || got[1].ConversationID != "conv-a" {
		t.Errorf("cache order = %+v, want conv-b then conv-a", got)
	}
	if refreshedAt.IsZero() || time.Since(refreshedAt) > time.Minute {
		t.Errorf("refreshedAt = %v, want a fresh timestamp", refreshedAt)
	}

	// Saving again replaces rather than appends.
	if err := store.SaveCache(vf.ID, entries[:1]); err != nil {
		t.Fatalf("SaveCache replace: %v", err)
	}
	got, _, found, err = store.LoadCache(vf.ID)
	if err != nil || !found || len(got) != 1 || got[0].ConversationID != "conv-b" {
		t.Errorf("after replace: got=%+v found=%v err=%v", got, found, err)
	}

	if err := store.Delete(vf.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, _, found, err := store.LoadCache(vf.ID); err != nil || found {
		t.Errorf("LoadCache after delete: found=%v err=%v, want false/nil", found, err)
	}
}

func TestDefinitionEditInvalidatesRetainedMembershipButRenameKeepsIt(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	s := NewStore(db)
	vf := &VirtualFolder{ID: "saved", Name: "Saved", FolderIDs: []string{"folder1", "folder2"}, SearchQuery: "from:alice"}
	if err := s.Create(vf); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveSearchCachePage(vf, "account1", []CachedConversation{{AccountID: "account1", ConversationID: "from-removed-folder"}}, true); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE folders SET hidden=1 WHERE id='folder1'`); err != nil {
		t.Fatal(err)
	}
	vf.Name = "Renamed"
	if err := s.Update(vf); err != nil {
		t.Fatal(err)
	}
	if refs, _, found, err := s.LoadCache(vf.ID); err != nil || !found || len(refs) != 1 {
		t.Fatal("rename lost membership", err)
	}
	vf.FolderIDs = []string{"folder1"}
	if err := s.Update(vf); err != nil {
		t.Fatal(err)
	}
	if refs, _, found, err := s.LoadCache(vf.ID); err != nil || found || len(refs) != 0 {
		t.Fatal("edited scope retained stale membership", err)
	}
}

func TestProviderSearchCachePagesRejectStaleDefinition(t *testing.T) {
	db := setupTestDB(t)
	store := NewStore(db)
	defer db.Close()
	vf := &VirtualFolder{Name: "Operator", SearchQuery: "from:alice"}
	if err := store.Create(vf); err != nil {
		t.Fatal(err)
	}
	first := []CachedConversation{{AccountID: "a", ConversationID: "one"}}
	second := []CachedConversation{{AccountID: "a", ConversationID: "two"}}
	if err := store.SaveSearchCachePage(vf, "a", first, true); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveSearchCachePage(vf, "a", second, false); err != nil {
		t.Fatal(err)
	}
	refs, _, _, err := store.LoadCache(vf.ID)
	if err != nil || len(refs) != 2 {
		t.Fatalf("pages %#v %v", refs, err)
	}
	if err := store.SaveCache(vf.ID, nil); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveCacheContext(context.Background(), vf.ID, nil); err != nil {
		t.Fatal(err)
	}
	if refs, _, _, err := store.LoadCache(vf.ID); err != nil || len(refs) != 2 {
		t.Fatalf("local maintenance replaced provider cache %#v %v", refs, err)
	}
	stale := *vf
	vf.SearchQuery = "from:bob"
	if err := store.Update(vf); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveSearchCachePage(&stale, "a", first, true); err != nil {
		t.Fatal(err)
	}
	if refs, _, found, err := store.LoadCache(vf.ID); err != nil || found || len(refs) != 0 {
		t.Fatalf("stale search repopulated edited view %#v %v %v", refs, found, err)
	}
}

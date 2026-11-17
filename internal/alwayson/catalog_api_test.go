package alwayson

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/hkdb/aerion/internal/database"
	"github.com/hkdb/aerion/internal/message"
	"github.com/hkdb/aerion/internal/virtualfolder"
)

func TestGetCachedVirtualFolderViewHydratesServerCacheInOrder(t *testing.T) {
	db, runtime, view := newCachedVirtualFolderTestRuntime(t)
	defer db.Close()
	cached, err := runtime.GetCachedVirtualFolderView(context.Background(), view.ID)
	if err != nil {
		t.Fatalf("GetCachedVirtualFolderView: %v", err)
	}
	if !cached.HasCache || cached.VirtualFolderID != view.ID || cached.RefreshedAt.IsZero() {
		t.Fatalf("cache metadata = %#v, want view ID, found cache, and refresh time", cached)
	}
	if len(cached.Conversations) != 2 || cached.Conversations[0].ThreadID != "thread-b" || cached.Conversations[1].ThreadID != "thread-a" {
		t.Fatalf("cached conversation order = %#v, want thread-b then thread-a", cached.Conversations)
	}
	if cached.Conversations[1].UnreadCount != 1 || cached.Conversations[1].Snippet != "fresh snippet" {
		t.Fatalf("hydrated current row = %#v, want unread count and fresh snippet", cached.Conversations[1])
	}
}

func TestVirtualFolderCacheMaintenanceRefreshesWithoutDesktopRequests(t *testing.T) {
	db, runtime, view := newCachedVirtualFolderTestRuntime(t)
	defer db.Close()
	ctx, cancel := context.WithCancel(context.Background())
	runtime.lifecycleWG.Add(1)
	go runtime.runVirtualFolderCacheMaintenance(ctx, 5*time.Millisecond)
	defer func() {
		cancel()
		runtime.lifecycleWG.Wait()
	}()

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if _, _, found, err := runtime.virtualFolders.LoadCache(view.ID); err != nil {
			t.Fatal(err)
		} else if found {
			break
		}
		time.Sleep(time.Millisecond)
	}
	newMessage := message.Message{ID: "msg-c", AccountID: "acc", FolderID: "inbox", UID: 3, ThreadID: "thread-c", Subject: "C", Date: time.Now().UTC().Add(2 * time.Minute)}
	if err := runtime.messages.Create(&newMessage); err != nil {
		t.Fatal(err)
	}

	for time.Now().Before(deadline.Add(time.Second)) {
		entries, _, found, err := runtime.virtualFolders.LoadCache(view.ID)
		if err != nil {
			t.Fatal(err)
		}
		if found && cachedConversationPresent(entries, "thread-c") {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("helper cache maintenance did not refresh after a new message arrived")
}

func newCachedVirtualFolderTestRuntime(t *testing.T) (*database.DB, *SyncRuntime, *virtualfolder.VirtualFolder) {
	t.Helper()
	db, err := database.Open(filepath.Join(t.TempDir(), "mail.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO accounts(id,name,email,imap_host,smtp_host,username) VALUES ('acc','Account','me@example.com','imap.example.com','smtp.example.com','me')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO folders(id,account_id,name,path,folder_type) VALUES ('inbox','acc','Inbox','INBOX','inbox')`); err != nil {
		t.Fatal(err)
	}
	messages := message.NewStore(db)
	for _, item := range []message.Message{
		{ID: "msg-a", AccountID: "acc", FolderID: "inbox", UID: 1, ThreadID: "thread-a", Subject: "A", Snippet: "fresh snippet", Date: time.Now().UTC(), IsRead: false},
		{ID: "msg-b", AccountID: "acc", FolderID: "inbox", UID: 2, ThreadID: "thread-b", Subject: "B", Date: time.Now().UTC().Add(time.Minute), IsRead: true},
	} {
		if err := messages.Create(&item); err != nil {
			t.Fatal(err)
		}
	}
	views := virtualfolder.NewStore(db)
	view := &virtualfolder.VirtualFolder{Name: "Cached", FolderIDs: []string{"inbox"}, FolderMode: virtualfolder.FolderModeInclude}
	if err := views.Create(view); err != nil {
		t.Fatal(err)
	}
	if err := views.SaveCache(view.ID, []virtualfolder.CachedConversation{
		{AccountID: "acc", ConversationID: "thread-b"},
		{AccountID: "acc", ConversationID: "thread-a"},
	}); err != nil {
		t.Fatal(err)
	}

	runtime := &SyncRuntime{virtualFolders: views, messages: messages}
	return db, runtime, view
}

func cachedConversationPresent(entries []virtualfolder.CachedConversation, id string) bool {
	for _, entry := range entries {
		if entry.ConversationID == id {
			return true
		}
	}
	return false
}

func TestSavedOperatorCacheMaintenanceDoesNotReplaceProviderMembership(t *testing.T) {
	db, runtime, view := newCachedVirtualFolderTestRuntime(t)
	defer db.Close()
	view.SearchQuery = "from:alice"
	if err := runtime.virtualFolders.Update(view); err != nil {
		t.Fatal(err)
	}
	if err := runtime.virtualFolders.SaveSearchCachePage(view, "acc", []virtualfolder.CachedConversation{{AccountID: "acc", ConversationID: "thread-a"}}, true); err != nil {
		t.Fatal(err)
	}
	if err := runtime.RefreshVirtualFolderCache(view.ID); err != nil {
		t.Fatal(err)
	}
	if err := runtime.refreshVirtualFolderCacheContext(context.Background(), view.ID); err != nil {
		t.Fatal(err)
	}
	cached, err := runtime.GetCachedVirtualFolderView(context.Background(), view.ID)
	if err != nil || !cached.HasCache || len(cached.Conversations) != 1 || cached.Conversations[0].ThreadID != "thread-a" {
		t.Fatalf("provider membership overwritten: %#v %v", cached, err)
	}
}

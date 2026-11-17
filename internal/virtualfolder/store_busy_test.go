package virtualfolder

import (
	"context"
	"database/sql"
	"net/url"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/hkdb/aerion/internal/database"
)

// Reproduces the virtual-folder "failed to begin transaction: database is
// locked (5) (SQLITE_BUSY)" failure: while another connection holds the write
// lock (sync churn, FTS batches), a short-timeout store hits SQLITE_BUSY
// immediately, and only the busy retry lets the write land once the holder
// releases. Uses a 50 ms busy_timeout so contention surfaces in milliseconds
// instead of the production 30 s.
func TestSaveCacheRetriesTransientLockContention(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vf-busy.db")
	db, err := database.Open(path, database.WithBusyTimeout(50))
	if err != nil {
		t.Fatalf("database.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Migrate(); err != nil {
		t.Fatalf("db.Migrate: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO accounts (id, name, email, imap_host, smtp_host, username)
		 VALUES ('acc', 'Account', 'a@example.com', 'imap.example.com', 'smtp.example.com', 'a')`); err != nil {
		t.Fatalf("seed account: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO folders (id, account_id, name, path, folder_type) VALUES ('fld', 'acc', 'INBOX', 'INBOX', 'inbox')`); err != nil {
		t.Fatalf("seed folder: %v", err)
	}
	store := NewStore(db)
	if err := store.Create(&VirtualFolder{ID: "vf", Name: "lukematta", FolderIDs: []string{"fld"}}); err != nil {
		t.Fatalf("create virtual folder: %v", err)
	}

	// A second connection simulates the competing writer (sync holding the
	// lock). It holds for 400 ms, far past the store's 50 ms busy_timeout.
	uri := url.URL{Scheme: "file", Path: path}
	holder, err := sql.Open("sqlite", uri.String()+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	if err != nil {
		t.Fatalf("open holder: %v", err)
	}
	defer holder.Close()
	holderTx, err := holder.Begin()
	if err != nil {
		t.Fatalf("holder begin: %v", err)
	}
	if _, err := holderTx.Exec(`UPDATE virtual_folders SET updated_at = updated_at WHERE id = 'vf'`); err != nil {
		t.Fatalf("holder write: %v", err)
	}
	released := make(chan struct{})
	go func() {
		time.Sleep(400 * time.Millisecond)
		_ = holderTx.Commit()
		close(released)
	}()

	entries := []CachedConversation{{AccountID: "acc", ConversationID: "conv-1"}}
	start := time.Now()
	if err := store.SaveCache("vf", entries); err != nil {
		t.Fatalf("SaveCache under contention: %v (want the busy retry to absorb the 400 ms lock hold)", err)
	}
	if elapsed := time.Since(start); elapsed < 400*time.Millisecond {
		t.Fatalf("SaveCache returned in %s, before the lock was released; retry did not wait for the holder", elapsed)
	}
	<-released

	refs, _, hasCache, err := store.LoadCache("vf")
	if err != nil {
		t.Fatalf("LoadCache: %v", err)
	}
	if !hasCache || len(refs) != 1 || refs[0].ConversationID != "conv-1" {
		t.Fatalf("cache = %#v hasCache=%v, want conv-1", refs, hasCache)
	}
}

// The detached cache refresh must not wait out the store's configured
// busy_timeout: the modernc driver does not interrupt a BEGIN IMMEDIATE busy
// wait on context expiry, so SaveCacheContext lowers busy_timeout on a
// pinned connection instead. With the lock held and a 5 s configured
// timeout, returning near the 2 s detached bound proves the override works.
// SQLite busy handlers may stop early (observed ~1.4 s for a 2 s limit).
// The lower bound distinguishes this path from the 300 ms canceled context;
// the upper bound distinguishes it from the configured 5 s timeout.
func TestSaveCacheContextBoundsTheLockWait(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vf-busy-ctx.db")
	db, err := database.Open(path, database.WithBusyTimeout(5000))
	if err != nil {
		t.Fatalf("database.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Migrate(); err != nil {
		t.Fatalf("db.Migrate: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO accounts (id, name, email, imap_host, smtp_host, username)
		 VALUES ('acc', 'Account', 'a@example.com', 'imap.example.com', 'smtp.example.com', 'a')`); err != nil {
		t.Fatalf("seed account: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO folders (id, account_id, name, path, folder_type) VALUES ('fld', 'acc', 'INBOX', 'INBOX', 'inbox')`); err != nil {
		t.Fatalf("seed folder: %v", err)
	}
	store := NewStore(db)
	if err := store.Create(&VirtualFolder{ID: "vf", Name: "lukematta", FolderIDs: []string{"fld"}}); err != nil {
		t.Fatalf("create virtual folder: %v", err)
	}

	uri := url.URL{Scheme: "file", Path: path}
	holder, err := sql.Open("sqlite", uri.String()+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	if err != nil {
		t.Fatalf("open holder: %v", err)
	}
	defer holder.Close()
	holderTx, err := holder.Begin()
	if err != nil {
		t.Fatalf("holder begin: %v", err)
	}
	if _, err := holderTx.Exec(`UPDATE virtual_folders SET updated_at = updated_at WHERE id = 'vf'`); err != nil {
		t.Fatalf("holder write: %v", err)
	}
	defer func() { _ = holderTx.Rollback() }()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	start := time.Now()
	err = store.SaveCacheContext(ctx, "vf", []CachedConversation{{AccountID: "acc", ConversationID: "conv-1"}})
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("SaveCacheContext under a held lock: want an error, got nil")
	}
	if !database.IsBusy(err) {
		t.Fatalf("SaveCacheContext error = %v, want SQLite lock contention", err)
	}
	if elapsed < time.Second || elapsed > 4*time.Second {
		t.Fatalf("SaveCacheContext waited %s; want the ~2 s detached bound, not the configured 5 s busy_timeout", elapsed)
	}
}

// Cancellation safety for the detached path rests on the lowered
// busy_timeout, not on the context (Devin finding store.go:152): a context
// cancelled mid-wait must NOT shorten the lock wait, because the modernc
// driver does not interrupt a BEGIN IMMEDIATE busy wait. Pin the contract so
// the bound cannot silently regress to context-based cancellation.
func TestSaveCacheContextBoundRestsOnBusyTimeoutNotContext(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vf-busy-ctx-cancel.db")
	db, err := database.Open(path, database.WithBusyTimeout(5000))
	if err != nil {
		t.Fatalf("database.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Migrate(); err != nil {
		t.Fatalf("db.Migrate: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO accounts (id, name, email, imap_host, smtp_host, username)
		 VALUES ('acc', 'Account', 'a@example.com', 'imap.example.com', 'smtp.example.com', 'a')`); err != nil {
		t.Fatalf("seed account: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO folders (id, account_id, name, path, folder_type) VALUES ('fld', 'acc', 'INBOX', 'INBOX', 'inbox')`); err != nil {
		t.Fatalf("seed folder: %v", err)
	}
	store := NewStore(db)
	if err := store.Create(&VirtualFolder{ID: "vf", Name: "lukematta", FolderIDs: []string{"fld"}}); err != nil {
		t.Fatalf("create virtual folder: %v", err)
	}

	uri := url.URL{Scheme: "file", Path: path}
	holder, err := sql.Open("sqlite", uri.String()+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	if err != nil {
		t.Fatalf("open holder: %v", err)
	}
	defer holder.Close()
	holderTx, err := holder.Begin()
	if err != nil {
		t.Fatalf("holder begin: %v", err)
	}
	if _, err := holderTx.Exec(`UPDATE virtual_folders SET updated_at = updated_at WHERE id = 'vf'`); err != nil {
		t.Fatalf("holder write: %v", err)
	}
	defer func() { _ = holderTx.Rollback() }()

	// Cancel 300 ms into the wait: the write must still wait out the ~2 s
	// busy_timeout override and report lock contention, not context.Canceled.
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(300 * time.Millisecond)
		cancel()
	}()
	start := time.Now()
	err = store.SaveCacheContext(ctx, "vf", []CachedConversation{{AccountID: "acc", ConversationID: "conv-1"}})
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("SaveCacheContext under a held lock: want an error, got nil")
	}
	if !database.IsBusy(err) {
		t.Fatalf("SaveCacheContext error = %v, want lock contention; context cancellation must not interrupt the wait", err)
	}
	if elapsed < time.Second || elapsed > 4*time.Second {
		t.Fatalf("SaveCacheContext waited %s; want the ~2 s busy_timeout override, unaffected by the 300 ms context cancellation", elapsed)
	}
}

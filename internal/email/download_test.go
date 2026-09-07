package email

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/hkdb/aerion/internal/message"
)

func TestSaveAttachmentSafeNames(t *testing.T) {
	for _, name := range []string{"", ".", "..", "/", "../../report.txt", `..\..\report.txt`, "\x00..", "..\x00/report.txt"} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			downloader := NewAttachmentDownloader(dir)
			for _, id := range []string{"", "short", "../../../../outside"} {
				path, err := downloader.SaveAttachment(&message.Attachment{Filename: name, MessageID: id}, []byte("data"), "")
				if err != nil {
					t.Fatal(err)
				}
				rel, err := filepath.Rel(dir, path)
				if err != nil || !filepath.IsLocal(rel) {
					t.Fatalf("escaped attachment directory: %q", path)
				}
				data, err := os.ReadFile(path)
				if err != nil || string(data) != "data" {
					t.Fatalf("saved content = %q, %v", data, err)
				}
			}
		})
	}
}

func TestSaveAttachmentRejectsCustomTraversal(t *testing.T) {
	dir := t.TempDir()
	downloader := NewAttachmentDownloader(dir)
	for _, path := range []string{"../escape.txt", dir + "/../escape.txt", dir + `\..\escape.txt`, dir + "/bad\x00.txt", ".", dir + "/"} {
		if _, err := downloader.SaveAttachment(&message.Attachment{}, []byte("bad"), path); err == nil {
			t.Errorf("accepted %q", path)
		}
	}
	// User-selected destinations outside the cache remain valid and overwritable.
	path := filepath.Join(t.TempDir(), "chosen.txt")
	for _, content := range []string{"original", "replacement"} {
		if _, err := downloader.SaveAttachment(&message.Attachment{}, []byte(content), path); err != nil {
			t.Fatal(err)
		}
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "replacement" {
		t.Fatalf("custom content = %q, %v", data, err)
	}
}

func TestSaveAllAttachmentStaysInSelectedDirectory(t *testing.T) {
	dir := t.TempDir()
	downloader := NewAttachmentDownloader(t.TempDir())
	for _, name := range []string{"../escape.txt", `..\escape.txt`, "/tmp/escape.txt"} {
		path, err := downloader.SaveAttachmentToDirectory(&message.Attachment{Filename: name}, []byte("data"), dir)
		if err != nil {
			t.Fatal(err)
		}
		if filepath.Dir(path) != dir {
			t.Fatalf("escaped selected directory: %q", path)
		}
	}
}

func TestSaveAttachmentRejectsEscapingSymlinks(t *testing.T) {
	dir, outside := t.TempDir(), t.TempDir()
	target := filepath.Join(outside, "target.txt")
	if err := os.WriteFile(target, []byte("unchanged"), 0600); err != nil {
		t.Fatal(err)
	}
	custom := filepath.Join(dir, "link.txt")
	if err := os.Symlink(target, custom); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	downloader := NewAttachmentDownloader(dir)
	if _, err := downloader.SaveAttachment(&message.Attachment{}, []byte("bad"), custom); err == nil {
		t.Fatal("followed custom symlink outside directory")
	}
	if err := os.Symlink(outside, filepath.Join(dir, "message1")); err != nil {
		t.Fatal(err)
	}
	if _, err := downloader.SaveAttachment(&message.Attachment{MessageID: "message1", Filename: "target.txt"}, []byte("bad"), ""); err == nil {
		t.Fatal("followed message directory symlink outside cache")
	}
	data, err := os.ReadFile(target)
	if err != nil || string(data) != "unchanged" {
		t.Fatalf("outside content = %q, %v", data, err)
	}
}

func TestSaveAttachmentConcurrentConflicts(t *testing.T) {
	downloader := NewAttachmentDownloader(t.TempDir())
	att := &message.Attachment{MessageID: "message1", Filename: "report.txt"}
	const count = 12
	paths := make(chan string, count)
	var wg sync.WaitGroup
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			path, err := downloader.SaveAttachment(att, []byte("data"), "")
			if err != nil {
				t.Error(err)
				return
			}
			paths <- path
		}()
	}
	wg.Wait()
	close(paths)
	seen := map[string]bool{}
	for path := range paths {
		if seen[path] {
			t.Errorf("overwrote concurrent download: %q", path)
		}
		seen[path] = true
	}
	if len(seen) != count {
		t.Fatalf("saved %d distinct attachments, want %d", len(seen), count)
	}
	if _, err := os.Stat(filepath.Join(downloader.attachmentsDir, "message1", "report1.txt")); err != nil {
		t.Fatal(err)
	}
}

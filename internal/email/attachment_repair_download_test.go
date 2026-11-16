package email

import (
	"github.com/hkdb/aerion/internal/message"
	"os"
	"path/filepath"
	"testing"
)

func TestSaveAllSameNamePartsNeverOverwrite(t *testing.T) {
	dir := t.TempDir()
	downloader := NewAttachmentDownloader("")
	att := &message.Attachment{Filename: "same.png"}
	one, err := downloader.SaveAttachmentToDirectory(att, []byte("one"), dir)
	if err != nil {
		t.Fatal(err)
	}
	two, err := downloader.SaveAttachmentToDirectory(att, []byte("two"), dir)
	if err != nil {
		t.Fatal(err)
	}
	if one == two || filepath.Base(two) != "same (2).png" {
		t.Fatal("same-name exports collide")
	}
	for path, want := range map[string]string{one: "one", two: "two"} {
		got, err := os.ReadFile(path)
		if err != nil || string(got) != want {
			t.Fatal("export lost real part")
		}
	}
}

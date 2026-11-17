package platform

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestPortalExportNamesKeepEveryOccurrence(t *testing.T) {
	got := portalSaveNames([]string{"same.png", "same.png", "same (2).png", "../same.png", "Same.PNG", "", "a\\b.txt"})
	want := []string{"same.png", "same (2).png", "same (2) (2).png", "same (3).png", "Same (4).PNG", "attachment", "b.txt"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("names %q, want %q", got, want)
	}
	dir := t.TempDir()
	paths := make([]string, len(got))
	for i, name := range got {
		paths[i] = filepath.Join(dir, name)
	}
	if err := validatePortalSavePaths(paths, len(got)); err != nil {
		t.Fatal(err)
	}
	for i, path := range paths {
		if err := os.WriteFile(path, []byte{byte(i)}, 0600); err != nil {
			t.Fatal(err)
		}
	}
	for i, path := range paths {
		b, err := os.ReadFile(path)
		if err != nil || len(b) != 1 || b[0] != byte(i) {
			t.Fatal("part overwritten", path, err)
		}
	}
}

func TestPortalRejectsDuplicateOrIncompleteDestinationsBeforeExport(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "original")
	if err := os.WriteFile(file, []byte("preserve"), 0600); err != nil {
		t.Fatal(err)
	}
	hard := filepath.Join(dir, "hardlink")
	if err := os.Link(file, hard); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(dir, "symlink")
	if err := os.Symlink(file, alias); err != nil {
		t.Fatal(err)
	}
	realDir := filepath.Join(dir, "real")
	if err := os.Mkdir(realDir, 0700); err != nil {
		t.Fatal(err)
	}
	dirAlias := filepath.Join(dir, "alias")
	if err := os.Symlink(realDir, dirAlias); err != nil {
		t.Fatal(err)
	}
	if err := validatePortalSavePaths([]string{filepath.Join(realDir, "new"), filepath.Join(dirAlias, "new")}, 2); err == nil {
		t.Fatal("same nonexistent file through symlinked directory accepted")
	}

	for name, paths := range map[string][]string{
		"same": {file, file}, "cleaned": {file, dir + "/./original"}, "hardlink": {file, hard}, "symlink": {file, alias}, "missing": {file}, "relative": {file, "relative"},
	} {
		t.Run(name, func(t *testing.T) {
			if err := validatePortalSavePaths(paths, 2); err == nil {
				t.Fatal("ambiguous destinations accepted")
			}
		})
	}
	if b, err := os.ReadFile(file); err != nil || string(b) != "preserve" {
		t.Fatal("validation changed file", err)
	}
}

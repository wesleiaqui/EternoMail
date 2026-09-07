//go:build linux

package app

import (
	gokeyring "github.com/zalando/go-keyring"
	"os"
	"path/filepath"
	"testing"
)

func TestFailedPreflightReleasesDatabase(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", filepath.Join(dir, "data"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(dir, "cache"))
	data := filepath.Join(dir, "data", "aerion")
	if err := os.MkdirAll(data, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(data, "device.key")
	if err := os.WriteFile(path, []byte("synthetic corrupt key"), 0600); err != nil {
		t.Fatal(err)
	}
	a := &App{}
	if err := a.Preflight(); err == nil {
		t.Fatal("expected key validation failure")
	}
	if a.db != nil {
		t.Fatal("partially initialized database still published")
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != "synthetic corrupt key" {
		t.Fatal("key changed", err)
	}
}

func TestPreflightIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"XDG_DATA_HOME", "XDG_CONFIG_HOME", "XDG_CACHE_HOME"} {
		t.Setenv(name, filepath.Join(dir, name))
	}
	gokeyring.MockInit()
	a := &App{}
	if err := a.Preflight(); err != nil {
		t.Fatal(err)
	}
	defer a.db.Close()
	db, credentials := a.db, a.credStore
	if err := a.Preflight(); err != nil {
		t.Fatal(err)
	}
	if a.db != db || a.credStore != credentials {
		t.Fatal("repeated bridge call replaced initialized stores")
	}
}

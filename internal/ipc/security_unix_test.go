//go:build linux || darwin

package ipc

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestUnixSocketPermissionsAndContextCleanup(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	tm, err := NewTokenManager()
	if err != nil {
		t.Fatal(err)
	}
	s := NewUnixServer(tm)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- s.Start(ctx) }()
	deadline := time.Now().Add(2 * time.Second)
	for {
		info, err := os.Stat(s.Address())
		if err == nil && info.Mode().Perm() == 0600 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("socket not ready with 0600")
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("Start ignored cancellation")
	}
	if _, err := os.Stat(s.Address()); !os.IsNotExist(err) {
		t.Fatal("socket not removed", err)
	}
	s.Stop()
}

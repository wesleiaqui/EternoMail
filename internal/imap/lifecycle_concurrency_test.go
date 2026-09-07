package imap

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestIdleExhaustionClosesDone(t *testing.T) {
	cfg := DefaultIdleConfig()
	cfg.MaxReconnectAttempts = 1
	c := newIdleConnection("a", "a", cfg, func(string) (*ClientConfig, error) { return nil, errors.New("unavailable") })
	c.Start(context.Background(), make(chan MailEvent, 1))
	select {
	case <-c.doneCh:
	case <-time.After(time.Second):
		t.Fatal("IDLE loop leaked")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.running {
		t.Fatal("still running")
	}
}

func TestReconnectBackoff(t *testing.T) {
	for _, tc := range []struct{ in, max, want time.Duration }{{1, 8, 2}, {4, 8, 8}, {8, 8, 8}, {1 << 62, 1<<63 - 1, 1<<63 - 1}} {
		if got := nextReconnectBackoff(tc.in, tc.max); got != tc.want {
			t.Fatalf("got %v want %v", got, tc.want)
		}
	}
}

func TestConnectInvalidSecurity(t *testing.T) {
	if err := NewClient(ClientConfig{Security: "invalid"}).Connect(); err == nil {
		t.Fatal("expected error")
	}
}

func TestIdleStopDoesNotAllowOverlappingRestart(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	cfg := DefaultIdleConfig()
	cfg.ShutdownTimeout = time.Millisecond
	cfg.MaxReconnectAttempts = 1
	c := newIdleConnection("a", "a", cfg, func(string) (*ClientConfig, error) { close(entered); <-release; return nil, errors.New("failed") })
	c.Start(context.Background(), make(chan MailEvent, 1))
	<-entered
	oldDone := c.doneCh
	c.Stop()
	c.Start(context.Background(), make(chan MailEvent, 1))
	if c.doneCh != oldDone {
		t.Fatal("restart replaced channel before old worker ended")
	}
	close(release)
	select {
	case <-oldDone:
	case <-time.After(time.Second):
		t.Fatal("old worker leaked")
	}
}

func TestReleaseDoesNotPublishAvailabilityOutsidePoolLock(t *testing.T) {
	p := NewPool(DefaultPoolConfig(), nil)
	c := &PooledConnection{inUse: true}
	p.mu.Lock()
	done := make(chan struct{})
	go func() { p.Release(c); close(done) }()
	// While ownership transfers are blocked, the connection must stay reserved.
	time.Sleep(20 * time.Millisecond)
	c.mu.Lock()
	reserved := c.inUse
	c.mu.Unlock()
	p.mu.Unlock()
	<-done
	if !reserved {
		t.Fatal("connection was available before pool ownership could transfer")
	}
}

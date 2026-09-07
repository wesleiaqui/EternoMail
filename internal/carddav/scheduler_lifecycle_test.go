package carddav

import (
	"context"
	"testing"
	"time"
)

func TestSchedulerStopWaitsForWorkers(t *testing.T) {
	s := NewScheduler(nil, nil)
	s.Start(context.Background())
	entered := make(chan struct{})
	release := make(chan struct{})
	s.launch(func() { close(entered); <-release })
	<-entered
	stopped := make(chan struct{})
	go func() { s.Stop(); close(stopped) }()
	select {
	case <-stopped:
		t.Fatal("Stop returned with active worker")
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("Stop blocked")
	}
	s.launch(func() { t.Error("started worker after stop") })
	s.Stop()
	s.Start(context.Background())
	s.Stop()
}

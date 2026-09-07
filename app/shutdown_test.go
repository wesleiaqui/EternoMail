package app

import (
	"context"
	"github.com/hkdb/aerion/internal/notification"
	"sync"
	"sync/atomic"
	"testing"
)

type shutdownNotifier struct{ stops atomic.Int32 }

func (n *shutdownNotifier) Start(context.Context) error                    { return nil }
func (n *shutdownNotifier) Stop()                                          { n.stops.Add(1) }
func (n *shutdownNotifier) Show(notification.Notification) (uint32, error) { return 0, nil }
func (n *shutdownNotifier) SetClickHandler(notification.ClickHandler)      {}
func TestConcurrentShutdownStopsResourcesOnce(t *testing.T) {
	n := &shutdownNotifier{}
	a := &App{notifier: n}
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); a.Shutdown(context.Background()) }()
	}
	wg.Wait()
	if n.stops.Load() != 1 {
		t.Fatalf("stopped resources %d times", n.stops.Load())
	}
}

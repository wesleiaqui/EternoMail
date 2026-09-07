package contact

import (
	"context"
	"errors"
	"net/http"
	"testing"
)

func TestMicrosoftExpiredDeltaFallsBackOnlyOnce(t *testing.T) {
	for _, delta := range []string{"", "https://graph.microsoft.com/v1.0/me/contacts/delta?$deltatoken=expired"} {
		calls := 0
		s := NewMicrosoftContactsSyncer()
		s.httpClient = &http.Client{Transport: fnRT(func(*http.Request) (*http.Response, error) {
			calls++
			if calls > 2 {
				t.Fatal("unbounded full-sync recursion")
			}
			return resp(http.StatusGone, "application/json", []byte(`{"error":"expired"}`)), nil
		})}
		if _, err := s.SyncContactsDelta("synthetic-token", delta); err == nil {
			t.Fatal("accepted full-sync error")
		}
		want := 1
		if delta != "" {
			want = 2
		}
		if calls != want {
			t.Fatalf("calls=%d want=%d", calls, want)
		}
	}
}

func TestContactSyncHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, kind := range []string{"google", "microsoft"} {
		client := &http.Client{Transport: fnRT(func(req *http.Request) (*http.Response, error) {
			if req.Context().Err() != context.Canceled {
				t.Fatal("lost caller cancellation")
			}
			return nil, req.Context().Err()
		})}
		var err error
		if kind == "google" {
			s := NewGoogleContactsSyncer()
			s.httpClient = client
			_, err = s.SyncContactsDeltaContext(ctx, "synthetic", "")
		} else {
			s := NewMicrosoftContactsSyncer()
			s.httpClient = client
			_, err = s.SyncContactsDeltaContext(ctx, "synthetic", "")
		}
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("%s: %v", kind, err)
		}
	}
}

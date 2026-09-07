package oauth2

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"
)

func TestManagerConcurrentSessionAccess(t *testing.T) {
	manager := NewManager()
	defer manager.CancelAuthFlow()
	provider := &ProviderConfig{Name: "test", ClientID: "test", AuthURL: "https://example.com/auth", LoopbackHost: "127.0.0.1"}
	var wg sync.WaitGroup
	for worker := 0; worker < 4; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 20; i++ {
				if _, err := manager.StartAuthFlowWithProvider(context.Background(), provider); err != nil {
					t.Error(err)
					return
				}
				manager.HasActiveSession()
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				manager.WaitForCallback(ctx)
				manager.CancelAuthFlow()
			}
		}()
	}
	wg.Wait()
	manager.CancelAuthFlow()
	if manager.HasActiveSession() {
		t.Fatal("session remains after cancellation")
	}
}

func TestManagerOldCallbackPreservesNewSession(t *testing.T) {
	tokenStarted := make(chan struct{})
	releaseToken := make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(releaseToken) }) }
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(tokenStarted)
		<-releaseToken
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"access_token":"test"}`))
	}))
	defer tokenServer.Close()
	defer release()
	manager := NewManager()
	defer manager.CancelAuthFlow()
	provider := &ProviderConfig{Name: "test", ClientID: "test", AuthURL: "https://example.com/auth", TokenURL: tokenServer.URL, LoopbackHost: "127.0.0.1"}
	authURL, err := manager.StartAuthFlowWithProvider(context.Background(), provider)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(authURL)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	finished := make(chan error, 1)
	go func() { _, _, err := manager.WaitForCallback(ctx); finished <- err }()
	response, err := http.Get(parsed.Query().Get("redirect_uri") + "?code=test&state=" + parsed.Query().Get("state"))
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	select {
	case <-tokenStarted:
	case <-ctx.Done():
		t.Fatal("token exchange never started")
	}
	if _, err := manager.StartAuthFlowWithProvider(ctx, provider); err != nil {
		t.Fatal(err)
	}
	release()
	select {
	case err := <-finished:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("callback did not finish")
	}
	if !manager.HasActiveSession() {
		t.Fatal("old callback cleared the new session")
	}
}

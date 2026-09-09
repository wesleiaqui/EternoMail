package auth

import (
	"github.com/hkdb/aerion/internal/credentials"
	gokeyring "github.com/zalando/go-keyring"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type round3Transport func(*http.Request) (*http.Response, error)

func (f round3Transport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestRefreshRetryReplaysBodyAndUsesAlreadyRotatedToken(t *testing.T) {
	gokeyring.MockInit()
	_, store, db := newTestBroker(t)
	insertTestAccount(t, db, "retry")
	tokens := &credentials.OAuthTokens{Provider: "google", AccessToken: "old", RefreshToken: "refresh", ExpiresAt: time.Now().Add(time.Hour)}
	if err := store.SetOAuthTokensForClientConfig("retry", "google-calendar", tokens); err != nil {
		t.Fatal(err)
	}
	calls := 0
	transport := &bearerRefreshTransport{credStore: store, accountID: "retry", clientConfigID: "google-calendar"}
	transport.base = round3Transport(func(req *http.Request) (*http.Response, error) {
		calls++
		b, err := io.ReadAll(req.Body)
		if err != nil || string(b) != "event payload" {
			t.Errorf("request %d lost body: %q %v", calls, b, err)
		}
		req.Body.Close()
		if calls == 1 {
			tokens.AccessToken = "rotated"
			if err := store.SetOAuthTokensForClientConfig("retry", "google-calendar", tokens); err != nil {
				t.Fatal(err)
			}
			return &http.Response{StatusCode: 401, Body: io.NopCloser(strings.NewReader("expired"))}, nil
		}
		if req.Header.Get("Authorization") != "Bearer rotated" {
			t.Error("used stale token")
		}
		return &http.Response{StatusCode: 200, Body: http.NoBody}, nil
	})
	req, err := http.NewRequest("POST", "https://example.test/events", strings.NewReader("event payload"))
	if err != nil {
		t.Fatal(err)
	}
	resp, err := transport.RoundTrip(req)
	if err != nil || resp.StatusCode != 200 || calls != 2 {
		t.Fatal("retry failed", err, calls)
	}
	resp.Body.Close()
}

func TestBearerRefreshTransportRedirectDoesNotLeak(t *testing.T) {
	gokeyring.MockInit()
	_, store, db := newTestBroker(t)
	insertTestAccount(t, db, "redirect")
	if err := store.SetOAuthTokensForClientConfig("redirect", "google-calendar", &credentials.OAuthTokens{
		Provider: "google", AccessToken: "secret-token", RefreshToken: "refresh", ExpiresAt: time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatal(err)
	}

	var leaked string
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		leaked = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer other.Close()
	var insideAuth string
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/start":
			http.Redirect(w, r, "/inside", http.StatusFound)
		case "/inside":
			insideAuth = r.Header.Get("Authorization")
			w.WriteHeader(http.StatusOK)
		case "/outside":
			http.Redirect(w, r, other.URL, http.StatusFound)
		}
	}))
	defer origin.Close()

	client := &http.Client{Transport: &bearerRefreshTransport{
		credStore: store, accountID: "redirect", clientConfigID: "google-calendar",
	}}
	resp, err := client.Get(origin.URL + "/start")
	if err != nil {
		t.Fatalf("same-origin redirect: %v", err)
	}
	resp.Body.Close()
	if insideAuth != "Bearer secret-token" {
		t.Fatalf("same-origin redirect authorization = %q", insideAuth)
	}
	_, err = client.Get(origin.URL + "/outside")
	if err == nil {
		t.Fatal("cross-origin redirect succeeded")
	}
	if leaked != "" {
		t.Fatalf("Bearer authorization leaked to another origin: %q", leaked)
	}
}

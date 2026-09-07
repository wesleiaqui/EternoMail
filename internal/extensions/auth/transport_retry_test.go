package auth

import (
	"github.com/hkdb/aerion/internal/credentials"
	gokeyring "github.com/zalando/go-keyring"
	"io"
	"net/http"
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

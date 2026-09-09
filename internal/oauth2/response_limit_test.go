package oauth2

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

type trackedResponseBody struct {
	io.Reader
	closed bool
}

func (b *trackedResponseBody) Close() error {
	b.closed = true
	return nil
}

type oauthResponseTransport struct {
	status        int
	body          *trackedResponseBody
	contentLength int64
	chunked       bool
}

func (t oauthResponseTransport) RoundTrip(*http.Request) (*http.Response, error) {
	resp := &http.Response{
		StatusCode:    t.status,
		Body:          t.body,
		ContentLength: t.contentLength,
		Header:        make(http.Header),
	}
	if t.chunked {
		resp.TransferEncoding = []string{"chunked"}
	}
	return resp, nil
}

func oauthTokenBodyAtSize(size int64) string {
	prefix := `{"access_token":"a","padding":"`
	suffix := `"}`
	return prefix + strings.Repeat("x", int(size)-len(prefix)-len(suffix)) + suffix
}

func TestRefreshTokenResponseLimitAcceptsExactSize(t *testing.T) {
	body := &trackedResponseBody{Reader: strings.NewReader(oauthTokenBodyAtSize(maxOAuthResponseBytes))}
	m := NewManager()
	m.httpClient = &http.Client{Transport: oauthResponseTransport{status: http.StatusOK, body: body, contentLength: -1, chunked: true}}

	tokens, err := m.RefreshTokenWithProvider(ProviderConfig{TokenURL: "https://example.test/token"}, "refresh")
	if err != nil {
		t.Fatalf("RefreshTokenWithProvider: %v", err)
	}
	if tokens.AccessToken != "a" || !body.closed {
		t.Fatalf("tokens=%+v closed=%v", tokens, body.closed)
	}
}

func TestRefreshTokenResponseLimitRejectsOversizeDespiteFalseContentLength(t *testing.T) {
	body := &trackedResponseBody{Reader: strings.NewReader(oauthTokenBodyAtSize(maxOAuthResponseBytes + 1))}
	m := NewManager()
	m.httpClient = &http.Client{Transport: oauthResponseTransport{status: http.StatusOK, body: body, contentLength: 1}}

	_, err := m.RefreshTokenWithProvider(ProviderConfig{TokenURL: "https://example.test/token"}, "refresh")
	if !errors.Is(err, errOAuthResponseTooLarge) {
		t.Fatalf("error = %v, want oversized OAuth response", err)
	}
	if !body.closed {
		t.Fatal("response body was not closed")
	}
}

func TestOAuthErrorResponseLimitRejectsOversize(t *testing.T) {
	body := &trackedResponseBody{Reader: strings.NewReader(strings.Repeat("x", int(maxOAuthResponseBytes+1)))}
	m := NewManager()
	m.httpClient = &http.Client{Transport: oauthResponseTransport{status: http.StatusBadRequest, body: body, contentLength: -1, chunked: true}}

	_, err := m.exchangeCode(ProviderConfig{TokenURL: "https://example.test/token"}, "code", "verifier", "http://127.0.0.1/callback")
	if !errors.Is(err, errOAuthResponseTooLarge) {
		t.Fatalf("error = %v, want oversized OAuth response", err)
	}
	if !body.closed {
		t.Fatal("response body was not closed")
	}
}

func TestUserinfoResponseLimitRejectsOversize(t *testing.T) {
	body := &trackedResponseBody{Reader: strings.NewReader(strings.Repeat("x", int(maxOAuthResponseBytes+1)))}
	m := NewManager()
	m.httpClient = &http.Client{Transport: oauthResponseTransport{status: http.StatusOK, body: body, contentLength: -1, chunked: true}}

	_, err := m.getUserEmail(ProviderConfig{UserinfoEndpoint: "https://example.test/userinfo"}, &TokenResponse{AccessToken: "access"})
	if !errors.Is(err, errOAuthResponseTooLarge) {
		t.Fatalf("error = %v, want oversized OAuth response", err)
	}
	if !body.closed {
		t.Fatal("response body was not closed")
	}
}

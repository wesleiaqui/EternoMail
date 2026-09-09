package oauth2

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
)

func discoveryBodyAtSize(size int64) string {
	prefix := `{"authorization_endpoint":"https://issuer.test/authorize","token_endpoint":"https://issuer.test/token","padding":"`
	suffix := `"}`
	return prefix + strings.Repeat("x", int(size)-len(prefix)-len(suffix)) + suffix
}

func TestFetchDiscoveryResponseLimit(t *testing.T) {
	valid := `{"authorization_endpoint":"https://issuer.test/authorize","token_endpoint":"https://issuer.test/token"}`
	validAtLimit := discoveryBodyAtSize(maxOAuthResponseBytes)

	tests := []struct {
		name          string
		body          string
		contentLength int64
		chunked       bool
		wantTooLarge  bool
	}{
		{name: "valid below limit", body: valid, contentLength: int64(len(valid))},
		{name: "valid exactly at limit", body: validAtLimit, contentLength: int64(len(validAtLimit))},
		{name: "limit plus one with false content length", body: discoveryBodyAtSize(maxOAuthResponseBytes + 1), contentLength: 1, wantTooLarge: true},
		{name: "limit plus one without content length", body: discoveryBodyAtSize(maxOAuthResponseBytes + 1), contentLength: -1, wantTooLarge: true},
		{name: "chunked limit plus one", body: discoveryBodyAtSize(maxOAuthResponseBytes + 1), contentLength: -1, chunked: true, wantTooLarge: true},
		{name: "valid prefix followed by extra byte", body: validAtLimit + "x", contentLength: -1, chunked: true, wantTooLarge: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			body := &trackedResponseBody{Reader: strings.NewReader(tc.body)}
			client := &http.Client{Transport: oauthResponseTransport{
				status:        http.StatusOK,
				body:          body,
				contentLength: tc.contentLength,
				chunked:       tc.chunked,
			}}

			doc, err := fetchDiscovery(context.Background(), client, "https://issuer.test/.well-known/openid-configuration")
			if tc.wantTooLarge {
				if !errors.Is(err, errOAuthResponseTooLarge) {
					t.Fatalf("error = %v, want oversized OAuth response", err)
				}
				if doc != (OIDCDiscovery{}) {
					t.Fatalf("truncated discovery was accepted: %+v", doc)
				}
			} else if err != nil {
				t.Fatalf("fetchDiscovery: %v", err)
			} else if doc.AuthorizationEndpoint == "" || doc.TokenEndpoint == "" {
				t.Fatalf("invalid discovery document: %+v", doc)
			}
			if !body.closed {
				t.Fatal("response body was not closed")
			}
		})
	}
}

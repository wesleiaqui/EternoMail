package oauth2

import "testing"

func TestTokenResponseClear(t *testing.T) {
	tokens := &TokenResponse{AccessToken: "access", RefreshToken: "refresh", IDToken: "identity"}
	tokens.Clear()
	if *tokens != (TokenResponse{}) {
		t.Fatal("pending tokens retained")
	}
	var absent *TokenResponse
	absent.Clear()
}

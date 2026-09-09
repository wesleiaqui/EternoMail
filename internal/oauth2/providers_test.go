package oauth2

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestGoogleProvider(t *testing.T) {
	p := GoogleProvider()

	if p.Name != "google" {
		t.Errorf("Name = %q, want %q", p.Name, "google")
	}
	if !strings.Contains(p.AuthURL, "google") {
		t.Errorf("AuthURL = %q, expected it to contain 'google'", p.AuthURL)
	}
	if len(p.Scopes) == 0 {
		t.Error("Scopes is empty, want at least one scope")
	}
}

func TestOfficialProvidersArePublicClients(t *testing.T) {
	origGoogle, origGoogleSecret, origMicrosoft := GoogleClientID, GoogleClientSecret, MicrosoftClientID
	t.Cleanup(func() {
		GoogleClientID, GoogleClientSecret, MicrosoftClientID = origGoogle, origGoogleSecret, origMicrosoft
	})

	GoogleClientID = "google-public-id"
	GoogleClientSecret = "google-desktop-configuration"
	MicrosoftClientID = "microsoft-public-id"

	google := GoogleProvider()
	if google.ClientSecret == "" || !IsGoogleConfigured() {
		t.Fatal("Google Desktop client must be configured with ID and configuration value")
	}
	microsoft := MicrosoftProvider()
	if microsoft.ClientSecret != "" || !IsMicrosoftConfigured() {
		t.Fatal("Microsoft public client must be configured with an ID and no secret")
	}
	if google.LoopbackHost != "127.0.0.1" || microsoft.LoopbackHost != "localhost" {
		t.Fatalf("unexpected loopback hosts: Google=%q Microsoft=%q", google.LoopbackHost, microsoft.LoopbackHost)
	}
}

func TestPublicClientDefaultsAreConfigured(t *testing.T) {
	if DefaultGoogleClientID == "" {
		t.Fatal("Google client ID default must be configured")
	}
	if DefaultGoogleClientSecret == "" {
		t.Fatal("Google Desktop client configuration default must be configured")
	}
	if DefaultMicrosoftClientID == "" {
		t.Fatal("Microsoft client ID default must be configured")
	}
}

func TestPublicClientDefaultsApplyWithoutEnvironmentOverrides(t *testing.T) {
	origGoogleID, origGoogleSecret, origMicrosoftID := GoogleClientID, GoogleClientSecret, MicrosoftClientID
	t.Cleanup(func() {
		GoogleClientID, GoogleClientSecret, MicrosoftClientID = origGoogleID, origGoogleSecret, origMicrosoftID
	})
	t.Setenv("GOOGLE_CLIENT_ID", "")
	t.Setenv("GOOGLE_CLIENT_SECRET", "")
	t.Setenv("MICROSOFT_CLIENT_ID", "")

	loadPublicClientDefaults()

	if GoogleClientID != DefaultGoogleClientID || GoogleClientSecret != DefaultGoogleClientSecret || MicrosoftClientID != DefaultMicrosoftClientID {
		t.Fatal("public OAuth defaults were not applied")
	}
}

func TestPublicClientEnvironmentOverrides(t *testing.T) {
	origGoogleID, origGoogleSecret, origMicrosoftID := GoogleClientID, GoogleClientSecret, MicrosoftClientID
	t.Cleanup(func() {
		GoogleClientID, GoogleClientSecret, MicrosoftClientID = origGoogleID, origGoogleSecret, origMicrosoftID
	})
	t.Setenv("GOOGLE_CLIENT_ID", "development-google-id")
	t.Setenv("GOOGLE_CLIENT_SECRET", "development-google-desktop-configuration")
	t.Setenv("MICROSOFT_CLIENT_ID", "development-microsoft-id")

	loadPublicClientDefaults()

	if GoogleClientID != "development-google-id" || GoogleClientSecret != "development-google-desktop-configuration" || MicrosoftClientID != "development-microsoft-id" {
		t.Fatal("development environment did not override public OAuth client configuration")
	}
}

func TestGoogleWithoutSecretIsUnavailable(t *testing.T) {
	origGoogle, origSecret, origOverride := GoogleClientID, GoogleClientSecret, UserOverrideLookup
	t.Cleanup(func() { GoogleClientID, GoogleClientSecret, UserOverrideLookup = origGoogle, origSecret, origOverride })
	GoogleClientID = "google-public-id"
	GoogleClientSecret = ""
	UserOverrideLookup = nil
	if IsGoogleConfigured() {
		t.Fatal("Google must be unavailable without required Desktop configuration")
	}
}

func TestGoogleWithoutClientIDIsUnavailable(t *testing.T) {
	origGoogle, origOverride := GoogleClientID, UserOverrideLookup
	t.Cleanup(func() { GoogleClientID, UserOverrideLookup = origGoogle, origOverride })
	GoogleClientID = ""
	UserOverrideLookup = nil
	if IsGoogleConfigured() {
		t.Fatal("Google must be unavailable without a client ID")
	}
}

func TestAuthorizationAndExchangeUseSameRedirectURI(t *testing.T) {
	provider := GoogleProvider()
	redirectURI := loopbackRedirectURI(provider.LoopbackHost, 4242)
	authURL := buildAuthURL(provider, "state", "challenge", redirectURI)
	parsed, err := url.Parse(authURL)
	if err != nil {
		t.Fatal(err)
	}
	if got := parsed.Query().Get("redirect_uri"); got != redirectURI {
		t.Fatalf("authorization redirect URI = %q, want %q", got, redirectURI)
	}
}

func TestPublicClientExchangeOmitsSecretAndKeepsRedirectURI(t *testing.T) {
	redirectURI := loopbackRedirectURI("127.0.0.1", 4242)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if got := r.Form.Get("client_secret"); got != "" {
			http.Error(w, "public client sent client_secret", http.StatusBadRequest)
			return
		}
		if got := r.Form.Get("redirect_uri"); got != redirectURI {
			http.Error(w, "redirect URI mismatch", http.StatusBadRequest)
			return
		}
		_, _ = io.WriteString(w, `{"access_token":"access","refresh_token":"refresh","expires_in":3600}`)
	}))
	defer server.Close()

	provider := ProviderConfig{ClientID: "public-client", TokenURL: server.URL}
	tokens, err := NewManager().exchangeCode(provider, "code", "verifier", redirectURI)
	if err != nil {
		t.Fatal(err)
	}
	if tokens.AccessToken == "" || tokens.RefreshToken == "" {
		t.Fatal("expected token response")
	}
}

func TestGoogleExchangeAndRefreshIncludeConfiguredSecret(t *testing.T) {
	redirectURI := loopbackRedirectURI("127.0.0.1", 4242)
	requests := make(chan url.Values, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		requests <- r.Form
		_, _ = io.WriteString(w, `{"access_token":"access","refresh_token":"refresh","expires_in":3600}`)
	}))
	defer server.Close()
	provider := ProviderConfig{ClientID: "google-id", ClientSecret: "google-desktop-secret", TokenURL: server.URL}
	manager := NewManager()
	if _, err := manager.exchangeCode(provider, "code", "verifier", redirectURI); err != nil {
		t.Fatal(err)
	}
	if form := <-requests; form.Get("client_secret") == "" || form.Get("code_verifier") != "verifier" {
		t.Fatal("Google token exchange omitted required client_secret or PKCE verifier")
	}
	if _, err := manager.RefreshTokenWithProvider(provider, "refresh"); err != nil {
		t.Fatal(err)
	}
	if form := <-requests; form.Get("client_secret") == "" || form.Get("refresh_token") != "refresh" {
		t.Fatal("Google refresh omitted required client_secret")
	}
}

func TestPublicGoogleFlowUsesPKCEStateAndIPv4Redirect(t *testing.T) {
	origGoogle, origSecret, origOverride := GoogleClientID, GoogleClientSecret, UserOverrideLookup
	t.Cleanup(func() { GoogleClientID, GoogleClientSecret, UserOverrideLookup = origGoogle, origSecret, origOverride })
	GoogleClientID = "google-public-id"
	GoogleClientSecret = "google-desktop-configuration"
	UserOverrideLookup = nil

	manager := NewManager()
	defer manager.CancelAuthFlow()
	authURL, err := manager.StartAuthFlow(context.Background(), "google")
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(authURL)
	if err != nil {
		t.Fatal(err)
	}
	query := parsed.Query()
	if query.Get("code_challenge_method") != "S256" || query.Get("code_challenge") == "" {
		t.Fatal("Google authorization flow must use PKCE S256")
	}
	if query.Get("state") == "" {
		t.Fatal("Google authorization flow must include state")
	}
	if got := query.Get("redirect_uri"); !strings.HasPrefix(got, "http://127.0.0.1:") {
		t.Fatalf("Google redirect URI = %q, want IPv4 loopback", got)
	}
}

func TestMicrosoftProvider(t *testing.T) {
	p := MicrosoftProvider()

	if p.Name != "microsoft" {
		t.Errorf("Name = %q, want %q", p.Name, "microsoft")
	}
	if !strings.Contains(p.AuthURL, "microsoftonline") {
		t.Errorf("AuthURL = %q, expected it to contain 'microsoftonline'", p.AuthURL)
	}
}

func TestGoogleContactsOnlyProvider(t *testing.T) {
	p := GoogleContactsOnlyProvider()

	if p.Name != "google-contacts" {
		t.Errorf("Name = %q, want %q", p.Name, "google-contacts")
	}
}

func TestGetProvider(t *testing.T) {
	tests := []struct {
		name     string
		provider string
		wantErr  bool
	}{
		{name: "google", provider: "google", wantErr: false},
		{name: "microsoft", provider: "microsoft", wantErr: false},
		{name: "unknown", provider: "unknown", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := GetProvider(tt.provider)
			if tt.wantErr && err == nil {
				t.Errorf("GetProvider(%q) = nil error, want error", tt.provider)
				return
			}
			if !tt.wantErr && err != nil {
				t.Errorf("GetProvider(%q) returned error: %v", tt.provider, err)
			}
		})
	}
}

func TestSupportedProviders(t *testing.T) {
	providers := SupportedProviders()

	if len(providers) != 2 {
		t.Fatalf("SupportedProviders() returned %d providers, want 2", len(providers))
	}

	want := map[string]bool{"google": true, "microsoft": true}
	for _, p := range providers {
		if !want[p] {
			t.Errorf("unexpected provider %q in SupportedProviders()", p)
		}
	}
}

// The exact sets also prohibit Contacts/Other Contacts and Gmail REST scopes
// on Mail, and prohibit Mail/profile/Other Contacts on initial Contacts login.
func TestGoogleAuthorizationLeastPrivilege(t *testing.T) {
	mail := []string{"https://mail.google.com/", "https://www.googleapis.com/auth/userinfo.email", "https://www.googleapis.com/auth/userinfo.profile", "openid"}
	contacts := []string{"https://www.googleapis.com/auth/contacts.readonly", "https://www.googleapis.com/auth/userinfo.email", "openid"}
	for _, tc := range []struct {
		name     string
		provider ProviderConfig
		want     []string
	}{
		{"mail", GoogleProvider(), mail},
		{"contacts", GoogleContactsOnlyProvider(), contacts},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if strings.Join(tc.provider.Scopes, " ") != strings.Join(tc.want, " ") {
				t.Fatalf("unexpected scopes: %v", tc.provider.Scopes)
			}
			cfg, err := GetProvider(tc.provider.Name) // also used by Mail reauth
			if err != nil {
				t.Fatal(err)
			}
			u, err := url.Parse(buildAuthURL(cfg, "state", "pkce", "http://127.0.0.1/callback"))
			if err != nil {
				t.Fatal(err)
			}
			if u.Query().Get("scope") != strings.Join(tc.want, " ") {
				t.Fatalf("auth URL changed scopes: %s", u.Query().Get("scope"))
			}
			if u.Query().Get("include_granted_scopes") != "false" {
				t.Fatal("Google must not combine prior Mail/Contacts grants")
			}
			if u.Query().Get("access_type") != "offline" || u.Query().Get("prompt") != "consent" {
				t.Fatal("refresh-token authorization settings lost")
			}
		})
	}
}

func TestContactsClientResolverKeepsContactsScopes(t *testing.T) {
	previous := UserOverrideLookup
	UserOverrideLookup = func(id string) (ClientCredentials, bool) {
		return ClientCredentials{ClientID: id + "-test", ClientSecret: "test"}, true
	}
	t.Cleanup(func() { UserOverrideLookup = previous })
	for _, id := range []string{"google-mail", "google-contacts", "microsoft-contacts", "google-calendar"} {
		cfg, err := GetProviderForClientConfig(id)
		if err != nil {
			t.Fatal(err)
		}
		if cfg.ClientID != id+"-test" {
			t.Fatal("client override not applied")
		}
		if id == "google-contacts" {
			if strings.Join(cfg.Scopes, " ") != strings.Join(GoogleContactsOnlyProvider().Scopes, " ") {
				t.Fatalf("Contacts resolver returned Mail scopes: %v", cfg.Scopes)
			}
			cfg.LoginHint = "person@example.com"
			u, _ := url.Parse(buildAuthURL(cfg, "state", "pkce", "http://localhost"))
			if u.Query().Get("login_hint") != cfg.LoginHint {
				t.Fatal("missing login hint")
			}
		}
		if id == "microsoft-contacts" && cfg.Scopes[0] != "https://graph.microsoft.com/Contacts.Read" {
			t.Fatal("Microsoft Contacts resolved to Outlook audience")
		}
	}
}

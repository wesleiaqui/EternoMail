package app

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hkdb/aerion/internal/account"
	"github.com/hkdb/aerion/internal/carddav"
	"github.com/hkdb/aerion/internal/contact"
	"github.com/hkdb/aerion/internal/credentials"
	"github.com/hkdb/aerion/internal/database"
	"github.com/hkdb/aerion/internal/oauth2"
	gokeyring "github.com/zalando/go-keyring"
)

func contactOAuthFixture(t *testing.T) (*App, *database.DB) {
	t.Helper()
	gokeyring.MockInit()
	dir := t.TempDir()
	db, err := database.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.Migrate(); err != nil {
		t.Fatal(err)
	}
	creds, err := credentials.NewStore(db.DB, dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("INSERT INTO accounts (id,name,email,imap_host,smtp_host,username,auth_type) VALUES ('mail','Mail','user@example.com','imap.gmail.com','smtp.gmail.com','user@example.com','oauth2')"); err != nil {
		t.Fatal(err)
	}
	a := &App{ctx: context.Background(), accountStore: account.NewStore(db), carddavStore: carddav.NewStore(db.DB), credStore: creds, contactStore: contact.NewStore(db.DB), oauth2Manager: oauth2.NewManager()}
	if err := creds.SetOAuthTokens("mail", &credentials.OAuthTokens{Provider: "google", AccessToken: "mail-access", RefreshToken: "mail-refresh", ExpiresAt: time.Now().Add(time.Hour), Scopes: oauth2.GoogleProvider().Scopes}); err != nil {
		t.Fatal(err)
	}
	return a, db
}

func contactSession() *pendingContactOAuth {
	return &pendingContactOAuth{
		provider: oauth2.GoogleContactsOnlyProvider(),
		tokens:   &oauth2.TokenResponse{AccessToken: "contacts-access", RefreshToken: "contacts-refresh", ExpiresIn: 3600},
		email:    "user@example.com",
	}
}

func assertMailUnchanged(t *testing.T, a *App) {
	t.Helper()
	tokens, err := a.credStore.GetOAuthTokens("mail")
	if err != nil || tokens.AccessToken != "mail-access" || tokens.RefreshToken != "mail-refresh" {
		t.Fatalf("Gmail credentials changed: %v", err)
	}
	if strings.Join(tokens.Scopes, " ") != strings.Join(oauth2.GoogleProvider().Scopes, " ") {
		t.Fatal("Contacts scopes contaminated Mail")
	}
}

func TestContactsSetupAndRemovalIsolatedFromGmail(t *testing.T) {
	a, db := contactOAuthFixture(t)
	accounts, err := a.GetLinkedAccountsForContactSync()
	if err != nil || len(accounts) != 1 || accounts[0].IsLinked {
		t.Fatalf("Mail without Contacts scope must remain eligible: %+v %v", accounts, err)
	}
	sources, _ := a.carddavStore.ListSources()
	if len(sources) != 0 {
		t.Fatal("Gmail silently created a Contacts source")
	}

	a.pendingOAuthTokens = &oauth2.TokenResponse{AccessToken: "pending-mail"}
	a.pendingContactOAuth = contactSession()
	source, err := a.CompleteContactSourceOAuthSetup("Contacts", 60)
	if err != nil {
		t.Fatal(err)
	}
	if source.Username != "user@example.com" {
		t.Fatal("verified source identity not persisted")
	}
	if a.pendingOAuthTokens.AccessToken != "pending-mail" {
		t.Fatal("consumed Mail callback")
	}
	tokens, err := a.credStore.GetContactSourceOAuthTokens(source.ID)
	if err != nil || tokens.Provider != "google-contacts" || tokens.AccessToken != "contacts-access" {
		t.Fatal("missing separate Contacts tokens", err)
	}
	var slot string
	if err := db.QueryRow("SELECT client_config_id FROM contact_source_oauth WHERE source_id=?", source.ID).Scan(&slot); err != nil || slot != "google-contacts" {
		t.Fatal("wrong source slot", slot, err)
	}
	assertMailUnchanged(t, a)
	accounts, _ = a.GetLinkedAccountsForContactSync()
	if !accounts[0].IsLinked || accounts[0].ContactSourceID != source.ID {
		t.Fatal("logical email association missing")
	}
	if err := a.DeleteContactSource(source.ID); err != nil {
		t.Fatal(err)
	}
	if a.credStore.HasContactSourceOAuthTokens(source.ID) {
		t.Fatal("source credentials survived removal")
	}
	assertMailUnchanged(t, a)
}

func TestLegacyGoogleSourceMigratesInPlaceAndSurvivesMailRemoval(t *testing.T) {
	a, _ := contactOAuthFixture(t)
	source, err := a.carddavStore.CreateSource(&carddav.SourceConfig{Name: "Old Contacts", Type: carddav.SourceTypeGoogle, AccountID: "mail", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	ab, err := a.carddavStore.CreateAddressbook(source.ID, "/", "Contacts", true)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.carddavStore.UpsertContactsBatch([]*carddav.Contact{{ID: "cached", AddressbookID: ab.ID, Email: "friend@example.com", DisplayName: "Friend", Href: "people/1"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.getValidContactSourceOAuthToken(source.ID); err == nil {
		t.Fatal("legacy source reused combined Gmail token")
	}
	got, err := a.saveContactAuthorization(contactSession(), source.ID, "mail", source.Name, 60)
	if err != nil || got.ID != source.ID {
		t.Fatal("legacy source not migrated in place", err)
	}
	found, err := a.SearchContacts("friend", 10)
	if err != nil || len(found) != 1 {
		t.Fatal("cached autocomplete lost", err)
	}
	assertMailUnchanged(t, a)
	if err := a.credStore.DeleteAllCredentials("mail"); err != nil {
		t.Fatal(err)
	}
	if err := a.accountStore.Delete("mail"); err != nil {
		t.Fatal(err)
	}
	got, err = a.carddavStore.GetSource(source.ID)
	if err != nil || got == nil || got.AccountID != nil || got.Username != "user@example.com" {
		t.Fatal("source corrupted on Mail removal", err)
	}
	if token, err := a.getValidContactSourceOAuthToken(source.ID); err != nil || token != "contacts-access" {
		t.Fatal("source token lost with Mail", err)
	}
	found, _ = a.SearchContacts("friend", 10)
	if len(found) != 1 {
		t.Fatal("cached contacts cascaded with Mail")
	}
}

func TestGoogleContactsSourceCannotEnableWriteAccess(t *testing.T) {
	a, _ := contactOAuthFixture(t)
	source, err := a.carddavStore.CreateSource(&carddav.SourceConfig{
		Name:    "Google Contacts",
		Type:    carddav.SourceTypeGoogle,
		Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.SetContactSourceWritable(source.ID, true); err == nil {
		t.Fatal("Google Contacts source enabled write access")
	}
	source, err = a.carddavStore.GetSource(source.ID)
	if err != nil {
		t.Fatal(err)
	}
	if source.Writable {
		t.Fatal("Google Contacts source was persisted as writable")
	}
}

func TestContactGrantIdentityAndPartialConsent(t *testing.T) {
	p := oauth2.GoogleContactsOnlyProvider()
	for _, tc := range []struct {
		email, expected, scopes string
		valid                   bool
	}{
		{"user@example.com", "USER@example.com", strings.Join(p.Scopes, " "), true},
		{"other@example.com", "user@example.com", "", false},
		{"", "user@example.com", "", false},
		{"user@example.com", "", "openid", false},
		{"user@example.com", "", "", true},
	} {
		err := validateContactGrant(p, &oauth2.TokenResponse{AccessToken: "token", Scope: tc.scopes}, tc.email, tc.expected)
		if (err == nil) != tc.valid {
			t.Fatalf("unexpected grant validation: %v", err)
		}
	}
}

type contactsRoundTrip func(*http.Request) (*http.Response, error)

func (f contactsRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestContactRefreshUsesContactsClientAndPreservesRotatedToken(t *testing.T) {
	a, _ := contactOAuthFixture(t)
	source, err := a.saveContactAuthorization(contactSession(), "", "mail", "Contacts", 60)
	if err != nil {
		t.Fatal(err)
	}
	tokens, _ := a.credStore.GetContactSourceOAuthTokens(source.ID)
	tokens.ExpiresAt = time.Now().Add(-time.Hour)
	if err := a.credStore.SetContactSourceOAuthTokens(source.ID, tokens); err != nil {
		t.Fatal(err)
	}
	oldLookup := oauth2.UserOverrideLookup
	oauth2.UserOverrideLookup = func(id string) (oauth2.ClientCredentials, bool) {
		return oauth2.ClientCredentials{ClientID: id + "-client", ClientSecret: "test"}, true
	}
	t.Cleanup(func() { oauth2.UserOverrideLookup = oldLookup })
	oldTransport := http.DefaultTransport
	http.DefaultTransport = contactsRoundTrip(func(req *http.Request) (*http.Response, error) {
		if err := req.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if req.Form.Get("client_id") != "google-contacts-client" || req.Form.Get("refresh_token") != "contacts-refresh" {
			t.Fatal("Contacts refreshed with wrong client/token")
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"access_token":"refreshed","refresh_token":"rotated","expires_in":3600}`))}, nil
	})
	t.Cleanup(func() { http.DefaultTransport = oldTransport })
	if token, err := a.getValidContactSourceOAuthToken(source.ID); err != nil || token != "refreshed" {
		t.Fatal("refresh failed", err)
	}
	tokens, _ = a.credStore.GetContactSourceOAuthTokens(source.ID)
	if tokens.RefreshToken != "rotated" {
		t.Fatal("refresh rotation lost")
	}
	assertMailUnchanged(t, a)
}

func TestAutocompleteNeedsNoOAuthDependencies(t *testing.T) {
	a, _ := contactOAuthFixture(t)
	if err := a.contactStore.AddOrUpdate("local@example.com", "Local"); err != nil {
		t.Fatal(err)
	}
	a.accountStore, a.credStore, a.oauth2Manager = nil, nil, nil
	got, err := a.SearchContacts("local", 0)
	if err != nil || len(got) != 1 || got[0].Email != "local@example.com" {
		t.Fatal("local autocomplete failed", err)
	}
}

func TestMailPendingCannotCompleteContacts(t *testing.T) {
	a, _ := contactOAuthFixture(t)
	a.pendingOAuthTokens = &oauth2.TokenResponse{AccessToken: "pending-mail", RefreshToken: "mail-refresh"}
	if _, err := a.CompleteContactSourceOAuthSetup("Contacts", 60); err == nil {
		t.Fatal("Mail callback accepted for Contacts")
	}
	sources, _ := a.carddavStore.ListSources()
	if len(sources) != 0 {
		t.Fatal("Contacts created without consent")
	}
	assertMailUnchanged(t, a)
}

func TestLegacyCombinedMailTokenStillWorksForMail(t *testing.T) {
	a, _ := contactOAuthFixture(t)
	tokens, _ := a.credStore.GetOAuthTokens("mail")
	tokens.Scopes = append(tokens.Scopes, "https://www.googleapis.com/auth/contacts.readonly")
	if err := a.credStore.SetOAuthTokens("mail", tokens); err != nil {
		t.Fatal(err)
	}
	ops := &composeOps{credStore: a.credStore}
	got, err := ops.getValidOAuthToken(context.Background(), "mail")
	if err != nil || got.AccessToken != "mail-access" {
		t.Fatal("legacy Mail grant stopped working", err)
	}
	if a.credStore.HasContactSourceOAuthTokens("mail") {
		t.Fatal("legacy Mail grant was copied to Contacts")
	}
}

func TestDeleteLinkedSourceDoesNotDeleteGmailCredentials(t *testing.T) {
	a, _ := contactOAuthFixture(t)
	source, err := a.saveContactAuthorization(contactSession(), "", "mail", "Contacts", 60)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.DeleteContactSource(source.ID); err != nil {
		t.Fatal(err)
	}
	if a.credStore.HasContactSourceOAuthTokens(source.ID) {
		t.Fatal("linked source tokens not removed")
	}
	assertMailUnchanged(t, a)
}

func TestStartingAndCancellingContactsDoesNotCancelMailOAuth(t *testing.T) {
	a, _ := contactOAuthFixture(t)
	mailConfig := oauth2.GoogleProvider()
	mailConfig.ClientID = "test-mail-client"
	mailURL, err := a.oauth2Manager.StartAuthFlowWithProvider(a.ctx, &mailConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer a.oauth2Manager.CancelAuthFlow()
	session, contactURL, err := a.prepareContactOAuth("google", "user@example.com")
	if err != nil {
		t.Fatal(err)
	}
	defer a.clearContactOAuth()
	if session.manager == a.oauth2Manager {
		t.Fatal("Mail and Contacts shared the flow manager")
	}
	u, _ := url.Parse(contactURL)
	if u.Query().Get("scope") != strings.Join(oauth2.GoogleContactsOnlyProvider().Scopes, " ") {
		t.Fatal("Contacts flow used Mail scopes")
	}
	if u.Query().Get("login_hint") != "user@example.com" {
		t.Fatal("missing identity hint")
	}
	a.clearContactOAuth()
	// Mail's loopback callback must still be live after Contacts cancellation.
	mail, _ := url.Parse(mailURL)
	callback, _ := url.Parse(mail.Query().Get("redirect_uri"))
	q := callback.Query()
	q.Set("state", mail.Query().Get("state"))
	q.Set("error", "access_denied")
	callback.RawQuery = q.Encode()
	resp, err := http.Get(callback.String())
	if err != nil {
		t.Fatal("Contacts cancelled Mail's callback server", err)
	}
	resp.Body.Close()
	_, _, err = a.oauth2Manager.WaitForCallback(a.ctx)
	if err == nil || !strings.Contains(err.Error(), "access_denied") {
		t.Fatal("Mail callback was lost", err)
	}
}

func TestMicrosoftContactsAcceptsShortScopeAndKeepsStandaloneGraphTokens(t *testing.T) {
	a, _ := contactOAuthFixture(t)
	session := contactSession()
	session.provider = oauth2.MicrosoftContactsOnlyProvider()
	session.tokens.Scope = "Contacts.Read openid email offline_access"
	if err := validateContactGrant(session.provider, session.tokens, session.email, session.email); err != nil {
		t.Fatal("Microsoft short Graph scope rejected", err)
	}
	source, err := a.saveContactAuthorization(session, "", "mail", "Microsoft Contacts", 60)
	if err != nil {
		t.Fatal(err)
	}
	if source.AccountID != nil {
		t.Fatal("Microsoft Contacts must not route to Outlook Mail token")
	}
	got, err := a.credStore.GetContactSourceOAuthTokens(source.ID)
	if err != nil || got.Provider != "microsoft-contacts" {
		t.Fatal("wrong Microsoft Contacts credential storage", err)
	}
	assertMailUnchanged(t, a)
}

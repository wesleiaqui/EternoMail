package app

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/hkdb/aerion/internal/account"
	"github.com/hkdb/aerion/internal/carddav"
	"github.com/hkdb/aerion/internal/credentials"
	"github.com/hkdb/aerion/internal/oauth2"
	"github.com/hkdb/aerion/internal/platform"
	wailsRuntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// A Contacts session cannot consume or overwrite pending Mail authorization.
type pendingContactOAuth struct {
	manager  *oauth2.Manager
	provider oauth2.ProviderConfig
	tokens   *oauth2.TokenResponse
	email    string
}

// Legacy standalone sources used shipped credentials and stored "google" /
// "microsoft". Preserve that client for refresh, but use Contacts configuration.
// Newly authorized sources store the explicit *-contacts provider.
func contactRefreshProvider(name string) (oauth2.ProviderConfig, error) {
	switch name {
	case "google":
		return oauth2.GoogleContactsOnlyProvider(), nil
	case "microsoft":
		return oauth2.MicrosoftContactsOnlyProvider(), nil
	case "google-contacts", "microsoft-contacts":
		return oauth2.GetProvider(name)
	default:
		return oauth2.ProviderConfig{}, fmt.Errorf("unsupported contact OAuth provider: %s", name)
	}
}

func (a *App) prepareContactOAuth(provider, email string) (*pendingContactOAuth, string, error) {
	if provider != "google" && provider != "microsoft" {
		return nil, "", fmt.Errorf("unsupported provider for contacts: %s", provider)
	}
	cfg, err := oauth2.GetProvider(provider + "-contacts")
	if err != nil {
		return nil, "", err
	}
	cfg.LoginHint = email
	a.contactOAuthMu.Lock()
	defer a.contactOAuthMu.Unlock()
	if a.contactOAuthManager != nil {
		return nil, "", fmt.Errorf("a Contacts sign-in is already in progress")
	}
	if a.pendingContactOAuth != nil {
		a.pendingContactOAuth.tokens.Clear()
		a.pendingContactOAuth = nil
	}
	session := &pendingContactOAuth{manager: oauth2.NewManager(), provider: cfg}
	authURL, err := session.manager.StartAuthFlowWithProvider(a.ctx, &cfg)
	if err != nil {
		return nil, "", err
	}
	a.contactOAuthManager = session.manager
	return session, authURL, nil
}

func (a *App) beginContactOAuth(provider, email string) (*pendingContactOAuth, error) {
	session, authURL, err := a.prepareContactOAuth(provider, email)
	if err != nil {
		return nil, err
	}
	wailsRuntime.EventsEmit(a.ctx, "contact-source-oauth:started", map[string]interface{}{
		"provider": provider, "authURL": authURL,
	})
	if err := platform.PortalOpenURI(authURL); err != nil {
		wailsRuntime.BrowserOpenURL(a.ctx, authURL)
	}
	return session, nil
}

func (a *App) waitContactOAuth(session *pendingContactOAuth, expectedEmail string) error {
	tokens, email, err := session.manager.WaitForCallback(a.ctx)
	if err != nil {
		return err
	}
	if err := validateContactGrant(session.provider, tokens, email, expectedEmail); err != nil {
		tokens.Clear()
		return err
	}
	session.tokens, session.email = tokens, email
	return nil
}

func validateContactGrant(provider oauth2.ProviderConfig, tokens *oauth2.TokenResponse, email, expectedEmail string) error {
	if tokens == nil || tokens.AccessToken == "" {
		return fmt.Errorf("Contacts authorization returned no access token")
	}
	if strings.TrimSpace(email) == "" {
		return fmt.Errorf("Contacts authorization did not return an email address; sign in again")
	}
	if expectedEmail != "" && !strings.EqualFold(email, expectedEmail) {
		return fmt.Errorf("Contacts account %q does not match selected account %q", email, expectedEmail)
	}
	// OAuth permits omission of scope when it equals the requested set.
	// A partial grant must not be recorded as if Contacts access were granted.
	if tokens.Scope != "" {
		granted := strings.Fields(tokens.Scope)
		covered := slices.Contains(granted, provider.Scopes[0])
		// Microsoft may return the short Graph scope instead of its URI.
		if provider.Name == "microsoft-contacts" {
			covered = covered || slices.Contains(granted, "Contacts.Read")
		}
		if !covered {
			return fmt.Errorf("Contacts read permission was not granted; sign in again and allow Contacts")
		}
	}
	return nil
}

func (a *App) finishContactOAuth(session *pendingContactOAuth) {
	a.contactOAuthMu.Lock()
	defer a.contactOAuthMu.Unlock()
	if a.contactOAuthManager == session.manager {
		a.contactOAuthManager = nil
	}
	session.manager.CancelAuthFlow()
	session.tokens.Clear()
}

// StartContactsOnlyOAuthFlow starts optional Contacts authorization. Mail has
// its own OAuth manager, pending response and credential namespace.
func (a *App) StartContactsOnlyOAuthFlow(provider string) error {
	session, err := a.beginContactOAuth(provider, "")
	if err != nil {
		return err
	}
	go func() {
		defer recoverPanic("app.contacts-oauth", "Contacts OAuth callback")
		err := a.waitContactOAuth(session, "")
		a.contactOAuthMu.Lock()
		defer a.contactOAuthMu.Unlock()
		if a.contactOAuthManager != session.manager {
			session.tokens.Clear()
			return // cancelled; do not resurrect pending tokens
		}
		a.contactOAuthManager = nil
		session.manager.CancelAuthFlow()
		if err != nil {
			session.tokens.Clear()
			wailsRuntime.EventsEmit(a.ctx, "contact-source-oauth:error", map[string]interface{}{"error": err.Error()})
			return
		}
		a.pendingContactOAuth = session
		wailsRuntime.EventsEmit(a.ctx, "contact-source-oauth:success", map[string]interface{}{
			"provider": provider, "email": session.email, "expiresIn": session.tokens.ExpiresIn,
		})
	}()
	return nil
}

// CompleteContactSourceOAuthSetup only reads the dedicated Contacts response.
func (a *App) CompleteContactSourceOAuthSetup(name string, syncInterval int) (*carddav.Source, error) {
	a.contactOAuthMu.Lock()
	defer a.contactOAuthMu.Unlock()
	session := a.pendingContactOAuth
	if session == nil || session.tokens == nil {
		return nil, fmt.Errorf("no pending Contacts authorization; sign in to Contacts first")
	}
	source, err := a.saveContactAuthorization(session, "", "", name, syncInterval)
	if err != nil {
		return nil, err
	}
	session.tokens.Clear()
	a.pendingContactOAuth = nil
	return source, nil
}

// LinkAccountContactSource treats Mail as an identity hint, never as a token
// source. The explicit user action opens separate Contacts consent and waits.
func (a *App) LinkAccountContactSource(accountID, name string, syncInterval int) (*carddav.Source, error) {
	acc, err := a.accountStore.Get(accountID)
	if err != nil {
		return nil, err
	}
	if acc == nil || acc.AuthType != account.AuthOAuth2 {
		return nil, fmt.Errorf("an OAuth email account is required")
	}
	provider, err := a.credStore.GetOAuthProvider(accountID)
	if err != nil {
		return nil, err
	}
	existing, err := a.carddavStore.GetSourceByAccountID(accountID)
	if err != nil {
		return nil, err
	}
	sourceID := ""
	if existing != nil {
		if a.credStore.HasContactSourceOAuthTokens(existing.ID) {
			return nil, fmt.Errorf("account already has a Contacts source; reauthorize that source in Contacts settings")
		}
		sourceID = existing.ID // migrate legacy source in place, preserving cache
	}
	session, err := a.beginContactOAuth(provider, acc.Email)
	if err != nil {
		return nil, err
	}
	defer a.finishContactOAuth(session)
	if err := a.waitContactOAuth(session, acc.Email); err != nil {
		return nil, err
	}
	a.contactOAuthMu.Lock()
	defer a.contactOAuthMu.Unlock()
	if a.contactOAuthManager != session.manager {
		return nil, fmt.Errorf("Contacts sign-in was cancelled")
	}
	return a.saveContactAuthorization(session, sourceID, accountID, name, syncInterval)
}

// ReauthorizeContactSource migrates legacy Google sources and repairs expired
// or revoked Contacts grants without touching Gmail. Cached contacts survive.
func (a *App) ReauthorizeContactSource(sourceID string) error {
	source, err := a.carddavStore.GetSource(sourceID)
	if err != nil {
		return err
	}
	if source == nil {
		return fmt.Errorf("contact source not found")
	}
	email := source.Username
	if email == "" && source.AccountID != nil {
		acc, err := a.accountStore.Get(*source.AccountID)
		if err != nil {
			return err
		}
		if acc != nil {
			email = acc.Email
		}
	}
	session, err := a.beginContactOAuth(string(source.Type), email)
	if err != nil {
		return err
	}
	defer a.finishContactOAuth(session)
	if err := a.waitContactOAuth(session, email); err != nil {
		return err
	}
	a.contactOAuthMu.Lock()
	defer a.contactOAuthMu.Unlock()
	if a.contactOAuthManager != session.manager {
		return fmt.Errorf("Contacts sign-in was cancelled")
	}
	accountID := ""
	if source.AccountID != nil {
		accountID = *source.AccountID
	}
	_, err = a.saveContactAuthorization(session, source.ID, accountID, source.Name, source.SyncInterval)
	return err
}

func (a *App) saveContactAuthorization(session *pendingContactOAuth, sourceID, accountID, name string, interval int) (*carddav.Source, error) {
	a.contactTokenMu.Lock()
	defer a.contactTokenMu.Unlock()
	provider := strings.TrimSuffix(session.provider.Name, "-contacts")
	// Avoid duplicate sources for the same authorized identity.
	if sourceID == "" {
		sources, err := a.carddavStore.ListSources()
		if err != nil {
			return nil, err
		}
		for _, s := range sources {
			if string(s.Type) == provider && strings.EqualFold(s.Username, session.email) {
				return nil, fmt.Errorf("Contacts for %s already exists; reauthorize the existing source", session.email)
			}
		}
	}
	// Microsoft Contacts retains the standalone Graph model (Outlook Mail
	// and Graph tokens have different audiences).
	if provider == "microsoft" {
		accountID = ""
	}
	created := sourceID == ""
	var source *carddav.Source
	var err error
	if created {
		source, err = a.carddavStore.CreateSource(&carddav.SourceConfig{
			Name: name, Type: carddav.SourceType(provider), Username: session.email,
			AccountID: accountID, Enabled: true, SyncInterval: interval,
		})
	} else {
		source, err = a.carddavStore.GetSource(sourceID)
	}
	if err != nil {
		return nil, err
	}
	if source == nil {
		return nil, fmt.Errorf("contact source no longer exists")
	}
	tokens := &credentials.OAuthTokens{
		Provider: session.provider.Name, AccessToken: session.tokens.AccessToken,
		RefreshToken: session.tokens.RefreshToken,
		ExpiresAt:    time.Now().Add(time.Duration(session.tokens.ExpiresIn) * time.Second),
		Scopes:       session.provider.Scopes,
	}
	if tokens.RefreshToken == "" && provider == "microsoft" {
		if previous, err := a.credStore.GetContactSourceOAuthTokens(source.ID); err == nil && previous.Provider == tokens.Provider {
			tokens.RefreshToken = previous.RefreshToken
		}
	}
	if tokens.RefreshToken == "" {
		if created {
			_ = a.carddavStore.DeleteSource(source.ID)
		}
		return nil, fmt.Errorf("Contacts authorization returned no refresh token; sign in again")
	}
	if err := a.credStore.SetContactSourceOAuthTokens(source.ID, tokens); err != nil {
		if created {
			_ = a.credStore.DeleteContactSourceOAuthTokens(source.ID)
			_ = a.carddavStore.DeleteSource(source.ID)
		}
		return nil, fmt.Errorf("save Contacts credentials: %w", err)
	}
	// Reauthorization can change the client/grant behind a legacy source.
	// Force a full provider sync while retaining the cache until it succeeds.
	if !created {
		books, err := a.carddavStore.ListAddressbooks(source.ID)
		if err != nil {
			return nil, err
		}
		for _, book := range books {
			if err := a.carddavStore.UpdateAddressbookSyncToken(book.ID, ""); err != nil {
				return nil, err
			}
		}
	}
	if err := a.carddavStore.SetOAuthIdentity(source.ID, session.email); err != nil {
		return nil, err
	}
	// A fresh read-only grant does not retain a previous write capability.
	if err := a.carddavStore.SetSourceWritable(source.ID, false); err != nil {
		return nil, err
	}
	source.Username, source.Writable = session.email, false
	if a.carddavSyncer != nil {
		go a.carddavSyncer.SyncSource(source.ID)
	}
	return source, nil
}

func (a *App) clearContactOAuth() {
	a.contactOAuthMu.Lock()
	defer a.contactOAuthMu.Unlock()
	if a.contactOAuthManager != nil {
		a.contactOAuthManager.CancelAuthFlow()
		a.contactOAuthManager = nil
	}
	if a.pendingContactOAuth != nil {
		a.pendingContactOAuth.tokens.Clear()
		a.pendingContactOAuth = nil
	}
}

func (a *App) CancelContactSourceOAuthFlow() {
	a.clearContactOAuth()
	wailsRuntime.EventsEmit(a.ctx, "contact-source-oauth:cancelled", nil)
}

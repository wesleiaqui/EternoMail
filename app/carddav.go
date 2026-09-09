package app

import (
	"fmt"
	"strings"
	"time"

	"github.com/hkdb/aerion/internal/account"
	"github.com/hkdb/aerion/internal/carddav"
	"github.com/hkdb/aerion/internal/kit/davutil"
	"github.com/hkdb/aerion/internal/logging"
)

// LinkedAccountInfo represents an email account that can be linked to a contact source
type LinkedAccountInfo struct {
	AccountID       string `json:"accountId"`
	Email           string `json:"email"`
	Name            string `json:"name"`
	Provider        string `json:"provider"`        // "google" or "microsoft"
	IsLinked        bool   `json:"isLinked"`        // Already has contact source linked
	ContactSourceID string `json:"contactSourceId"` // Separate Contacts authorization, never a Mail scope check
}

// ============================================================================
// CardDAV Contact Source API - Exposed to frontend via Wails bindings
// ============================================================================

// DiscoverCardDAVAddressbooks discovers available addressbooks from a CardDAV server
func (a *App) DiscoverCardDAVAddressbooks(url, username, password string) ([]carddav.AddressbookInfo, error) {
	return carddav.DiscoverAddressbooks(url, username, password)
}

// TestCardDAVConnection tests connection to a CardDAV server
func (a *App) TestCardDAVConnection(url, username, password string) error {
	return carddav.TestConnection(url, username, password)
}

// DiscoverCardDAVAddressbooksOAuth discovers addressbooks from a CardDAV server using
// a bearer token from an OAuth mail account (unified-grant path — a custom OIDC account
// whose token also authorizes CardDAV, e.g. Stalwart). The account token getter is
// already custom-OAuth-aware and refreshing; discovery doubles as the connection test.
func (a *App) DiscoverCardDAVAddressbooksOAuth(url, accountID string) ([]carddav.AddressbookInfo, error) {
	tokens, err := a.getValidOAuthToken(accountID)
	if err != nil {
		return nil, fmt.Errorf("failed to get OAuth token: %w", err)
	}
	httpClient := davutil.NewBearerHTTPClient(tokens.AccessToken, 30*time.Second)
	return carddav.DiscoverAddressbooksWithHTTPClient(url, httpClient)
}

// GetContactSources returns all configured contact sources
func (a *App) GetContactSources() ([]*carddav.Source, error) {
	return a.carddavStore.ListSources()
}

// GetContactSource returns a single contact source by ID
func (a *App) GetContactSource(id string) (*carddav.Source, error) {
	return a.carddavStore.GetSource(id)
}

// AddContactSource creates a new contact source with addressbooks
func (a *App) AddContactSource(config carddav.SourceConfig) (*carddav.Source, error) {
	log := logging.WithComponent("app")

	// Create the source
	source, err := a.carddavStore.CreateSource(&config)
	if err != nil {
		return nil, fmt.Errorf("failed to create source: %w", err)
	}

	// Store password in credential store (use CardDAV-specific method)
	if config.Password != "" {
		if err := a.credStore.SetCardDAVPassword(source.ID, config.Password); err != nil {
			// Rollback source creation
			a.carddavStore.DeleteSource(source.ID)
			return nil, fmt.Errorf("failed to store password: %w", err)
		}
	}

	// Create addressbooks based on enabled paths
	for _, path := range config.EnabledAddressbooks {
		_, err := a.carddavStore.CreateAddressbook(source.ID, path, addressbookDisplayName(config.AddressbookNames, path), true)
		if err != nil {
			log.Warn().Err(err).Str("path", path).Msg("Failed to create addressbook")
		}
	}

	// Trigger initial sync
	go a.carddavSyncer.SyncSource(source.ID)

	log.Info().Str("id", source.ID).Str("name", source.Name).Msg("Contact source created")
	return source, nil
}

// UpdateContactSource updates an existing contact source
func (a *App) UpdateContactSource(id string, config carddav.SourceConfig) error {
	log := logging.WithComponent("app")

	existingSource, err := a.carddavStore.GetSource(id)
	if err != nil {
		return err
	}
	if existingSource != nil && existingSource.Type != carddav.SourceTypeCardDAV {
		config.Username = existingSource.Username
	}

	// Update the source
	if err := a.carddavStore.UpdateSource(id, &config); err != nil {
		return fmt.Errorf("failed to update source: %w", err)
	}

	// Update password if provided (use CardDAV-specific method)
	if config.Password != "" {
		if err := a.credStore.SetCardDAVPassword(id, config.Password); err != nil {
			return fmt.Errorf("failed to update password: %w", err)
		}
	}

	// Differential addressbook update: only delete addressbooks whose paths
	// are no longer in EnabledAddressbooks; only create addressbooks for
	// paths not already present. Existing addressbooks keep their UUID,
	// sync_token, last_synced_at, and all the carddav_record_state rows
	// pointing at them — so a writable-toggle save or a name-only edit
	// becomes a no-op on the addressbook side.
	//
	// The previous behavior tore down ALL addressbooks on every call and
	// re-created them with new UUIDs. carddav_record_state.addressbook_id
	// has no FK cascade, so every record row was orphaned (UI went empty;
	// the next full sync rebuilt the cache from scratch and left the old
	// rows as dead bloat).
	if len(config.EnabledAddressbooks) > 0 {
		existing, listErr := a.carddavStore.ListAddressbooks(id)
		if listErr != nil {
			return fmt.Errorf("failed to list current addressbooks: %w", listErr)
		}

		existingByPath := make(map[string]*carddav.Addressbook, len(existing))
		for _, ab := range existing {
			existingByPath[ab.Path] = ab
		}
		incomingByPath := make(map[string]bool, len(config.EnabledAddressbooks))
		for _, path := range config.EnabledAddressbooks {
			incomingByPath[path] = true
		}

		// Delete addressbooks the user removed from their selection. Uses
		// DeleteAddressbookByID (tx-wrapped) so the carddav_record_state
		// rows + contact_records under that addressbook are cleaned up.
		for path, ab := range existingByPath {
			if incomingByPath[path] {
				continue
			}
			if err := a.carddavStore.DeleteAddressbookByID(ab.ID); err != nil {
				log.Warn().Err(err).Str("path", path).Msg("Failed to delete removed addressbook")
			}
		}

		// Create addressbooks for paths the user newly enabled. Existing
		// paths are skipped — preserving their UUID and downstream cache.
		for _, path := range config.EnabledAddressbooks {
			if _, exists := existingByPath[path]; exists {
				continue
			}
			if _, err := a.carddavStore.CreateAddressbook(id, path, addressbookDisplayName(config.AddressbookNames, path), true); err != nil {
				log.Warn().Err(err).Str("path", path).Msg("Failed to create new addressbook")
			}
		}
	}

	// Trigger resync
	go a.carddavSyncer.SyncSource(id)

	log.Info().Str("id", id).Msg("Contact source updated")
	return nil
}

// DeleteContactSource deletes a contact source and all its data
func (a *App) DeleteContactSource(id string) error {
	a.contactTokenMu.Lock()
	defer a.contactTokenMu.Unlock()
	log := logging.WithComponent("app")

	// Get source first to check type
	source, _ := a.carddavStore.GetSource(id)

	// Delete from database (cascades to addressbooks and contacts)
	if err := a.carddavStore.DeleteSource(id); err != nil {
		return fmt.Errorf("failed to delete source: %w", err)
	}

	// Delete credentials based on source type
	if source != nil {
		switch source.Type {
		case carddav.SourceTypeCardDAV:
			a.credStore.DeleteCardDAVPassword(id)
		case carddav.SourceTypeGoogle, carddav.SourceTypeMicrosoft:
			// Source credentials are always independent of Mail, including linked Google sources.
			a.credStore.DeleteContactSourceOAuthTokens(id)
		}
	}

	log.Info().Str("id", id).Msg("Contact source deleted")
	return nil
}

// GetSourceAddressbooks returns all addressbooks for a source
func (a *App) GetSourceAddressbooks(sourceID string) ([]*carddav.Addressbook, error) {
	return a.carddavStore.ListAddressbooks(sourceID)
}

// SetAddressbookEnabled enables or disables an addressbook
func (a *App) SetAddressbookEnabled(addressbookID string, enabled bool) error {
	return a.carddavStore.SetAddressbookEnabled(addressbookID, enabled)
}

// SetContactSourceWritable flips the writable flag for a source that supports
// writes. Google Contacts sources are permanently read-only.
func (a *App) SetContactSourceWritable(sourceID string, writable bool) error {
	if writable {
		source, err := a.carddavStore.GetSource(sourceID)
		if err != nil {
			return err
		}
		if source != nil && source.Type == carddav.SourceTypeGoogle {
			return fmt.Errorf("Google Contacts is read-only")
		}
	}
	return a.carddavStore.SetSourceWritable(sourceID, writable)
}

// SyncContactSource manually triggers a sync for a source
func (a *App) SyncContactSource(id string) error {
	return a.carddavSyncer.SyncSource(id)
}

// SyncAllContactSources manually triggers a sync for all sources
func (a *App) SyncAllContactSources() error {
	return a.carddavSyncer.SyncAllSources()
}

// ForceSyncContactSource clears the per-addressbook sync tokens for a
// CardDAV source so the next sync re-fetches every vCard from the
// server. Used to backfill multi-field data (phones, addresses, org,
// notes, etc.) for contacts originally synced under a legacy schema
// where the old parser only stored email + display name. Mirrors
// App.ForceSyncFolder for mail messages.
func (a *App) ForceSyncContactSource(sourceID string) error {
	abs, err := a.carddavStore.ListAddressbooks(sourceID)
	if err != nil {
		return fmt.Errorf("failed to list addressbooks: %w", err)
	}
	for _, ab := range abs {
		if err := a.carddavStore.UpdateAddressbookSyncToken(ab.ID, ""); err != nil {
			return fmt.Errorf("failed to clear sync token for addressbook %s: %w", ab.ID, err)
		}
	}
	return a.carddavSyncer.SyncSource(sourceID)
}

// GetContactSourceErrors returns all sources that have errors
func (a *App) GetContactSourceErrors() ([]*carddav.SourceError, error) {
	return a.carddavStore.GetSourcesWithErrors()
}

// GetContactSourceStats returns statistics for contact sources
func (a *App) GetContactSourceStats() (map[string]interface{}, error) {
	sources, err := a.carddavStore.ListSources()
	if err != nil {
		return nil, err
	}

	totalContacts, _ := a.carddavStore.CountContacts()
	sourcesWithErrors, _ := a.carddavStore.GetSourcesWithErrors()

	return map[string]interface{}{
		"total_sources":       len(sources),
		"total_contacts":      totalContacts,
		"sources_with_errors": len(sourcesWithErrors),
	}, nil
}

// ============================================================================
// OAuth Contact Source API - Google/Microsoft contact sync
// ============================================================================

// GetLinkedAccountsForContactSync returns email accounts that can be linked to contact sources
func (a *App) GetLinkedAccountsForContactSync() ([]LinkedAccountInfo, error) {
	log := logging.WithComponent("app")

	accounts, err := a.accountStore.List()
	if err != nil {
		return nil, fmt.Errorf("failed to list accounts: %w", err)
	}

	// Get existing contact sources to check which accounts are already linked
	sources, err := a.carddavStore.ListSources()
	if err != nil {
		return nil, fmt.Errorf("failed to list sources: %w", err)
	}

	var result []LinkedAccountInfo
	for _, acc := range accounts {
		// Only OAuth accounts (Google/Microsoft) can be linked for contacts
		if acc.AuthType != account.AuthOAuth2 {
			continue
		}

		// Get OAuth provider
		provider, err := a.credStore.GetOAuthProvider(acc.ID)
		if err != nil || provider == "" {
			continue
		}

		// Only Google supports linked contact sync
		// Microsoft can't share tokens between Outlook (email) and Graph API (contacts) due to audience restrictions
		if provider != "google" {
			continue
		}

		contactSourceID := ""
		for _, source := range sources {
			if source.Type == carddav.SourceTypeGoogle && ((source.AccountID != nil && *source.AccountID == acc.ID) || strings.EqualFold(source.Username, acc.Email)) {
				contactSourceID = source.ID
				break
			}
		}

		result = append(result, LinkedAccountInfo{
			AccountID:       acc.ID,
			Email:           acc.Email,
			Name:            acc.Name,
			Provider:        provider,
			IsLinked:        contactSourceID != "",
			ContactSourceID: contactSourceID,
		})
	}

	log.Debug().Int("count", len(result)).Msg("Found linkable accounts for contact sync")
	return result, nil
}

// GetCustomOAuthAccounts returns mail accounts that authenticate via a custom
// ("bring your own app") OIDC provider. Their access token can be reused to
// authenticate a CardDAV source against the same unified server (e.g. Stalwart),
// so these populate the OAuth-account picker in the Add CardDAV Source dialog.
// Unlike GetLinkedAccountsForContactSync (Google People API), this is the DAV path.
func (a *App) GetCustomOAuthAccounts() ([]LinkedAccountInfo, error) {
	accounts, err := a.accountStore.List()
	if err != nil {
		return nil, fmt.Errorf("failed to list accounts: %w", err)
	}

	sources, err := a.carddavStore.ListSources()
	if err != nil {
		return nil, fmt.Errorf("failed to list sources: %w", err)
	}
	linkedAccountIDs := make(map[string]bool)
	for _, source := range sources {
		if source.AccountID != nil && *source.AccountID != "" {
			linkedAccountIDs[*source.AccountID] = true
		}
	}

	var result []LinkedAccountInfo
	for _, acc := range accounts {
		if acc.AuthType != account.AuthOAuth2 {
			continue
		}
		provider, perr := a.credStore.GetOAuthProvider(acc.ID)
		if perr != nil || provider != customOAuthProviderName {
			continue
		}
		result = append(result, LinkedAccountInfo{
			AccountID: acc.ID,
			Email:     acc.Email,
			Name:      acc.Name,
			Provider:  provider,
			IsLinked:  linkedAccountIDs[acc.ID],
		})
	}
	return result, nil
}

// addressbookDisplayName resolves the stored name for an addressbook path:
// the display name discovery found (passed through SourceConfig, #366),
// falling back to the path's last segment.
func addressbookDisplayName(names map[string]string, path string) string {
	if name := names[path]; name != "" {
		return name
	}
	if parts := strings.Split(strings.Trim(path, "/"), "/"); len(parts) > 0 && parts[len(parts)-1] != "" {
		return parts[len(parts)-1]
	}
	return path
}

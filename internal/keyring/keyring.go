// Package keyring provides secure credential storage using the OS keychain
package keyring

import (
	"errors"
	"fmt"
	"log/slog"

	gokeyring "github.com/zalando/go-keyring"
)

const serviceName = "aerion"

// Keyring provides secure credential storage
type Keyring struct{}

// New creates a new Keyring instance
func New() *Keyring {
	return &Keyring{}
}

// SetPassword stores a password for an account
func (k *Keyring) SetPassword(accountID, password string) error {
	err := gokeyring.Set(serviceName, accountID, password)
	if err != nil {
		return keyringFailure("failed to store password", err)
	}
	return nil
}

// GetPassword retrieves a password for an account
func (k *Keyring) GetPassword(accountID string) (string, error) {
	password, err := gokeyring.Get(serviceName, accountID)
	if err == gokeyring.ErrNotFound {
		return "", ErrCredentialNotFound
	}
	if err != nil {
		return "", keyringFailure("failed to retrieve password", err)
	}
	return password, nil
}

// DeletePassword removes a password for an account
func (k *Keyring) DeletePassword(accountID string) error {
	err := gokeyring.Delete(serviceName, accountID)
	if err == gokeyring.ErrNotFound {
		return nil // Already deleted, not an error
	}
	if err != nil {
		return keyringFailure("failed to delete password", err)
	}
	return nil
}

// SetOAuthTokens stores OAuth2 tokens for an account
func (k *Keyring) SetOAuthTokens(accountID, accessToken, refreshToken string) error {
	// Store access token
	if err := gokeyring.Set(serviceName, accountID+":access_token", accessToken); err != nil {
		return keyringFailure("failed to store access token", err)
	}
	// Store refresh token
	if err := gokeyring.Set(serviceName, accountID+":refresh_token", refreshToken); err != nil {
		return keyringFailure("failed to store refresh token", err)
	}
	return nil
}

// GetOAuthTokens retrieves OAuth2 tokens for an account
func (k *Keyring) GetOAuthTokens(accountID string) (accessToken, refreshToken string, err error) {
	accessToken, err = gokeyring.Get(serviceName, accountID+":access_token")
	if err == gokeyring.ErrNotFound {
		return "", "", ErrCredentialNotFound
	}
	if err != nil {
		return "", "", keyringFailure("failed to retrieve access token", err)
	}

	refreshToken, err = gokeyring.Get(serviceName, accountID+":refresh_token")
	if err == gokeyring.ErrNotFound {
		return "", "", ErrCredentialNotFound
	}
	if err != nil {
		return "", "", keyringFailure("failed to retrieve refresh token", err)
	}

	return accessToken, refreshToken, nil
}

// DeleteOAuthTokens removes OAuth2 tokens for an account
func (k *Keyring) DeleteOAuthTokens(accountID string) error {
	return errors.Join(deleteEntry(accountID+":access_token"), deleteEntry(accountID+":refresh_token"))
}

// DeleteAllCredentials removes all credentials for an account
func (k *Keyring) DeleteAllCredentials(accountID string) error {
	return errors.Join(k.DeletePassword(accountID), k.DeleteOAuthTokens(accountID))
}

// Do not expose backend error strings: they can contain secret-service payloads.
func keyringFailure(operation string, err error) error {
	slog.Warn("OS keyring operation failed", "operation", operation)
	return fmt.Errorf("%s: keyring unavailable — run in a desktop session with an unlocked keyring", operation)
}
func deleteEntry(key string) error {
	err := gokeyring.Delete(serviceName, key)
	if err == nil || errors.Is(err, gokeyring.ErrNotFound) {
		return nil
	}
	return keyringFailure("failed to delete credential", err)
}

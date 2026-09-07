package ipc

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"sync"
)

const (
	// TokenSize is the number of random bytes in the token (256 bits)
	TokenSize = 32
)

// TokenManager handles secure token generation and validation.
// It is safe for concurrent use.
type TokenManager struct {
	token string
	mu    sync.RWMutex
}

// NewTokenManager creates a new TokenManager with a freshly generated token.
func NewTokenManager() (*TokenManager, error) {
	tm := &TokenManager{}
	if err := tm.Regenerate(); err != nil {
		return nil, err
	}
	return tm, nil
}

// Regenerate creates a new random token, replacing any existing token.
func (tm *TokenManager) Regenerate() error {
	bytes := make([]byte, TokenSize)
	if _, err := rand.Read(bytes); err != nil {
		return err
	}

	tm.mu.Lock()
	tm.token = hex.EncodeToString(bytes)
	tm.mu.Unlock()

	return nil
}

// GetToken returns the current token.
func (tm *TokenManager) GetToken() string {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	return tm.token
}

// Validate checks if the provided token matches the stored token.
// Uses constant-time comparison to prevent timing attacks.
func (tm *TokenManager) Validate(provided string) bool {
	tm.mu.RLock()
	expected := tm.token
	tm.mu.RUnlock()

	// Use constant-time comparison to prevent timing attacks
	if expected == "" || provided == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(expected), []byte(provided)) == 1
}

// GenerateToken creates a new random token without storing it.
// This is useful for one-off token generation.
func GenerateToken() (string, error) {
	bytes := make([]byte, TokenSize)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

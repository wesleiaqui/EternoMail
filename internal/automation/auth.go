package automation

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"

	v1 "github.com/hkdb/aerion/internal/automation/v1"
)

var (
	ErrClientNotFound   = errors.New("automation: client not found")
	ErrTokenRevoked     = errors.New("automation: token revoked")
	ErrCapabilityDenied = errors.New("automation: capability denied")
)

// ClientGrant holds authorized capabilities and lifecycle metadata for a registered client.
type ClientGrant struct {
	ClientID     string          `json:"clientId"`
	Name         string          `json:"name"`
	Token        string          `json:"-"` // Never serialized
	Capabilities []v1.Capability `json:"capabilities"`
	CreatedAt    time.Time       `json:"createdAt"`
	Revoked      bool            `json:"revoked"`
}

// TokenStore manages authorized clients and bearer tokens.
type TokenStore interface {
	GetGrant(token string) (*ClientGrant, error)
	RegisterClient(name string, capabilities []v1.Capability) (*ClientGrant, error)
	RevokeClient(clientID string) error
	ListClients() ([]*ClientGrant, error)
}

// MemoryTokenStore is an in-memory implementation of TokenStore with thread safety.
type MemoryTokenStore struct {
	mu      sync.RWMutex
	byToken map[string]*ClientGrant
	byID    map[string]*ClientGrant
}

// NewMemoryTokenStore initializes an empty in-memory token store.
func NewMemoryTokenStore() *MemoryTokenStore {
	return &MemoryTokenStore{
		byToken: make(map[string]*ClientGrant),
		byID:    make(map[string]*ClientGrant),
	}
}

// RegisterClient generates a secure random token and registers a new client with specified capabilities.
func (s *MemoryTokenStore) RegisterClient(name string, capabilities []v1.Capability) (*ClientGrant, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return nil, fmt.Errorf("generate token: %w", err)
	}
	token := hex.EncodeToString(tokenBytes)

	idBytes := make([]byte, 16)
	if _, err := rand.Read(idBytes); err != nil {
		return nil, fmt.Errorf("generate client id: %w", err)
	}
	clientID := "client-" + hex.EncodeToString(idBytes)

	var validCaps []v1.Capability
	for _, cap := range capabilities {
		if cap.Known() {
			validCaps = append(validCaps, cap)
		}
	}

	grant := &ClientGrant{
		ClientID:     clientID,
		Name:         name,
		Token:        token,
		Capabilities: validCaps,
		CreatedAt:    time.Now().UTC(),
		Revoked:      false,
	}

	s.byToken[token] = grant
	s.byID[clientID] = grant

	return grant, nil
}

// GetGrant retrieves the client grant corresponding to the bearer token.
func (s *MemoryTokenStore) GetGrant(token string) (*ClientGrant, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	grant, ok := s.byToken[token]
	if !ok {
		return nil, ErrClientNotFound
	}
	if grant.Revoked {
		return nil, ErrTokenRevoked
	}

	return grant, nil
}

// RevokeClient marks a client grant as revoked.
func (s *MemoryTokenStore) RevokeClient(clientID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	grant, ok := s.byID[clientID]
	if !ok {
		return ErrClientNotFound
	}
	grant.Revoked = true
	return nil
}

// ListClients returns a copy of all registered client grants (excluding tokens).
func (s *MemoryTokenStore) ListClients() ([]*ClientGrant, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	res := make([]*ClientGrant, 0, len(s.byID))
	for _, g := range s.byID {
		copyGrant := *g
		copyGrant.Token = ""
		res = append(res, &copyGrant)
	}
	return res, nil
}

// TokenAuthorizer adapts a ClientGrant into v1.Authorizer.
type TokenAuthorizer struct {
	grant *ClientGrant
}

// NewTokenAuthorizer creates an Authorizer checking against the given grant.
func NewTokenAuthorizer(grant *ClientGrant) *TokenAuthorizer {
	return &TokenAuthorizer{grant: grant}
}

// RequireCapability checks whether the grant contains the specified capability.
func (a *TokenAuthorizer) RequireCapability(_ context.Context, capability string) error {
	if a == nil || a.grant == nil || a.grant.Revoked {
		return ErrCapabilityDenied
	}
	target := v1.Capability(capability)
	for _, c := range a.grant.Capabilities {
		if c == target {
			return nil
		}
	}
	return fmt.Errorf("%w: %s", ErrCapabilityDenied, capability)
}

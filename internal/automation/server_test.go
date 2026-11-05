package automation_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/hkdb/aerion/internal/automation"
	v1 "github.com/hkdb/aerion/internal/automation/v1"
)

func setupTestServer(t *testing.T) (*automation.Server, *automation.ClientGrant, *automation.MemoryTokenStore) {
	store := automation.NewMemoryTokenStore()
	grant, err := store.RegisterClient("test-agent", []v1.Capability{v1.CapabilityMailRead, v1.CapabilityDraftWrite})
	if err != nil {
		t.Fatalf("failed to register client: %v", err)
	}

	server := automation.NewServer(automation.ServerConfig{
		Addr:       "127.0.0.1:0",
		TokenStore: store,
	})

	if err := server.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = server.Stop(ctx)
	})

	return server, grant, store
}

func TestServer_AuthenticationAndCapabilities(t *testing.T) {
	server, grant, store := setupTestServer(t)

	// 1. Missing Authorization header
	req, _ := http.NewRequest(http.MethodGet, server.URL()+"/v1/capabilities", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401 Unauthorized, got %d", resp.StatusCode)
	}

	// 2. Invalid bearer token
	req, _ = http.NewRequest(http.MethodGet, server.URL()+"/v1/capabilities", nil)
	req.Header.Set("Authorization", "Bearer invalid-token")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401 Unauthorized, got %d", resp.StatusCode)
	}

	// 3. Valid bearer token
	req, _ = http.NewRequest(http.MethodGet, server.URL()+"/v1/capabilities", nil)
	req.Header.Set("Authorization", "Bearer "+grant.Token)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 OK, got %d", resp.StatusCode)
	}

	var capsResp v1.Response[v1.Capabilities]
	if err := json.NewDecoder(resp.Body).Decode(&capsResp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if capsResp.Data.Version != "v1" {
		t.Errorf("expected version v1, got %s", capsResp.Data.Version)
	}
	if len(capsResp.Data.Capabilities) != 2 {
		t.Errorf("expected 2 capabilities, got %d", len(capsResp.Data.Capabilities))
	}

	// 4. Revoked token
	if err := store.RevokeClient(grant.ClientID); err != nil {
		t.Fatalf("failed to revoke client: %v", err)
	}
	req, _ = http.NewRequest(http.MethodGet, server.URL()+"/v1/capabilities", nil)
	req.Header.Set("Authorization", "Bearer "+grant.Token)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401 for revoked token, got %d", resp.StatusCode)
	}
}

func TestServer_SecurityProtections(t *testing.T) {
	server, grant, _ := setupTestServer(t)

	// 1. Origin header rejected (No browser requests / No CORS)
	req, _ := http.NewRequest(http.MethodGet, server.URL()+"/v1/capabilities", nil)
	req.Header.Set("Authorization", "Bearer "+grant.Token)
	req.Header.Set("Origin", "http://evil.com")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("expected 403 Forbidden on Origin header, got %d", resp.StatusCode)
	}

	// 2. Reject credentials in query string
	req, _ = http.NewRequest(http.MethodGet, server.URL()+"/v1/capabilities?token=secret", nil)
	req.Header.Set("Authorization", "Bearer "+grant.Token)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 Bad Request on token in query string, got %d", resp.StatusCode)
	}

	// 3. Body size limit
	bigPayload := strings.Repeat("A", automation.MaxRequestBodyBytes+100)
	req, _ = http.NewRequest(http.MethodPost, server.URL()+"/v1/capabilities", strings.NewReader(bigPayload))
	req.Header.Set("Authorization", "Bearer "+grant.Token)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusMethodNotAllowed && resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Errorf("unexpected status %d, body: %s", resp.StatusCode, string(body))
	}
}

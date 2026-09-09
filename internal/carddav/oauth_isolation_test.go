package carddav

import (
	"fmt"
	"testing"
)

func TestGoogleSyncNeverUsesMailToken(t *testing.T) {
	accountID := "gmail-account"
	s := NewSyncer(nil, nil)
	s.SetAccessTokenGetters(func(string) (string, error) {
		t.Fatal("Google Contacts attempted to read Gmail credentials")
		return "", nil
	}, func(id string) (string, error) {
		if id != "source" {
			t.Fatalf("unexpected credential key %q", id)
		}
		return "contacts-token", nil
	})
	for _, linked := range []*string{nil, &accountID} {
		token, err := s.getOAuthToken(&Source{ID: "source", Type: SourceTypeGoogle, AccountID: linked})
		if err != nil || token != "contacts-token" {
			t.Fatalf("token routing: %q %v", token, err)
		}
	}
	s.getSourceToken = func(string) (string, error) { return "", fmt.Errorf("authorize Contacts") }
	if _, err := s.getOAuthToken(&Source{ID: "source", Type: SourceTypeGoogle, AccountID: &accountID}); err == nil {
		t.Fatal("missing Contacts grant silently fell back to Mail")
	}
}

func TestCustomCardDAVStillUsesAccountToken(t *testing.T) {
	id := "custom-account"
	s := NewSyncer(nil, nil)
	s.SetAccessTokenGetters(func(string) (string, error) { return "custom-token", nil },
		func(string) (string, error) { t.Fatal("custom CardDAV used Contacts credentials"); return "", nil })
	token, err := s.getOAuthToken(&Source{ID: "dav", Type: SourceTypeCardDAV, AccountID: &id})
	if err != nil || token != "custom-token" {
		t.Fatal("custom OAuth regression", err)
	}
}

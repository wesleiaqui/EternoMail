package credentials

import (
	"bytes"
	"database/sql"
	"github.com/rs/zerolog"
	gokeyring "github.com/zalando/go-keyring"
	"strings"
	"testing"
)

func TestCleanupLogsDatabaseFailure(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	var output bytes.Buffer
	s := &Store{db: db, log: zerolog.New(&output)}
	s.clearDBPassword("account")
	if !strings.Contains(output.String(), "Failed to clear credential") {
		t.Fatal("cleanup error hidden")
	}
}
func TestTokenColumnRejectsSQL(t *testing.T) {
	for _, write := range []bool{true, false} {
		if _, err := tokenColumnQuery("encrypted_access_token = NULL; DROP TABLE accounts; --", write); err == nil {
			t.Fatal("accepted SQL identifier")
		}
		for _, col := range []string{"encrypted_access_token", "encrypted_refresh_token"} {
			if _, err := tokenColumnQuery(col, write); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestOAuthTokensClear(t *testing.T) {
	tokens := &OAuthTokens{AccessToken: "access", RefreshToken: "refresh", Scopes: []string{"mail"}}
	tokens.Clear()
	if tokens.AccessToken != "" || tokens.RefreshToken != "" || tokens.Scopes != nil {
		t.Fatal("secrets retained")
	}
}

func TestDeleteOAuthTokensRemovesClientConfigKeyringSlots(t *testing.T) {
	gokeyring.MockInit()
	s, db := newTestStore(t)
	s.keyringEnabled = true
	insertTestAccount(t, db, "account-to-delete")
	for _, slot := range []string{"google-mail", "google-contacts"} {
		if err := s.SetOAuthTokensForClientConfig("account-to-delete", slot, &OAuthTokens{Provider: "google", AccessToken: "access", RefreshToken: "refresh"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.DeleteOAuthTokens("account-to-delete"); err != nil {
		t.Fatal(err)
	}
	for _, slot := range []string{"google-mail", "google-contacts"} {
		for _, kind := range []string{"access_token", "refresh_token"} {
			if _, err := gokeyring.Get(serviceName, "account-to-delete:"+slot+":"+kind); err != gokeyring.ErrNotFound {
				t.Fatal("orphaned keyring token", err)
			}
		}
	}
}

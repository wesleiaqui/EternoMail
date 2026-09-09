package backend

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hkdb/aerion/internal/carddav"
	coreapi "github.com/hkdb/aerion/internal/core/api/v1"
)

func TestGoogleLinkedSourceHTTPUsesSourceCredentials(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer source-token" {
			t.Error("wrong bearer token")
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	api := &API{} // no core broker: using it would fail
	api.SetStandaloneSourceTokenGetter(func(id string) (string, error) {
		if id != "source" {
			t.Fatalf("wrong source id %q", id)
		}
		return "source-token", nil
	})
	accountID := "gmail"
	client, err := api.httpClientForSource(&carddav.Source{ID: "source", Type: carddav.SourceTypeGoogle, AccountID: &accountID}, coreapi.AuthScope{Resource: "https://www.googleapis.com/auth/contacts.readonly"})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
}

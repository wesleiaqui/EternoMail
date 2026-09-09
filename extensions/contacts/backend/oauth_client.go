package backend

import (
	"fmt"
	"net/http"

	"github.com/hkdb/aerion/internal/carddav"
	coreapi "github.com/hkdb/aerion/internal/core/api/v1"
)

// cardDAVWriteScope is the nominal scope passed when building a bearer client for an
// account-linked CardDAV write. Account-linked CardDAV sources are custom-OAuth-only,
// and the broker skips scope gating for custom (single opaque grant), so this never
// drives incremental consent — it's a label for diagnostics.
var cardDAVWriteScope = coreapi.AuthScope{
	Resource: "carddav",
	Reason:   "Write contacts to your CardDAV server",
}

// httpClientForSource uses source-owned credentials for all Google sources,
// including those logically associated with Mail. Microsoft legacy account
// routing and custom CardDAV behavior are preserved. The host refreshes
// source tokens proactively; an in-flight revocation surfaces to the UI.
func (a *API) httpClientForSource(source *carddav.Source, scope coreapi.AuthScope) (*http.Client, error) {
	if source == nil {
		return nil, fmt.Errorf("contacts: httpClientForSource: nil source")
	}

	if source.Type != carddav.SourceTypeGoogle && source.AccountID != nil && *source.AccountID != "" {
		if a.core == nil {
			return nil, fmt.Errorf("contacts: httpClientForSource: core not wired")
		}
		return a.core.Auth().HTTPClient(*source.AccountID, []coreapi.AuthScope{scope})
	}

	if a.getStandaloneSourceToken == nil {
		return nil, fmt.Errorf("contacts: httpClientForSource: standalone-source token getter not wired (source %s)", source.ID)
	}
	token, err := a.getStandaloneSourceToken(source.ID)
	if err != nil {
		return nil, fmt.Errorf("contacts: httpClientForSource: get standalone token for %s: %w", source.ID, err)
	}
	return &http.Client{
		Transport: &bearerTransport{token: token},
	}, nil
}

// bearerTransport stamps a static bearer token on every request. Used for
// standalone contact source HTTP clients where token refresh is handled
// proactively by the host before the client is built (see
// app.getValidContactSourceOAuthToken). No 401 retry — a stale token after
// build surfaces as a 401 to the caller, who returns the error up to the UI.
type bearerTransport struct {
	base  http.RoundTripper
	token string
}

func (t *bearerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	cloned := req.Clone(req.Context())
	if cloned.Header == nil {
		cloned.Header = make(http.Header)
	}
	cloned.Header.Set("Authorization", "Bearer "+t.token)
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}
	return base.RoundTrip(cloned)
}

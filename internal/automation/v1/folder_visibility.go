package v1

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
)

// FolderVisibilityRequest changes only the owning mailbox's local preference.
type FolderVisibilityRequest struct {
	AccountID string   `json:"accountId"`
	FolderIDs []string `json:"folderIds"`
	Hidden    *bool    `json:"hidden"`
	Refresh   bool     `json:"refresh"`
}

// FolderVisibilityOperations changes preferences on the mailbox-owning runtime.
type FolderVisibilityOperations interface {
	SetFolderVisibility(context.Context, string, []string, bool, bool) error
}

// MountFolderVisibilityRoutes exposes account-validated folder visibility with mail-write authorization.
func MountFolderVisibilityRoutes(mux *http.ServeMux, ops FolderVisibilityOperations, requireCap func(http.ResponseWriter, *http.Request, Capability) bool) {
	mux.HandleFunc("/v1/folders/visibility", func(w http.ResponseWriter, r *http.Request) {
		if !requireCap(w, r, CapabilityMailWrite) {
			return
		}
		if r.Method != http.MethodPost {
			writeHeaderError(w, r, http.StatusMethodNotAllowed, CodeInvalidRequest, "Method not allowed", false)
			return
		}
		var req FolderVisibilityRequest
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil || strings.TrimSpace(req.AccountID) == "" || req.Hidden == nil || len(req.FolderIDs) == 0 || len(req.FolderIDs) > 10000 || (*req.Hidden && req.Refresh) {
			writeHeaderError(w, r, http.StatusBadRequest, CodeInvalidRequest, "Valid accountId, folderIds and hidden are required; refresh is only for showing folders", false)
			return
		}
		if err := ops.SetFolderVisibility(r.Context(), req.AccountID, req.FolderIDs, *req.Hidden, req.Refresh); err != nil {
			writeHeaderError(w, r, http.StatusBadRequest, CodeInvalidRequest, err.Error(), false)
			return
		}
		writeAPIResponse(w, r, http.StatusOK, struct{}{})
	})
}

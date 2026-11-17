package v1

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/hkdb/aerion/internal/account"
	"github.com/hkdb/aerion/internal/email/replycontent"
	"github.com/hkdb/aerion/internal/folder"
	"github.com/hkdb/aerion/internal/message"
	"github.com/hkdb/aerion/internal/views"
	"github.com/hkdb/aerion/internal/virtualfolder"
)

// CatalogOperations defines store-backed metadata reads for accounts,
// folders, and saved views.
type CatalogOperations interface {
	GetAccounts() ([]*account.Account, error)
	GetFolders(accountID string) ([]*folder.Folder, error)
	GetVirtualFolders() ([]*virtualfolder.VirtualFolder, error)
}

// IdentityCatalogOperations exposes account-owned sender identities needed by
// compose clients without returning provider credentials.
type IdentityCatalogOperations interface {
	GetIdentities(accountID string) ([]*account.Identity, error)
}

// MessageHeaderOperations defines bounded, folder-scoped header reads.
type MessageHeaderOperations interface {
	GetFolder(folderID string) (*folder.Folder, error)
	ListByFolder(folderID string, offset, limit int) ([]*message.MessageHeader, error)
}

// SearchOperations defines the provider-aware bounded search operation.
type SearchOperations interface {
	SearchMessages(ctx context.Context, scope views.Scope, query string, cursor string, limit int) (*views.ThreadPage, error)
}

// FilteredSearchOperations supports the desktop's bounded triage filters.
// SearchOperations remains compatible with read-only adapters that do not
// provide those filters.
type FilteredSearchOperations interface {
	SearchMessagesWithFilter(ctx context.Context, scope views.Scope, query, filter, cursor string, limit int) (*views.ThreadPage, error)
}

// MessageReadOperations defines thread and body reads. Body fetch remains an
// explicit operation and is guarded by a separate capability.
type MessageReadOperations interface {
	GetThread(ctx context.Context, ref views.ThreadRef, scope views.Scope) (*views.ThreadView, error)
	GetMessage(accountID, messageID string) (*message.Message, error)
	GetReplyContent(accountID, messageID, channel string, fetch bool) (*replycontent.Reply, error)
	FetchMessageBody(ctx context.Context, accountID, messageID string) (*message.Message, error)
}

// AttachmentReadOperations defines attachment metadata and byte reads.
type AttachmentReadOperations interface {
	GetAttachments(messageID string) ([]*message.Attachment, error)
	ReadAttachmentBytes(accountID, messageID, attachmentID string) ([]byte, string, error)
}

// ReadOperations defines the complete read surface for callers that implement
// catalog, search, message, and attachment reads together.
type ReadOperations interface {
	CatalogOperations
	SearchOperations
	MessageReadOperations
	AttachmentReadOperations
}

// ReadHandler routes and handles read/search/thread/body/attachment requests.
type ReadHandler struct {
	search      SearchOperations
	messages    MessageReadOperations
	attachments AttachmentReadOperations
	catalog     CatalogOperations
}

// NewReadHandler creates a ReadHandler backed by the provided operations.
func NewReadHandler(ops ReadOperations) *ReadHandler {
	return &ReadHandler{search: ops, messages: ops, attachments: ops, catalog: ops}
}

// MountCatalogRoutes mounts only the account, folder, and saved-view routes.
// The enclosing automation server must authenticate the request and apply its
// loopback, Host, and Origin protections.
func MountCatalogRoutes(mux *http.ServeMux, ops CatalogOperations, requireCap func(http.ResponseWriter, *http.Request, Capability) bool) {
	handler := &ReadHandler{catalog: ops}
	handler.mountCatalogRoutes(mux, requireCap)
}

// MountSearchRoute mounts only the authenticated search operation.
func MountSearchRoute(mux *http.ServeMux, ops SearchOperations, requireCap func(http.ResponseWriter, *http.Request, Capability) bool) {
	(&ReadHandler{search: ops}).mountSearchRoute(mux, requireCap)
}

// MountThreadBodyRoutes mounts thread and body reads without requiring
// attachment operations to be implemented by the same backend.
func MountThreadBodyRoutes(mux *http.ServeMux, ops MessageReadOperations, requireCap func(http.ResponseWriter, *http.Request, Capability) bool) {
	(&ReadHandler{messages: ops}).mountThreadBodyRoutes(mux, requireCap)
}

// MountRoutes mounts read API endpoints onto a standard http.ServeMux.
func (h *ReadHandler) MountRoutes(mux *http.ServeMux, requireCap func(http.ResponseWriter, *http.Request, Capability) bool) {
	h.mountCatalogRoutes(mux, requireCap)
	h.mountSearchRoute(mux, requireCap)
	h.mountThreadBodyRoutes(mux, requireCap)
	h.mountAttachmentRoutes(mux, requireCap)
}

func (h *ReadHandler) mountSearchRoute(mux *http.ServeMux, requireCap func(http.ResponseWriter, *http.Request, Capability) bool) {
	mux.HandleFunc("/v1/search", func(w http.ResponseWriter, r *http.Request) {
		if !requireCap(w, r, CapabilityMailRead) {
			return
		}
		if r.Method != http.MethodPost {
			h.writeError(w, r, http.StatusMethodNotAllowed, CodeInvalidRequest, "Method not allowed", false)
			return
		}
		h.handleSearch(w, r)
	})
}

func (h *ReadHandler) mountThreadBodyRoutes(mux *http.ServeMux, requireCap func(http.ResponseWriter, *http.Request, Capability) bool) {
	mux.HandleFunc("GET /v1/messages/{messageID}/header", func(w http.ResponseWriter, r *http.Request) {
		if !requireCap(w, r, CapabilityMailRead) {
			return
		}
		accountID := strings.TrimSpace(r.URL.Query().Get("accountId"))
		messageID := strings.TrimSpace(r.PathValue("messageID"))
		if accountID == "" || messageID == "" {
			h.writeError(w, r, http.StatusBadRequest, CodeInvalidRequest, "accountId and messageId are required", false)
			return
		}
		item, err := h.messages.GetMessage(accountID, messageID)
		if err != nil || item == nil || item.AccountID != accountID {
			h.writeError(w, r, http.StatusNotFound, CodeNotFound, "Message not found", false)
			return
		}
		header := *item
		header.BodyText, header.BodyHTML = "", ""
		h.writeResponse(w, r, http.StatusOK, &header, nil)
	})
	mux.HandleFunc("/v1/threads/read", func(w http.ResponseWriter, r *http.Request) {
		if !requireCap(w, r, CapabilityMailRead) {
			return
		}
		if r.Method != http.MethodPost {
			h.writeError(w, r, http.StatusMethodNotAllowed, CodeInvalidRequest, "Method not allowed", false)
			return
		}
		h.handleThreadRead(w, r)
	})

	mux.HandleFunc("/v1/messages/body", func(w http.ResponseWriter, r *http.Request) {
		if !requireCap(w, r, CapabilityMailRead) {
			return
		}
		if r.Method != http.MethodPost {
			h.writeError(w, r, http.StatusMethodNotAllowed, CodeInvalidRequest, "Method not allowed", false)
			return
		}
		h.handleMessageBody(w, r, requireCap)
	})
}

func (h *ReadHandler) mountAttachmentRoutes(mux *http.ServeMux, requireCap func(http.ResponseWriter, *http.Request, Capability) bool) {
	mux.HandleFunc("/v1/messages/attachments/list", func(w http.ResponseWriter, r *http.Request) {
		if !requireCap(w, r, CapabilityAttachmentRead) {
			return
		}
		if r.Method != http.MethodPost {
			h.writeError(w, r, http.StatusMethodNotAllowed, CodeInvalidRequest, "Method not allowed", false)
			return
		}
		h.handleAttachmentList(w, r)
	})

	mux.HandleFunc("/v1/messages/attachments/read", func(w http.ResponseWriter, r *http.Request) {
		if !requireCap(w, r, CapabilityAttachmentRead) {
			return
		}
		if r.Method != http.MethodPost {
			h.writeError(w, r, http.StatusMethodNotAllowed, CodeInvalidRequest, "Method not allowed", false)
			return
		}
		h.handleAttachmentRead(w, r)
	})
}

func (h *ReadHandler) mountCatalogRoutes(mux *http.ServeMux, requireCap func(http.ResponseWriter, *http.Request, Capability) bool) {
	if identities, ok := h.catalog.(IdentityCatalogOperations); ok {
		mux.HandleFunc("GET /v1/accounts/{accountID}/identities", func(w http.ResponseWriter, r *http.Request) {
			if !requireCap(w, r, CapabilityMailRead) {
				return
			}
			items, err := identities.GetIdentities(r.PathValue("accountID"))
			if err != nil {
				h.writeError(w, r, http.StatusInternalServerError, CodeInternal, "Failed to load account identities", true)
				return
			}
			h.writeResponse(w, r, http.StatusOK, items, nil)
		})
	}
	mux.HandleFunc("/v1/accounts", func(w http.ResponseWriter, r *http.Request) {
		if !requireCap(w, r, CapabilityMailRead) {
			return
		}
		if r.Method != http.MethodGet {
			h.writeError(w, r, http.StatusMethodNotAllowed, CodeInvalidRequest, "Method not allowed", false)
			return
		}
		h.handleAccounts(w, r)
	})
	mux.HandleFunc("/v1/accounts/", func(w http.ResponseWriter, r *http.Request) {
		if !requireCap(w, r, CapabilityMailRead) {
			return
		}
		if r.Method != http.MethodGet {
			h.writeError(w, r, http.StatusMethodNotAllowed, CodeInvalidRequest, "Method not allowed", false)
			return
		}
		accountID := strings.TrimPrefix(r.URL.Path, "/v1/accounts/")
		if accountID == "" || strings.Contains(accountID, "/") {
			h.writeError(w, r, http.StatusBadRequest, CodeInvalidRequest, "Account ID is required", false)
			return
		}
		h.handleAccount(w, r, accountID)
	})

	mux.HandleFunc("/v1/folders", func(w http.ResponseWriter, r *http.Request) {
		if !requireCap(w, r, CapabilityMailRead) {
			return
		}
		if r.Method != http.MethodGet {
			h.writeError(w, r, http.StatusMethodNotAllowed, CodeInvalidRequest, "Method not allowed", false)
			return
		}
		h.handleFolders(w, r)
	})

	mux.HandleFunc("/v1/views", func(w http.ResponseWriter, r *http.Request) {
		if !requireCap(w, r, CapabilityMailRead) {
			return
		}
		if r.Method != http.MethodGet {
			h.writeError(w, r, http.StatusMethodNotAllowed, CodeInvalidRequest, "Method not allowed", false)
			return
		}
		h.handleViews(w, r)
	})
}

// AccountSummary contains non-secret account configuration for settings and agents.
type AccountSummary struct {
	ID                       string                `json:"id"`
	Email                    string                `json:"email"`
	Name                     string                `json:"name"`
	SharedMailboxParentID    string                `json:"sharedMailboxParentId,omitempty"`
	IMAPHost                 string                `json:"imapHost"`
	IMAPPort                 int                   `json:"imapPort"`
	IMAPSecurity             account.SecurityType  `json:"imapSecurity"`
	IMAPAuthMechanism        account.AuthMechanism `json:"imapAuthMechanism"`
	SMTPHost                 string                `json:"smtpHost"`
	SMTPPort                 int                   `json:"smtpPort"`
	SMTPSecurity             account.SecurityType  `json:"smtpSecurity"`
	SMTPAuthMechanism        account.AuthMechanism `json:"smtpAuthMechanism"`
	NoOutgoingServer         bool                  `json:"noOutgoingServer"`
	SMTPUsername             string                `json:"smtpUsername"`
	ReplyForwardIdentityID   string                `json:"replyForwardIdentityId"`
	AuthType                 account.AuthType      `json:"authType"`
	Username                 string                `json:"username"`
	Enabled                  bool                  `json:"enabled"`
	OrderIndex               int                   `json:"orderIndex"`
	Color                    string                `json:"color"`
	SyncPeriodDays           int                   `json:"syncPeriodDays"`
	SyncInterval             int                   `json:"syncInterval"`
	SyncAllFolders           bool                  `json:"syncAllFolders"`
	SyncFoldersEnabled       bool                  `json:"syncFoldersEnabled"`
	ReadReceiptRequestPolicy string                `json:"readReceiptRequestPolicy"`
	SentFolderPath           string                `json:"sentFolderPath,omitempty"`
	DraftsFolderPath         string                `json:"draftsFolderPath,omitempty"`
	TrashFolderPath          string                `json:"trashFolderPath,omitempty"`
	SpamFolderPath           string                `json:"spamFolderPath,omitempty"`
	ArchiveFolderPath        string                `json:"archiveFolderPath,omitempty"`
	AllMailFolderPath        string                `json:"allMailFolderPath,omitempty"`
	StarredFolderPath        string                `json:"starredFolderPath,omitempty"`
	CreatedAt                time.Time             `json:"createdAt"`
	UpdatedAt                time.Time             `json:"updatedAt"`
}

// ToAccount maps the public non-secret account representation to the desktop's existing model.
func (a AccountSummary) ToAccount() *account.Account {
	return &account.Account{
		ID: a.ID, Email: a.Email, Name: a.Name, SharedMailboxParentID: a.SharedMailboxParentID,
		IMAPHost: a.IMAPHost, IMAPPort: a.IMAPPort, IMAPSecurity: a.IMAPSecurity, IMAPAuthMechanism: a.IMAPAuthMechanism,
		SMTPHost: a.SMTPHost, SMTPPort: a.SMTPPort, SMTPSecurity: a.SMTPSecurity, SMTPAuthMechanism: a.SMTPAuthMechanism,
		NoOutgoingServer: a.NoOutgoingServer, SMTPUsername: a.SMTPUsername, ReplyForwardIdentityID: a.ReplyForwardIdentityID,
		AuthType: a.AuthType, Username: a.Username, Enabled: a.Enabled, OrderIndex: a.OrderIndex, Color: a.Color,
		SyncPeriodDays: a.SyncPeriodDays, SyncInterval: a.SyncInterval, SyncAllFolders: a.SyncAllFolders,
		SyncFoldersEnabled: a.SyncFoldersEnabled, ReadReceiptRequestPolicy: a.ReadReceiptRequestPolicy,
		SentFolderPath: a.SentFolderPath, DraftsFolderPath: a.DraftsFolderPath, TrashFolderPath: a.TrashFolderPath,
		SpamFolderPath: a.SpamFolderPath, ArchiveFolderPath: a.ArchiveFolderPath, AllMailFolderPath: a.AllMailFolderPath,
		StarredFolderPath: a.StarredFolderPath, CreatedAt: a.CreatedAt, UpdatedAt: a.UpdatedAt,
	}
}

func accountSummaryFrom(acc *account.Account) AccountSummary {
	if acc == nil {
		return AccountSummary{}
	}
	return AccountSummary{
		ID: acc.ID, Email: acc.Email, Name: acc.Name, SharedMailboxParentID: acc.SharedMailboxParentID,
		IMAPHost: acc.IMAPHost, IMAPPort: acc.IMAPPort, IMAPSecurity: acc.IMAPSecurity, IMAPAuthMechanism: acc.IMAPAuthMechanism,
		SMTPHost: acc.SMTPHost, SMTPPort: acc.SMTPPort, SMTPSecurity: acc.SMTPSecurity, SMTPAuthMechanism: acc.SMTPAuthMechanism,
		NoOutgoingServer: acc.NoOutgoingServer, SMTPUsername: acc.SMTPUsername, ReplyForwardIdentityID: acc.ReplyForwardIdentityID,
		AuthType: acc.AuthType, Username: acc.Username, Enabled: acc.Enabled, OrderIndex: acc.OrderIndex, Color: acc.Color,
		SyncPeriodDays: acc.SyncPeriodDays, SyncInterval: acc.SyncInterval, SyncAllFolders: acc.SyncAllFolders,
		SyncFoldersEnabled: acc.SyncFoldersEnabled, ReadReceiptRequestPolicy: acc.ReadReceiptRequestPolicy,
		SentFolderPath: acc.SentFolderPath, DraftsFolderPath: acc.DraftsFolderPath, TrashFolderPath: acc.TrashFolderPath,
		SpamFolderPath: acc.SpamFolderPath, ArchiveFolderPath: acc.ArchiveFolderPath, AllMailFolderPath: acc.AllMailFolderPath,
		StarredFolderPath: acc.StarredFolderPath, CreatedAt: acc.CreatedAt, UpdatedAt: acc.UpdatedAt,
	}
}

func (h *ReadHandler) handleAccounts(w http.ResponseWriter, r *http.Request) {
	accounts, err := h.catalog.GetAccounts()
	if err != nil {
		h.writeError(w, r, http.StatusInternalServerError, CodeInternal, "Failed to load accounts", true)
		return
	}

	summaries := make([]AccountSummary, 0, len(accounts))
	for _, acc := range accounts {
		summaries = append(summaries, accountSummaryFrom(acc))
	}

	h.writeResponse(w, r, http.StatusOK, summaries, nil)
}

func (h *ReadHandler) handleAccount(w http.ResponseWriter, r *http.Request, accountID string) {
	accounts, err := h.catalog.GetAccounts()
	if err != nil {
		h.writeError(w, r, http.StatusInternalServerError, CodeInternal, "Failed to load account", true)
		return
	}
	for _, acc := range accounts {
		if acc.ID == accountID {
			h.writeResponse(w, r, http.StatusOK, accountSummaryFrom(acc), nil)
			return
		}
	}
	h.writeError(w, r, http.StatusNotFound, CodeNotFound, "Account not found", false)
}

// FolderSummary contains safe folder metadata.
type FolderSummary struct {
	ID              string     `json:"id"`
	AccountID       string     `json:"accountId"`
	Name            string     `json:"name"`
	Path            string     `json:"path"`
	Type            string     `json:"type,omitempty"`
	ParentID        string     `json:"parentId,omitempty"`
	UIDValidity     uint32     `json:"uidValidity"`
	UIDNext         uint32     `json:"uidNext"`
	HighestModSeq   uint64     `json:"highestModSeq"`
	FlagsSyncModSeq uint64     `json:"flagsSyncModSeq"`
	Hidden          bool       `json:"hidden"`
	Subscribed      bool       `json:"subscribed"`
	TotalCount      int        `json:"totalCount"`
	UnreadCount     int        `json:"unreadCount"`
	LastSync        *time.Time `json:"lastSync,omitempty"`
}

func (h *ReadHandler) handleFolders(w http.ResponseWriter, r *http.Request) {
	accountID := r.URL.Query().Get("accountId")
	var accountsToQuery []string

	if accountID != "" {
		accountsToQuery = append(accountsToQuery, accountID)
	} else {
		all, err := h.catalog.GetAccounts()
		if err != nil {
			h.writeError(w, r, http.StatusInternalServerError, CodeInternal, "Failed to query accounts", true)
			return
		}
		for _, acc := range all {
			accountsToQuery = append(accountsToQuery, acc.ID)
		}
	}

	var allFolders []FolderSummary
	for _, accID := range accountsToQuery {
		folders, err := h.catalog.GetFolders(accID)
		if err != nil {
			continue
		}
		for _, f := range folders {
			allFolders = append(allFolders, FolderSummary{
				ID: f.ID, AccountID: f.AccountID, Name: f.Name, Path: f.Path,
				Type: string(f.Type), ParentID: f.ParentID, UIDValidity: f.UIDValidity,
				UIDNext: f.UIDNext, HighestModSeq: f.HighestModSeq, FlagsSyncModSeq: f.FlagsSyncModSeq,
				Subscribed: f.Subscribed, Hidden: f.Hidden,
				TotalCount: f.TotalCount, UnreadCount: f.UnreadCount, LastSync: f.LastSync,
			})
		}
	}

	h.writeResponse(w, r, http.StatusOK, allFolders, nil)
}

// ViewSummary contains saved virtual view metadata.
type ViewSummary struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	OrderIndex  int       `json:"orderIndex"`
	FolderIDs   []string  `json:"folderIds"`
	FolderMode  string    `json:"folderMode"`
	ReadStatus  string    `json:"readStatus"`
	ReplyStatus string    `json:"replyStatus"`
	StarStatus  string    `json:"starStatus"`
	DateWindow  string    `json:"dateWindow"`
	SearchQuery string    `json:"searchQuery,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

func (h *ReadHandler) handleViews(w http.ResponseWriter, r *http.Request) {
	vfs, err := h.catalog.GetVirtualFolders()
	if err != nil {
		h.writeError(w, r, http.StatusInternalServerError, CodeInternal, "Failed to load virtual views", true)
		return
	}

	views := make([]ViewSummary, 0, len(vfs))
	for _, vf := range vfs {
		views = append(views, ViewSummary{
			ID:          vf.ID,
			Name:        vf.Name,
			OrderIndex:  vf.OrderIndex,
			FolderIDs:   vf.FolderIDs,
			FolderMode:  vf.FolderMode,
			ReadStatus:  vf.ReadStatus,
			ReplyStatus: vf.ReplyStatus,
			StarStatus:  vf.StarStatus,
			DateWindow:  vf.DateWindow,
			SearchQuery: vf.SearchQuery,
			CreatedAt:   vf.CreatedAt,
			UpdatedAt:   vf.UpdatedAt,
		})
	}

	h.writeResponse(w, r, http.StatusOK, views, nil)
}

func (h *ReadHandler) handleSearch(w http.ResponseWriter, r *http.Request) {
	var req SearchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, r, http.StatusBadRequest, CodeInvalidRequest, fmt.Sprintf("Malformed request body: %v", err), false)
		return
	}

	normalizedPage, err := req.Page.Normalized()
	if err != nil {
		h.writeError(w, r, http.StatusBadRequest, CodeInvalidRequest, err.Error(), false)
		return
	}

	var res *views.ThreadPage
	if req.Filter == "" {
		res, err = h.search.SearchMessages(r.Context(), req.Scope, req.Scope.Query, normalizedPage.Cursor, normalizedPage.Limit)
	} else if filtered, ok := h.search.(FilteredSearchOperations); ok {
		res, err = filtered.SearchMessagesWithFilter(r.Context(), req.Scope, req.Scope.Query, req.Filter, normalizedPage.Cursor, normalizedPage.Limit)
	} else {
		h.writeError(w, r, http.StatusBadRequest, CodeInvalidRequest, "Search filter is not supported", false)
		return
	}
	if err != nil {
		h.writeError(w, r, http.StatusInternalServerError, CodeInternal, fmt.Sprintf("Search failed: %v", err), true)
		return
	}

	h.writeResponse(w, r, http.StatusOK, res, nil)
}

func (h *ReadHandler) handleThreadRead(w http.ResponseWriter, r *http.Request) {
	var req ThreadReadRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, r, http.StatusBadRequest, CodeInvalidRequest, fmt.Sprintf("Malformed request: %v", err), false)
		return
	}

	if req.Ref.AccountID == "" || req.Ref.ThreadID == "" {
		h.writeError(w, r, http.StatusBadRequest, CodeInvalidRequest, "Thread ref requires accountId and threadId", false)
		return
	}

	threadView, err := h.messages.GetThread(r.Context(), req.Ref, req.Scope)
	if err != nil {
		h.writeError(w, r, http.StatusNotFound, CodeNotFound, fmt.Sprintf("Thread not found: %v", err), false)
		return
	}

	h.writeResponse(w, r, http.StatusOK, threadView, nil)
}

func (h *ReadHandler) handleMessageBody(w http.ResponseWriter, r *http.Request, requireCap func(http.ResponseWriter, *http.Request, Capability) bool) {
	var req BodyReadRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, r, http.StatusBadRequest, CodeInvalidRequest, fmt.Sprintf("Malformed request: %v", err), false)
		return
	}

	if req.Ref.AccountID == "" || req.Ref.MessageID == "" {
		h.writeError(w, r, http.StatusBadRequest, CodeInvalidRequest, "Message ref requires accountId and messageId", false)
		return
	}

	shouldFetch := (req.Mode == FetchExplicit)
	if shouldFetch {
		// Explicit fetch requires mail.body.fetch capability
		if !requireCap(w, r, CapabilityMailBodyFetch) {
			return
		}
	}

	msg, err := h.messages.GetMessage(req.Ref.AccountID, req.Ref.MessageID)
	if err != nil || msg == nil {
		h.writeError(w, r, http.StatusNotFound, CodeNotFound, "Message not found", false)
		return
	}

	if shouldFetch && !msg.BodyFetched {
		fetchedMsg, fetchErr := h.messages.FetchMessageBody(r.Context(), req.Ref.AccountID, req.Ref.MessageID)
		if fetchErr != nil {
			h.writeError(w, r, http.StatusBadGateway, CodeProviderError, fmt.Sprintf("Provider fetch failed: %v", fetchErr), true)
			return
		}
		msg = fetchedMsg
	}

	contentState := views.ContentCached
	if !msg.BodyFetched {
		if msg.BodyFailed {
			contentState = views.ContentFetchFailed
		} else {
			contentState = views.ContentNotFetched
		}
	}

	resp := MessageBody{
		Ref:          req.Ref,
		ContentState: contentState,
	}

	if contentState == views.ContentCached {
		reply, err := h.messages.GetReplyContent(req.Ref.AccountID, req.Ref.MessageID, "automation", false)
		if err == nil && reply != nil {
			resp.Extracted = &reply.Result
		}

		if req.IncludeOriginal {
			resp.Original = &OriginalBody{
				Text: msg.BodyText,
				HTML: msg.BodyHTML,
			}
		}
	}

	h.writeResponse(w, r, http.StatusOK, resp, nil)
}

func (h *ReadHandler) handleAttachmentList(w http.ResponseWriter, r *http.Request) {
	var req AttachmentListRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, r, http.StatusBadRequest, CodeInvalidRequest, fmt.Sprintf("Malformed request: %v", err), false)
		return
	}

	if req.Ref.AccountID == "" || req.Ref.MessageID == "" {
		h.writeError(w, r, http.StatusBadRequest, CodeInvalidRequest, "Message ref requires accountId and messageId", false)
		return
	}
	msg, err := h.messages.GetMessage(req.Ref.AccountID, req.Ref.MessageID)
	if err != nil || msg == nil || msg.AccountID != req.Ref.AccountID || msg.ID != req.Ref.MessageID {
		h.writeError(w, r, http.StatusNotFound, CodeNotFound, "Message not found", false)
		return
	}

	atts, err := h.attachments.GetAttachments(req.Ref.MessageID)
	if err != nil {
		h.writeError(w, r, http.StatusInternalServerError, CodeInternal, "Failed to retrieve attachments", true)
		return
	}

	items := make([]AttachmentSummary, 0, len(atts))
	for _, a := range atts {
		items = append(items, AttachmentSummary{
			ID:          a.ID,
			Filename:    a.Filename,
			ContentType: a.ContentType,
			Size:        a.Size,
			ContentID:   a.ContentID,
			IsInline:    a.IsInline,
		})
	}

	page := Page[AttachmentSummary]{
		Items:    items,
		Complete: true,
	}

	h.writeResponse(w, r, http.StatusOK, page, nil)
}

func (h *ReadHandler) handleAttachmentRead(w http.ResponseWriter, r *http.Request) {
	var req AttachmentReadRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, r, http.StatusBadRequest, CodeInvalidRequest, fmt.Sprintf("Malformed request: %v", err), false)
		return
	}

	if req.Ref.AccountID == "" || req.Ref.MessageID == "" || req.AttachmentID == "" {
		h.writeError(w, r, http.StatusBadRequest, CodeInvalidRequest, "Requires accountId, messageId, and attachmentId", false)
		return
	}
	msg, err := h.messages.GetMessage(req.Ref.AccountID, req.Ref.MessageID)
	if err != nil || msg == nil || msg.AccountID != req.Ref.AccountID || msg.ID != req.Ref.MessageID {
		h.writeError(w, r, http.StatusNotFound, CodeNotFound, "Message not found", false)
		return
	}

	data, contentType, err := h.attachments.ReadAttachmentBytes(req.Ref.AccountID, req.Ref.MessageID, req.AttachmentID)
	if err != nil {
		h.writeError(w, r, http.StatusNotFound, CodeNotFound, fmt.Sprintf("Attachment read failed: %v", err), false)
		return
	}

	if contentType == "" {
		contentType = "application/octet-stream"
	}

	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func (h *ReadHandler) writeResponse(w http.ResponseWriter, r *http.Request, statusCode int, data any, scopedErrors []ScopedError) {
	reqID := r.Header.Get("X-Request-Id")
	if reqID == "" {
		reqID = "req-unknown"
	}

	resp := Response[any]{
		Data:      data,
		Errors:    scopedErrors,
		RequestID: reqID,
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(resp)
}

func (h *ReadHandler) writeError(w http.ResponseWriter, r *http.Request, statusCode int, code ErrorCode, message string, retryable bool) {
	reqID := r.Header.Get("X-Request-Id")
	if reqID == "" {
		reqID = "req-unknown"
	}
	resp := ErrorResponse{
		Error: ErrorBody{
			Code:      code,
			Message:   message,
			Retryable: retryable,
			RequestID: reqID,
		},
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(resp)
}

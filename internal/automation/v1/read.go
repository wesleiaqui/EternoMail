package v1

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/hkdb/aerion/internal/account"
	"github.com/hkdb/aerion/internal/email/replycontent"
	"github.com/hkdb/aerion/internal/folder"
	"github.com/hkdb/aerion/internal/message"
	"github.com/hkdb/aerion/internal/views"
	"github.com/hkdb/aerion/internal/virtualfolder"
)

// ReadOperations defines backend application capabilities required by read endpoints.
type ReadOperations interface {
	GetAccounts() ([]*account.Account, error)
	GetFolders(accountID string) ([]*folder.Folder, error)
	GetVirtualFolders() ([]*virtualfolder.VirtualFolder, error)
	SearchMessages(ctx context.Context, scope views.Scope, query string, cursor string, limit int) (*views.ThreadPage, error)
	GetThread(ctx context.Context, ref views.ThreadRef, scope views.Scope) (*views.ThreadView, error)
	GetMessage(accountID, messageID string) (*message.Message, error)
	GetReplyContent(accountID, messageID, channel string, fetch bool) (*replycontent.Reply, error)
	FetchMessageBody(ctx context.Context, accountID, messageID string) (*message.Message, error)
	GetAttachments(messageID string) ([]*message.Attachment, error)
	ReadAttachmentBytes(accountID, messageID, attachmentID string) ([]byte, string, error)
}

// ReadHandler routes and handles read/search/thread/body/attachment requests.
type ReadHandler struct {
	ops ReadOperations
}

// NewReadHandler creates a ReadHandler backed by the provided operations.
func NewReadHandler(ops ReadOperations) *ReadHandler {
	return &ReadHandler{ops: ops}
}

// MountRoutes mounts read API endpoints onto a standard http.ServeMux.
func (h *ReadHandler) MountRoutes(mux *http.ServeMux, requireCap func(http.ResponseWriter, *http.Request, Capability) bool) {
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

// AccountSummary contains safe account metadata for agent consumption.
type AccountSummary struct {
	ID    string `json:"id"`
	Email string `json:"email"`
	Name  string `json:"name"`
}

func (h *ReadHandler) handleAccounts(w http.ResponseWriter, r *http.Request) {
	accounts, err := h.ops.GetAccounts()
	if err != nil {
		h.writeError(w, r, http.StatusInternalServerError, CodeInternal, "Failed to load accounts", true)
		return
	}

	summaries := make([]AccountSummary, 0, len(accounts))
	for _, acc := range accounts {
		summaries = append(summaries, AccountSummary{
			ID:    acc.ID,
			Email: acc.Email,
			Name:  acc.Name,
		})
	}

	h.writeResponse(w, r, http.StatusOK, summaries, nil)
}

// FolderSummary contains safe folder metadata.
type FolderSummary struct {
	ID        string `json:"id"`
	AccountID string `json:"accountId"`
	Name      string `json:"name"`
	Path      string `json:"path"`
	Type      string `json:"type,omitempty"`
}

func (h *ReadHandler) handleFolders(w http.ResponseWriter, r *http.Request) {
	accountID := r.URL.Query().Get("accountId")
	var accountsToQuery []string

	if accountID != "" {
		accountsToQuery = append(accountsToQuery, accountID)
	} else {
		all, err := h.ops.GetAccounts()
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
		folders, err := h.ops.GetFolders(accID)
		if err != nil {
			continue
		}
		for _, f := range folders {
			allFolders = append(allFolders, FolderSummary{
				ID:        f.ID,
				AccountID: f.AccountID,
				Name:      f.Name,
				Path:      f.Path,
				Type:      string(f.Type),
			})
		}
	}

	h.writeResponse(w, r, http.StatusOK, allFolders, nil)
}

// ViewSummary contains saved virtual view metadata.
type ViewSummary struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	SearchQuery string `json:"searchQuery,omitempty"`
}

func (h *ReadHandler) handleViews(w http.ResponseWriter, r *http.Request) {
	vfs, err := h.ops.GetVirtualFolders()
	if err != nil {
		h.writeError(w, r, http.StatusInternalServerError, CodeInternal, "Failed to load virtual views", true)
		return
	}

	views := make([]ViewSummary, 0, len(vfs))
	for _, vf := range vfs {
		views = append(views, ViewSummary{
			ID:          vf.ID,
			Name:        vf.Name,
			SearchQuery: vf.SearchQuery,
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

	res, err := h.ops.SearchMessages(r.Context(), req.Scope, req.Scope.Query, normalizedPage.Cursor, normalizedPage.Limit)
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

	threadView, err := h.ops.GetThread(r.Context(), req.Ref, req.Scope)
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

	msg, err := h.ops.GetMessage(req.Ref.AccountID, req.Ref.MessageID)
	if err != nil || msg == nil {
		h.writeError(w, r, http.StatusNotFound, CodeNotFound, "Message not found", false)
		return
	}

	if shouldFetch && !msg.BodyFetched {
		fetchedMsg, fetchErr := h.ops.FetchMessageBody(r.Context(), req.Ref.AccountID, req.Ref.MessageID)
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
		reply, err := h.ops.GetReplyContent(req.Ref.AccountID, req.Ref.MessageID, "automation", false)
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

	atts, err := h.ops.GetAttachments(req.Ref.MessageID)
	if err != nil {
		h.writeError(w, r, http.StatusInternalServerError, CodeInternal, "Failed to retrieve attachments", true)
		return
	}

	type AttachmentSummary struct {
		ID          string `json:"id"`
		Filename    string `json:"filename"`
		ContentType string `json:"contentType"`
		Size        int    `json:"size"`
		IsInline    bool   `json:"isInline"`
	}

	items := make([]AttachmentSummary, 0, len(atts))
	for _, a := range atts {
		items = append(items, AttachmentSummary{
			ID:          a.ID,
			Filename:    a.Filename,
			ContentType: a.ContentType,
			Size:        a.Size,
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

	data, contentType, err := h.ops.ReadAttachmentBytes(req.Ref.AccountID, req.Ref.MessageID, req.AttachmentID)
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

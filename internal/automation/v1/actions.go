package v1

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/hkdb/aerion/internal/operations"
)

// ActionKind represents a valid mail action type.
type ActionKind string

const (
	ActionArchive    ActionKind = "archive"
	ActionMarkRead   ActionKind = "mark_read"
	ActionMarkUnread ActionKind = "mark_unread"
	ActionStar       ActionKind = "star"
	ActionUnstar     ActionKind = "unstar"
	ActionTrash      ActionKind = "trash"
	ActionMove       ActionKind = "move"
)

// ActionRequest specifies a mutation on an explicit target.
type ActionRequest struct {
	Action      ActionKind        `json:"action"`
	Target      operations.Target `json:"target"`
	Destination string            `json:"destination,omitempty"` // Required for "move"
}

// ActionResponse describes the outcome and provides an operation receipt.
type ActionResponse struct {
	Receipt    OperationReceipt `json:"receipt"`
	MessageIDs []string         `json:"messageIds"`
}

// ActionOperations defines the mutations supported by the backend mail service.
type ActionOperations interface {
	operations.MessageLookup
	ArchiveMessages(messageIDs []string) (string, error)
	MarkAsRead(messageIDs []string) error
	MarkAsUnread(messageIDs []string) error
	StarMessages(messageIDs []string) error
	UnstarMessages(messageIDs []string) error
	TrashMessages(messageIDs []string) error
	MoveMessages(messageIDs []string, destinationFolderID string) (string, error)
}

// ActionsHandler executes mail mutations with idempotency checking and target validation.
type ActionsHandler struct {
	ops             ActionOperations
	mu              sync.Mutex
	idempotencyKeys map[IdempotencyKey]*ActionResponse
}

// NewActionsHandler initializes an ActionsHandler.
func NewActionsHandler(ops ActionOperations) *ActionsHandler {
	return &ActionsHandler{
		ops:             ops,
		idempotencyKeys: make(map[IdempotencyKey]*ActionResponse),
	}
}

// MountRoutes mounts the action endpoints onto a standard ServeMux.
func (h *ActionsHandler) MountRoutes(mux *http.ServeMux, requireCap func(http.ResponseWriter, *http.Request, Capability) bool) {
	mux.HandleFunc("/v1/actions", func(w http.ResponseWriter, r *http.Request) {
		if !requireCap(w, r, CapabilityMailWrite) {
			return
		}
		if r.Method != http.MethodPost {
			h.writeError(w, r, http.StatusMethodNotAllowed, CodeInvalidRequest, "Method not allowed", false)
			return
		}
		h.handleAction(w, r)
	})
}

func (h *ActionsHandler) handleAction(w http.ResponseWriter, r *http.Request) {
	idempotencyKey := IdempotencyKey(strings.TrimSpace(r.Header.Get("Idempotency-Key")))
	if idempotencyKey == "" {
		h.writeError(w, r, http.StatusBadRequest, CodeInvalidRequest, "Idempotency-Key header is required for mutations", false)
		return
	}

	// Check existing idempotency record
	h.mu.Lock()
	if existing, ok := h.idempotencyKeys[idempotencyKey]; ok {
		h.mu.Unlock()
		replayedResp := *existing
		replayedResp.Receipt.Replayed = true
		h.writeResponse(w, r, http.StatusOK, replayedResp)
		return
	}
	h.mu.Unlock()

	var req ActionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, r, http.StatusBadRequest, CodeInvalidRequest, fmt.Sprintf("Malformed request: %v", err), false)
		return
	}

	messageIDs, err := operations.Resolve(req.Target, h.ops)
	if err != nil {
		h.writeError(w, r, http.StatusBadRequest, CodeInvalidRequest, fmt.Sprintf("Target resolution failed: %v", err), false)
		return
	}

	operationID := generateOpID()
	var opErr error

	switch req.Action {
	case ActionArchive:
		id, err := h.ops.ArchiveMessages(messageIDs)
		if id != "" {
			operationID = id
		}
		opErr = err

	case ActionMarkRead:
		opErr = h.ops.MarkAsRead(messageIDs)

	case ActionMarkUnread:
		opErr = h.ops.MarkAsUnread(messageIDs)

	case ActionStar:
		opErr = h.ops.StarMessages(messageIDs)

	case ActionUnstar:
		opErr = h.ops.UnstarMessages(messageIDs)

	case ActionTrash:
		opErr = h.ops.TrashMessages(messageIDs)

	case ActionMove:
		if strings.TrimSpace(req.Destination) == "" {
			h.writeError(w, r, http.StatusBadRequest, CodeInvalidRequest, "Destination folder ID required for move action", false)
			return
		}
		id, err := h.ops.MoveMessages(messageIDs, req.Destination)
		if id != "" {
			operationID = id
		}
		opErr = err

	default:
		h.writeError(w, r, http.StatusBadRequest, CodeInvalidRequest, fmt.Sprintf("Unknown action %q", req.Action), false)
		return
	}

	if opErr != nil {
		h.writeError(w, r, http.StatusInternalServerError, CodeInternal, fmt.Sprintf("Action failed: %v", opErr), true)
		return
	}

	resp := ActionResponse{
		Receipt: OperationReceipt{
			OperationID: operationID,
			State:       OperationSent,
			Replayed:    false,
		},
		MessageIDs: messageIDs,
	}

	h.mu.Lock()
	h.idempotencyKeys[idempotencyKey] = &resp
	h.mu.Unlock()

	h.writeResponse(w, r, http.StatusOK, resp)
}

func (h *ActionsHandler) writeResponse(w http.ResponseWriter, r *http.Request, statusCode int, data any) {
	reqID := r.Header.Get("X-Request-Id")
	if reqID == "" {
		reqID = "req-unknown"
	}
	resp := Response[any]{
		Data:      data,
		RequestID: reqID,
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(resp)
}

func (h *ActionsHandler) writeError(w http.ResponseWriter, r *http.Request, statusCode int, code ErrorCode, message string, retryable bool) {
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

func generateOpID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return fmt.Sprintf("op-%d-%s", time.Now().Unix(), hex.EncodeToString(b))
}

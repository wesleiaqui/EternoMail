package v1

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
)

var errTriageReceiptAlreadyExists = errors.New("triage receipt already exists")

// TriageMutationRequest is the canonical, account-qualified operation passed
// to the helper's triage owner. It is constructed from the route-specific DTOs.
type TriageMutationRequest struct {
	UnifiedLater   bool `json:",omitempty"`
	Action         ActionKind
	AccountID      string
	ConversationID string
	InQueue        bool
	WakeAt         time.Time
}

// TriageMutationOperations serializes a state transition, asks reserve to
// persist the operation before effects, then returns its private undo snapshot.
type TriageMutationOperations interface {
	ApplyTriageMutation(context.Context, TriageMutationRequest, func([]byte) error) (bool, []byte, error)
}

// TriageActionReceiptStore atomically persists account-qualified queue refs
// with the operation reservation, so interrupted provider effects remain
// targeted and recoverable after restart.
type TriageActionReceiptStore interface {
	ReserveTriage(context.Context, string, string, string, ActionKind, int, []AffectedTriageRef) (ActionReceiptRecord, bool, error)
}

// TriageMutationsHandler executes helper-owned queue changes with durable
// idempotency and snapshots stored in the shared action receipt ledger.
type TriageMutationsHandler struct {
	ops      TriageMutationOperations
	receipts ActionReceiptStore
	digest   func(string) string
}

// NewTriageMutationsHandler creates the durable triage mutation API adapter.
func NewTriageMutationsHandler(ops TriageMutationOperations, receipts ActionReceiptStore, keyDigest func(string) string) *TriageMutationsHandler {
	if keyDigest == nil {
		keyDigest = func(key string) string {
			digest := sha256.Sum256([]byte(key))
			return hex.EncodeToString(digest[:])
		}
	}
	return &TriageMutationsHandler{ops: ops, receipts: receipts, digest: keyDigest}
}

// MountRoutes mounts all triage mutation paths with mail.write authorization.
func (h *TriageMutationsHandler) MountRoutes(mux *http.ServeMux, requireCap func(http.ResponseWriter, *http.Request, Capability) bool) {
	mux.HandleFunc("/v1/reply-later/mutations", func(w http.ResponseWriter, r *http.Request) {
		if !requireCap(w, r, CapabilityMailWrite) {
			return
		}
		if r.Method != http.MethodPost {
			writeTriageMutationError(w, r, http.StatusMethodNotAllowed, CodeInvalidRequest, "Method not allowed", false)
			return
		}
		var body ReplyLaterMutationRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeTriageMutationError(w, r, http.StatusBadRequest, CodeInvalidRequest, "Malformed Reply Later request", false)
			return
		}
		action := ActionReplyLaterRemove
		if body.InQueue {
			action = ActionReplyLaterAdd
		}
		h.handle(w, r, TriageMutationRequest{Action: action, AccountID: body.Ref.AccountID, ConversationID: body.Ref.ConversationID, InQueue: body.InQueue})
	})
	mux.HandleFunc("/v1/triage/read-later/mutations", func(w http.ResponseWriter, r *http.Request) {
		if !requireCap(w, r, CapabilityMailWrite) {
			return
		}
		if r.Method != http.MethodPost {
			writeTriageMutationError(w, r, http.StatusMethodNotAllowed, CodeInvalidRequest, "Method not allowed", false)
			return
		}
		var body ReadLaterMutationRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeTriageMutationError(w, r, http.StatusBadRequest, CodeInvalidRequest, "Malformed Read Later request", false)
			return
		}
		action := ActionReadLaterRemove
		if body.InQueue {
			action = ActionReadLaterAdd
		}
		h.handle(w, r, TriageMutationRequest{Action: action, AccountID: body.Ref.AccountID, ConversationID: body.Ref.ConversationID, InQueue: body.InQueue})
	})
	mux.HandleFunc("/v1/triage/later/mutations", func(w http.ResponseWriter, r *http.Request) {
		if !requireCap(w, r, CapabilityMailWrite) {
			return
		}
		if r.Method != http.MethodPost {
			writeTriageMutationError(w, r, http.StatusMethodNotAllowed, CodeInvalidRequest, "Method not allowed", false)
			return
		}
		var body ReadLaterMutationRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeTriageMutationError(w, r, http.StatusBadRequest, CodeInvalidRequest, "Malformed Read Later request", false)
			return
		}
		action := ActionReadLaterRemove
		if body.InQueue {
			action = ActionReadLaterAdd
		}
		h.handle(w, r, TriageMutationRequest{UnifiedLater: true, Action: action, AccountID: body.Ref.AccountID, ConversationID: body.Ref.ConversationID, InQueue: body.InQueue})
	})
	mux.HandleFunc("/v1/triage/snoozes/mutations", func(w http.ResponseWriter, r *http.Request) {
		if !requireCap(w, r, CapabilityMailWrite) {
			return
		}
		if r.Method != http.MethodPost {
			writeTriageMutationError(w, r, http.StatusMethodNotAllowed, CodeInvalidRequest, "Method not allowed", false)
			return
		}
		var body SnoozeMutationRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeTriageMutationError(w, r, http.StatusBadRequest, CodeInvalidRequest, "Malformed Snooze request", false)
			return
		}
		request := TriageMutationRequest{AccountID: body.Ref.AccountID, ConversationID: body.Ref.ConversationID}
		switch body.Operation {
		case "snooze":
			wakeAt, err := time.Parse(time.RFC3339Nano, body.WakeAt)
			if err != nil || !wakeAt.After(time.Now()) {
				writeTriageMutationError(w, r, http.StatusBadRequest, CodeInvalidRequest, "Snooze deadline must be a future RFC3339 timestamp", false)
				return
			}
			request.Action = ActionSnooze
			request.WakeAt = wakeAt.UTC()
		case "unsnooze":
			request.Action = ActionUnsnooze
		default:
			writeTriageMutationError(w, r, http.StatusBadRequest, CodeInvalidRequest, "Unknown Snooze operation", false)
			return
		}
		h.handle(w, r, request)
	})
}

func (h *TriageMutationsHandler) handle(w http.ResponseWriter, r *http.Request, request TriageMutationRequest) {
	if h == nil || h.ops == nil || h.receipts == nil {
		writeTriageMutationError(w, r, http.StatusInternalServerError, CodeInternal, "Triage mutation service is unavailable", true)
		return
	}
	request.AccountID = strings.TrimSpace(request.AccountID)
	request.ConversationID = strings.TrimSpace(request.ConversationID)
	if request.AccountID == "" || request.ConversationID == "" {
		writeTriageMutationError(w, r, http.StatusBadRequest, CodeInvalidRequest, "Account and conversation IDs are required", false)
		return
	}
	if !request.WakeAt.IsZero() {
		request.WakeAt = request.WakeAt.UTC().Round(0)
	}
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if key == "" {
		writeTriageMutationError(w, r, http.StatusBadRequest, CodeInvalidRequest, "Idempotency-Key header is required for mutations", false)
		return
	}
	requestJSON, err := json.Marshal(request)
	if err != nil {
		writeTriageMutationError(w, r, http.StatusBadRequest, CodeInvalidRequest, "Malformed triage request", false)
		return
	}
	requestDigest := sha256.Sum256(requestJSON)
	requestHash := hex.EncodeToString(requestDigest[:])
	keyHash := h.digest(key)
	if stored, found, err := h.receipts.Lookup(r.Context(), keyHash); err != nil {
		writeTriageMutationError(w, r, http.StatusInternalServerError, CodeInternal, "Could not read triage receipt", true)
		return
	} else if found {
		h.writeStored(w, r, stored, requestHash)
		return
	}

	operationID := generateOpID()
	reserved := false
	reserve := func(snapshot []byte) error {
		var stored ActionReceiptRecord
		var owner bool
		var err error
		if triageReceipts, ok := h.receipts.(TriageActionReceiptStore); ok {
			stored, owner, err = triageReceipts.ReserveTriage(r.Context(), keyHash, requestHash, operationID, request.Action, 1, triageMutationRefs(request, false))
		} else {
			stored, owner, err = h.receipts.Reserve(r.Context(), keyHash, requestHash, operationID, request.Action, 1)
		}
		if err != nil {
			return err
		}
		if !owner {
			if stored.RequestHash != requestHash {
				return ErrActionReceiptConflict
			}
			return errTriageReceiptAlreadyExists
		}
		reserved = true
		return nil
	}
	changed, snapshot, applyErr := h.ops.ApplyTriageMutation(r.Context(), request, reserve)
	if errors.Is(applyErr, errTriageReceiptAlreadyExists) {
		stored, found, lookupErr := h.receipts.Lookup(r.Context(), keyHash)
		if lookupErr != nil || !found {
			writeTriageMutationError(w, r, http.StatusInternalServerError, CodeInternal, "Could not read triage receipt", true)
			return
		}
		h.writeStored(w, r, stored, requestHash)
		return
	}
	if applyErr != nil && !reserved {
		if errors.Is(applyErr, ErrActionReceiptConflict) {
			writeTriageMutationError(w, r, http.StatusConflict, CodeIdempotencyConflict, "Idempotency-Key was already used for a different triage request", false)
			return
		}
		writeTriageMutationError(w, r, http.StatusBadRequest, CodeInvalidRequest, applyErr.Error(), false)
		return
	}

	state := OperationSent
	undoPayload := snapshot
	if applyErr != nil {
		state = OperationUnknown
		undoPayload = nil
	}
	response := TriageMutationResponse{
		Changed: changed && state == OperationSent,
		Receipt: OperationReceipt{OperationID: operationID, State: state},
	}
	if state == OperationSent && !changed {
		response.AffectedTriage = nil
	} else {
		response.AffectedTriage = triageMutationRefs(request, state == OperationSent)
	}
	actionKind := request.Action
	if !response.Changed || state != OperationSent {
		undoPayload = nil
	}
	if !response.Changed && state == OperationSent {
		// Keep the idempotency receipt, but omit it from user Undo history.
		actionKind = ""
	}
	if len(undoPayload) > 0 && state == OperationSent {
		response.UndoAvailable = true
		response.UndoExpiresAt = time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339)
	}
	encoded, err := json.Marshal(response)
	if err != nil {
		writeTriageMutationError(w, r, http.StatusInternalServerError, CodeInternal, "Could not encode triage receipt", false)
		return
	}
	completeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := h.receipts.Complete(completeCtx, keyHash, requestHash, actionKind, state, http.StatusOK, encoded, undoPayload); err != nil {
		writeTriageMutationError(w, r, http.StatusInternalServerError, CodeInternal, "Triage mutation completed but its receipt could not be saved", true)
		return
	}
	writeTriageMutationResponse(w, r, response)
}

func triageMutationRef(request TriageMutationRequest, known bool) AffectedTriageRef {
	ref := AffectedTriageRef{AccountID: request.AccountID, ConversationID: request.ConversationID, Known: known}
	switch request.Action {
	case ActionReplyLaterAdd, ActionReplyLaterRemove:
		ref.Queue = "replyLater"
		ref.InQueue = request.InQueue
	case ActionReadLaterAdd, ActionReadLaterRemove:
		ref.Queue = "readLater"
		ref.InQueue = request.InQueue
	case ActionSnooze:
		ref.Queue = "snoozed"
		ref.InQueue = true
		ref.WakeAt = request.WakeAt.UTC().Format(time.RFC3339Nano)
	case ActionUnsnooze:
		ref.Queue = "snoozed"
		ref.InQueue = false
	}
	return ref
}

func (h *TriageMutationsHandler) writeStored(w http.ResponseWriter, r *http.Request, stored ActionReceiptRecord, requestHash string) {
	if stored.RequestHash != requestHash {
		writeTriageMutationError(w, r, http.StatusConflict, CodeIdempotencyConflict, "Idempotency-Key was already used for a different triage request", false)
		return
	}
	if len(stored.Response) > 0 {
		var response TriageMutationResponse
		if err := json.Unmarshal(stored.Response, &response); err != nil {
			writeTriageMutationError(w, r, http.StatusInternalServerError, CodeInternal, "Stored triage receipt is unreadable", false)
			return
		}
		response.Receipt.Replayed = true
		writeTriageMutationResponse(w, r, response)
		return
	}
	state := OperationUnknown
	if stored.State == "processing" {
		state = OperationSending
	}
	writeTriageMutationResponse(w, r, TriageMutationResponse{
		Receipt: OperationReceipt{OperationID: stored.OperationID, State: state, Replayed: true},
	})
}

func writeTriageMutationResponse(w http.ResponseWriter, r *http.Request, data TriageMutationResponse) {
	requestID := r.Header.Get("X-Request-Id")
	if requestID == "" {
		requestID = "req-unknown"
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(Response[TriageMutationResponse]{Data: data, RequestID: requestID})
}

func writeTriageMutationError(w http.ResponseWriter, r *http.Request, status int, code ErrorCode, message string, retryable bool) {
	requestID := r.Header.Get("X-Request-Id")
	if requestID == "" {
		requestID = "req-unknown"
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(ErrorResponse{Error: ErrorBody{Code: code, Message: message, Retryable: retryable, RequestID: requestID}})
}

func triageMutationRefs(request TriageMutationRequest, known bool) []AffectedTriageRef {
	ref := triageMutationRef(request, known)
	refs := []AffectedTriageRef{ref}
	if request.UnifiedLater {
		ref.Queue = "replyLater"
		ref.InQueue = false
		refs = append(refs, ref)
		if request.InQueue { // Add does not rewrite an existing legacy mark; reload authoritative sets.
			refs[0].Known = false
			refs[1].Known = false
		}
	}
	return refs
}

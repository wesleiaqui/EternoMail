package v1_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	v1 "github.com/hkdb/aerion/internal/automation/v1"
	"github.com/hkdb/aerion/internal/message"
	"github.com/hkdb/aerion/internal/operations"
)

type mockActionOperations struct {
	messages           map[string]*message.Message
	threadMap          map[string][]string
	archived           []string
	markedRead         []string
	moved              []string
	destination        string
	archiveOperationID string
}

func (m *mockActionOperations) GetByIDs(ids []string) ([]*message.Message, error) {
	var res []*message.Message
	for _, id := range ids {
		if msg, ok := m.messages[id]; ok {
			res = append(res, msg)
		}
	}
	return res, nil
}

func (m *mockActionOperations) GetMessageIDsInThread(accountID, threadID string) ([]string, error) {
	return m.threadMap[accountID+":"+threadID], nil
}

func (m *mockActionOperations) ArchiveMessages(messageIDs []string) (string, error) {
	m.archived = append(m.archived, messageIDs...)
	return m.archiveOperationID, nil
}

func (m *mockActionOperations) MarkAsRead(messageIDs []string) error {
	m.markedRead = append(m.markedRead, messageIDs...)
	return nil
}

func (m *mockActionOperations) MarkAsUnread(messageIDs []string) error {
	return nil
}

func (m *mockActionOperations) StarMessages(messageIDs []string) error {
	return nil
}

func (m *mockActionOperations) UnstarMessages(messageIDs []string) error {
	return nil
}

func (m *mockActionOperations) TrashMessages(messageIDs []string) error {
	return nil
}

func (m *mockActionOperations) MoveMessages(messageIDs []string, destinationFolderID string) (string, error) {
	m.moved = append(m.moved, messageIDs...)
	m.destination = destinationFolderID
	return "op-move-123", nil
}

func setupActionsTestServer(ops v1.ActionOperations, grantedCaps map[v1.Capability]bool) (*http.ServeMux, *mockActionOperations) {
	mux := http.NewServeMux()
	handler := v1.NewActionsHandler(ops)

	requireCap := func(w http.ResponseWriter, r *http.Request, cap v1.Capability) bool {
		if !grantedCaps[cap] {
			w.WriteHeader(http.StatusForbidden)
			_ = json.NewEncoder(w).Encode(v1.ErrorResponse{
				Error: v1.ErrorBody{
					Code:    v1.CodeCapabilityDenied,
					Message: "Capability denied",
				},
			})
			return false
		}
		return true
	}

	handler.MountRoutes(mux, requireCap)
	return mux, ops.(*mockActionOperations)
}

func TestActionsHandler_CapabilityCheck(t *testing.T) {
	ops := &mockActionOperations{
		messages: map[string]*message.Message{
			"msg-1": {ID: "msg-1", AccountID: "acc-1"},
		},
	}
	// Without mail.write capability
	mux, _ := setupActionsTestServer(ops, map[v1.Capability]bool{v1.CapabilityMailRead: true})

	body := v1.ActionRequest{
		Action: v1.ActionArchive,
		Target: operations.Target{
			Kind: operations.TargetMessage,
			Messages: []operations.MessageRef{
				{AccountID: "acc-1", MessageID: "msg-1"},
			},
		},
	}
	payload, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/actions", bytes.NewReader(payload))
	req.Header.Set("Idempotency-Key", "key-1")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 Forbidden without mail.write, got %d", rec.Code)
	}
}

func TestActionsHandler_IdempotencyAndReplay(t *testing.T) {
	ops := &mockActionOperations{
		archiveOperationID: "op-arch-001",
		messages: map[string]*message.Message{
			"msg-1": {ID: "msg-1", AccountID: "acc-1"},
		},
	}
	mux, _ := setupActionsTestServer(ops, map[v1.Capability]bool{v1.CapabilityMailWrite: true})

	body := v1.ActionRequest{
		Action: v1.ActionArchive,
		Target: operations.Target{
			Kind: operations.TargetMessage,
			Messages: []operations.MessageRef{
				{AccountID: "acc-1", MessageID: "msg-1"},
			},
		},
	}
	payload, _ := json.Marshal(body)

	// 1. Missing Idempotency-Key
	req := httptest.NewRequest(http.MethodPost, "/v1/actions", bytes.NewReader(payload))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 Bad Request on missing Idempotency-Key, got %d", rec.Code)
	}

	// 2. Initial request with Idempotency-Key
	req = httptest.NewRequest(http.MethodPost, "/v1/actions", bytes.NewReader(payload))
	req.Header.Set("Idempotency-Key", "idem-key-100")
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", rec.Code)
	}

	var resp1 v1.Response[v1.ActionResponse]
	if err := json.NewDecoder(rec.Body).Decode(&resp1); err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if resp1.Data.Receipt.Replayed {
		t.Errorf("first request should not be replayed")
	}
	if resp1.Data.Receipt.OperationID != "op-arch-001" {
		t.Errorf("expected operationID op-arch-001, got %s", resp1.Data.Receipt.OperationID)
	}

	// 3. Replay with identical Idempotency-Key
	req = httptest.NewRequest(http.MethodPost, "/v1/actions", bytes.NewReader(payload))
	req.Header.Set("Idempotency-Key", "idem-key-100")
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK on replay, got %d", rec.Code)
	}

	var resp2 v1.Response[v1.ActionResponse]
	if err := json.NewDecoder(rec.Body).Decode(&resp2); err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if !resp2.Data.Receipt.Replayed {
		t.Errorf("second request with same key should be marked replayed")
	}
	if resp2.Data.Receipt.OperationID != "op-arch-001" {
		t.Errorf("expected operationID op-arch-001 on replay, got %s", resp2.Data.Receipt.OperationID)
	}
}

func TestActionsHandler_TargetResolution(t *testing.T) {
	ops := &mockActionOperations{
		messages: map[string]*message.Message{
			"msg-1": {ID: "msg-1", AccountID: "acc-1"},
		},
		threadMap: map[string][]string{
			"acc-1:thr-1": {"msg-1", "msg-2"},
		},
	}
	mux, _ := setupActionsTestServer(ops, map[v1.Capability]bool{v1.CapabilityMailWrite: true})

	// Single message target with non-existent message
	body := v1.ActionRequest{
		Action: v1.ActionMarkRead,
		Target: operations.Target{
			Kind: operations.TargetMessage,
			Messages: []operations.MessageRef{
				{AccountID: "acc-1", MessageID: "non-existent"},
			},
		},
	}
	payload, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/actions", bytes.NewReader(payload))
	req.Header.Set("Idempotency-Key", "k-target-1")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 Bad Request on target resolution error, got %d", rec.Code)
	}
}

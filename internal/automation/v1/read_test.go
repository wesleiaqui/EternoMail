package v1_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hkdb/aerion/internal/account"
	v1 "github.com/hkdb/aerion/internal/automation/v1"
	"github.com/hkdb/aerion/internal/email/replycontent"
	"github.com/hkdb/aerion/internal/folder"
	"github.com/hkdb/aerion/internal/message"
	"github.com/hkdb/aerion/internal/views"
	"github.com/hkdb/aerion/internal/virtualfolder"
)

type mockReadOperations struct {
	accounts        []*account.Account
	folders         []*folder.Folder
	virtualFolders  []*virtualfolder.VirtualFolder
	searchResult    *views.ThreadPage
	threadResult    *views.ThreadView
	msg             *message.Message
	reply           *replycontent.Reply
	fetchErr        error
	fetchMsg        *message.Message
	attachments     []*message.Attachment
	attachmentBytes []byte
}

func (m *mockReadOperations) GetAccounts() ([]*account.Account, error) {
	return m.accounts, nil
}
func (m *mockReadOperations) GetFolders(accountID string) ([]*folder.Folder, error) {
	return m.folders, nil
}
func (m *mockReadOperations) GetVirtualFolders() ([]*virtualfolder.VirtualFolder, error) {
	return m.virtualFolders, nil
}
func (m *mockReadOperations) SearchMessages(ctx context.Context, scope views.Scope, query string, cursor string, limit int) (*views.ThreadPage, error) {
	return m.searchResult, nil
}
func (m *mockReadOperations) GetThread(ctx context.Context, ref views.ThreadRef, scope views.Scope) (*views.ThreadView, error) {
	return m.threadResult, nil
}
func (m *mockReadOperations) GetMessage(accountID, messageID string) (*message.Message, error) {
	return m.msg, nil
}
func (m *mockReadOperations) GetReplyContent(accountID, messageID, channel string, fetch bool) (*replycontent.Reply, error) {
	return m.reply, nil
}
func (m *mockReadOperations) FetchMessageBody(ctx context.Context, accountID, messageID string) (*message.Message, error) {
	if m.fetchErr != nil {
		return nil, m.fetchErr
	}
	return m.fetchMsg, nil
}
func (m *mockReadOperations) GetAttachments(messageID string) ([]*message.Attachment, error) {
	return m.attachments, nil
}
func (m *mockReadOperations) ReadAttachmentBytes(accountID, messageID, attachmentID string) ([]byte, string, error) {
	return m.attachmentBytes, "text/plain", nil
}

func setupReadTestServer(ops v1.ReadOperations, grantedCaps map[v1.Capability]bool) (*http.ServeMux, *mockReadOperations) {
	mux := http.NewServeMux()
	handler := v1.NewReadHandler(ops)

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
	return mux, ops.(*mockReadOperations)
}

func TestReadHandler_AccountsAndFolders(t *testing.T) {
	ops := &mockReadOperations{
		accounts: []*account.Account{
			{ID: "acc-1", Email: "ari@example.test", Name: "Ari"},
		},
		folders: []*folder.Folder{
			{ID: "fld-1", AccountID: "acc-1", Name: "Inbox", Path: "INBOX", Type: folder.TypeInbox},
		},
	}
	mux, _ := setupReadTestServer(ops, map[v1.Capability]bool{v1.CapabilityMailRead: true})

	// GET /v1/accounts
	req := httptest.NewRequest(http.MethodGet, "/v1/accounts", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", rec.Code)
	}

	// GET /v1/folders
	req = httptest.NewRequest(http.MethodGet, "/v1/folders", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", rec.Code)
	}
}

func TestReadHandler_MessageBodyExplicitFetch(t *testing.T) {
	ops := &mockReadOperations{
		msg: &message.Message{
			ID:          "msg-1",
			AccountID:   "acc-1",
			BodyFetched: false,
		},
		fetchMsg: &message.Message{
			ID:          "msg-1",
			AccountID:   "acc-1",
			BodyFetched: true,
			BodyText:    "Hello world",
		},
	}

	// 1. Without mail.body.fetch capability, mode="fetch" must be rejected with 403
	mux, _ := setupReadTestServer(ops, map[v1.Capability]bool{v1.CapabilityMailRead: true})

	bodyReq := v1.BodyReadRequest{
		Ref:  views.MessageRef{AccountID: "acc-1", MessageID: "msg-1"},
		Mode: v1.FetchExplicit,
	}
	payload, _ := json.Marshal(bodyReq)
	req := httptest.NewRequest(http.MethodPost, "/v1/messages/body", bytes.NewReader(payload))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("expected 403 Forbidden without mail.body.fetch, got %d", rec.Code)
	}

	// 2. With mail.body.fetch capability, mode="fetch" succeeds
	muxWithFetch, _ := setupReadTestServer(ops, map[v1.Capability]bool{
		v1.CapabilityMailRead:      true,
		v1.CapabilityMailBodyFetch: true,
	})
	req = httptest.NewRequest(http.MethodPost, "/v1/messages/body", bytes.NewReader(payload))
	rec = httptest.NewRecorder()
	muxWithFetch.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 OK with mail.body.fetch, got %d", rec.Code)
	}
}

func TestReadHandler_ProviderFetchFailure(t *testing.T) {
	ops := &mockReadOperations{
		msg: &message.Message{
			ID:          "msg-1",
			AccountID:   "acc-1",
			BodyFetched: false,
		},
		fetchErr: errors.New("timeout connecting to imap"),
	}

	mux, _ := setupReadTestServer(ops, map[v1.Capability]bool{
		v1.CapabilityMailRead:      true,
		v1.CapabilityMailBodyFetch: true,
	})

	bodyReq := v1.BodyReadRequest{
		Ref:  views.MessageRef{AccountID: "acc-1", MessageID: "msg-1"},
		Mode: v1.FetchExplicit,
	}
	payload, _ := json.Marshal(bodyReq)
	req := httptest.NewRequest(http.MethodPost, "/v1/messages/body", bytes.NewReader(payload))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Errorf("expected 502 Bad Gateway on provider failure, got %d", rec.Code)
	}
}

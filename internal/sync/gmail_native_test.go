package sync

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"github.com/hkdb/aerion/internal/account"
	"github.com/hkdb/aerion/internal/database"
	"github.com/hkdb/aerion/internal/folder"
	"github.com/hkdb/aerion/internal/mailfilter"
	"github.com/hkdb/aerion/internal/message"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestNativeGmailEmptyCacheSyncAndOnDemandBodyWithoutIMAP(t *testing.T) {
	db, err := database.Open(filepath.Join(t.TempDir(), "mail.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = db.Migrate(); err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO accounts(id,name,email,imap_host,smtp_host,username,auth_type) VALUES('g','Gmail','me@example.org','imap.gmail.com','smtp.gmail.com','me','oauth2')`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO folders(id,account_id,name,path,folder_type) VALUES('inbox','g','Inbox','INBOX','inbox')`)
	if err != nil {
		t.Fatal(err)
	}
	var rawCalls atomic.Int32
	var archived atomic.Bool
	var bodyCalls, attachmentCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/labels":
			_, _ = w.Write([]byte(`{"labels":[{"id":"INBOX","name":"INBOX","type":"system"}]}`))
		case "/profile":
			_, _ = w.Write([]byte(`{"historyId":"100"}`))
		case "/history":
			if archived.Load() {
				_, _ = w.Write([]byte(`{"historyId":"102","history":[{"messages":[{"id":"provider"}]}]}`))
			} else {
				_, _ = w.Write([]byte(`{"historyId":"101","history":[]}`))
			}

		case "/messages":
			_, _ = w.Write([]byte(`{"messages":[{"id":"provider","threadId":"thread"}]}`))
		case "/messages/provider/attachments/file":
			attachmentCalls.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]string{"data": base64.RawURLEncoding.EncodeToString([]byte("file data"))})
		case "/messages/provider":
			if r.URL.Query().Get("format") == "full" {
				bodyCalls.Add(1)
				_ = json.NewEncoder(w).Encode(map[string]any{"payload": map[string]any{"mimeType": "multipart/mixed", "parts": []any{map[string]any{"partId": "0", "mimeType": "text/plain", "body": map[string]any{"data": base64.RawURLEncoding.EncodeToString([]byte("On demand body"))}}, map[string]any{"partId": "1", "mimeType": "application/zip", "filename": "example.zip", "body": map[string]any{"attachmentId": "file", "size": 9}}}}})
				return
			}
			if r.URL.Query().Get("format") == "raw" {
				rawCalls.Add(1)
				_ = json.NewEncoder(w).Encode(map[string]string{"raw": base64.RawURLEncoding.EncodeToString([]byte("From: Peer <peer@example.org>\r\nTo: me@example.org\r\nSubject: Remote only\r\nContent-Type: text/plain; charset=utf-8\r\n\r\nOn demand body"))})
				return
			}
			metadata := `{"id":"provider","threadId":"thread","internalDate":"1793000000000","labelIds":["INBOX","UNREAD"],"payload":{"headers":[{"name":"From","value":"Peer <peer@example.org>"},{"name":"Subject","value":"Remote only"},{"name":"Message-ID","value":"<remote@example.org>"}]}}`
			if archived.Load() {
				metadata = strings.Replace(metadata, `["INBOX","UNREAD"]`, `[]`, 1)
			}
			_, _ = w.Write([]byte(metadata))
		case "/labels/INBOX":
			_, _ = w.Write([]byte(`{"id":"INBOX","name":"INBOX","messagesTotal":1,"messagesUnread":1}`))
		default:
			t.Errorf("unexpected route %s", r.URL)
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	store := message.NewStore(db)
	engine := NewEngine(nil, account.NewStore(db), folder.NewStore(db), store, message.NewAttachmentStore(db))
	engine.gmailBaseURL = server.URL
	engine.SetGmailTokenSource(func(context.Context, string, bool) (string, error) { return "token", nil })
	callbacks := make(chan *message.Message, 1)
	engine.SetNewMessageHandler(func(_, _ string, m *message.Message) { callbacks <- m })
	if err = engine.SyncMessages(context.Background(), "g", "inbox", 0, false); err != nil {
		t.Fatal(err)
	}
	select {
	case snapshot := <-callbacks:
		stored, err := store.Get(snapshot.ID)
		if err != nil || snapshot.UID == 0 || snapshot.UID != stored.UID {
			t.Fatalf("callback identity mismatch: %+v stored %+v error %v", snapshot, stored, err)
		}
		rules := mailfilter.NewStore(db)
		rule := &mailfilter.Rule{ID: "gmail-arrival", Name: "Read arrival", Enabled: true, ActionMarkRead: true}
		if err := rules.Create(rule); err != nil {
			t.Fatal(err)
		}
		jobs := mailfilter.NewJobStore(db)
		router := mailfilter.NewAutomaticRouter(rules, folder.NewStore(db), func(rule *mailfilter.Rule, _ *folder.Folder, m *message.Message) {
			if err := jobs.Enqueue(rule, "", m); err != nil {
				t.Error(err)
			}
		})
		router.Route(snapshot)
		job, err := jobs.ClaimNext()
		if err != nil || job == nil || job.SourceUID != stored.UID {
			t.Fatalf("Gmail arrival not queued: %+v %v", job, err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("missing Gmail arrival callback")
	}
	conversations, err := store.ListConversationsByFolder("inbox", 0, 50, "newest", "")
	if err != nil || len(conversations) != 1 {
		t.Fatalf("empty cache not populated: %+v %v", conversations, err)
	}
	if rawCalls.Load() != 0 {
		t.Fatal("sync downloaded body")
	}
	m, err := engine.FetchMessageBody(context.Background(), "g", conversations[0].MessageIDs[0])
	if err != nil || m == nil || m.BodyText != "On demand body" {
		t.Fatalf("on-demand: %+v %v", m, err)
	}
	if rawCalls.Load() != 0 || bodyCalls.Load() != 1 || attachmentCalls.Load() != 0 {
		t.Fatal("body read fetched raw message/file attachment")
	}
	atts, err := engine.attachmentStore.GetByMessage(m.ID)
	if err != nil || len(atts) != 1 {
		t.Fatalf("attachment metadata: %+v %v", atts, err)
	}
	content, err := engine.FetchGmailAttachment(context.Background(), "g", atts[0].ID)
	if err != nil || string(content) != "file data" || attachmentCalls.Load() != 1 {
		t.Fatalf("on-demand attachment: %s %v", content, err)
	}
	if err = engine.SyncFolders(context.Background(), "g"); err != nil {
		t.Fatal(err)
	}
	if _, err = engine.SyncGmailAccount(context.Background(), "g"); err != nil {
		t.Fatal(err)
	}
	archived.Store(true)
	if _, err = engine.SyncGmailAccount(context.Background(), "g"); err != nil {
		t.Fatal(err)
	}
	remaining, err := store.ListConversationsByFolder("inbox", 0, 50, "newest", "")
	if err != nil || len(remaining) != 0 {
		t.Fatalf("removed server label remained in Inbox: %+v %v", remaining, err)
	}
	if rawCalls.Load() != 0 || bodyCalls.Load() != 1 || attachmentCalls.Load() != 1 {
		t.Fatal("history refresh downloaded content")
	}

}

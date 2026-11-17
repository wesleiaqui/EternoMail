package app

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/hkdb/aerion/internal/account"
	"github.com/hkdb/aerion/internal/database"
	"github.com/hkdb/aerion/internal/folder"
	"github.com/hkdb/aerion/internal/gmail"
	"github.com/hkdb/aerion/internal/message"
	syncengine "github.com/hkdb/aerion/internal/sync"
	"github.com/hkdb/aerion/internal/undo"
	"github.com/hkdb/aerion/internal/virtualfolder"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestGmailSearchAcrossAccountsAndVirtualFolderFromEmptyCache(t *testing.T) {
	db, err := database.Open(filepath.Join(t.TempDir(), "mail.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = db.Migrate(); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"a", "b"} {
		if _, err = db.Exec(`INSERT INTO accounts(id,name,email,imap_host,smtp_host,username,auth_type,enabled) VALUES(?,?,?,'imap.gmail.com','smtp.gmail.com',?,'oauth2',1)`, id, id, id+"@example.org", id); err != nil {
			t.Fatal(err)
		}
		for _, n := range []string{"1", "2"} {
			if _, err = db.Exec(`INSERT INTO folders(id,account_id,name,path,folder_type) VALUES(?,?,?,?, 'folder')`, id+n, id, "Folder "+n, "Folder "+n); err != nil {
				t.Fatal(err)
			}
		}
	}
	var bodyCalls, modifies, metadataCalls, providerCalls atomic.Int32
	var failB, quotaThreads, includeReplies, crossLabelStar atomic.Bool
	var lastListQuery atomic.Value
	header := func(id string) map[string]any {
		return map[string]any{"id": id, "threadId": id, "internalDate": fmt.Sprint(time.Now().UnixMilli()), "labelIds": []string{"Label_1", "UNREAD"}, "payload": map[string]any{"headers": []map[string]string{{"name": "Subject", "value": "Remote only"}, {"name": "From", "value": "peer@example.org"}, {"name": "Message-ID", "value": "<" + id + "@example.org>"}}}}
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		providerCalls.Add(1)
		if failB.Load() && r.Header.Get("Authorization") == "Bearer b" {
			w.WriteHeader(401)
			return
		}
		if quotaThreads.Load() && strings.HasPrefix(r.URL.Path, "/threads/") {
			w.WriteHeader(403)
			_, _ = w.Write([]byte(`{"error":{"errors":[{"reason":"rateLimitExceeded"}],"message":"quota"}}`))
			return
		}
		switch {
		case r.URL.Path == "/labels":
			_, _ = w.Write([]byte(`{"labels":[{"id":"Label_1","name":"Folder 1"},{"id":"Label_2","name":"Folder 2"}]}`))
		case r.URL.Path == "/messages" && r.Method == http.MethodPost:
			_, _ = w.Write([]byte(`{"id":"copied"}`))
		case r.URL.Path == "/messages" || r.URL.Path == "/threads":
			lastListQuery.Store(r.URL.Query().Get("q"))
			id, next := "same", ""
			if r.Header.Get("Authorization") == "Bearer a" && r.URL.Query().Get("pageToken") == "" {
				next = "next"
			}
			if r.URL.Query().Get("pageToken") == "next" {
				id = "second"
			}
			key := "messages"
			if r.URL.Path == "/threads" {
				key = "threads"
			}
			_ = json.NewEncoder(w).Encode(map[string]any{key: []map[string]string{{"id": id}}, "nextPageToken": next})
		case strings.HasSuffix(r.URL.Path, "/modify"):
			modifies.Add(1)
			_, _ = w.Write([]byte(`{}`))
		case strings.HasPrefix(r.URL.Path, "/messages/"):
			if r.URL.Query().Get("format") == "full" {
				bodyCalls.Add(1)
				_ = json.NewEncoder(w).Encode(map[string]any{"payload": map[string]any{"mimeType": "text/plain", "body": map[string]any{"data": base64.RawURLEncoding.EncodeToString([]byte("Requested content"))}}})
				return
			}
			if r.URL.Query().Get("format") == "raw" {
				bodyCalls.Add(1)
				_ = json.NewEncoder(w).Encode(map[string]string{"raw": base64.RawURLEncoding.EncodeToString([]byte("From: peer@example.org\r\nMessage-ID: <same@example.org>\r\n\r\nContent"))})
				return
			}
			metadataCalls.Add(1)
			_ = json.NewEncoder(w).Encode(header(strings.TrimPrefix(r.URL.Path, "/messages/")))
		case strings.HasPrefix(r.URL.Path, "/threads/"):
			item := header(strings.TrimPrefix(r.URL.Path, "/threads/"))
			if includeReplies.Load() {
				item["labelIds"] = []string{"Label_1", "UNREAD", "SENT"}
				payload := item["payload"].(map[string]any)
				payload["headers"] = append(payload["headers"].([]map[string]string), map[string]string{"name": "In-Reply-To", "value": "<original@example.org>"})
			}
			items := []any{item}
			if crossLabelStar.Load() {
				outside := header(strings.TrimPrefix(r.URL.Path, "/threads/") + "-outside")
				outside["threadId"] = item["threadId"]
				outside["labelIds"] = []string{"Label_2", "STARRED"}
				items = append(items, outside)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"messages": items})
		default:
			t.Errorf("unexpected request %s", r.URL)
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	a := &App{ctx: context.Background(), accountStore: account.NewStore(db), folderStore: folder.NewStore(db), messageStore: message.NewStore(db), virtualFolderStore: virtualfolder.NewStore(db), undoStack: undo.NewStack(10, time.Minute), runtimeEventEmitter: func(string, ...interface{}) {}}
	a.syncEngine = syncengine.NewEngine(nil, a.accountStore, a.folderStore, a.messageStore, message.NewAttachmentStore(db))
	a.syncEngine.SetGmailTokenSource(func(_ context.Context, id string, _ bool) (string, error) { return id, nil })
	a.syncEngine.SetGmailClientFactory(func(src gmail.TokenSource) *gmail.Client { c := gmail.NewClient(src); c.BaseURL = server.URL; return c })
	page, err := a.SearchMail(MailSearchRequest{Query: "is:unread", Limit: 10})
	if err != nil || len(page.Issues) > 0 || len(page.Conversations) != 2 || page.Cursor == "" {
		t.Fatalf("first page %+v %v", page, err)
	}
	if page.Conversations[0].ThreadID == page.Conversations[1].ThreadID {
		t.Fatal("cross-account thread identity collision")
	}
	next, err := a.SearchMail(MailSearchRequest{Query: "is:unread", Limit: 10, Cursor: page.Cursor})
	if err != nil || len(next.Conversations) != 1 || next.Cursor != "" {
		t.Fatalf("next page %+v %v", next, err)
	}
	if _, err = a.SearchMail(MailSearchRequest{Query: "different", Cursor: page.Cursor}); err == nil {
		t.Fatal("cursor accepted under a different query")
	}
	vf := &virtualfolder.VirtualFolder{Name: "Both accounts", FolderIDs: []string{"a1", "a2", "b1"}, ReadStatus: "unread"}
	if err = a.virtualFolderStore.Create(vf); err != nil {
		t.Fatal(err)
	}
	virtual, err := a.SearchMail(MailSearchRequest{VirtualFolderID: vf.ID})
	if err != nil || len(virtual.Issues) > 0 || len(virtual.Conversations) != 2 {
		t.Fatalf("virtual page %+v %v", virtual, err)
	}
	// A saved-search virtual folder applies its stored query on the provider.
	vf.SearchQuery = "invoice"
	if err = a.virtualFolderStore.Update(vf); err != nil {
		t.Fatal(err)
	}
	if _, err = a.SearchMail(MailSearchRequest{VirtualFolderID: vf.ID}); err != nil {
		t.Fatal(err)
	}
	if q, _ := lastListQuery.Load().(string); !strings.Contains(q, "invoice") {
		t.Fatalf("stored query not applied to provider search: %q", q)
	}
	// A typed in-folder search ANDs onto the stored query.
	if _, err = a.SearchMail(MailSearchRequest{VirtualFolderID: vf.ID, Query: "report"}); err != nil {
		t.Fatal(err)
	}
	if q, _ := lastListQuery.Load().(string); !strings.Contains(q, "invoice") || !strings.Contains(q, "report") {
		t.Fatalf("typed query not combined with stored query: %q", q)
	}
	// Resolve an operator through the provider, retain both pages, and hide
	// sources. No test-only cache seeding: this covers the production writer.
	vf.SearchQuery = "from:peer"
	if err = a.virtualFolderStore.Update(vf); err != nil {
		t.Fatal(err)
	}
	resolved, err := a.SearchMail(MailSearchRequest{VirtualFolderID: vf.ID})
	if err != nil || len(resolved.Issues) != 0 || resolved.Cursor == "" {
		t.Fatalf("operator first page %#v %v", resolved, err)
	}
	if _, err = a.SearchMail(MailSearchRequest{VirtualFolderID: vf.ID, Cursor: resolved.Cursor}); err != nil {
		t.Fatal(err)
	}
	refs, _, found, err := a.virtualFolderStore.LoadCache(vf.ID)
	if err != nil || !found || len(refs) != 3 {
		t.Fatalf("provider membership %#v %v %v", refs, found, err)
	}
	// Local refresh and a narrower typed query cannot overwrite membership.
	if err = a.RefreshVirtualFolderCache(vf.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = a.SearchMail(MailSearchRequest{VirtualFolderID: vf.ID, Query: "narrower"}); err != nil {
		t.Fatal(err)
	}
	if err = a.folderStore.SetHiddenBatch("a", []string{"a1"}, true); err != nil {
		t.Fatal(err)
	}
	if err = a.folderStore.SetHiddenBatch("b", []string{"b1"}, true); err != nil {
		t.Fatal(err)
	}
	beforeHidden := providerCalls.Load()
	retained, err := a.SearchMail(MailSearchRequest{VirtualFolderID: vf.ID})
	if err != nil || len(retained.Conversations) != 3 || len(retained.Issues) != 2 {
		t.Fatalf("operator retained view %#v %v", retained, err)
	}
	for _, issue := range retained.Issues {
		if !issue.Warning {
			t.Fatalf("not a stale warning: %#v", issue)
		}
	}
	if providerCalls.Load() != beforeHidden {
		t.Fatal("hidden saved search contacted provider")
	}
	if err = a.folderStore.SetHiddenBatch("a", []string{"a1"}, false); err != nil {
		t.Fatal(err)
	}
	if err = a.folderStore.SetHiddenBatch("b", []string{"b1"}, false); err != nil {
		t.Fatal(err)
	}
	// Gmail reply status is a provider thread decision, not IMAP Answered.
	// Retention must preserve both replied and unreplied provider memberships.
	for _, replyStatus := range []string{"replied", "unreplied"} {
		includeReplies.Store(replyStatus == "replied")
		vf.ReplyStatus = replyStatus
		if err = a.virtualFolderStore.Update(vf); err != nil {
			t.Fatal(err)
		}
		online, err := a.SearchMail(MailSearchRequest{VirtualFolderID: vf.ID})
		if err != nil || len(online.Conversations) != 2 || len(online.Issues) != 0 {
			t.Fatalf("Gmail %s online %#v %v", replyStatus, online, err)
		}
		if err = a.folderStore.SetHiddenBatch("a", []string{"a1"}, true); err != nil {
			t.Fatal(err)
		}
		if err = a.folderStore.SetHiddenBatch("b", []string{"b1"}, true); err != nil {
			t.Fatal(err)
		}
		before := providerCalls.Load()
		offline, err := a.SearchMail(MailSearchRequest{VirtualFolderID: vf.ID})
		if err != nil || len(offline.Conversations) != 2 || len(offline.Issues) != 2 || providerCalls.Load() != before {
			t.Fatalf("Gmail %s retained %#v %v", replyStatus, offline, err)
		}
		if err = a.folderStore.SetHiddenBatch("a", []string{"a1"}, false); err != nil {
			t.Fatal(err)
		}
		if err = a.folderStore.SetHiddenBatch("b", []string{"b1"}, false); err != nil {
			t.Fatal(err)
		}
	}
	includeReplies.Store(false)
	vf.ReplyStatus = "all"
	vf.StarStatus = "starred"
	vf.FolderIDs = []string{"a1", "b1"}
	crossLabelStar.Store(true)
	if err = a.virtualFolderStore.Update(vf); err != nil {
		t.Fatal(err)
	}
	online, err := a.SearchMail(MailSearchRequest{VirtualFolderID: vf.ID})
	if err != nil || len(online.Conversations) != 2 || len(online.Issues) != 0 {
		t.Fatalf("cross-label online %#v %v", online, err)
	}
	if err = a.folderStore.SetHiddenBatch("a", []string{"a1"}, true); err != nil {
		t.Fatal(err)
	}
	if err = a.folderStore.SetHiddenBatch("b", []string{"b1"}, true); err != nil {
		t.Fatal(err)
	}
	before := providerCalls.Load()
	offline, err := a.SearchMail(MailSearchRequest{VirtualFolderID: vf.ID})
	if err != nil || len(offline.Conversations) != 2 || len(offline.Issues) != 2 || providerCalls.Load() != before {
		t.Fatalf("cross-label retained %#v %v", offline, err)
	}
	if err = a.folderStore.SetHiddenBatch("a", []string{"a1"}, false); err != nil {
		t.Fatal(err)
	}
	if err = a.folderStore.SetHiddenBatch("b", []string{"b1"}, false); err != nil {
		t.Fatal(err)
	}
	crossLabelStar.Store(false)
	vf.StarStatus = "all"
	vf.SearchQuery = ""
	if err = a.virtualFolderStore.Update(vf); err != nil {
		t.Fatal(err)
	}
	failB.Store(true)
	partial, err := a.SearchMail(MailSearchRequest{Query: "is:unread"})
	if err != nil || len(partial.Conversations) != 1 || len(partial.Issues) != 1 || !partial.Issues[0].Reauthorize {
		t.Fatalf("one account hid another: %+v %v", partial, err)
	}
	failB.Store(false)
	if metadataCalls.Load() != 0 {
		t.Fatalf("search duplicated message and thread metadata: %d message gets", metadataCalls.Load())
	}
	if bodyCalls.Load() != 0 {
		t.Fatal("search fetched bodies")
	}
	var chosen *message.Conversation
	for _, c := range page.Conversations {
		if c.AccountID == "a" {
			chosen = c
		}
	}
	opened, err := a.messageStore.Get(chosen.MessageIDs[0])
	if err != nil || opened.BodyFetched {
		t.Fatalf("search-created message is not header-only: %+v %v", opened, err)
	}
	opened, err = a.FetchMessageBody(opened.ID)
	if err != nil || !opened.BodyFetched || opened.BodyText != "Requested content" {
		t.Fatalf("on-demand body unavailable: %+v %v", opened, err)
	}
	quotaThreads.Store(true)
	cached, err := a.GetConversation(chosen.ThreadID, chosen.FolderID)
	hasRequestedBody := false
	if cached != nil {
		for _, m := range cached.Messages {
			hasRequestedBody = hasRequestedBody || m.BodyText == "Requested content"
		}
	}
	if err != nil || cached == nil || !hasRequestedBody {
		t.Fatalf("quota hid cached reader: %+v %v", cached, err)
	}
	quotaThreads.Store(false)
	operation, err := a.moveToFolder(chosen.MessageIDs, "a2", true, "")
	if err != nil || operation == "" {
		t.Fatalf("native move failed %s %v", operation, err)
	}
	if _, err = a.UndoOperation(operation); err != nil {
		t.Fatal(err)
	}
	moved, err := a.messageStore.Get(chosen.MessageIDs[0])
	if err != nil || moved.FolderID != "a1" || modifies.Load() != 2 {
		t.Fatalf("native move/undo: %+v %v (%d requests)", moved, err, modifies.Load())
	}
	_, err = a.moveToFolder(chosen.MessageIDs, "b2", true, "")
	if err == nil {
		t.Fatal("unconfirmed destination copy was accepted")
	}
	source, err := a.messageStore.Get(chosen.MessageIDs[0])
	if err != nil || source == nil || source.FolderID != "a1" || modifies.Load() != 2 {
		t.Fatalf("unconfirmed copy changed the source: %+v %v", source, err)
	}

}

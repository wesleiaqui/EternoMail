package alwayson

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/hkdb/aerion/internal/account"
	v1 "github.com/hkdb/aerion/internal/automation/v1"
	"github.com/hkdb/aerion/internal/folder"
	"github.com/hkdb/aerion/internal/message"
	"github.com/hkdb/aerion/internal/replylater"
	"github.com/hkdb/aerion/internal/triage"
	"github.com/hkdb/aerion/internal/views"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRemoteLaterCatalogUnion(t *testing.T) {
	db, _, _ := newCachedVirtualFolderTestRuntime(t)
	defer db.Close()
	for _, query := range []string{`INSERT INTO read_later_queue(account_id,conversation_id) VALUES('acc','thread-a')`, `INSERT INTO reply_later_queue(account_id,conversation_id) VALUES('acc','thread-a'),('acc','thread-b')`} {
		if _, err := db.Exec(query); err != nil {
			t.Fatal(err)
		}
	}
	o := &catalogReadOperations{accounts: account.NewStore(db), folders: folder.NewStore(db), messages: message.NewStore(db)}
	for _, alias := range []string{"readLater", "replyLater"} {
		if err := (views.Scope{Kind: views.ScopeUnifiedFolder, FolderType: alias}).Validate(); err != nil {
			t.Fatal(err)
		}
		items, err := o.ListConversationsUnifiedFolder(alias, 0, 50, "newest", "")
		if err != nil {
			t.Fatal(err)
		}
		count, err := o.CountConversationsUnifiedFolder(alias, "")
		if err != nil {
			t.Fatal(err)
		}
		if len(items) != 2 || count != 2 {
			t.Fatalf("%s: list=%d count=%d", alias, len(items), count)
		}
		// Empty text search still exercises scope/folder resolution and union filtering.
		_, _, err = o.SearchConversationsUnifiedFolder(alias, "project", 0, 50, "")
		if err != nil {
			t.Fatal(err)
		}
	}
}

// Exercise the real durable HTTP/ledger path, including a new runtime over the
// same database before Undo, rather than merely reversing a frontend mock.
func TestLaterOneReceiptAndExactUndo(t *testing.T) {
	for _, prior := range []triage.LaterState{{}, {Read: true}, {Reply: true}, {Read: true, Reply: true}} {
		for _, on := range []bool{false, true} {
			for _, targeted := range []bool{false, true} {
				t.Run(fmt.Sprintf("read=%t/reply=%t/add=%t/targeted=%t", prior.Read, prior.Reply, on, targeted), func(t *testing.T) {
					db, _, _ := newCachedVirtualFolderTestRuntime(t)
					defer db.Close()
					ref := triage.Ref{AccountID: "acc", ConversationID: "thread-a"}
					store := triage.NewStore(db)
					if err := store.SetLaterState(context.Background(), ref, triage.LaterState{}, prior, nil); err != nil {
						t.Fatal(err)
					}
					makeMux := func() *http.ServeMux {
						r := &SyncRuntime{db: db, triage: triage.NewStore(db), replyLater: replylater.NewStore(db)}
						ledger := newActionReceiptLedger(db)
						mux := http.NewServeMux()
						allow := func(http.ResponseWriter, *http.Request, v1.Capability) bool { return true }
						v1.NewTriageMutationsHandler(r, ledger, nil).MountRoutes(mux, allow)
						v1.MountActionUndoRoutes(mux, ledger, &mailboxActionOperations{runtime: r}, nil, allow)
						return mux
					}
					mux := makeMux()
					post := func(path, key, body string) *httptest.ResponseRecorder {
						req := httptest.NewRequest("POST", path, strings.NewReader(body))
						req.Header.Set("Idempotency-Key", key)
						w := httptest.NewRecorder()
						mux.ServeHTTP(w, req)
						if w.Code != 200 {
							t.Fatalf("%s status %d: %s", path, w.Code, w.Body)
						}
						return w
					}
					body := fmt.Sprintf(`{"ref":{"accountId":"acc","conversationId":"thread-a"},"inQueue":%t}`, on)
					var response v1.Response[v1.TriageMutationResponse]
					if err := json.Unmarshal(post("/v1/triage/later/mutations", "one", body).Body.Bytes(), &response); err != nil {
						t.Fatal(err)
					}
					expected := triage.LaterState{}
					if on {
						expected = prior
						if !prior.Read && !prior.Reply {
							expected.Read = true
						}
					}
					got, err := store.LaterState(ref)
					if err != nil || got != expected {
						t.Fatalf("state=%+v err=%v", got, err)
					}
					var replay v1.Response[v1.TriageMutationResponse]
					if err := json.Unmarshal(post("/v1/triage/later/mutations", "one", body).Body.Bytes(), &replay); err != nil {
						t.Fatal(err)
					}
					if !replay.Data.Receipt.Replayed || replay.Data.Receipt.OperationID != response.Data.Receipt.OperationID {
						t.Fatal("idempotency changed receipt")
					}
					history, err := newActionReceiptLedger(db).ListUndoHistory(context.Background(), 25)
					if err != nil {
						t.Fatal(err)
					}
					changed := expected != prior
					if !changed {
						if len(history) != 0 || response.Data.UndoAvailable {
							t.Fatal("no-op created Undo history")
						}
						return
					}
					if len(history) != 1 || !response.Data.UndoAvailable || len(response.Data.AffectedTriage) != 2 {
						t.Fatalf("not one complete operation: %+v history=%+v", response.Data, history)
					}
					mux = makeMux() // A restarted helper reads the persisted combined snapshot.
					path := "/v1/undo/latest"
					if targeted {
						path = "/v1/undo/operations/" + response.Data.Receipt.OperationID
					}
					var result v1.Response[v1.ActionUndoResult]
					if err := json.Unmarshal(post(path, "undo", "{}").Body.Bytes(), &result); err != nil {
						t.Fatal(err)
					}
					if result.Data.State != "undone" || len(result.Data.AffectedTriage) != 2 {
						t.Fatalf("undo=%+v", result.Data)
					}
					got, err = store.LaterState(ref)
					if err != nil || got != prior {
						t.Fatalf("exact state not restored: got=%+v want=%+v err=%v", got, prior, err)
					}
				})
			}
		}
	}
}

func TestLaterProviderFailureDoesNotPartiallyRemove(t *testing.T) {
	db, _, _ := newCachedVirtualFolderTestRuntime(t)
	defer db.Close()
	store := triage.NewStore(db)
	ref := triage.Ref{AccountID: "acc", ConversationID: "thread-a"}
	prior := triage.LaterState{Read: true, Reply: true}
	if err := store.SetLaterState(context.Background(), ref, triage.LaterState{}, prior, nil); err != nil {
		t.Fatal(err)
	}
	remote := prior
	failed := false
	err := store.SetLaterState(context.Background(), ref, prior, triage.LaterState{}, func(queue string, on bool) error {
		if queue == "reply-later" && !on && !failed {
			failed = true
			return errors.New("provider offline")
		}
		if queue == "read-later" {
			remote.Read = on
		} else {
			remote.Reply = on
		}
		return nil
	})
	got, _ := store.LaterState(ref)
	if err == nil || got != prior || remote != prior {
		t.Fatalf("err=%v local=%+v provider=%+v", err, got, remote)
	}
	calls := 0
	if err := store.SetLaterState(context.Background(), ref, triage.LaterState{}, prior, func(string, bool) error { calls++; return nil }); err == nil || calls != 0 {
		t.Fatal("stale Undo modified provider")
	}
}

func TestLaterDatabaseFailureRollsBackBothMemberships(t *testing.T) {
	db, _, _ := newCachedVirtualFolderTestRuntime(t)
	defer db.Close()
	store := triage.NewStore(db)
	ref := triage.Ref{AccountID: "acc", ConversationID: "thread-a"}
	prior := triage.LaterState{Read: true, Reply: true}
	if err := store.SetLaterState(context.Background(), ref, triage.LaterState{}, prior, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TRIGGER reject_later_delete BEFORE DELETE ON reply_later_queue BEGIN SELECT RAISE(ABORT,'injected failure'); END`); err != nil {
		t.Fatal(err)
	}
	remote := prior
	err := store.SetLaterState(context.Background(), ref, prior, triage.LaterState{}, func(queue string, on bool) error {
		if queue == "read-later" {
			remote.Read = on
		} else {
			remote.Reply = on
		}
		return nil
	})
	got, _ := store.LaterState(ref)
	if err == nil || got != prior || remote != prior {
		t.Fatalf("err=%v local=%+v provider=%+v", err, got, remote)
	}
}

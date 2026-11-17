package app

import (
	"context"
	"github.com/hkdb/aerion/internal/account"
	"github.com/hkdb/aerion/internal/database"
	"github.com/hkdb/aerion/internal/triage"
	"github.com/hkdb/aerion/internal/undo"
	"path/filepath"
	"testing"
	"time"
)

func TestRemoteLaterFolderGuard(t *testing.T) {
	for _, folderType := range []string{"readLater", "replyLater"} {
		if !isRemoteUnifiedFolderType(folderType) {
			t.Errorf("remote guard rejects %s", folderType)
		}
	}
	if isRemoteUnifiedFolderType("arbitrary") {
		t.Fatal("remote guard accepts unknown view")
	}
}

func TestStandaloneLaterGlobalUndoRestoresBothFlags(t *testing.T) {
	db, err := database.Open(filepath.Join(t.TempDir(), "later.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO accounts(id,name,email,imap_host,smtp_host,username) VALUES('acc','Account','test@example.test','imap.example.test','smtp.example.test','test')`); err != nil {
		t.Fatal(err)
	}
	events := map[string]int{}
	a := &App{db: db, accountStore: account.NewStore(db), triageStore: triage.NewStore(db), undoStack: undo.NewStack(100, time.Hour), runtimeEventEmitter: func(name string, _ ...interface{}) { events[name]++ }}
	ref := triage.Ref{AccountID: "acc", ConversationID: "c"}
	prior := triage.LaterState{Read: true, Reply: true}
	if err := a.triageStore.SetLaterState(context.Background(), ref, triage.LaterState{}, prior, nil); err != nil {
		t.Fatal(err)
	}
	result, err := a.ChangeLaterWithUndo("acc", "c", false)
	if err != nil || !result.Changed || result.OperationID == "" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if events["readLater:refresh"] != 1 || events["replyLater:refresh"] != 1 {
		t.Fatal("Later list refresh missing")
	}
	if a.undoStack.Size() != 1 {
		t.Fatal("not one command")
	}
	restored, err := a.undoLatest()
	if err != nil || restored.OperationID != result.OperationID {
		t.Fatalf("undo=%+v err=%v", restored, err)
	}
	got, err := a.triageStore.LaterState(ref)
	if err != nil || got != prior {
		t.Fatalf("got=%+v err=%v", got, err)
	}
}

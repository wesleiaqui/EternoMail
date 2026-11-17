package undo

import (
	"github.com/hkdb/aerion/internal/triage"
	"testing"
	"time"
)

type laterTestContext struct {
	UndoContext
	state triage.LaterState
}

func (c *laterTestContext) RestoreLaterMembership(_ triage.Ref, expected, prior triage.LaterState) error {
	c.state = prior
	return nil
}
func TestLaterCommandDurableSingleOperation(t *testing.T) {
	db := openStoreDB(t)
	before := triage.LaterState{Read: true, Reply: true}
	after := triage.LaterState{}
	ctx := &laterTestContext{state: after}
	stack := NewStack(100, 24*time.Hour)
	stack.AttachStore(NewOperationStore(db), ctx)
	id := stack.PushOperation("", NewLaterChangeCommand(ctx, triage.Ref{AccountID: "a", ConversationID: "c"}, before, after, "Removed from Later"), OperationMetadata{Action: "read-later-remove"})
	restarted := NewStack(100, 24*time.Hour)
	restarted.AttachStore(NewOperationStore(db), ctx)
	claim := restarted.PopLatestOperation()
	if claim == nil || claim.OperationID != id || len(claim.Commands()) != 1 {
		t.Fatal("did not restore one operation")
	}
	if err := claim.Commands()[0].Undo(); err != nil {
		t.Fatal(err)
	}
	if ctx.state != before {
		t.Fatalf("restored %+v", ctx.state)
	}
}

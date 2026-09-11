package undo

import (
	"sync"
	"testing"
	"time"
)

type mockCommand struct {
	BaseCommand
	executeFn func() error
	undoFn    func() error
}

func (m *mockCommand) Execute() error {
	if m.executeFn != nil {
		return m.executeFn()
	}
	return nil
}

func (m *mockCommand) Undo() error {
	if m.undoFn != nil {
		return m.undoFn()
	}
	return nil
}

func newMock(desc string) *mockCommand {
	return &mockCommand{BaseCommand: NewBaseCommand(desc)}
}

func TestNewStack(t *testing.T) {
	s := NewStack(50, 30*time.Second)
	if s == nil {
		t.Fatal("expected non-nil stack")
	}
	if s.Size() != 0 {
		t.Fatalf("expected size 0, got %d", s.Size())
	}
	if s.CanUndo() {
		t.Fatal("expected CanUndo=false for empty stack")
	}
}

func TestPushAndPop(t *testing.T) {
	s := NewStack(50, 30*time.Second)
	cmds := []*mockCommand{newMock("first"), newMock("second"), newMock("third")}
	for _, c := range cmds {
		s.Push(c)
	}

	// Pop should return in LIFO order
	for i := len(cmds) - 1; i >= 0; i-- {
		got := s.Pop()
		if got == nil {
			t.Fatalf("expected command at index %d, got nil", i)
		}
		if got.Description() != cmds[i].Description() {
			t.Fatalf("expected %q, got %q", cmds[i].Description(), got.Description())
		}
	}
}

func TestPeek(t *testing.T) {
	s := NewStack(50, 30*time.Second)
	cmd := newMock("peek-me")
	s.Push(cmd)

	peeked := s.Peek()
	if peeked == nil {
		t.Fatal("expected non-nil from Peek")
	}
	if peeked.Description() != "peek-me" {
		t.Fatalf("expected %q, got %q", "peek-me", peeked.Description())
	}
	if s.Size() != 1 {
		t.Fatalf("expected size 1 after Peek, got %d", s.Size())
	}
}

func TestClear(t *testing.T) {
	s := NewStack(50, 30*time.Second)
	s.Push(newMock("a"))
	s.Push(newMock("b"))
	s.Clear()
	if s.Size() != 0 {
		t.Fatalf("expected size 0 after Clear, got %d", s.Size())
	}
}

func TestCanUndo(t *testing.T) {
	s := NewStack(50, 30*time.Second)
	if s.CanUndo() {
		t.Fatal("expected CanUndo=false for empty stack")
	}

	s.Push(newMock("cmd"))
	if !s.CanUndo() {
		t.Fatal("expected CanUndo=true after push")
	}

	s.Pop()
	if s.CanUndo() {
		t.Fatal("expected CanUndo=false after popping all")
	}
}

func TestMaxSize(t *testing.T) {
	s := NewStack(2, 30*time.Second)
	s.Push(newMock("first"))
	s.Push(newMock("second"))
	s.Push(newMock("third"))

	if s.Size() != 2 {
		t.Fatalf("expected size 2, got %d", s.Size())
	}

	// Oldest ("first") should have been dropped
	got := s.Pop()
	if got.Description() != "third" {
		t.Fatalf("expected %q, got %q", "third", got.Description())
	}
	got = s.Pop()
	if got.Description() != "second" {
		t.Fatalf("expected %q, got %q", "second", got.Description())
	}
}

func TestExpiration(t *testing.T) {
	s := NewStack(50, 50*time.Millisecond)
	s.Push(newMock("expires"))

	time.Sleep(100 * time.Millisecond)

	if s.CanUndo() {
		t.Fatal("expected CanUndo=false after expiration")
	}
}

func TestPopEmptyStack(t *testing.T) {
	s := NewStack(50, 30*time.Second)
	got := s.Pop()
	if got != nil {
		t.Fatalf("expected nil from empty stack, got %v", got)
	}
}

func TestBaseCommand(t *testing.T) {
	before := time.Now()
	bc := NewBaseCommand("test description")
	after := time.Now()

	if bc.Description() != "test description" {
		t.Fatalf("expected %q, got %q", "test description", bc.Description())
	}

	created := bc.CreatedAt()
	if created.Before(before) || created.After(after) {
		t.Fatalf("CreatedAt %v not between %v and %v", created, before, after)
	}
}

func TestSize(t *testing.T) {
	s := NewStack(50, 30*time.Second)
	n := 5
	for i := 0; i < n; i++ {
		s.Push(newMock("cmd"))
	}
	if s.Size() != n {
		t.Fatalf("expected size %d, got %d", n, s.Size())
	}
}

func TestPopOperationClaimsOnlyRequestedAction(t *testing.T) {
	s := NewStack(50, 30*time.Second)
	a := s.PushOperation("action-a", newMock("Done A"))
	s.PushOperation("action-b", newMock("Delete B"))

	if a != "action-a" {
		t.Fatalf("expected stable operation id, got %q", a)
	}
	commands := s.PopOperation("action-a")
	if len(commands) != 1 || commands[0].Description() != "Done A" {
		t.Fatalf("unexpected targeted undo: %#v", commands)
	}
	if got := s.Pop(); got == nil || got.Description() != "Delete B" {
		t.Fatalf("targeted undo consumed another action: %#v", got)
	}
	if again := s.PopOperation("action-a"); len(again) != 0 {
		t.Fatalf("claimed operation must not run twice: %#v", again)
	}
}

func TestPushOperationGeneratesDistinctTokens(t *testing.T) {
	s := NewStack(50, 30*time.Second)
	first := s.Push(newMock("first"))
	second := s.Push(newMock("second"))
	if first == "" || second == "" || first == second {
		t.Fatalf("expected distinct non-empty operation IDs, got %q and %q", first, second)
	}
}

func TestPushOperationGroupsCommandsUnderOneToken(t *testing.T) {
	s := NewStack(50, 30*time.Second)
	operationID := s.PushOperation("", newMock("first folder"))
	operationID = s.PushOperation(operationID, newMock("second folder"))

	commands := s.PopOperation(operationID)
	if len(commands) != 2 {
		t.Fatalf("expected both commands in one operation, got %d", len(commands))
	}
	if commands[0].Description() != "second folder" || commands[1].Description() != "first folder" {
		t.Fatalf("commands were not returned in reverse execution order: %q, %q", commands[0].Description(), commands[1].Description())
	}
}

func TestPopOperationConcurrentClaimsOnlyOnce(t *testing.T) {
	s := NewStack(50, 30*time.Second)
	s.PushOperation("action", newMock("only once"))

	var wg sync.WaitGroup
	claimed := make(chan int, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			claimed <- len(s.PopOperation("action"))
		}()
	}
	wg.Wait()
	close(claimed)

	total := 0
	for count := range claimed {
		total += count
	}
	if total != 1 {
		t.Fatalf("concurrent claims executed %d commands, want 1", total)
	}
}

func TestTargetedAndGlobalUndoKeepOperationLIFO(t *testing.T) {
	s := NewStack(50, 30*time.Second)
	s.PushOperation("A", newMock("A"))
	s.PushOperation("B", newMock("B"))
	s.PushOperation("C", newMock("C"))

	if got := s.PopOperation("B"); len(got) != 1 || got[0].Description() != "B" {
		t.Fatalf("targeted B = %#v", got)
	}
	claimC := s.PopLatestOperation()
	if claimC == nil || claimC.OperationID != "C" || len(claimC.Commands()) != 1 || claimC.Commands()[0].Description() != "C" {
		t.Fatalf("first global claim = %#v", claimC)
	}
	claimA := s.PopLatestOperation()
	if claimA == nil || claimA.OperationID != "A" || len(claimA.Commands()) != 1 || claimA.Commands()[0].Description() != "A" {
		t.Fatalf("second global claim = %#v", claimA)
	}
}

func TestFailedGlobalOperationCanBeRetried(t *testing.T) {
	s := NewStack(50, 20*time.Millisecond)
	s.PushOperation("A", newMock("A"))
	s.PushOperation("B", newMock("B"))
	if got := s.PopOperation("B"); len(got) != 1 {
		t.Fatalf("targeted B = %#v", got)
	}

	claim := s.PopLatestOperation()
	if claim == nil || claim.OperationID != "A" {
		t.Fatalf("global claim = %#v", claim)
	}
	// A failed command is restored with a fresh retry window, even if its
	// original TTL expires while the failed attempt is being reported.
	time.Sleep(25 * time.Millisecond)
	s.RestoreOperation(claim, 0)
	retry := s.PopLatestOperation()
	if retry == nil || retry.OperationID != "A" || len(retry.Commands()) != 1 {
		t.Fatalf("retry claim = %#v", retry)
	}
}

func TestFailedGroupedOperationRestoresOnlyUnfinishedCommands(t *testing.T) {
	s := NewStack(50, 30*time.Second)
	s.PushOperation("group", newMock("first"))
	s.PushOperation("group", newMock("second"))
	s.PushOperation("group", newMock("third"))

	claim := s.PopLatestOperation()
	if claim == nil || len(claim.Commands()) != 3 {
		t.Fatalf("group claim = %#v", claim)
	}
	// "third" completed, "second" failed, so second and first are retried.
	s.RestoreOperation(claim, 1)
	retry := s.PopLatestOperation()
	commands := retry.Commands()
	if len(commands) != 2 || commands[0].Description() != "second" || commands[1].Description() != "first" {
		t.Fatalf("unfinished retry commands = %#v", commands)
	}
}

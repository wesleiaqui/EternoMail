package app

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/hkdb/aerion/internal/undo"
)

type globalUndoTestCommand struct {
	undo.BaseCommand
	mu            sync.Mutex
	failRemaining int
	completed     *[]string
}

func newGlobalUndoTestCommand(description string, failRemaining int, completed *[]string) *globalUndoTestCommand {
	return &globalUndoTestCommand{
		BaseCommand:   undo.NewBaseCommand(description),
		failRemaining: failRemaining,
		completed:     completed,
	}
}

func (c *globalUndoTestCommand) Execute() error { return nil }

func (c *globalUndoTestCommand) Undo() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.failRemaining > 0 {
		c.failRemaining--
		return errors.New("intentional undo failure")
	}
	*c.completed = append(*c.completed, c.Description())
	return nil
}

func newGlobalUndoTestApp() *App {
	return &App{
		undoStack:           undo.NewStack(50, 30*time.Second),
		runtimeEventEmitter: func(string, ...interface{}) {},
	}
}

func TestGlobalUndoAfterTargetedUndoKeepsLIFOOrder(t *testing.T) {
	app := newGlobalUndoTestApp()
	completed := []string{}
	app.undoStack.PushOperation("A", newGlobalUndoTestCommand("A", 0, &completed))
	app.undoStack.PushOperation("B", newGlobalUndoTestCommand("B", 0, &completed))
	app.undoStack.PushOperation("C", newGlobalUndoTestCommand("C", 0, &completed))

	if _, err := app.UndoOperation("B"); err != nil {
		t.Fatalf("targeted B: %v", err)
	}
	first, err := app.UndoLatestWithResult()
	if err != nil {
		t.Fatalf("global C: %v", err)
	}
	second, err := app.UndoLatestWithResult()
	if err != nil {
		t.Fatalf("global A: %v", err)
	}
	if first.OperationID != "C" || second.OperationID != "A" {
		t.Fatalf("global results = %#v, %#v", first, second)
	}
	if got, want := completed, []string{"B", "C", "A"}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Fatalf("completion order = %v, want %v", got, want)
	}
}

func TestFailedGlobalUndoRetainsOperationForRetry(t *testing.T) {
	app := newGlobalUndoTestApp()
	completed := []string{}
	app.undoStack.PushOperation("A", newGlobalUndoTestCommand("A", 1, &completed))
	app.undoStack.PushOperation("B", newGlobalUndoTestCommand("B", 0, &completed))
	if _, err := app.UndoOperation("B"); err != nil {
		t.Fatalf("targeted B: %v", err)
	}

	if _, err := app.UndoLatestWithResult(); err == nil {
		t.Fatal("first global A unexpectedly succeeded")
	}
	if !app.undoStack.ContainsOperation("A") {
		t.Fatal("failed global operation A was consumed")
	}
	retry, err := app.UndoLatestWithResult()
	if err != nil {
		t.Fatalf("retry global A: %v", err)
	}
	if retry.OperationID != "A" || retry.Description != "A" {
		t.Fatalf("retry result = %#v", retry)
	}
}

func TestTargetedAndGlobalTwoActionOrders(t *testing.T) {
	for _, test := range []struct {
		name          string
		targeted      string
		wantGlobal    string
		wantCompleted []string
	}{
		{name: "target A then global B", targeted: "A", wantGlobal: "B", wantCompleted: []string{"A", "B"}},
		{name: "target B then global A", targeted: "B", wantGlobal: "A", wantCompleted: []string{"B", "A"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			app := newGlobalUndoTestApp()
			completed := []string{}
			app.undoStack.PushOperation("A", newGlobalUndoTestCommand("A", 0, &completed))
			app.undoStack.PushOperation("B", newGlobalUndoTestCommand("B", 0, &completed))
			if _, err := app.UndoOperation(test.targeted); err != nil {
				t.Fatalf("targeted %s: %v", test.targeted, err)
			}
			result, err := app.UndoLatestWithResult()
			if err != nil {
				t.Fatalf("global: %v", err)
			}
			if result.OperationID != test.wantGlobal {
				t.Fatalf("global operation = %q, want %q", result.OperationID, test.wantGlobal)
			}
			if completed[0] != test.wantCompleted[0] || completed[1] != test.wantCompleted[1] {
				t.Fatalf("completed = %v, want %v", completed, test.wantCompleted)
			}
		})
	}
}

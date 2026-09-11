package app

import (
	"fmt"
	"sort"
	"strings"

	"github.com/hkdb/aerion/internal/message"
	"github.com/hkdb/aerion/internal/undo"
)

// moveMutationFlight represents one physical local-first move. resultReady is
// closed as soon as the Wails method has its operation ID; the per-message map
// remains occupied until every background move completion has settled.
type moveMutationFlight struct {
	keys               []string
	action             string
	reverseUndo        bool
	resultReady        chan struct{}
	terminal           chan struct{}
	waiterReady        chan struct{}
	operationID        string
	moved              bool
	err                error
	resultPublished    bool
	pendingCompletions int
	waiterPublished    bool
	released           bool
}

func mutationKeys(messages []*message.Message) []string {
	keys := make([]string, 0, len(messages))
	seen := make(map[string]struct{}, len(messages))
	for _, msg := range messages {
		if msg == nil || msg.ID == "" || msg.AccountID == "" {
			continue
		}
		key := msg.AccountID + "\x00" + msg.ID
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func sameMutationKeys(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func (a *App) prepareMoveMutationKeys(messageIDs []string) ([]string, error) {
	if len(messageIDs) == 0 || a.messageStore == nil {
		return nil, nil
	}
	uniqueIDs := make(map[string]struct{}, len(messageIDs))
	for _, id := range messageIDs {
		if id != "" {
			uniqueIDs[id] = struct{}{}
		}
	}
	ids := make([]string, 0, len(uniqueIDs))
	for id := range uniqueIDs {
		ids = append(ids, id)
	}
	messages, err := a.messageStore.GetByIDs(ids)
	if err != nil {
		return nil, fmt.Errorf("resolve move mutation identities: %w", err)
	}
	if len(messages) != len(uniqueIDs) {
		return nil, fmt.Errorf("requested messages are no longer available")
	}
	keys := mutationKeys(messages)
	if len(keys) != len(uniqueIDs) {
		return nil, fmt.Errorf("failed to resolve move mutation identities")
	}
	return keys, nil
}

// runMoveMutation coalesces an exact duplicate/competing batch onto the first
// physical move. A partially overlapping batch is rejected as a whole so it
// can never execute silently for only a subset of its messages.
func (a *App) runMoveMutation(
	messageIDs []string,
	action string,
	mutation func() (moved bool, operationID string, err error),
) (moved bool, operationID string, coalesced bool, err error) {
	return a.runMoveMutationWithMode(messageIDs, action, false, mutation)
}

// runUndoMoveMutation puts the reverse leg of Undo under the same per-message
// lease as frontend move-like mutations. It deliberately has a distinct action
// identity, so a subsequent Done/Trash/Move waits for remote reconciliation
// instead of inheriting the consumed Undo operation ID.
func (a *App) runUndoMoveMutation(messageIDs []string, destFolderID, operationID string) error {
	_, _, _, err := a.runMoveMutationWithMode(
		messageIDs,
		"undo:"+operationID+":"+destFolderID,
		true,
		func() (bool, string, error) {
			_, moveErr := a.moveToFolder(messageIDs, destFolderID, false, operationID)
			return false, operationID, moveErr
		},
	)
	return err
}

func (a *App) runMoveMutationWithMode(
	messageIDs []string,
	action string,
	reverseUndo bool,
	mutation func() (moved bool, operationID string, err error),
) (moved bool, operationID string, coalesced bool, err error) {
	for {
		// This lookup intentionally lives inside the acquisition loop. If another
		// semantic action owns a lease, its local-first move can change folder and
		// UID while we wait; the next iteration must resolve current rows afresh.
		keys, prepareErr := a.prepareMoveMutationKeys(messageIDs)
		if prepareErr != nil {
			return false, "", false, prepareErr
		}
		if len(keys) == 0 {
			moved, operationID, err = mutation()
			return moved, operationID, false, err
		}

		a.moveMutationMu.Lock()
		if a.moveMutationFlights == nil {
			a.moveMutationFlights = make(map[string]*moveMutationFlight)
		}
		var existing *moveMutationFlight
		for _, key := range keys {
			flight := a.moveMutationFlights[key]
			if flight == nil {
				continue
			}
			if existing != nil && existing != flight {
				a.moveMutationMu.Unlock()
				return false, "", true, fmt.Errorf("move mutation batch overlaps multiple in-flight operations")
			}
			existing = flight
		}
		if existing != nil {
			if !sameMutationKeys(existing.keys, keys) {
				a.moveMutationMu.Unlock()
				return false, "", true, fmt.Errorf("move mutation batch partially overlaps an in-flight operation")
			}
			if reverseUndo || existing.reverseUndo {
				terminal := existing.terminal
				if !existing.waiterPublished {
					existing.waiterPublished = true
					close(existing.waiterReady)
				}
				a.moveMutationMu.Unlock()
				<-terminal
				continue
			}
			ready := existing.resultReady
			a.moveMutationMu.Unlock()
			<-ready
			return existing.moved, existing.operationID, true, existing.err
		}

		flight := &moveMutationFlight{
			keys:        keys,
			action:      action,
			reverseUndo: reverseUndo,
			resultReady: make(chan struct{}),
			terminal:    make(chan struct{}),
			waiterReady: make(chan struct{}),
		}
		for _, key := range keys {
			a.moveMutationFlights[key] = flight
		}
		a.moveMutationMu.Unlock()

		func() {
			defer func() {
				if recovered := recover(); recovered != nil {
					a.moveMutationMu.Lock()
					flight.err = fmt.Errorf("move mutation panicked: %v", recovered)
					flight.resultPublished = true
					close(flight.resultReady)
					if flight.pendingCompletions == 0 {
						a.releaseMoveMutationLocked(flight)
					}
					a.moveMutationMu.Unlock()
					panic(recovered)
				}
			}()
			moved, operationID, err = mutation()
		}()

		a.moveMutationMu.Lock()
		flight.moved = moved
		flight.operationID = operationID
		flight.err = err
		flight.resultPublished = true
		close(flight.resultReady)
		if flight.pendingCompletions == 0 {
			a.releaseMoveMutationLocked(flight)
		}
		a.moveMutationMu.Unlock()

		return moved, operationID, false, err
	}
}

// bindMoveMutationCompletion extends a frontend-call lease through the remote
// IMAP/reconcile phase created by moveToFolder. Legacy/internal calls that did
// not enter runMoveMutation simply have no matching flight and are unchanged.
func (a *App) bindMoveMutationCompletion(messages []*message.Message, completion *undo.MoveCompletion) {
	if completion == nil {
		return
	}
	keys := mutationKeys(messages)
	if len(keys) == 0 {
		return
	}

	a.moveMutationMu.Lock()
	var flight *moveMutationFlight
	for _, key := range keys {
		candidate := a.moveMutationFlights[key]
		if candidate == nil || (flight != nil && candidate != flight) {
			a.moveMutationMu.Unlock()
			return
		}
		flight = candidate
	}
	if flight == nil {
		a.moveMutationMu.Unlock()
		return
	}
	flight.pendingCompletions++
	a.moveMutationMu.Unlock()

	go func() {
		_ = completion.Wait()
		a.moveMutationMu.Lock()
		flight.pendingCompletions--
		if flight.resultPublished && flight.pendingCompletions == 0 {
			a.releaseMoveMutationLocked(flight)
		}
		a.moveMutationMu.Unlock()
	}()
}

func (a *App) releaseMoveMutationLocked(flight *moveMutationFlight) {
	if flight == nil || flight.released {
		return
	}
	flight.released = true
	for _, key := range flight.keys {
		if a.moveMutationFlights[key] == flight {
			delete(a.moveMutationFlights, key)
		}
	}
	close(flight.terminal)
}

func isSQLiteBusy(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToUpper(err.Error())
	return strings.Contains(text, "SQLITE_BUSY") ||
		strings.Contains(text, "DATABASE IS LOCKED") ||
		strings.Contains(text, "DATABASE TABLE IS LOCKED")
}

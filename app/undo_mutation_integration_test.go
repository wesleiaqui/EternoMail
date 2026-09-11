package app

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hkdb/aerion/internal/account"
	"github.com/hkdb/aerion/internal/database"
	"github.com/hkdb/aerion/internal/folder"
	appimap "github.com/hkdb/aerion/internal/imap"
	"github.com/hkdb/aerion/internal/message"
	"github.com/hkdb/aerion/internal/undo"
)

type undoMutationIMAPServer struct {
	listener   net.Listener
	mu         sync.Mutex
	nextUID    uint32
	copyUID    bool
	copiedUIDs []uint32
	afterCopy  func([]uint32)
	failSelect bool
	selectFail chan struct{}
}

func newUndoMutationIMAPServer(t *testing.T) *undoMutationIMAPServer {
	return newUndoMutationIMAPServerWithCopyUID(t, true)
}

func newUndoMutationIMAPServerWithCopyUID(t *testing.T, copyUID bool) *undoMutationIMAPServer {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := &undoMutationIMAPServer{listener: listener, nextUID: 100, copyUID: copyUID}
	t.Cleanup(func() { _ = listener.Close() })
	go server.serve()
	return server
}

func (s *undoMutationIMAPServer) serve() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return
		}
		go s.handle(conn)
	}
}

func (s *undoMutationIMAPServer) handle(conn net.Conn) {
	defer conn.Close()
	capability := "IMAP4rev1"
	if s.copyUID {
		capability += " UIDPLUS"
	}
	_, _ = fmt.Fprintf(conn, "* OK [CAPABILITY %s] undo test server\r\n", capability)
	scanner := bufio.NewScanner(conn)
	for scanner.Scan() {
		line := scanner.Text()
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		tag := fields[0]
		upper := strings.ToUpper(line)
		switch {
		case strings.Contains(upper, " CAPABILITY"):
			_, _ = fmt.Fprintf(conn, "* CAPABILITY %s\r\n%s OK CAPABILITY completed\r\n", capability, tag)
		case strings.Contains(upper, " LOGIN "):
			_, _ = fmt.Fprintf(conn, "%s OK LOGIN completed\r\n", tag)
		case strings.Contains(upper, " SELECT "):
			s.mu.Lock()
			failSelect := s.failSelect
			selectFail := s.selectFail
			if failSelect {
				s.failSelect = false
				s.selectFail = nil
			}
			s.mu.Unlock()
			if failSelect {
				if selectFail != nil {
					close(selectFail)
				}
				_, _ = fmt.Fprintf(conn, "%s NO SELECT rejected for test\r\n", tag)
				continue
			}
			_, _ = fmt.Fprintf(conn, "* FLAGS (\\Seen \\Deleted)\r\n* 1 EXISTS\r\n* OK [UIDVALIDITY 1] valid\r\n* OK [UIDNEXT 1000] next\r\n%s OK [READ-WRITE] SELECT completed\r\n", tag)
		case strings.Contains(upper, " UID COPY "):
			sourceSet := fields[3]
			sourceUIDs := expandTestUIDSet(sourceSet)
			s.mu.Lock()
			destinationUIDs := make([]uint32, len(sourceUIDs))
			for index := range sourceUIDs {
				destinationUIDs[index] = s.nextUID
				s.copiedUIDs = append(s.copiedUIDs, s.nextUID)
				s.nextUID++
			}
			afterCopy := s.afterCopy
			s.afterCopy = nil
			s.mu.Unlock()
			if afterCopy != nil {
				afterCopy(destinationUIDs)
			}
			if s.copyUID {
				_, _ = fmt.Fprintf(conn, "%s OK [COPYUID 1 %s %s] COPY completed\r\n", tag, sourceSet, joinTestUIDs(destinationUIDs))
			} else {
				_, _ = fmt.Fprintf(conn, "%s OK COPY completed\r\n", tag)
			}
		case strings.Contains(upper, " UID SEARCH"):
			s.mu.Lock()
			copiedUIDs := append([]uint32(nil), s.copiedUIDs...)
			s.mu.Unlock()
			_, _ = fmt.Fprintf(conn, "* SEARCH")
			for _, uid := range copiedUIDs {
				_, _ = fmt.Fprintf(conn, " %d", uid)
			}
			_, _ = fmt.Fprintf(conn, "\r\n%s OK SEARCH completed\r\n", tag)
		case strings.Contains(upper, " UID STORE "):
			_, _ = fmt.Fprintf(conn, "%s OK STORE completed\r\n", tag)
		case strings.Contains(upper, " UID EXPUNGE "):
			_, _ = fmt.Fprintf(conn, "* 1 EXPUNGE\r\n%s OK EXPUNGE completed\r\n", tag)
		case strings.Contains(upper, " NOOP"):
			_, _ = fmt.Fprintf(conn, "%s OK NOOP completed\r\n", tag)
		case strings.Contains(upper, " LOGOUT"):
			_, _ = fmt.Fprintf(conn, "* BYE logging out\r\n%s OK LOGOUT completed\r\n", tag)
			return
		default:
			_, _ = fmt.Fprintf(conn, "%s OK completed\r\n", tag)
		}
	}
}

func expandTestUIDSet(value string) []uint32 {
	var result []uint32
	for _, part := range strings.Split(value, ",") {
		bounds := strings.SplitN(part, ":", 2)
		start, err := strconv.ParseUint(bounds[0], 10, 32)
		if err != nil {
			continue
		}
		end := start
		if len(bounds) == 2 {
			parsedEnd, parseErr := strconv.ParseUint(bounds[1], 10, 32)
			if parseErr != nil {
				continue
			}
			end = parsedEnd
		}
		for uid := start; uid <= end; uid++ {
			result = append(result, uint32(uid))
		}
	}
	return result
}

func joinTestUIDs(uids []uint32) string {
	parts := make([]string, len(uids))
	for index, uid := range uids {
		parts[index] = strconv.FormatUint(uint64(uid), 10)
	}
	return strings.Join(parts, ",")
}

func (s *undoMutationIMAPServer) credentials(accountID string) (*appimap.ClientConfig, error) {
	host, portText, err := net.SplitHostPort(s.listener.Addr().String())
	if err != nil {
		return nil, err
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		return nil, err
	}
	return &appimap.ClientConfig{
		Host:           host,
		Port:           port,
		Security:       appimap.SecurityNone,
		Username:       accountID,
		Password:       "test",
		AuthMechanism:  "login",
		ConnectTimeout: time.Second,
		ReadTimeout:    time.Second,
		WriteTimeout:   time.Second,
	}, nil
}

func (s *undoMutationIMAPServer) copiedCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.copiedUIDs)
}

func (s *undoMutationIMAPServer) rejectNextSelect() <-chan struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	failed := make(chan struct{})
	s.failSelect = true
	s.selectFail = failed
	return failed
}

func undoMutationFixture(t *testing.T) (*App, *database.DB, string) {
	return undoMutationFixtureWithCopyUID(t, true)
}

func undoMutationFixtureWithCopyUID(t *testing.T, copyUID bool) (*App, *database.DB, string) {
	app, db, messageID, _ := undoMutationFixtureWithServer(t, copyUID)
	return app, db, messageID
}

func undoMutationFixtureWithServer(t *testing.T, copyUID bool) (*App, *database.DB, string, *undoMutationIMAPServer) {
	t.Helper()
	db, err := database.Open(filepath.Join(t.TempDir(), "undo.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Migrate(); err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{
		"INSERT INTO accounts (id,name,email,imap_host,smtp_host,username,sync_period_days) VALUES ('account','Test','test@example.com','127.0.0.1','smtp','test',30)",
		"INSERT INTO folders (id,account_id,name,path,folder_type) VALUES ('inbox','account','Inbox','INBOX','inbox')",
		"INSERT INTO folders (id,account_id,name,path,folder_type) VALUES ('archive','account','Archive','Archive','archive')",
		"INSERT INTO folders (id,account_id,name,path,folder_type) VALUES ('trash','account','Trash','Trash','trash')",
		"INSERT INTO folders (id,account_id,name,path,folder_type) VALUES ('spam','account','Spam','Spam','spam')",
	} {
		if _, err := db.Exec(query); err != nil {
			t.Fatal(err)
		}
	}
	server := newUndoMutationIMAPServerWithCopyUID(t, copyUID)
	pool := appimap.NewPool(appimap.PoolConfig{
		MaxConnections: 2,
		IdleTimeout:    time.Minute,
		ConnectTimeout: time.Second,
		WaiterTimeout:  time.Second,
	}, server.credentials)
	t.Cleanup(pool.CloseAll)
	messageStore := message.NewStore(db)
	messageID := "message-A"
	if err := messageStore.Create(&message.Message{
		ID:        messageID,
		AccountID: "account",
		FolderID:  "inbox",
		UID:       10,
		MessageID: "<old-search-result@example.com>",
		Subject:   "old search result",
		Date:      time.Now().AddDate(-2, 0, 0),
	}); err != nil {
		t.Fatal(err)
	}
	app := &App{
		ctx:                 context.Background(),
		db:                  db,
		accountStore:        account.NewStore(db),
		folderStore:         folder.NewStore(db),
		messageStore:        messageStore,
		imapPool:            pool,
		undoStack:           undo.NewStack(50, 30*time.Second),
		ownExpungeAt:        make(map[string]time.Time),
		syncContexts:        make(map[string]context.CancelFunc),
		syncLastRequest:     make(map[string]time.Time),
		moveSyncTimers:      make(map[string]*time.Timer),
		moveSyncWaiters:     make(map[string][]*undo.MoveCompletion),
		runtimeEventEmitter: func(string, ...interface{}) {},
	}
	return app, db, messageID, server
}

func createDuplicateMessage(t *testing.T, app *App, id, folderID string, uid uint32) {
	t.Helper()
	if err := app.messageStore.Create(&message.Message{
		ID:        id,
		AccountID: "account",
		FolderID:  folderID,
		UID:       uid,
		MessageID: "<old-search-result@example.com>",
		Subject:   "duplicate identity fixture",
		Date:      time.Now().AddDate(-2, 0, 0),
	}); err != nil {
		t.Fatal(err)
	}
}

func createIndependentMessage(t *testing.T, app *App, id, folderID string, uid uint32) {
	t.Helper()
	if err := app.messageStore.Create(&message.Message{
		ID:        id,
		AccountID: "account",
		FolderID:  folderID,
		UID:       uid,
		MessageID: fmt.Sprintf("<%s@example.com>", id),
		Subject:   "independent targeted undo fixture",
		Date:      time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
}

func waitForMessageFolderAndPositiveUID(t *testing.T, app *App, messageID, folderID string) *message.Message {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		messages, err := app.messageStore.GetByIDs([]string{messageID})
		if err == nil && len(messages) == 1 && messages[0].FolderID == folderID && int32(messages[0].UID) > 0 {
			return messages[0]
		}
		if time.Now().After(deadline) {
			t.Fatalf("message %s was not reconciled in %s: messages=%v err=%v", messageID, folderID, messages, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func assertUndoTokenLifecycle(t *testing.T, app *App, operationID, messageID string) {
	t.Helper()
	if operationID == "" {
		t.Fatal("mutation returned an empty operation ID")
	}
	if !app.undoStack.ContainsOperation(operationID) {
		t.Fatalf("operation %q was not registered in the stack", operationID)
	}
	if _, err := app.UndoOperation(operationID); err != nil {
		t.Fatalf("UndoOperation(%q): %v", operationID, err)
	}
	if app.undoStack.ContainsOperation(operationID) {
		t.Fatalf("operation %q remained available after Undo", operationID)
	}
	if _, err := app.UndoOperation(operationID); err == nil {
		t.Fatalf("second UndoOperation(%q) succeeded; token must be single-use", operationID)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		messages, err := app.messageStore.GetByIDs([]string{messageID})
		if err == nil && len(messages) == 1 && messages[0].FolderID == "inbox" && int32(messages[0].UID) > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("message was not fully restored to Inbox: messages=%v err=%v", messages, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestTrashWithUndoReturnsRegisteredSingleUseToken(t *testing.T) {
	app, _, messageID := undoMutationFixture(t)
	result, err := app.TrashWithUndo([]string{messageID})
	if err != nil {
		t.Fatal(err)
	}
	if !result.MovedToTrash {
		t.Fatal("TrashWithUndo did not report a move to Trash")
	}
	assertUndoTokenLifecycle(t, app, result.OperationID, messageID)
}

func TestRemoveFromInboxWithUndoReturnsRegisteredSingleUseToken(t *testing.T) {
	app, _, messageID := undoMutationFixture(t)
	result, err := app.RemoveFromInboxWithUndo([]string{messageID})
	if err != nil {
		t.Fatal(err)
	}
	assertUndoTokenLifecycle(t, app, result.OperationID, messageID)
}

func TestTargetedArchiveAndSpamToastsCannotStealEachOthersOperation(t *testing.T) {
	app, _, messageA := undoMutationFixture(t)
	const messageB = "message-B"
	createIndependentMessage(t, app, messageB, "inbox", 11)

	archive, err := app.ArchiveWithUndo([]string{messageA})
	if err != nil {
		t.Fatal(err)
	}
	waitForMessageFolderAndPositiveUID(t, app, messageA, "archive")
	spam, err := app.MarkAsSpamWithUndo([]string{messageB})
	if err != nil {
		t.Fatal(err)
	}
	if !spam.MovedToSpam || archive.OperationID == "" || spam.OperationID == "" || archive.OperationID == spam.OperationID {
		t.Fatalf("invalid targeted results: archive=%#v spam=%#v", archive, spam)
	}
	waitForMessageFolderAndPositiveUID(t, app, messageB, "spam")

	if _, err := app.UndoOperation(archive.OperationID); err != nil {
		t.Fatalf("Undo Archive A: %v", err)
	}
	waitForMessageFolderAndPositiveUID(t, app, messageA, "inbox")
	messageBState, err := app.messageStore.Get(messageB)
	if err != nil || messageBState == nil || messageBState.FolderID != "spam" {
		t.Fatalf("Undo Archive A changed Spam B: message=%v err=%v", messageBState, err)
	}

	if _, err := app.UndoOperation(spam.OperationID); err != nil {
		t.Fatalf("Undo Spam B: %v", err)
	}
	waitForMessageFolderAndPositiveUID(t, app, messageB, "inbox")
}

func TestTargetedTrashAndMoveToastsCannotStealEachOthersOperation(t *testing.T) {
	app, _, messageA := undoMutationFixture(t)
	const messageB = "message-B"
	createIndependentMessage(t, app, messageB, "inbox", 11)

	trash, err := app.TrashWithUndo([]string{messageA})
	if err != nil {
		t.Fatal(err)
	}
	waitForMessageFolderAndPositiveUID(t, app, messageA, "trash")
	move, err := app.MoveToFolderWithUndo([]string{messageB}, "archive")
	if err != nil {
		t.Fatal(err)
	}
	if !trash.MovedToTrash || trash.OperationID == "" || move.OperationID == "" || trash.OperationID == move.OperationID {
		t.Fatalf("invalid targeted results: trash=%#v move=%#v", trash, move)
	}
	waitForMessageFolderAndPositiveUID(t, app, messageB, "archive")

	if _, err := app.UndoOperation(trash.OperationID); err != nil {
		t.Fatalf("Undo Trash A: %v", err)
	}
	waitForMessageFolderAndPositiveUID(t, app, messageA, "inbox")
	messageBState, err := app.messageStore.Get(messageB)
	if err != nil || messageBState == nil || messageBState.FolderID != "archive" {
		t.Fatalf("Undo Trash A changed Move B: message=%v err=%v", messageBState, err)
	}

	if _, err := app.UndoOperation(move.OperationID); err != nil {
		t.Fatalf("Undo Move B: %v", err)
	}
	waitForMessageFolderAndPositiveUID(t, app, messageB, "inbox")
}

func runConcurrentMoveCalls(
	t *testing.T,
	first func() (string, bool, error),
	second func() (string, bool, error),
) [2]struct {
	operationID string
	coalesced   bool
	err         error
} {
	t.Helper()
	start := make(chan struct{})
	results := make(chan struct {
		operationID string
		coalesced   bool
		err         error
	}, 2)
	for _, call := range []func() (string, bool, error){first, second} {
		go func(call func() (string, bool, error)) {
			<-start
			operationID, coalesced, err := call()
			results <- struct {
				operationID string
				coalesced   bool
				err         error
			}{operationID: operationID, coalesced: coalesced, err: err}
		}(call)
	}
	close(start)
	return [2]struct {
		operationID string
		coalesced   bool
		err         error
	}{<-results, <-results}
}

func assertSingleCoalescedMove(
	t *testing.T,
	app *App,
	server *undoMutationIMAPServer,
	results [2]struct {
		operationID string
		coalesced   bool
		err         error
	},
) string {
	t.Helper()
	for _, result := range results {
		if result.err != nil {
			t.Fatalf("concurrent mutation: %v", result.err)
		}
	}
	if results[0].operationID == "" || results[0].operationID != results[1].operationID {
		t.Fatalf("operation IDs differ: %#v", results)
	}
	if results[0].coalesced == results[1].coalesced {
		t.Fatalf("expected exactly one coalesced result: %#v", results)
	}
	if app.undoStack.Size() != 1 {
		t.Fatalf("undo stack size = %d, want 1", app.undoStack.Size())
	}
	if server.copiedCount() != 1 {
		t.Fatalf("remote COPY count = %d, want 1", server.copiedCount())
	}
	return results[0].operationID
}

func installBlockingCopy(t *testing.T, server *undoMutationIMAPServer) (started <-chan struct{}, release func()) {
	t.Helper()
	copyStarted := make(chan struct{})
	releaseCopy := make(chan struct{})
	var releaseOnce sync.Once
	server.mu.Lock()
	server.afterCopy = func([]uint32) {
		close(copyStarted)
		<-releaseCopy
	}
	server.mu.Unlock()
	release = func() { releaseOnce.Do(func() { close(releaseCopy) }) }
	t.Cleanup(release)
	return copyStarted, release
}

func currentMoveMutationWaiter(t *testing.T, app *App, messageID string) <-chan struct{} {
	t.Helper()
	messageState, err := app.messageStore.Get(messageID)
	if err != nil || messageState == nil {
		t.Fatalf("resolve active move identity: message=%v err=%v", messageState, err)
	}
	key := messageState.AccountID + "\x00" + messageState.ID
	app.moveMutationMu.Lock()
	defer app.moveMutationMu.Unlock()
	flight := app.moveMutationFlights[key]
	if flight == nil {
		t.Fatalf("message %s has no active move lease", messageID)
	}
	return flight.waiterReady
}

func assertSingleStableMessageRow(t *testing.T, db *database.DB, messageID, folderID string) {
	t.Helper()
	var rows, temporaryRows int
	if err := db.QueryRow(`SELECT COUNT(*) FROM messages WHERE id = ? AND folder_id = ? AND uid > 0`, messageID, folderID).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM messages WHERE id = ? AND uid <= 0`, messageID).Scan(&temporaryRows); err != nil {
		t.Fatal(err)
	}
	if rows != 1 || temporaryRows != 0 {
		t.Fatalf("message %s final rows=%d temporaryRows=%d, want one stable row in %s", messageID, rows, temporaryRows, folderID)
	}
}

func startUndoScenarioMutation(t *testing.T, app *App, action, messageID string) string {
	t.Helper()
	switch action {
	case "done":
		result, err := app.RemoveFromInboxWithUndo([]string{messageID})
		if err != nil {
			t.Fatal(err)
		}
		return result.OperationID
	case "trash":
		result, err := app.TrashWithUndo([]string{messageID})
		if err != nil {
			t.Fatal(err)
		}
		return result.OperationID
	case "move-archive":
		result, err := app.MoveToFolderWithUndo([]string{messageID}, "archive")
		if err != nil {
			t.Fatal(err)
		}
		return result.OperationID
	case "move-spam":
		result, err := app.MoveToFolderWithUndo([]string{messageID}, "spam")
		if err != nil {
			t.Fatal(err)
		}
		return result.OperationID
	default:
		t.Fatalf("unknown scenario action %q", action)
		return ""
	}
}

func TestReverseUndoLeaseSerializesImmediateNextMove(t *testing.T) {
	tests := []struct {
		name        string
		firstAction string
		firstFolder string
		nextAction  string
		finalFolder string
	}{
		{name: "DeleteUndoDone", firstAction: "trash", firstFolder: "trash", nextAction: "done", finalFolder: "archive"},
		{name: "DoneUndoTrash", firstAction: "done", firstFolder: "archive", nextAction: "trash", finalFolder: "trash"},
		{name: "TrashUndoArchive", firstAction: "trash", firstFolder: "trash", nextAction: "move-archive", finalFolder: "archive"},
		{name: "MoveUndoMove", firstAction: "move-archive", firstFolder: "archive", nextAction: "move-spam", finalFolder: "spam"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			app, db, messageID, server := undoMutationFixtureWithServer(t, true)
			firstOperationID := startUndoScenarioMutation(t, app, test.firstAction, messageID)
			waitForMessageFolderAndPositiveUID(t, app, messageID, test.firstFolder)

			reverseCopyStarted, releaseReverseCopy := installBlockingCopy(t, server)
			if _, err := app.UndoOperation(firstOperationID); err != nil {
				t.Fatalf("UndoOperation(%q): %v", firstOperationID, err)
			}
			select {
			case <-reverseCopyStarted:
			case <-time.After(2 * time.Second):
				t.Fatal("reverse Undo COPY did not reach the deterministic barrier")
			}

			waiterReady := currentMoveMutationWaiter(t, app, messageID)
			nextResult := make(chan struct {
				operationID string
				err         error
			}, 1)
			go func() {
				var operationID string
				var err error
				switch test.nextAction {
				case "done":
					var result UndoableActionResult
					result, err = app.RemoveFromInboxWithUndo([]string{messageID})
					operationID = result.OperationID
				case "trash":
					var result TrashUndoResult
					result, err = app.TrashWithUndo([]string{messageID})
					operationID = result.OperationID
				case "move-archive":
					var result UndoableActionResult
					result, err = app.MoveToFolderWithUndo([]string{messageID}, "archive")
					operationID = result.OperationID
				case "move-spam":
					var result UndoableActionResult
					result, err = app.MoveToFolderWithUndo([]string{messageID}, "spam")
					operationID = result.OperationID
				}
				nextResult <- struct {
					operationID string
					err         error
				}{operationID: operationID, err: err}
			}()

			select {
			case <-waiterReady:
			case <-time.After(2 * time.Second):
				t.Fatal("next mutation did not register behind the reverse Undo lease")
			}
			select {
			case result := <-nextResult:
				t.Fatalf("next mutation completed before reverse reconcile: result=%+v", result)
			default:
			}
			stateDuringReverse, err := app.messageStore.Get(messageID)
			if err != nil || stateDuringReverse == nil || stateDuringReverse.FolderID != "inbox" || int32(stateDuringReverse.UID) >= 0 {
				t.Fatalf("unexpected state at reverse barrier: message=%v err=%v", stateDuringReverse, err)
			}

			releaseReverseCopy()
			var result struct {
				operationID string
				err         error
			}
			select {
			case result = <-nextResult:
			case <-time.After(2 * time.Second):
				t.Fatal("next mutation did not continue after reverse reconcile")
			}
			if result.err != nil {
				t.Fatalf("next mutation: %v", result.err)
			}
			if result.operationID == "" || result.operationID == firstOperationID {
				t.Fatalf("next mutation returned invalid operation ID %q after %q", result.operationID, firstOperationID)
			}
			finalState := waitForMessageFolderAndPositiveUID(t, app, messageID, test.finalFolder)
			if finalState.UID == stateDuringReverse.UID {
				t.Fatalf("next mutation reused temporary UID %d", finalState.UID)
			}
			assertSingleStableMessageRow(t, db, messageID, test.finalFolder)
			if server.copiedCount() != 3 {
				t.Fatalf("remote COPY count = %d, want forward + reverse + next", server.copiedCount())
			}
		})
	}
}

func TestReverseUndoLeaseDoesNotBlockIndependentMessage(t *testing.T) {
	app, _, messageA, server := undoMutationFixtureWithServer(t, true)
	const messageB = "message-B"
	createIndependentMessage(t, app, messageB, "inbox", 11)

	trash, err := app.TrashWithUndo([]string{messageA})
	if err != nil {
		t.Fatal(err)
	}
	waitForMessageFolderAndPositiveUID(t, app, messageA, "trash")
	reverseCopyStarted, releaseReverseCopy := installBlockingCopy(t, server)
	if _, err := app.UndoOperation(trash.OperationID); err != nil {
		t.Fatal(err)
	}
	select {
	case <-reverseCopyStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("reverse Undo COPY did not start")
	}

	bResult := make(chan error, 1)
	go func() {
		_, err := app.RemoveFromInboxWithUndo([]string{messageB})
		bResult <- err
	}()
	select {
	case err := <-bResult:
		if err != nil {
			t.Fatalf("independent B mutation: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("independent B mutation was blocked by A's lease")
	}
	bState, err := app.messageStore.Get(messageB)
	if err != nil || bState == nil || bState.FolderID != "archive" {
		t.Fatalf("independent B did not move while A was blocked: message=%v err=%v", bState, err)
	}

	releaseReverseCopy()
	waitForMessageFolderAndPositiveUID(t, app, messageA, "inbox")
	waitForMessageFolderAndPositiveUID(t, app, messageB, "archive")
}

func TestMoveWithTemporarySourceUIDFailsBeforeLocalMoveOrUndoPush(t *testing.T) {
	app, db, messageID, server := undoMutationFixtureWithServer(t, true)
	if _, err := db.Exec(`UPDATE messages SET uid = -123 WHERE id = ?`, messageID); err != nil {
		t.Fatal(err)
	}

	result, err := app.RemoveFromInboxWithUndo([]string{messageID})
	if err == nil {
		t.Fatal("move with a temporary source UID unexpectedly succeeded")
	}
	if result.OperationID != "" || app.undoStack.Size() != 0 {
		t.Fatalf("failed remote-start precondition left an undo token: result=%+v stack=%d", result, app.undoStack.Size())
	}
	if server.copiedCount() != 0 {
		t.Fatalf("temporary source UID triggered %d remote COPY commands", server.copiedCount())
	}
	state, getErr := app.messageStore.Get(messageID)
	if getErr != nil || state == nil || state.FolderID != "inbox" || int32(state.UID) != -123 {
		t.Fatalf("failed precondition changed local state: message=%v err=%v", state, getErr)
	}
	var rows int
	if err := db.QueryRow(`SELECT COUNT(*) FROM messages WHERE id = ?`, messageID).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 1 {
		t.Fatalf("failed precondition changed row cardinality: %d", rows)
	}
}

func TestMoveCompensatesAndInvalidatesTokenWhenRemoteCannotStart(t *testing.T) {
	app, db, messageID, server := undoMutationFixtureWithServer(t, true)
	selectFailed := server.rejectNextSelect()
	syncError := make(chan struct{}, 1)
	app.runtimeEventEmitter = func(eventName string, _ ...interface{}) {
		if eventName == "folder:syncError" {
			syncError <- struct{}{}
		}
	}

	result, err := app.RemoveFromInboxWithUndo([]string{messageID})
	if err != nil {
		t.Fatalf("local-first call returned before remote start failure: %v", err)
	}
	if result.OperationID == "" {
		t.Fatal("local-first call did not identify its provisional undo operation")
	}
	select {
	case <-selectFailed:
	case <-time.After(2 * time.Second):
		t.Fatal("remote SELECT was not attempted")
	}
	select {
	case <-syncError:
	case <-time.After(2 * time.Second):
		t.Fatal("remote-start failure did not reach terminal compensation")
	}

	state, getErr := app.messageStore.Get(messageID)
	if getErr != nil || state == nil || state.FolderID != "inbox" || state.UID != 10 {
		t.Fatalf("unstarted move was not compensated: message=%v err=%v", state, getErr)
	}
	if server.copiedCount() != 0 {
		t.Fatalf("remote-start failure issued %d COPY commands", server.copiedCount())
	}
	if app.undoStack.ContainsOperation(result.OperationID) || app.undoStack.Size() != 0 {
		t.Fatalf("unstarted move left operation %q undoable", result.OperationID)
	}
	assertSingleStableMessageRow(t, db, messageID, "inbox")
}

func TestMoveMutationGuardReleasesLeaseAfterErrorAndPanic(t *testing.T) {
	app, _, messageID := undoMutationFixture(t)
	wantErr := fmt.Errorf("controlled failure")
	if _, _, _, err := app.runMoveMutation([]string{messageID}, "error", func() (bool, string, error) {
		return false, "", wantErr
	}); err != wantErr {
		t.Fatalf("controlled error = %v, want %v", err, wantErr)
	}

	calledAfterError := false
	if _, _, _, err := app.runMoveMutation([]string{messageID}, "after-error", func() (bool, string, error) {
		calledAfterError = true
		return false, "", nil
	}); err != nil || !calledAfterError {
		t.Fatalf("lease remained after error: called=%t err=%v", calledAfterError, err)
	}

	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("move mutation panic was not propagated")
			}
		}()
		_, _, _, _ = app.runMoveMutation([]string{messageID}, "panic", func() (bool, string, error) {
			panic("test panic")
		})
	}()

	calledAfterPanic := false
	if _, _, _, err := app.runMoveMutation([]string{messageID}, "after-panic", func() (bool, string, error) {
		calledAfterPanic = true
		return false, "", nil
	}); err != nil || !calledAfterPanic {
		t.Fatalf("lease remained after panic: called=%t err=%v", calledAfterPanic, err)
	}
}

func TestConcurrentRemoveFromInboxCoalescesOneRemoteMoveAndUndoOperation(t *testing.T) {
	app, _, messageID, server := undoMutationFixtureWithServer(t, true)
	copyStarted, releaseCopy := installBlockingCopy(t, server)

	results := runConcurrentMoveCalls(t,
		func() (string, bool, error) {
			result, err := app.RemoveFromInboxWithUndo([]string{messageID})
			return result.OperationID, result.Coalesced, err
		},
		func() (string, bool, error) {
			result, err := app.RemoveFromInboxWithUndo([]string{messageID})
			return result.OperationID, result.Coalesced, err
		},
	)
	select {
	case <-copyStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("remote COPY did not start")
	}
	assertSingleCoalescedMove(t, app, server, results)
	releaseCopy()
	waitForMessageFolderAndPositiveUID(t, app, messageID, "archive")
}

func TestConcurrentTrashCoalescesOneRemoteMoveAndUndoOperation(t *testing.T) {
	app, _, messageID, server := undoMutationFixtureWithServer(t, true)
	copyStarted, releaseCopy := installBlockingCopy(t, server)

	results := runConcurrentMoveCalls(t,
		func() (string, bool, error) {
			result, err := app.TrashWithUndo([]string{messageID})
			return result.OperationID, result.Coalesced, err
		},
		func() (string, bool, error) {
			result, err := app.TrashWithUndo([]string{messageID})
			return result.OperationID, result.Coalesced, err
		},
	)
	select {
	case <-copyStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("remote COPY did not start")
	}
	assertSingleCoalescedMove(t, app, server, results)
	releaseCopy()
	waitForMessageFolderAndPositiveUID(t, app, messageID, "trash")
}

func TestConcurrentDoneAndTrashCoalesceWithoutDuplicateRemoteMove(t *testing.T) {
	app, _, messageID, server := undoMutationFixtureWithServer(t, true)
	copyStarted, releaseCopy := installBlockingCopy(t, server)

	results := runConcurrentMoveCalls(t,
		func() (string, bool, error) {
			result, err := app.RemoveFromInboxWithUndo([]string{messageID})
			return result.OperationID, result.Coalesced, err
		},
		func() (string, bool, error) {
			result, err := app.TrashWithUndo([]string{messageID})
			return result.OperationID, result.Coalesced, err
		},
	)
	select {
	case <-copyStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("remote COPY did not start")
	}
	assertSingleCoalescedMove(t, app, server, results)
	releaseCopy()

	deadline := time.Now().Add(2 * time.Second)
	for {
		messages, err := app.messageStore.GetByIDs([]string{messageID})
		if err == nil && len(messages) == 1 && (messages[0].FolderID == "archive" || messages[0].FolderID == "trash") && int32(messages[0].UID) > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("competing move did not settle consistently: messages=%v err=%v", messages, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestSQLiteBusyClassifierDoesNotRetryIdentityConflicts(t *testing.T) {
	for _, text := range []string{
		"database is locked (5) (SQLITE_BUSY)",
		"SQLITE_BUSY_SNAPSHOT: database is locked",
		"database table is locked",
	} {
		if !isSQLiteBusy(fmt.Errorf("%s", text)) {
			t.Fatalf("busy error was not classified for retry: %q", text)
		}
	}
	if isSQLiteBusy(&message.MovedUIDConflictError{MovingLocalID: "A", ExistingLocalID: "B"}) {
		t.Fatal("identity conflict was incorrectly classified as SQLITE_BUSY")
	}
}

func TestMutationEventsDoNotClaimUndoToken(t *testing.T) {
	app, _, messageID := undoMutationFixture(t)
	result, err := app.TrashWithUndo([]string{messageID})
	if err != nil {
		t.Fatal(err)
	}
	// Runtime messages:moved/messages:updated handlers are frontend observers;
	// no event path calls Pop or PopOperation. The token remains claimable after
	// the mutation's normal event phase.
	time.Sleep(20 * time.Millisecond)
	if !app.undoStack.ContainsOperation(result.OperationID) {
		t.Fatal("normal mutation event processing consumed the undo token")
	}
	assertUndoTokenLifecycle(t, app, result.OperationID, messageID)
}

func TestInboxSyncWindowCleanupDoesNotDeleteOldMovedMessage(t *testing.T) {
	app, _, messageID := undoMutationFixture(t)
	result, err := app.TrashWithUndo([]string{messageID})
	if err != nil {
		t.Fatal(err)
	}

	deleted, err := app.messageStore.DeleteOlderThanInFolder("inbox", time.Now().AddDate(0, 0, -30))
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 0 {
		t.Fatalf("Inbox cleanup deleted %d messages after the only old row moved to Trash", deleted)
	}
	messages, err := app.messageStore.GetByIDs([]string{messageID})
	if err != nil || len(messages) != 1 || messages[0].FolderID != "trash" {
		t.Fatalf("old moved message was not preserved in Trash: messages=%v err=%v", messages, err)
	}
	assertUndoTokenLifecycle(t, app, result.OperationID, messageID)
}

func TestMoveUndoUsesExactLocalIdentityWhenMessageIDIsDuplicated(t *testing.T) {
	app, _, messageA := undoMutationFixture(t)
	const messageB = "message-B"
	createDuplicateMessage(t, app, messageB, "inbox", 11)

	result, err := app.TrashWithUndo([]string{messageA})
	if err != nil {
		t.Fatal(err)
	}
	assertUndoTokenLifecycle(t, app, result.OperationID, messageA)

	messages, err := app.messageStore.GetByIDs([]string{messageB})
	if err != nil || len(messages) != 1 {
		t.Fatalf("failed to reload untouched duplicate B: messages=%v err=%v", messages, err)
	}
	if messages[0].FolderID != "inbox" || messages[0].UID != 11 {
		t.Fatalf("duplicate B was changed by Undo for A: folder=%s uid=%d", messages[0].FolderID, messages[0].UID)
	}
}

func TestNoCopyUIDFallbackMapsDuplicateMessageIDsOneToOne(t *testing.T) {
	app, _, messageA := undoMutationFixtureWithCopyUID(t, false)
	const messageB = "message-B"
	createDuplicateMessage(t, app, messageB, "inbox", 11)

	if _, err := app.TrashWithUndo([]string{messageA, messageB}); err != nil {
		t.Fatal(err)
	}
	a := waitForMessageFolderAndPositiveUID(t, app, messageA, "trash")
	b := waitForMessageFolderAndPositiveUID(t, app, messageB, "trash")
	if a.UID == b.UID {
		t.Fatalf("no-COPYUID fallback assigned duplicate destination UID %d", a.UID)
	}
}

func TestGlobalUndoRetainsTokenWhenMessageIDFallbackIsAmbiguous(t *testing.T) {
	app, _, messageA := undoMutationFixture(t)
	result, err := app.TrashWithUndo([]string{messageA})
	if err != nil {
		t.Fatal(err)
	}
	waitForMessageFolderAndPositiveUID(t, app, messageA, "trash")
	if err := app.messageStore.DeleteBatch([]string{messageA}); err != nil {
		t.Fatal(err)
	}
	createDuplicateMessage(t, app, "message-C", "trash", 201)
	createDuplicateMessage(t, app, "message-D", "trash", 202)

	if _, err := app.UndoLatestWithResult(); err == nil {
		t.Fatal("ambiguous Message-ID fallback unexpectedly succeeded")
	}
	if !app.undoStack.ContainsOperation(result.OperationID) {
		t.Fatal("global Undo did not retain the token after ambiguous lookup")
	}
	for _, id := range []string{"message-C", "message-D"} {
		messages, err := app.messageStore.GetByIDs([]string{id})
		if err != nil || len(messages) != 1 || messages[0].FolderID != "trash" {
			t.Fatalf("ambiguous fallback moved %s: messages=%v err=%v", id, messages, err)
		}
	}
}

func TestMoveReconcileMergesConcurrentSyncRowAndUndoKeepsLocalIdentity(t *testing.T) {
	app, db, messageA, server := undoMutationFixtureWithServer(t, true)
	if _, err := db.Exec(`INSERT INTO attachments (id, message_id, filename, is_inline) VALUES ('moving-attachment', ?, 'moving.txt', 0)`, messageA); err != nil {
		t.Fatal(err)
	}
	hookErr := make(chan error, 1)
	server.mu.Lock()
	server.afterCopy = func(destinationUIDs []uint32) {
		if len(destinationUIDs) != 1 {
			hookErr <- fmt.Errorf("destination UID count = %d", len(destinationUIDs))
			return
		}
		err := app.messageStore.Upsert(&message.Message{
			ID:             "sync-A",
			AccountID:      "account",
			FolderID:       "archive",
			UID:            destinationUIDs[0],
			MessageID:      "<old-search-result@example.com>",
			ThreadID:       "sync-thread",
			InboxCategory:  "primary",
			IsRead:         true,
			BodyText:       "body fetched by concurrent sync",
			BodyFetched:    true,
			HasAttachments: true,
			Date:           time.Now().AddDate(-2, 0, 0),
		})
		if err == nil {
			_, err = db.Exec(`INSERT INTO attachments (id, message_id, filename, is_inline) VALUES ('sync-attachment', 'sync-A', 'sync.txt', 0)`)
		}
		hookErr <- err
	}
	server.mu.Unlock()

	result, err := app.RemoveFromInboxWithUndo([]string{messageA})
	if err != nil {
		t.Fatal(err)
	}
	if err := <-hookErr; err != nil {
		t.Fatalf("concurrent destination sync fixture: %v", err)
	}
	reconciled := waitForMessageFolderAndPositiveUID(t, app, messageA, "archive")
	if reconciled.UID != 100 || !reconciled.IsRead || !reconciled.BodyFetched || reconciled.BodyText == "" {
		t.Fatalf("merged message state was not preserved: uid=%d read=%t bodyFetched=%t body=%q", reconciled.UID, reconciled.IsRead, reconciled.BodyFetched, reconciled.BodyText)
	}
	if syncRow, err := app.messageStore.Get("sync-A"); err != nil || syncRow != nil {
		t.Fatalf("sync-created duplicate still exists: row=%v err=%v", syncRow, err)
	}
	var messageCount, attachmentCount, ftsCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM messages WHERE folder_id = 'archive' AND uid = 100`).Scan(&messageCount); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM attachments WHERE message_id = ?`, messageA).Scan(&attachmentCount); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM messages_fts WHERE messages_fts MATCH 'concurrent'`).Scan(&ftsCount); err != nil {
		t.Fatal(err)
	}
	if messageCount != 1 || attachmentCount != 2 || ftsCount != 1 {
		t.Fatalf("merge cardinality mismatch: messages=%d attachments=%d fts=%d", messageCount, attachmentCount, ftsCount)
	}
	rows, err := db.Query(`PRAGMA foreign_key_check`)
	if err != nil {
		t.Fatal(err)
	}
	if rows.Next() {
		rows.Close()
		t.Fatal("foreign_key_check found a violation after merge")
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}

	assertUndoTokenLifecycle(t, app, result.OperationID, messageA)
}

func TestMoveReconcileDoesNotMergeDifferentMessageOwningDestinationUID(t *testing.T) {
	app, _, messageA, server := undoMutationFixtureWithServer(t, true)
	errorEvent := make(chan struct{}, 1)
	app.runtimeEventEmitter = func(eventName string, _ ...interface{}) {
		if eventName == "folder:syncError" {
			select {
			case errorEvent <- struct{}{}:
			default:
			}
		}
	}
	hookErr := make(chan error, 1)
	server.mu.Lock()
	server.afterCopy = func(destinationUIDs []uint32) {
		if len(destinationUIDs) != 1 {
			hookErr <- fmt.Errorf("destination UID count = %d", len(destinationUIDs))
			return
		}
		hookErr <- app.messageStore.Upsert(&message.Message{
			ID:            "message-B",
			AccountID:     "account",
			FolderID:      "archive",
			UID:           destinationUIDs[0],
			MessageID:     "<different-message@example.com>",
			InboxCategory: "primary",
			Date:          time.Now(),
		})
	}
	server.mu.Unlock()

	result, err := app.RemoveFromInboxWithUndo([]string{messageA})
	if err != nil {
		t.Fatal(err)
	}
	if err := <-hookErr; err != nil {
		t.Fatalf("conflicting destination row fixture: %v", err)
	}
	select {
	case <-errorEvent:
	case <-time.After(2 * time.Second):
		t.Fatal("identity conflict did not emit folder:syncError")
	}

	aRows, err := app.messageStore.GetByIDs([]string{messageA})
	if err != nil || len(aRows) != 1 || aRows[0].FolderID != "archive" || int32(aRows[0].UID) >= 0 {
		t.Fatalf("moving A was overwritten after identity conflict: rows=%v err=%v", aRows, err)
	}
	b, err := app.messageStore.Get("message-B")
	if err != nil || b == nil || b.FolderID != "archive" || b.UID != 100 || b.MessageID != "<different-message@example.com>" {
		t.Fatalf("legitimate destination B was changed: row=%v err=%v", b, err)
	}
	if !app.undoStack.ContainsOperation(result.OperationID) {
		t.Fatal("identity conflict consumed the mutation's undo token")
	}
}

func TestMoveUIDReconcileIsIdempotentWhenLocalRowAlreadyHasFinalUID(t *testing.T) {
	app, _, messageA := undoMutationFixture(t)
	if err := app.messageStore.MoveMessages([]string{messageA}, "archive"); err != nil {
		t.Fatal(err)
	}
	first, err := app.messageStore.ReconcileMovedMessageUIDs("archive", map[string]uint32{messageA: 500})
	if err != nil || len(first) != 1 || first[0].Resolution != "updated" {
		t.Fatalf("initial reconcile: results=%v err=%v", first, err)
	}
	second, err := app.messageStore.ReconcileMovedMessageUIDs("archive", map[string]uint32{messageA: 500})
	if err != nil || len(second) != 1 || second[0].Resolution != "noop" {
		t.Fatalf("idempotent reconcile: results=%v err=%v", second, err)
	}
	message, err := app.messageStore.Get(messageA)
	if err != nil || message == nil || message.FolderID != "archive" || message.UID != 500 {
		t.Fatalf("idempotent reconcile changed final identity: message=%v err=%v", message, err)
	}
}

func TestMoveFallbackSyncStateIsConsistentAndUndoRemainsResolvable(t *testing.T) {
	app, db, messageA := undoMutationFixture(t)
	if err := app.messageStore.MoveMessages([]string{messageA}, "archive"); err != nil {
		t.Fatal(err)
	}
	// Simulate the authoritative header-sync outcome: it materializes the
	// remote UID as its own row, then fallback cleanup removes the temp row.
	if err := app.messageStore.Upsert(&message.Message{
		ID:            "sync-A",
		AccountID:     "account",
		FolderID:      "archive",
		UID:           500,
		MessageID:     "<old-search-result@example.com>",
		InboxCategory: "primary",
		Date:          time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	if err := app.messageStore.DeleteTempUIDs("archive"); err != nil {
		t.Fatal(err)
	}
	var rows, tempRows int
	if err := db.QueryRow(`SELECT COUNT(*) FROM messages WHERE folder_id = 'archive' AND uid = 500`).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM messages WHERE folder_id = 'archive' AND uid < 0`).Scan(&tempRows); err != nil {
		t.Fatal(err)
	}
	if rows != 1 || tempRows != 0 {
		t.Fatalf("fallback sync state is inconsistent: finalRows=%d tempRows=%d", rows, tempRows)
	}

	cmd := undo.NewMoveCommand(
		app,
		"account",
		[]undo.MoveMessageIdentity{{LocalID: messageA, MessageID: "<old-search-result@example.com>"}},
		"inbox",
		"archive",
		"Move to Archive",
	)
	cmd.SetOperationID("fallback-operation")
	cmd.SetTracedMove(func(messageIDs []string, destinationFolderID, operationID string) error {
		return app.runUndoMoveMutation(messageIDs, destinationFolderID, operationID)
	})
	if err := cmd.Undo(); err != nil {
		t.Fatalf("Undo after fallback sync: %v", err)
	}
	waitForMessageFolderAndPositiveUID(t, app, "sync-A", "inbox")
}

func TestMoveWithoutRFC822MessageIDStillRegistersUndo(t *testing.T) {
	app, db, messageID := undoMutationFixture(t)
	if _, err := db.Exec("UPDATE messages SET message_id = NULL WHERE id = ?", messageID); err != nil {
		t.Fatal(err)
	}
	result, err := app.RemoveFromInboxWithUndo([]string{messageID})
	if err != nil {
		t.Fatal(err)
	}
	assertUndoTokenLifecycle(t, app, result.OperationID, messageID)
}

func TestMoveOfMissingMessagesReturnsErrorInsteadOfEmptySuccess(t *testing.T) {
	app, db, messageID := undoMutationFixture(t)
	if _, err := db.Exec("DELETE FROM messages WHERE id = ?", messageID); err != nil {
		t.Fatal(err)
	}
	operationID, err := app.moveToFolder([]string{messageID}, "archive", true, "")
	if err == nil {
		t.Fatal("missing-message move returned a successful no-op")
	}
	if operationID != "" {
		t.Fatalf("missing-message move registered operation %q", operationID)
	}
}

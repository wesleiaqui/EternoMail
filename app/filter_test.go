package app

import (
	"context"
	"fmt"
	"io"
	"net"
	"path/filepath"
	"strconv"
	"strings"
	stdsync "sync"
	"testing"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapserver"
	"github.com/hkdb/aerion/internal/account"
	"github.com/hkdb/aerion/internal/database"
	"github.com/hkdb/aerion/internal/folder"
	appimap "github.com/hkdb/aerion/internal/imap"
	"github.com/hkdb/aerion/internal/mailfilter"
	"github.com/hkdb/aerion/internal/message"
	syncre "github.com/hkdb/aerion/internal/sync"
	"github.com/hkdb/aerion/internal/undo"
)

// cannedRaw is the RFC822 payload served by the fake source server for the
// message under test and expected to be APPENDed to the destination account.
const cannedRaw = "From: bob@example.com\r\nTo: me@corp.example\r\nSubject: invoice #42\r\nMessage-ID: <m1@example.com>\r\n\r\nbody line\r\n"

// filterOpsRecorder is a shared, ordered operation log so tests can assert the
// cross-server ordering of IMAP mutations (append before source deletion).
type filterOpsRecorder struct {
	mu  stdsync.Mutex
	ops []string
}

func (r *filterOpsRecorder) record(op string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ops = append(r.ops, op)
}

func (r *filterOpsRecorder) indexOf(prefix string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, op := range r.ops {
		if strings.HasPrefix(op, prefix) {
			return i
		}
	}
	return -1
}

// testIMAPSession implements imapserver.Session for testing. It stores
// messages in memory keyed by UID and records IMAP operations in the shared
// ops log.
type testIMAPSession struct {
	ops          *filterOpsRecorder
	mu           stdsync.Mutex
	messages     map[uint32][]byte // UID -> raw RFC822
	nextUID      uint32
	selected     string
	rejectAppend bool // when true, Append is rejected after recording the attempt
}

func newTestIMAPSession(ops *filterOpsRecorder) *testIMAPSession {
	return &testIMAPSession{
		ops:      ops,
		messages: make(map[uint32][]byte),
		nextUID:  100,
	}
}

func (s *testIMAPSession) Close() error { return nil }

func (s *testIMAPSession) Login(username, password string) error { return nil }

func (s *testIMAPSession) Select(mailbox string, options *imap.SelectOptions) (*imap.SelectData, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.selected = mailbox
	return &imap.SelectData{
		UIDValidity: 1,
		UIDNext:     imap.UID(s.nextUID),
		NumMessages: uint32(len(s.messages)),
	}, nil
}

func (s *testIMAPSession) Status(mailbox string, options *imap.StatusOptions) (*imap.StatusData, error) {
	return &imap.StatusData{}, nil
}

func (s *testIMAPSession) Append(mailbox string, r imap.LiteralReader, options *imap.AppendOptions) (*imap.AppendData, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.nextUID++
	s.messages[s.nextUID] = data
	s.mu.Unlock()
	s.ops.record("append:" + mailbox)
	if s.rejectAppend {
		return nil, fmt.Errorf("append rejected for test")
	}
	return &imap.AppendData{UID: imap.UID(s.nextUID)}, nil
}

// Fetch responds to FETCH commands with the stored messages. Each requested
// UID is returned with its full RFC822 content as a body section.
func (s *testIMAPSession) Fetch(w *imapserver.FetchWriter, numSet imap.NumSet, options *imap.FetchOptions) error {
	s.mu.Lock()
	messages := make(map[uint32][]byte, len(s.messages))
	for uid, raw := range s.messages {
		messages[uid] = raw
	}
	s.mu.Unlock()

	fixedDate := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)

	// Collect requested UIDs from the num set (UID FETCH or SEQ FETCH).
	var wanted []uint32
	if uidSet, ok := numSet.(imap.UIDSet); ok {
		for _, rng := range uidSet {
			for uid := rng.Start; uid <= rng.Stop; uid++ {
				wanted = append(wanted, uint32(uid))
			}
		}
	} else if seqSet, ok := numSet.(imap.SeqSet); ok {
		for _, rng := range seqSet {
			for seq := rng.Start; seq <= rng.Stop; seq++ {
				wanted = append(wanted, seq)
			}
		}
	}

	for _, uid := range wanted {
		raw, ok := messages[uid]
		if !ok {
			continue
		}
		mw := w.CreateMessage(uid)
		mw.WriteUID(imap.UID(uid))
		mw.WriteRFC822Size(int64(len(raw)))
		mw.WriteInternalDate(fixedDate)
		mw.WriteFlags(nil)
		for _, bs := range options.BodySection {
			wc := mw.WriteBodySection(bs, int64(len(raw)))
			if _, err := wc.Write(raw); err != nil {
				wc.Close()
				return err
			}
			if err := wc.Close(); err != nil {
				return err
			}
		}
		if err := mw.Close(); err != nil {
			return err
		}
	}
	return nil
}

func (s *testIMAPSession) Search(kind imapserver.NumKind, criteria *imap.SearchCriteria, options *imap.SearchOptions) (*imap.SearchData, error) {
	if kind == imapserver.NumKindUID {
		return &imap.SearchData{All: imap.UIDSet{}}, nil
	}
	return &imap.SearchData{All: imap.SeqSet{}}, nil
}

func (s *testIMAPSession) Copy(numSet imap.NumSet, dest string) (*imap.CopyData, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var wanted []uint32
	if uidSet, ok := numSet.(imap.UIDSet); ok {
		for _, rng := range uidSet {
			for uid := rng.Start; uid <= rng.Stop; uid++ {
				wanted = append(wanted, uint32(uid))
			}
		}
	} else if seqSet, ok := numSet.(imap.SeqSet); ok {
		for _, rng := range seqSet {
			for seq := rng.Start; seq <= rng.Stop; seq++ {
				wanted = append(wanted, seq)
			}
		}
	}
	var sourceUIDs, destUIDs imap.UIDSet
	for _, uid := range wanted {
		if raw, ok := s.messages[uid]; ok {
			s.nextUID++
			s.messages[s.nextUID] = raw
			sourceUIDs = append(sourceUIDs, imap.UIDRange{Start: imap.UID(uid), Stop: imap.UID(uid)})
			destUIDs = append(destUIDs, imap.UIDRange{Start: imap.UID(s.nextUID), Stop: imap.UID(s.nextUID)})
		}
	}
	s.ops.record("copy:" + dest)
	return &imap.CopyData{
		UIDValidity: 1,
		SourceUIDs:  sourceUIDs,
		DestUIDs:    destUIDs,
	}, nil
}

func (s *testIMAPSession) Store(w *imapserver.FetchWriter, numSet imap.NumSet, flags *imap.StoreFlags, options *imap.StoreOptions) error {
	return nil
}

func (s *testIMAPSession) Expunge(w *imapserver.ExpungeWriter, uids *imap.UIDSet) error {
	return nil
}

func (s *testIMAPSession) Unselect() error                                          { return nil }
func (s *testIMAPSession) Create(mailbox string, options *imap.CreateOptions) error { return nil }
func (s *testIMAPSession) Delete(mailbox string) error                              { return nil }
func (s *testIMAPSession) Rename(mailbox, newName string, options *imap.RenameOptions) error {
	return nil
}
func (s *testIMAPSession) Subscribe(mailbox string) error   { return nil }
func (s *testIMAPSession) Unsubscribe(mailbox string) error { return nil }
func (s *testIMAPSession) List(w *imapserver.ListWriter, ref string, patterns []string, options *imap.ListOptions) error {
	return nil
}
func (s *testIMAPSession) Poll(w *imapserver.UpdateWriter, allowExpunge bool) error    { return nil }
func (s *testIMAPSession) Idle(w *imapserver.UpdateWriter, stop <-chan struct{}) error { return nil }

// testIMAPServer wraps an imapserver.Server listening on a loopback port.
type testIMAPServer struct {
	server *imapserver.Server
	ln     net.Listener
	sess   *testIMAPSession
}

func newTestIMAPServer(t *testing.T, ops *filterOpsRecorder) *testIMAPServer {
	t.Helper()
	sess := newTestIMAPSession(ops)
	srv := imapserver.New(&imapserver.Options{
		NewSession: func(conn *imapserver.Conn) (imapserver.Session, *imapserver.GreetingData, error) {
			return sess, nil, nil
		},
		Caps:         imap.CapSet{imap.CapIMAP4rev1: {}},
		InsecureAuth: true,
	})
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		_ = srv.Serve(ln)
	}()
	t.Cleanup(func() { _ = srv.Close(); _ = ln.Close() })
	return &testIMAPServer{server: srv, ln: ln, sess: sess}
}

func (s *testIMAPServer) clientConfig(accountID string) (*appimap.ClientConfig, error) {
	host, portText, err := net.SplitHostPort(s.ln.Addr().String())
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
		ConnectTimeout: 5 * time.Second,
		ReadTimeout:    5 * time.Second,
		WriteTimeout:   5 * time.Second,
	}, nil
}

type filterFixture struct {
	app     *App
	db      *database.DB
	ops     *filterOpsRecorder
	serverA *testIMAPServer
	serverB *testIMAPServer
	message *message.Message
	rule    *mailfilter.Rule
}

// newFilterFixture builds a two-account App: account A (the source, with a
// real sync engine so the move path can fetch raw bytes) and account B (the
// destination). a.ctx is cancelled after 500ms so any delayed SyncFolder
// triggered by the move (e.g. the 750ms move-destination reconcile) bails out
// before it can touch Wails runtime event emission, which requires a live
// Wails context. The destination folder's sync key is pre-seeded in
// syncLastRequest so the immediate post-move destination sync is debounced.
func newFilterFixture(t *testing.T, rejectAppend bool) *filterFixture {
	t.Helper()

	db, err := database.Open(filepath.Join(t.TempDir(), "filters.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Migrate(); err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{
		"INSERT INTO accounts (id,name,email,imap_host,smtp_host,username,sync_period_days) VALUES ('acct-a','Source','source@example.com','127.0.0.1','smtp','source',30)",
		"INSERT INTO accounts (id,name,email,imap_host,smtp_host,username,sync_period_days) VALUES ('acct-b','Destination','dest@example.com','127.0.0.1','smtp','dest',30)",
		"INSERT INTO folders (id,account_id,name,path,folder_type) VALUES ('a-inbox','acct-a','Inbox','INBOX','inbox')",
		"INSERT INTO folders (id,account_id,name,path,folder_type) VALUES ('a-trash','acct-a','Trash','Trash','trash')",
		"INSERT INTO folders (id,account_id,name,path,folder_type) VALUES ('b-dest','acct-b','Dest','Dest','folder')",
	} {
		if _, err := db.Exec(query); err != nil {
			t.Fatal(err)
		}
	}

	ops := &filterOpsRecorder{}
	serverA := newTestIMAPServer(t, ops)
	serverB := newTestIMAPServer(t, ops)

	// Pool credentials dispatch by account ID to the matching fake server.
	poolCredentials := func(accountID string) (*appimap.ClientConfig, error) {
		switch accountID {
		case "acct-a":
			return serverA.clientConfig(accountID)
		default:
			return serverB.clientConfig(accountID)
		}
	}
	pool := appimap.NewPool(appimap.PoolConfig{
		MaxConnections: 2,
		IdleTimeout:    time.Minute,
		ConnectTimeout: 5 * time.Second,
		WaiterTimeout:  5 * time.Second,
	}, poolCredentials)
	t.Cleanup(pool.CloseAll)

	accountStore := account.NewStore(db)
	folderStore := folder.NewStore(db)
	messageStore := message.NewStore(db)

	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(500*time.Millisecond, cancel)
	t.Cleanup(cancel)

	app := &App{
		ctx:          ctx,
		db:           db,
		accountStore: accountStore,
		folderStore:  folderStore,
		messageStore: messageStore,
		imapPool:     pool,
		syncEngine:   syncre.NewEngine(pool, accountStore, folderStore, messageStore, message.NewAttachmentStore(db)),
		undoStack:    undo.NewStack(50, 30*time.Second),
		ownExpungeAt: make(map[string]time.Time),
		syncContexts: make(map[string]context.CancelFunc),
		syncLastRequest: map[string]time.Time{
			"acct-b:b-dest": time.Now(),
		},
		moveSyncTimers:      make(map[string]*time.Timer),
		moveSyncWaiters:     make(map[string][]*undo.MoveCompletion),
		runtimeEventEmitter: func(string, ...interface{}) {},
		filterStore:         mailfilter.NewStore(db),
	}

	// Pre-populate server A with the test message (UID 10 in the inbox).
	if err := messageStore.Create(&message.Message{
		ID:        "msg-1",
		AccountID: "acct-a",
		FolderID:  "a-inbox",
		UID:       10,
		MessageID: "<m1@example.com>",
		FromName:  "Bob",
		FromEmail: "bob@example.com",
		ToList:    `[{"name":"Me","email":"me@corp.example"}]`,
		Subject:   "invoice #42",
		Date:      time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	serverA.sess.mu.Lock()
	serverA.sess.messages[10] = []byte(cannedRaw)
	serverA.sess.mu.Unlock()

	rule := &mailfilter.Rule{
		Name:            "Route Bob's invoices",
		Enabled:         true,
		AccountID:       "acct-a",
		FromContains:    []string{"bob@example.com"},
		SubjectContains: []string{"invoice"},
		DestFolderID:    "b-dest",
	}
	if err := app.CreateFilterRule(rule); err != nil {
		t.Fatalf("CreateFilterRule failed: %v", err)
	}

	msg, err := messageStore.GetByIDs([]string{"msg-1"})
	if err != nil || len(msg) == 0 {
		t.Fatal("fixture message missing")
	}

	return &filterFixture{
		app:     app,
		db:      db,
		ops:     ops,
		serverA: serverA,
		serverB: serverB,
		message: msg[0],
		rule:    rule,
	}
}

func waitFor(t *testing.T, timeout time.Duration, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !condition() {
		t.Fatal("condition not met within timeout")
	}
}

func (f *filterFixture) messageFolderID(t *testing.T) string {
	t.Helper()
	messages, err := f.app.messageStore.GetByIDs([]string{"msg-1"})
	if err != nil || len(messages) == 0 {
		t.Fatalf("message no longer resolvable: %v", err)
	}
	return messages[0].FolderID
}

// prependAppendReject makes server B reject every APPEND. Used to test the
// safety property that a failed destination copy leaves the source untouched.
func (f *filterFixture) prependAppendReject(t *testing.T) {
	t.Helper()
	f.serverB.sess.mu.Lock()
	f.serverB.sess.rejectAppend = true
	f.serverB.sess.mu.Unlock()
}

func TestFilterRoutesIncomingMessageAcrossAccounts(t *testing.T) {
	f := newFilterFixture(t, false)

	f.app.handleNewMessageForFilters(f.message.AccountID, f.message.FolderID, f.message)

	// The cross-account move APPENDs to the destination first, then routes the
	// source through the Trash pipeline, so the local row ends up in account A's
	// trash folder. The local move is optimistic, so observing that folder
	// alone does not mean the background source-side COPY has run. Wait for
	// both before checking the remote ordering or tearing down the fixture.
	waitFor(t, 10*time.Second, func() bool {
		return f.messageFolderID(t) == "a-trash" && f.ops.indexOf("copy:") >= 0
	})

	appendIdx := f.ops.indexOf("append:")
	copyIdx := f.ops.indexOf("copy:")
	if appendIdx < 0 {
		t.Fatal("destination APPEND was never issued")
	}
	if copyIdx < 0 {
		t.Fatal("source-side copy was never issued")
	}
	if appendIdx > copyIdx {
		t.Fatalf("destination append (op %d) happened after source deletion (op %d); copy must succeed before source deletion", appendIdx, copyIdx)
	}
}

func TestFilterKeepsSourceWhenAppendFails(t *testing.T) {
	f := newFilterFixture(t, true)
	f.prependAppendReject(t)

	f.app.handleNewMessageForFilters(f.message.AccountID, f.message.FolderID, f.message)

	// Wait until the destination server has seen (and rejected) the append.
	waitFor(t, 10*time.Second, func() bool {
		return f.ops.indexOf("append:") >= 0
	})
	// Give a buggy implementation a short window to touch the source.
	time.Sleep(300 * time.Millisecond)

	if got := f.messageFolderID(t); got != "a-inbox" {
		t.Fatalf("source message moved to %q although the destination append failed", got)
	}
	if f.ops.indexOf("copy:") >= 0 {
		t.Fatal("source-side copy ran although the destination append failed")
	}
}

func TestFilterHandlerIgnoresNonMatchingMessages(t *testing.T) {
	f := newFilterFixture(t, false)

	other := &message.Message{
		ID:        "msg-2",
		AccountID: "acct-a",
		FolderID:  "a-inbox",
		UID:       11,
		MessageID: "<m2@example.com>",
		FromName:  "Carol",
		FromEmail: "carol@example.com",
		Subject:   "hello",
		Date:      time.Now().UTC(),
	}
	if err := f.app.messageStore.Create(other); err != nil {
		t.Fatal(err)
	}
	// Make the unmatched message available on the source server too.
	f.serverA.sess.mu.Lock()
	f.serverA.sess.messages[11] = []byte("From: carol@example.com\r\nSubject: hello\r\n\r\nbody\r\n")
	f.serverA.sess.mu.Unlock()

	f.app.handleNewMessageForFilters("acct-a", "a-inbox", other)
	time.Sleep(300 * time.Millisecond)

	if got := f.messageFolderID(t); got != "a-inbox" {
		t.Fatalf("unmatched message affected source folder: %q", got)
	}
	if f.ops.indexOf("append:") >= 0 {
		t.Fatal("non-matching message triggered a cross-account append")
	}
}

func TestCreateFilterRuleValidation(t *testing.T) {
	db, err := database.Open(filepath.Join(t.TempDir(), "filter-validation.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Migrate(); err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{
		"INSERT INTO accounts (id,name,email,imap_host,smtp_host,username,sync_period_days) VALUES ('acct-a','Source','source@example.com','127.0.0.1','smtp','source',30)",
		"INSERT INTO accounts (id,name,email,imap_host,smtp_host,username,sync_period_days) VALUES ('acct-b','Destination','dest@example.com','127.0.0.1','smtp','dest',30)",
		"INSERT INTO folders (id,account_id,name,path,folder_type) VALUES ('a-inbox','acct-a','Inbox','INBOX','inbox')",
		"INSERT INTO folders (id,account_id,name,path,folder_type) VALUES ('a-work','acct-a','Work','Work','folder')",
		"INSERT INTO folders (id,account_id,name,path,folder_type) VALUES ('b-dest','acct-b','Dest','Dest','folder')",
	} {
		if _, err := db.Exec(query); err != nil {
			t.Fatal(err)
		}
	}
	app := &App{
		ctx:             context.Background(),
		db:              db,
		accountStore:    account.NewStore(db),
		folderStore:     folder.NewStore(db),
		filterStore:     mailfilter.NewStore(db),
		messageStore:    message.NewStore(db),
		syncContexts:    make(map[string]context.CancelFunc),
		syncLastRequest: make(map[string]time.Time),
	}

	t.Run("same-account destination is rejected", func(t *testing.T) {
		rule := &mailfilter.Rule{Name: "bad", AccountID: "acct-a", DestFolderID: "a-work"}
		if err := app.CreateFilterRule(rule); err == nil {
			t.Fatal("expected error for same-account destination")
		}
	})

	t.Run("missing destination is rejected", func(t *testing.T) {
		rule := &mailfilter.Rule{Name: "missing", AccountID: "acct-a", DestFolderID: "nope"}
		if err := app.CreateFilterRule(rule); err == nil {
			t.Fatal("expected error for missing destination")
		}
	})

	t.Run("missing source account is rejected", func(t *testing.T) {
		rule := &mailfilter.Rule{Name: "ghost account", AccountID: "ghost", DestFolderID: "b-dest"}
		if err := app.CreateFilterRule(rule); err == nil {
			t.Fatal("expected error for unknown source account")
		}
	})

	t.Run("valid cross-account rule is persisted", func(t *testing.T) {
		rule := &mailfilter.Rule{Name: "good", AccountID: "acct-a", FromContains: []string{"x@y"}, DestFolderID: "b-dest"}
		if err := app.CreateFilterRule(rule); err != nil {
			t.Fatalf("CreateFilterRule failed: %v", err)
		}
		if rule.ID == "" {
			t.Fatal("rule ID not assigned")
		}
		rules, err := app.GetFilterRules()
		if err != nil || len(rules) != 1 || rules[0].ID != rule.ID {
			t.Fatalf("GetFilterRules round trip failed: %v %+v", err, rules)
		}
	})
}

// Package alwayson composes the existing mailbox sync engine for a small
// headless process. It deliberately reuses sync's header-first scheduler
// instead of introducing a second provider implementation.
package alwayson

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/hkdb/aerion/internal/account"
	v1 "github.com/hkdb/aerion/internal/automation/v1"
	"github.com/hkdb/aerion/internal/database"
	"github.com/hkdb/aerion/internal/draft"
	"github.com/hkdb/aerion/internal/email/replycontent"
	"github.com/hkdb/aerion/internal/folder"
	"github.com/hkdb/aerion/internal/imap"
	"github.com/hkdb/aerion/internal/logging"
	"github.com/hkdb/aerion/internal/mailfilter"
	"github.com/hkdb/aerion/internal/mailmove"
	"github.com/hkdb/aerion/internal/mailservice"
	"github.com/hkdb/aerion/internal/message"
	"github.com/hkdb/aerion/internal/oauth2"
	"github.com/hkdb/aerion/internal/replylater"
	"github.com/hkdb/aerion/internal/scheduledsend"
	mailSync "github.com/hkdb/aerion/internal/sync"
	"github.com/hkdb/aerion/internal/syncowner"
	"github.com/hkdb/aerion/internal/triage"
	"github.com/hkdb/aerion/internal/virtualfolder"
)

// IMAPConfigSource supplies account credentials without making the runtime
// responsible for choosing or storing authentication secrets.
type IMAPConfigSource func(context.Context, string) (*imap.ClientConfig, error)

// GmailTriageLabeler applies an optional Gmail provider label for queue changes.
type GmailTriageLabeler func(context.Context, string, string, string, bool) error

// SyncRuntime owns one local database handle, IMAP pool, sync engine, and
// scheduler. The caller starts it under the independently supervised helper's
// context and closes it before releasing the credential provider.
type SyncRuntime struct {
	db                          *database.DB
	accounts                    *account.Store
	messages                    *message.Store
	drafts                      *draft.Store
	pool                        *imap.Pool
	scheduler                   *mailSync.Scheduler
	mailService                 *mailservice.Service
	syncEngine                  *mailSync.Engine
	replyContent                *replycontent.Service
	attachments                 *message.AttachmentStore
	virtualFolders              *virtualfolder.Store
	replyLater                  *replylater.Store
	gmailTriage                 GmailTriageLabeler
	triage                      *triage.Store
	schemaOwner                 *syncowner.Lock
	filterOp                    mailmove.FilterOperation
	filterer                    *mailmove.IMAPFilterMover
	gmailPush                   *GmailPushConfig
	filterJobs                  *mailfilter.JobStore
	rules                       *mailfilter.Store
	folders                     *folder.Store
	filterWake                  chan struct{}
	filterStop                  chan struct{}
	syncLockPath                string
	ctxMu                       sync.RWMutex
	ctx                         context.Context
	cancel                      context.CancelFunc
	filterCancel                context.CancelFunc
	filterWG                    sync.WaitGroup
	lifecycleWG                 sync.WaitGroup
	maintenanceWG               sync.WaitGroup
	maintenanceCancel           context.CancelFunc
	triageMutationMu            sync.Mutex
	actionReceiptRecoveryOnce   sync.Once
	actionReceiptRecoveryErr    error
	contacts                    *contactsRuntime
	calendar                    *calendarReadRuntime
	oauth                       *oauth2.Manager
	started                     bool
	maintenanceOnce             sync.Once
	filterStopOnce              sync.Once
	startOnce                   sync.Once
	closeOnce                   sync.Once
	accountMutationMu           sync.Mutex
	credentialCleanup           func(string) error
	passwordMutation            func(string, v1.PasswordCredentialMutation, func() error) error
	oauthMutation               func(string, v1.OAuthCredentialMutation) error
	accountMutationFingerprint  func([]byte) []byte
	immediateSend               func(context.Context, scheduledsend.Delivery) error
	validateSender              func(string, string) error
	sharedMailboxValidate       func(context.Context, string, string) error
	sharedMailboxCredentialCopy func(string, string) error
	virtualFolderCacheInterval  time.Duration
}

const virtualFolderCacheRefreshInterval = 5 * time.Minute

// OpenSyncRuntime opens an existing Postilaatikko data directory. It refuses
// to create an empty database: the desktop must first configure accounts.
func OpenSyncRuntime(dataDirectory string, credentials IMAPConfigSource) (*SyncRuntime, error) {
	if credentials == nil {
		return nil, errors.New("always-on sync requires an IMAP credential source")
	}
	databasePath := filepath.Join(dataDirectory, "aerion.db")
	info, err := os.Stat(databasePath)
	if err != nil || !info.Mode().IsRegular() {
		return nil, errors.New("configure mail accounts in Postilaatikko before starting the local sync service")
	}
	db, err := database.Open(databasePath)
	if err != nil {
		return nil, fmt.Errorf("open local mail database: %w", err)
	}
	closeOnError := func(err error) (*SyncRuntime, error) {
		_ = db.Close()
		return nil, err
	}
	schemaLockPath := filepath.Join(dataDirectory, "mail-schema.owner.lock")
	currentVersion, err := db.CurrentSchemaVersion()
	if err != nil {
		return closeOnError(err)
	}
	maxSchemaVersion := database.MaxSchemaVersion()
	if currentVersion > maxSchemaVersion {
		return closeOnError(&database.ErrSchemaTooNew{DBVersion: currentVersion, BuildVersion: maxSchemaVersion})
	}
	var schemaOwner *syncowner.Lock
	if currentVersion == maxSchemaVersion {
		schemaOwner, err = syncowner.TryAcquireShared(schemaLockPath)
	} else {
		schemaOwner, err = syncowner.TryAcquire(schemaLockPath)
		if err == nil {
			if err = db.Migrate(); err == nil {
				err = schemaOwner.DowngradeToShared()
			}
		}
	}
	if err != nil {
		if schemaOwner != nil {
			_ = schemaOwner.Release()
		}
		return closeOnError(fmt.Errorf("prepare local mail database schema: %w", err))
	}

	accounts := account.NewStore(db)
	folders := folder.NewStore(db)
	messages := message.NewStore(db)
	drafts := draft.NewStore(db)
	attachments := message.NewAttachmentStore(db)
	virtualFolders := virtualfolder.NewStore(db)
	replyLater := replylater.NewStore(db)
	triageStore := triage.NewStore(db)
	runtime := &SyncRuntime{db: db, schemaOwner: schemaOwner, accounts: accounts, messages: messages, drafts: drafts, attachments: attachments, virtualFolders: virtualFolders, replyLater: replyLater, triage: triageStore, replyContent: replycontent.NewService(replycontent.ServiceOptions{}), ctx: context.Background(), syncLockPath: filepath.Join(dataDirectory, "mail-sync.owner.lock")}
	oauthManager := oauth2.NewManager()
	runtime.oauth = oauthManager
	contacts, contactsErr := newContactsRuntime(db, dataDirectory, accounts, oauthManager)
	if contactsErr != nil {
		if schemaOwner != nil {
			_ = schemaOwner.Release()
		}
		return closeOnError(fmt.Errorf("open contacts runtime: %w", contactsErr))
	}
	runtime.contacts = contacts
	runtime.calendar = newCalendarReadRuntime(db, dataDirectory)
	pool := imap.NewPool(imap.DefaultPoolConfig(), func(accountID string) (*imap.ClientConfig, error) {
		runtime.ctxMu.RLock()
		ctx := runtime.ctx
		runtime.ctxMu.RUnlock()
		return credentials(ctx, accountID)
	})
	engine := mailSync.NewEngine(pool, accounts, folders, messages, attachments)
	scheduler := mailSync.NewScheduler(engine, accounts, folders)
	runtime.pool, runtime.scheduler, runtime.folders, runtime.syncEngine = pool, scheduler, folders, engine
	runtime.mailService = mailservice.New(mailservice.Options{Accounts: accounts, Folders: folders, Messages: messages, ReplyLater: replyLater, Triage: triageStore, VirtualFolders: virtualFolders, Sync: engine, ReplyContent: runtime.replyContent})
	runtime.filterJobs = mailfilter.NewJobStore(db)
	runtime.rules, runtime.folders = mailfilter.NewStore(db), folders
	runtime.filterWake = make(chan struct{}, 1)
	runtime.filterStop = make(chan struct{})
	filterLog := logging.WithComponent("alwayson.filters")
	if interrupted, recoverErr := runtime.filterJobs.RecoverInterrupted(); recoverErr != nil {
		return closeOnError(fmt.Errorf("recover automatic filter jobs: %w", recoverErr))
	} else if interrupted > 0 {
		filterLog.Warn().Int64("jobs", interrupted).Msg("Previous process stopped during automatic filter moves; outcomes need inspection")
	}
	runtime.filterer = &mailmove.IMAPFilterMover{Messages: messages, Folders: folders, Raw: engine, Pool: pool}
	runtime.filterOp = mailmove.FilterOperation{Messages: messages, Move: func(ctx context.Context, _ string, source *message.Message, destination *folder.Folder) error {
		ops := &mailboxActionOperations{runtime: runtime, contextOverride: ctx}
		_, err := ops.MoveMessages([]string{source.ID}, destination.ID)
		return err
	}}
	router := mailfilter.NewAutomaticRouter(runtime.rules, folders, func(rule *mailfilter.Rule, _ *folder.Folder, msg *message.Message) { runtime.enqueueFilter(rule, msg) })
	engine.SetNewMessageHandler(func(_ string, _ string, msg *message.Message) { router.Route(msg) })
	return runtime, nil
}

// SetImmediateSendHandler wires helper-local delivery and sender validation before Start.
func (r *SyncRuntime) SetImmediateSendHandler(send func(context.Context, scheduledsend.Delivery) error, validateSender func(string, string) error) error {
	if r == nil || send == nil || validateSender == nil {
		return errors.New("helper immediate-send dependencies are required")
	}
	r.ctxMu.Lock()
	defer r.ctxMu.Unlock()
	if r.started {
		return errors.New("cannot configure immediate send after runtime start")
	}
	r.immediateSend, r.validateSender = send, validateSender
	return nil
}

// SetGmailTokenSource configures helper-local OAuth access for Gmail API
// operations. Call it before Start; credentials remain in the helper process.
func (r *SyncRuntime) SetGmailTokenSource(source mailSync.GmailTokenSource) error {
	if r == nil || r.syncEngine == nil {
		return errors.New("always-on runtime is not open")
	}
	r.ctxMu.Lock()
	defer r.ctxMu.Unlock()
	if r.started {
		return errors.New("cannot configure Gmail credentials after runtime start")
	}
	r.syncEngine.SetGmailTokenSource(source)
	return nil
}

// SetGmailTriageLabeler configures helper-local Gmail label updates before Start.
func (r *SyncRuntime) SetGmailTriageLabeler(labeler GmailTriageLabeler) error {
	if r == nil || r.replyLater == nil {
		return errors.New("always-on runtime is not open")
	}
	r.ctxMu.Lock()
	defer r.ctxMu.Unlock()
	if r.started {
		return errors.New("cannot configure Gmail triage after runtime start")
	}
	r.gmailTriage = labeler
	return nil
}

// SetAccountCredentialLifecycle wires helper-local credential cleanup and shared-mailbox setup before runtime start.
func (r *SyncRuntime) SetAccountCredentialLifecycle(cleanup func(string) error, validateShared func(context.Context, string, string) error, copyShared func(string, string) error) error {
	if r == nil || cleanup == nil || validateShared == nil || copyShared == nil {
		return errors.New("account credential lifecycle dependencies are required")
	}
	r.ctxMu.Lock()
	defer r.ctxMu.Unlock()
	if r.started {
		return errors.New("cannot configure account credential lifecycle after runtime start")
	}
	r.credentialCleanup = cleanup
	r.sharedMailboxValidate = validateShared
	r.sharedMailboxCredentialCopy = copyShared
	return nil
}

// SetAccountPasswordMutationHandler wires the helper credential transaction before runtime start.
func (r *SyncRuntime) SetAccountPasswordMutationHandler(handler func(string, v1.PasswordCredentialMutation, func() error) error, fingerprint func([]byte) []byte) error {
	if r == nil || handler == nil || fingerprint == nil {
		return errors.New("account password mutation handler and secure fingerprint are required")
	}
	r.ctxMu.Lock()
	defer r.ctxMu.Unlock()
	if r.started {
		return errors.New("cannot configure account password mutation after runtime start")
	}
	r.passwordMutation = handler
	r.accountMutationFingerprint = fingerprint
	return nil
}

// SetAccountOAuthMutationHandler wires the helper's OAuth token writer before runtime start.
func (r *SyncRuntime) SetAccountOAuthMutationHandler(handler func(string, v1.OAuthCredentialMutation) error) error {
	if r == nil || handler == nil {
		return errors.New("account OAuth mutation handler is required")
	}
	r.ctxMu.Lock()
	defer r.ctxMu.Unlock()
	if r.started {
		return errors.New("cannot configure account OAuth mutation after runtime start")
	}
	r.oauthMutation = handler
	return nil
}

// SetAccountOAuthCredentials stores OAuth callback material only in the helper.
func (r *SyncRuntime) SetAccountOAuthCredentials(accountID string, mutation v1.OAuthCredentialMutation) error {
	if r == nil || r.accounts == nil || r.oauthMutation == nil {
		return errors.New("helper OAuth credential management is unavailable")
	}
	defer mutation.ClearSecrets()
	r.accountMutationMu.Lock()
	defer r.accountMutationMu.Unlock()
	if err := mutation.Normalize(); err != nil {
		return err
	}
	acc, err := r.accounts.Get(accountID)
	if err != nil {
		return err
	}
	if acc.AuthType != account.AuthOAuth2 {
		return v1.InvalidRequest("OAuth credentials require an OAuth account", nil)
	}
	if err := r.oauthMutation(accountID, mutation); err != nil {
		return err
	}
	r.refreshAccountAfterChange(acc, true)
	return nil
}

// CreateAccount adds non-secret account configuration to the helper-owned catalog.
func (r *SyncRuntime) CreateAccount(mutation v1.AccountMutation, passwords v1.PasswordCredentialMutation) (*account.Account, error) {
	if r == nil || r.accounts == nil {
		return nil, errors.New("always-on runtime is not open")
	}
	defer passwords.ClearSecrets()
	r.accountMutationMu.Lock()
	defer r.accountMutationMu.Unlock()
	if err := passwords.Normalize(); err != nil {
		return nil, err
	}
	if passwords.HasChanges() && r.passwordMutation == nil {
		return nil, errors.New("helper password mutation is unavailable")
	}
	config := mutation.ToAccountConfig()
	if err := config.Validate(); err != nil {
		return nil, v1.InvalidRequest("Account configuration is invalid", err)
	}
	created, err := r.accounts.Create(&config)
	if err != nil {
		return nil, err
	}
	if r.passwordMutation != nil {
		if err := r.passwordMutation(created.ID, passwords, nil); err != nil {
			if errors.Is(err, v1.ErrCredentialRepairRequired) {
				disableErr := r.accounts.SetEnabled(created.ID, false)
				return nil, errors.Join(v1.ErrCredentialRepairRequired, fmt.Errorf("account was retained for password repair: %w", errors.Join(err, disableErr)))
			}
			if r.credentialCleanup == nil {
				disableErr := r.accounts.SetEnabled(created.ID, false)
				return nil, errors.Join(v1.ErrCredentialRepairRequired, fmt.Errorf("account was retained because credential cleanup is unavailable: %w", errors.Join(err, disableErr)))
			}
			if cleanupErr := r.credentialCleanup(created.ID); cleanupErr != nil {
				disableErr := r.accounts.SetEnabled(created.ID, false)
				return nil, errors.Join(v1.ErrCredentialRepairRequired, fmt.Errorf("account was retained because credential cleanup failed: %w", errors.Join(err, cleanupErr, disableErr)))
			}
			if deleteErr := r.accounts.Delete(created.ID); deleteErr != nil {
				disableErr := r.accounts.SetEnabled(created.ID, false)
				return nil, errors.Join(v1.ErrCredentialRepairRequired, fmt.Errorf("account creation failed and account cleanup failed: %w", errors.Join(err, deleteErr, disableErr)))
			}
			return nil, err
		}
	}
	r.refreshAccountAfterChange(created, true)
	return created, nil
}

// UpdateAccount changes non-secret account configuration in the helper.
func (r *SyncRuntime) UpdateAccount(id string, mutation v1.AccountMutation, passwords v1.PasswordCredentialMutation) (*account.Account, error) {
	if r == nil || r.accounts == nil || r.folders == nil {
		return nil, errors.New("always-on runtime is not open")
	}
	defer passwords.ClearSecrets()
	r.accountMutationMu.Lock()
	defer r.accountMutationMu.Unlock()
	if err := passwords.Normalize(); err != nil {
		return nil, err
	}
	if passwords.HasChanges() && r.passwordMutation == nil {
		return nil, errors.New("helper password mutation is unavailable")
	}
	config := mutation.ToAccountConfig()
	if err := config.Validate(); err != nil {
		return nil, v1.InvalidRequest("Account configuration is invalid", err)
	}
	for label, path := range map[string]string{
		"sent": config.SentFolderPath, "drafts": config.DraftsFolderPath, "trash": config.TrashFolderPath,
		"spam": config.SpamFolderPath, "archive": config.ArchiveFolderPath, "all mail": config.AllMailFolderPath,
		"starred": config.StarredFolderPath,
	} {
		if path == "" {
			continue
		}
		folder, err := r.folders.GetByPath(id, path)
		if err != nil {
			return nil, v1.InvalidRequest(fmt.Sprintf("Could not validate %s folder mapping", label), err)
		}
		if folder == nil {
			return nil, v1.InvalidRequest(fmt.Sprintf("%s folder mapping does not belong to this account", label), nil)
		}
	}
	var updated *account.Account
	var err error
	commitAccount := func() error {
		var updateErr error
		updated, updateErr = r.accounts.Update(id, &config)
		return updateErr
	}
	if r.passwordMutation != nil {
		err = r.passwordMutation(id, passwords, commitAccount)
	} else {
		err = commitAccount()
	}
	if err != nil {
		return nil, err
	}
	r.refreshAccountAfterChange(updated, true)
	return updated, nil
}

// SetAccountEnabled enables or disables an account and updates helper connections and scheduling.
func (r *SyncRuntime) SetAccountEnabled(id string, enabled bool) (*account.Account, error) {
	if r == nil || r.accounts == nil {
		return nil, errors.New("always-on runtime is not open")
	}
	r.accountMutationMu.Lock()
	defer r.accountMutationMu.Unlock()
	if err := r.accounts.SetEnabled(id, enabled); err != nil {
		return nil, err
	}
	updated, err := r.accounts.Get(id)
	if err != nil {
		return nil, err
	}
	if r.pool != nil {
		r.pool.CloseAccount(id)
	}
	if r.scheduler != nil {
		if !enabled {
			r.scheduler.CancelSync(id)
		} else {
			r.scheduler.TriggerSync(id)
		}
	}
	return updated, nil
}

// ReorderAccounts updates the full account order after validating an exact account permutation.
func (r *SyncRuntime) ReorderAccounts(ids []string) error {
	if r == nil || r.accounts == nil {
		return errors.New("always-on runtime is not open")
	}
	r.accountMutationMu.Lock()
	defer r.accountMutationMu.Unlock()
	accounts, err := r.accounts.List()
	if err != nil {
		return err
	}
	if len(ids) != len(accounts) {
		return v1.InvalidRequest("Account order must include each account exactly once", nil)
	}
	known := make(map[string]struct{}, len(accounts))
	for _, acc := range accounts {
		known[acc.ID] = struct{}{}
	}
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		if _, ok := known[id]; !ok {
			return v1.InvalidRequest("Account order contains an unknown account", nil)
		}
		if _, ok := seen[id]; ok {
			return v1.InvalidRequest("Account order contains a duplicate account", nil)
		}
		seen[id] = struct{}{}
	}
	return r.accounts.Reorder(ids)
}

// DeleteAccount removes helper-owned credentials and cascaded account data.
func (r *SyncRuntime) DeleteAccount(id string) error {
	if r == nil || r.accounts == nil {
		return errors.New("always-on runtime is not open")
	}
	r.accountMutationMu.Lock()
	defer r.accountMutationMu.Unlock()
	return r.deleteAccountTree(id)
}

func (r *SyncRuntime) deleteAccountTree(id string) error {
	acc, err := r.accounts.Get(id)
	if err != nil {
		return err
	}
	children, err := r.accounts.ListBySharedMailboxParent(id)
	if err != nil {
		return err
	}
	for _, child := range children {
		if err := r.deleteAccountTree(child.ID); err != nil {
			return fmt.Errorf("remove linked shared mailbox: %w", err)
		}
	}
	if acc.Enabled {
		if err := r.accounts.SetEnabled(id, false); err != nil {
			return fmt.Errorf("disable account before removal: %w", err)
		}
	}
	if r.scheduler != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		err := r.scheduler.CancelSyncAndWait(ctx, id)
		cancel()
		if err != nil {
			return fmt.Errorf("stop account sync before removal; account data was preserved: %w", err)
		}
	}
	if r.pool != nil {
		r.pool.CloseAccount(id)
	}
	if r.credentialCleanup == nil {
		return errors.New("helper credential cleanup is unavailable; account data was preserved")
	}
	if err := r.credentialCleanup(id); err != nil {
		return fmt.Errorf("remove account credentials; account data was preserved: %w", err)
	}
	if r.replyContent != nil {
		r.replyContent.PurgeAccount(id)
	}
	if err := r.accounts.Delete(id); err != nil {
		return fmt.Errorf("remove account data after credential cleanup: %w", err)
	}
	return nil
}

// AddSharedMailbox validates access through the primary account and creates a linked child account.
func (r *SyncRuntime) AddSharedMailbox(ctx context.Context, mutation v1.SharedMailboxMutation) (*account.Account, error) {
	if r == nil || r.accounts == nil {
		return nil, errors.New("always-on runtime is not open")
	}
	r.accountMutationMu.Lock()
	defer r.accountMutationMu.Unlock()
	email := strings.TrimSpace(mutation.Email)
	parsed, err := mail.ParseAddress(email)
	if err != nil || parsed.Address != email || !strings.Contains(email, "@") {
		return nil, v1.InvalidRequest("Shared mailbox email is invalid", err)
	}
	parent, err := r.accounts.Get(mutation.PrimaryAccountID)
	if err != nil {
		return nil, err
	}
	if parent.AuthType != account.AuthOAuth2 || r.sharedMailboxValidate == nil || r.sharedMailboxCredentialCopy == nil {
		return nil, errors.New("shared mailboxes require a configured OAuth credential lifecycle")
	}
	if err := r.sharedMailboxValidate(ctx, parent.ID, email); err != nil {
		return nil, fmt.Errorf("validate shared mailbox access: %w", err)
	}
	children, err := r.accounts.ListBySharedMailboxParent(parent.ID)
	if err != nil {
		return nil, err
	}
	for _, child := range children {
		if strings.EqualFold(child.Email, email) {
			return nil, account.ErrAccountExists
		}
	}
	displayName := strings.TrimSpace(mutation.DisplayName)
	if displayName == "" {
		displayName = email
	}
	config := account.AccountConfig{
		Name: displayName, DisplayName: displayName, Email: email, SharedMailboxParentID: parent.ID,
		IMAPHost: parent.IMAPHost, IMAPPort: parent.IMAPPort, IMAPSecurity: parent.IMAPSecurity,
		SMTPHost: parent.SMTPHost, SMTPPort: parent.SMTPPort, SMTPSecurity: parent.SMTPSecurity,
		AuthType: account.AuthOAuth2, Username: email, Color: parent.Color,
		SyncPeriodDays: parent.SyncPeriodDays, SyncInterval: parent.SyncInterval,
	}
	child, err := r.accounts.Create(&config)
	if err != nil {
		return nil, err
	}
	if err := r.sharedMailboxCredentialCopy(parent.ID, child.ID); err != nil {
		var rollbackErr error
		if r.credentialCleanup != nil {
			rollbackErr = r.credentialCleanup(child.ID)
		} else {
			rollbackErr = errors.New("helper credential cleanup is unavailable")
		}
		if rollbackErr != nil {
			return nil, fmt.Errorf("copy shared mailbox credentials failed and cleanup failed; account %s was retained: %w", child.ID, errors.Join(err, rollbackErr))
		}
		if deleteErr := r.accounts.Delete(child.ID); deleteErr != nil {
			return nil, fmt.Errorf("copy shared mailbox credentials failed and account cleanup failed; account %s was retained: %w", child.ID, errors.Join(err, deleteErr))
		}
		return nil, fmt.Errorf("copy shared mailbox credentials: %w", err)
	}
	stableID, err := r.accounts.GetOAuthStableID(parent.ID)
	if err != nil {
		return nil, r.rollbackSharedMailbox(child.ID, fmt.Errorf("read primary Microsoft identity: %w", err))
	}
	if stableID != "" {
		if err := r.accounts.SetOAuthStableID(child.ID, stableID); err != nil {
			return nil, r.rollbackSharedMailbox(child.ID, fmt.Errorf("persist shared mailbox identity: %w", err))
		}
	}
	r.refreshAccountAfterChange(child, true)
	return child, nil
}

func (r *SyncRuntime) rollbackSharedMailbox(accountID string, cause error) error {
	if r.credentialCleanup == nil {
		return fmt.Errorf("%w; account %s was retained because helper credential cleanup is unavailable", cause, accountID)
	}
	if err := r.credentialCleanup(accountID); err != nil {
		return fmt.Errorf("%w; account %s was retained because credential cleanup failed: %v", cause, accountID, err)
	}
	if err := r.accounts.Delete(accountID); err != nil {
		return fmt.Errorf("%w; account %s was retained because account cleanup failed: %v", cause, accountID, err)
	}
	return cause
}

func (r *SyncRuntime) refreshAccountAfterChange(acc *account.Account, trigger bool) {
	if acc == nil {
		return
	}
	if r.pool != nil {
		r.pool.CloseAccount(acc.ID)
	}
	if r.scheduler == nil {
		return
	}
	r.scheduler.CancelSync(acc.ID)
	if trigger && acc.Enabled {
		r.scheduler.TriggerSync(acc.ID)
	}
}

// GetReplyLaterQueue returns the persisted helper-owned Reply Later entries.
func (r *SyncRuntime) GetReplyLaterQueue() ([]replylater.Ref, error) {
	if r == nil || r.replyLater == nil {
		return nil, errors.New("always-on runtime is not open")
	}
	return r.replyLater.List()
}

// SetReplyLaterMembership updates provider labeling before helper-owned queue state.
func (r *SyncRuntime) SetReplyLaterMembership(ctx context.Context, accountID, conversationID string, inQueue bool) (bool, error) {
	if r == nil || r.replyLater == nil {
		return false, errors.New("always-on runtime is not open")
	}
	ref := replylater.Ref{AccountID: accountID, ConversationID: conversationID}
	if _, err := r.replyLater.Contains(ref); err != nil {
		return false, err
	}
	r.ctxMu.RLock()
	labeler := r.gmailTriage
	r.ctxMu.RUnlock()
	if ctx == nil {
		r.ctxMu.RLock()
		ctx = r.ctx
		r.ctxMu.RUnlock()
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if labeler != nil {
		if err := labeler(ctx, accountID, conversationID, "reply-later", inQueue); err != nil {
			return false, err
		}
	}
	if inQueue {
		return r.replyLater.Add(ref)
	}
	return r.replyLater.Remove(ref)
}

// GetReadLaterQueue lists helper-owned Read Later membership.
func (r *SyncRuntime) GetReadLaterQueue() ([]triage.Ref, error) {
	if r == nil || r.triage == nil {
		return nil, errors.New("always-on runtime is not open")
	}
	return r.triage.ListReadLater()
}

// SetReadLater updates helper-owned membership and its optional Gmail label.
func (r *SyncRuntime) SetReadLater(accountID, conversationID string, inQueue bool) (bool, error) {
	if r == nil || r.triage == nil {
		return false, errors.New("always-on runtime is not open")
	}
	r.ctxMu.RLock()
	ctx, labeler := r.ctx, r.gmailTriage
	r.ctxMu.RUnlock()
	if ctx == nil {
		ctx = context.Background()
	}
	if labeler != nil {
		if err := labeler(ctx, accountID, conversationID, "read-later", inQueue); err != nil {
			return false, err
		}
	}
	return r.triage.SetReadLater(triage.Ref{AccountID: accountID, ConversationID: conversationID}, inQueue)
}

// SnoozeConversation stores a helper-owned wake deadline.
func (r *SyncRuntime) SnoozeConversation(accountID, conversationID string, wakeAt time.Time) error {
	if r == nil || r.triage == nil {
		return errors.New("always-on runtime is not open")
	}
	return r.triage.Snooze(triage.Ref{AccountID: accountID, ConversationID: conversationID}, wakeAt)
}

// UnsnoozeConversation removes one helper-owned Snooze.
func (r *SyncRuntime) UnsnoozeConversation(accountID, conversationID string) (bool, error) {
	if r == nil || r.triage == nil {
		return false, errors.New("always-on runtime is not open")
	}
	return r.triage.Unsnooze(accountID, conversationID)
}

// GetSnoozedConversations lists active helper-owned Snoozes.
func (r *SyncRuntime) GetSnoozedConversations() ([]triage.Ref, error) {
	if r == nil || r.triage == nil {
		return nil, errors.New("always-on runtime is not open")
	}
	return r.triage.ListSnoozed()
}

// GetSnoozedCount counts active helper-owned Snoozes.
func (r *SyncRuntime) GetSnoozedCount() (int, error) {
	if r == nil || r.triage == nil {
		return 0, errors.New("always-on runtime is not open")
	}
	return r.triage.CountSnoozed()
}

// StartTriageMaintenance expires due Snoozes even when mailbox sync is disabled.
func (r *SyncRuntime) StartTriageMaintenance(ctx context.Context) error {
	if r == nil || r.triage == nil {
		return errors.New("always-on runtime is not open")
	}
	r.maintenanceOnce.Do(func() {
		if ctx == nil {
			ctx = context.Background()
		}
		maintenanceContext, cancel := context.WithCancel(ctx)
		r.ctxMu.Lock()
		r.maintenanceCancel = cancel
		r.ctxMu.Unlock()
		r.maintenanceWG.Add(1)
		go r.runTriageMaintenance(maintenanceContext)
	})
	return nil
}

func (r *SyncRuntime) runTriageMaintenance(ctx context.Context) {
	defer r.maintenanceWG.Done()
	log := logging.WithComponent("alwayson.triage")
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		r.triageMutationMu.Lock()
		_, expireErr := r.triage.ExpireSnoozes(time.Now())
		r.triageMutationMu.Unlock()
		if expireErr != nil {
			log.Warn().Err(expireErr).Msg("Failed to expire due Snoozes")
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// Start begins the existing bounded, header-first periodic sync scheduler.
func (r *SyncRuntime) Start(ctx context.Context) {
	if r == nil {
		return
	}
	r.startOnce.Do(func() {
		if ctx == nil {
			ctx = context.Background()
		}
		workerContext, cancel := context.WithCancel(ctx)
		r.ctxMu.Lock()
		r.ctx = workerContext
		r.cancel = cancel
		r.started = true
		r.ctxMu.Unlock()
		r.lifecycleWG.Add(3)
		go r.runSyncLifecycle(workerContext)
		interval := r.virtualFolderCacheInterval
		if interval <= 0 {
			interval = virtualFolderCacheRefreshInterval
		}
		go r.runVirtualFolderCacheMaintenance(workerContext, interval)
		go r.runActionUndoMaintenance(workerContext)
		if r.contacts != nil && r.contacts.scheduler != nil {
			r.contacts.scheduler.Start(workerContext)
		}
	})
}

func (r *SyncRuntime) runActionUndoMaintenance(ctx context.Context) {
	defer r.lifecycleWG.Done()
	log := logging.WithComponent("alwayson.action-undo")
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	ledger := newActionReceiptLedger(r.db)
	for {
		if err := ledger.ExpireUndoSnapshots(ctx); err != nil {
			log.Warn().Err(err).Msg("Failed to expire mail action undo snapshots")
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (r *SyncRuntime) runVirtualFolderCacheMaintenance(ctx context.Context, interval time.Duration) {
	defer r.lifecycleWG.Done()
	log := logging.WithComponent("alwayson.virtualfolder")
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if err := r.refreshAllVirtualFolderCaches(); err != nil {
			log.Warn().Err(err).Msg("Failed to refresh virtual-folder caches")
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (r *SyncRuntime) refreshAllVirtualFolderCaches() error {
	if r == nil || r.virtualFolders == nil {
		return errors.New("always-on runtime is not open")
	}
	views, err := r.virtualFolders.List()
	if err != nil {
		return fmt.Errorf("list virtual folders for cache refresh: %w", err)
	}
	log := logging.WithComponent("alwayson.virtualfolder")
	for _, view := range views {
		if err := r.refreshVirtualFolderCache(view.ID); err != nil {
			log.Warn().Err(err).Str("virtualFolderID", view.ID).Msg("Failed to refresh virtual-folder cache")
		}
	}
	return nil
}

// ConfigureGmailPush enables optional Gmail Pub/Sub receive and watch renewal.
// Configuration must be provided before Start; periodic sync remains enabled
// as the recovery path regardless of Pub/Sub availability.
func (r *SyncRuntime) ConfigureGmailPush(config GmailPushConfig) error {
	if r == nil {
		return errors.New("always-on runtime is unavailable")
	}
	if err := config.Validate(); err != nil {
		return err
	}
	r.ctxMu.Lock()
	defer r.ctxMu.Unlock()
	if r.started {
		return errors.New("Gmail Pub/Sub must be configured before the runtime starts")
	}
	copy := config
	r.gmailPush = &copy
	return nil
}

// Close stops background work before closing its IMAP and database resources.
func (r *SyncRuntime) Close() error {
	if r == nil {
		return nil
	}
	var closeErr error
	r.closeOnce.Do(func() {
		r.ctxMu.RLock()
		cancel, started := r.cancel, r.started
		maintenanceCancel := r.maintenanceCancel
		r.ctxMu.RUnlock()
		if maintenanceCancel != nil {
			maintenanceCancel()
			r.maintenanceWG.Wait()
		}
		if started && cancel != nil {
			cancel()
			r.lifecycleWG.Wait()
		} else {
			r.scheduler.Stop()
			r.stopFilterWorker()
		}
		r.pool.CloseAll()
		var contactsErr error
		if r.contacts != nil {
			contactsErr = r.contacts.Close()
		}
		calendarErr := r.calendar.Close()
		closeErr = errors.Join(r.db.Close(), r.schemaOwner.Release(), contactsErr, calendarErr)
	})
	return closeErr
}

func (r *SyncRuntime) cancelFilterWorker() {
	r.ctxMu.Lock()
	cancel := r.filterCancel
	r.filterCancel = nil
	r.ctxMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (r *SyncRuntime) runSyncLifecycle(ctx context.Context) {
	defer r.lifecycleWG.Done()
	log := logging.WithComponent("alwayson.sync")
	log.Info().Msg("Waiting for exclusive mailbox sync ownership")
	owner, err := syncowner.Acquire(ctx, r.syncLockPath)
	if err != nil {
		if ctx.Err() == nil {
			log.Error().Err(err).Msg("Could not acquire mailbox sync ownership")
		}
		return
	}
	defer func() {
		if err := owner.Release(); err != nil {
			log.Warn().Err(err).Msg("Failed to release mailbox sync ownership")
		}
	}()
	if ctx.Err() != nil {
		return
	}
	log.Info().Msg("Acquired mailbox sync ownership")
	filterContext, cancel := context.WithCancel(context.WithoutCancel(ctx))
	r.ctxMu.Lock()
	r.ctx, r.filterCancel = filterContext, cancel
	r.ctxMu.Unlock()
	r.filterWG.Add(1)
	go r.runFilterQueue(filterContext)
	r.scheduler.Start(ctx)
	var gmailPushDone chan struct{}
	r.ctxMu.RLock()
	gmailPush := r.gmailPush
	r.ctxMu.RUnlock()
	if gmailPush != nil {
		gmailPushDone = make(chan struct{})
		go func() {
			defer close(gmailPushDone)
			r.runGmailPush(ctx, *gmailPush)
		}()
	}
	<-ctx.Done()
	r.scheduler.Stop()
	r.stopFilterWorker()
	if gmailPushDone != nil {
		<-gmailPushDone
	}
}

func (r *SyncRuntime) stopFilterWorker() {
	r.filterStopOnce.Do(func() { close(r.filterStop) })
	log := logging.WithComponent("alwayson.filters")
	filterDone := make(chan struct{})
	go func() {
		r.filterWG.Wait()
		close(filterDone)
	}()
	select {
	case <-filterDone:
	case <-time.After(30 * time.Second):
		log.Warn().Msg("Filter shutdown deadline reached; canceling unfinished filter moves")
		r.cancelFilterWorker()
		<-filterDone
	}
	r.cancelFilterWorker()
}

package app

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/hkdb/aerion/internal/automation"
	automationclient "github.com/hkdb/aerion/internal/automation/client"
	v1 "github.com/hkdb/aerion/internal/automation/v1"
	"github.com/hkdb/aerion/internal/database"
	"github.com/hkdb/aerion/internal/folder"
	"github.com/hkdb/aerion/internal/keyring"
	"github.com/hkdb/aerion/internal/mailfilter"
	"github.com/hkdb/aerion/internal/message"
	"github.com/hkdb/aerion/internal/operations"
	"github.com/hkdb/aerion/internal/replylater"
	"github.com/hkdb/aerion/internal/servicebootstrap"
	"github.com/hkdb/aerion/internal/settings"
	"github.com/hkdb/aerion/internal/triage"
	"github.com/hkdb/aerion/internal/views"
	"github.com/hkdb/aerion/internal/virtualfolder"
)

const (
	desktopMailboxClientName       = "Postilaatikko Desktop Local"
	desktopMailboxSecretName       = "mailbox_service_client"
	desktopRemoteMailboxSecretName = "mailbox_service_remote_client"
	mailboxServiceEndpoint         = "http://127.0.0.1:8029"
	mailboxAPIRequestTimeout       = 2 * time.Second
	mailboxMutationTimeout         = 30 * time.Second
)

var (
	errDesktopMailboxAuthorizationRequired                  = errors.New("desktop mailbox service authorization required")
	errDesktopMailboxWriteAuthorizationRequired             = errors.New("desktop mailbox service needs explicit mail-write authorization")
	errDesktopMailboxAccountManagementAuthorizationRequired = errors.New("desktop mailbox service needs explicit account-management authorization")
)

type desktopMailboxCredential struct {
	ClientID string `json:"clientId,omitempty"`
	Token    string `json:"token"`
}

type mailboxSecretStore interface {
	GetSecret(name string) (string, error)
	SetSecret(name, secret string) error
	DeleteSecret(name string) error
}

// MailboxServiceStatus describes the local service connection without exposing
// the bearer token or raw server responses.
type MailboxServiceStatus struct {
	Connected                   bool     `json:"connected"`
	WriteAuthorized             bool     `json:"writeAuthorized"`
	AccountManagementAuthorized bool     `json:"accountManagementAuthorized"`
	DraftAccessAuthorized       bool     `json:"draftAccessAuthorized"`
	SendAuthorized              bool     `json:"sendAuthorized"`
	APIVersion                  string   `json:"apiVersion,omitempty"`
	Capabilities                []string `json:"capabilities,omitempty"`
	ErrorCode                   string   `json:"errorCode,omitempty"`
	Message                     string   `json:"message,omitempty"`
}

// MailboxServiceLifecycleStatus describes the local OS service controls.
type MailboxServiceLifecycleStatus struct {
	Supported       bool `json:"supported"`
	HelperAvailable bool `json:"helperAvailable"`
	Installed       bool `json:"installed"`
	Running         bool `json:"running"`
}

type helperAgentStatus struct {
	Supported bool `json:"supported"`
	Installed bool `json:"installed"`
	Running   bool `json:"running"`
}

type mailboxServiceConnector struct {
	mu                 sync.Mutex
	db                 *database.DB
	keyring            mailboxSecretStore
	credential         *desktopMailboxCredential
	client             *automationclient.Client
	searchMu           sync.Mutex
	actionEventSyncMu  sync.Mutex
	searches           map[string]activeMailboxSearch
	actionKeys         map[[32]byte]string
	triageKeys         map[[32]byte]string
	undoKeys           map[[32]byte]string
	resolvedActionRefs map[string]operations.MessageRef
	statusFunc         func(context.Context) MailboxServiceStatus
	capabilitiesFunc   func(context.Context) (*v1.Capabilities, error)
	searchFunc         func(context.Context, string, v1.SearchRequest) (*views.ThreadPage, error)
	cachedViewFunc     func(context.Context, string) (*v1.CachedVirtualFolderView, error)
	unreadCountsFunc   func(context.Context) (map[string]int, error)
	mutateActionFunc   func(context.Context, v1.ActionRequest, string) (*v1.ActionResponse, error)
	undoHistoryFunc    func(context.Context, int) ([]v1.ActionUndoSummary, error)
	undoLatestFunc     func(context.Context, string) (*v1.ActionUndoResult, error)
	undoOperationFunc  func(context.Context, string, string) (*v1.ActionUndoResult, error)
	endpoint           string
}

type activeMailboxSearch struct {
	ctx    context.Context
	cancel context.CancelFunc
}

func newMailboxServiceConnector(db *database.DB) *mailboxServiceConnector {
	return &mailboxServiceConnector{db: db, keyring: keyring.New()}
}

func (c *mailboxServiceConnector) getClient() (*automationclient.Client, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.client != nil {
		return c.client, nil
	}
	if c.keyring == nil {
		return nil, errors.New("mailbox service connection is not initialized")
	}

	endpoint := c.endpoint
	if endpoint == "" {
		endpoint = mailboxServiceEndpoint
	}
	validatedEndpoint, endpointErr := validateMailboxServiceEndpoint(endpoint)
	if endpointErr != nil {
		return nil, endpointErr
	}
	endpoint = validatedEndpoint
	remote := endpoint != mailboxServiceEndpoint
	var tokenStore *automation.SQLiteTokenStore
	if !remote {
		if c.db == nil {
			return nil, errors.New("local mailbox service connection requires a database")
		}
		tokenStore = automation.NewSQLiteTokenStore(c.db)
	}
	secretName := desktopMailboxSecretName
	if remote {
		secretName = desktopRemoteMailboxSecretName
	}
	secret, err := c.keyring.GetSecret(secretName)
	if err == nil {
		credential := desktopMailboxCredential{}
		if err := json.Unmarshal([]byte(secret), &credential); err != nil || credential.Token == "" || (!remote && credential.ClientID == "") {
			return nil, errDesktopMailboxAuthorizationRequired
		}
		if !remote {
			grant, grantErr := tokenStore.GetGrant(credential.Token)
			if errors.Is(grantErr, automation.ErrClientNotFound) || errors.Is(grantErr, automation.ErrTokenRevoked) {
				return nil, errDesktopMailboxAuthorizationRequired
			}
			if grantErr != nil {
				return nil, fmt.Errorf("validate desktop mailbox credential: %w", grantErr)
			}
			if grant.ClientID != credential.ClientID || !hasCapability(grant.Capabilities, v1.CapabilityMailRead) || !hasCapability(grant.Capabilities, v1.CapabilityMailBodyFetch) {
				return nil, errDesktopMailboxAuthorizationRequired
			}
		}
		c.credential = &credential
		c.client = newMailboxAPIClientAt(endpoint, credential.Token)
		return c.client, nil
	} else if !errors.Is(err, keyring.ErrCredentialNotFound) {
		return nil, err
	}
	if remote {
		return nil, errDesktopMailboxAuthorizationRequired
	}

	clients, err := tokenStore.ListClients()
	if err != nil {
		return nil, fmt.Errorf("list desktop mailbox credentials: %w", err)
	}
	for _, existing := range clients {
		if existing != nil && existing.Name == desktopMailboxClientName {
			return nil, errDesktopMailboxAuthorizationRequired
		}
	}

	grant, err := tokenStore.RegisterClient(desktopMailboxClientName, []v1.Capability{v1.CapabilityMailRead, v1.CapabilityMailBodyFetch})
	if err != nil {
		return nil, fmt.Errorf("provision desktop mailbox credential: %w", err)
	}
	encoded, err := json.Marshal(desktopMailboxCredential{ClientID: grant.ClientID, Token: grant.Token})
	if err != nil {
		_ = tokenStore.RevokeClient(grant.ClientID)
		return nil, fmt.Errorf("encode desktop mailbox credential: %w", err)
	}
	if err := c.keyring.SetSecret(desktopMailboxSecretName, string(encoded)); err != nil {
		_ = tokenStore.RevokeClient(grant.ClientID)
		return nil, err
	}
	c.credential = &desktopMailboxCredential{ClientID: grant.ClientID, Token: grant.Token}
	c.client = newMailboxAPIClient(grant.Token)
	return c.client, nil
}

func validateMailboxServiceEndpoint(endpoint string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(endpoint))
	if err != nil || parsed == nil || parsed.Hostname() == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", errors.New("enter a mailbox service URL without credentials or query parameters")
	}
	if parsed.Path != "" && parsed.Path != "/" {
		return "", errors.New("mailbox service URL must not include a path")
	}
	host := strings.ToLower(parsed.Hostname())
	loopback := host == "localhost"
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		loopback = true
	}
	if parsed.Scheme == "http" && loopback {
		return mailboxServiceEndpoint, nil
	}
	if parsed.Scheme != "https" {
		return "", errors.New("remote mailbox services require HTTPS")
	}
	return strings.TrimRight(parsed.String(), "/"), nil
}

func (c *mailboxServiceConnector) setRemoteCredential(endpoint, token string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.endpoint = endpoint
	c.credential = &desktopMailboxCredential{Token: token}
	c.client = newMailboxAPIClientAt(endpoint, token)
}

func (c *mailboxServiceConnector) setEndpoint(endpoint string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.endpoint = endpoint
	c.client = nil
	c.credential = nil
}

func (c *mailboxServiceConnector) clearCredential(endpoint string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.keyring != nil {
		if err := c.keyring.DeleteSecret(desktopRemoteMailboxSecretName); err != nil && !errors.Is(err, keyring.ErrCredentialNotFound) {
			return err
		}
	}
	c.endpoint = endpoint
	c.credential = nil
	c.client = nil
	return nil
}

func (c *mailboxServiceConnector) isRemote() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.endpoint != "" && c.endpoint != mailboxServiceEndpoint
}

func newMailboxAPIClient(token string) *automationclient.Client {
	return newMailboxAPIClientWithTimeout(token, mailboxAPIRequestTimeout)
}

func newMailboxAPIClientWithTimeout(token string, timeout time.Duration) *automationclient.Client {
	return newMailboxAPIClientAtWithTimeout(mailboxServiceEndpoint, token, timeout)
}

func newMailboxAPIClientAt(endpoint, token string) *automationclient.Client {
	return newMailboxAPIClientAtWithTimeout(endpoint, token, mailboxAPIRequestTimeout)
}

func newMailboxAPIClientAtWithTimeout(endpoint, token string, timeout time.Duration) *automationclient.Client {
	return automationclient.New(automationclient.Config{
		Endpoint: endpoint,
		Token:    token,
		Timeout:  timeout,
	})
}

func hasCapability(capabilities []v1.Capability, required v1.Capability) bool {
	for _, capability := range capabilities {
		if capability == required {
			return true
		}
	}
	return false
}

func (c *mailboxServiceConnector) status(ctx context.Context) MailboxServiceStatus {
	if c.statusFunc != nil {
		return c.statusFunc(ctx)
	}
	client, err := c.getClient()
	if err != nil {
		if errors.Is(err, errDesktopMailboxAuthorizationRequired) {
			if c.isRemote() {
				return MailboxServiceStatus{ErrorCode: "authorization_required", Message: "This desktop needs a valid device grant for the configured remote helper."}
			}
			return MailboxServiceStatus{ErrorCode: "authorization_required", Message: "Desktop access to the local mailbox service needs repair."}
		}
		return MailboxServiceStatus{ErrorCode: "credential_unavailable", Message: "Desktop service credentials are unavailable."}
	}
	c.mu.Lock()
	credential := c.credential
	c.mu.Unlock()
	if credential == nil {
		return MailboxServiceStatus{ErrorCode: "credential_unavailable", Message: "Desktop service credentials are unavailable."}
	}
	if !c.isRemote() {
		grant, err := automation.NewSQLiteTokenStore(c.db).GetGrant(credential.Token)
		if errors.Is(err, automation.ErrClientNotFound) || errors.Is(err, automation.ErrTokenRevoked) || (err == nil && grant.ClientID != credential.ClientID) {
			return MailboxServiceStatus{ErrorCode: "authorization_required", Message: "Desktop access to the local mailbox service needs repair."}
		}
		if err != nil {
			return MailboxServiceStatus{ErrorCode: "credential_unavailable", Message: "Desktop service credentials are unavailable."}
		}
	}
	statusCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	capabilities, err := c.getCapabilities(statusCtx, client)
	if err != nil {
		if c.isRemote() {
			return MailboxServiceStatus{ErrorCode: "remote_unavailable", Message: "The configured remote mailbox service is not reachable or did not authorize this device."}
		}
		return MailboxServiceStatus{ErrorCode: "service_unavailable", Message: "The local mailbox service is not reachable."}
	}
	if capabilities == nil || capabilities.Version != v1.Version || !hasCapability(capabilities.Capabilities, v1.CapabilityMailRead) || !hasCapability(capabilities.Capabilities, v1.CapabilityMailBodyFetch) {
		if c.isRemote() {
			return MailboxServiceStatus{ErrorCode: "capability_mismatch", Message: "The remote helper does not provide the required API version and read capabilities."}
		}
		return MailboxServiceStatus{ErrorCode: "capability_mismatch", Message: "The local mailbox service does not provide the required read and body-fetch capabilities."}
	}
	granted := make([]string, 0, len(capabilities.Capabilities))
	for _, capability := range capabilities.Capabilities {
		granted = append(granted, string(capability))
	}
	return MailboxServiceStatus{
		Connected:                   true,
		WriteAuthorized:             hasCapability(capabilities.Capabilities, v1.CapabilityMailWrite),
		AccountManagementAuthorized: hasCapability(capabilities.Capabilities, v1.CapabilityAccountManage),
		DraftAccessAuthorized:       hasCapability(capabilities.Capabilities, v1.CapabilityDraftRead) && hasCapability(capabilities.Capabilities, v1.CapabilityDraftWrite),
		SendAuthorized:              hasCapability(capabilities.Capabilities, v1.CapabilityMailSend),
		APIVersion:                  capabilities.Version,
		Capabilities:                granted,
	}
}

func (a *App) undoService(ctx context.Context, write bool) (*mailboxServiceConnector, bool, error) {
	if a.mailboxService == nil {
		return nil, false, nil
	}
	service := a.mailboxService
	status := service.status(ctx)
	if status.Connected {
		if write && !status.WriteAuthorized {
			return service, true, errDesktopMailboxWriteAuthorizationRequired
		}
		return service, true, nil
	}
	if service.isRemote() {
		message := status.Message
		if message == "" {
			message = "the configured remote mailbox service is unavailable"
		}
		return service, true, fmt.Errorf("remote mailbox service unavailable: %s", message)
	}
	if status.ErrorCode == "service_unavailable" {
		return service, false, nil
	}
	message := status.Message
	if message == "" {
		message = "local mailbox service authorization is unavailable"
	}
	return service, true, fmt.Errorf("mailbox service unavailable: %s", message)
}

func (a *App) rejectUnsupportedServiceMutation(ctx context.Context, message string) (bool, error) {
	if a.mailboxService == nil {
		return false, nil
	}
	_, handled, err := a.undoService(ctx, true)
	if !handled {
		return false, nil
	}
	if err != nil {
		return true, err
	}
	return true, errors.New(message)
}

func (c *mailboxServiceConnector) getCapabilities(ctx context.Context, client *automationclient.Client) (*v1.Capabilities, error) {
	if c.capabilitiesFunc != nil {
		return c.capabilitiesFunc(ctx)
	}
	return client.GetCapabilities(ctx)
}

func (c *mailboxServiceConnector) fetchMessageBody(ctx context.Context, accountID, messageID string) error {
	client, err := c.getClient()
	if err != nil {
		return err
	}
	_, err = client.ReadBody(ctx, v1.BodyReadRequest{
		Ref:  v1.MessageRef{AccountID: accountID, MessageID: messageID},
		Mode: v1.FetchExplicit,
		// The desktop reader needs the original display body, not only the
		// derived reply excerpt returned by default.
		IncludeOriginal: true,
	})
	return err
}

func (c *mailboxServiceConnector) readMessageBody(ctx context.Context, accountID, messageID string) (*message.Message, error) {
	client, err := c.getClient()
	if err != nil {
		return nil, err
	}
	msg, err := client.GetMessage(ctx, accountID, messageID)
	if err != nil {
		return nil, err
	}
	body, err := client.ReadBody(ctx, v1.BodyReadRequest{
		Ref:             v1.MessageRef{AccountID: accountID, MessageID: messageID},
		Mode:            v1.FetchExplicit,
		IncludeOriginal: true,
	})
	if err != nil {
		return nil, err
	}
	if msg == nil || body == nil || body.Original == nil {
		return nil, errors.New("mailbox service returned no original message body")
	}
	msg.BodyText = body.Original.Text
	msg.BodyHTML = body.Original.HTML
	msg.BodyFetched = body.ContentState == views.ContentCached
	if !msg.BodyFetched {
		return nil, fmt.Errorf("mailbox service body state is %s", body.ContentState)
	}
	return msg, nil
}

func (c *mailboxServiceConnector) listConversations(ctx context.Context, req v1.ConversationListRequest) (*v1.ConversationListPage, error) {
	client, err := c.getClient()
	if err != nil {
		return nil, err
	}
	return client.ListConversations(ctx, req)
}

func (c *mailboxServiceConnector) countConversations(ctx context.Context, req v1.ConversationCountRequest) (int, error) {
	client, err := c.getClient()
	if err != nil {
		return 0, err
	}
	return client.CountConversations(ctx, req)
}

func (c *mailboxServiceConnector) searchUnifiedFolder(ctx context.Context, req v1.ConversationSearchRequest) (*v1.ConversationSearchPage, error) {
	client, err := c.getClient()
	if err != nil {
		return nil, err
	}
	return client.SearchUnifiedFolder(ctx, req)
}

func (c *mailboxServiceConnector) folders(ctx context.Context, accountID string) ([]*folder.Folder, error) {
	client, err := c.getClient()
	if err != nil {
		return nil, err
	}
	summaries, err := client.GetFolders(ctx, accountID)
	if err != nil {
		return nil, err
	}
	items := make([]*folder.Folder, 0, len(summaries))
	for _, summary := range summaries {
		items = append(items, &folder.Folder{
			ID: summary.ID, AccountID: summary.AccountID, Name: summary.Name, Path: summary.Path,
			Type: folder.Type(summary.Type), ParentID: summary.ParentID,
			UIDValidity: summary.UIDValidity, UIDNext: summary.UIDNext,
			HighestModSeq: summary.HighestModSeq, FlagsSyncModSeq: summary.FlagsSyncModSeq,
			Subscribed: summary.Subscribed, Hidden: summary.Hidden,
			TotalCount: summary.TotalCount, UnreadCount: summary.UnreadCount, LastSync: summary.LastSync,
		})
	}
	return items, nil
}

func (c *mailboxServiceConnector) syncFolders(ctx context.Context, accountID string) error {
	client, err := c.getClient()
	if err != nil {
		return err
	}
	return client.SyncFolders(ctx, accountID)
}

func (c *mailboxServiceConnector) setFolderSubscription(ctx context.Context, accountID, folderID string, subscribed bool) error {
	client, err := c.getClient()
	if err != nil {
		return err
	}
	return client.SetFolderSubscription(ctx, accountID, folderID, subscribed)
}

func (c *mailboxServiceConnector) setAllFolderSubscriptions(ctx context.Context, accountID string, subscribed bool) error {
	client, err := c.getClient()
	if err != nil {
		return err
	}
	return client.SetAllFolderSubscriptions(ctx, accountID, subscribed)
}

func (c *mailboxServiceConnector) search(ctx context.Context, sessionID string, req v1.SearchRequest) (*views.ThreadPage, error) {
	if c.searchFunc != nil {
		return c.searchFunc(ctx, sessionID, req)
	}
	client, err := c.getClient()
	if err != nil {
		return nil, err
	}
	searchCtx, cancel := context.WithCancel(ctx)
	if sessionID != "" {
		c.searchMu.Lock()
		if c.searches == nil {
			c.searches = map[string]activeMailboxSearch{}
		}
		if previous, ok := c.searches[sessionID]; ok {
			previous.cancel()
		}
		c.searches[sessionID] = activeMailboxSearch{ctx: searchCtx, cancel: cancel}
		c.searchMu.Unlock()
	}
	defer func() {
		cancel()
		if sessionID == "" {
			return
		}
		c.searchMu.Lock()
		defer c.searchMu.Unlock()
		if current, ok := c.searches[sessionID]; ok && current.ctx == searchCtx {
			delete(c.searches, sessionID)
		}
	}()
	return client.Search(searchCtx, req)
}

func (c *mailboxServiceConnector) readThread(ctx context.Context, ref views.ThreadRef, scope views.Scope) (*views.ThreadView, error) {
	client, err := c.getClient()
	if err != nil {
		return nil, err
	}
	return client.ReadThread(ctx, v1.ThreadReadRequest{Ref: ref, Scope: scope})
}

func (c *mailboxServiceConnector) mutateAction(ctx context.Context, req v1.ActionRequest) (*v1.ActionResponse, error) {
	_, err := c.getClient()
	if err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("encode mailbox action for idempotency: %w", err)
	}
	requestHash := sha256.Sum256(encoded)
	c.mu.Lock()
	if c.actionKeys == nil {
		c.actionKeys = make(map[[32]byte]string)
	}
	idempotencyKey := c.actionKeys[requestHash]
	if idempotencyKey == "" {
		idempotencyKey = uuid.NewString()
	}
	c.mu.Unlock()

	var response *v1.ActionResponse
	if c.mutateActionFunc != nil {
		response, err = c.mutateActionFunc(ctx, req, idempotencyKey)
	} else {
		c.mu.Lock()
		credential := c.credential
		c.mu.Unlock()
		if credential == nil || credential.Token == "" {
			return nil, errDesktopMailboxAuthorizationRequired
		}
		mutationClient := newMailboxAPIClientWithTimeout(credential.Token, mailboxMutationTimeout)
		response, err = mutationClient.MutateAction(ctx, req, idempotencyKey)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err == nil && response != nil && (response.Receipt.State == v1.OperationUnknown || response.Receipt.State == v1.OperationSending) && !response.Partial {
		c.actionKeys[requestHash] = idempotencyKey
		return response, fmt.Errorf("action outcome is ambiguous: %s", response.Receipt.State)
	}
	if err == nil || !isAmbiguousActionError(ctx, err) {
		delete(c.actionKeys, requestHash)
		return response, err
	}
	c.actionKeys[requestHash] = idempotencyKey
	return response, err
}

func (c *mailboxServiceConnector) undoHistory(ctx context.Context, limit int) ([]v1.ActionUndoSummary, error) {
	client, err := c.getClient()
	if err != nil {
		return nil, err
	}
	if c.undoHistoryFunc != nil {
		return c.undoHistoryFunc(ctx, limit)
	}
	return client.GetUndoHistory(ctx, limit)
}

func (c *mailboxServiceConnector) resolveActionTarget(ctx context.Context, target operations.Target) ([]string, error) {
	client, err := c.getClient()
	if err != nil {
		return nil, err
	}
	refs, err := client.ResolveActionTarget(ctx, target)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	if c.resolvedActionRefs == nil {
		c.resolvedActionRefs = make(map[string]operations.MessageRef)
	}
	ids := make([]string, 0, len(refs))
	for _, ref := range refs {
		c.resolvedActionRefs[ref.MessageID] = ref
		ids = append(ids, ref.MessageID)
	}
	c.mu.Unlock()
	return ids, nil
}

func (c *mailboxServiceConnector) actionRefs(ids []string) ([]operations.MessageRef, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	refs := make([]operations.MessageRef, 0, len(ids))
	for _, id := range ids {
		ref, ok := c.resolvedActionRefs[id]
		if !ok {
			return nil, fmt.Errorf("message %q has no helper-validated account reference", id)
		}
		refs = append(refs, ref)
	}
	return refs, nil
}

func (c *mailboxServiceConnector) consumeActionRefs(ids []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, id := range ids {
		delete(c.resolvedActionRefs, id)
	}
}

func (a *App) mutateResolvedAction(ctx context.Context, ids []string, action v1.ActionKind, destination string) (*v1.ActionResponse, bool, error) {
	if a.mailboxService == nil {
		return nil, false, nil
	}
	service, handled, err := a.undoService(ctx, true)
	if !handled {
		return nil, false, nil
	}
	if err != nil {
		return nil, true, err
	}
	refs, err := service.actionRefs(ids)
	if err != nil {
		if service.isRemote() {
			return nil, true, err
		}
		messages, lookupErr := a.messageStore.GetByIDs(ids)
		if lookupErr != nil {
			return nil, true, lookupErr
		}
		if len(messages) != len(ids) {
			return nil, true, errors.New("one or more action messages are unavailable")
		}
		refs = make([]operations.MessageRef, 0, len(messages))
		for _, item := range messages {
			if item == nil || item.ID == "" || item.AccountID == "" {
				return nil, true, errors.New("action requires account-qualified message identities")
			}
			refs = append(refs, operations.MessageRef{AccountID: item.AccountID, MessageID: item.ID})
		}
	}
	response, err := service.mutateAction(ctx, v1.ActionRequest{
		Action:      action,
		Destination: destination,
		Target:      operations.Target{Kind: operations.TargetMatchedMessages, Messages: refs},
	})
	if response != nil {
		a.publishMailboxReconciliation(response.Receipt.OperationID, string(response.Receipt.State), response.Affected, response.AffectedTriage, response.Partial, response.UndoAvailable, response.Groups)
	}
	if err == nil {
		service.consumeActionRefs(ids)
		return response, true, nil
	}
	if !isAmbiguousActionError(ctx, err) {
		service.consumeActionRefs(ids)
	}
	return response, true, err
}

func (a *App) publishMailboxReconciliation(operationID, state string, affected []v1.AffectedMessageRef, affectedTriage []v1.AffectedTriageRef, partial, undoAvailable bool, groups []v1.ActionGroupOutcome) {
	if state == "sent" || state == "undone" || state == "reset_required" || partial {
		a.invalidateViewCounts()
	}
	a.emitRuntimeEvent("mail:reconciled", map[string]interface{}{
		"operationId":   operationID,
		"state":         state,
		"affected":      affected,
		"partial":       partial,
		"groups":        groups,
		"undoAvailable": undoAvailable,
	})
	a.publishTriageUndoReconciliation(affectedTriage)
	if state == "outcome_unknown" {
		a.emitRuntimeEvent("undo:history-changed")
	}
}

// SyncMailboxActionEvents replays one bounded helper event page to the active desktop.
func (a *App) SyncMailboxActionEvents() error {
	ctx := a.actionContext()
	service, handled, err := a.undoService(ctx, false)
	if !handled {
		return nil
	}
	if err != nil {
		return err
	}
	return service.syncActionEvents(ctx, func(event v1.ActionEvent) {
		a.publishMailboxReconciliation(event.OperationID, event.State, event.Affected, event.AffectedTriage, event.Partial, event.UndoAvailable, event.Groups)
	})
}

func (c *mailboxServiceConnector) syncActionEvents(ctx context.Context, dispatch func(v1.ActionEvent)) error {
	c.actionEventSyncMu.Lock()
	defer c.actionEventSyncMu.Unlock()
	client, err := c.getClient()
	if err != nil {
		return err
	}
	page, err := client.GetActionEvents(ctx, 50)
	if err != nil {
		return err
	}
	if page.ResetRequired {
		dispatch(v1.ActionEvent{State: "reset_required"})
		return client.AcknowledgeActionEvents(ctx, page.Cursor)
	}
	for _, event := range page.Events {
		dispatch(event)
	}
	if page.Cursor == 0 {
		return nil
	}
	return client.AcknowledgeActionEvents(ctx, page.Cursor)
}

func (a *App) actionContext() context.Context {
	if a.ctx != nil {
		return a.ctx
	}
	return context.Background()
}

func (a *App) publishRemoteMove(messageIDs []string, destinationFolderID string) {
	a.emitRuntimeEvent("messages:moved", map[string]interface{}{
		"messageIds":   messageIDs,
		"destFolderId": destinationFolderID,
	})
	a.emitRuntimeEvent("undo:history-changed")
}

func (c *mailboxServiceConnector) undoLatest(ctx context.Context) (*v1.ActionUndoResult, error) {
	return c.undoRequest(ctx, "latest", func(client *automationclient.Client, key string) (*v1.ActionUndoResult, error) {
		if c.undoLatestFunc != nil {
			return c.undoLatestFunc(ctx, key)
		}
		return client.UndoLatest(ctx, key)
	})
}

func (c *mailboxServiceConnector) undoOperation(ctx context.Context, operationID string) (*v1.ActionUndoResult, error) {
	return c.undoRequest(ctx, "operation:"+operationID, func(client *automationclient.Client, key string) (*v1.ActionUndoResult, error) {
		if c.undoOperationFunc != nil {
			return c.undoOperationFunc(ctx, operationID, key)
		}
		return client.UndoOperation(ctx, operationID, key)
	})
}

func (c *mailboxServiceConnector) undoRequest(ctx context.Context, identity string, call func(*automationclient.Client, string) (*v1.ActionUndoResult, error)) (*v1.ActionUndoResult, error) {
	client, err := c.getClient()
	if err != nil {
		return nil, err
	}
	requestHash := sha256.Sum256([]byte(identity))
	c.mu.Lock()
	if c.undoKeys == nil {
		c.undoKeys = make(map[[32]byte]string)
	}
	idempotencyKey := c.undoKeys[requestHash]
	if idempotencyKey == "" {
		idempotencyKey = uuid.NewString()
	}
	c.mu.Unlock()
	result, err := call(client, idempotencyKey)
	c.mu.Lock()
	defer c.mu.Unlock()
	if err == nil && result != nil && result.State != "outcome_unknown" {
		delete(c.undoKeys, requestHash)
		return result, nil
	}
	if err != nil && !isAmbiguousActionError(ctx, err) {
		delete(c.undoKeys, requestHash)
		return nil, err
	}
	c.undoKeys[requestHash] = idempotencyKey
	return result, err
}

func isAmbiguousActionError(ctx context.Context, err error) bool {
	if err == nil {
		return false
	}
	if (ctx != nil && (errors.Is(ctx.Err(), context.Canceled) || errors.Is(ctx.Err(), context.DeadlineExceeded))) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	if strings.Contains(err.Error(), "Matching action is still in progress") {
		return true
	}
	if strings.Contains(err.Error(), "action outcome is ambiguous:") {
		return true
	}
	if strings.Contains(err.Error(), "Action undo is already in progress") || strings.Contains(err.Error(), "Undo outcome is uncertain") {
		return true
	}
	var timeout interface{ Timeout() bool }
	return errors.As(err, &timeout) && timeout.Timeout()
}

func (c *mailboxServiceConnector) virtualFolders(ctx context.Context) ([]*virtualfolder.VirtualFolder, error) {
	client, err := c.getClient()
	if err != nil {
		return nil, err
	}
	return client.VirtualFolders(ctx)
}

func (c *mailboxServiceConnector) cachedVirtualFolderView(ctx context.Context, viewID string) (*v1.CachedVirtualFolderView, error) {
	if c.cachedViewFunc != nil {
		return c.cachedViewFunc(ctx, viewID)
	}
	client, err := c.getClient()
	if err != nil {
		return nil, err
	}
	return client.CachedVirtualFolderView(ctx, viewID)
}

func (c *mailboxServiceConnector) virtualFolderUnreadCounts(ctx context.Context) (map[string]int, error) {
	if c.unreadCountsFunc != nil {
		return c.unreadCountsFunc(ctx)
	}
	client, err := c.getClient()
	if err != nil {
		return nil, err
	}
	return client.VirtualFolderUnreadCounts(ctx)
}

func (c *mailboxServiceConnector) mutateVirtualFolder(ctx context.Context, req v1.VirtualFolderMutationRequest) error {
	client, err := c.getClient()
	if err != nil {
		return err
	}
	return client.MutateVirtualFolder(ctx, req)
}

func (c *mailboxServiceConnector) filterRules(ctx context.Context) ([]*mailfilter.Rule, error) {
	client, err := c.getClient()
	if err != nil {
		return nil, err
	}
	return client.GetFilterRules(ctx)
}

func (c *mailboxServiceConnector) mutateFilterRule(ctx context.Context, req v1.FilterRuleMutationRequest) error {
	client, err := c.getClient()
	if err != nil {
		return err
	}
	return client.MutateFilterRule(ctx, req)
}

func (c *mailboxServiceConnector) replyLaterQueue(ctx context.Context) ([]replylater.Ref, error) {
	client, err := c.getClient()
	if err != nil {
		return nil, err
	}
	return client.GetReplyLaterQueue(ctx)
}

func (c *mailboxServiceConnector) setReplyLaterMembership(ctx context.Context, ref replylater.Ref, inQueue bool) (bool, error) {
	response, err := c.setReplyLaterMembershipWithReceipt(ctx, ref, inQueue)
	if err != nil {
		return false, err
	}
	return response.Changed, nil
}

func (c *mailboxServiceConnector) setReplyLaterMembershipWithReceipt(ctx context.Context, ref replylater.Ref, inQueue bool) (*v1.ReplyLaterMutationResponse, error) {
	client, err := c.getClient()
	if err != nil {
		return nil, err
	}
	request := v1.ReplyLaterMutationRequest{Ref: ref, InQueue: inQueue}
	encoded, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("encode Reply Later mutation for idempotency: %w", err)
	}
	requestHash := sha256.Sum256(encoded)
	c.mu.Lock()
	if c.triageKeys == nil {
		c.triageKeys = make(map[[32]byte]string)
	}
	idempotencyKey := c.triageKeys[requestHash]
	if idempotencyKey == "" {
		idempotencyKey = uuid.NewString()
	}
	c.mu.Unlock()

	response, err := client.SetReplyLaterMembershipWithKey(ctx, ref, inQueue, idempotencyKey)
	c.mu.Lock()
	defer c.mu.Unlock()
	if err == nil && response != nil && (response.Receipt.State == v1.OperationUnknown || response.Receipt.State == v1.OperationSending) {
		c.triageKeys[requestHash] = idempotencyKey
		return response, fmt.Errorf("Reply Later outcome is ambiguous: %s", response.Receipt.State)
	}
	if err == nil || !isAmbiguousActionError(ctx, err) {
		delete(c.triageKeys, requestHash)
		return response, err
	}
	c.triageKeys[requestHash] = idempotencyKey
	return response, err
}

func (c *mailboxServiceConnector) readLaterQueue(ctx context.Context) ([]triage.Ref, error) {
	client, err := c.getClient()
	if err != nil {
		return nil, err
	}
	return client.GetReadLaterQueue(ctx)
}

func (c *mailboxServiceConnector) setReadLater(ctx context.Context, ref triage.Ref, inQueue bool) (bool, error) {
	response, err := c.setReadLaterWithReceipt(ctx, ref, inQueue)
	if err != nil {
		return false, err
	}
	return response.Changed, nil
}

func (c *mailboxServiceConnector) setReadLaterWithReceipt(ctx context.Context, ref triage.Ref, inQueue bool) (*v1.TriageMutationResponse, error) {
	client, err := c.getClient()
	if err != nil {
		return nil, err
	}
	request := v1.ReadLaterMutationRequest{Ref: ref, InQueue: inQueue}
	return c.mutateTriageWithReceipt(ctx, "/v1/triage/read-later/mutations", request, func(key string) (*v1.TriageMutationResponse, error) {
		return client.SetReadLaterWithKey(ctx, ref, inQueue, key)
	})
}

func (c *mailboxServiceConnector) setLaterWithReceipt(ctx context.Context, ref triage.Ref, inQueue bool) (*v1.TriageMutationResponse, error) {
	client, err := c.getClient()
	if err != nil {
		return nil, err
	}
	request := v1.ReadLaterMutationRequest{Ref: ref, InQueue: inQueue}
	return c.mutateTriageWithReceipt(ctx, "/v1/triage/later/mutations", request, func(key string) (*v1.TriageMutationResponse, error) {
		return client.SetLaterWithKey(ctx, ref, inQueue, key)
	})
}

func (c *mailboxServiceConnector) snoozedConversations(ctx context.Context) ([]triage.Ref, error) {
	client, err := c.getClient()
	if err != nil {
		return nil, err
	}
	return client.GetSnoozedConversations(ctx)
}

func (c *mailboxServiceConnector) snoozedCount(ctx context.Context) (int, error) {
	client, err := c.getClient()
	if err != nil {
		return 0, err
	}
	return client.GetSnoozedCount(ctx)
}

func (c *mailboxServiceConnector) snooze(ctx context.Context, ref triage.Ref, wakeAt string) error {
	_, err := c.snoozeWithReceipt(ctx, ref, wakeAt)
	return err
}

func (c *mailboxServiceConnector) snoozeWithReceipt(ctx context.Context, ref triage.Ref, wakeAt string) (*v1.TriageMutationResponse, error) {
	client, err := c.getClient()
	if err != nil {
		return nil, err
	}
	request := v1.SnoozeMutationRequest{Operation: "snooze", Ref: ref, WakeAt: wakeAt}
	return c.mutateTriageWithReceipt(ctx, "/v1/triage/snoozes/mutations", request, func(key string) (*v1.TriageMutationResponse, error) {
		return client.SnoozeConversationWithKey(ctx, ref, wakeAt, key)
	})
}

func (c *mailboxServiceConnector) unsnooze(ctx context.Context, ref triage.Ref) (bool, error) {
	response, err := c.unsnoozeWithReceipt(ctx, ref)
	if err != nil {
		return false, err
	}
	return response.Changed, nil
}

func (c *mailboxServiceConnector) unsnoozeWithReceipt(ctx context.Context, ref triage.Ref) (*v1.TriageMutationResponse, error) {
	client, err := c.getClient()
	if err != nil {
		return nil, err
	}
	request := v1.SnoozeMutationRequest{Operation: "unsnooze", Ref: ref}
	return c.mutateTriageWithReceipt(ctx, "/v1/triage/snoozes/mutations", request, func(key string) (*v1.TriageMutationResponse, error) {
		return client.UnsnoozeConversationWithKey(ctx, ref, key)
	})
}

func (c *mailboxServiceConnector) mutateTriageWithReceipt(ctx context.Context, endpoint string, request any, send func(string) (*v1.TriageMutationResponse, error)) (*v1.TriageMutationResponse, error) {
	encoded, err := json.Marshal(struct {
		Endpoint string `json:"endpoint"`
		Request  any    `json:"request"`
	}{endpoint, request})
	if err != nil {
		return nil, fmt.Errorf("encode triage mutation for idempotency: %w", err)
	}
	requestHash := sha256.Sum256(encoded)
	c.mu.Lock()
	if c.triageKeys == nil {
		c.triageKeys = make(map[[32]byte]string)
	}
	idempotencyKey := c.triageKeys[requestHash]
	if idempotencyKey == "" {
		idempotencyKey = uuid.NewString()
	}
	c.mu.Unlock()

	response, err := send(idempotencyKey)
	c.mu.Lock()
	defer c.mu.Unlock()
	if err == nil && response != nil && (response.Receipt.State == v1.OperationUnknown || response.Receipt.State == v1.OperationSending) {
		c.triageKeys[requestHash] = idempotencyKey
		return response, fmt.Errorf("triage mutation outcome is ambiguous: %s", response.Receipt.State)
	}
	if err == nil || !isAmbiguousActionError(ctx, err) {
		delete(c.triageKeys, requestHash)
		return response, err
	}
	c.triageKeys[requestHash] = idempotencyKey
	return response, err
}

func (c *mailboxServiceConnector) authorizeAccountManagement(ctx context.Context) error {
	return c.authorizeCapability(ctx, v1.CapabilityAccountManage)
}

func (c *mailboxServiceConnector) authorizeCapability(ctx context.Context, capability v1.Capability) error {
	if !capability.Known() {
		return fmt.Errorf("unknown mailbox service capability %q", capability)
	}
	client, err := c.getClient()
	if err != nil {
		return err
	}
	capabilities, err := c.getCapabilities(ctx, client)
	if err != nil {
		return fmt.Errorf("check mailbox service capabilities: %w", err)
	}
	if capabilities == nil || capabilities.Version != v1.Version || !hasCapability(capabilities.Capabilities, v1.CapabilityMailRead) || !hasCapability(capabilities.Capabilities, v1.CapabilityMailBodyFetch) {
		return errors.New("mailbox service read capabilities are unavailable")
	}
	if hasCapability(capabilities.Capabilities, capability) {
		return nil
	}
	if c.isRemote() {
		return errors.New("grant this capability to a new device client on the remote helper, then reconnect with its new token")
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.credential == nil || c.db == nil || c.keyring == nil {
		return errDesktopMailboxAuthorizationRequired
	}
	oldCredential := *c.credential
	tokenStore := automation.NewSQLiteTokenStore(c.db)
	oldGrant, err := tokenStore.GetGrant(oldCredential.Token)
	if err != nil || oldGrant.ClientID != oldCredential.ClientID {
		return errDesktopMailboxAuthorizationRequired
	}
	requestedCapabilities := append([]v1.Capability(nil), oldGrant.Capabilities...)
	if !hasCapability(requestedCapabilities, capability) {
		requestedCapabilities = append(requestedCapabilities, capability)
	}
	grant, err := tokenStore.RegisterClient(desktopMailboxClientName, requestedCapabilities)
	if err != nil {
		return fmt.Errorf("create desktop grant with %s: %w", capability, err)
	}
	encoded, err := json.Marshal(desktopMailboxCredential{ClientID: grant.ClientID, Token: grant.Token})
	if err != nil {
		_ = tokenStore.RevokeClient(grant.ClientID)
		return fmt.Errorf("encode desktop grant with %s: %w", capability, err)
	}
	if err := c.keyring.SetSecret(desktopMailboxSecretName, string(encoded)); err != nil {
		_ = tokenStore.RevokeClient(grant.ClientID)
		return fmt.Errorf("save desktop grant with %s: %w", capability, err)
	}
	if err := tokenStore.RevokeClient(oldCredential.ClientID); err != nil {
		oldEncoded, marshalErr := json.Marshal(oldCredential)
		if marshalErr == nil {
			if restoreErr := c.keyring.SetSecret(desktopMailboxSecretName, string(oldEncoded)); restoreErr == nil {
				if revokeErr := tokenStore.RevokeClient(grant.ClientID); revokeErr != nil {
					return fmt.Errorf("revoke previous desktop grant: %w; replacement grant cleanup also failed: %v", err, revokeErr)
				}
				return fmt.Errorf("revoke previous desktop grant: %w", err)
			}
		}
		// The replacement is already stored in the keyring. Keep the in-memory
		// connector aligned with it if rollback cannot restore the old secret.
		c.credential = &desktopMailboxCredential{ClientID: grant.ClientID, Token: grant.Token}
		c.client = newMailboxAPIClient(grant.Token)
		return fmt.Errorf("desktop access with %s is enabled, but the previous grant could not be revoked: %w", capability, err)
	}
	c.credential = &desktopMailboxCredential{ClientID: grant.ClientID, Token: grant.Token}
	c.client = newMailboxAPIClient(grant.Token)
	return nil
}

func (a *App) mutateFlagThroughMailboxService(ctx context.Context, messages []*message.Message, action v1.ActionKind) (bool, error) {
	_, handled, err := a.mutateMessagesThroughMailboxService(ctx, messages, v1.ActionRequest{Action: action})
	return handled, err
}

func (a *App) mutateMessagesThroughMailboxService(ctx context.Context, messages []*message.Message, req v1.ActionRequest) (*v1.ActionResponse, bool, error) {
	if a.mailboxService == nil {
		return nil, false, nil
	}
	status := a.mailboxService.status(ctx)
	if !status.Connected {
		if status.ErrorCode == "service_unavailable" {
			return nil, false, nil
		}
		return nil, true, fmt.Errorf("local mailbox service is unavailable for %s: %s", req.Action, status.Message)
	}
	if !status.WriteAuthorized {
		return nil, true, errDesktopMailboxWriteAuthorizationRequired
	}
	refs := make([]operations.MessageRef, 0, len(messages))
	for _, item := range messages {
		if item == nil {
			continue
		}
		if item.ID == "" || item.AccountID == "" {
			return nil, true, errors.New("mailbox service action requires account-qualified message identities")
		}
		refs = append(refs, operations.MessageRef{AccountID: item.AccountID, MessageID: item.ID})
	}
	if len(refs) == 0 {
		return &v1.ActionResponse{}, true, nil
	}
	req.Target = operations.Target{Kind: operations.TargetMatchedMessages, Messages: refs}
	response, err := a.mailboxService.mutateAction(ctx, req)
	if response != nil {
		a.publishMailboxReconciliation(response.Receipt.OperationID, string(response.Receipt.State), response.Affected, response.AffectedTriage, response.Partial, response.UndoAvailable, response.Groups)
	}
	if err != nil {
		return response, true, fmt.Errorf("run %s through local mailbox service: %w", req.Action, err)
	}
	return response, true, nil
}

func (c *mailboxServiceConnector) cancelSearch(sessionID string) {
	if sessionID == "" {
		return
	}
	c.searchMu.Lock()
	defer c.searchMu.Unlock()
	if active, ok := c.searches[sessionID]; ok {
		active.cancel()
		delete(c.searches, sessionID)
	}
}

// GetMailboxServiceStatus checks the authenticated localhost service without
// allowing a missing service to block application startup.
func (a *App) GetMailboxServiceStatus() MailboxServiceStatus {
	if a.mailboxService == nil {
		return MailboxServiceStatus{ErrorCode: "service_unavailable", Message: "The local mailbox service is not reachable."}
	}
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	return a.mailboxService.status(ctx)
}

// GetMailboxServiceEndpoint returns the configured endpoint without exposing credentials.
func (a *App) GetMailboxServiceEndpoint() string {
	if a.mailboxService == nil {
		return mailboxServiceEndpoint
	}
	a.mailboxService.mu.Lock()
	defer a.mailboxService.mu.Unlock()
	if a.mailboxService.endpoint == "" {
		return mailboxServiceEndpoint
	}
	return a.mailboxService.endpoint
}

// ConfigureRemoteMailboxService validates and stores a remote helper endpoint
// and one-time per-device token. The token is kept only in the OS keyring.
func (a *App) ConfigureRemoteMailboxService(endpoint, token string) (MailboxServiceStatus, error) {
	if a.mailboxService == nil || a.settingsStore == nil {
		return MailboxServiceStatus{ErrorCode: "service_unavailable"}, errors.New("mailbox service is not initialized")
	}
	validated, err := validateMailboxServiceEndpoint(endpoint)
	if err != nil {
		return MailboxServiceStatus{ErrorCode: "invalid_endpoint", Message: err.Error()}, err
	}
	if validated == mailboxServiceEndpoint || !strings.HasPrefix(validated, "https://") {
		err := errors.New("enter the remote helper's HTTPS URL")
		return MailboxServiceStatus{ErrorCode: "invalid_endpoint", Message: err.Error()}, err
	}
	token = strings.TrimSpace(token)
	if token == "" || strings.ContainsAny(token, " \t\r\n") {
		err := errors.New("enter the one-time bearer token issued for this desktop")
		return MailboxServiceStatus{ErrorCode: "invalid_token", Message: err.Error()}, err
	}
	client := newMailboxAPIClientAtWithTimeout(validated, token, mailboxAPIRequestTimeout)
	ctx := a.helperCommandContext()
	capabilities, err := client.GetCapabilities(ctx)
	if err != nil {
		return MailboxServiceStatus{ErrorCode: "remote_authorization_failed", Message: "Could not authenticate to the remote helper. Check its HTTPS certificate, URL, and device grant."}, errors.New("remote helper did not authorize this device")
	}
	if capabilities == nil || capabilities.Version != v1.Version || !hasCapability(capabilities.Capabilities, v1.CapabilityMailRead) || !hasCapability(capabilities.Capabilities, v1.CapabilityMailBodyFetch) {
		err := errors.New("remote helper grant must include the current API version, mail.read, and mail.body.fetch")
		return MailboxServiceStatus{ErrorCode: "capability_mismatch", Message: err.Error()}, err
	}
	encoded, err := json.Marshal(desktopMailboxCredential{Token: token})
	if err != nil {
		return MailboxServiceStatus{ErrorCode: "configuration_failed"}, err
	}
	oldEndpoint, err := a.settingsStore.Get(settings.KeyMailboxServiceEndpoint)
	if err != nil {
		return MailboxServiceStatus{ErrorCode: "configuration_failed"}, err
	}
	if err := a.settingsStore.Set(settings.KeyMailboxServiceEndpoint, validated); err != nil {
		return MailboxServiceStatus{ErrorCode: "configuration_failed"}, err
	}
	if err := a.mailboxService.keyring.SetSecret(desktopRemoteMailboxSecretName, string(encoded)); err != nil {
		_ = a.settingsStore.Set(settings.KeyMailboxServiceEndpoint, oldEndpoint)
		return MailboxServiceStatus{ErrorCode: "credential_storage_failed"}, errors.New("could not store remote device credential in the OS keyring")
	}
	a.mailboxService.setRemoteCredential(validated, token)
	return a.GetMailboxServiceStatus(), nil
}

// DisconnectRemoteMailboxService removes the local device credential and
// returns this desktop to its default loopback helper.
func (a *App) DisconnectRemoteMailboxService() (MailboxServiceStatus, error) {
	if a.mailboxService == nil || a.settingsStore == nil {
		return MailboxServiceStatus{ErrorCode: "service_unavailable"}, errors.New("mailbox service is not initialized")
	}
	oldEndpoint := a.GetMailboxServiceEndpoint()
	if err := a.settingsStore.Set(settings.KeyMailboxServiceEndpoint, mailboxServiceEndpoint); err != nil {
		return MailboxServiceStatus{ErrorCode: "configuration_failed"}, err
	}
	if err := a.mailboxService.clearCredential(mailboxServiceEndpoint); err != nil {
		_ = a.settingsStore.Set(settings.KeyMailboxServiceEndpoint, oldEndpoint)
		a.mailboxService.setEndpoint(oldEndpoint)
		return MailboxServiceStatus{ErrorCode: "credential_removal_failed"}, errors.New("could not remove the remote credential from the OS keyring")
	}
	return a.GetMailboxServiceStatus(), nil
}

// GetMailboxServiceLifecycleStatus reports whether the bundled helper's
// per-user service can be managed and whether its LaunchAgent is installed.
func (a *App) GetMailboxServiceLifecycleStatus() (MailboxServiceLifecycleStatus, error) {
	if runtime.GOOS != "darwin" {
		return MailboxServiceLifecycleStatus{}, nil
	}
	status := MailboxServiceLifecycleStatus{Supported: true}
	if _, err := a.mailboxHelperPath(); err != nil {
		return status, err
	}
	status.HelperAvailable = true
	output, err := a.runMailboxHelperCommand("agent-status")
	if err != nil {
		return status, err
	}
	var agentStatus helperAgentStatus
	if err := json.Unmarshal(output, &agentStatus); err != nil {
		return status, fmt.Errorf("decode helper service status: %w", err)
	}
	status.Supported = agentStatus.Supported
	status.Installed = agentStatus.Installed
	status.Running = agentStatus.Running
	return status, nil
}

// InstallMailboxService installs and starts the bundled local helper agent.
func (a *App) InstallMailboxService() (MailboxServiceLifecycleStatus, error) {
	if runtime.GOOS != "darwin" {
		return MailboxServiceLifecycleStatus{}, errors.New("local helper service installation is supported on macOS only")
	}
	if a.paths == nil || a.paths.Data == "" {
		return MailboxServiceLifecycleStatus{}, errors.New("application data directory is unavailable")
	}
	queuePath := filepath.Join(a.paths.Data, "send-later-helper")
	if _, err := a.runMailboxHelperCommand("install-agent", "--identity", "studio", "--data", a.paths.Data, "--queue", queuePath); err != nil {
		return MailboxServiceLifecycleStatus{}, err
	}
	return a.GetMailboxServiceLifecycleStatus()
}

// StartMailboxService starts an installed helper agent without changing its configuration.
func (a *App) StartMailboxService() (MailboxServiceLifecycleStatus, error) {
	if _, err := a.runMailboxHelperCommand("start-agent"); err != nil {
		return MailboxServiceLifecycleStatus{}, err
	}
	return a.GetMailboxServiceLifecycleStatus()
}

// StopMailboxService stops the helper agent while retaining its LaunchAgent configuration.
func (a *App) StopMailboxService() (MailboxServiceLifecycleStatus, error) {
	if _, err := a.runMailboxHelperCommand("stop-agent"); err != nil {
		return MailboxServiceLifecycleStatus{}, err
	}
	return a.GetMailboxServiceLifecycleStatus()
}

func (a *App) mailboxHelperPath() (string, error) {
	executable, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("locate Postilaatikko app executable: %w", err)
	}
	executable, err = filepath.EvalSymlinks(executable)
	if err != nil {
		return "", fmt.Errorf("resolve Postilaatikko app executable: %w", err)
	}
	helperName := "postilaatikko-helper"
	if runtime.GOOS == "windows" {
		helperName += ".exe"
	}
	helperPath := filepath.Join(filepath.Dir(executable), helperName)
	info, err := os.Stat(helperPath)
	if err != nil {
		return "", fmt.Errorf("locate bundled mailbox helper: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode()&0111 == 0 {
		return "", errors.New("bundled mailbox helper is not an executable file")
	}
	return helperPath, nil
}

func (a *App) bootstrapMailboxDatabase() error {
	if a.paths == nil || a.paths.Data == "" {
		return errors.New("application data directory is unavailable for mailbox service bootstrap")
	}
	output, err := a.runMailboxHelperCommand("bootstrap", "--data", a.paths.Data)
	if err != nil {
		return fmt.Errorf("prepare mailbox service database: %w", err)
	}
	var status servicebootstrap.Status
	if err := json.Unmarshal(output, &status); err != nil {
		return fmt.Errorf("decode mailbox service bootstrap response: %w", err)
	}
	if status.ProtocolVersion != servicebootstrap.ProtocolVersion || status.APIVersion != v1.Version {
		return fmt.Errorf("mailbox helper bootstrap/API version %d/%s is incompatible with desktop %d/%s", status.ProtocolVersion, status.APIVersion, servicebootstrap.ProtocolVersion, v1.Version)
	}
	if status.MaxSchemaVersion != database.MaxSchemaVersion() || status.SchemaVersion != status.MaxSchemaVersion {
		return fmt.Errorf("mailbox helper reported incompatible database schema %d/%d; desktop supports %d", status.SchemaVersion, status.MaxSchemaVersion, database.MaxSchemaVersion())
	}
	if err := a.verifyRunningMailboxService(); err != nil {
		return err
	}
	return nil
}

func (a *App) verifyRunningMailboxService() error {
	connection, err := net.DialTimeout("tcp", "127.0.0.1:8028", 300*time.Millisecond)
	if err != nil {
		var operationError *net.OpError
		if errors.As(err, &operationError) && errors.Is(operationError.Err, syscall.ECONNREFUSED) {
			return nil
		}
		return fmt.Errorf("check local mailbox helper compatibility: %w", err)
	}
	_ = connection.Close()
	client := &http.Client{Timeout: 300 * time.Millisecond, Transport: &http.Transport{Proxy: nil}}
	response, err := client.Get("http://127.0.0.1:8028/healthz")
	if err != nil {
		return fmt.Errorf("read local mailbox helper compatibility: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("running local mailbox service returned health status %d", response.StatusCode)
	}
	var status struct {
		Service string `json:"service"`
		servicebootstrap.Status
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 64<<10)).Decode(&status); err != nil {
		return fmt.Errorf("decode running mailbox service status: %w", err)
	}
	if status.Service != "mail-service" && status.Service != "send-later" {
		return errors.New("another service is listening on the local mailbox helper port")
	}
	if status.ProtocolVersion != servicebootstrap.ProtocolVersion ||
		status.APIVersion != v1.Version ||
		status.SchemaVersion != database.MaxSchemaVersion() ||
		status.MaxSchemaVersion != database.MaxSchemaVersion() {
		return fmt.Errorf("running mailbox helper is incompatible (bootstrap protocol %d, schema %d/%d); stop or update the helper and retry", status.ProtocolVersion, status.SchemaVersion, status.MaxSchemaVersion)
	}
	return nil
}

func (a *App) runMailboxHelperCommand(args ...string) ([]byte, error) {
	helperPath, err := a.mailboxHelperPath()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(a.helperCommandContext(), 30*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, helperPath, args...).CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("mailbox helper %s failed: %s: %w", args[0], strings.TrimSpace(string(output)), err)
	}
	return output, nil
}

func (a *App) helperCommandContext() context.Context {
	if a.ctx != nil {
		return a.ctx
	}
	return context.Background()
}

// EnableMailboxServiceWrites explicitly grants this desktop mailbox write operations.
func (a *App) EnableMailboxServiceWrites() (MailboxServiceStatus, error) {
	if a.mailboxService == nil {
		return MailboxServiceStatus{ErrorCode: "service_unavailable", Message: "The local mailbox service is not reachable."}, errors.New("mailbox service is not initialized")
	}
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	if err := a.mailboxService.authorizeCapability(ctx, v1.CapabilityMailWrite); err != nil {
		return a.mailboxService.status(ctx), err
	}
	return a.mailboxService.status(ctx), nil
}

// EnableMailboxServiceAccountManagement explicitly grants this desktop account and credential management access.
func (a *App) EnableMailboxServiceAccountManagement() (MailboxServiceStatus, error) {
	if a.mailboxService == nil {
		return MailboxServiceStatus{ErrorCode: "service_unavailable", Message: "The local mailbox service is not reachable."}, errors.New("mailbox service is not initialized")
	}
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	if err := a.mailboxService.authorizeAccountManagement(ctx); err != nil {
		return a.mailboxService.status(ctx), err
	}
	return a.mailboxService.status(ctx), nil
}

// EnableMailboxServiceDraftAccess explicitly grants this desktop draft read and write access.
func (a *App) EnableMailboxServiceDraftAccess() (MailboxServiceStatus, error) {
	if a.mailboxService == nil {
		return MailboxServiceStatus{ErrorCode: "service_unavailable", Message: "The local mailbox service is not reachable."}, errors.New("mailbox service is not initialized")
	}
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	if err := a.mailboxService.authorizeCapability(ctx, v1.CapabilityDraftRead); err != nil {
		return a.mailboxService.status(ctx), err
	}
	if err := a.mailboxService.authorizeCapability(ctx, v1.CapabilityDraftWrite); err != nil {
		return a.mailboxService.status(ctx), err
	}
	return a.mailboxService.status(ctx), nil
}

// EnableMailboxServiceSending explicitly grants this desktop immediate-send access.
func (a *App) EnableMailboxServiceSending() (MailboxServiceStatus, error) {
	if a.mailboxService == nil {
		return MailboxServiceStatus{ErrorCode: "service_unavailable", Message: "The local mailbox service is not reachable."}, errors.New("mailbox service is not initialized")
	}
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	if err := a.mailboxService.authorizeCapability(ctx, v1.CapabilityMailSend); err != nil {
		return a.mailboxService.status(ctx), err
	}
	return a.mailboxService.status(ctx), nil
}

func (c *mailboxServiceConnector) evaluateFilterRule(ctx context.Context, req v1.FilterRuleMutationRequest, result any) error {
	client, err := c.getClient()
	if err != nil {
		return err
	}
	return client.EvaluateFilterRule(ctx, req, result)
}

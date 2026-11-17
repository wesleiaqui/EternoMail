package alwayson

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/hkdb/aerion/internal/account"
	"github.com/hkdb/aerion/internal/automation"
	v1 "github.com/hkdb/aerion/internal/automation/v1"
	"github.com/hkdb/aerion/internal/folder"
	"github.com/hkdb/aerion/internal/gmail"
	"github.com/hkdb/aerion/internal/logging"
	"github.com/hkdb/aerion/internal/mailfilter"
	"github.com/hkdb/aerion/internal/message"
	"github.com/hkdb/aerion/internal/virtualfolder"
)

type catalogReadOperations struct {
	accounts       *account.Store
	folders        *folder.Store
	virtualFolders *virtualfolder.Store
	messages       *message.Store
}

func (o *catalogReadOperations) GetAccounts() ([]*account.Account, error) {
	return o.accounts.List()
}

func (o *catalogReadOperations) GetIdentities(accountID string) ([]*account.Identity, error) {
	if _, err := o.accounts.Get(accountID); err != nil {
		return nil, err
	}
	return o.accounts.GetIdentities(accountID)
}

func (o *catalogReadOperations) GetFolders(accountID string) ([]*folder.Folder, error) {
	return o.folders.List(accountID)
}

func (o *catalogReadOperations) GetVirtualFolders() ([]*virtualfolder.VirtualFolder, error) {
	return o.virtualFolders.List()
}

func (o *catalogReadOperations) GetFolder(folderID string) (*folder.Folder, error) {
	return o.folders.Get(folderID)
}

func (o *catalogReadOperations) ListByFolder(folderID string, offset, limit int) ([]*message.MessageHeader, error) {
	return o.messages.ListByFolder(folderID, offset, limit)
}

func (o *catalogReadOperations) ListConversationsByFolder(folderID string, offset, limit int, sortOrder, filter string) ([]*message.Conversation, error) {
	return o.messages.ListConversationsByFolder(folderID, offset, limit, sortOrder, filter)
}

func (o *catalogReadOperations) CountConversationsByFolder(accountID, folderID, filter string) (int, error) {
	f, err := o.folders.Get(folderID)
	if err != nil {
		return 0, err
	}
	if f == nil || f.AccountID != accountID {
		return 0, errors.New("folder not found for account")
	}
	return o.messages.CountConversationsByFolder(folderID, filter)
}

func (o *catalogReadOperations) ListConversationsUnifiedInbox(offset, limit int, sortOrder, filter string) ([]*message.Conversation, error) {
	folderIDs, err := resolveSpecialFolderIDs(o.accounts, o.folders, folder.TypeInbox)
	if err != nil {
		return nil, err
	}
	return o.messages.ListConversationsByFolderIDs(folderIDs, offset, limit, sortOrder, filter)
}

func (o *catalogReadOperations) ListConversationsUnifiedFolder(folderType string, offset, limit int, sortOrder, filter string) ([]*message.Conversation, error) {
	if folderType == "readLater" || folderType == "replyLater" {
		return o.messages.ListReadLaterConversations(offset, limit, sortOrder, filter)
	}
	typeValue, err := parseUnifiedFolderType(folderType)
	if err != nil {
		return nil, err
	}
	if typeValue != folder.TypeArchive {
		folderIDs, err := resolveSpecialFolderIDs(o.accounts, o.folders, typeValue)
		if err != nil {
			return nil, err
		}
		return o.messages.ListConversationsByFolderIDs(folderIDs, offset, limit, sortOrder, filter)
	}
	physicalIDs, allMailIDs, err := o.unifiedArchiveFolderIDs()
	if err != nil {
		return nil, err
	}
	window := offset + limit
	physical, err := o.messages.ListConversationsByFolderIDs(physicalIDs, 0, window, sortOrder, filter)
	if err != nil {
		return nil, err
	}
	virtual, err := o.messages.ListVirtualArchivedConversations(allMailIDs, 0, window, sortOrder, filter)
	if err != nil {
		return nil, err
	}
	combined := append(physical, virtual...)
	sort.SliceStable(combined, func(i, j int) bool {
		if combined[i].LatestDate.Equal(combined[j].LatestDate) {
			if combined[i].AccountID == combined[j].AccountID {
				return combined[i].ThreadID < combined[j].ThreadID
			}
			return combined[i].AccountID < combined[j].AccountID
		}
		if sortOrder == "oldest" {
			return combined[i].LatestDate.Before(combined[j].LatestDate)
		}
		return combined[i].LatestDate.After(combined[j].LatestDate)
	})
	if offset >= len(combined) {
		return []*message.Conversation{}, nil
	}
	end := offset + limit
	if end > len(combined) {
		end = len(combined)
	}
	return combined[offset:end], nil
}

func (o *catalogReadOperations) CountConversationsUnifiedFolder(folderType, filter string) (int, error) {
	if folderType == "readLater" || folderType == "replyLater" {
		return o.messages.CountReadLaterConversations(filter)
	}
	typeValue, err := parseUnifiedFolderType(folderType)
	if err != nil {
		return 0, err
	}
	if typeValue != folder.TypeArchive {
		folderIDs, err := resolveSpecialFolderIDs(o.accounts, o.folders, typeValue)
		if err != nil {
			return 0, err
		}
		return o.messages.CountConversationsByFolderIDs(folderIDs, filter)
	}
	physicalIDs, allMailIDs, err := o.unifiedArchiveFolderIDs()
	if err != nil {
		return 0, err
	}
	physical, err := o.messages.CountConversationsByFolderIDs(physicalIDs, filter)
	if err != nil {
		return 0, err
	}
	virtual, err := o.messages.CountVirtualArchivedConversations(allMailIDs, filter)
	return physical + virtual, err
}

func (o *catalogReadOperations) SearchConversationsUnifiedFolder(folderType, query string, offset, limit int, filter string) ([]*message.ConversationSearchResult, int, error) {
	if folderType == "readLater" || folderType == "replyLater" {
		folderIDs := []string{}
		accounts, err := o.accounts.List()
		if err != nil {
			return nil, 0, err
		}
		for _, account := range accounts {
			if !account.Enabled {
				continue
			}
			folders, err := o.folders.List(account.ID)
			if err != nil {
				return nil, 0, err
			}
			for _, folder := range folders {
				folderIDs = append(folderIDs, folder.ID)
			}
		}
		if filter == "" {
			filter = "readLater"
		} else if filter != "readLater" {
			filter = "readLater+" + filter
		}
		return o.messages.SearchConversationsByFolderIDs(folderIDs, query, offset, limit, filter)
	}
	typeValue, err := parseUnifiedFolderType(folderType)
	if err != nil {
		return nil, 0, err
	}
	if typeValue != folder.TypeArchive {
		folderIDs, err := resolveSpecialFolderIDs(o.accounts, o.folders, typeValue)
		if err != nil {
			return nil, 0, err
		}
		return o.messages.SearchConversationsByFolderIDs(folderIDs, query, offset, limit, filter)
	}
	physicalIDs, allMailIDs, err := o.unifiedArchiveFolderIDs()
	if err != nil {
		return nil, 0, err
	}
	window := offset + limit
	physical, physicalCount, err := o.messages.SearchConversationsByFolderIDs(physicalIDs, query, 0, window, filter)
	if err != nil {
		return nil, 0, err
	}
	virtual, virtualCount, err := o.messages.SearchVirtualArchivedConversations(allMailIDs, query, 0, window, filter)
	if err != nil {
		return nil, 0, err
	}
	combined := append(physical, virtual...)
	sort.SliceStable(combined, func(i, j int) bool {
		if combined[i].LatestDate.Equal(combined[j].LatestDate) {
			if combined[i].AccountID == combined[j].AccountID {
				return combined[i].ThreadID < combined[j].ThreadID
			}
			return combined[i].AccountID < combined[j].AccountID
		}
		return combined[i].LatestDate.After(combined[j].LatestDate)
	})
	if offset >= len(combined) {
		return []*message.ConversationSearchResult{}, physicalCount + virtualCount, nil
	}
	end := offset + limit
	if end > len(combined) {
		end = len(combined)
	}
	return combined[offset:end], physicalCount + virtualCount, nil
}

func (o *catalogReadOperations) unifiedArchiveFolderIDs() ([]string, []string, error) {
	accounts, err := o.accounts.List()
	if err != nil {
		return nil, nil, err
	}
	physicalIDs := make([]string, 0, len(accounts))
	allMailIDs := make([]string, 0, len(accounts))
	for _, account := range accounts {
		if account == nil || !account.Enabled {
			continue
		}
		physical, err := resolveSpecialFolder(o.accounts, o.folders, account.ID, folder.TypeArchive)
		if err != nil {
			return nil, nil, err
		}
		if physical != nil {
			physicalIDs = append(physicalIDs, physical.ID)
			continue
		}
		allMail, err := resolveSpecialFolder(o.accounts, o.folders, account.ID, folder.TypeAll)
		if err != nil {
			return nil, nil, err
		}
		if allMail != nil {
			allMailIDs = append(allMailIDs, allMail.ID)
		}
	}
	return physicalIDs, allMailIDs, nil
}

func parseUnifiedFolderType(value string) (folder.Type, error) {
	typeValue := folder.Type(value)
	switch typeValue {
	case folder.TypeDrafts, folder.TypeSent, folder.TypeTrash, folder.TypeStarred, folder.TypeArchive, folder.TypeSpam:
		return typeValue, nil
	default:
		return "", fmt.Errorf("unsupported unified folder type: %s", value)
	}
}

func (o *catalogReadOperations) CountConversationsUnifiedInbox(filter string) (int, error) {
	folderIDs, err := resolveSpecialFolderIDs(o.accounts, o.folders, folder.TypeInbox)
	if err != nil {
		return 0, err
	}
	return o.messages.CountConversationsByFolderIDs(folderIDs, filter)
}

func (o *catalogReadOperations) CountConversationsVirtualFolder(virtualFolderID string) (int, error) {
	vf, err := o.virtualFolders.Get(virtualFolderID)
	if err != nil {
		return 0, err
	}
	if vf == nil {
		return 0, errors.New("virtual folder not found")
	}
	folderIDs, err := o.virtualFolders.QueryFolderIDs(vf)
	if err != nil {
		return 0, err
	}
	criteria := message.VirtualFolderCriteria{ReadStatus: vf.ReadStatus, ReplyStatus: vf.ReplyStatus, StarStatus: vf.StarStatus, DateWindow: vf.DateWindow}
	if vf.SearchQuery != "" {
		_, count, err := o.messages.SearchVirtualFolderConversations(folderIDs, vf.SearchQuery, 0, 0, criteria)
		return count, err
	}
	return o.messages.CountVirtualFolderConversations(folderIDs, criteria)
}

func (r *SyncRuntime) CreateVirtualFolder(vf *virtualfolder.VirtualFolder) error {
	if r == nil || r.virtualFolders == nil || r.messages == nil {
		return errors.New("always-on runtime is not open")
	}
	if err := r.virtualFolders.Create(vf); err != nil {
		return err
	}
	return r.refreshVirtualFolderCache(vf.ID)
}

func (r *SyncRuntime) UpdateVirtualFolder(vf *virtualfolder.VirtualFolder) error {
	if r == nil || r.virtualFolders == nil || r.messages == nil {
		return errors.New("always-on runtime is not open")
	}
	if err := r.virtualFolders.Update(vf); err != nil {
		return err
	}
	return r.refreshVirtualFolderCache(vf.ID)
}

func (r *SyncRuntime) DeleteVirtualFolder(id string) error {
	if r == nil || r.virtualFolders == nil {
		return errors.New("always-on runtime is not open")
	}
	return r.virtualFolders.Delete(id)
}

func (r *SyncRuntime) ReorderVirtualFolders(ids []string) error {
	if r == nil || r.virtualFolders == nil {
		return errors.New("always-on runtime is not open")
	}
	return r.virtualFolders.Reorder(ids)
}

func (r *SyncRuntime) RefreshVirtualFolderCache(id string) error {
	if r == nil || r.virtualFolders == nil || r.messages == nil {
		return errors.New("always-on runtime is not open")
	}
	return r.refreshVirtualFolderCache(id)
}

func (r *SyncRuntime) GetCachedVirtualFolderView(ctx context.Context, id string) (*v1.CachedVirtualFolderView, error) {
	if r == nil || r.virtualFolders == nil || r.messages == nil {
		return nil, errors.New("always-on runtime is not open")
	}
	vf, err := r.virtualFolders.Get(id)
	if err != nil {
		return nil, fmt.Errorf("load virtual folder for cached view: %w", err)
	}
	if vf == nil {
		return nil, fmt.Errorf("%w: %s", v1.ErrCachedVirtualFolderNotFound, id)
	}
	refs, refreshedAt, found, err := r.virtualFolders.LoadCache(id)
	if err != nil {
		return nil, err
	}
	if !found {
		if err := r.refreshVirtualFolderCacheContext(ctx, id); err != nil {
			return nil, err
		}
		refs, refreshedAt, found, err = r.virtualFolders.LoadCache(id)
		if err != nil {
			return nil, err
		}
	}
	conversationRefs := make([]message.ConversationRef, 0, len(refs))
	for _, ref := range refs {
		conversationRefs = append(conversationRefs, message.ConversationRef{AccountID: ref.AccountID, ConversationID: ref.ConversationID})
	}
	conversations, err := r.messages.ListConversationsByRefs(conversationRefs)
	if err != nil {
		return nil, err
	}
	return &v1.CachedVirtualFolderView{
		VirtualFolderID: id,
		Conversations:   conversations,
		RefreshedAt:     refreshedAt,
		HasCache:        found,
	}, nil
}

func (r *SyncRuntime) GetVirtualFolderUnreadCounts() (map[string]int, error) {
	if r == nil || r.virtualFolders == nil {
		return nil, errors.New("always-on runtime is not open")
	}
	return r.virtualFolders.UnreadCounts()
}

var errProviderSearchCache = errors.New("saved search membership is maintained by provider search")

func (r *SyncRuntime) refreshVirtualFolderCache(id string) error {
	entries, err := r.virtualFolderCacheEntries(id)
	if errors.Is(err, errProviderSearchCache) {
		return nil
	}
	if err != nil {
		return err
	}
	return r.virtualFolders.SaveCache(id, entries)
}

// refreshVirtualFolderCacheContext serves detached cache-route work: no
// retry loop runs, and the cache write's lock wait is bounded by
// SaveCacheContext's lowered busy_timeout - the driver ignores context
// cancellation during the busy wait - so an abandoned refresh frees its
// pool connection within that bound.
func (r *SyncRuntime) refreshVirtualFolderCacheContext(ctx context.Context, id string) error {
	entries, err := r.virtualFolderCacheEntries(id)
	if errors.Is(err, errProviderSearchCache) {
		return nil
	}
	if err != nil {
		return err
	}
	return r.virtualFolders.SaveCacheContext(ctx, id, entries)
}

func (r *SyncRuntime) virtualFolderCacheEntries(id string) ([]virtualfolder.CachedConversation, error) {
	vf, err := r.virtualFolders.Get(id)
	if err != nil {
		return nil, fmt.Errorf("load virtual folder for cache refresh: %w", err)
	}
	if vf == nil {
		return nil, fmt.Errorf("virtual folder not found: %s", id)
	}
	folderIDs, err := r.virtualFolders.QueryFolderIDs(vf)
	if err != nil {
		return nil, fmt.Errorf("resolve virtual folder members: %w", err)
	}
	if vf.SearchQuery != "" {
		return nil, errProviderSearchCache
	}

	refs, err := r.messages.ListVirtualFolderConversationIDs(folderIDs, vf.SearchQuery, 200, message.VirtualFolderCriteria{
		ReadStatus: vf.ReadStatus, ReplyStatus: vf.ReplyStatus, StarStatus: vf.StarStatus, DateWindow: vf.DateWindow,
	})
	if err != nil {
		return nil, fmt.Errorf("query virtual folder cache members: %w", err)
	}
	entries := make([]virtualfolder.CachedConversation, 0, len(refs))
	for _, ref := range refs {
		entries = append(entries, virtualfolder.CachedConversation{AccountID: ref.AccountID, ConversationID: ref.ConversationID})
	}
	return entries, nil
}

func (r *SyncRuntime) GetFilterRules() ([]*mailfilter.Rule, error) {
	if r == nil || r.rules == nil {
		return nil, errors.New("always-on runtime is not open")
	}
	return r.rules.List()
}

func (r *SyncRuntime) CreateFilterRule(rule *mailfilter.Rule) error {
	if r == nil || r.rules == nil || r.accounts == nil || r.folders == nil {
		return errors.New("always-on runtime is not open")
	}
	if err := mailfilter.ValidateRule(rule, r.accounts, r.folders); err != nil {
		return err
	}
	if rule.OrderIndex == 0 {
		if existing, err := r.rules.List(); err == nil {
			rule.OrderIndex = len(existing)
		}
	}
	return r.rules.Create(rule)
}

func (r *SyncRuntime) UpdateFilterRule(rule *mailfilter.Rule) error {
	if r == nil || r.rules == nil || r.accounts == nil || r.folders == nil {
		return errors.New("always-on runtime is not open")
	}
	if rule == nil || rule.ID == "" {
		return errors.New("filter rule ID cannot be empty")
	}
	if err := mailfilter.ValidateRule(rule, r.accounts, r.folders); err != nil {
		return err
	}
	return r.rules.Update(rule)
}

func (r *SyncRuntime) DeleteFilterRule(id string) error {
	if r == nil || r.rules == nil {
		return errors.New("always-on runtime is not open")
	}
	return r.rules.Delete(id)
}

func (r *SyncRuntime) ReorderFilterRules(ids []string) error {
	if r == nil || r.rules == nil {
		return errors.New("always-on runtime is not open")
	}
	return r.rules.Reorder(ids)
}

// SyncFolders refreshes one account's provider folder catalog in the helper.
func (r *SyncRuntime) SyncFolders(ctx context.Context, accountID string) error {
	if r == nil || r.syncEngine == nil || r.accounts == nil {
		return errors.New("always-on runtime is not open")
	}
	if strings.TrimSpace(accountID) == "" {
		return errors.New("account ID is required")
	}
	account, err := r.accounts.Get(accountID)
	if err != nil {
		return fmt.Errorf("load account for folder sync: %w", err)
	}
	if account == nil {
		return fmt.Errorf("account not found: %s", accountID)
	}
	return r.syncEngine.SyncFolders(ctx, accountID)
}

// SetFolderSubscription applies provider subscription state and updates the helper cache.
func (r *SyncRuntime) SetFolderSubscription(ctx context.Context, accountID, folderID string, subscribed bool) error {
	if r == nil || r.syncEngine == nil || r.folders == nil || r.accounts == nil {
		return errors.New("always-on runtime is not open")
	}
	account, err := r.accounts.Get(accountID)
	if err != nil {
		return fmt.Errorf("load account for folder subscription: %w", err)
	}
	if account == nil {
		return fmt.Errorf("account not found: %s", accountID)
	}
	f, err := r.folders.Get(folderID)
	if err != nil {
		return fmt.Errorf("load folder for subscription: %w", err)
	}
	if f == nil || f.AccountID != accountID {
		return errors.New("folder not found for account")
	}

	client, apiErr := r.syncEngine.GmailClient(ctx, accountID)
	if apiErr == nil {
		label, err := r.syncEngine.GmailLabelID(ctx, client, f)
		if err != nil {
			return err
		}
		if label != "" {
			if err := client.SetLabelVisible(ctx, label, subscribed); err != nil {
				return fmt.Errorf("update Gmail folder visibility: %w", err)
			}
		}
		return r.folders.UpdateSubscribed(folderID, subscribed)
	}
	if !errors.Is(apiErr, gmail.ErrNotCapable) {
		return fmt.Errorf("prepare Gmail folder subscription: %w", apiErr)
	}

	connection, err := r.syncEngine.GetPoolConnection(ctx, accountID)
	if err != nil {
		return fmt.Errorf("get IMAP connection for folder subscription: %w", err)
	}
	defer r.syncEngine.ReleasePoolConnection(connection)
	if subscribed {
		err = connection.Client().Subscribe(f.Path)
	} else {
		err = connection.Client().Unsubscribe(f.Path)
	}
	if err != nil {
		return fmt.Errorf("update IMAP folder subscription: %w", err)
	}
	return r.folders.UpdateSubscribed(folderID, subscribed)
}

// SetAllFolderSubscriptions applies the provider-specific bulk subscription behavior.
func (r *SyncRuntime) SetAllFolderSubscriptions(ctx context.Context, accountID string, subscribed bool) error {
	if r == nil || r.syncEngine == nil || r.folders == nil || r.accounts == nil {
		return errors.New("always-on runtime is not open")
	}
	account, err := r.accounts.Get(accountID)
	if err != nil {
		return fmt.Errorf("load account for folder subscriptions: %w", err)
	}
	if account == nil {
		return fmt.Errorf("account not found: %s", accountID)
	}
	folders, err := r.folders.List(accountID)
	if err != nil {
		return fmt.Errorf("list folders for account: %w", err)
	}
	log := logging.WithComponent("alwayson.folders")
	client, apiErr := r.syncEngine.GmailClient(ctx, accountID)
	if apiErr == nil {
		for _, f := range folders {
			label, err := r.syncEngine.GmailLabelID(ctx, client, f)
			if err != nil {
				return err
			}
			if label != "" {
				if err := client.SetLabelVisible(ctx, label, subscribed); err != nil {
					return fmt.Errorf("update Gmail folder visibility: %w", err)
				}
			}
			if err := r.folders.UpdateSubscribed(f.ID, subscribed); err != nil {
				return err
			}
		}
		return nil
	}
	if !errors.Is(apiErr, gmail.ErrNotCapable) {
		return fmt.Errorf("prepare Gmail folder subscriptions: %w", apiErr)
	}
	connection, err := r.syncEngine.GetPoolConnection(ctx, accountID)
	if err != nil {
		return fmt.Errorf("get IMAP connection for folder subscriptions: %w", err)
	}
	defer r.syncEngine.ReleasePoolConnection(connection)
	for _, f := range folders {
		if subscribed {
			err = connection.Client().Subscribe(f.Path)
		} else {
			err = connection.Client().Unsubscribe(f.Path)
		}
		if err != nil {
			log.Warn().Err(err).Str("folder", f.Path).Msg("Failed to update IMAP folder subscription")
			continue
		}
		if err := r.folders.UpdateSubscribed(f.ID, subscribed); err != nil {
			log.Warn().Err(err).Str("folder", f.Path).Msg("Failed to update folder subscription cache")
		}
	}
	return nil
}

// ListUncertainFilterJobs returns durable automatic moves for inspection only.
func (r *SyncRuntime) ListUncertainFilterJobs(offset, limit int) ([]mailfilter.FilterJobInspection, error) {
	if r == nil || r.filterJobs == nil {
		return nil, errors.New("always-on runtime is not open")
	}
	return r.filterJobs.ListOutcomeUnknown(offset, limit)
}

// CatalogAPIHandler is kept as the compatible constructor for the mailbox read
// API handler. Mount the result under the authenticated server's /v1/ prefix.
func (r *SyncRuntime) CatalogAPIHandler() (http.Handler, error) {
	return r.MailboxReadAPIHandler()
}

// MailboxReadAPIHandler creates the headless account, folder, saved-view, and
// bounded message-header routes. Mount it under the authenticated automation
// server's /v1/ prefix; this handler has no listener or authentication layer.
func (r *SyncRuntime) MailboxReadAPIHandler() (http.Handler, error) {
	if r == nil || r.db == nil || r.mailService == nil || r.syncEngine == nil {
		return nil, errors.New("always-on runtime is not open")
	}
	actionReceipts := newActionReceiptLedger(r.db)
	r.actionReceiptRecoveryOnce.Do(func() {
		r.actionReceiptRecoveryErr = actionReceipts.RecoverInterrupted(context.Background())
	})
	if r.actionReceiptRecoveryErr != nil {
		return nil, fmt.Errorf("recover action receipts: %w", r.actionReceiptRecoveryErr)
	}
	operations := &catalogReadOperations{
		accounts:       r.accounts,
		folders:        r.folders,
		virtualFolders: r.virtualFolders,
		messages:       r.messages,
	}
	readOperations := &mailboxReadOperations{runtime: r}
	mailboxActions := &mailboxActionOperations{runtime: r}
	mux := http.NewServeMux()
	v1.MountCatalogRoutes(mux, operations, automation.RequireGrantCapability)
	v1.MountVirtualFolderCacheRoute(mux, r, automation.RequireGrantCapability)
	v1.MountVirtualFolderUnreadCountRoute(mux, r, automation.RequireGrantCapability)
	v1.NewAccountMutationHandler(r, newAccountMutationLedger(r.db), r.accountMutationFingerprint).MountRoutes(mux, automation.RequireGrantCapability)
	v1.MountFolderMutationRoutes(mux, r, automation.RequireGrantCapability)
	v1.MountFolderVisibilityRoutes(mux, r, automation.RequireGrantCapability)
	v1.MountVirtualFolderMutationRoutes(mux, r, automation.RequireGrantCapability)
	v1.MountFilterRuleRoutes(mux, r, automation.RequireGrantCapability)
	v1.MountFilterJobRoutes(mux, r, automation.RequireGrantCapability)
	v1.NewDraftsHandler(r, r.accountMutationFingerprint).MountRoutes(mux, automation.RequireGrantCapability)
	v1.MountReplyLaterRoutes(mux, r, automation.RequireGrantCapability)
	v1.MountTriageRoutes(mux, r, automation.RequireGrantCapability)
	v1.NewTriageMutationsHandler(r, actionReceipts,
		func(key string) string { return actionReceipts.digestKey(key, r.accountMutationFingerprint) },
	).MountRoutes(mux, automation.RequireGrantCapability)
	v1.MountMessageHeaderRoutes(mux, operations, automation.RequireGrantCapability)
	v1.MountConversationListRoute(mux, operations, automation.RequireGrantCapability)
	v1.MountConversationCountRoute(mux, operations, automation.RequireGrantCapability)
	v1.MountConversationSearchRoute(mux, operations, automation.RequireGrantCapability)
	v1.MountSearchRoute(mux, readOperations, automation.RequireGrantCapability)
	v1.MountThreadBodyRoutes(mux, readOperations, automation.RequireGrantCapability)
	v1.MountActionTargetRoute(mux, mailboxActions, automation.RequireGrantCapability)
	v1.NewActionsHandlerWithReceiptStore(mailboxActions, actionReceipts,
		func(key string) string { return actionReceipts.digestKey(key, r.accountMutationFingerprint) },
	).MountRoutes(mux, automation.RequireGrantCapability)
	v1.MountActionUndoRoutes(mux, actionReceipts, mailboxActions,
		func(key string) string { return actionReceipts.digestKey(key, r.accountMutationFingerprint) },
		automation.RequireGrantCapability,
	)
	v1.MountActionEventRoutes(mux, actionReceipts, func(request *http.Request) string {
		grant := automation.GetClientGrant(request.Context())
		if grant == nil {
			return ""
		}
		return grant.ClientID
	}, automation.RequireGrantCapability)
	// The remote contacts surface shares this authenticated /v1 router; without
	// this mount every desktop contacts call in remote service mode 404s.
	if r.contacts != nil {
		v1.MountContactsRoutes(mux, &contactsOperations{runtime: r.contacts}, newContactsMutationLedger(r.db), r.accountMutationFingerprint, automation.RequireGrantCapability)
	}
	if r.calendar != nil {
		v1.MountCalendarReadRoutes(mux, r.calendar, automation.RequireGrantCapability)
	}
	return mux, nil
}

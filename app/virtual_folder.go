package app

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/hkdb/aerion/internal/account"
	v1 "github.com/hkdb/aerion/internal/automation/v1"
	"github.com/hkdb/aerion/internal/database"
	"github.com/hkdb/aerion/internal/logging"
	"github.com/hkdb/aerion/internal/message"
	"github.com/hkdb/aerion/internal/views"
	"github.com/hkdb/aerion/internal/virtualfolder"
)

// ============================================================================
// Virtual Folder API - Exposed to frontend via Wails bindings
// ============================================================================

// GetVirtualFolders returns all virtual folders
func (a *App) GetVirtualFolders() ([]*virtualfolder.VirtualFolder, error) {
	if a.mailboxService != nil {
		ctx := a.helperCommandContext()
		status := a.mailboxService.status(ctx)
		if status.Connected {
			return a.mailboxService.virtualFolders(ctx)
		}
		if status.ErrorCode != "service_unavailable" {
			return nil, fmt.Errorf("load virtual folders from mailbox service: %s", status.Message)
		}
	}
	return a.virtualFolderStore.List()
}

// GetVirtualFolderUnreadCounts returns unread conversation counts for all virtual folders.
func (a *App) GetVirtualFolderUnreadCounts() (map[string]int, error) {
	if a.mailboxService != nil {
		ctx := a.helperCommandContext()
		status := a.mailboxService.status(ctx)
		if status.Connected {
			return a.mailboxService.virtualFolderUnreadCounts(ctx)
		}
		if status.ErrorCode != "service_unavailable" {
			return nil, fmt.Errorf("load virtual-folder unread counts from mailbox service: %s", status.Message)
		}
	}
	if a.virtualFolderStore == nil {
		return nil, fmt.Errorf("virtual-folder store is not ready")
	}
	return a.virtualFolderStore.UnreadCounts()
}

// CreateVirtualFolder creates a new virtual folder
func (a *App) CreateVirtualFolder(vf *virtualfolder.VirtualFolder) error {
	if vf == nil {
		return fmt.Errorf("virtual folder cannot be nil")
	}
	if handled, err := a.mutateVirtualFolderThroughService(v1.VirtualFolderMutationRequest{Operation: "create", VirtualFolder: vf}); handled {
		return err
	}
	if err := a.virtualFolderStore.Create(vf); err != nil {
		return err
	}
	// Warm the index cache so the first open renders instantly.
	if err := a.refreshVirtualFolderCache(vf); err != nil {
		log := logging.WithComponent("virtualfolder")
		log.Warn().Err(err).Str("virtualFolderID", vf.ID).Msg("Failed to warm virtual-folder cache on create")
	}
	return nil
}

// UpdateVirtualFolder updates an existing virtual folder
func (a *App) UpdateVirtualFolder(vf *virtualfolder.VirtualFolder) error {
	if vf == nil {
		return fmt.Errorf("virtual folder cannot be nil")
	}
	if vf.ID == "" {
		return fmt.Errorf("virtual folder ID cannot be empty")
	}
	if handled, err := a.mutateVirtualFolderThroughService(v1.VirtualFolderMutationRequest{Operation: "update", VirtualFolder: vf}); handled {
		return err
	}
	if err := a.virtualFolderStore.Update(vf); err != nil {
		return err
	}
	// Folder set or criteria may have changed; rebuild the cached index.
	if err := a.refreshVirtualFolderCache(vf); err != nil {
		log := logging.WithComponent("virtualfolder")
		log.Warn().Err(err).Str("virtualFolderID", vf.ID).Msg("Failed to refresh virtual-folder cache on update")
	}
	return nil
}

// ReorderVirtualFolders saves the sidebar order of virtual folders.
func (a *App) ReorderVirtualFolders(ids []string) error {
	if handled, err := a.mutateVirtualFolderThroughService(v1.VirtualFolderMutationRequest{Operation: "reorder", IDs: ids}); handled {
		return err
	}
	return a.virtualFolderStore.Reorder(ids)
}

// DeleteVirtualFolder deletes a virtual folder
func (a *App) DeleteVirtualFolder(id string) error {
	if id == "" {
		return fmt.Errorf("virtual folder ID cannot be empty")
	}
	if handled, err := a.mutateVirtualFolderThroughService(v1.VirtualFolderMutationRequest{Operation: "delete", ID: id}); handled {
		return err
	}
	return a.virtualFolderStore.Delete(id)
}

func (a *App) mutateVirtualFolderThroughService(req v1.VirtualFolderMutationRequest) (bool, error) {
	if a.mailboxService == nil {
		return false, nil
	}
	ctx := a.helperCommandContext()
	status := a.mailboxService.status(ctx)
	if !status.Connected {
		if status.ErrorCode == "service_unavailable" {
			return false, nil
		}
		return true, fmt.Errorf("mailbox service is not ready for virtual-folder %s: %s", req.Operation, status.Message)
	}
	if !status.WriteAuthorized {
		return true, errDesktopMailboxWriteAuthorizationRequired
	}
	if req.Operation == "create" && req.VirtualFolder != nil && req.VirtualFolder.ID == "" {
		req.VirtualFolder.ID = uuid.NewString()
	}
	if err := a.mailboxService.mutateVirtualFolder(ctx, req); err != nil {
		return true, fmt.Errorf("run virtual-folder %s through local mailbox service: %w", req.Operation, err)
	}
	if (req.Operation == "create" || req.Operation == "update" || req.Operation == "refresh") && req.VirtualFolder != nil {
		// Refresh requests carry only an ID; create and update carry the model.
		a.emitRuntimeEvent("virtualfolder:cacheUpdated", map[string]interface{}{"virtualFolderId": req.VirtualFolder.ID})
	} else if req.Operation == "refresh" {
		a.emitRuntimeEvent("virtualfolder:cacheUpdated", map[string]interface{}{"virtualFolderId": req.ID})
	}
	return true, nil
}

// GetVirtualFolderConversations returns conversations matching a virtual folder's criteria
func (a *App) GetVirtualFolderConversations(virtualFolderID string, offset, limit int, sortOrder string) ([]*message.Conversation, error) {
	if a.syncEngine != nil || a.remoteServiceMode {
		result := []*message.Conversation{}
		cursor := ""
		if limit <= 0 {
			limit = 50
		}
		for pages := 0; pages < 100; pages++ {
			// SearchMail applies the folder's saved search query itself.
			page, err := a.SearchMail(MailSearchRequest{VirtualFolderID: virtualFolderID, Cursor: cursor, Limit: 100})
			if err != nil {
				return nil, err
			}
			if len(page.Issues) > 0 {
				return nil, fmt.Errorf("virtual-folder account unavailable: %s", page.Issues[0].Message)
			}
			result = append(result, page.Conversations...)
			cursor = page.Cursor
			if cursor == "" || len(result) >= offset+limit {
				break
			}
		}
		if sortOrder == "oldest" {
			sort.Slice(result, func(i, j int) bool { return result[i].LatestDate.Before(result[j].LatestDate) })
		}
		if offset >= len(result) {
			return []*message.Conversation{}, nil
		}
		end := offset + limit
		if end > len(result) {
			end = len(result)
		}
		return result[offset:end], nil
	}

	vf, err := a.virtualFolderStore.Get(virtualFolderID)
	if err != nil {
		return nil, fmt.Errorf("failed to get virtual folder: %w", err)
	}
	if vf == nil {
		return nil, fmt.Errorf("virtual folder not found: %s", virtualFolderID)
	}
	return a.listVirtualFolderConversationsLocal(vf, offset, limit, sortOrder)
}

// listVirtualFolderConversationsLocal reads a virtual folder's conversations
// straight from the message store, bypassing the cached index. WAL readers do
// not block on writers, so this stays available while the write side (cache
// refresh, sync) is contended.
func (a *App) listVirtualFolderConversationsLocal(vf *virtualfolder.VirtualFolder, offset, limit int, sortOrder string) ([]*message.Conversation, error) {
	folderIDs, err := a.virtualFolderStore.QueryFolderIDs(vf)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve virtual folder members: %w", err)
	}
	if len(folderIDs) == 0 {
		return []*message.Conversation{}, nil
	}
	if vf.SearchQuery != "" {
		// A saved search defines membership: list via the text-search path so the
		// stored query and the saved criteria both apply.
		results, _, err := a.messageStore.SearchVirtualFolderConversations(folderIDs, vf.SearchQuery, offset, limit, virtualFolderMessageCriteria(vf))
		if err != nil {
			return nil, err
		}
		conversations := make([]*message.Conversation, 0, len(results))
		for _, result := range results {
			conversations = append(conversations, &result.Conversation)
		}
		return conversations, nil
	}
	return a.messageStore.ListVirtualFolderConversations(folderIDs, offset, limit, sortOrder, virtualFolderMessageCriteria(vf))
}

// isVirtualFolderCacheBusy reports whether a cached-view failure is transient
// and worth a direct local read: raw SQLite busy text (local stores and older
// helpers), the cache route's public database_busy code, the route's
// cache_timeout budget expiry, or the desktop's two-second request budget
// expiring while the helper is still working. cache_timeout is not lock
// contention - a slow healthy hydration also trips it - but the direct read
// serves the first paint either way; remote desktops without local stores
// still surface the original error.
func isVirtualFolderCacheBusy(err error) bool {
	if err == nil {
		return false
	}
	if database.IsBusy(err) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	text := err.Error()
	return strings.Contains(text, string(v1.CodeDatabaseBusy)) ||
		strings.Contains(text, string(v1.CodeCacheTimeout)) ||
		strings.Contains(text, "context deadline exceeded")
}

// virtualFolderDirectView answers GetVirtualFolderCachedView without the cache
// when the cache cannot be loaded or refreshed because of SQLite lock
// contention. Returns nil, false when the local stores cannot serve the view
// (for example a remote desktop without a local mailbox database), so the
// caller can surface the original error instead.
func (a *App) virtualFolderDirectView(virtualFolderID string) (*VirtualFolderCachedView, bool) {
	if a.virtualFolderStore == nil || a.messageStore == nil {
		return nil, false
	}
	vf, err := a.virtualFolderStore.Get(virtualFolderID)
	if err != nil || vf == nil {
		return nil, false
	}
	conversations, err := a.listVirtualFolderConversationsLocal(vf, 0, virtualFolderCacheLimit, "")
	if err != nil {
		return nil, false
	}
	return &VirtualFolderCachedView{
		VirtualFolderID: virtualFolderID,
		Conversations:   conversations,
		HasCache:        false,
	}, true
}

// GetVirtualFolderCount returns the count of conversations matching a virtual folder's criteria.
func (a *App) GetVirtualFolderCount(virtualFolderID string) (int, error) {
	count, handled, err := a.serviceConversationCount(views.Scope{Kind: views.ScopeVirtualFolder, VirtualFolderID: virtualFolderID}, "")
	if handled || err != nil {
		return count, err
	}
	if a.virtualFolderStore == nil || a.messageStore == nil {
		return 0, fmt.Errorf("virtual-folder counts require a connected mailbox service")
	}
	vf, err := a.virtualFolderStore.Get(virtualFolderID)
	if err != nil {
		return 0, fmt.Errorf("failed to get virtual folder: %w", err)
	}
	if vf == nil {
		return 0, fmt.Errorf("virtual folder not found: %s", virtualFolderID)
	}

	folderIDs, err := a.virtualFolderStore.QueryFolderIDs(vf)
	if err != nil {
		return 0, fmt.Errorf("failed to resolve virtual folder members: %w", err)
	}
	if len(folderIDs) == 0 {
		return 0, nil
	}
	if vf.SearchQuery != "" {
		_, count, err := a.messageStore.SearchVirtualFolderConversations(folderIDs, vf.SearchQuery, 0, 0, virtualFolderMessageCriteria(vf))
		return count, err
	}
	return a.messageStore.CountVirtualFolderConversations(folderIDs, virtualFolderMessageCriteria(vf))
}

// SearchVirtualFolder searches messages in a virtual folder's member folders.
func (a *App) SearchVirtualFolder(virtualFolderID, query string, offset, limit int, _ string) ([]*message.ConversationSearchResult, error) {
	vf, err := a.virtualFolderStore.Get(virtualFolderID)
	if err != nil {
		return nil, fmt.Errorf("failed to get virtual folder: %w", err)
	}
	if vf == nil {
		return nil, fmt.Errorf("virtual folder not found: %s", virtualFolderID)
	}
	folderIDs, err := a.virtualFolderStore.QueryFolderIDs(vf)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve virtual folder members: %w", err)
	}
	query = combineVirtualFolderQueries(vf.SearchQuery, query)
	results, _, err := a.messageStore.SearchVirtualFolderConversations(folderIDs, query, offset, limit, virtualFolderMessageCriteria(vf))
	return results, err
}

// GetVirtualFolderSearchCount returns the matching message-search count.
func (a *App) GetVirtualFolderSearchCount(virtualFolderID, query, _ string) (int, error) {
	vf, err := a.virtualFolderStore.Get(virtualFolderID)
	if err != nil {
		return 0, fmt.Errorf("failed to get virtual folder: %w", err)
	}
	if vf == nil {
		return 0, fmt.Errorf("virtual folder not found: %s", virtualFolderID)
	}
	folderIDs, err := a.virtualFolderStore.QueryFolderIDs(vf)
	if err != nil {
		return 0, fmt.Errorf("failed to resolve virtual folder members: %w", err)
	}
	query = combineVirtualFolderQueries(vf.SearchQuery, query)
	_, count, err := a.messageStore.SearchVirtualFolderConversations(folderIDs, query, 0, 0, virtualFolderMessageCriteria(vf))
	return count, err
}

func virtualFolderMessageCriteria(vf *virtualfolder.VirtualFolder) message.VirtualFolderCriteria {
	return message.VirtualFolderCriteria{
		ReadStatus:  vf.ReadStatus,
		ReplyStatus: vf.ReplyStatus,
		StarStatus:  vf.StarStatus,
		DateWindow:  vf.DateWindow,
	}
}

// combineVirtualFolderQueries ANDs a saved search query with a typed in-folder
// search. FTS5 treats space-separated terms as AND, and both sides go through
// the same term preparation in the message store.
func combineVirtualFolderQueries(stored, typed string) string {
	return strings.TrimSpace(strings.TrimSpace(stored) + " " + strings.TrimSpace(typed))
}

// virtualFolderCacheLimit caps how many conversations one cached index holds.
const virtualFolderCacheLimit = 200

// VirtualFolderCachedView is a virtual folder's cached conversation index for
// instant open. RefreshedAt tells the UI how stale the membership may be.
type VirtualFolderCachedView struct {
	VirtualFolderID string                  `json:"virtualFolderId"`
	Conversations   []*message.Conversation `json:"conversations"`
	RefreshedAt     time.Time               `json:"refreshedAt"`
	HasCache        bool                    `json:"hasCache"`
}

// GetVirtualFolderCachedView returns a virtual folder's cached conversation
// index, computing and storing it on first use. Row state (unread, stars,
// snippets) is hydrated live; only membership and order come from the cache.
func (a *App) GetVirtualFolderCachedView(virtualFolderID string) (*VirtualFolderCachedView, error) {
	if a.mailboxService != nil {
		ctx := a.helperCommandContext()
		status := a.mailboxService.status(ctx)
		if status.Connected {
			cached, err := a.mailboxService.cachedVirtualFolderView(ctx, virtualFolderID)
			if err != nil {
				// Lock contention is transient: serve the view from the local
				// direct read instead of failing the whole folder.
				if isVirtualFolderCacheBusy(err) {
					if view, ok := a.virtualFolderDirectView(virtualFolderID); ok {
						log := logging.WithComponent("virtual-folder")
						log.Warn().Err(err).Str("virtualFolderID", virtualFolderID).Msg("Mailbox service cache busy; serving virtual folder from direct read")
						return view, nil
					}
				}
				return nil, fmt.Errorf("load virtual-folder cache from mailbox service: %w", err)
			}
			return &VirtualFolderCachedView{
				VirtualFolderID: cached.VirtualFolderID,
				Conversations:   cached.Conversations,
				RefreshedAt:     cached.RefreshedAt,
				HasCache:        cached.HasCache,
			}, nil
		}
		if status.ErrorCode != "service_unavailable" {
			return nil, fmt.Errorf("load virtual-folder cache from mailbox service: %s", status.Message)
		}
	}
	vf, err := a.virtualFolderStore.Get(virtualFolderID)
	if err != nil {
		return nil, fmt.Errorf("failed to get virtual folder: %w", err)
	}
	if vf == nil {
		return nil, fmt.Errorf("virtual folder not found: %s", virtualFolderID)
	}
	refs, refreshedAt, hasCache, err := a.virtualFolderStore.LoadCache(virtualFolderID)
	if err != nil {
		return nil, err
	}
	if !hasCache {
		if err := a.RefreshVirtualFolderCache(virtualFolderID); err != nil {
			// A busy cache is optional state: fall back to the direct read so
			// the view still loads under writer contention.
			if isVirtualFolderCacheBusy(err) {
				if view, ok := a.virtualFolderDirectView(virtualFolderID); ok {
					log := logging.WithComponent("virtual-folder")
					log.Warn().Err(err).Str("virtualFolderID", virtualFolderID).Msg("Virtual-folder cache refresh busy; serving direct read")
					return view, nil
				}
			}
			return nil, err
		}
		if refs, refreshedAt, _, err = a.virtualFolderStore.LoadCache(virtualFolderID); err != nil {
			if isVirtualFolderCacheBusy(err) {
				if view, ok := a.virtualFolderDirectView(virtualFolderID); ok {
					return view, nil
				}
			}
			return nil, err
		}
	}
	msgRefs := make([]message.ConversationRef, 0, len(refs))
	for _, ref := range refs {
		msgRefs = append(msgRefs, message.ConversationRef{AccountID: ref.AccountID, ConversationID: ref.ConversationID})
	}
	conversations, err := a.messageStore.ListConversationsByRefs(msgRefs)
	if err != nil {
		return nil, err
	}
	return &VirtualFolderCachedView{
		VirtualFolderID: virtualFolderID,
		Conversations:   conversations,
		RefreshedAt:     refreshedAt,
		HasCache:        true,
	}, nil
}

// RefreshVirtualFolderCache recomputes one virtual folder's cached index and
// notifies the frontend. Used by the background refresher and available for a
// manual refresh.
func (a *App) RefreshVirtualFolderCache(virtualFolderID string) error {
	a.invalidateViewCounts()
	if handled, err := a.mutateVirtualFolderThroughService(v1.VirtualFolderMutationRequest{Operation: "refresh", ID: virtualFolderID}); handled {
		return err
	}
	vf, err := a.virtualFolderStore.Get(virtualFolderID)
	if err != nil {
		return fmt.Errorf("failed to get virtual folder: %w", err)
	}
	if vf == nil {
		return fmt.Errorf("virtual folder not found: %s", virtualFolderID)
	}
	return a.refreshVirtualFolderCache(vf)
}

// SyncVirtualFolder refreshes member-folder headers and then rebuilds the
// virtual-folder cache. It is intentionally header-only; message bodies and
// attachments remain on demand. Gmail members are searched through the Gmail
// API when the view reloads, so only non-Gmail folders need IMAP sync here.
func (a *App) SyncVirtualFolder(virtualFolderID string) error {
	vf, err := a.virtualFolderStore.Get(virtualFolderID)
	if err != nil {
		return fmt.Errorf("failed to get virtual folder: %w", err)
	}
	if vf == nil {
		return fmt.Errorf("virtual folder not found: %s", virtualFolderID)
	}

	refreshErrors := map[string]error{}
	if a.syncEngine != nil {
		folderIDs, resolveErr := a.virtualFolderStore.QueryFolderIDs(vf)
		if resolveErr != nil {
			return fmt.Errorf("failed to resolve virtual folder members: %w", resolveErr)
		}

		type member struct{ accountID, folderID, label string }
		members := make([]member, 0, len(folderIDs))
		for _, folderID := range folderIDs {
			f, getErr := a.folderStore.Get(folderID)
			if getErr != nil {
				refreshErrors[folderID] = getErr
				continue
			}
			if f == nil || f.Hidden {
				continue // a deleted selected folder is no longer a member
			}
			acc, getErr := a.accountStore.Get(f.AccountID)
			if getErr != nil {
				refreshErrors[folderID] = getErr
				continue
			}
			if acc == nil || !acc.Enabled {
				continue
			}
			if acc.AuthType == account.AuthOAuth2 && strings.EqualFold(acc.IMAPHost, gmailIMAPHost) {
				continue
			}
			members = append(members, member{accountID: f.AccountID, folderID: f.ID, label: acc.Name + "/" + f.Path})
		}

		// Bound parallel IMAP work so a broad exclude-mode view does not start
		// one network operation per folder at once.
		const maxConcurrentFolderRefreshes = 3
		sem := make(chan struct{}, maxConcurrentFolderRefreshes)
		var wg sync.WaitGroup
		var mu sync.Mutex
		for _, item := range members {
			item := item
			sem <- struct{}{}
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer func() { <-sem }()
				// Do not cancel an existing user-initiated sync; its completion
				// event will refresh the visible virtual view after this cache pass.
				if syncErr := a.syncFolder(item.accountID, item.folderID, false); syncErr != nil {
					mu.Lock()
					refreshErrors[item.label] = syncErr
					mu.Unlock()
				}
			}()
		}
		wg.Wait()
	}

	cacheErr := a.RefreshVirtualFolderCache(virtualFolderID)
	if cacheErr != nil {
		return cacheErr
	}
	return virtualFolderRefreshError(virtualFolderID, refreshErrors)
}

// refreshVirtualFolderCache recomputes and stores the cached index for one
// virtual folder, then emits virtualfolder:cacheUpdated so an open view can
// pick up the new membership.
func (a *App) refreshVirtualFolderCache(vf *virtualfolder.VirtualFolder) error {
	folderIDs, err := a.virtualFolderStore.QueryFolderIDs(vf)
	if err != nil {
		return fmt.Errorf("failed to resolve virtual folder members: %w", err)
	}
	// Only provider search can resolve operators such as from: or has:.
	// Search records each successful page; local maintenance must not replace it.
	if vf.SearchQuery != "" {
		return nil
	}

	refs := []message.ConversationRef{}
	if len(folderIDs) > 0 {
		refs, err = a.messageStore.ListVirtualFolderConversationIDs(folderIDs, vf.SearchQuery, virtualFolderCacheLimit, virtualFolderMessageCriteria(vf))
		if err != nil {
			return err
		}
	}
	entries := make([]virtualfolder.CachedConversation, 0, len(refs))
	for _, ref := range refs {
		entries = append(entries, virtualfolder.CachedConversation{AccountID: ref.AccountID, ConversationID: ref.ConversationID})
	}
	if err := a.virtualFolderStore.SaveCache(vf.ID, entries); err != nil {
		return err
	}
	if a.ctx != nil {
		a.emitRuntimeEvent("virtualfolder:cacheUpdated", map[string]interface{}{
			"virtualFolderId": vf.ID,
		})
	}
	return nil
}

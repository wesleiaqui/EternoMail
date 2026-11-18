package app

import (
	"fmt"

	v1 "github.com/hkdb/aerion/internal/automation/v1"
	"github.com/hkdb/aerion/internal/folder"
	"github.com/hkdb/aerion/internal/logging"
	"github.com/hkdb/aerion/internal/mailfilter"
	"github.com/hkdb/aerion/internal/message"
	syncruntime "github.com/hkdb/aerion/internal/sync"
)

// ============================================================================
// Local Filter API - Exposed to frontend via Wails bindings
// ============================================================================

// GetFilterRules returns all local filter rules in priority order.
func (a *App) GetFilterRules() ([]*mailfilter.Rule, error) {
	if a.mailboxService != nil {
		ctx := a.helperCommandContext()
		status := a.mailboxService.status(ctx)
		if status.Connected {
			return a.mailboxService.filterRules(ctx)
		}
		if status.ErrorCode != "service_unavailable" || a.mailboxService.isRemote() {
			return nil, fmt.Errorf("load filter rules from mailbox service: %s", status.Message)
		}
	}
	return a.filterStore.List()
}

// CreateFilterRule validates and persists a new local filter rule.
func (a *App) CreateFilterRule(rule *mailfilter.Rule) error {
	if rule == nil {
		return fmt.Errorf("filter rule cannot be nil")
	}
	if handled, err := a.mutateFilterRuleThroughService(v1.FilterRuleMutationRequest{Operation: "create", Rule: rule}); handled {
		return err
	}
	if err := a.validateFilterRule(rule); err != nil {
		return err
	}
	// New rules go to the end of the priority list.
	if rule.OrderIndex == 0 {
		if existing, listErr := a.filterStore.List(); listErr == nil {
			rule.OrderIndex = len(existing)
		}
	}
	return a.filterStore.Create(rule)
}

// UpdateFilterRule validates and persists changes to an existing filter rule.
func (a *App) UpdateFilterRule(rule *mailfilter.Rule) error {
	if rule == nil {
		return fmt.Errorf("filter rule cannot be nil")
	}
	if rule.ID == "" {
		return fmt.Errorf("filter rule ID cannot be empty")
	}
	if handled, err := a.mutateFilterRuleThroughService(v1.FilterRuleMutationRequest{Operation: "update", Rule: rule}); handled {
		return err
	}
	if err := a.validateFilterRule(rule); err != nil {
		return err
	}
	return a.filterStore.Update(rule)
}

// DeleteFilterRule removes a local filter rule.
func (a *App) DeleteFilterRule(id string) error {
	if id == "" {
		return fmt.Errorf("filter rule ID cannot be empty")
	}
	if handled, err := a.mutateFilterRuleThroughService(v1.FilterRuleMutationRequest{Operation: "delete", ID: id}); handled {
		return err
	}
	return a.filterStore.Delete(id)
}

// ReorderFilterRules saves the priority order of filter rules.
func (a *App) ReorderFilterRules(ids []string) error {
	if handled, err := a.mutateFilterRuleThroughService(v1.FilterRuleMutationRequest{Operation: "reorder", IDs: ids}); handled {
		return err
	}
	return a.filterStore.Reorder(ids)
}

// PreviewFilterRule counts the stored messages matching a rule's scope and
// criteria without changing any mail. The rule does not need to be saved
// first, so the create/edit dialog can preview as the user types.
func (a *App) PreviewFilterRule(rule *mailfilter.Rule) (int, error) {
	if rule == nil {
		return 0, fmt.Errorf("filter rule cannot be nil")
	}
	var result int
	if handled, err := a.evaluateFilterThroughService(v1.FilterRuleMutationRequest{Operation: "preview", Rule: rule}, &result); handled {
		return result, err
	}
	return a.filterStore.CountMatching(rule)
}

// ApplyFilterRuleToExisting applies a saved rule to the stored messages it
// matches, with a 500-message safety limit. Explicit application is separate
// from the enabled flag, which controls only future incoming mail.
func (a *App) ApplyFilterRuleToExisting(id string) (int, error) {
	if id == "" {
		return 0, fmt.Errorf("filter rule ID cannot be empty")
	}
	var result int
	if handled, err := a.evaluateFilterThroughService(v1.FilterRuleMutationRequest{Operation: "apply", ID: id}, &result); handled {
		return result, err
	}
	rule, err := a.filterStore.Get(id)
	if err != nil {
		return 0, fmt.Errorf("load filter rule: %w", err)
	}
	if rule == nil {
		return 0, fmt.Errorf("filter rule not found: %s", id)
	}
	if err := a.validateFilterRule(rule); err != nil {
		return 0, err
	}
	// Cap one run so a broad rule cannot turn a misclick into an unbounded
	// mailbox mutation. Refuse broad matches before changing any messages.
	const maxPerRun = 500
	ids, err := a.filterStore.MatchingMessageIDs(rule, maxPerRun+1)
	if err != nil {
		return 0, err
	}
	if len(ids) > maxPerRun {
		return 0, fmt.Errorf("more than 500 messages match; narrow the rule before applying to existing mail")
	}
	if len(ids) == 0 {
		return 0, nil
	}
	return a.applyFilterActions(rule, ids)
}

// GetMessageListID returns the stored List-ID header value for a message,
// used to prefill the create-filter dialog. Empty when the message has no
// List-ID or predates header capture.
func (a *App) GetMessageListID(messageID string) (string, error) {
	if messageID == "" {
		return "", fmt.Errorf("message ID cannot be empty")
	}
	var result string
	if handled, err := a.evaluateFilterThroughService(v1.FilterRuleMutationRequest{Operation: "list-id", ID: messageID}, &result); handled {
		return result, err
	}
	value, err := a.messageStore.GetListID(messageID)
	if err != nil || value != "" || a.syncEngine == nil {
		return value, err
	}
	messages, err := a.messageStore.GetByIDs([]string{messageID})
	if err != nil {
		return "", err
	}
	if len(messages) != 1 {
		return "", fmt.Errorf("message not found")
	}
	m := messages[0]
	raw, err := a.syncEngine.FetchRawMessage(a.actionContext(), m.AccountID, m.FolderID, m.UID)
	if err != nil {
		return "", err
	}
	value = syncruntime.ExtractListID(raw)
	return value, a.messageStore.SetListID(messageID, value)
}

func (a *App) mutateFilterRuleThroughService(req v1.FilterRuleMutationRequest) (bool, error) {
	if a.mailboxService == nil {
		return false, nil
	}
	ctx := a.helperCommandContext()
	status := a.mailboxService.status(ctx)
	if !status.Connected {
		if status.ErrorCode == "service_unavailable" && !a.mailboxService.isRemote() {
			return false, nil
		}
		return true, fmt.Errorf("mailbox service is not ready for filter rule %s: %s", req.Operation, status.Message)
	}
	if !status.WriteAuthorized {
		return true, errDesktopMailboxWriteAuthorizationRequired
	}
	if err := a.mailboxService.mutateFilterRule(ctx, req); err != nil {
		return true, fmt.Errorf("run filter rule %s through local mailbox service: %w", req.Operation, err)
	}
	return true, nil
}

// validateFilterRule enforces the rule structure: a destination folder that
// exists when the rule moves mail, existing accounts for every scoped
// account, and at most one relocation action. Same-account destinations are
// allowed.
func (a *App) validateFilterRule(rule *mailfilter.Rule) error {
	return mailfilter.ValidateRule(rule, a.accountStore, a.folderStore)
}

// handleNewMessageForFilters delegates rule selection to the reusable
// backend router. The app supplies its shared action pipeline as the applier.
func (a *App) handleNewMessageForFilters(msg *message.Message) {
	if a.filterRouter != nil {
		a.filterRouter.Route(msg)
	}
}

// applyMatchedFilterRule reloads the rule before applying a delayed sync callback.
func (a *App) applyMatchedFilterRule(rule *mailfilter.Rule, msg *message.Message) {
	defer recoverPanic("app.filters", "apply filter rule")
	if rule == nil || msg == nil {
		return
	}
	current, err := a.filterStore.Get(rule.ID)
	if err != nil || current == nil || !current.Enabled || !current.UpdatedAt.Equal(rule.UpdatedAt) {
		return
	}
	if _, err := a.applyFilterActions(current, []string{msg.ID}, msg); err != nil {
		log := logging.WithComponent("app.filters")
		log.Warn().Err(err).Str("ruleID", rule.ID).Msg("Filter actions failed")
	}
}

// applyFilterActions uses one provider/location snapshot and the same move
// lease as user actions. Recheck inside the lease after waiting, never move
// a user's newly relocated message based on an old sync callback. Trash is
// always a folder move, never the permanent-delete behavior of the UI command.
func (a *App) applyFilterActions(rule *mailfilter.Rule, ids []string, snapshots ...*message.Message) (int, error) {
	if rule == nil || len(ids) == 0 {
		return 0, nil
	}
	if len(snapshots) == 0 {
		var err error
		snapshots, err = a.messageStore.GetByIDs(ids)
		if err != nil {
			return 0, err
		}
	}
	count := 0
	for _, snapshot := range snapshots {
		if snapshot == nil {
			continue
		}
		acted := false
		_, _, _, err := a.runMoveMutationWithMode([]string{snapshot.ID}, "filter:"+rule.ID, true, func() (bool, string, error) {
			current, err := a.messageStore.GetByIDs([]string{snapshot.ID})
			if err != nil {
				return false, "", err
			}
			if len(current) != 1 || current[0] == nil {
				return false, "", nil
			}
			m := current[0]
			if m.AccountID != snapshot.AccountID || m.FolderID != snapshot.FolderID || m.UID != snapshot.UID || m.IsDeleted || m.IsDraft {
				return false, "", nil
			}
			m.ListID, err = a.messageStore.GetListID(m.ID)
			if err != nil {
				return false, "", err
			}
			effective := *rule
			effective.Enabled = true
			if !mailfilter.Match(&effective, mailfilter.FromMessage(m)) {
				return false, "", nil
			}
			destID := rule.DestFolderID
			if rule.ActionArchive || rule.ActionTrash {
				kind := folder.TypeArchive
				if rule.ActionArchive && a.isGmailAccount(m.AccountID) {
					kind = folder.TypeAll
				}
				if rule.ActionTrash {
					kind = folder.TypeTrash
				}
				dest, err := a.GetSpecialFolder(m.AccountID, kind)
				if err != nil {
					return false, "", err
				}
				if dest == nil || dest.Hidden {
					return false, "", fmt.Errorf("filter destination unavailable or hidden")
				}
				destID = dest.ID
			}
			if destID != "" {
				dest, err := a.folderStore.Get(destID)
				if err != nil {
					return false, "", err
				}
				if dest == nil || dest.Hidden {
					return false, "", fmt.Errorf("filter destination unavailable or hidden")
				}
			}
			if rule.ActionMarkRead {
				if err := a.markFilterReadSynchronously(m); err != nil {
					return false, "", err
				}
			}
			if destID != "" && m.FolderID != destID {
				if _, err := a.moveToFolder([]string{m.ID}, destID, false, ""); err != nil {
					return false, "", err
				}
			}
			acted = true
			return false, "", nil
		})
		if err != nil {
			return count, err
		}
		if acted {
			count++
		}
	}
	return count, nil
}

func (a *App) evaluateFilterThroughService(req v1.FilterRuleMutationRequest, result any) (bool, error) {
	if a.mailboxService == nil {
		return false, nil
	}
	ctx := a.helperCommandContext()
	status := a.mailboxService.status(ctx)
	if !status.Connected {
		if status.ErrorCode == "service_unavailable" && !a.mailboxService.isRemote() {
			return false, nil
		}
		return true, fmt.Errorf("mailbox service unavailable: %s", status.Message)
	}
	if req.Operation == "apply" && !status.WriteAuthorized {
		return true, errDesktopMailboxWriteAuthorizationRequired
	}
	return true, a.mailboxService.evaluateFilterRule(ctx, req, result)
}

// Filters cannot use the optimistic UI read path: provider STORE must finish
// before COPY or the destination can retain the old unread flag.
func (a *App) markFilterReadSynchronously(m *message.Message) error {
	if m.IsRead {
		return nil
	}
	if err := a.syncFlagsToIMAP([]*message.Message{m}, m.FolderID, "read", true); err != nil {
		return err
	}
	read := true
	if err := a.messageStore.UpdateFlagsBatch([]string{m.ID}, &read, nil); err != nil {
		return err
	}
	unread, err := a.messageStore.CountUnreadByFolder(m.FolderID)
	if err != nil {
		return err
	}
	f, err := a.folderStore.Get(m.FolderID)
	if err != nil {
		return err
	}
	if f != nil {
		if err := a.folderStore.UpdateCounts(f.ID, f.TotalCount, unread); err != nil {
			return err
		}
		a.emitRuntimeEvent("folders:countsChanged", map[string]int{f.ID: unread})
	}
	a.emitRuntimeEvent("messages:readChanged", map[string]interface{}{"messageIds": []string{m.ID}, "isRead": true})
	return nil
}

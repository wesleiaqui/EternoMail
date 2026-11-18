package alwayson

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"

	goimap "github.com/emersion/go-imap/v2"
	"github.com/hkdb/aerion/internal/automation/v1"
	"github.com/hkdb/aerion/internal/folder"
	"github.com/hkdb/aerion/internal/gmail"
	"github.com/hkdb/aerion/internal/imap"
	"github.com/hkdb/aerion/internal/mailmove"
	"github.com/hkdb/aerion/internal/message"
	"github.com/hkdb/aerion/internal/operations"
)

type mailboxActionOperations struct {
	contextOverride      context.Context
	runtime              *SyncRuntime
	gmailClientFunc      func(accountID string) (*gmail.Client, bool, error)
	setIMAPFlagsFunc     func(accountID, folderID string, messages []*message.Message, kind string, enabled bool) error
	moveToFolderFunc     func(messages []*message.Message, destination *folder.Folder) error
	persistFlagGroupFunc func(messages []*message.Message, kind string, enabled bool) error
}

func (o *mailboxActionOperations) PlanActionGroups(ctx context.Context, req v1.ActionRequest, ids []string) ([]v1.ActionProviderGroup, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	messages, err := o.loadActionMessages(ids)
	if err != nil {
		return nil, err
	}
	type groupKey struct {
		accountID            string
		sourceFolderID       string
		destinationAccountID string
		destinationFolderID  string
		messageID            string
	}
	groupsByKey := make(map[groupKey]*v1.ActionProviderGroup)
	for _, msg := range messages {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		destinationAccountID, destinationFolderID, err := o.actionExpectedLocation(req, msg)
		if err != nil {
			return nil, err
		}
		switch req.Action {
		case v1.ActionArchive, v1.ActionArchiveMarkRead, v1.ActionTrash, v1.ActionMove, v1.ActionMoveToInbox, v1.ActionMoveToSpam:
		default:
			destinationAccountID, destinationFolderID = "", ""
		}
		messageKey := ""
		if req.Action == v1.ActionMove && destinationAccountID != msg.AccountID {
			// Cross-account moves are individually copy-confirm-delete operations;
			// journal each message independently so earlier confirmed copies remain undoable.
			messageKey = msg.ID
		}
		key := groupKey{
			accountID:            msg.AccountID,
			sourceFolderID:       msg.FolderID,
			destinationAccountID: destinationAccountID,
			destinationFolderID:  destinationFolderID,
			messageID:            messageKey,
		}
		group := groupsByKey[key]
		if group == nil {
			groupID := make([]byte, 16)
			if _, err := rand.Read(groupID); err != nil {
				return nil, fmt.Errorf("generate provider group ID: %w", err)
			}
			group = &v1.ActionProviderGroup{
				ID:                   "group-" + hex.EncodeToString(groupID),
				AccountID:            key.accountID,
				SourceFolderID:       key.sourceFolderID,
				DestinationAccountID: key.destinationAccountID,
				DestinationFolderID:  key.destinationFolderID,
			}
			groupsByKey[key] = group
		}
		group.MessageIDs = append(group.MessageIDs, msg.ID)
	}
	keys := make([]groupKey, 0, len(groupsByKey))
	for key := range groupsByKey {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		left, right := keys[i], keys[j]
		if left.accountID != right.accountID {
			return left.accountID < right.accountID
		}
		if left.sourceFolderID != right.sourceFolderID {
			return left.sourceFolderID < right.sourceFolderID
		}
		if left.destinationAccountID != right.destinationAccountID {
			return left.destinationAccountID < right.destinationAccountID
		}
		if left.destinationFolderID != right.destinationFolderID {
			return left.destinationFolderID < right.destinationFolderID
		}
		return left.messageID < right.messageID
	})
	groups := make([]v1.ActionProviderGroup, 0, len(keys))
	for _, key := range keys {
		group := groupsByKey[key]
		sort.Strings(group.MessageIDs)
		groups = append(groups, *group)
	}
	return groups, nil
}

func (o *mailboxActionOperations) ExecuteActionGroup(ctx context.Context, req v1.ActionRequest, group v1.ActionProviderGroup) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(group.MessageIDs) == 0 || group.AccountID == "" || group.SourceFolderID == "" {
		return errors.New("action provider group has incomplete identity")
	}
	switch req.Action {
	case v1.ActionArchive:
		_, err := o.ArchiveMessages(group.MessageIDs)
		return err
	case v1.ActionArchiveMarkRead:
		original, err := o.loadActionMessages(group.MessageIDs)
		if err != nil {
			return err
		}
		if _, err := o.ArchiveMessages(group.MessageIDs); err != nil {
			return err
		}
		if err := o.MarkAsRead(group.MessageIDs); err != nil {
			// The move already happened at the provider, the read leg can fail
			// after changing some provider flags, and a group that is not
			// confirmed keeps no Undo snapshot. Compensate both legs before
			// reporting the failure; any compensation error is surfaced with
			// the failure so the state is repairable instead of silent.
			return errors.Join(err, o.restoreGroupState(group, original))
		}
		return nil
	case v1.ActionMarkRead:
		return o.MarkAsRead(group.MessageIDs)
	case v1.ActionMarkUnread:
		return o.MarkAsUnread(group.MessageIDs)
	case v1.ActionStar:
		return o.StarMessages(group.MessageIDs)
	case v1.ActionUnstar:
		return o.UnstarMessages(group.MessageIDs)
	case v1.ActionTrash:
		return o.TrashMessages(group.MessageIDs)
	case v1.ActionMove:
		_, err := o.MoveMessages(group.MessageIDs, req.Destination)
		return err
	case v1.ActionMoveToInbox:
		return o.MoveMessagesToInbox(group.MessageIDs)
	case v1.ActionMoveToSpam:
		return o.MoveMessagesToSpam(group.MessageIDs)
	default:
		return fmt.Errorf("unsupported action %q", req.Action)
	}
}

func (o *mailboxActionOperations) GetByIDs(ids []string) ([]*message.Message, error) {
	return o.runtime.messages.GetByIDs(ids)
}

func (o *mailboxActionOperations) ActionAffectedRefs(ctx context.Context, ids []string) ([]v1.AffectedMessageRef, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	messages, err := o.loadActionMessages(ids)
	if err != nil {
		return nil, err
	}
	refs := make([]v1.AffectedMessageRef, 0, len(messages))
	for _, msg := range messages {
		refs = append(refs, v1.AffectedMessageRef{AccountID: msg.AccountID, MessageID: msg.ID, FolderID: msg.FolderID})
	}
	return refs, nil
}

func (o *mailboxActionOperations) GetMessageIDsInThread(accountID, threadID string) ([]string, error) {
	return o.runtime.messages.GetMessageIDsInThread(accountID, threadID)
}

func (o *mailboxActionOperations) ArchiveMessages(ids []string) (string, error) {
	messages, err := o.loadActionMessages(ids)
	if err != nil {
		return "", err
	}
	byAccount := groupMessagesByAccount(messages)
	destinations := make(map[string]*folder.Folder, len(byAccount))
	for _, accountID := range sortedMessageGroupKeys(byAccount) {
		target := folder.TypeArchive
		_, gmailAccount, err := o.gmailClient(accountID)
		if err != nil {
			return "", err
		}
		if gmailAccount {
			target = folder.TypeAll
		}
		destination, err := o.runtime.folders.GetByType(accountID, target)
		if err != nil {
			return "", err
		}
		if destination == nil && target == folder.TypeAll {
			destination, err = o.runtime.folders.GetByPath(accountID, "[Gmail]/All Mail")
			if err != nil {
				return "", err
			}
		}
		if destination == nil {
			return "", fmt.Errorf("account %s has no archive destination", accountID)
		}
		destinations[accountID] = destination
	}
	for _, accountID := range sortedMessageGroupKeys(byAccount) {
		if err := o.moveToFolder(messagesForAccount(byAccount, accountID), destinations[accountID]); err != nil {
			return "", err
		}
	}
	return "", nil
}

func (o *mailboxActionOperations) MarkAsRead(ids []string) error {
	return o.setFlags(ids, "read", true)
}

func (o *mailboxActionOperations) MarkAsUnread(ids []string) error {
	return o.setFlags(ids, "read", false)
}

func (o *mailboxActionOperations) StarMessages(ids []string) error {
	return o.setFlags(ids, "starred", true)
}

func (o *mailboxActionOperations) UnstarMessages(ids []string) error {
	return o.setFlags(ids, "starred", false)
}

func (o *mailboxActionOperations) TrashMessages(ids []string) error {
	messages, err := o.loadActionMessages(ids)
	if err != nil {
		return err
	}
	byAccount := groupMessagesByAccount(messages)
	destinations := make(map[string]*folder.Folder, len(byAccount))
	for _, accountID := range sortedMessageGroupKeys(byAccount) {
		destination, err := o.runtime.folders.GetByType(accountID, folder.TypeTrash)
		if err != nil {
			return err
		}
		if destination == nil {
			return fmt.Errorf("account %s has no Trash folder", accountID)
		}
		destinations[accountID] = destination
	}
	for _, accountID := range sortedMessageGroupKeys(byAccount) {
		if err := o.moveToFolder(messagesForAccount(byAccount, accountID), destinations[accountID]); err != nil {
			return err
		}
	}
	return nil
}

func (o *mailboxActionOperations) MoveMessages(ids []string, destinationFolderID string) (string, error) {
	if destinationFolderID == "" {
		return "", errors.New("destination folder ID is required")
	}
	destination, err := o.runtime.folders.Get(destinationFolderID)
	if err != nil {
		return "", err
	}
	if destination == nil || destination.AccountID == "" || destination.Path == "" {
		return "", errors.New("destination folder is unavailable")
	}
	messages, err := o.loadActionMessages(ids)
	if err != nil {
		return "", err
	}
	if err := o.moveToFolder(messages, destination); err != nil {
		return "", err
	}
	return "", nil
}

func (o *mailboxActionOperations) MoveMessagesToInbox(ids []string) error {
	messages, err := o.loadActionMessages(ids)
	if err != nil {
		return err
	}
	byAccount := groupMessagesByAccount(messages)
	destinations := make(map[string]*folder.Folder, len(byAccount))
	for _, accountID := range sortedMessageGroupKeys(byAccount) {
		destination, err := o.runtime.folders.GetByType(accountID, folder.TypeInbox)
		if err != nil {
			return err
		}
		if destination == nil {
			return fmt.Errorf("account %s has no Inbox folder", accountID)
		}
		destinations[accountID] = destination
	}
	for _, accountID := range sortedMessageGroupKeys(byAccount) {
		if err := o.moveToFolder(messagesForAccount(byAccount, accountID), destinations[accountID]); err != nil {
			return err
		}
	}
	return nil
}

func (o *mailboxActionOperations) MoveMessagesToSpam(ids []string) error {
	messages, err := o.loadActionMessages(ids)
	if err != nil {
		return err
	}
	byAccount := groupMessagesByAccount(messages)
	destinations := make(map[string]*folder.Folder, len(byAccount))
	for _, accountID := range sortedMessageGroupKeys(byAccount) {
		destination, err := o.runtime.folders.GetByType(accountID, folder.TypeSpam)
		if err != nil {
			return err
		}
		if destination == nil {
			return fmt.Errorf("account %s has no Spam folder", accountID)
		}
		destinations[accountID] = destination
	}
	for _, accountID := range sortedMessageGroupKeys(byAccount) {
		if err := o.moveToFolder(messagesForAccount(byAccount, accountID), destinations[accountID]); err != nil {
			return err
		}
	}
	return nil
}

func (o *mailboxActionOperations) loadActionMessages(ids []string) ([]*message.Message, error) {
	if len(ids) == 0 {
		return nil, errors.New("at least one message is required")
	}
	messages, err := o.runtime.messages.GetByIDs(ids)
	if err != nil {
		return nil, err
	}
	if len(messages) != len(ids) {
		return nil, errors.New("one or more target messages are no longer available")
	}
	for _, msg := range messages {
		if msg == nil || msg.ID == "" || msg.AccountID == "" || msg.FolderID == "" {
			return nil, errors.New("target message has incomplete account or folder identity")
		}
	}
	return messages, nil
}

func (o *mailboxActionOperations) setFlags(ids []string, kind string, enabled bool) error {
	messages, err := o.loadActionMessages(ids)
	if err != nil {
		return err
	}
	byAccount := groupMessagesByAccount(messages)
	for _, accountID := range sortedMessageGroupKeys(byAccount) {
		group := messagesForAccount(byAccount, accountID)
		client, gmailAccount, err := o.gmailClient(accountID)
		if err != nil {
			return err
		}
		if gmailAccount {
			providerIDs := make([]string, 0, len(group))
			for _, msg := range group {
				providerID, err := o.runtime.syncEngine.GmailProviderID(o.operationContext(), client, msg)
				if err != nil {
					return err
				}
				providerIDs = append(providerIDs, providerID)
			}
			label, add := "STARRED", enabled
			if kind == "read" {
				label, add = "UNREAD", !enabled
			}
			adds, removes := []string{}, []string{}
			if add {
				adds = append(adds, label)
			} else {
				removes = append(removes, label)
			}
			if err := client.Modify(o.operationContext(), providerIDs, adds, removes); err != nil {
				return err
			}
			if err := o.persistFlagGroup(group, kind, enabled); err != nil {
				return err
			}
			continue
		}
		byFolder := groupMessagesByFolder(group)
		for _, folderID := range sortedMessageGroupKeys(byFolder) {
			folderMessages := messagesForAccount(byFolder, folderID)
			if err := o.setIMAPFlags(accountID, folderID, folderMessages, kind, enabled); err != nil {
				return err
			}
			if err := o.persistFlagGroup(folderMessages, kind, enabled); err != nil {
				return err
			}
		}
	}
	return nil
}

func (o *mailboxActionOperations) persistFlagGroup(messages []*message.Message, kind string, enabled bool) error {
	if o.persistFlagGroupFunc != nil {
		return o.persistFlagGroupFunc(messages, kind, enabled)
	}
	ids := make([]string, 0, len(messages))
	for _, msg := range messages {
		ids = append(ids, msg.ID)
	}
	read, starred := (*bool)(nil), (*bool)(nil)
	if kind == "read" {
		read = &enabled
	}
	if kind == "starred" {
		starred = &enabled
	}
	return o.runtime.messages.UpdateFlagsBatch(ids, read, starred)
}

func (o *mailboxActionOperations) setIMAPFlags(accountID, folderID string, messages []*message.Message, kind string, enabled bool) error {
	if o.setIMAPFlagsFunc != nil {
		return o.setIMAPFlagsFunc(accountID, folderID, messages, kind, enabled)
	}
	if len(messages) == 0 {
		return nil
	}
	f, err := o.runtime.folders.Get(folderID)
	if err != nil {
		return err
	}
	if f == nil || f.AccountID != accountID || f.Path == "" {
		return fmt.Errorf("source folder %s is unavailable in account %s", folderID, accountID)
	}
	uids := make([]goimap.UID, 0, len(messages))
	for _, msg := range messages {
		if msg.AccountID != accountID || msg.FolderID != folderID || msg.UID == 0 || int32(msg.UID) < 0 {
			return fmt.Errorf("message %s has no stable UID in its source folder", msg.ID)
		}
		uids = append(uids, goimap.UID(msg.UID))
	}
	conn, err := o.runtime.pool.GetConnection(o.operationContext(), accountID)
	if err != nil {
		return err
	}
	selected, err := conn.Client().SelectMailbox(o.operationContext(), f.Path)
	if err != nil {
		o.runtime.pool.Discard(conn)
		return err
	}
	_ = selected
	defer o.runtime.pool.Release(conn)
	flag := goimap.FlagFlagged
	if kind == "read" {
		flag = goimap.FlagSeen
	}
	if enabled {
		return conn.Client().AddMessageFlags(uids, []goimap.Flag{flag})
	}
	return conn.Client().RemoveMessageFlags(uids, []goimap.Flag{flag})
}

// restoreGroupState reverses a partially applied archive-and-mark-read: the
// messages move back to their source folder, and every message that was unread
// before the action is marked unread again at the provider and locally. The
// read leg can fail after changing provider flags (provider update succeeds,
// local persistence fails), so folder-only compensation would carry an
// unintended read state back into Inbox. The original slice must be captured
// before the action runs.
func (o *mailboxActionOperations) restoreGroupState(group v1.ActionProviderGroup, original []*message.Message) error {
	var errs []error
	if err := o.restoreGroupFolder(group); err != nil {
		errs = append(errs, fmt.Errorf("restore archived messages to folder %s: %w", group.SourceFolderID, err))
	}
	unreadIDs := make([]string, 0, len(original))
	for _, msg := range original {
		if !msg.IsRead {
			unreadIDs = append(unreadIDs, msg.ID)
		}
	}
	if len(unreadIDs) > 0 {
		if err := o.setFlags(unreadIDs, "read", false); err != nil {
			errs = append(errs, fmt.Errorf("restore original unread state: %w", err))
		}
	}
	return errors.Join(errs...)
}

// restoreGroupFolder moves a provider group's messages back to their source
// folder when a later leg of a compound action failed after the move.
func (o *mailboxActionOperations) restoreGroupFolder(group v1.ActionProviderGroup) error {
	messages, err := o.loadActionMessages(group.MessageIDs)
	if err != nil {
		return err
	}
	source, err := o.runtime.folders.Get(group.SourceFolderID)
	if err != nil {
		return err
	}
	if source == nil || source.AccountID != group.AccountID {
		return fmt.Errorf("source folder %s is unavailable for move compensation", group.SourceFolderID)
	}
	return o.moveToFolder(messages, source)
}

func (o *mailboxActionOperations) moveToFolder(messages []*message.Message, destination *folder.Folder) error {
	if o.moveToFolderFunc != nil {
		return o.moveToFolderFunc(messages, destination)
	}
	if len(messages) == 0 {
		return nil
	}
	byAccount := groupMessagesByAccount(messages)
	for _, accountID := range sortedMessageGroupKeys(byAccount) {
		group := messagesForAccount(byAccount, accountID)
		if accountID != destination.AccountID {
			for _, msg := range group {
				if err := o.runtime.filterer.MoveFilterMessage(o.operationContext(), msg, destination.ID); err != nil {
					return err
				}
			}
			continue
		}
		client, gmailAccount, err := o.gmailClient(accountID)
		if err != nil {
			return err
		}
		if gmailAccount {
			if err := o.moveGmailMessages(client, group, destination); err != nil {
				return err
			}
			continue
		}
		byFolder := groupMessagesByFolder(group)
		for _, folderID := range sortedMessageGroupKeys(byFolder) {
			source, err := o.runtime.folders.Get(folderID)
			if err != nil {
				return err
			}
			if source == nil || source.AccountID != accountID || source.Path == "" {
				return fmt.Errorf("source folder %s is unavailable in account %s", folderID, accountID)
			}
			if source.ID == destination.ID {
				continue
			}
			if err := o.moveIMAPMessages(messagesForAccount(byFolder, folderID), source, destination); err != nil {
				return err
			}
		}
	}
	return nil
}

func (o *mailboxActionOperations) moveGmailMessages(client *gmail.Client, messages []*message.Message, destination *folder.Folder) error {
	if len(messages) == 0 {
		return nil
	}
	accountID := messages[0].AccountID
	unlock, err := o.runtime.syncEngine.LockGmailAccount(o.operationContext(), accountID)
	if err != nil {
		return err
	}
	defer unlock()
	byFolder := groupMessagesByFolder(messages)
	for _, sourceID := range sortedMessageGroupKeys(byFolder) {
		group := messagesForAccount(byFolder, sourceID)
		if sourceID == destination.ID {
			continue
		}
		source, err := o.runtime.folders.Get(sourceID)
		if err != nil {
			return err
		}
		if source == nil || source.AccountID != accountID {
			return fmt.Errorf("source folder %s is unavailable", sourceID)
		}
		providerIDs := make([]string, 0, len(group))
		for _, msg := range group {
			providerID, err := o.runtime.syncEngine.GmailProviderID(o.operationContext(), client, msg)
			if err != nil {
				return err
			}
			providerIDs = append(providerIDs, providerID)
		}
		addLabel, err := o.runtime.syncEngine.GmailLabelID(o.operationContext(), client, destination)
		if err != nil {
			return err
		}
		removeLabel, err := o.runtime.syncEngine.GmailLabelID(o.operationContext(), client, source)
		if err != nil {
			return err
		}
		adds, removes := []string{}, []string{}
		if addLabel != "" {
			adds = append(adds, addLabel)
		}
		if removeLabel != "" && removeLabel != addLabel {
			removes = append(removes, removeLabel)
		}
		if destination.Type == folder.TypeArchive || destination.Type == folder.TypeAll {
			removes = append(removes, "INBOX")
		}
		if destination.Type == folder.TypeInbox {
			removes = append(removes, "SPAM", "TRASH")
		}
		if err := client.Modify(o.operationContext(), providerIDs, adds, removes); err != nil {
			return err
		}
		ids := make([]string, 0, len(group))
		for _, msg := range group {
			ids = append(ids, msg.ID)
		}
		if err := o.runtime.messages.MoveGmailMessages(ids, destination.ID); err != nil {
			return err
		}
	}
	return nil
}

func (o *mailboxActionOperations) moveIMAPMessages(messages []*message.Message, source, destination *folder.Folder) error {
	if len(messages) == 0 {
		return nil
	}
	ordered := append([]*message.Message(nil), messages...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].UID < ordered[j].UID })
	uids := make([]goimap.UID, 0, len(ordered))
	for _, msg := range ordered {
		if msg.UID == 0 || int32(msg.UID) < 0 {
			return fmt.Errorf("message %s has no stable UID in its source folder", msg.ID)
		}
		uids = append(uids, goimap.UID(msg.UID))
	}
	accountID := ordered[0].AccountID
	conn, err := o.runtime.pool.GetConnection(o.operationContext(), accountID)
	if err != nil {
		return err
	}
	client := conn.Client()
	caps := client.Caps()
	baseline := map[string]map[uint32]bool{}
	if !caps.Has(goimap.CapUIDPlus) {
		for _, msg := range ordered {
			if msg.MessageID == "" {
				o.runtime.pool.Release(conn)
				return fmt.Errorf("cannot safely map a destination copy for message %s without UIDPLUS or Message-ID", msg.ID)
			}
		}
	}
	{
		if _, err = client.SelectMailbox(o.operationContext(), destination.Path); err != nil {
			o.runtime.pool.Discard(conn)
			return fmt.Errorf("select destination folder: %w", err)
		}
		for _, msg := range ordered {
			if msg.MessageID == "" {
				continue
			}
			if _, found := baseline[msg.MessageID]; found {
				continue
			}
			uids, err := searchMessageUIDs(client, msg.MessageID)
			if err != nil {
				o.runtime.pool.Discard(conn)
				return fmt.Errorf("snapshot destination before move: %w", err)
			}
			baseline[msg.MessageID] = uids
		}
	}
	if _, err = client.SelectMailbox(o.operationContext(), source.Path); err != nil {
		o.runtime.pool.Discard(conn)
		return fmt.Errorf("select source folder: %w", err)
	}
	destinationUIDs, err := client.CopyMessages(uids, destination.Path)
	if err != nil {
		o.runtime.pool.Discard(conn)
		return fmt.Errorf("copy messages to destination; source preserved: %w", err)
	}
	resolved := make(map[string]uint32, len(ordered))
	if len(destinationUIDs) == len(ordered) {
		for i, uid := range destinationUIDs {
			resolved[ordered[i].ID] = uint32(uid)
		}
	}
	if len(resolved) != len(ordered) {
		if _, err = client.SelectMailbox(o.operationContext(), destination.Path); err != nil {
			o.runtime.pool.Discard(conn)
			return fmt.Errorf("destination copy succeeded but source is preserved; select destination for UID resolution: %w", err)
		}
		groups := map[string][]*message.Message{}
		for _, msg := range ordered {
			if msg.MessageID == "" {
				o.runtime.pool.Release(conn)
				return fmt.Errorf("destination copy succeeded but source is preserved: no Message-ID available to resolve the new UID for %s", msg.ID)
			}
			groups[msg.MessageID] = append(groups[msg.MessageID], msg)
		}
		for messageID, group := range groups {
			candidates, err := searchMessageUIDs(client, messageID)
			if err != nil {
				o.runtime.pool.Discard(conn)
				return fmt.Errorf("destination copy succeeded but source is preserved; resolve destination UIDs: %w", err)
			}
			for uid := range baseline[messageID] {
				delete(candidates, uid)
			}
			if len(candidates) != len(group) {
				o.runtime.pool.Release(conn)
				return fmt.Errorf("destination copy succeeded but source is preserved: expected %d new destination copies for message-id %q, found %d", len(group), messageID, len(candidates))
			}
			orderedUIDs := make([]uint32, 0, len(candidates))
			for uid := range candidates {
				orderedUIDs = append(orderedUIDs, uid)
			}
			sort.Slice(orderedUIDs, func(i, j int) bool { return orderedUIDs[i] < orderedUIDs[j] })
			sort.Slice(group, func(i, j int) bool { return group[i].UID < group[j].UID })
			for i, msg := range group {
				resolved[msg.ID] = orderedUIDs[i]
			}
		}
	}
	if _, err = client.SelectMailbox(o.operationContext(), source.Path); err != nil {
		o.runtime.pool.Discard(conn)
		return fmt.Errorf("destination copy confirmed but source is preserved; reselect source: %w", err)
	}
	ids := make([]string, 0, len(ordered))
	for _, msg := range ordered {
		ids = append(ids, msg.ID)
	}
	if err := o.runtime.messages.MoveMessages(ids, destination.ID); err != nil {
		o.runtime.pool.Release(conn)
		return fmt.Errorf("destination copy confirmed but local move failed; source preserved: %w", err)
	}
	if _, err := o.runtime.messages.ReconcileMovedMessageUIDs(destination.ID, resolved); err != nil {
		o.runtime.pool.Release(conn)
		return fmt.Errorf("destination copy confirmed but local UID reconciliation failed; source preserved: %w", err)
	}
	for _, uid := range uids {
		if err := mailmove.DeleteCopiedSource(client, uid); err != nil {
			o.runtime.pool.Discard(conn)
			return fmt.Errorf("destination copy confirmed; source cleanup failed: %w", err)
		}
	}
	o.runtime.pool.Release(conn)
	return nil
}

func searchMessageUIDs(client *imap.Client, messageID string) (map[uint32]bool, error) {
	search, err := client.RawClient().UIDSearch(&goimap.SearchCriteria{
		Header:  []goimap.SearchCriteriaHeaderField{{Key: "MESSAGE-ID", Value: messageID}},
		NotFlag: []goimap.Flag{goimap.FlagDeleted},
	}, nil).Wait()
	if err != nil {
		return nil, err
	}
	uids := make(map[uint32]bool, len(search.AllUIDs()))
	for _, uid := range search.AllUIDs() {
		uids[uint32(uid)] = true
	}
	return uids, nil
}

func (o *mailboxActionOperations) gmailClient(accountID string) (*gmail.Client, bool, error) {
	if o.gmailClientFunc != nil {
		return o.gmailClientFunc(accountID)
	}
	client, err := o.runtime.syncEngine.GmailClient(o.operationContext(), accountID)
	if errors.Is(err, gmail.ErrNotCapable) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return client, true, nil
}

func (o *mailboxActionOperations) operationContext() context.Context {
	if o != nil && o.contextOverride != nil {
		return o.contextOverride
	}
	if o == nil || o.runtime == nil {
		return context.Background()
	}
	o.runtime.ctxMu.RLock()
	ctx := o.runtime.ctx
	o.runtime.ctxMu.RUnlock()
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

func groupMessagesByAccount(messages []*message.Message) map[string][]*message.Message {
	groups := make(map[string][]*message.Message)
	for _, msg := range messages {
		groups[msg.AccountID] = append(groups[msg.AccountID], msg)
	}
	return groups
}

func groupMessagesByFolder(messages []*message.Message) map[string][]*message.Message {
	groups := make(map[string][]*message.Message)
	for _, msg := range messages {
		groups[msg.FolderID] = append(groups[msg.FolderID], msg)
	}
	return groups
}

func messagesForAccount(groups map[string][]*message.Message, key string) []*message.Message {
	return groups[key]
}

func sortedMessageGroupKeys(groups map[string][]*message.Message) []string {
	keys := make([]string, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

var _ v1.ActionOperations = (*mailboxActionOperations)(nil)
var _ operations.MessageLookup = (*mailboxActionOperations)(nil)
var _ v1.ActionProviderGroupOperations = (*mailboxActionOperations)(nil)

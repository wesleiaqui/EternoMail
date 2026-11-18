package mailmove

import (
	"context"
	"fmt"

	"github.com/hkdb/aerion/internal/folder"
	"github.com/hkdb/aerion/internal/mailfilter"
	"github.com/hkdb/aerion/internal/message"
)

// FilterMessageReader reloads a locally cached message before an automatic
// filter changes it. Sync callbacks can be delayed, so the original location
// must still be current when the move starts.
type FilterMessageReader interface {
	GetByIDs(ids []string) ([]*message.Message, error)
}

// FilterMoveFunc performs the actual move through the caller's shared mailbox
// operation layer. Implementations must preserve the copy-before-cleanup rule
// for cross-account moves.
type FilterMoveFunc func(ctx context.Context, ruleID string, source *message.Message, destination *folder.Folder) error

// FilterOperation applies one already-matched automatic rule using a
// provider-specific move adapter. Rule selection remains in mailfilter, while
// the stale-message and cross-account checks are shared by desktop and
// headless service callers.
type FilterOperation struct {
	Messages FilterMessageReader
	Move     FilterMoveFunc
}

// Apply moves msg when it still exists in its original folder. A stale or
// deleted message is skipped and reported as applied=false; lookup and move
// failures are returned to the caller.
func (o FilterOperation) Apply(ctx context.Context, rule *mailfilter.Rule, destination *folder.Folder, msg *message.Message) (bool, error) {
	if ctx == nil || rule == nil || destination == nil || msg == nil {
		return false, fmt.Errorf("filter move requires a context, rule, destination, and message")
	}
	if rule.ID == "" || rule.DestFolderID == "" || rule.DestFolderID != destination.ID {
		return false, fmt.Errorf("filter move destination does not match the rule")
	}
	if msg.ID == "" || msg.AccountID == "" || msg.FolderID == "" {
		return false, fmt.Errorf("filter move requires a message with an account and folder")
	}
	if o.Messages == nil || o.Move == nil {
		return false, fmt.Errorf("filter move operation is not configured")
	}

	current, err := o.Messages.GetByIDs([]string{msg.ID})
	if err != nil {
		return false, fmt.Errorf("reload filter target: %w", err)
	}
	if len(current) == 0 || current[0] == nil {
		return false, nil
	}
	if current[0].ID != msg.ID || current[0].AccountID != msg.AccountID || current[0].FolderID != msg.FolderID {
		return false, nil
	}
	if current[0].FolderID == destination.ID {
		return true, nil
	}
	if err := o.Move(ctx, rule.ID, current[0], destination); err != nil {
		return false, err
	}
	return true, nil
}

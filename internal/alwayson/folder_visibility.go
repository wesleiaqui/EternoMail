package alwayson

import (
	"context"
	"fmt"
)

// SetFolderVisibility changes the helper-owned local preference. Refresh is
// synchronous so a failed catch-up is never reported as successful.
func (r *SyncRuntime) SetFolderVisibility(ctx context.Context, accountID string, ids []string, hidden, refresh bool) error {
	if hidden && refresh {
		return fmt.Errorf("cannot refresh hidden folders")
	}
	if r == nil || r.folders == nil || r.syncEngine == nil {
		return fmt.Errorf("mailbox runtime is not open")
	}
	if err := r.syncEngine.SetFolderVisibility(ctx, accountID, ids, hidden); err != nil {
		return err
	}
	if refresh {
		acc, err := r.accounts.Get(accountID)
		if err != nil {
			return err
		}
		if acc == nil {
			return fmt.Errorf("account not found")
		}
		for _, id := range ids {
			if err := r.syncEngine.SyncMessages(ctx, accountID, id, acc.SyncPeriodDays, false); err != nil {
				return fmt.Errorf("folder shown, but refresh failed: %w", err)
			}
		}
	}
	return nil
}

package app

import (
	"fmt"
	v1 "github.com/hkdb/aerion/internal/automation/v1"
)

// SetFolderVisibility changes account-owned local visibility, not subscriptions.
func (a *App) SetFolderVisibility(accountID string, ids []string, hidden, refresh bool) error {
	if hidden && refresh {
		return fmt.Errorf("cannot refresh hidden folders")
	}
	if a.mailboxService != nil {
		ctx := a.helperCommandContext()
		status := a.mailboxService.status(ctx)
		if status.Connected {
			if !status.WriteAuthorized {
				return errDesktopMailboxWriteAuthorizationRequired
			}
			client, err := a.mailboxService.getClient()
			if err != nil {
				return err
			}
			err = client.SetFolderVisibility(ctx, v1.FolderVisibilityRequest{AccountID: accountID, FolderIDs: ids, Hidden: &hidden, Refresh: refresh})
			a.emitRuntimeEvent("folders:visibilityChanged", map[string]string{"accountId": accountID})
			return err
		}
		if status.ErrorCode != "service_unavailable" || a.mailboxService.isRemote() {
			return fmt.Errorf("owning mailbox service unavailable: %s", status.Message)
		}
	}
	if err := a.ensureSyncOwnership(); err != nil {
		return err
	}
	if err := a.syncEngine.SetFolderVisibility(a.ctx, accountID, ids, hidden); err != nil {
		return err
	}
	for _, id := range ids {
		if hidden {
			a.CancelFolderSync(accountID, id)
			a.cancelFolderBodyPrefetch(accountID, id)
		}
	}
	a.emitRuntimeEvent("folders:visibilityChanged", map[string]string{"accountId": accountID})
	if refresh {
		for _, id := range ids {
			if err := a.syncFolder(accountID, id, false); err != nil {
				return fmt.Errorf("folder shown, but refresh failed: %w", err)
			}
		}
	}
	return nil
}

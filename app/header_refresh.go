package app

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/hkdb/aerion/internal/folder"
)

// refreshVirtualFolderHeadersInBackground refreshes only message headers for a
// virtual folder's real member folders. Gmail views are already queried live
// through the Gmail API by SearchMail, so they do not need a second sync here.
func (a *App) refreshVirtualFolderHeadersInBackground(virtualFolderID string) error {
	key := "virtual:" + virtualFolderID
	a.refreshMu.Lock()
	if a.refreshLast == nil {
		a.refreshLast = map[string]time.Time{}
		a.refreshActive = map[string]bool{}
	}
	if a.refreshActive[key] || time.Since(a.refreshLast[key]) < 30*time.Second {
		a.refreshMu.Unlock()
		return nil
	}
	a.refreshActive[key] = true
	a.refreshMu.Unlock()

	go func() {
		defer recoverPanic("app.refresh", "virtual-folder headers")
		defer func() {
			a.refreshMu.Lock()
			delete(a.refreshActive, key)
			a.refreshMu.Unlock()
		}()

		err := a.SyncVirtualFolder(virtualFolderID)
		a.refreshMu.Lock()
		a.refreshLast[key] = time.Now()
		a.refreshMu.Unlock()
		if err != nil {
			a.emitRuntimeEvent("virtualfolder:refreshError", map[string]string{
				"virtualFolderId": virtualFolderID,
				"error":           err.Error(),
			})
		} else {
			a.emitRuntimeEvent("virtualfolder:refreshComplete", map[string]string{"virtualFolderId": virtualFolderID})
		}
	}()
	return nil
}

func virtualFolderRefreshError(virtualFolderID string, errorsByFolder map[string]error) error {
	if len(errorsByFolder) == 0 {
		return nil
	}
	ids := make([]string, 0, len(errorsByFolder))
	for id := range errorsByFolder {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	parts := make([]string, 0, len(ids))
	for _, id := range ids {
		parts = append(parts, id+": "+errorsByFolder[id].Error())
	}
	return fmt.Errorf("virtual folder %q partially refreshed: %s", virtualFolderID, strings.Join(parts, "; "))
}

// RefreshViewHeaders is nonblocking and coalesced. Opening a folder never clears
// its cache or cancels a manual sync. Metadata events update the visible list.
func (a *App) RefreshViewHeaders(accountID, folderID string) error {
	if accountID == "virtual" {
		return a.refreshVirtualFolderHeadersInBackground(folderID)
	}
	if accountID == "unified" && folderID == "replyLater" {
		return nil
	}
	if accountID == "unified" {
		accounts, err := a.accountStore.List()
		if err != nil {
			return err
		}
		for _, acc := range accounts {
			if !acc.Enabled {
				continue
			}
			f, err := a.folderStore.GetByType(acc.ID, folder.Type(folderID))
			if err != nil {
				return err
			}
			if f != nil {
				if err = a.RefreshViewHeaders(acc.ID, f.ID); err != nil {
					return err
				}
			}
		}
		return nil
	}
	f, err := a.folderStore.Get(folderID)
	if err != nil {
		return err
	}
	if f == nil || f.AccountID != accountID {
		return fmt.Errorf("folder account mismatch")
	}
	if f.Hidden {
		return nil
	}
	key := accountID + ":" + folderID
	a.refreshMu.Lock()
	if a.refreshLast == nil {
		a.refreshLast = map[string]time.Time{}
		a.refreshActive = map[string]bool{}
	}
	if a.refreshActive[key] || time.Since(a.refreshLast[key]) < 30*time.Second {
		a.refreshMu.Unlock()
		return nil
	}
	a.refreshActive[key] = true
	a.refreshMu.Unlock()
	go func() {
		defer recoverPanic("app.refresh", "folder metadata")
		defer func() { a.refreshMu.Lock(); delete(a.refreshActive, key); a.refreshMu.Unlock() }()
		a.syncMu.Lock()
		_, running := a.syncContexts[key]
		a.syncMu.Unlock()
		if running {
			return
		}
		if err := a.syncFolder(accountID, folderID, false); err != nil {
			a.emitRuntimeEvent("folder:syncError", map[string]string{"accountId": accountID, "folderId": folderID, "error": err.Error()})
			return
		}
		a.refreshMu.Lock()
		a.refreshLast[key] = time.Now()
		a.refreshMu.Unlock()
	}()
	return nil
}

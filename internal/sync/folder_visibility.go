package sync

import (
	"context"
	"fmt"
	"sort"
)

// SetFolderVisibility waits for any active header/flag reconciliation, then
// changes the preference while holding the same folder locks. Queued syncs
// re-read Hidden under those locks before making provider requests.
func (e *Engine) SetFolderVisibility(ctx context.Context, accountID string, ids []string, hidden bool) error {
	ordered := append([]string{}, ids...)
	sort.Strings(ordered)
	var unlocks []func()
	defer func() {
		for i := len(unlocks) - 1; i >= 0; i-- {
			unlocks[i]()
		}
	}()
	previous := ""
	for _, id := range ordered {
		if id == previous {
			continue
		}
		previous = id
		unlock, err := e.folderLocks.lock(ctx, id)
		if err != nil {
			return err
		}
		unlocks = append(unlocks, unlock)
	}
	return e.folderStore.SetHiddenBatch(accountID, ids, hidden)
}

// LockVisibleFolder serializes destructive cache maintenance with visibility changes.
func (e *Engine) LockVisibleFolder(ctx context.Context, accountID, folderID string) (func(), error) {
	unlock, err := e.folderLocks.lock(ctx, folderID)
	if err != nil {
		return nil, err
	}
	f, err := e.folderStore.Get(folderID)
	if err != nil || f == nil || f.AccountID != accountID || f.Hidden {
		unlock()
		return nil, fmt.Errorf("folder unavailable or hidden")
	}
	return unlock, nil
}

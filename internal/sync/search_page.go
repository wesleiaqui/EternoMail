package sync

import (
	"context"
	"fmt"
	"sort"

	"github.com/emersion/go-imap/v2"
)

// IMAPSearchCursor binds a decreasing UID continuation to one mailbox epoch.
// New arrivals cannot push older matches out of subsequent pages.
type IMAPSearchCursor struct {
	AccountID   string `json:"a"`
	FolderID    string `json:"f"`
	Query       string `json:"q"`
	UIDValidity uint32 `json:"v"`
	Before      uint32 `json:"b"`
}

type IMAPSearchPage struct {
	Results []*IMAPSearchResult
	Next    *IMAPSearchCursor
}

func validateSearchCursor(c IMAPSearchCursor, account, folder, query string, epoch uint32) error {
	if c.AccountID != account || c.FolderID != folder || c.Query != query || c.UIDValidity != epoch || c.Before == 0 {
		return fmt.Errorf("mailbox or search scope changed; start a new search")
	}
	return nil
}

func searchUIDPage(uids []uint32, before uint32, limit int) ([]uint32, uint32) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	selected := make([]uint32, 0, len(uids))
	for _, uid := range uids {
		if uid != 0 && (before == 0 || uid < before) {
			selected = append(selected, uid)
		}
	}
	sort.Slice(selected, func(i, j int) bool { return selected[i] > selected[j] })
	if len(selected) <= limit {
		return selected, 0
	}
	return selected[:limit], selected[limit-1]
}

// SearchIMAPPage searches on the server and fetches fresh envelopes and flags
// for one page only. No body sections or attachment payloads are requested.
func (e *Engine) SearchIMAPPage(ctx context.Context, accountID, folderID, query string, cursor *IMAPSearchCursor, limit int) (*IMAPSearchPage, error) {
	unlock, lockErr := e.folderLocks.lock(ctx, folderID)
	if lockErr != nil {
		return nil, lockErr
	}
	defer unlock()

	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f, err := e.folderStore.Get(folderID)
	if err != nil {
		return nil, err
	}
	if f == nil || f.AccountID != accountID {
		return nil, fmt.Errorf("search folder does not belong to account")
	}
	if f.Hidden {
		return nil, fmt.Errorf("folder is hidden; show it before searching")
	}

	conn, err := e.pool.GetConnection(ctx, accountID)
	if err != nil {
		return nil, err
	}
	defer e.pool.Release(conn)
	mailbox, err := conn.Client().SelectMailbox(ctx, f.Path)
	if err != nil {
		return nil, err
	}
	criteria := &imap.SearchCriteria{}
	if query != "" {
		criteria = buildSearchCriteria(query)
	}
	var before uint32
	if cursor != nil {
		if err := validateSearchCursor(*cursor, accountID, folderID, query, mailbox.UIDValidity); err != nil {
			return nil, err
		}
		before = cursor.Before
		if before == 1 {
			return &IMAPSearchPage{}, nil
		}
		set := imap.UIDSet{}
		set.AddRange(1, imap.UID(before-1))
		criteria.UID = append(criteria.UID, set)
	}
	uids, err := uidSearch(ctx, conn.Client().RawClient(), criteria)
	if err != nil {
		return nil, err
	}
	pageUIDs, next := searchUIDPage(uids, before, limit)
	hits, err := e.fetchEnvelopesForSearch(ctx, conn.Client().RawClient(), accountID, folderID, f.Name, pageUIDs)
	if err != nil {
		return nil, err
	}
	page := &IMAPSearchPage{Results: hits}
	if next != 0 {
		page.Next = &IMAPSearchCursor{AccountID: accountID, FolderID: folderID, Query: query, UIDValidity: mailbox.UIDValidity, Before: next}
	}
	return page, nil
}

// Package mailfilter provides local filter rules that match incoming or
// stored messages and apply actions to them: move (including same-account
// and cross-account), mark read, archive, or move to Trash. Rules may be
// scoped to all accounts or to an explicit set of accounts.
package mailfilter

import (
	"time"
)

// Rule is a local filter rule. A message matches when the rule is enabled,
// the account/folder scope holds, and every non-empty criterion list
// contains at least one pattern that matches. Matching is a pure function of
// local header data; the actions below decide what a match does.
type Rule struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
	// OrderIndex is the priority position: lower values are evaluated first
	// and the first matching rule wins.
	OrderIndex int `json:"orderIndex"`

	// Source scope. An empty account scope matches any account; empty
	// FolderIDs matches any folder on the matched accounts.
	//
	// AccountID is the legacy single-account scope kept for compatibility
	// with rules created before multi-account scope existed. AccountIDs is
	// the explicit multi-account scope and takes precedence when non-empty.
	AccountID  string   `json:"accountId"`
	AccountIDs []string `json:"accountIds"`
	FolderIDs  []string `json:"folderIds"`

	// Match criteria. Each non-empty list requires at least one of its
	// case-insensitive substring patterns to match.
	FromContains    []string `json:"fromContains"`
	ToContains      []string `json:"toContains"`
	SubjectContains []string `json:"subjectContains"`
	ListIDContains  []string `json:"listIdContains"`

	// Actions. DestFolderID is the move destination (any account, including
	// the source account); empty means no move. Archive and Trash relocate
	// the message to the account's Archive or Trash folder. At most one
	// relocation action (DestFolderID, ActionArchive, ActionTrash) may be
	// set; mark-read combines freely with any of them.
	DestFolderID   string `json:"destFolderId"`
	ActionMarkRead bool   `json:"actionMarkRead"`
	ActionArchive  bool   `json:"actionArchive"`
	ActionTrash    bool   `json:"actionTrash"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// Message is the header data a rule is matched against. It mirrors the
// persisted envelope fields so matching stays a pure function of local data.
type Message struct {
	AccountID string
	FolderID  string
	FromName  string
	FromEmail string
	ToNames   []string
	ToEmails  []string
	Subject   string
	ListID    string
}

// ScopeAccountIDs returns the effective account scope: the explicit
// multi-account list when set, otherwise the legacy single account. An
// empty result means the rule applies to every account.
func (r *Rule) ScopeAccountIDs() []string {
	if r == nil {
		return nil
	}
	if len(r.AccountIDs) > 0 {
		out := make([]string, 0, len(r.AccountIDs))
		seen := make(map[string]bool, len(r.AccountIDs))
		for _, id := range r.AccountIDs {
			if id == "" || seen[id] {
				continue
			}
			seen[id] = true
			out = append(out, id)
		}
		if len(out) > 0 {
			return out
		}
	}
	if r.AccountID != "" {
		return []string{r.AccountID}
	}
	return nil
}

// RelocationCount reports how many relocation actions the rule carries.
// Validation requires this to be at most one.
func (r *Rule) RelocationCount() int {
	if r == nil {
		return 0
	}
	count := 0
	if r.DestFolderID != "" {
		count++
	}
	if r.ActionArchive {
		count++
	}
	if r.ActionTrash {
		count++
	}
	return count
}

// HasActions reports whether the rule does anything at all when it matches.
func (r *Rule) HasActions() bool {
	if r == nil {
		return false
	}
	return r.ActionMarkRead || r.RelocationCount() > 0
}

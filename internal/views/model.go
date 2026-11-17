// Package views defines the shared contract for mail views, threads and
// message references (D-002, UX-002).
//
// The contract is data only. It does not query the database, change how
// threads are built, or render anything. The GUI, the CLI and the agent API
// read and write these types, so they carry no UI state. The JSON shapes are
// mirrored by frontend/src/lib/views/types.ts and checked against the shared
// fixtures in testdata/.
//
// Four choices stay independent of each other:
//
//   - Scope: which messages are eligible (this package).
//   - Grouping: how eligible items are organized (this package).
//   - Reader: how an opened thread is rendered (UI only, see types.ts).
//   - Layout: which panels are visible (UI only, see types.ts).
package views

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// ScopeKind says which messages a view makes eligible.
type ScopeKind string

const (
	// ScopeAllInboxes is the Inbox folders of every enabled account, resolved
	// at query time.
	ScopeAllInboxes ScopeKind = "all_inboxes"
	// ScopeAllAccounts includes every folder of every enabled account.
	ScopeAllAccounts ScopeKind = "all_accounts"
	// ScopeAccount includes every folder of one enabled account.
	ScopeAccount ScopeKind = "account"
	// ScopeFolder is one real folder of one account.
	ScopeFolder ScopeKind = "folder"
	// ScopeUnifiedFolder is one system folder resolved across enabled accounts.
	ScopeUnifiedFolder ScopeKind = "unified_folder"
	// ScopeVirtualFolder is a saved cross-account view.
	ScopeVirtualFolder ScopeKind = "virtual_folder"
	// ScopeSearch is a query, optionally limited to another scope.
	ScopeSearch ScopeKind = "search"
)

// Grouping says how eligible messages are organized in a list.
type Grouping string

const (
	// GroupingThreads groups messages into threads. It is the only grouping
	// implemented today.
	GroupingThreads Grouping = "threads"
	// GroupingPeople is reserved for the later People view.
	GroupingPeople Grouping = "people"
)

// Scope identifies the messages a view contains. Fields that do not belong to
// the scope's kind must be empty, so one scope has exactly one representation.
type Scope struct {
	Kind            ScopeKind `json:"kind"`
	AccountID       string    `json:"accountId,omitempty"`
	FolderID        string    `json:"folderId,omitempty"`
	FolderType      string    `json:"folderType,omitempty"`
	VirtualFolderID string    `json:"virtualFolderId,omitempty"`
	Query           string    `json:"query,omitempty"`
	// Within limits a search. Nil means all inboxes. It must not be a search.
	Within *Scope `json:"within,omitempty"`
}

// Validate reports whether the scope has the fields its kind requires and no others.
func (s Scope) Validate() error {
	switch s.Kind {
	case ScopeAllInboxes:
		if s.AccountID != "" || s.FolderID != "" || s.FolderType != "" || s.VirtualFolderID != "" || s.Query != "" || s.Within != nil {
			return errors.New("all_inboxes scope takes no other fields")
		}
	case ScopeAllAccounts:
		if s.AccountID != "" || s.FolderID != "" || s.FolderType != "" || s.VirtualFolderID != "" || s.Query != "" || s.Within != nil {
			return errors.New("all_accounts scope takes no other fields")
		}
	case ScopeAccount:
		if s.AccountID == "" {
			return errors.New("account scope needs accountId")
		}
		if s.FolderID != "" || s.FolderType != "" || s.VirtualFolderID != "" || s.Query != "" || s.Within != nil {
			return errors.New("account scope takes only accountId")
		}
	case ScopeFolder:
		if s.AccountID == "" || s.FolderID == "" {
			return errors.New("folder scope needs accountId and folderId")
		}
		if s.FolderType != "" || s.VirtualFolderID != "" || s.Query != "" || s.Within != nil {
			return errors.New("folder scope takes only accountId and folderId")
		}
	case ScopeUnifiedFolder:
		if s.FolderType != "replyLater" && s.FolderType != "readLater" && s.FolderType != "drafts" && s.FolderType != "sent" && s.FolderType != "trash" && s.FolderType != "starred" && s.FolderType != "archive" && s.FolderType != "spam" {
			return errors.New("unified folder scope needs a supported folderType")
		}
		if s.AccountID != "" || s.FolderID != "" || s.VirtualFolderID != "" || s.Query != "" || s.Within != nil {
			return errors.New("unified folder scope takes only folderType")
		}
	case ScopeVirtualFolder:
		if s.VirtualFolderID == "" {
			return errors.New("virtual_folder scope needs virtualFolderId")
		}
		if s.AccountID != "" || s.FolderID != "" || s.FolderType != "" || s.Query != "" || s.Within != nil {
			return errors.New("virtual_folder scope takes only virtualFolderId")
		}
	case ScopeSearch:
		if strings.TrimSpace(s.Query) == "" {
			return errors.New("search scope needs a query")
		}
		if s.AccountID != "" || s.FolderID != "" || s.FolderType != "" || s.VirtualFolderID != "" {
			return errors.New("search scope takes only query and within")
		}
		if s.Within == nil {
			return nil
		}
		if s.Within.Kind == ScopeSearch {
			return errors.New("a search cannot be limited to another search")
		}
		if err := s.Within.Validate(); err != nil {
			return fmt.Errorf("search within: %w", err)
		}
	default:
		return fmt.Errorf("unknown scope kind %q", s.Kind)
	}
	return nil
}

// ScopeKey returns a stable identifier for a scope. Equal scopes give equal
// keys, a search with no scope equals a search within all inboxes, and ids are
// escaped so no two different scopes can share a key.
func ScopeKey(s Scope) string {
	switch s.Kind {
	case ScopeFolder:
		return "folder:" + url.PathEscape(s.AccountID) + "/" + url.PathEscape(s.FolderID)
	case ScopeUnifiedFolder:
		return "unified_folder:" + url.PathEscape(s.FolderType)
	case ScopeAccount:
		return "account:" + url.PathEscape(s.AccountID)
	case ScopeVirtualFolder:
		return "virtual_folder:" + url.PathEscape(s.VirtualFolderID)
	case ScopeSearch:
		within := Scope{Kind: ScopeAllInboxes}
		if s.Within != nil {
			within = *s.Within
		}
		sum := sha256.Sum256([]byte(s.Query + "\x00" + ScopeKey(within)))
		return "search:" + hex.EncodeToString(sum[:8])
	default:
		return string(s.Kind)
	}
}

// ViewSpec is what a client asks for: a scope and how to group it.
type ViewSpec struct {
	Scope    Scope    `json:"scope"`
	Grouping Grouping `json:"grouping"`
}

// MessageRef identifies one local message. The account is part of the
// identity: two accounts can hold copies of the same email, and each copy is
// its own message.
type MessageRef struct {
	AccountID string `json:"accountId"`
	MessageID string `json:"messageId"`
}

// Key returns a string that is unique per account and message.
func (r MessageRef) Key() string {
	return url.PathEscape(r.AccountID) + "/" + url.PathEscape(r.MessageID)
}

// ThreadRef identifies one thread within one account.
type ThreadRef struct {
	AccountID string `json:"accountId"`
	ThreadID  string `json:"threadId"`
}

// Key returns a string that is unique per account and thread.
func (r ThreadRef) Key() string {
	return url.PathEscape(r.AccountID) + "/" + url.PathEscape(r.ThreadID)
}

// Address is a sender or participant.
type Address struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

// AttachmentSummary describes an attachment without exposing its bytes or
// local storage path.
type AttachmentSummary struct {
	ID          string `json:"id"`
	Filename    string `json:"filename"`
	ContentType string `json:"contentType"`
	Size        int    `json:"size"`
	ContentID   string `json:"contentId,omitempty"`
	IsInline    bool   `json:"isInline"`
}

// ContentState says whether a message's body is available locally.
type ContentState string

const (
	// ContentCached means the body has been downloaded and can be read offline.
	ContentCached ContentState = "cached"
	// ContentNotFetched means only the header is stored. Reading it needs an
	// explicit fetch.
	ContentNotFetched ContentState = "not_fetched"
	// ContentFetchFailed means a fetch ran but produced no usable body.
	ContentFetchFailed ContentState = "fetch_failed"
)

// ThreadMessage is one message in an opened thread.
type ThreadMessage struct {
	Ref          MessageRef `json:"ref"`
	FolderID     string     `json:"folderId"`
	RFCMessageID string     `json:"rfcMessageId,omitempty"`
	Subject      string     `json:"subject,omitempty"`
	Snippet      string     `json:"snippet,omitempty"`
	From         Address    `json:"from"`
	ToList       string     `json:"toList,omitempty"`
	CcList       string     `json:"ccList,omitempty"`
	BccList      string     `json:"bccList,omitempty"`
	ReplyTo      string     `json:"replyTo,omitempty"`
	Date         time.Time  `json:"date"`
	ReceivedAt   *time.Time `json:"receivedAt,omitempty"`
	// Matched is true when the message is in the view's scope. Other messages
	// are conversation context that makes the thread understandable. An
	// action on the view must not reach them unless the target says so.
	Matched            bool                `json:"matched"`
	IsRead             bool                `json:"isRead"`
	IsStarred          bool                `json:"isStarred"`
	IsAnswered         bool                `json:"isAnswered"`
	IsForwarded        bool                `json:"isForwarded,omitempty"`
	IsDraft            bool                `json:"isDraft,omitempty"`
	IsDeleted          bool                `json:"isDeleted,omitempty"`
	Size               int                 `json:"size,omitempty"`
	InboxCategory      string              `json:"inboxCategory,omitempty"`
	HasAttachments     bool                `json:"hasAttachments"`
	Attachments        []AttachmentSummary `json:"attachments,omitempty"`
	SMIMEStatus        string              `json:"smimeStatus,omitempty"`
	SMIMESignerEmail   string              `json:"smimeSignerEmail,omitempty"`
	SMIMESignerSubject string              `json:"smimeSignerSubject,omitempty"`
	SMIMEEncrypted     bool                `json:"smimeEncrypted,omitempty"`
	HasSMIME           bool                `json:"hasSMIME,omitempty"`
	PGPStatus          string              `json:"pgpStatus,omitempty"`
	PGPSignerEmail     string              `json:"pgpSignerEmail,omitempty"`
	PGPSignerKeyID     string              `json:"pgpSignerKeyId,omitempty"`
	PGPEncrypted       bool                `json:"pgpEncrypted,omitempty"`
	HasPGP             bool                `json:"hasPGP,omitempty"`
	ContentState       ContentState        `json:"contentState"`
}

// BodyCounts counts how many bodies are in each state.
type BodyCounts struct {
	Total       int `json:"total"`
	Cached      int `json:"cached"`
	NotFetched  int `json:"notFetched"`
	FetchFailed int `json:"fetchFailed"`
}

// Completeness tells a client whether it has seen the whole thread. A summary
// built from an incomplete thread must say so.
type Completeness struct {
	Complete bool       `json:"complete"`
	Bodies   BodyCounts `json:"bodies"`
}

// CompletenessOf counts the body states of messages. The result is complete
// only when every body is cached.
func CompletenessOf(messages []ThreadMessage) Completeness {
	var counts BodyCounts
	for _, m := range messages {
		counts.Total++
		switch m.ContentState {
		case ContentCached:
			counts.Cached++
		case ContentFetchFailed:
			counts.FetchFailed++
		default:
			counts.NotFetched++
		}
	}
	return Completeness{
		Complete: counts.NotFetched == 0 && counts.FetchFailed == 0,
		Bodies:   counts,
	}
}

// MatchedRefs returns the refs of the messages that are in the view's scope.
func MatchedRefs(messages []ThreadMessage) []MessageRef {
	refs := []MessageRef{}
	for _, m := range messages {
		if m.Matched {
			refs = append(refs, m.Ref)
		}
	}
	return refs
}

// ThreadView is an opened thread: every known message in chronological order,
// with the ones that match the view marked.
type ThreadView struct {
	Ref          ThreadRef       `json:"ref"`
	Subject      string          `json:"subject"`
	LatestDate   time.Time       `json:"latestDate"`
	Participants []Address       `json:"participants"`
	MessageCount int             `json:"messageCount"`
	MatchedCount int             `json:"matchedCount"`
	Messages     []ThreadMessage `json:"messages"`
	Completeness Completeness    `json:"completeness"`
}

// ThreadSummary is one row of a list. Matched names the messages that put the
// thread in the view; MessageCount also counts conversation context.
type ThreadSummary struct {
	Ref            ThreadRef    `json:"ref"`
	AccountName    string       `json:"accountName,omitempty"`
	FolderID       string       `json:"folderId,omitempty"`
	Subject        string       `json:"subject"`
	Snippet        string       `json:"snippet"`
	LatestDate     time.Time    `json:"latestDate"`
	Participants   []Address    `json:"participants"`
	MessageCount   int          `json:"messageCount"`
	UnreadCount    int          `json:"unreadCount"`
	HasAttachments bool         `json:"hasAttachments"`
	IsStarred      bool         `json:"isStarred"`
	Matched        []MessageRef `json:"matched"`
}

// PartialError says one account could not answer. The page is still returned
// but is not complete.
type PartialError struct {
	AccountID   string `json:"accountId"`
	AccountName string `json:"accountName,omitempty"`
	Code        string `json:"code"`
	Message     string `json:"message"`
	Reauthorize bool   `json:"reauthorize,omitempty"`
}

// ThreadPage is one page of a list. Complete is false when any account failed
// or when the list was cut short, so a client never mistakes a partial answer
// for the whole mailbox.
type ThreadPage struct {
	Items      []ThreadSummary `json:"items"`
	NextCursor string          `json:"nextCursor,omitempty"`
	Complete   bool            `json:"complete"`
	Errors     []PartialError  `json:"errors,omitempty"`
}

// Position is where the next page starts: after this thread, in latest-date order.
type Position struct {
	LatestDate time.Time
	Thread     ThreadRef
}

var (
	// ErrCursorInvalid means the cursor is malformed or from an unsupported version.
	ErrCursorInvalid = errors.New("invalid cursor")
	// ErrCursorScope means the cursor was issued for a different scope.
	ErrCursorScope = errors.New("cursor belongs to a different scope")
)

const cursorVersion = 1

type cursorPayload struct {
	Version    int       `json:"v"`
	Scope      string    `json:"s"`
	LatestDate time.Time `json:"d"`
	AccountID  string    `json:"a"`
	ThreadID   string    `json:"t"`
}

// EncodeCursor returns an opaque cursor bound to the scope it was issued for.
// Clients treat it as a string and must not parse it.
func EncodeCursor(scope Scope, pos Position) (string, error) {
	if err := scope.Validate(); err != nil {
		return "", fmt.Errorf("cursor scope: %w", err)
	}
	data, err := json.Marshal(cursorPayload{
		Version:    cursorVersion,
		Scope:      ScopeKey(scope),
		LatestDate: pos.LatestDate.UTC(),
		AccountID:  pos.Thread.AccountID,
		ThreadID:   pos.Thread.ThreadID,
	})
	if err != nil {
		return "", fmt.Errorf("encode cursor: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}

// DecodeCursor reads a cursor and checks that it was issued for the same scope.
func DecodeCursor(scope Scope, cursor string) (Position, error) {
	data, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil || len(data) == 0 {
		return Position{}, ErrCursorInvalid
	}
	var payload cursorPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		return Position{}, ErrCursorInvalid
	}
	if payload.Version != cursorVersion {
		return Position{}, ErrCursorInvalid
	}
	if payload.Scope != ScopeKey(scope) {
		return Position{}, ErrCursorScope
	}
	return Position{
		LatestDate: payload.LatestDate,
		Thread:     ThreadRef{AccountID: payload.AccountID, ThreadID: payload.ThreadID},
	}, nil
}

package mailservice

import (
	"github.com/hkdb/aerion/internal/message"
)

// MailSearchRequest identifies a bounded provider-aware conversation search.
type MailSearchRequest struct {
	SessionID       string   `json:"sessionId"`
	Query           string   `json:"query"`
	AccountIDs      []string `json:"accountIds"`
	FolderIDs       []string `json:"folderIds"`
	VirtualFolderID string   `json:"virtualFolderId"`
	Cursor          string   `json:"cursor"`
	Limit           int      `json:"limit"`
	Filter          string   `json:"filter"`
}

// MailSearchIssue reports one account's partial search failure.
type MailSearchIssue struct {
	AccountID   string `json:"accountId"`
	AccountName string `json:"accountName"`
	Message     string `json:"message"`
	Reauthorize bool   `json:"reauthorize"`
	Warning     bool   `json:"warning,omitempty"`
}

// MailSearchPage contains header-only conversation results and retry state.
type MailSearchPage struct {
	Conversations []*message.Conversation `json:"conversations"`
	Cursor        string                  `json:"cursor"`
	Issues        []MailSearchIssue       `json:"issues"`
}

type mailCursor struct {
	Revision string            `json:"r,omitempty"`
	Query    string            `json:"q"`
	Virtual  string            `json:"v"`
	Filter   string            `json:"f,omitempty"`
	Accounts []string          `json:"a"`
	Folders  []string          `json:"x,omitempty"`
	Pages    map[string]string `json:"p"`
	Seen     map[string]bool   `json:"s"`
}

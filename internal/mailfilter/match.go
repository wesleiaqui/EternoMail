package mailfilter

import (
	"encoding/json"
	"strings"

	"github.com/hkdb/aerion/internal/message"
)

// Match reports whether the rule matches message m. Matching is
// case-insensitive substring matching and is independent of any action
// destination:
//
//   - an empty account scope matches any account;
//   - an empty folder scope (rule.FolderIDs) matches any folder;
//   - each non-empty criterion list passes when at least one of its patterns
//     is a substring of the corresponding header data (from: sender address
//     and name; to: every To and Cc address and name; subject: subject line;
//     list-id: the message's List-ID header value).
func Match(rule *Rule, m *Message) bool {
	if rule == nil || m == nil {
		return false
	}
	if !rule.Enabled {
		return false
	}
	if scope := rule.ScopeAccountIDs(); len(scope) > 0 && !containsFold(scope, m.AccountID) {
		return false
	}
	if len(rule.FolderIDs) > 0 && !containsFold(rule.FolderIDs, m.FolderID) {
		return false
	}
	if len(rule.FromContains) > 0 && !anyContainsFold(rule.FromContains, m.fromHaystack()) {
		return false
	}
	if len(rule.ToContains) > 0 && !anyContainsFold(rule.ToContains, m.toHaystack()) {
		return false
	}
	if len(rule.SubjectContains) > 0 && !anyContainsFold(rule.SubjectContains, strings.ToLower(m.Subject)) {
		return false
	}
	if len(rule.ListIDContains) > 0 && !anyContainsFold(rule.ListIDContains, strings.ToLower(m.ListID)) {
		return false
	}
	return true
}

// FromMessage builds matcher criteria from a persisted message. The List-ID
// criterion uses the message's transient ListID field, populated by sync
// when headers were parsed for this message; stored messages match List-ID
// rules through the SQL preview/apply path, which joins the persisted
// message_list_ids table instead.
func FromMessage(m *message.Message) *Message {
	out := &Message{
		AccountID: m.AccountID,
		FolderID:  m.FolderID,
		FromName:  m.FromName,
		FromEmail: m.FromEmail,
		Subject:   m.Subject,
		ListID:    m.ListID,
	}
	for _, list := range []string{m.ToList, m.CcList} {
		for _, addr := range parseAddressList(list) {
			out.ToNames = append(out.ToNames, addr.Name)
			out.ToEmails = append(out.ToEmails, addr.Email)
		}
	}
	return out
}

// parseAddressList decodes a stored To/Cc list (JSON array of
// {name, email}); empty or malformed input yields no addresses.
func parseAddressList(jsonValue string) []message.Address {
	trimmed := strings.TrimSpace(jsonValue)
	if trimmed == "" {
		return nil
	}
	var list []message.Address
	if err := json.Unmarshal([]byte(trimmed), &list); err != nil {
		return nil
	}
	return list
}

// fromHaystack is the lower-cased search space for FromContains patterns:
// the sender's address and display name.
func (m *Message) fromHaystack() string {
	return strings.ToLower(m.FromEmail + "\x00" + m.FromName)
}

// toHaystack is the lower-cased search space for ToContains patterns: every
// recipient address and display name.
func (m *Message) toHaystack() string {
	parts := make([]string, 0, len(m.ToEmails)+len(m.ToNames))
	parts = append(parts, m.ToEmails...)
	parts = append(parts, m.ToNames...)
	return strings.ToLower(strings.Join(parts, "\x00"))
}

func containsFold(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

// anyContainsFold reports whether any non-empty pattern is a substring of
// haystack; both sides are compared case-insensitively.
func anyContainsFold(patterns []string, haystack string) bool {
	for _, pattern := range patterns {
		pattern = strings.TrimSpace(pattern)
		if pattern == "" {
			continue
		}
		if strings.Contains(haystack, strings.ToLower(pattern)) {
			return true
		}
	}
	return false
}

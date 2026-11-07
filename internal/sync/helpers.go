package sync

import (
	"encoding/json"
	"fmt"
	"html"
	"strings"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/hkdb/aerion/internal/folder"
	imapPkg "github.com/hkdb/aerion/internal/imap"
	"github.com/hkdb/aerion/internal/message"
)

// applyFlagsToMessage sets boolean flag fields on a Message from IMAP flags
func applyFlagsToMessage(m *message.Message, flags []imap.Flag) {
	for _, flag := range flags {
		switch flag {
		case imap.FlagSeen:
			m.IsRead = true
		case imap.FlagFlagged:
			m.IsStarred = true
		case imap.FlagAnswered:
			m.IsAnswered = true
		case imap.FlagDraft:
			m.IsDraft = true
		case imap.FlagDeleted:
			m.IsDeleted = true
		case "$Forwarded", "\\Forwarded":
			m.IsForwarded = true
		}
	}
}

// applyEnvelopeToMessage sets envelope fields on a Message from an IMAP envelope
func applyEnvelopeToMessage(m *message.Message, envelope *imap.Envelope) {
	defer sanitizeMessageHeaders(m)
	if envelope == nil {
		return
	}
	m.Subject = decodeMIMEWord(envelope.Subject)
	m.MessageID = envelope.MessageID
	if len(envelope.InReplyTo) > 0 {
		m.InReplyTo = envelope.InReplyTo[0]
	}
	m.Date = envelope.Date.UTC()

	// From
	if len(envelope.From) > 0 {
		m.FromName = decodeMIMEWord(envelope.From[0].Name)
		m.FromEmail = envelope.From[0].Addr()
	}

	// To
	if len(envelope.To) > 0 {
		m.ToList = addressListToJSON(envelope.To)
	}

	// Cc
	if len(envelope.Cc) > 0 {
		m.CcList = addressListToJSON(envelope.Cc)
	}

	// Reply-To
	if len(envelope.ReplyTo) > 0 {
		m.ReplyTo = envelope.ReplyTo[0].Addr()
	}
}

// addressListToJSON converts an address list to JSON
func addressListToJSON(addrs []imap.Address) string {
	type addr struct {
		Name  string `json:"name"`
		Email string `json:"email"`
	}

	list := make([]addr, len(addrs))
	for i, a := range addrs {
		list[i] = addr{
			Name:  sanitizeHeader(decodeMIMEWord(a.Name)),
			Email: sanitizeHeader(a.Addr()),
		}
	}

	data, _ := json.Marshal(list)
	return sanitizeAddressJSON(string(data))
}

// generateSnippet creates a preview snippet from message body.
// Optimization: single-pass scan into a strings.Builder that trims whitespace
// inline, skips quoted (">"-prefixed) lines, and stops early once maxLen
// characters are collected — avoiding the full-line strings.Split slice,
// per-line TrimSpace allocations, and a trailing strings.Join over the whole
// body. Since the result is truncated at maxLen anyway, processing stops as
// soon as enough characters are gathered instead of scanning the entire body.
func generateSnippet(body string, maxLen int) string {
	var sb strings.Builder
	sb.Grow(min(maxLen+16, len(body)))
	needSpace := false
	start := 0
	for start <= len(body) {
		// Find end of current line (up to '\n' or end of string).
		end := start
		for end < len(body) && body[end] != '\n' {
			end++
		}
		line := body[start:end]

		// Trim leading and trailing ASCII whitespace inline.
		ls := 0
		for ls < len(line) && (line[ls] == ' ' || line[ls] == '\t' || line[ls] == '\r') {
			ls++
		}
		le := len(line)
		for le > ls && (line[le-1] == ' ' || line[le-1] == '\t' || line[le-1] == '\r') {
			le--
		}
		trimmed := line[ls:le]

		if len(trimmed) > 0 && trimmed[0] != '>' { // Skip blank and quoted lines
			if needSpace {
				sb.WriteByte(' ')
			}
			sb.WriteString(trimmed)
			needSpace = true
			if sb.Len() >= maxLen {
				break // Enough for the truncated preview; no need to scan further.
			}
		}

		if end >= len(body) {
			break
		}
		start = end + 1 // Skip the '\n' and continue to the next line.
	}

	text := sb.String()
	if len(text) > maxLen {
		text = text[:maxLen] + "..."
	}
	return text
}

// stripHTMLTags removes HTML tags (and the contents of <script>, <style> and
// <noscript> elements) and decodes HTML entities so the result is safe to use
// as a plain-text message preview snippet.
func stripHTMLTags(s string) string {
	var result strings.Builder
	skipBlock := "" // "script"/"style"/"noscript": skip until its close tag

	i := 0
	for i < len(s) {
		if skipBlock != "" {
			if i+1 < len(s) && s[i] == '<' {
				if name, end := readTagName(s, i+1); strings.EqualFold(name, "/"+skipBlock) {
					skipBlock = ""
					i = end
					continue
				}
			}
			i++
			continue
		}
		if s[i] == '<' {
			name, end := readTagName(s, i+1)
			lower := strings.ToLower(name)
			if strings.HasPrefix(lower, "/") {
				// Closing tag: only ends a skipped block.
				if skipBlock != "" && strings.EqualFold(strings.TrimPrefix(lower, "/"), skipBlock) {
					skipBlock = ""
				}
			} else if lower == "script" || lower == "style" || lower == "noscript" {
				skipBlock = lower
			}
			// Void tags and normal elements are fully consumed by readTagName,
			// so nothing is written for them.
			i = end
			continue
		}
		result.WriteRune(rune(s[i]))
		i++
	}
	return html.UnescapeString(result.String())
}

// readTagName reads an HTML tag starting at s[start] (the character after '<').
// It returns the tag name (including a leading '/' for closing tags) and the
// index of the first character after the closing '>', or len(s) when the tag
// is not terminated. Quoted attribute values are respected so a '>' inside an
// attribute does not terminate the tag early.
func readTagName(s string, start int) (string, int) {
	i := start
	for i < len(s) && (s[i] == ' ' || s[i] == '\t' || s[i] == '\n' || s[i] == '\r') {
		i++
	}
	nameStart := i
	for i < len(s) {
		c := s[i]
		if c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '>' || c == '=' {
			break
		}
		i++
	}
	name := s[nameStart:i]

	inQuote := byte(0)
	for i < len(s) {
		c := s[i]
		if inQuote != 0 {
			if c == inQuote {
				inQuote = 0
			}
			i++
			continue
		}
		if c == '"' || c == '\'' {
			inQuote = c
			i++
			continue
		}
		if c == '>' {
			return name, i + 1
		}
		i++
	}
	return name, len(s)
}

// isVoidTag reports whether name is an HTML void (self-closing) element that
// never wraps text content.
func isVoidTag(name string) bool {
	switch name {
	case "area", "base", "br", "col", "embed", "hr", "img", "input", "link", "meta", "param", "source", "track", "wbr":
		return true
	}
	return false
}

// convertFolderType converts IMAP folder type to our folder type
func convertFolderType(t imapPkg.FolderType) folder.Type {
	switch t {
	case imapPkg.FolderTypeInbox:
		return folder.TypeInbox
	case imapPkg.FolderTypeSent:
		return folder.TypeSent
	case imapPkg.FolderTypeDrafts:
		return folder.TypeDrafts
	case imapPkg.FolderTypeTrash:
		return folder.TypeTrash
	case imapPkg.FolderTypeSpam:
		return folder.TypeSpam
	case imapPkg.FolderTypeArchive:
		return folder.TypeArchive
	case imapPkg.FolderTypeAll:
		return folder.TypeAll
	case imapPkg.FolderTypeStarred:
		return folder.TypeStarred
	default:
		return folder.TypeFolder
	}
}

// extractFolderName extracts the folder name from the full path
func extractFolderName(path, delimiter string) string {
	if delimiter == "" {
		return path
	}
	parts := strings.Split(path, delimiter)
	return parts[len(parts)-1]
}

// generateID generates a unique ID for attachments
func generateID() string {
	return fmt.Sprintf("%d", time.Now().UnixNano())
}

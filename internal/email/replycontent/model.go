// Package replycontent defines what a reply extractor returns: a message body
// split into the text its sender wrote, the history they quoted and their
// signature. UX-004 of the D-002 plan holds only the schema, its validation and
// a fixture corpus; the parsers (UX-005 plain text, UX-006 HTML) come later and
// run against the same fixtures.
//
// The rule that shapes everything here: uncertainty never hides text. A block
// the extractor cannot classify is BlockUnknown, and unknown blocks are always
// shown. When nothing can be classified the result is one unknown block with
// the whole body.
package replycontent

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// ExtractorVersion identifies the extraction rules. Stored results with another
// version are recomputed rather than trusted.
const ExtractorVersion = "reply-v4"

// BlockKind says what a block of a message body is.
type BlockKind string

const (
	// BlockAuthored is new text the sender wrote in this message.
	BlockAuthored BlockKind = "authored"
	// BlockQuoted is earlier conversation the sender included.
	BlockQuoted BlockKind = "quoted"
	// BlockSignature is a closing signature or a mobile-client footer.
	BlockSignature BlockKind = "signature"
	// BlockUnknown is text the extractor could not classify. It is never folded away.
	BlockUnknown BlockKind = "unknown"
)

// Status summarizes how far extraction got.
type Status string

const (
	// StatusExtracted means every block is classified.
	StatusExtracted Status = "extracted"
	// StatusPartial means some blocks are classified and some are unknown.
	StatusPartial Status = "partial"
	// StatusFallback means nothing was classified: one unknown block holds the body.
	StatusFallback Status = "fallback"
	// StatusUnavailable means there is no body to extract from (for example, not fetched).
	StatusUnavailable Status = "unavailable"
)

// Part names the stored body part a block or its source range belongs to.
type Part string

const (
	PartText Part = "text"
	PartHTML Part = "html"
)

// Warnings explain why an extractor was unsure. They are for diagnostics and
// tests, never for control flow in the reader.
const (
	WarnQuoteOnly            = "quote-only"
	WarnAmbiguousQuoteMarker = "ambiguous-quote-marker"
	WarnForwardedContent     = "forwarded-content"
	WarnMalformedHTML        = "malformed-html"
	WarnInputTooLarge        = "input-too-large"
)

var knownWarnings = map[string]bool{
	WarnQuoteOnly:            true,
	WarnAmbiguousQuoteMarker: true,
	WarnForwardedContent:     true,
	WarnMalformedHTML:        true,
	WarnInputTooLarge:        true,
}

// IsKnownWarning reports whether w is a warning this version can produce.
func IsKnownWarning(w string) bool { return knownWarnings[w] }

// Source locates a block in the stored body part, so a client can show the
// original text again. Offsets are bytes into the stored part; End is exclusive.
type Source struct {
	Part  Part `json:"part"`
	Start int  `json:"start"`
	End   int  `json:"end"`
}

// Block is one contiguous piece of a message body.
type Block struct {
	// ID is unique within a result and stable for one body and extractor version.
	ID   string    `json:"id"`
	Kind BlockKind `json:"kind"`
	// Format is how Content is written: plain text, or sanitized HTML.
	Format  Part   `json:"format"`
	Content string `json:"content"`
	// Source is optional; the extractor sets it when it can point at the original range.
	Source *Source `json:"source,omitempty"`
}

// Collapsible reports whether a reader may fold the block away by default.
// Authored and unknown blocks are always visible.
func (b Block) Collapsible() bool {
	return b.Kind == BlockQuoted || b.Kind == BlockSignature
}

// Result is the outcome of extracting one message body.
type Result struct {
	Version  string   `json:"version"`
	Status   Status   `json:"status"`
	Blocks   []Block  `json:"blocks"`
	Warnings []string `json:"warnings,omitempty"`
}

func validPart(p Part) bool { return p == PartText || p == PartHTML }

// Validate checks the structural rules every extractor result must satisfy.
func (r Result) Validate() error {
	if strings.TrimSpace(r.Version) == "" {
		return fmt.Errorf("replycontent: version is required")
	}
	switch r.Status {
	case StatusExtracted, StatusPartial, StatusFallback, StatusUnavailable:
	default:
		return fmt.Errorf("replycontent: unknown status %q", r.Status)
	}

	seen := make(map[string]bool, len(r.Blocks))
	var unknown, classified int
	for i, b := range r.Blocks {
		if b.ID == "" {
			return fmt.Errorf("replycontent: block %d has an empty id", i)
		}
		if seen[b.ID] {
			return fmt.Errorf("replycontent: duplicate block id %q", b.ID)
		}
		seen[b.ID] = true
		switch b.Kind {
		case BlockUnknown:
			unknown++
		case BlockAuthored, BlockQuoted, BlockSignature:
			classified++
		default:
			return fmt.Errorf("replycontent: block %q has unknown kind %q", b.ID, b.Kind)
		}
		if !validPart(b.Format) {
			return fmt.Errorf("replycontent: block %q has invalid format %q", b.ID, b.Format)
		}
		if strings.TrimSpace(b.Content) == "" {
			return fmt.Errorf("replycontent: block %q has blank content", b.ID)
		}
		if s := b.Source; s != nil {
			if !validPart(s.Part) || s.Start < 0 || s.End < s.Start {
				return fmt.Errorf("replycontent: block %q has an invalid source %+v", b.ID, *s)
			}
		}
	}

	switch r.Status {
	case StatusExtracted:
		if len(r.Blocks) == 0 {
			return fmt.Errorf("replycontent: extracted result has no blocks")
		}
		if unknown > 0 {
			return fmt.Errorf("replycontent: extracted result has %d unknown block(s); use partial", unknown)
		}
	case StatusPartial:
		if unknown == 0 {
			return fmt.Errorf("replycontent: partial result has no unknown block; use extracted")
		}
		if classified == 0 {
			return fmt.Errorf("replycontent: partial result has no classified block; use fallback")
		}
	case StatusFallback:
		if len(r.Blocks) != 1 || unknown != 1 {
			return fmt.Errorf("replycontent: fallback result must be exactly one unknown block")
		}
	case StatusUnavailable:
		if len(r.Blocks) != 0 {
			return fmt.Errorf("replycontent: unavailable result must have no blocks")
		}
	}

	for _, w := range r.Warnings {
		if !IsKnownWarning(w) {
			return fmt.Errorf("replycontent: unknown warning %q", w)
		}
	}
	return nil
}

// NormalizeText collapses every run of whitespace, line breaks and
// non-breaking spaces included, into one space and trims the ends. Fixtures
// express expected text this way so they do not depend on serialization.
//
// Performance optimization: single pass into a strings.Builder that collapses
// whitespace runs inline instead of strings.Fields (which allocates a []string
// of field headers) followed by strings.Join. ASCII whitespace is detected with
// a byte comparison; only non-ASCII runes (e.g. U+00A0 non-breaking space) are
// decoded and tested with unicode.IsSpace, preserving strings.Fields parity.
func NormalizeText(s string) string {
	n := len(s)
	var sb strings.Builder
	sb.Grow(n)
	i := 0
	wrote := false
	for i < n {
		c := s[i]
		var r rune
		var size int
		var space bool
		if c < 0x80 {
			size = 1
			space = c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f' || c == '\v'
		} else {
			r, size = utf8.DecodeRuneInString(s[i:])
			if r == utf8.RuneError {
				r = rune(s[i])
				size = 1
			}
			space = unicode.IsSpace(r)
		}
		if space {
			// Consume the entire whitespace run (ASCII and non-ASCII mixed) and
			// emit exactly one space before the next word, never at the edges.
			i += size
			for i < n {
				c = s[i]
				if c < 0x80 {
					if c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f' || c == '\v' {
						i++
						continue
					}
					break
				}
				r, size = utf8.DecodeRuneInString(s[i:])
				if r == utf8.RuneError {
					size = 1
				}
				if !unicode.IsSpace(r) {
					break
				}
				i += size
			}
			if wrote && i < n {
				sb.WriteByte(' ')
			}
			continue
		}
		// Copy the run of non-whitespace runes in one block.
		start := i
		i += size
		for i < n {
			c = s[i]
			if c < 0x80 {
				if c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f' || c == '\v' {
					break
				}
				i++
				continue
			}
			r, size = utf8.DecodeRuneInString(s[i:])
			if r == utf8.RuneError {
				size = 1
			}
			if unicode.IsSpace(r) {
				break
			}
			i += size
		}
		sb.WriteString(s[start:i])
		wrote = true
	}
	return sb.String()
}

// Package email provides email content processing utilities
package email

import (
	"regexp"
	"strings"

	"github.com/microcosm-cc/bluemonday"
)

// Pre-compiled regular expressions for HTML sanitization and extraction.
// Pre-compiling these regular expressions at package initialization avoids
// recompiling regex patterns repeatedly on every email rendering, sanitization,
// and plain-text extraction call, significantly reducing CPU cycles and memory allocations.
var (
	scriptTagRe = regexp.MustCompile(`(?is)<script[^>]*>.*?</script>`)
	styleTagRe  = regexp.MustCompile(`(?is)<style[^>]*>.*?</style>`)
	imgRemoteRe = regexp.MustCompile(`(?i)<img([^>]*)\ssrc=["'](https?://[^"']+)["']([^>]*)>`)
	imgSrcRe    = regexp.MustCompile(`(?i)src=["'](https?://[^"']+)["']`)
	cssURLRe    = regexp.MustCompile(`(?i)url\(\s*(?:['"]|&#(?:39|x27|34|x22);|&(?:apos|quot);)?\s*https?://[^)]*?(?:['"]|&#(?:39|x27|34|x22);|&(?:apos|quot);)?\s*\)`)
	bgAttrRe    = regexp.MustCompile(`(?i)\bbackground\s*=\s*["'](https?://[^"']+)["']`)
	blockTagsRe = regexp.MustCompile(`(?i)</(p|div|br|h[1-6]|li|tr)>`)
	brTagsRe    = regexp.MustCompile(`(?i)<br[^>]*>`)
	tagRe       = regexp.MustCompile(`<[^>]*>`)
	wsRe        = regexp.MustCompile(`[ \t]+`)
	nlRe        = regexp.MustCompile(`\n{3,}`)

	// htmlAttrEscaper replaces HTML special characters for attributes in a single pass,
	// avoiding multiple intermediate string allocations.
	htmlAttrEscaper = strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		`"`, "&quot;",
		"'", "&#39;",
	)

	// htmlEntityReplacer decodes common HTML entities in a single pass instead of
	// multiple sequential strings.ReplaceAll scans across the entire text buffer.
	htmlEntityReplacer = strings.NewReplacer(
		"&nbsp;", " ",
		"&amp;", "&",
		"&lt;", "<",
		"&gt;", ">",
		"&quot;", `"`,
		"&#39;", "'",
	)
)

// Sanitizer provides HTML email sanitization
type Sanitizer struct {
	policy      *bluemonday.Policy
	eventPolicy *bluemonday.Policy
}

// NewSanitizer creates a new HTML sanitizer configured for email content
func NewSanitizer() *Sanitizer {
	// Create a custom policy for email HTML
	p := bluemonday.NewPolicy()

	// ==========================================================================
	// Document structure
	// ==========================================================================
	p.AllowElements("html", "head", "body", "title", "meta", "link")

	// ==========================================================================
	// Semantic/structural elements
	// ==========================================================================
	p.AllowElements("div", "span", "p", "br", "hr", "wbr")
	p.AllowElements("h1", "h2", "h3", "h4", "h5", "h6")
	p.AllowElements("article", "section", "header", "footer", "main", "aside", "nav")
	p.AllowElements("figure", "figcaption")
	p.AllowElements("details", "summary")
	p.AllowElements("address")

	// ==========================================================================
	// Deprecated but commonly used in HTML emails
	// ==========================================================================
	p.AllowElements("center") // Used for centering content (common in email templates)
	p.AllowElements("font")   // Legacy font styling
	p.AllowElements("basefont")
	p.AllowElements("big")
	p.AllowElements("tt") // Teletype/monospace

	// ==========================================================================
	// Text formatting
	// ==========================================================================
	p.AllowElements("b", "i", "u", "s", "strong", "em", "mark", "small", "sub", "sup")
	p.AllowElements("del", "ins", "strike")
	p.AllowElements("abbr", "acronym")
	p.AllowElements("cite", "dfn", "q")
	p.AllowElements("kbd", "samp", "var")
	p.AllowElements("bdi", "bdo")
	p.AllowElements("ruby", "rt", "rp") // Ruby annotations (for East Asian text)
	p.AllowElements("time")
	p.AllowElements("data")

	// ==========================================================================
	// Lists
	// ==========================================================================
	p.AllowElements("ul", "ol", "li", "dl", "dt", "dd")
	p.AllowElements("menu")

	// ==========================================================================
	// Tables (heavily used in HTML emails for layout)
	// ==========================================================================
	p.AllowElements("table", "thead", "tbody", "tfoot", "tr", "th", "td")
	p.AllowElements("caption", "colgroup", "col")

	// ==========================================================================
	// Links
	// ==========================================================================
	p.AllowAttrs("href", "title", "name").OnElements("a")
	p.AllowElements("a")
	p.AllowRelativeURLs(false)
	p.RequireNoFollowOnLinks(true)
	p.RequireNoReferrerOnLinks(true)
	p.AddTargetBlankToFullyQualifiedLinks(true)

	// ==========================================================================
	// Images and media
	// ==========================================================================
	p.AllowAttrs("src", "alt", "title", "width", "height", "loading").OnElements("img")
	p.AllowElements("img")
	p.AllowElements("picture", "source")
	p.AllowElements("map", "area")

	// Allow cid: scheme for inline attachments, data: for base64, http/https for remote
	p.AllowURLSchemes("cid", "data", "http", "https", "mailto")

	// ==========================================================================
	// Blockquotes and code (common in email replies)
	// ==========================================================================
	p.AllowElements("blockquote", "pre", "code")

	// ==========================================================================
	// Style elements (safe since emails render in sandboxed iframe)
	// ==========================================================================
	p.AllowElements("style")

	// ==========================================================================
	// Global attributes
	// ==========================================================================
	p.AllowAttrs("style").Globally()
	p.AllowAttrs("class", "id").Globally()
	p.AllowAttrs("title").Globally()
	p.AllowAttrs("lang", "dir").Globally()
	p.AllowAttrs("role", "aria-label", "aria-hidden", "aria-describedby").Globally()

	// ==========================================================================
	// Table-specific attributes (crucial for email layouts)
	// ==========================================================================
	p.AllowAttrs("align", "valign", "width", "height", "border", "cellpadding", "cellspacing").OnElements("table", "td", "th", "tr", "tbody", "thead", "tfoot")
	p.AllowAttrs("colspan", "rowspan", "scope", "headers").OnElements("td", "th")
	p.AllowAttrs("bgcolor", "background").OnElements("table", "td", "th", "tr", "tbody", "thead", "tfoot", "body", "center")

	// ==========================================================================
	// Legacy formatting attributes
	// ==========================================================================
	p.AllowAttrs("color", "size", "face").OnElements("font", "basefont")
	p.AllowAttrs("align").OnElements("div", "p", "h1", "h2", "h3", "h4", "h5", "h6", "center", "img", "table", "td", "th", "tr")
	p.AllowAttrs("noshade", "size", "width").OnElements("hr")

	// ==========================================================================
	// Data attributes (used by email templates for lazy-loading, CSS selectors, etc.)
	// ==========================================================================
	p.AllowDataAttributes()

	// Event descriptions render directly in the application document, unlike
	// email bodies which render in a sandboxed iframe. UGCPolicy keeps useful
	// semantic markup while refusing style, class, and id attributes.
	eventPolicy := bluemonday.UGCPolicy()
	eventPolicy.AllowURLSchemes("http", "https", "mailto")
	eventPolicy.RequireNoFollowOnLinks(true)
	eventPolicy.RequireNoReferrerOnLinks(true)
	eventPolicy.AddTargetBlankToFullyQualifiedLinks(true)

	return &Sanitizer{policy: p, eventPolicy: eventPolicy}
}

// Sanitize cleans HTML content for safe display
func (s *Sanitizer) Sanitize(html string) string {
	// Remove script tags for security
	html = removeScriptTags(html)
	// Note: We keep <style> blocks since emails are rendered in a sandboxed iframe
	// and many HTML emails rely on CSS rules for proper layout

	// Apply bluemonday sanitization
	return s.policy.Sanitize(html)
}

// SanitizeWithRemoteImageBlocking sanitizes HTML and replaces remote images with placeholders
func (s *Sanitizer) SanitizeWithRemoteImageBlocking(html string) string {
	// First sanitize
	sanitized := s.Sanitize(html)

	// Then replace remote images with placeholders
	return BlockRemoteImages(sanitized)
}

// SanitizeForEvent cleans rich event descriptions for insertion into the
// application document. It intentionally does not inherit email's CSS policy.
func (s *Sanitizer) SanitizeForEvent(html string) string {
	if s == nil || s.eventPolicy == nil {
		return ""
	}
	return BlockRemoteImages(s.eventPolicy.Sanitize(removeScriptTags(html)))
}

// removeScriptTags removes all script tags and their content
func removeScriptTags(html string) string {
	return scriptTagRe.ReplaceAllString(html, "")
}

// removeStyleTags removes all style tags and their content
// Note: This removes <style> blocks but preserves inline style attributes
func removeStyleTags(html string) string {
	return styleTagRe.ReplaceAllString(html, "")
}

// indexFoldASCII finds the first occurrence of substr (which must be lowercase ASCII)
// in s at or after fromIndex, ignoring ASCII case, without allocating temporary strings.
func indexFoldASCII(s, substr string, fromIndex int) int {
	if len(substr) == 0 {
		return fromIndex
	}
	if fromIndex < 0 {
		fromIndex = 0
	}
	limit := len(s) - len(substr)
	if fromIndex > limit {
		return -1
	}
	sub0 := substr[0]
	for i := fromIndex; i <= limit; i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		if c == sub0 {
			match := true
			for j := 1; j < len(substr); j++ {
				cj := s[i+j]
				if cj >= 'A' && cj <= 'Z' {
					cj += 'a' - 'A'
				}
				if cj != substr[j] {
					match = false
					break
				}
			}
			if match {
				return i
			}
		}
	}
	return -1
}

// containsFoldASCII checks if s contains substr (which must be lowercase ASCII)
// ignoring ASCII case, without allocating temporary strings.
func containsFoldASCII(s, substr string) bool {
	return indexFoldASCII(s, substr, 0) != -1
}

// hasBackgroundAttr checks if s contains a background attribute assignment (background\s*=)
// without allocating strings, avoiding expensive regex scans on CSS "background:" or plain text.
func hasBackgroundAttr(s string) bool {
	idx := 0
	for {
		pos := indexFoldASCII(s, "background", idx)
		if pos == -1 {
			return false
		}
		offset := pos + len("background")
		for offset < len(s) && (s[offset] == ' ' || s[offset] == '\t' || s[offset] == '\r' || s[offset] == '\n') {
			offset++
		}
		if offset < len(s) && s[offset] == '=' {
			return true
		}
		idx = offset
	}
}

const imagePlaceholderPrefix = `src="data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='100' height='100'%3E%3Crect fill='%23ddd' width='100' height='100'/%3E%3Ctext x='50%25' y='50%25' text-anchor='middle' dy='.3em' fill='%23666' font-size='12'%3EImage blocked%3C/text%3E%3C/svg%3E" data-original-src="`

// BlockRemoteImages replaces remote image sources with a placeholder SVG,
// storing the original URL in a data-original-src attribute for later restoration.
func BlockRemoteImages(html string) string {
	// Performance optimization: Remote images only match http:// or https:// schemes.
	// If neither scheme is present in the HTML, return immediately with 0 allocations.
	if !containsFoldASCII(html, "http://") && !containsFoldASCII(html, "https://") {
		return html
	}

	// Performance optimization: Pre-check for presence of relevant tokens before running
	// expensive regex passes. If none are present, return the original string immediately
	// with 0 allocations.
	hasImg := containsFoldASCII(html, "<img")
	hasURL := containsFoldASCII(html, "url(")
	hasBg := hasBackgroundAttr(html)
	if !hasImg && !hasURL && !hasBg {
		return html
	}

	result := html

	// Match img tags with http/https sources
	if hasImg {
		result = imgRemoteRe.ReplaceAllStringFunc(result, func(match string) string {
			// Extract src indices and replace directly, avoiding a second regex pass
			loc := imgSrcRe.FindStringSubmatchIndex(match)
			if len(loc) < 4 {
				return match
			}

			originalSrc := match[loc[2]:loc[3]]

			// Replace with placeholder using direct slice concatenation instead of regex ReplaceAllString
			return match[:loc[0]] + imagePlaceholderPrefix + escapeHTML(originalSrc) + `"` + match[loc[1]:]
		})
	}

	// Block remote URLs in CSS url() references (background-image, background, etc.)
	// Handles all quote encodings: raw, decimal (&#39;/&#34;), hex (&#x27;/&#x22;), named (&apos;/&quot;)
	if hasURL {
		result = cssURLRe.ReplaceAllString(result, `url()`)
	}

	// Block HTML background attribute with remote URLs
	if hasBg {
		result = bgAttrRe.ReplaceAllString(result, `background=""`)
	}

	return result
}

// escapeHTML escapes HTML special characters for use in attributes.
// Uses a pre-allocated strings.Replacer to escape all 5 special characters in a single pass.
func escapeHTML(s string) string {
	return htmlAttrEscaper.Replace(s)
}

// ExtractPlainTextFromHTML converts HTML to plain text
func ExtractPlainTextFromHTML(html string) string {
	// Remove style and script content
	html = removeScriptTags(html)
	html = removeStyleTags(html)

	// Convert common block elements to newlines
	html = blockTagsRe.ReplaceAllString(html, "\n")

	// Handle <br> tags
	html = brTagsRe.ReplaceAllString(html, "\n")

	// Remove all remaining HTML tags
	text := tagRe.ReplaceAllString(html, "")

	// Decode common HTML entities in a single pass via pre-configured replacer
	text = htmlEntityReplacer.Replace(text)

	// Collapse multiple whitespace
	text = wsRe.ReplaceAllString(text, " ")

	// Collapse multiple newlines
	text = nlRe.ReplaceAllString(text, "\n\n")

	return strings.TrimSpace(text)
}

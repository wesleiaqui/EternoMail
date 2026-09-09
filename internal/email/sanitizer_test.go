package email

import (
	"strings"
	"testing"
)

func TestSanitize_RemovesScripts(t *testing.T) {
	s := NewSanitizer()
	input := `<p>Hello</p><script>alert('xss')</script><p>World</p>`
	result := s.Sanitize(input)

	if strings.Contains(result, "<script") {
		t.Fatalf("expected script tag removed, got: %s", result)
	}
	if strings.Contains(result, "alert") {
		t.Fatalf("expected script content removed, got: %s", result)
	}
}

func TestSanitize_PreservesBasicHTML(t *testing.T) {
	s := NewSanitizer()
	input := `<p><b>bold</b></p>`
	result := s.Sanitize(input)

	if !strings.Contains(result, "<p>") {
		t.Fatalf("expected <p> preserved, got: %s", result)
	}
	if !strings.Contains(result, "<b>") {
		t.Fatalf("expected <b> preserved, got: %s", result)
	}
	if !strings.Contains(result, "bold") {
		t.Fatalf("expected text preserved, got: %s", result)
	}
}

func TestSanitize_StripsStyleTags(t *testing.T) {
	s := NewSanitizer()
	input := `<style>.foo{color:red}</style><p>text</p>`
	result := s.Sanitize(input)

	if strings.Contains(result, "<style>") {
		t.Fatalf("expected <style> stripped, got: %s", result)
	}
	if !strings.Contains(result, "<p>text</p>") {
		t.Fatalf("expected <p> content preserved, got: %s", result)
	}
}

func TestSanitizeWithRemoteImageBlocking_BlocksHTTP(t *testing.T) {
	s := NewSanitizer()
	input := `<img src="https://example.com/img.png">`
	result := s.SanitizeWithRemoteImageBlocking(input)

	// The src attribute should now be the SVG placeholder, not the original URL
	if !strings.Contains(result, `src="data:image/svg+xml`) {
		t.Fatalf("expected SVG placeholder in src, got: %s", result)
	}
	if !strings.Contains(result, `data-original-src="https://example.com/img.png"`) {
		t.Fatalf("expected original URL in data-original-src, got: %s", result)
	}
}

func TestSanitizeWithRemoteImageBlocking_AllowsCID(t *testing.T) {
	s := NewSanitizer()
	input := `<img src="cid:abc123">`
	result := s.SanitizeWithRemoteImageBlocking(input)

	if !strings.Contains(result, `src="cid:abc123"`) {
		t.Fatalf("expected cid: src preserved, got: %s", result)
	}
}

func TestSanitizeWithRemoteImageBlocking_AllowsData(t *testing.T) {
	s := NewSanitizer()
	input := `<img src="data:image/png;base64,abc">`
	result := s.SanitizeWithRemoteImageBlocking(input)

	if !strings.Contains(result, `src="data:image/png;base64,abc"`) {
		t.Fatalf("expected data: src preserved, got: %s", result)
	}
}

func TestSanitizeForEvent_RemovesCSSAndPreservesFormatting(t *testing.T) {
	s := NewSanitizer()
	input := `<style>body { visibility: hidden }</style><p style="position:fixed;inset:0;z-index:9999;opacity:0;pointer-events:auto;transform:scale(2);width:100vw;height:100vh"><strong>bold</strong><em>italic</em></p><ul><li>item</li></ul>`
	result := s.SanitizeForEvent(input)

	for _, forbidden := range []string{"<style", "style=", "position:", "z-index", "100vw", "100vh", "pointer-events", "transform:"} {
		if strings.Contains(result, forbidden) {
			t.Fatalf("event sanitizer retained %q: %s", forbidden, result)
		}
	}
	for _, required := range []string{"<p>", "<strong>bold</strong>", "<em>italic</em>", "<ul>", "<li>item</li>"} {
		if !strings.Contains(result, required) {
			t.Fatalf("event sanitizer removed %q: %s", required, result)
		}
	}
}

func TestSanitizeForEvent_PreservesSafeLinksAndRemovesActiveContent(t *testing.T) {
	s := NewSanitizer()
	result := s.SanitizeForEvent(`<a href="https://example.com" onclick="evil()">link</a><script>alert(1)</script>`)

	if !strings.Contains(result, `href="https://example.com"`) || !strings.Contains(result, "nofollow") || !strings.Contains(result, "noreferrer") {
		t.Fatalf("event sanitizer did not preserve protected link: %s", result)
	}
	for _, forbidden := range []string{"<script", "alert", "onclick"} {
		if strings.Contains(result, forbidden) {
			t.Fatalf("event sanitizer retained %q: %s", forbidden, result)
		}
	}
}

func TestExtractPlainTextFromHTML(t *testing.T) {
	input := `<p>Hello</p><p>World</p>`
	result := ExtractPlainTextFromHTML(input)

	if !strings.Contains(result, "Hello") {
		t.Fatalf("expected 'Hello' in result, got: %s", result)
	}
	if !strings.Contains(result, "World") {
		t.Fatalf("expected 'World' in result, got: %s", result)
	}
}

func TestExtractPlainTextFromHTML_Entities(t *testing.T) {
	input := `<p>&amp; &lt;</p>`
	result := ExtractPlainTextFromHTML(input)

	if !strings.Contains(result, "& <") {
		t.Fatalf("expected '& <' in result, got: %s", result)
	}
}

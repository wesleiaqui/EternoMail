package sync

import (
	"strings"
	"testing"
)

func BenchmarkGenerateSnippet(b *testing.B) {
	// Realistic email body: many short lines, a few quoted lines, far more
	// content than maxLen so the early-stop path is exercised.
	var lines []string
	for i := 0; i < 120; i++ {
		lines = append(lines, "This is a normal line of body content in the email.")
		lines = append(lines, "> This is a quoted reply line that should be skipped.")
	}
	body := strings.Join(lines, "\n") + "\n" + strings.Repeat("trailing filler content ", 80)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = generateSnippet(body, 200)
	}
}

func TestGenerateSnippet(t *testing.T) {
	longText := "This is a long text that should be truncated when it exceeds the maximum length specified by the caller of the generateSnippet function."
	maxLen := 30

	result := generateSnippet(longText, maxLen)

	// The result should be truncated to maxLen + "..."
	if len(result) > maxLen+3 {
		t.Errorf("snippet length = %d, want at most %d (maxLen + '...')", len(result), maxLen+3)
	}
	if !strings.HasSuffix(result, "...") {
		t.Errorf("expected truncated snippet to end with '...', got %q", result)
	}
}

func TestGenerateSnippet_Empty(t *testing.T) {
	result := generateSnippet("", 100)

	if result != "" {
		t.Errorf("generateSnippet('', 100) = %q, want empty string", result)
	}
}

func TestGenerateSnippet_QuotedLines(t *testing.T) {
	text := "Hello there\n> This is a quoted line\n> Another quoted line\nActual content here"
	result := generateSnippet(text, 200)

	if strings.Contains(result, "quoted line") {
		t.Errorf("snippet should skip quoted lines, got %q", result)
	}
	if !strings.Contains(result, "Hello there") {
		t.Errorf("snippet should contain non-quoted content 'Hello there', got %q", result)
	}
	if !strings.Contains(result, "Actual content here") {
		t.Errorf("snippet should contain non-quoted content 'Actual content here', got %q", result)
	}
}

func TestStripHTMLTags(t *testing.T) {
	result := stripHTMLTags("<b>bold</b> text")

	if result != "bold text" {
		t.Errorf("stripHTMLTags('<b>bold</b> text') = %q, want 'bold text'", result)
	}
}

func TestStripHTMLTags_Empty(t *testing.T) {
	result := stripHTMLTags("")

	if result != "" {
		t.Errorf("stripHTMLTags('') = %q, want empty string", result)
	}
}

func TestStripHTMLTags_RemovesScriptContent(t *testing.T) {
	result := stripHTMLTags("<script>()=>{if(le())return le();const Y=Gt.accounts.find(U=>l</script>Real text")

	if strings.Contains(result, "if(le())") {
		t.Errorf("snippet should not leak <script> JavaScript, got %q", result)
	}
	if !strings.Contains(result, "Real text") {
		t.Errorf("snippet should keep real text, got %q", result)
	}
}

func TestStripHTMLTags_RemovesStyleContent(t *testing.T) {
	result := stripHTMLTags("<style>body{color:red;}</style>Real text")

	if strings.Contains(result, "color:red") {
		t.Errorf("snippet should not leak <style> CSS, got %q", result)
	}
	if !strings.Contains(result, "Real text") {
		t.Errorf("snippet should keep real text, got %q", result)
	}
}

func TestStripHTMLTags_DecodesHTMLEntities(t *testing.T) {
	result := stripHTMLTags("&quot;LEGO Boost roboottisarja&quot; &amp; &lt;test&gt;")

	if result != `"LEGO Boost roboottisarja" & <test>` {
		t.Errorf("stripHTMLTags entities = %q, want %q", result, `"LEGO Boost roboottisarja" & <test>`)
	}
}

func TestStripHTMLTags_RemovesComments(t *testing.T) {
	result := stripHTMLTags("<!-- conditional comment -->Real text")

	if strings.Contains(result, "comment") {
		t.Errorf("snippet should not leak HTML comments, got %q", result)
	}
	if !strings.Contains(result, "Real text") {
		t.Errorf("snippet should keep real text, got %q", result)
	}
}

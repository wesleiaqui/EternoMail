package replycontent

import (
	"strings"
	"testing"
)

func block(id string, kind BlockKind) Block {
	return Block{ID: id, Kind: kind, Format: PartText, Content: "text " + id}
}

func TestResultValidate(t *testing.T) {
	tests := []struct {
		name    string
		result  Result
		wantErr string
	}{
		{"extracted", Result{Version: ExtractorVersion, Status: StatusExtracted,
			Blocks: []Block{block("b1", BlockAuthored), block("b2", BlockQuoted), block("b3", BlockSignature)}}, ""},
		{"partial has unknown and classified blocks", Result{Version: ExtractorVersion, Status: StatusPartial,
			Blocks: []Block{block("b1", BlockAuthored), block("b2", BlockUnknown)}, Warnings: []string{WarnAmbiguousQuoteMarker}}, ""},
		{"fallback is one unknown block", Result{Version: ExtractorVersion, Status: StatusFallback,
			Blocks: []Block{block("b1", BlockUnknown)}, Warnings: []string{WarnQuoteOnly}}, ""},
		{"unavailable has no blocks", Result{Version: ExtractorVersion, Status: StatusUnavailable}, ""},

		{"missing version", Result{Status: StatusUnavailable}, "version"},
		{"unknown status", Result{Version: ExtractorVersion, Status: "done"}, "status"},
		{"unknown block kind", Result{Version: ExtractorVersion, Status: StatusExtracted,
			Blocks: []Block{block("b1", "heading")}}, "kind"},
		{"duplicate ids", Result{Version: ExtractorVersion, Status: StatusExtracted,
			Blocks: []Block{block("b1", BlockAuthored), block("b1", BlockQuoted)}}, "duplicate"},
		{"empty id", Result{Version: ExtractorVersion, Status: StatusExtracted,
			Blocks: []Block{block("", BlockAuthored)}}, "id"},
		{"blank content", Result{Version: ExtractorVersion, Status: StatusExtracted,
			Blocks: []Block{{ID: "b1", Kind: BlockAuthored, Format: PartText, Content: "  \n "}}}, "content"},
		{"bad format", Result{Version: ExtractorVersion, Status: StatusExtracted,
			Blocks: []Block{{ID: "b1", Kind: BlockAuthored, Format: "pdf", Content: "x"}}}, "format"},
		{"extracted with no blocks", Result{Version: ExtractorVersion, Status: StatusExtracted}, "block"},
		{"extracted must not leave unknown blocks", Result{Version: ExtractorVersion, Status: StatusExtracted,
			Blocks: []Block{block("b1", BlockAuthored), block("b2", BlockUnknown)}}, "unknown"},
		{"partial without an unknown block", Result{Version: ExtractorVersion, Status: StatusPartial,
			Blocks: []Block{block("b1", BlockAuthored), block("b2", BlockQuoted)}}, "unknown"},
		{"partial with only unknown blocks", Result{Version: ExtractorVersion, Status: StatusPartial,
			Blocks: []Block{block("b1", BlockUnknown)}}, "classified"},
		{"fallback with two blocks", Result{Version: ExtractorVersion, Status: StatusFallback,
			Blocks: []Block{block("b1", BlockUnknown), block("b2", BlockUnknown)}}, "fallback"},
		{"fallback with a classified block", Result{Version: ExtractorVersion, Status: StatusFallback,
			Blocks: []Block{block("b1", BlockAuthored)}}, "fallback"},
		{"unavailable with blocks", Result{Version: ExtractorVersion, Status: StatusUnavailable,
			Blocks: []Block{block("b1", BlockAuthored)}}, "unavailable"},
		{"unknown warning", Result{Version: ExtractorVersion, Status: StatusUnavailable, Warnings: []string{"odd"}}, "warning"},
		{"source with a negative start", Result{Version: ExtractorVersion, Status: StatusExtracted,
			Blocks: []Block{{ID: "b1", Kind: BlockAuthored, Format: PartText, Content: "x", Source: &Source{Part: PartText, Start: -1, End: 1}}}}, "source"},
		{"source that ends before it starts", Result{Version: ExtractorVersion, Status: StatusExtracted,
			Blocks: []Block{{ID: "b1", Kind: BlockAuthored, Format: PartText, Content: "x", Source: &Source{Part: PartText, Start: 5, End: 2}}}}, "source"},
		{"source with an unknown part", Result{Version: ExtractorVersion, Status: StatusExtracted,
			Blocks: []Block{{ID: "b1", Kind: BlockAuthored, Format: PartText, Content: "x", Source: &Source{Part: "pdf", Start: 0, End: 1}}}}, "source"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.result.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate() = %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Validate() = %v, want an error mentioning %q", err, tt.wantErr)
			}
		})
	}
}

// TestCollapsible pins which blocks a reader may fold away. Unknown blocks are
// never folded, so uncertainty can never hide text.
func TestCollapsible(t *testing.T) {
	tests := []struct {
		kind BlockKind
		want bool
	}{
		{BlockAuthored, false},
		{BlockUnknown, false},
		{BlockQuoted, true},
		{BlockSignature, true},
	}
	for _, tt := range tests {
		if got := (Block{Kind: tt.kind}).Collapsible(); got != tt.want {
			t.Errorf("%s: Collapsible() = %v, want %v", tt.kind, got, tt.want)
		}
	}
}

func TestNormalizeText(t *testing.T) {
	tests := []struct{ in, want string }{
		{"", ""},
		{"  hello \n\n world\t!  ", "hello world !"},
		{"On Mon,\r\nAlice wrote:\r\n> hi", "On Mon, Alice wrote: > hi"},
		{"Lähettäjä: Alice", "Lähettäjä: Alice"},
	}
	for _, tt := range tests {
		if got := NormalizeText(tt.in); got != tt.want {
			t.Errorf("NormalizeText(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// BenchmarkNormalizeText measures the whitespace-collapsing normalization used
// on extracted HTML/plain body text during reply-content extraction. It runs on
// a realistic multi-KB message body containing mixed whitespace (spaces, tabs,
// line breaks and a non-breaking space) to reflect a real conversation body.
func BenchmarkNormalizeText(b *testing.B) {
	body := strings.Builder{}
	body.Grow(8 * 1024)
	for i := 0; i < 400; i++ {
		body.WriteString("On Mon, ")
		body.WriteString("Alice <alice@example.com> wrote:\r\n")
		body.WriteString("> ")
		body.WriteString("This is a quoted line\twith a tab\u00a0and a non-breaking space.\n\n")
		body.WriteString("Regards,\nBob\n\n")
	}
	input := body.String()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if got := NormalizeText(input); got == "" {
			b.Fatal("NormalizeText returned empty")
		}
	}
}

// BenchmarkNormalizeTextShort checks a short, sender-name-sized string with no
// whitespace to confirm the single-pass decoder does not regress small inputs.
func BenchmarkNormalizeTextShort(b *testing.B) {
	input := "Alice <alice@example.com>"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if got := NormalizeText(input); got != input {
			b.Fatalf("NormalizeText(%q) = %q, want %q", input, got, input)
		}
	}
}

func TestFixtureWarningsAreKnown(t *testing.T) {
	for _, f := range loadFixtures(t) {
		for _, w := range f.Expect.Warnings {
			if !IsKnownWarning(w) {
				t.Errorf("%s: unknown warning %q", f.Name, w)
			}
		}
	}
}

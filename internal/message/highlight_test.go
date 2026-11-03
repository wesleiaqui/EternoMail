package message

import (
	"testing"
)

func TestHighlightMatches(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		query    string
		expected string
	}{
		{
			name:     "empty text",
			text:     "",
			query:    "test",
			expected: "",
		},
		{
			name:     "empty query",
			text:     "Hello World",
			query:    "",
			expected: "Hello World",
		},
		{
			name:     "whitespace query",
			text:     "Hello World",
			query:    "   ",
			expected: "Hello World",
		},
		{
			name:     "single match case insensitive",
			text:     "Important Meeting Tomorrow",
			query:    "meeting",
			expected: "Important <mark>Meeting</mark> Tomorrow",
		},
		{
			name:     "multiple query terms",
			text:     "Project roadmap update and sync notes",
			query:    "roadmap sync",
			expected: "Project <mark>roadmap</mark> update and <mark>sync</mark> notes",
		},
		{
			name:     "HTML escaping prevents XSS",
			text:     `<script>alert("xss")</script> & urgent`,
			query:    "urgent",
			expected: `&lt;script&gt;alert(&#34;xss&#34;)&lt;/script&gt; &amp; <mark>urgent</mark>`,
		},
		{
			name:     "special regex characters in query",
			text:     "Cost is $100 [USD] (confirmed)?",
			query:    "$100 [USD] (confirmed)?",
			expected: "Cost is <mark>$100</mark> <mark>[USD]</mark> <mark>(confirmed)?</mark>",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := highlightMatches(tt.text, tt.query)
			if got != tt.expected {
				t.Errorf("highlightMatches(%q, %q) = %q; want %q", tt.text, tt.query, got, tt.expected)
			}
		})
	}
}

func BenchmarkHighlightMatches(b *testing.B) {
	query := "roadmap quarterly planning review"
	subject := "Q4 Product Roadmap and Quarterly Planning Review"
	snippet := "Here are the notes from our discussion regarding the roadmap and quarterly planning targets for next quarter."
	fromName := "Product Planning Team"

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = highlightMatches(subject, query)
		_ = highlightMatches(snippet, query)
		_ = highlightMatches(fromName, query)
	}
}

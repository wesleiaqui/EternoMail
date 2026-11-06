package message

import (
	"testing"
)

func TestPrepareFTSQuery(t *testing.T) {
	tests := []struct {
		name     string
		query    string
		expected string
	}{
		{
			name:     "empty query",
			query:    "",
			expected: "",
		},
		{
			name:     "whitespace only",
			query:    "   \t \n  ",
			expected: "",
		},
		{
			name:     "single word",
			query:    "hello",
			expected: `"hello"*`,
		},
		{
			name:     "multiple words",
			query:    "urgent invoice payment",
			expected: `"urgent"* "invoice"* "payment"*`,
		},
		{
			name:     "word with existing wildcard suffix",
			query:    "report*",
			expected: "report*",
		},
		{
			name:     "mixed regular words and wildcard suffix",
			query:    "quarterly report*",
			expected: `"quarterly"* report*`,
		},
		{
			name:     "word with double quotes",
			query:    `test"quote`,
			expected: `"test""quote"*`,
		},
		{
			name:     "phrase with double quotes",
			query:    `"contract agreement"`,
			expected: `"""contract"* "agreement"""*`,
		},
		{
			name:     "wildcard with quotes",
			query:    `"quoted"*`,
			expected: `""quoted""*`,
		},
		{
			name:     "unicode words",
			query:    "päivää maailma",
			expected: `"päivää"* "maailma"*`,
		},
		{
			name:     "special punctuation characters",
			query:    "foo:bar status^1 (urgent)",
			expected: `"foo:bar"* "status^1"* "(urgent)"*`,
		},
		{
			name:     "unicode whitespace non-breaking and ideographic",
			query:    "hello\u00a0world\u3000test",
			expected: `"hello"* "world"* "test"*`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := prepareFTSQuery(tt.query)
			if got != tt.expected {
				t.Errorf("prepareFTSQuery(%q) = %q; want %q", tt.query, got, tt.expected)
			}
		})
	}
}

func BenchmarkPrepareFTSQuery(b *testing.B) {
	queries := []string{
		"urgent invoice",
		"quarterly financial planning review 2026",
		"simple",
		`"contract agreement"`,
		"report*",
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, q := range queries {
			_ = prepareFTSQuery(q)
		}
	}
}

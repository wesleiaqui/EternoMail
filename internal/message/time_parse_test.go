package message

import (
	"testing"
	"time"
)

func TestParseTimeString(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected time.Time
	}{
		{
			name:     "empty",
			input:    "",
			expected: time.Time{},
		},
		{
			name:     "standard sqlite datetime",
			input:    "2026-11-05 14:32:10",
			expected: time.Date(2026, 11, 5, 14, 32, 10, 0, time.UTC),
		},
		{
			name:     "iso datetime without timezone",
			input:    "2026-11-05T14:32:10",
			expected: time.Date(2026, 11, 5, 14, 32, 10, 0, time.UTC),
		},
		{
			name:     "date only",
			input:    "2026-11-05",
			expected: time.Date(2026, 11, 5, 0, 0, 0, 0, time.UTC),
		},
		{
			name:     "sqlite time.Time.String() format",
			input:    "2026-11-05 14:32:10 +0000 UTC",
			expected: time.Date(2026, 11, 5, 14, 32, 10, 0, time.UTC),
		},
		{
			name:     "rfc3339",
			input:    "2026-11-05T14:32:10Z",
			expected: time.Date(2026, 11, 5, 14, 32, 10, 0, time.UTC),
		},
		{
			name:     "invalid format",
			input:    "not-a-date",
			expected: time.Time{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseTimeString(tt.input)
			if !got.Equal(tt.expected) {
				t.Errorf("parseTimeString(%q) = %v; want %v", tt.input, got, tt.expected)
			}
		})
	}
}

func BenchmarkParseTimeString(b *testing.B) {
	samples := []string{
		"2026-11-05 14:32:10",
		"2026-11-05 14:32:10 +0000 UTC",
		"2026-11-05T14:32:10Z",
		"2026-11-05",
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, s := range samples {
			_ = parseTimeString(s)
		}
	}
}

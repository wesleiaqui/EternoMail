package sync

import "testing"

// BenchmarkExtractCharsetFromHTML measures the hot per-message path of
// extracting a charset from an HTML meta tag. extractCharsetFromHTML is
// invoked once per HTML message part during sync parsing (5 call sites in
// parse.go), so avoiding per-call work here compounds across a whole mailbox.
func BenchmarkExtractCharsetFromHTML(b *testing.B) {
	// A realistic HTML message head containing a charset meta declaration,
	// representative of inbound mail passed to extractCharsetFromHTML.
	body := []byte(
		`<!DOCTYPE html><html><head><meta http-equiv="Content-Type" content="text/html; charset=utf-8"><title>Example</title></head><body>` +
			`<p>This is a realistic multi-kilobyte HTML email body. ` +
			`It contains enough content to exceed the 1024-byte search window, ` +
			`so the function scans the trimmed prefix on every call.</p>` +
			`</body></html>`,
	)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = extractCharsetFromHTML(body)
	}
}

package sync

import (
	"context"
	"encoding/json"
	"github.com/emersion/go-imap/v2"
	"github.com/hkdb/aerion/internal/message"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestHeaderSanitization(t *testing.T) {
	m := new(message.Message)
	applyEnvelopeToMessage(m, &imap.Envelope{Subject: strings.Repeat("é", 5000) + "\x00", From: []imap.Address{{Name: "a\x00b", Mailbox: "x", Host: "example.com"}}, To: []imap.Address{{Name: strings.Repeat("é", 5000), Mailbox: "x", Host: "example.com"}}})
	if len(m.Subject) != 8192 || !utf8.ValidString(m.Subject) || m.FromName != "ab" {
		t.Fatalf("invalid sanitized headers")
	}
	if len(m.ToList) > 8192 || !json.Valid([]byte(m.ToList)) || strings.Contains(m.ToList, `\u0000`) {
		t.Fatal("invalid recipients")
	}
	recovered := new(message.Message)
	if err := parseHeadersIntoMessage(recovered, []byte("Subject: "+strings.Repeat("a", 9000)+"\r\n\r\n")); err != nil {
		t.Fatal(err)
	}
	if len(recovered.Subject) != 8192 {
		t.Fatal("recovery bypasses limit")
	}
}

func TestCharsetFallbackOriginal(t *testing.T) {
	original := "original text"
	if got := decodeCharset([]byte(original), "unknown-charset"); got != original {
		t.Fatal(got)
	}
	if got := decodeMIMEWord("=?invalid?Q?hello?="); got != "=?invalid?Q?hello?=" {
		t.Fatal(got)
	}
}

func TestSchedulerCancellationWaitsForWorkers(t *testing.T) {
	s := NewScheduler(nil, nil, nil)
	s.Start(context.Background())
	exited := make(chan struct{})
	s.wg.Add(1)
	go func(ctx context.Context) { defer s.wg.Done(); <-ctx.Done(); close(exited) }(s.ctx)
	done := make(chan struct{})
	go func() { s.Stop(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Stop blocked")
	}
	select {
	case <-exited:
	default:
		t.Fatal("worker still alive")
	}
	s.startAccountSync(nil) // stopped scheduler must not launch work
	s.Start(context.Background())
	s.Stop()
}

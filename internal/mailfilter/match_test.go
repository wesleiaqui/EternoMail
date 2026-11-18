package mailfilter

import (
	"encoding/json"
	"testing"

	"github.com/hkdb/aerion/internal/message"
)

func TestMatch(t *testing.T) {
	tests := []struct {
		name string
		rule *Rule
		msg  *Message
		want bool
	}{
		{
			name: "no criteria matches any message on another account",
			rule: &Rule{ID: "r1", Name: "route", Enabled: true, DestFolderID: "dest"},
			msg:  &Message{AccountID: "acct-a", FolderID: "inbox-a", FromEmail: "bob@example.com", Subject: "hello"},
			want: true,
		},
		{
			name: "disabled rule never matches",
			rule: &Rule{ID: "r2", Name: "off", Enabled: false, DestFolderID: "dest"},
			msg:  &Message{AccountID: "acct-a", FolderID: "inbox-a", FromEmail: "bob@example.com"},
			want: false,
		},
		{
			name: "account scope excludes other accounts",
			rule: &Rule{ID: "r3", Name: "scoped", Enabled: true, AccountID: "acct-a", DestFolderID: "dest"},
			msg:  &Message{AccountID: "acct-b", FolderID: "inbox-b", FromEmail: "bob@example.com"},
			want: false,
		},
		{
			name: "account scope matches the scoped account",
			rule: &Rule{ID: "r4", Name: "scoped", Enabled: true, AccountID: "acct-a", DestFolderID: "dest"},
			msg:  &Message{AccountID: "acct-a", FolderID: "inbox-a", FromEmail: "bob@example.com"},
			want: true,
		},
		{
			name: "empty rule account matches any account",
			rule: &Rule{ID: "r5", Name: "any account", Enabled: true, DestFolderID: "dest"},
			msg:  &Message{AccountID: "acct-b", FolderID: "inbox-b", FromEmail: "bob@example.com"},
			want: true,
		},
		{
			name: "folder scope excludes non-listed folders",
			rule: &Rule{ID: "r6", Name: "folders", Enabled: true, FolderIDs: []string{"inbox-a", "work-a"}, DestFolderID: "dest"},
			msg:  &Message{AccountID: "acct-a", FolderID: "spam-a", FromEmail: "bob@example.com"},
			want: false,
		},
		{
			name: "folder scope matches a listed folder",
			rule: &Rule{ID: "r7", Name: "folders", Enabled: true, FolderIDs: []string{"inbox-a", "work-a"}, DestFolderID: "dest"},
			msg:  &Message{AccountID: "acct-a", FolderID: "work-a", FromEmail: "bob@example.com"},
			want: true,
		},
		{
			name: "same-account destination still matches; the mover skips the no-op relocation",
			rule: &Rule{ID: "r8", Name: "same account", Enabled: true, AccountID: "acct-a", DestFolderID: "dest"},
			msg:  &Message{AccountID: "acct-a", FolderID: "inbox-a", FromEmail: "bob@example.com"},
			want: true,
		},
		{
			name: "from contains matches email case-insensitively",
			rule: &Rule{ID: "r9", Name: "from", Enabled: true, FromContains: []string{"BOB@example.com"}, DestFolderID: "dest"},
			msg:  &Message{AccountID: "acct-a", FolderID: "inbox-a", FromEmail: "bob@example.com"},
			want: true,
		},
		{
			name: "from contains matches the display name",
			rule: &Rule{ID: "r10", Name: "from name", Enabled: true, FromContains: []string{"bob"}, DestFolderID: "dest"},
			msg:  &Message{AccountID: "acct-a", FolderID: "inbox-a", FromName: "Bob Smith", FromEmail: "mail@example.com"},
			want: true,
		},
		{
			name: "from contains misses when no pattern hits",
			rule: &Rule{ID: "r11", Name: "from miss", Enabled: true, FromContains: []string{"mall@example.com"}, DestFolderID: "dest"},
			msg:  &Message{AccountID: "acct-a", FolderID: "inbox-a", FromEmail: "bob@example.com"},
			want: false,
		},
		{
			name: "any pattern inside a criterion suffices",
			rule: &Rule{ID: "r12", Name: "from any", Enabled: true, FromContains: []string{"mall@example.com", "bob@example.com"}, DestFolderID: "dest"},
			msg:  &Message{AccountID: "acct-a", FolderID: "inbox-a", FromEmail: "bob@example.com"},
			want: true,
		},
		{
			name: "to contains matches a recipient email",
			rule: &Rule{ID: "r13", Name: "to", Enabled: true, ToContains: []string{"me@corp.example"}, DestFolderID: "dest"},
			msg:  &Message{AccountID: "acct-a", FolderID: "inbox-a", FromEmail: "bob@example.com", ToEmails: []string{"me@corp.example", "other@example.com"}},
			want: true,
		},
		{
			name: "to contains matches a recipient display name",
			rule: &Rule{ID: "r14", Name: "to name", Enabled: true, ToContains: []string{"Alice"}, DestFolderID: "dest"},
			msg:  &Message{AccountID: "acct-a", FolderID: "inbox-a", FromEmail: "bob@example.com", ToNames: []string{"Alice"}},
			want: true,
		},
		{
			name: "to contains misses when absent",
			rule: &Rule{ID: "r15", Name: "to miss", Enabled: true, ToContains: []string{"nobody@example.com"}, DestFolderID: "dest"},
			msg:  &Message{AccountID: "acct-a", FolderID: "inbox-a", FromEmail: "bob@example.com", ToEmails: []string{"me@corp.example"}},
			want: false,
		},
		{
			name: "subject contains is case-insensitive",
			rule: &Rule{ID: "r16", Name: "subject", Enabled: true, SubjectContains: []string{"invoice"}, DestFolderID: "dest"},
			msg:  &Message{AccountID: "acct-a", FolderID: "inbox-a", FromEmail: "bob@example.com", Subject: "Your INVOICE #42"},
			want: true,
		},
		{
			name: "subject contains misses when absent",
			rule: &Rule{ID: "r17", Name: "subject miss", Enabled: true, SubjectContains: []string{"invoice"}, DestFolderID: "dest"},
			msg:  &Message{AccountID: "acct-a", FolderID: "inbox-a", FromEmail: "bob@example.com", Subject: "hello"},
			want: false,
		},
		{
			name: "all non-empty criteria must hold",
			rule: &Rule{ID: "r18", Name: "and", Enabled: true, FromContains: []string{"bob@"}, SubjectContains: []string{"invoice"}, DestFolderID: "dest"},
			msg:  &Message{AccountID: "acct-a", FolderID: "inbox-a", FromEmail: "bob@example.com", Subject: "hello"},
			want: false,
		},
		{
			name: "all non-empty criteria hold",
			rule: &Rule{ID: "r19", Name: "and ok", Enabled: true, FromContains: []string{"bob@"}, SubjectContains: []string{"invoice"}, DestFolderID: "dest"},
			msg:  &Message{AccountID: "acct-a", FolderID: "inbox-a", FromEmail: "bob@example.com", Subject: "invoice #42"},
			want: true,
		},
		{
			name: "from patterns never match recipients",
			rule: &Rule{ID: "r20", Name: "no cross field", Enabled: true, FromContains: []string{"bob@example.com"}, DestFolderID: "dest"},
			msg:  &Message{AccountID: "acct-a", FolderID: "inbox-a", FromEmail: "other@example.com", ToEmails: []string{"bob@example.com"}},
			want: false,
		},
		{
			name: "list id matches case-insensitively",
			rule: &Rule{ID: "r21", Name: "list", Enabled: true, ListIDContains: []string{"Announce.Example.COM"}, DestFolderID: "dest"},
			msg:  &Message{AccountID: "acct-a", FolderID: "inbox-a", FromEmail: "bob@example.com", ListID: "announce.example.com"},
			want: true,
		},
		{
			name: "list id misses when absent",
			rule: &Rule{ID: "r22", Name: "list miss", Enabled: true, ListIDContains: []string{"announce.example.com"}, DestFolderID: "dest"},
			msg:  &Message{AccountID: "acct-a", FolderID: "inbox-a", FromEmail: "bob@example.com"},
			want: false,
		},
		{
			name: "multi-account scope matches a listed account",
			rule: &Rule{ID: "r23", Name: "multi", Enabled: true, AccountIDs: []string{"acct-a", "acct-b"}, ActionMarkRead: true},
			msg:  &Message{AccountID: "acct-b", FolderID: "inbox-b", FromEmail: "bob@example.com"},
			want: true,
		},
		{
			name: "multi-account scope excludes unlisted accounts",
			rule: &Rule{ID: "r24", Name: "multi", Enabled: true, AccountIDs: []string{"acct-a", "acct-b"}, ActionMarkRead: true},
			msg:  &Message{AccountID: "acct-c", FolderID: "inbox-c", FromEmail: "bob@example.com"},
			want: false,
		},
		{
			name: "multi-account scope takes precedence over the legacy account",
			rule: &Rule{ID: "r25", Name: "multi wins", Enabled: true, AccountID: "acct-z", AccountIDs: []string{"acct-a"}, ActionMarkRead: true},
			msg:  &Message{AccountID: "acct-a", FolderID: "inbox-a", FromEmail: "bob@example.com"},
			want: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := Match(tc.rule, tc.msg); got != tc.want {
				t.Fatalf("Match() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestFromMessage(t *testing.T) {
	t.Run("parses To and Cc lists", func(t *testing.T) {
		to, _ := json.Marshal([]message.Address{{Name: "Me", Email: "me@corp.example"}, {Name: "", Email: "cc-me@corp.example"}})
		in := &message.Message{
			AccountID: "acct-a",
			FolderID:  "inbox-a",
			FromName:  "Bob",
			FromEmail: "bob@example.com",
			Subject:   "hi",
			ToList:    string(to),
			CcList:    `[{"name":"","email":"extra@example.com"}]`,
		}
		out := FromMessage(in)
		if out.AccountID != "acct-a" || out.FolderID != "inbox-a" {
			t.Fatalf("identity fields not copied: %+v", out)
		}
		if out.FromEmail != "bob@example.com" || out.FromName != "Bob" || out.Subject != "hi" {
			t.Fatalf("from/subject fields not copied: %+v", out)
		}
		if len(out.ToEmails) != 3 || out.ToEmails[0] != "me@corp.example" || out.ToEmails[1] != "cc-me@corp.example" || out.ToEmails[2] != "extra@example.com" {
			t.Fatalf("unexpected ToEmails: %v", out.ToEmails)
		}
	})

	t.Run("tolerates empty or malformed lists", func(t *testing.T) {
		out := FromMessage(&message.Message{AccountID: "a", FolderID: "f", ToList: "", CcList: "not-json"})
		if len(out.ToEmails) != 0 || len(out.ToNames) != 0 {
			t.Fatalf("expected no recipients, got %v %v", out.ToEmails, out.ToNames)
		}
	})
}

package message

import "testing"

func TestSnapshotAttachmentIdentityDoesNotConfuseSimilarParts(t *testing.T) {
	parts := func() []*Attachment {
		return []*Attachment{{Filename: "same.png", Size: 3}, {Filename: "same.png", Size: 3}}
	}
	first := parts()
	AssignSnapshotAttachmentIDs(first, "mail-a", []byte("raw-complete"), "sync-body")
	repeated := parts()
	AssignSnapshotAttachmentIDs(repeated, "mail-a", []byte("raw-complete"), "sync-body")
	if first[0].ID != repeated[0].ID || first[1].ID != repeated[1].ID {
		t.Fatal("exact snapshot refetch changes identity")
	}
	if first[0].ID == first[1].ID {
		t.Fatal("two real same-name/same-size parts collapsed")
	}
	for _, c := range []struct{ message, raw, parser string }{{"mail-b", "raw-complete", "sync-body"}, {"mail-a", "raw-partial", "sync-body"}, {"mail-a", "raw-complete", "attachment-extractor"}} {
		other := parts()
		AssignSnapshotAttachmentIDs(other, c.message, []byte(c.raw), c.parser)
		if other[0].ID == first[0].ID {
			t.Fatalf("different input overwrites prior part: %+v", c)
		}
	}
}

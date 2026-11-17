package email

import (
	"strings"
	"testing"
)

func repairFixture() string {
	return "MIME-Version: 1.0\r\nContent-Type: multipart/mixed; boundary=b\r\n\r\n--b\r\nContent-Type: text/plain\r\n\r\nHello\r\n--b\r\nContent-Type: image/png\r\nContent-Disposition: inline; filename=same.png\r\nContent-ID: <img>\r\n\r\none\r\n--b\r\nContent-Type: image/png\r\nContent-Disposition: attachment; filename=same.png\r\n\r\ntwo\r\n--b--\r\n"
}
func TestRepairParseRequiresCompleteMIMEAndKeepsRealParts(t *testing.T) {
	raw := repairFixture()
	parts, err := ParseAttachmentsForRepair("mail", []byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if len(parts) != 2 || string(parts[0].Content) != "one" || string(parts[1].Content) != "two" {
		t.Fatal("real parts lost")
	}
	if parts[0].Attachment.ID == parts[1].Attachment.ID {
		t.Fatal("same-name parts merged")
	}
	repeated, err := ParseAttachmentsForRepair("mail", []byte(raw))
	if err != nil || repeated[0].Attachment.ID != parts[0].Attachment.ID {
		t.Fatal("repair identity unstable")
	}
	if AttachmentContentDigest(parts[0].Content) == AttachmentContentDigest(parts[1].Content) {
		t.Fatal("distinct content conflated")
	}
	for name, bad := range map[string]string{
		"missing final boundary":            strings.TrimSuffix(raw, "--b--\r\n"),
		"truncated after attachment header": strings.Split(raw, "\r\n\r\ntwo")[0],
		"unknown encoding":                  strings.Replace(raw, "Content-Type: image/png", "Content-Transfer-Encoding: invalid\r\nContent-Type: image/png", 1),
		"corrupt base64":                    strings.Replace(raw, "\r\n\r\none", "\r\nContent-Transfer-Encoding: base64\r\n\r\n%%%%", 1),
		"encrypted":                         strings.Replace(raw, "multipart/mixed", "multipart/encrypted", 1),
		"signed":                            strings.Replace(raw, "multipart/mixed", "multipart/signed", 1),
		"nested original":                   strings.Replace(raw, "image/png", "message/rfc822", 1),
		"converted text attachment":         strings.Replace(raw, "Content-Type: image/png\r\nContent-Disposition: attachment", "Content-Type: text/plain; charset=iso-8859-1\r\nContent-Disposition: attachment", 1),
		"tnef":                              strings.Replace(raw, "image/png", "application/ms-tnef", 1),
	} {
		t.Run(name, func(t *testing.T) {
			if got, err := ParseAttachmentsForRepair("mail", []byte(bad)); err == nil || got != nil {
				t.Fatalf("unsafe repair accepted: %v parts=%d", err, len(got))
			}
		})
	}
}

func TestRepairPreservesInlineTextFile(t *testing.T) {
	raw := []byte("Content-Type: text/plain; charset=utf-8\r\nContent-Disposition: inline; filename=notes.txt\r\n\r\ntext file")
	got, err := ParseAttachmentsForRepair("mail", raw)
	if err != nil || len(got) != 1 || string(got[0].Content) != "text file" || got[0].Attachment.Filename != "notes.txt" {
		t.Fatal("inline text file lost", err)
	}
}

func TestRepairRejectsAttachedMultipartWithoutReturningReplacement(t *testing.T) {
	for _, disposition := range []string{"attachment; filename=archive.mht", "inline; filename=archive.mht"} {
		raw := []byte("Content-Type: multipart/related; boundary=archive\r\nContent-Disposition: " + disposition + "\r\n\r\n--archive\r\nContent-Type: text/html\r\n\r\n<p>Only child</p>\r\n--archive--\r\n")
		if got, err := ParseAttachmentsForRepair("mail", raw); err == nil || got != nil {
			t.Fatal("attached multipart allowed to drop its named part", err)
		}
	}
}

func TestRepairInlineBodyClassification(t *testing.T) {
	for _, ct := range []string{"text/plain", "text/html", "text/calendar", "text/vcard"} {
		for _, disposition := range []string{"inline", "inline; filename=body.txt", "attachment"} {
			t.Run(ct+"/"+disposition, func(t *testing.T) {
				raw := strings.Replace(repairFixture(), "Content-Type: text/plain\r\n\r\nHello", "Content-Type: "+ct+"\r\nContent-Disposition: "+disposition+"\r\n\r\nHello", 1)
				parts, err := ParseAttachmentsForRepair("mail", []byte(raw))
				if err != nil {
					t.Fatal(err)
				}
				want := 3
				if disposition == "inline" && (ct == "text/plain" || ct == "text/html") {
					want = 2
				}
				if len(parts) != want {
					t.Fatalf("got %d parts, want %d", len(parts), want)
				}
				if string(parts[len(parts)-2].Content) != "one" || string(parts[len(parts)-1].Content) != "two" {
					t.Fatal("real attachments lost or reordered")
				}
			})
		}
	}
}

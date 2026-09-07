package email

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	gomessage "github.com/emersion/go-message"
	"github.com/hkdb/aerion/internal/message"
	"os"
	"strings"
	"testing"
)

func TestTransferEncodingDecodedExactlyOnce(t *testing.T) {
	for _, encoding := range []string{"base64", "quoted-printable"} {
		payload := "SGVsbG8="
		encoded := base64.StdEncoding.EncodeToString([]byte(payload))
		if encoding == "quoted-printable" {
			payload = "literal=41"
			encoded = "literal=3D41"
		}
		for _, multipart := range []bool{false, true} {
			raw := "Content-Type: application/octet-stream\r\nContent-Disposition: attachment; filename=test.txt\r\nContent-ID: <cid>\r\nContent-Transfer-Encoding: " + encoding + "\r\n\r\n" + encoded
			if multipart {
				raw = "Content-Type: multipart/mixed; boundary=b\r\n\r\n--b\r\n" + raw + "\r\n--b--\r\n"
			}
			d := NewAttachmentDownloader(t.TempDir())
			got, err := d.ExtractAttachmentContent([]byte(raw), "test.txt")
			if err != nil || string(got) != payload {
				t.Fatalf("%s multipart=%v: bytes corrupted: %q %v", encoding, multipart, got, err)
			}
			saved, err := d.SaveAttachment(&message.Attachment{Filename: "test.txt", MessageID: "synthetic"}, got, "")
			if err != nil {
				t.Fatal(err)
			}
			disk, err := os.ReadFile(saved)
			if err != nil || string(disk) != payload {
				t.Fatalf("saved bytes changed: %q %v", disk, err)
			}
			if multipart {
				attachments, err := NewAttachmentExtractor().ExtractAttachments("m", []byte(raw))
				if err != nil || len(attachments) != 1 || string(attachments[0].Content) != payload {
					t.Fatal("extractor corrupted bytes", err)
				}
				inline, err := d.ExtractInlineAttachments([]byte(raw))
				if err != nil || inline["cid"] != buildDataURL("application/octet-stream", []byte(payload)) {
					t.Fatal("inline bytes corrupted", err)
				}
			}
		}
	}
}

type failingMultipart struct{ calls int }

func (r *failingMultipart) Close() error { return nil }
func (r *failingMultipart) NextPart() (*gomessage.Entity, error) {
	r.calls++
	if r.calls > 25 {
		panic("unbounded MIME error loop")
	}
	return nil, fmt.Errorf("malformed multipart")
}
func TestMalformedMultipartStops(t *testing.T) {
	NewAttachmentExtractor().extractFromMultipart("m", &failingMultipart{})
	d := NewAttachmentDownloader(t.TempDir())
	d.findInlineAttachmentsInMultipart(&failingMultipart{}, map[string]string{})
	if _, err := d.findAttachmentInMultipart(&failingMultipart{}, "x"); err == nil {
		t.Fatal("expected error")
	}
}

func TestDeepMultipartBounded(t *testing.T) {
	var raw strings.Builder
	for i := 0; i < 100; i++ {
		fmt.Fprintf(&raw, "Content-Type: multipart/mixed; boundary=b%d\r\n\r\n--b%d\r\n", i, i)
	}
	raw.WriteString("Content-Type: application/octet-stream\r\nContent-Disposition: attachment; filename=x\r\n\r\npayload")
	for i := 99; i >= 0; i-- {
		fmt.Fprintf(&raw, "\r\n--b%d--\r\n", i)
	}
	attachments, err := NewAttachmentExtractor().ExtractAttachments("m", []byte(raw.String()))
	if err != nil || len(attachments) != 0 {
		t.Fatal("nesting bound not applied", err)
	}
	if _, err := NewAttachmentDownloader(t.TempDir()).ExtractAttachmentContent([]byte(raw.String()), "x"); err == nil {
		t.Fatal("nesting bound not applied to download")
	}
}

func TestHostileTNEFDoesNotPanicOrLoop(t *testing.T) {
	hugeCount := make([]byte, 4)
	binary.LittleEndian.PutUint32(hugeCount, 0xffffffff)
	malformed := append([]byte{0x78, 0x9f, 0x3e, 0x22, 0, 0}, tnefObj(1, 0x9003, hugeCount)...)
	for _, data := range [][]byte{nil, {1}, {0x78, 0x9f, 0x3e, 0x22, 0, 0, 1}, malformed, append([]byte{0x78, 0x9f, 0x3e, 0x22, 0, 0}, tnefObj(2, 0x8010, []byte("no attachment header"))...)} {
		if got := DecodeTNEFAttachments(data); len(got) != 0 {
			t.Fatal("accepted malformed TNEF")
		}
	}
}

func TestAttachmentReadLimit(t *testing.T) {
	// Reuses one small buffer; the reader must stop after limit+1 bytes.
	r := &repeatingAttachmentReader{}
	if _, err := readAttachmentContent(r); err == nil {
		t.Fatal("accepted unbounded input")
	}
	if r.read != maxAttachmentBytes+1 {
		t.Fatalf("read %d bytes", r.read)
	}
	if got, err := readAttachmentContent(bytes.NewBufferString("ordinary attachment")); err != nil || string(got) != "ordinary attachment" {
		t.Fatal(err)
	}
}

type repeatingAttachmentReader struct{ read int }

func (r *repeatingAttachmentReader) Read(p []byte) (int, error) {
	clear(p)
	r.read += len(p)
	return len(p), nil
}

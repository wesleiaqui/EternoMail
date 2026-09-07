// Package email provides email content processing utilities
package email

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"mime"
	"path/filepath"
	"strings"

	gomessage "github.com/emersion/go-message"
	"github.com/google/uuid"
	"github.com/hkdb/aerion/internal/message"
	"github.com/teamwork/tnef"
)

// AttachmentExtractor extracts attachment metadata and content from emails
type AttachmentExtractor struct{}

// NewAttachmentExtractor creates a new attachment extractor
func NewAttachmentExtractor() *AttachmentExtractor {
	return &AttachmentExtractor{}
}

// AttachmentData holds both metadata and content for an attachment
type AttachmentData struct {
	Attachment *message.Attachment
	Content    []byte
}

// ExtractAttachments extracts all attachments from a raw email message
func (e *AttachmentExtractor) ExtractAttachments(messageID string, raw []byte) ([]*AttachmentData, error) {
	if len(raw) > maxAttachmentBytes {
		return nil, fmt.Errorf("message exceeds attachment extraction limit")
	}
	reader := bytes.NewReader(raw)

	entity, err := gomessage.Read(reader)
	if err != nil {
		return nil, fmt.Errorf("failed to parse message: %w", err)
	}

	var attachments []*AttachmentData

	// Check if it's a multipart message
	if mr := entity.MultipartReader(); mr != nil {
		attachments = e.extractFromMultipart(messageID, mr)
	}

	return attachments, nil
}

// extractFromMultipart extracts attachments from a multipart message
func (e *AttachmentExtractor) extractFromMultipart(messageID string, mr gomessage.MultipartReader) []*AttachmentData {
	return e.extractFromMultipartDepth(messageID, mr, 0)
}

func (e *AttachmentExtractor) extractFromMultipartDepth(messageID string, mr gomessage.MultipartReader, depth int) []*AttachmentData {
	if depth >= 64 {
		return nil
	}
	var attachments []*AttachmentData

	errorsInARow := 0
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			errorsInARow++
			if errorsInARow >= 20 {
				break
			}
			continue
		}
		errorsInARow = 0

		contentType, params, _ := mime.ParseMediaType(part.Header.Get("Content-Type"))
		disposition, dispParams, _ := mime.ParseMediaType(part.Header.Get("Content-Disposition"))
		contentID := strings.Trim(part.Header.Get("Content-ID"), "<>")

		// Handle nested multipart
		if strings.HasPrefix(contentType, "multipart/") {
			if nestedMr := part.MultipartReader(); nestedMr != nil {
				nested := e.extractFromMultipartDepth(messageID, nestedMr, depth+1)
				attachments = append(attachments, nested...)
			}
			continue
		}

		// Check for TNEF (winmail.dat)
		if contentType == "application/ms-tnef" ||
			(disposition == "attachment" && strings.EqualFold(dispParams["filename"], "winmail.dat")) {
			tnefAttachments := e.extractFromTNEF(messageID, part.Body)
			attachments = append(attachments, tnefAttachments...)
			continue
		}

		// Determine if this is an attachment
		isAttachment := disposition == "attachment"
		isInline := disposition == "inline" || contentID != ""

		// Skip text/plain and text/html unless they're explicit attachments
		if !isAttachment && (contentType == "text/plain" || contentType == "text/html") {
			continue
		}

		// If it's not text and has content, treat it as an attachment
		if isAttachment || isInline || (!strings.HasPrefix(contentType, "text/") && contentType != "") {
			// Get filename
			filename := dispParams["filename"]
			if filename == "" {
				filename = params["name"]
			}
			if filename == "" {
				ext := ".bin"
				if strings.HasPrefix(contentType, "image/") {
					parts := strings.SplitN(contentType, "/", 2)
					if len(parts) == 2 {
						ext = "." + parts[1]
					}
				}
				filename = "attachment" + ext
			}

			// Decode filename if encoded
			decodedFilename, err := decodeRFC2047(filename)
			if err == nil {
				filename = decodedFilename
			}

			// Read content
			content, err := readAttachmentContent(part.Body)
			if err != nil {
				continue
			}

			// The MIME reader has already applied transfer decoding.
			decodedContent := content // go-message already decoded Content-Transfer-Encoding

			att := &message.Attachment{
				ID:          uuid.New().String(),
				MessageID:   messageID,
				Filename:    filename,
				ContentType: contentType,
				Size:        len(decodedContent),
				ContentID:   contentID,
				IsInline:    isInline && contentID != "",
			}

			attachments = append(attachments, &AttachmentData{
				Attachment: att,
				Content:    decodedContent,
			})
		}
	}

	return attachments
}

// TNEFAttachment is a single attachment decoded from a TNEF (winmail.dat) container.
type TNEFAttachment struct {
	Filename    string
	ContentType string
	Content     []byte
}

// DecodeTNEFAttachments decodes a TNEF (winmail.dat) container into its inner
// attachments, returning nil if the bytes are not valid TNEF. This is the single
// source of TNEF decoding shared by the sync extractor, the on-demand extractor,
// and the downloader, so the three paths can never diverge on filename/type — the
// invariant that lets sync store a name the downloader can later resolve.
func DecodeTNEFAttachments(data []byte) (result []TNEFAttachment) {
	// The legacy decoder slices untrusted offsets and may panic on truncated
	// attributes. Confine that failure to this optional attachment container.
	defer func() {
		if recover() != nil {
			result = nil
		}
	}()
	if !validTNEFEnvelope(data) {
		return nil
	}

	tnefData, err := tnef.Decode(data)
	if err != nil {
		return nil
	}

	var out []TNEFAttachment
	for _, att := range tnefData.Attachments {
		filename := att.Title
		if filename == "" {
			filename = "attachment"
		}

		// Guess content type from the filename extension.
		contentType := "application/octet-stream"
		if guessed := mime.TypeByExtension(filepath.Ext(filename)); guessed != "" {
			contentType = guessed
		}

		out = append(out, TNEFAttachment{
			Filename:    filename,
			ContentType: contentType,
			Content:     att.Data,
		})
	}

	return out
}

// extractFromTNEF extracts attachments from a TNEF (winmail.dat) file
func (e *AttachmentExtractor) extractFromTNEF(messageID string, reader io.Reader) []*AttachmentData {
	data, err := readAttachmentContent(reader)
	if err != nil {
		return nil
	}

	var attachments []*AttachmentData
	for _, tnefAtt := range DecodeTNEFAttachments(data) {
		attachments = append(attachments, &AttachmentData{
			Attachment: &message.Attachment{
				ID:          uuid.New().String(),
				MessageID:   messageID,
				Filename:    tnefAtt.Filename,
				ContentType: tnefAtt.ContentType,
				Size:        len(tnefAtt.Content),
				IsInline:    false,
			},
			Content: tnefAtt.Content,
		})
	}

	return attachments
}

// decodeRFC2047 decodes RFC 2047 encoded strings (like filenames)
func decodeRFC2047(s string) (string, error) {
	dec := new(mime.WordDecoder)
	return dec.DecodeHeader(s)
}

// Validate lengths and property counts before entering the legacy decoder.
// In particular, its MAPI loop keeps iterating after the input is exhausted.
func validTNEFEnvelope(data []byte) bool {
	if len(data) < 6 || len(data) > maxAttachmentBytes || binary.LittleEndian.Uint32(data[:4]) != 0x223e9f78 {
		return false
	}
	for offset := 6; offset < len(data); {
		if len(data)-offset < 11 {
			return false
		}
		size := uint64(binary.LittleEndian.Uint32(data[offset+5 : offset+9]))
		if size > uint64(len(data)-offset-11) {
			return false
		}
		name := binary.LittleEndian.Uint16(data[offset+1 : offset+3])
		if name == 0x9003 {
			payload := data[offset+9 : offset+9+int(size)]
			if len(payload) < 4 || uint64(binary.LittleEndian.Uint32(payload[:4])) > uint64((len(payload)-4)/4) {
				return false
			}
		}
		offset += 11 + int(size)
	}
	return true
}

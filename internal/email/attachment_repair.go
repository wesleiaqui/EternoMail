package email

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"io"
	"mime"
	"strings"

	gomessage "github.com/emersion/go-message"
	"github.com/hkdb/aerion/internal/message"
)

// ParseAttachmentsForRepair is deliberately stricter than display parsing.
// A caller may replace cache metadata only when this returns without error.
// Unsupported signed/encrypted/TNEF mail is left untouched, never guessed.
// Content includes every attachment's decoded bytes for fingerprint matching.
func ParseAttachmentsForRepair(messageID string, raw []byte) ([]*AttachmentData, error) {
	if len(raw) == 0 || len(raw) > maxAttachmentBytes {
		return nil, fmt.Errorf("invalid repair MIME snapshot size")
	}
	entity, err := gomessage.Read(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("parse repair MIME: %w", err)
	}
	var result []*AttachmentData
	totalBytes := 0
	totalParts := 0
	var walk func(*gomessage.Entity, int) error
	walk = func(part *gomessage.Entity, depth int) error {
		totalParts++
		if totalParts > 10000 {
			return fmt.Errorf("repair part count exceeds limit")
		}
		if depth >= 64 {
			return fmt.Errorf("repair MIME nesting exceeds limit")
		}
		ct, params, err := mime.ParseMediaType(part.Header.Get("Content-Type"))
		if err != nil {
			return fmt.Errorf("repair requires valid content type: %w", err)
		}
		ct = strings.ToLower(ct)
		if strings.Contains(ct, "pkcs7") || strings.Contains(ct, "pgp") || ct == "multipart/signed" || ct == "multipart/encrypted" || ct == "application/ms-tnef" || ct == "message/rfc822" {
			return fmt.Errorf("unsupported repair MIME type %s", ct)
		}
		disp, dp := "", map[string]string{}
		if value := part.Header.Get("Content-Disposition"); value != "" {
			disp, dp, err = mime.ParseMediaType(value)
			if err != nil {
				return fmt.Errorf("invalid repair disposition: %w", err)
			}
		}
		if strings.HasPrefix(ct, "multipart/") && (disp == "attachment" || disp == "inline" || dp["filename"] != "" || params["name"] != "" || part.Header.Get("Content-ID") != "") {
			return fmt.Errorf("attached multipart repair is unsupported")
		}
		if strings.HasPrefix(ct, "multipart/") {
			mr := part.MultipartReader()
			if mr == nil {
				return fmt.Errorf("missing multipart boundary")
			}
			for {
				child, err := mr.NextPart()
				if err == io.EOF {
					return nil
				}
				if err != nil {
					return fmt.Errorf("incomplete repair multipart: %w", err)
				}
				if err = walk(child, depth+1); err != nil {
					return err
				}
			}
		}
		content, err := io.ReadAll(io.LimitReader(part.Body, int64(maxAttachmentBytes)+1))
		if err != nil {
			return fmt.Errorf("incomplete repair part: %w", err)
		}
		totalBytes += len(content)
		if totalBytes > maxAttachmentBytes {
			return fmt.Errorf("repair part exceeds limit")
		}
		cid := strings.Trim(part.Header.Get("Content-ID"), "<>")
		// Match ordinary MIME attachment classification, without folding parts by
		// filename, CID, size or payload. Every occurrence is a separate part.
		if disp != "attachment" && disp != "inline" && dp["filename"] == "" && params["name"] == "" && cid == "" && strings.HasPrefix(ct, "text/") {
			return nil
		}
		if disp != "attachment" && disp != "inline" && cid == "" && ct == "" {
			return nil
		}
		// go-message converts text bodies to UTF-8. Never install converted
		// bytes as an exact attachment payload. Unsupported charsets leave the
		// whole existing cache untouched instead.
		charset := strings.ToLower(params["charset"])
		if strings.HasPrefix(ct, "text/") && charset != "" && charset != "utf-8" && charset != "us-ascii" {
			return fmt.Errorf("repair cannot preserve text attachment charset %s", charset)
		}
		filename := dp["filename"]
		if filename == "" {
			filename = params["name"]
		}
		if filename == "" {
			filename = "attachment.bin"
		}
		if strings.EqualFold(filename, "winmail.dat") {
			return fmt.Errorf("TNEF repair is unsupported")
		}
		filename, err = decodeRFC2047(filename)
		if err != nil {
			return fmt.Errorf("invalid repair filename: %w", err)
		}
		att := &message.Attachment{MessageID: messageID, Filename: filename, ContentType: ct, Size: len(content), ContentID: cid, IsInline: disp == "inline" || cid != ""}
		result = append(result, &AttachmentData{Attachment: att, Content: content})
		return nil
	}
	if err = walk(entity, 0); err != nil {
		return nil, err
	}
	metadata := make([]*message.Attachment, len(result))
	for i, part := range result {
		metadata[i] = part.Attachment
	}
	message.AssignSnapshotAttachmentIDs(metadata, messageID, raw, "strict-repair-v1")
	return result, nil
}

// AttachmentContentDigest binds a cache payload to the exact MIME occurrence.
func AttachmentContentDigest(content []byte) string { return fmt.Sprintf("%x", sha256.Sum256(content)) }

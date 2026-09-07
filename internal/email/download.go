// Package email provides email content processing utilities
package email

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"os"
	"path/filepath"
	"strings"

	gomessage "github.com/emersion/go-message"
	msgcharset "github.com/emersion/go-message/charset"
	"github.com/hkdb/aerion/internal/message"
	"golang.org/x/text/encoding/htmlindex"
)

// AttachmentDownloader handles downloading and saving attachments
type AttachmentDownloader struct {
	attachmentsDir string
}

// NewAttachmentDownloader creates a new attachment downloader
func NewAttachmentDownloader(attachmentsDir string) *AttachmentDownloader {
	return &AttachmentDownloader{
		attachmentsDir: attachmentsDir,
	}
}

// ExtractAttachmentContent extracts the content of a specific attachment from raw email bytes
func (d *AttachmentDownloader) ExtractAttachmentContent(raw []byte, targetFilename string) ([]byte, error) {
	if len(raw) > maxAttachmentBytes {
		return nil, fmt.Errorf("message exceeds attachment extraction limit")
	}
	reader := bytes.NewReader(raw)

	entity, err := gomessage.Read(reader)
	if err != nil {
		return nil, fmt.Errorf("failed to parse message: %w", err)
	}

	// We need to find the attachment by matching properties
	if mr := entity.MultipartReader(); mr != nil {
		return d.findAttachmentInMultipart(mr, targetFilename)
	}

	// Single-part message: the whole entity may itself be the attachment.
	if getFilename(entity) == targetFilename {
		content, err := readAttachmentContent(entity.Body)
		if err != nil {
			return nil, err
		}
		return content, nil // go-message already decoded Content-Transfer-Encoding
	}

	return nil, fmt.Errorf("attachment not found: %s", targetFilename)
}

// InlineAttachmentResult holds content-id to data URL mapping
type InlineAttachmentResult struct {
	ContentID   string
	ContentType string
	Content     []byte
}

// ExtractInlineAttachments extracts all inline attachments from raw email bytes
// Returns a map of content-id to base64 data URL
func (d *AttachmentDownloader) ExtractInlineAttachments(raw []byte) (map[string]string, error) {
	if len(raw) > maxAttachmentBytes {
		return nil, fmt.Errorf("message exceeds attachment extraction limit")
	}
	reader := bytes.NewReader(raw)

	entity, err := gomessage.Read(reader)
	if err != nil {
		return nil, fmt.Errorf("failed to parse message: %w", err)
	}

	result := make(map[string]string)

	if mr := entity.MultipartReader(); mr != nil {
		d.findInlineAttachmentsInMultipart(mr, result)
	}

	return result, nil
}

// findInlineAttachmentsInMultipart searches for inline attachments and builds data URLs
func (d *AttachmentDownloader) findInlineAttachmentsInMultipart(mr gomessage.MultipartReader, result map[string]string) {
	d.findInlineAttachmentsInMultipartDepth(mr, result, 0)
}

func (d *AttachmentDownloader) findInlineAttachmentsInMultipartDepth(mr gomessage.MultipartReader, result map[string]string, depth int) {
	if depth >= 64 {
		return
	}
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

		// Handle nested multipart
		if nestedMr := part.MultipartReader(); nestedMr != nil {
			d.findInlineAttachmentsInMultipartDepth(nestedMr, result, depth+1)
			continue
		}

		// Check for Content-ID header (indicates inline attachment)
		contentID := strings.Trim(part.Header.Get("Content-ID"), "<>")
		if contentID == "" {
			continue
		}

		// Get content type
		contentType, _, _ := mime.ParseMediaType(part.Header.Get("Content-Type"))
		if contentType == "" {
			contentType = "application/octet-stream"
		}

		// Read content
		content, err := readAttachmentContent(part.Body)
		if err != nil {
			continue
		}

		// The MIME reader has already applied transfer decoding.
		decodedContent := content // go-message already decoded Content-Transfer-Encoding

		// Build data URL
		dataURL := buildDataURL(contentType, decodedContent)
		result[contentID] = dataURL
	}
}

// buildDataURL creates a data URL from content type and binary content
func buildDataURL(contentType string, content []byte) string {
	encoded := base64.StdEncoding.EncodeToString(content)
	return fmt.Sprintf("data:%s;base64,%s", contentType, encoded)
}

// findAttachmentInMultipart searches for an attachment by filename in a multipart message
func (d *AttachmentDownloader) findAttachmentInMultipart(mr gomessage.MultipartReader, targetFilename string) ([]byte, error) {
	return d.findAttachmentInMultipartDepth(mr, targetFilename, 0)
}

func (d *AttachmentDownloader) findAttachmentInMultipartDepth(mr gomessage.MultipartReader, targetFilename string, depth int) ([]byte, error) {
	if depth >= 64 {
		return nil, fmt.Errorf("MIME nesting limit exceeded")
	}
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

		// Handle nested multipart
		if nestedMr := part.MultipartReader(); nestedMr != nil {
			if content, err := d.findAttachmentInMultipartDepth(nestedMr, targetFilename, depth+1); err == nil {
				return content, nil
			}
			continue
		}

		// TNEF (winmail.dat): the requested file may live INSIDE the container.
		// The sync extractor stored the inner TNEF Title as the filename, so match
		// the target against the decoded inner attachments. part.Body is already
		// transfer-decoded by go-message, so it feeds tnef.Decode directly.
		contentType, _, _ := mime.ParseMediaType(part.Header.Get("Content-Type"))
		disposition, dispParams, _ := mime.ParseMediaType(part.Header.Get("Content-Disposition"))
		if contentType == "application/ms-tnef" ||
			(disposition == "attachment" && strings.EqualFold(dispParams["filename"], "winmail.dat")) {
			raw, err := readAttachmentContent(part.Body)
			if err != nil {
				continue
			}
			for _, inner := range DecodeTNEFAttachments(raw) {
				if inner.Filename == targetFilename {
					return inner.Content, nil
				}
			}
			// Decode failed or no inner match: only the container itself can satisfy
			// a request for "winmail.dat"; otherwise move on.
			if strings.EqualFold(targetFilename, "winmail.dat") {
				return raw, nil
			}
			continue
		}

		// Check filename
		filename := getFilename(part)
		if filename == targetFilename {
			content, err := readAttachmentContent(part.Body)
			if err != nil {
				return nil, err
			}

			// The MIME reader has already applied transfer decoding.
			return content, nil // go-message already decoded Content-Transfer-Encoding
		}
	}

	return nil, fmt.Errorf("attachment not found: %s", targetFilename)
}

// decodeMIMEFilename decodes a MIME-encoded filename with full charset support.
// Mirrors the sync code's decodeMIMEWord() to ensure filenames match between
// sync (when stored to DB) and download (when extracting from raw message).
func decodeMIMEFilename(s string) string {
	if s == "" {
		return s
	}
	dec := &mime.WordDecoder{
		CharsetReader: func(charsetName string, r io.Reader) (io.Reader, error) {
			if reader, err := msgcharset.Reader(charsetName, r); err == nil {
				return reader, nil
			}
			enc, err := htmlindex.Get(charsetName)
			if err != nil {
				return nil, fmt.Errorf("unknown charset: %s", charsetName)
			}
			return enc.NewDecoder().Reader(r), nil
		},
	}
	decoded, err := dec.DecodeHeader(s)
	if err != nil {
		return s
	}
	return decoded
}

// getFilename extracts the filename from a message part
func getFilename(part *gomessage.Entity) string {
	// Try Content-Disposition first
	if disp := part.Header.Get("Content-Disposition"); disp != "" {
		_, params, _ := mime.ParseMediaType(disp)
		if filename := params["filename"]; filename != "" {
			return decodeMIMEFilename(filename)
		}
	}

	// Try Content-Type name parameter
	if ct := part.Header.Get("Content-Type"); ct != "" {
		_, params, _ := mime.ParseMediaType(ct)
		if name := params["name"]; name != "" {
			return decodeMIMEFilename(name)
		}
	}

	// Synthetic fallback: match sync/parse.go extractAttachmentMetadata logic
	contentType := "application/octet-stream"
	if ct := part.Header.Get("Content-Type"); ct != "" {
		mt, _, _ := mime.ParseMediaType(ct)
		if mt != "" {
			contentType = mt
		}
	}

	ext := ".bin"
	if strings.HasPrefix(contentType, "image/") {
		parts := strings.SplitN(contentType, "/", 2)
		if len(parts) == 2 {
			ext = "." + parts[1]
		}
	}
	return "attachment" + ext
}

// sanitizeFilename removes directory components and NUL bytes from a filename.
// Both separator styles are handled because attachments may come from any OS.
func sanitizeFilename(name string) string {
	name = strings.ReplaceAll(name, "\x00", "")
	name = strings.ReplaceAll(name, "\\", "/")
	name = filepath.Base(filepath.Clean(name))
	if name == "" || name == "." || name == ".." || name == string(filepath.Separator) {
		return "attachment"
	}
	return name
}

// SaveAttachmentToDirectory saves an attachment inside a user-selected directory.
// Sanitize before joining: filepath.Join would erase evidence of traversal.
func (d *AttachmentDownloader) SaveAttachmentToDirectory(att *message.Attachment, content []byte, directory string) (string, error) {
	return d.SaveAttachment(att, content, filepath.Join(directory, sanitizeFilename(att.Filename)))
}

// SaveAttachment saves attachment content to disk. Custom paths are destinations
// selected by the user; automatic downloads stay inside attachmentsDir.
func (d *AttachmentDownloader) SaveAttachment(att *message.Attachment, content []byte, customPath string) (string, error) {
	var root *os.Root
	var relativePath, savePath string
	var err error
	flags := os.O_WRONLY | os.O_CREATE | os.O_TRUNC

	if customPath != "" {
		// Reject traversal before Clean removes it. Absolute Save As destinations
		// (including document portal paths) are intentionally supported.
		for _, part := range strings.Split(strings.ReplaceAll(customPath, "\\", "/"), "/") {
			if part == ".." {
				return "", fmt.Errorf("invalid save path: %q contains parent traversal", customPath)
			}
		}
		if strings.ContainsRune(customPath, '\x00') || filepath.Base(customPath) == "." ||
			strings.HasSuffix(customPath, string(filepath.Separator)) {
			return "", fmt.Errorf("invalid save path: %q", customPath)
		}
		savePath = filepath.Clean(customPath)
		relativePath = filepath.Base(savePath)
		root, err = os.OpenRoot(filepath.Dir(savePath))
	} else {
		if err := os.MkdirAll(d.attachmentsDir, 0700); err != nil {
			return "", fmt.Errorf("failed to create attachment directory: %w", err)
		}
		root, err = os.OpenRoot(d.attachmentsDir)
		if err != nil {
			return "", fmt.Errorf("failed to open attachment directory: %w", err)
		}
		// Encrypted attachments may have an empty or short message ID.
		subDir := sanitizeFilename(att.MessageID[:min(8, len(att.MessageID))])
		if err := root.MkdirAll(subDir, 0700); err != nil {
			root.Close()
			return "", fmt.Errorf("failed to create attachment directory: %w", err)
		}
		relativePath = filepath.Join(subDir, sanitizeFilename(att.Filename))
		savePath = filepath.Join(d.attachmentsDir, relativePath)
		// Reserve names atomically, also avoiding existing symlinks.
		flags = os.O_WRONLY | os.O_CREATE | os.O_EXCL
	}
	if err != nil {
		return "", fmt.Errorf("failed to open attachment directory: %w", err)
	}
	defer root.Close()

	originalPath := relativePath
	ext := filepath.Ext(originalPath)
	base := strings.TrimSuffix(originalPath, ext)
	var file *os.File
	for i := 1; ; i++ {
		// Root confines writes even when a path component is a symlink.
		file, err = root.OpenFile(relativePath, flags, 0600)
		if customPath == "" && os.IsExist(err) {
			relativePath = fmt.Sprintf("%s%d%s", base, i, ext)
			savePath = filepath.Join(d.attachmentsDir, relativePath)
			continue
		}
		if err != nil {
			return "", fmt.Errorf("failed to write attachment: %w", err)
		}
		break
	}
	_, writeErr := file.Write(content)
	closeErr := file.Close()
	if writeErr != nil {
		return "", fmt.Errorf("failed to write attachment: %w", writeErr)
	}
	if closeErr != nil {
		return "", fmt.Errorf("failed to close attachment: %w", closeErr)
	}
	return savePath, nil
}

package app

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"net/mail"
	"strings"

	"github.com/hkdb/aerion/internal/email"
	"github.com/hkdb/aerion/internal/message"
)

// RepairMessageAttachments explicitly re-fetches one local IMAP message. It
// archives old rows and exact bytes before replacing only its attachment cache.
// The returned backup ID is required by RestoreMessageAttachmentRepair.
// Remote service repair is not exposed over the network.
func (a *App) RepairMessageAttachments(accountID, messageID string) (string, error) {
	if a.remoteServiceMode || a.messageStore == nil || a.attachmentStore == nil || a.syncEngine == nil {
		return "", fmt.Errorf("repair must run on the mailbox-owning local app")
	}
	msg, err := a.messageStore.Get(messageID)
	if err != nil {
		return "", err
	}
	if msg == nil || msg.AccountID != accountID {
		return "", fmt.Errorf("message account mismatch")
	}
	if id, err := a.attachmentStore.RepairBackupID(messageID); err != nil || id != "" {
		return id, err
	}
	raw, err := a.syncEngine.FetchRawMessageForRepair(a.ctx, accountID, msg.FolderID, msg.UID)
	if err != nil {
		return "", err
	}
	parsed, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		return "", err
	}
	if msg.MessageID == "" || strings.Trim(parsed.Header.Get("Message-ID"), " <>\r\n\t") != strings.Trim(msg.MessageID, " <>\r\n\t") {
		return "", fmt.Errorf("original MIME Message-ID not verified")
	}
	extracted, err := email.ParseAttachmentsForRepair(messageID, raw)
	if err != nil {
		return "", err
	}
	parts := make([]message.RepairPart, 0, len(extracted))
	for _, part := range extracted {
		parts = append(parts, message.RepairPart{Metadata: *part.Attachment, Payload: part.Content})
	}
	digest := fmt.Sprintf("%x", sha256.Sum256(raw))
	return a.attachmentStore.RepairFromMIME(accountID, messageID, digest, parts, a.paths.AttachmentsPath(), message.RepairSource{FolderID: msg.FolderID, UID: msg.UID, MessageID: msg.MessageID})
}

// RestoreMessageAttachmentRepair restores the specified local repair backup after ownership and generation checks.
func (a *App) RestoreMessageAttachmentRepair(accountID, messageID, backupID string) error {
	if a.remoteServiceMode || a.attachmentStore == nil {
		return fmt.Errorf("restore must run on mailbox-owning local app")
	}
	return a.attachmentStore.RestoreRepair(accountID, messageID, backupID)
}

// Exact repaired payloads must never use the legacy filename-only resolver.
func (a *App) storedAttachmentContent(downloader *email.AttachmentDownloader, raw []byte, att *message.Attachment) ([]byte, error) {
	if a.attachmentStore != nil {
		content, ok, err := a.attachmentStore.RepairedContent(att.ID)
		if err != nil {
			return nil, err
		}
		if ok {
			return content, nil
		}
	}
	return downloader.ExtractAttachmentContent(raw, att.Filename)
}

// This is only a trigger to request an authoritative parse, never a dedupe key.
// Real repeated filenames stay separate in the strict parse and replacement.
func hasRepeatedAttachmentMetadata(items []*message.Attachment) bool {
	type key struct {
		name, typ, cid string
		size           int
		inline         bool
	}
	seen := map[key]bool{}
	for _, a := range items {
		if a == nil {
			continue
		}
		k := key{a.Filename, a.ContentType, a.ContentID, a.Size, a.IsInline}
		if seen[k] {
			return true
		}
		seen[k] = true
	}
	return false
}

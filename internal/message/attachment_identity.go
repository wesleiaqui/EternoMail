package message

import (
	"crypto/sha256"
	"fmt"
)

// AssignSnapshotAttachmentIDs makes re-parsing the exact same MIME snapshot
// idempotent without guessing that matching filenames/sizes are the same part.
// The parser route is included because the two extractors may classify parts
// differently. Each ordinal stays distinct, even for byte-identical MIME parts.
// A different or truncated snapshot gets separate IDs, never replacing an
// attachment from an earlier complete parse. This does not repair legacy rows.
func AssignSnapshotAttachmentIDs(attachments []*Attachment, messageID string, raw []byte, parser string) {
	snapshot := sha256.Sum256(raw)
	for ordinal, attachment := range attachments {
		key := fmt.Sprintf("mime-cache-v1:%d:%s:%x:%s:%d", len(messageID), messageID, snapshot, parser, ordinal)
		id := sha256.Sum256([]byte(key))
		attachment.ID = fmt.Sprintf("mime-%x", id)
	}
}

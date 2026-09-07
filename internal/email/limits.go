package email

import (
	"fmt"
	"io"
)

// Matches the existing raw message limit used by the sync engine.
const maxAttachmentBytes = 50 * 1024 * 1024

func readAttachmentContent(r io.Reader) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, maxAttachmentBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxAttachmentBytes {
		return nil, fmt.Errorf("attachment exceeds message size limit")
	}
	return data, nil
}

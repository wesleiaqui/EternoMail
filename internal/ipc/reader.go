package ipc

import (
	"encoding/json"
	"fmt"
	"io"
)

const maxIPCMessageSize = 10 * 1024 * 1024

// messageReader limits each JSON message and preserves decoder read-ahead
// across authentication and normal traffic, including pipelined messages.
type messageReader struct{ reader io.Reader }

func (r *messageReader) Decode(msg *Message) error {
	decoder := json.NewDecoder(io.LimitReader(r.reader, maxIPCMessageSize+1))
	if err := decoder.Decode(msg); err != nil {
		return err
	}
	if decoder.InputOffset() > maxIPCMessageSize {
		return fmt.Errorf("IPC message exceeds 10 MB")
	}
	r.reader = io.MultiReader(decoder.Buffered(), r.reader)
	return nil
}

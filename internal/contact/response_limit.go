package contact

import (
	"errors"
	"io"
)

const (
	// Contact APIs return at most one page per request. Ten MiB accommodates a
	// full page of rich contacts while keeping one anomalous page bounded.
	maxContactPageResponseBytes int64 = 10 << 20
	// Error bodies are only used to classify and report a request failure.
	maxContactErrorResponseBytes int64 = 64 << 10
)

var errContactResponseTooLarge = errors.New("contact API response exceeds size limit")

func readContactResponseBody(r io.Reader, limit int64) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > limit {
		return nil, errContactResponseTooLarge
	}
	return body, nil
}

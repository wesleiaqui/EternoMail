package davutil

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"testing"
)

func TestXMLFixTransportResponseLimits(t *testing.T) {
	smallXML := []byte(`<D:getetag>abc</D:getetag>`)
	tooLarge := bytes.Repeat([]byte("x"), maxXMLResponseBytes+1)
	exactLimit := bytes.Repeat([]byte("x"), maxXMLResponseBytes)

	tests := []struct {
		name          string
		body          []byte
		contentType   string
		contentLength int64
		chunked       bool
		wantBody      []byte
		wantTooLarge  bool
		wantClosed    bool
	}{
		{
			name:          "small XML is normalized",
			body:          smallXML,
			contentType:   "application/xml",
			contentLength: int64(len(smallXML)),
			wantBody:      []byte(`<D:getetag>"abc"</D:getetag>`),
			wantClosed:    true,
		},
		{
			name:          "XML exactly at limit is accepted",
			body:          exactLimit,
			contentType:   "application/xml",
			contentLength: maxXMLResponseBytes,
			wantBody:      exactLimit,
			wantClosed:    true,
		},
		{
			name:          "XML without Content-Length over limit is rejected",
			body:          tooLarge,
			contentType:   "application/xml",
			contentLength: -1,
			wantTooLarge:  true,
			wantClosed:    true,
		},
		{
			name:          "chunked XML over limit is rejected",
			body:          tooLarge,
			contentType:   "application/xml",
			contentLength: -1,
			chunked:       true,
			wantTooLarge:  true,
			wantClosed:    true,
		},
		{
			name:          "declared Content-Length over limit is rejected before reading",
			body:          []byte("ignored"),
			contentType:   "application/xml",
			contentLength: maxXMLResponseBytes + 1,
			wantTooLarge:  true,
			wantClosed:    true,
		},
		{
			name:          "false smaller Content-Length cannot bypass limit",
			body:          tooLarge,
			contentType:   "application/xml",
			contentLength: 1,
			wantTooLarge:  true,
			wantClosed:    true,
		},
		{
			name:          "empty XML remains valid",
			body:          nil,
			contentType:   "application/xml",
			contentLength: 0,
			wantBody:      nil,
			wantClosed:    true,
		},
		{
			name:          "non XML is unchanged",
			body:          smallXML,
			contentType:   "text/plain",
			contentLength: int64(len(smallXML)),
			wantBody:      smallXML,
			wantClosed:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := &trackingReadCloser{Reader: bytes.NewReader(tt.body)}
			response := &http.Response{
				StatusCode:    http.StatusMultiStatus,
				Header:        make(http.Header),
				Body:          body,
				ContentLength: tt.contentLength,
			}
			response.Header.Set("Content-Type", tt.contentType)
			if tt.chunked {
				response.TransferEncoding = []string{"chunked"}
			}
			transport := NewXMLFixTransport(roundTripperFunc(func(*http.Request) (*http.Response, error) {
				return response, nil
			}))

			resp, err := transport.RoundTrip(testRequest(t))
			if tt.wantTooLarge {
				if !errors.Is(err, errXMLResponseTooLarge) {
					t.Fatalf("RoundTrip error = %v, want errXMLResponseTooLarge", err)
				}
				if resp != nil {
					t.Fatal("RoundTrip returned a response for an oversized XML body")
				}
			} else {
				if err != nil {
					t.Fatalf("RoundTrip: %v", err)
				}
				got, readErr := io.ReadAll(resp.Body)
				if readErr != nil {
					t.Fatal(readErr)
				}
				if !bytes.Equal(got, tt.wantBody) {
					t.Fatalf("body = %q, want %q", got, tt.wantBody)
				}
				if body.closed != tt.wantClosed {
					t.Fatalf("source body closed = %v, want %v", body.closed, tt.wantClosed)
				}
				if err := resp.Body.Close(); err != nil {
					t.Fatal(err)
				}
			}
			if tt.wantTooLarge && body.closed != tt.wantClosed {
				t.Fatalf("source body closed = %v, want %v", body.closed, tt.wantClosed)
			}
		})
	}
}

type trackingReadCloser struct {
	io.Reader
	closed bool
}

func (r *trackingReadCloser) Close() error {
	r.closed = true
	return nil
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func testRequest(t *testing.T) *http.Request {
	t.Helper()
	req, err := http.NewRequest("PROPFIND", "https://dav.example.test/", nil)
	if err != nil {
		t.Fatal(err)
	}
	return req
}

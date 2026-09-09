package contact

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

type trackedContactBody struct {
	io.Reader
	closed bool
}

func (b *trackedContactBody) Close() error {
	b.closed = true
	return nil
}

type contactResponseTransport struct {
	responses []*http.Response
	requests  int
}

func (t *contactResponseTransport) RoundTrip(*http.Request) (*http.Response, error) {
	resp := t.responses[t.requests]
	t.requests++
	return resp, nil
}

func contactResponse(status int, body *trackedContactBody, contentLength int64, chunked bool) *http.Response {
	resp := &http.Response{StatusCode: status, Body: body, ContentLength: contentLength, Header: make(http.Header)}
	if chunked {
		resp.TransferEncoding = []string{"chunked"}
	}
	return resp
}

func googlePageAtSize(size int64, pageToken string) string {
	prefix := `{"connections":[],"nextPageToken":"` + pageToken + `","nextSyncToken":"sync","padding":"`
	suffix := `"}`
	return prefix + strings.Repeat("x", int(size)-len(prefix)-len(suffix)) + suffix
}

func TestGoogleContactsResponseLimitAcceptsExactSize(t *testing.T) {
	body := &trackedContactBody{Reader: strings.NewReader(googlePageAtSize(maxContactPageResponseBytes, ""))}
	s := NewGoogleContactsSyncer()
	s.httpClient = &http.Client{Transport: &contactResponseTransport{responses: []*http.Response{contactResponse(http.StatusOK, body, -1, true)}}}

	result, err := s.SyncContactsDelta("token", "")
	if err != nil {
		t.Fatalf("SyncContactsDelta: %v", err)
	}
	if result.NextSyncToken != "sync" || !body.closed {
		t.Fatalf("result=%+v closed=%v", result, body.closed)
	}
}

func TestGoogleContactsResponseLimitRejectsOversizeChunked(t *testing.T) {
	body := &trackedContactBody{Reader: strings.NewReader(strings.Repeat("x", int(maxContactPageResponseBytes+1)))}
	s := NewGoogleContactsSyncer()
	s.httpClient = &http.Client{Transport: &contactResponseTransport{responses: []*http.Response{contactResponse(http.StatusOK, body, -1, true)}}}

	_, err := s.SyncContactsDelta("token", "")
	if !errors.Is(err, errContactResponseTooLarge) {
		t.Fatalf("error = %v, want oversized contact response", err)
	}
	if !body.closed {
		t.Fatal("response body was not closed")
	}
}

func TestGoogleContactsPaginationUsesLimitPerPage(t *testing.T) {
	first := &trackedContactBody{Reader: strings.NewReader(`{"connections":[],"nextPageToken":"page2"}`)}
	second := &trackedContactBody{Reader: strings.NewReader(`{"connections":[],"nextSyncToken":"sync"}`)}
	transport := &contactResponseTransport{responses: []*http.Response{
		contactResponse(http.StatusOK, first, -1, true),
		contactResponse(http.StatusOK, second, 1, false),
	}}
	s := NewGoogleContactsSyncer()
	s.httpClient = &http.Client{Transport: transport}

	result, err := s.SyncContactsDelta("token", "")
	if err != nil {
		t.Fatalf("SyncContactsDelta: %v", err)
	}
	if transport.requests != 2 || result.NextSyncToken != "sync" || !first.closed || !second.closed {
		t.Fatalf("requests=%d result=%+v firstClosed=%v secondClosed=%v", transport.requests, result, first.closed, second.closed)
	}
}

func TestMicrosoftContactsPaginationUsesLimitPerPage(t *testing.T) {
	first := &trackedContactBody{Reader: strings.NewReader(`{"value":[],"@odata.nextLink":"https://example.test/page2"}`)}
	second := &trackedContactBody{Reader: strings.NewReader(`{"value":[],"@odata.deltaLink":"delta"}`)}
	transport := &contactResponseTransport{responses: []*http.Response{
		contactResponse(http.StatusOK, first, 1, false),
		contactResponse(http.StatusOK, second, -1, true),
	}}
	s := NewMicrosoftContactsSyncer()
	s.httpClient = &http.Client{Transport: transport}

	result, err := s.SyncContactsDelta("token", "")
	if err != nil {
		t.Fatalf("SyncContactsDelta: %v", err)
	}
	if transport.requests != 2 || result.NextSyncToken != "delta" || !first.closed || !second.closed {
		t.Fatalf("requests=%d result=%+v firstClosed=%v secondClosed=%v", transport.requests, result, first.closed, second.closed)
	}
}

func TestMicrosoftContactsErrorResponseLimitRejectsOversize(t *testing.T) {
	body := &trackedContactBody{Reader: strings.NewReader(strings.Repeat("x", int(maxContactErrorResponseBytes+1)))}
	s := NewMicrosoftContactsSyncer()
	s.httpClient = &http.Client{Transport: &contactResponseTransport{responses: []*http.Response{contactResponse(http.StatusBadRequest, body, 1, false)}}}

	_, err := s.SyncContactsDelta("token", "")
	if !errors.Is(err, errContactResponseTooLarge) {
		t.Fatalf("error = %v, want oversized contact response", err)
	}
	if !body.closed {
		t.Fatal("response body was not closed")
	}
}

package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	v1 "github.com/hkdb/aerion/internal/automation/v1"
	"github.com/hkdb/aerion/internal/views"
)

// Client interacts with Postilaatikko's local automation API.
type Client struct {
	endpoint   string
	token      string
	httpClient *http.Client
}

// Config configures a Client instance.
type Config struct {
	Endpoint string
	Token    string
	Timeout  time.Duration
}

// New creates an automation Client.
func New(cfg Config) *Client {
	endpoint := strings.TrimRight(cfg.Endpoint, "/")
	if endpoint == "" {
		endpoint = "http://127.0.0.1:8080"
	}
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	return &Client{
		endpoint: endpoint,
		token:    cfg.Token,
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}
}

func (c *Client) doRequest(ctx context.Context, method, path string, body any, idempotencyKey string) ([]byte, error) {
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal request: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	url := c.endpoint + path
	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.token)
	if bodyReader != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if idempotencyKey != "" {
		req.Header.Set("Idempotency-Key", idempotencyKey)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("execute request: %w", err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var errResp v1.ErrorResponse
		if jsonErr := json.Unmarshal(respBytes, &errResp); jsonErr == nil && errResp.Error.Code != "" {
			return nil, fmt.Errorf("api error (%s): %s", errResp.Error.Code, errResp.Error.Message)
		}
		return nil, fmt.Errorf("api error (http %d): %s", resp.StatusCode, string(respBytes))
	}

	return respBytes, nil
}

// GetCapabilities returns authorized capabilities for this token.
func (c *Client) GetCapabilities(ctx context.Context) (*v1.Capabilities, error) {
	bytes, err := c.doRequest(ctx, http.MethodGet, "/v1/capabilities", nil, "")
	if err != nil {
		return nil, err
	}
	var resp v1.Response[v1.Capabilities]
	if err := json.Unmarshal(bytes, &resp); err != nil {
		return nil, err
	}
	return &resp.Data, nil
}

// GetAccounts lists configured accounts.
func (c *Client) GetAccounts(ctx context.Context) ([]v1.AccountSummary, error) {
	bytes, err := c.doRequest(ctx, http.MethodGet, "/v1/accounts", nil, "")
	if err != nil {
		return nil, err
	}
	var resp v1.Response[[]v1.AccountSummary]
	if err := json.Unmarshal(bytes, &resp); err != nil {
		return nil, err
	}
	return resp.Data, nil
}

// GetFolders lists folders, optionally filtered by account.
func (c *Client) GetFolders(ctx context.Context, accountID string) ([]v1.FolderSummary, error) {
	path := "/v1/folders"
	if accountID != "" {
		path += "?accountId=" + accountID
	}
	bytes, err := c.doRequest(ctx, http.MethodGet, path, nil, "")
	if err != nil {
		return nil, err
	}
	var resp v1.Response[[]v1.FolderSummary]
	if err := json.Unmarshal(bytes, &resp); err != nil {
		return nil, err
	}
	return resp.Data, nil
}

// GetViews lists saved views/virtual folders.
func (c *Client) GetViews(ctx context.Context) ([]v1.ViewSummary, error) {
	bytes, err := c.doRequest(ctx, http.MethodGet, "/v1/views", nil, "")
	if err != nil {
		return nil, err
	}
	var resp v1.Response[[]v1.ViewSummary]
	if err := json.Unmarshal(bytes, &resp); err != nil {
		return nil, err
	}
	return resp.Data, nil
}

// Search queries messages across a given view scope.
func (c *Client) Search(ctx context.Context, req v1.SearchRequest) (*views.ThreadPage, error) {
	bytes, err := c.doRequest(ctx, http.MethodPost, "/v1/search", req, "")
	if err != nil {
		return nil, err
	}
	var resp v1.Response[views.ThreadPage]
	if err := json.Unmarshal(bytes, &resp); err != nil {
		return nil, err
	}
	return &resp.Data, nil
}

// ReadThread retrieves thread messages and metadata.
func (c *Client) ReadThread(ctx context.Context, req v1.ThreadReadRequest) (*views.ThreadView, error) {
	bytes, err := c.doRequest(ctx, http.MethodPost, "/v1/threads/read", req, "")
	if err != nil {
		return nil, err
	}
	var resp v1.Response[views.ThreadView]
	if err := json.Unmarshal(bytes, &resp); err != nil {
		return nil, err
	}
	return &resp.Data, nil
}

// ReadBody reads or explicitly fetches message body and extracted reply content.
func (c *Client) ReadBody(ctx context.Context, req v1.BodyReadRequest) (*v1.MessageBody, error) {
	bytes, err := c.doRequest(ctx, http.MethodPost, "/v1/messages/body", req, "")
	if err != nil {
		return nil, err
	}
	var resp v1.Response[v1.MessageBody]
	if err := json.Unmarshal(bytes, &resp); err != nil {
		return nil, err
	}
	return &resp.Data, nil
}

// MutateAction executes a mutation with an Idempotency-Key.
func (c *Client) MutateAction(ctx context.Context, req v1.ActionRequest, idempotencyKey string) (*v1.ActionResponse, error) {
	bytes, err := c.doRequest(ctx, http.MethodPost, "/v1/actions", req, idempotencyKey)
	if err != nil {
		return nil, err
	}
	var resp v1.Response[v1.ActionResponse]
	if err := json.Unmarshal(bytes, &resp); err != nil {
		return nil, err
	}
	return &resp.Data, nil
}

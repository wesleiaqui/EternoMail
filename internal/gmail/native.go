package gmail

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/mail"
	"net/url"
	"strconv"
	"strings"
	"time"
)

var metadataHeaders = []string{"Message-ID", "Subject", "From", "Date", "To", "Cc", "Bcc", "Reply-To", "In-Reply-To", "References", "Content-Type", "Disposition-Notification-To", "List-Id", "List-Unsubscribe", "List-Post", "Precedence", "Auto-Submitted", "Sender", "X-Auto-Response-Suppress"}

type wireMessage struct {
	ID           string   `json:"id"`
	ThreadID     string   `json:"threadId"`
	LabelIDs     []string `json:"labelIds"`
	Snippet      string   `json:"snippet"`
	InternalDate string   `json:"internalDate"`
	Size         int      `json:"sizeEstimate"`
	Payload      struct {
		Headers []struct {
			Name  string `json:"name"`
			Value string `json:"value"`
		} `json:"headers"`
	} `json:"payload"`
}

func (r wireMessage) metadata() *Message {
	m := &Message{Headers: map[string]string{}, ID: r.ID, ThreadID: r.ThreadID, LabelIDs: r.LabelIDs, Snippet: r.Snippet, Size: r.Size}
	if ms, err := strconv.ParseInt(r.InternalDate, 10, 64); err == nil {
		m.Date = time.UnixMilli(ms)
	}
	for _, h := range r.Payload.Headers {
		m.Headers[h.Name] = h.Value
		switch strings.ToLower(h.Name) {
		case "message-id":
			m.RFCMessageID = h.Value
		case "subject":
			m.Subject = h.Value
		case "from":
			m.From = h.Value
		case "to":
			m.To = h.Value
		case "cc":
			m.Cc = h.Value
		case "bcc":
			m.Bcc = h.Value
		case "reply-to":
			m.ReplyTo = h.Value
		case "in-reply-to":
			m.InReplyTo = h.Value
		case "references":
			m.References = h.Value
		}
	}
	return m
}

type Label struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Type           string `json:"type"`
	MessagesTotal  int    `json:"messagesTotal"`
	MessagesUnread int    `json:"messagesUnread"`
}

// CreateHiddenUserLabel creates a Gmail user label for app-owned metadata.
// It is hidden from Gmail's navigation so internal triage state does not add
// clutter to the user's folder list.
func (c *Client) CreateHiddenUserLabel(ctx context.Context, name string) (*Label, error) {
	var label Label
	err := c.do(ctx, http.MethodPost, "/labels", nil, map[string]string{
		"name": name, "labelListVisibility": "labelHide", "messageListVisibility": "hide",
	}, &label)
	if err != nil {
		return nil, err
	}
	if label.ID == "" {
		return nil, fmt.Errorf("gmail: label creation returned no ID")
	}
	return &label, nil
}

// ModifyThread changes labels on every message in a Gmail conversation.
func (c *Client) ModifyThread(ctx context.Context, threadID string, addLabels, removeLabels []string) error {
	body := map[string][]string{}
	if len(addLabels) > 0 {
		body["addLabelIds"] = addLabels
	}
	if len(removeLabels) > 0 {
		body["removeLabelIds"] = removeLabels
	}
	return c.do(ctx, http.MethodPost, "/threads/"+url.PathEscape(threadID)+"/modify", nil, body, nil)
}

func (c *Client) Labels(ctx context.Context) ([]Label, error) {
	var r struct {
		Labels []Label `json:"labels"`
	}
	err := c.do(ctx, http.MethodGet, "/labels", nil, nil, &r)
	return r.Labels, err
}
func (c *Client) Label(ctx context.Context, id string) (*Label, error) {
	var r Label
	err := c.do(ctx, http.MethodGet, "/labels/"+url.PathEscape(id), nil, nil, &r)
	return &r, err
}
func (c *Client) Raw(ctx context.Context, id string) ([]byte, error) {
	var r struct {
		Raw string `json:"raw"`
	}
	err := c.request(ctx, http.MethodGet, "/messages/"+url.PathEscape(id), url.Values{"format": {"raw"}}, nil, &r, true, 100<<20)
	if err != nil {
		return nil, err
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(r.Raw, "="))
	if err != nil {
		return nil, fmt.Errorf("gmail: invalid raw message: %w", err)
	}
	return raw, nil
}
func (c *Client) ThreadMetadata(ctx context.Context, id string, headers ...string) ([]Message, error) {
	if len(headers) == 0 {
		headers = metadataHeaders
	}
	q := url.Values{"format": {"metadata"}, "metadataHeaders": headers, "fields": {"messages(id,threadId,labelIds,snippet,internalDate,sizeEstimate,payload/headers)"}}
	var r struct {
		Messages []wireMessage `json:"messages"`
	}
	if err := c.do(ctx, http.MethodGet, "/threads/"+url.PathEscape(id), q, nil, &r); err != nil {
		return nil, err
	}
	ms := make([]Message, 0, len(r.Messages))
	for _, m := range r.Messages {
		ms = append(ms, *m.metadata())
	}
	return ms, nil
}

// Non-idempotent operations deliberately do not retry transport/5xx failures.
// An error may mean Gmail accepted the request; callers must not fall back to SMTP/IMAP.
func (c *Client) Send(ctx context.Context, raw []byte, recipients ...string) (string, error) {
	return c.SendInThread(ctx, "", raw, recipients...)
}

// SendInThread sends a message and, when threadID is set, adds it to that
// Gmail conversation. Gmail still requires matching RFC threading headers in
// the raw message; callers prepare those before invoking this method.
func (c *Client) SendInThread(ctx context.Context, threadID string, raw []byte, recipients ...string) (string, error) {
	prepared, err := WithEnvelopeRecipients(raw, recipients)
	if err != nil {
		return "", err
	}
	return c.writeRawInThread(ctx, "/messages/send", prepared, nil, threadID)
}

// ThreadIDForRFCMessageID resolves the provider thread that contains an RFC
// Message-ID. It is used when a reply draft no longer carries the provider ID.
func (c *Client) ThreadIDForRFCMessageID(ctx context.Context, rfcMessageID string) (string, error) {
	messageID, err := c.FindByRFCMessageID(ctx, rfcMessageID)
	if err != nil {
		return "", fmt.Errorf("find Gmail parent message: %w", err)
	}
	messages, err := c.Metadata(ctx, []string{messageID})
	if err != nil {
		return "", fmt.Errorf("load Gmail parent metadata: %w", err)
	}
	if len(messages) != 1 || messages[0].ThreadID == "" {
		return "", fmt.Errorf("Gmail parent message has no conversation ID")
	}
	return messages[0].ThreadID, nil
}

func (c *Client) Insert(ctx context.Context, raw []byte, labels []string) (string, error) {
	return c.writeRaw(ctx, "/messages", raw, labels)
}
func (c *Client) writeRaw(ctx context.Context, path string, raw []byte, labels []string) (string, error) {
	return c.writeRawInThread(ctx, path, raw, labels, "")
}

func (c *Client) writeRawInThread(ctx context.Context, path string, raw []byte, labels []string, threadID string) (string, error) {
	var r struct {
		ID string `json:"id"`
	}
	body := map[string]any{"raw": base64.RawURLEncoding.EncodeToString(raw)}
	if threadID != "" {
		body["threadId"] = threadID
	}
	if labels != nil {
		body["labelIds"] = labels
	}
	err := c.request(ctx, http.MethodPost, path, url.Values{"internalDateSource": {"dateHeader"}}, body, &r, false, maxResponseBytes)
	if err == nil && r.ID == "" {
		err = fmt.Errorf("gmail: missing accepted message id")
	}
	return r.ID, err
}
func (c *Client) Delete(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/messages/"+url.PathEscape(id), nil, nil, nil)
}
func (c *Client) SaveDraft(ctx context.Context, id string, raw []byte) (string, error) {
	path, method := "/drafts", http.MethodPost
	if id != "" {
		path += "/" + url.PathEscape(id)
		method = http.MethodPut
	}
	var r struct {
		ID string `json:"id"`
	}
	err := c.request(ctx, method, path, nil, map[string]any{"message": map[string]string{"raw": base64.RawURLEncoding.EncodeToString(raw)}}, &r, false, maxResponseBytes)
	return r.ID, err
}
func (c *Client) DeleteDraft(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/drafts/"+url.PathEscape(id), nil, nil, nil)
}

type HistoryPage struct {
	HistoryID     string `json:"historyId"`
	NextPageToken string `json:"nextPageToken"`
	History       []struct {
		Messages []struct {
			ID string `json:"id"`
		} `json:"messages"`
		Deleted []struct {
			Message struct {
				ID string `json:"id"`
			} `json:"message"`
		} `json:"messagesDeleted"`
	} `json:"history"`
}

func (c *Client) ProfileHistoryID(ctx context.Context) (string, error) {
	var r struct {
		HistoryID string `json:"historyId"`
	}
	err := c.do(ctx, http.MethodGet, "/profile", nil, nil, &r)
	return r.HistoryID, err
}
func (c *Client) History(ctx context.Context, start, page string) (*HistoryPage, error) {
	q := url.Values{"startHistoryId": {start}, "maxResults": {"100"}}
	if page != "" {
		q.Set("pageToken", page)
	}
	var r HistoryPage
	err := c.do(ctx, http.MethodGet, "/history", q, nil, &r)
	return &r, err
}

func (c *Client) DraftIDForMessage(ctx context.Context, messageID string) (string, error) {
	page := ""
	for count := 0; count < 100; count++ {
		q := url.Values{"maxResults": {"100"}}
		if page != "" {
			q.Set("pageToken", page)
		}
		var r struct {
			Drafts []struct {
				ID      string `json:"id"`
				Message struct {
					ID string `json:"id"`
				} `json:"message"`
			} `json:"drafts"`
			Next string `json:"nextPageToken"`
		}
		if err := c.do(ctx, http.MethodGet, "/drafts", q, nil, &r); err != nil {
			return "", err
		}
		for _, d := range r.Drafts {
			if d.Message.ID == messageID {
				return d.ID, nil
			}
		}
		page = r.Next
		if page == "" {
			return "", ErrNotFound
		}
	}
	return "", fmt.Errorf("draft lookup exceeds limit")
}
func (c *Client) SetLabelVisible(ctx context.Context, id string, visible bool) error {
	list, mailbox := "labelHide", "hide"
	if visible {
		list, mailbox = "labelShow", "show"
	}
	return c.do(ctx, http.MethodPatch, "/labels/"+url.PathEscape(id), nil, map[string]string{"labelListVisibility": list, "messageListVisibility": mailbox}, nil)
}

type Part struct {
	PartID   string `json:"partId"`
	MIMEType string `json:"mimeType"`
	Filename string `json:"filename"`
	Headers  []struct {
		Name  string `json:"name"`
		Value string `json:"value"`
	} `json:"headers"`
	Body struct {
		AttachmentID string `json:"attachmentId"`
		Size         int    `json:"size"`
		Data         string `json:"data"`
	} `json:"body"`
	Parts []Part `json:"parts"`
}

func (c *Client) Full(ctx context.Context, id string) (*Part, error) {
	var r struct {
		Payload Part `json:"payload"`
	}
	err := c.request(ctx, http.MethodGet, "/messages/"+url.PathEscape(id), url.Values{"format": {"full"}}, nil, &r, true, 20<<20)
	return &r.Payload, err
}
func (c *Client) Attachment(ctx context.Context, messageID, attachmentID string) ([]byte, error) {
	var r struct {
		Data string `json:"data"`
	}
	err := c.request(ctx, http.MethodGet, "/messages/"+url.PathEscape(messageID)+"/attachments/"+url.PathEscape(attachmentID), nil, nil, &r, true, 100<<20)
	if err != nil {
		return nil, err
	}
	return base64.RawURLEncoding.DecodeString(strings.TrimRight(r.Data, "="))
}
func (c *Client) DraftRaw(ctx context.Context, id string) ([]byte, error) {
	var r struct {
		Message struct {
			Raw string `json:"raw"`
		} `json:"message"`
	}
	err := c.request(ctx, http.MethodGet, "/drafts/"+url.PathEscape(id), url.Values{"format": {"raw"}}, nil, &r, true, 100<<20)
	if err != nil {
		return nil, err
	}
	return base64.RawURLEncoding.DecodeString(strings.TrimRight(r.Message.Raw, "="))
}

// Gmail delivers to MIME header recipients; SMTP-only Bcc envelope recipients
// must be represented explicitly in the API upload. Never drop hidden recipients.
func WithEnvelopeRecipients(raw []byte, recipients []string) ([]byte, error) {
	if len(recipients) == 0 {
		return raw, nil
	}
	m, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, field := range []string{"To", "Cc", "Bcc"} {
		addresses, err := m.Header.AddressList(field)
		if err != nil && m.Header.Get(field) != "" {
			return nil, err
		}
		for _, a := range addresses {
			seen[strings.ToLower(a.Address)] = true
		}
	}
	hidden := []string{}
	for _, recipient := range recipients {
		a, err := mail.ParseAddress(recipient)
		if err != nil {
			return nil, err
		}
		key := strings.ToLower(a.Address)
		if !seen[key] {
			seen[key] = true
			hidden = append(hidden, a.Address)
		}
	}
	if len(hidden) == 0 {
		return raw, nil
	}
	return append([]byte("Bcc: "+strings.Join(hidden, ", ")+"\r\n"), raw...), nil
}

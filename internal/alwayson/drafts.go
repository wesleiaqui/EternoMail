package alwayson

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"net/mail"
	"net/textproto"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/hkdb/aerion/internal/account"
	"github.com/hkdb/aerion/internal/automation/v1"
	"github.com/hkdb/aerion/internal/draft"
	"github.com/hkdb/aerion/internal/email"
	"github.com/hkdb/aerion/internal/message"
	"github.com/hkdb/aerion/internal/scheduledsend"
	"github.com/hkdb/aerion/internal/smtp"
)

func (r *SyncRuntime) ListDrafts(accountID string) ([]v1.DraftDocument, error) {
	if r == nil || r.drafts == nil || r.accounts == nil {
		return nil, errors.New("draft service is unavailable")
	}
	if _, err := r.accounts.Get(accountID); err != nil {
		return nil, err
	}
	items, err := r.drafts.ListByAccount(accountID)
	if err != nil {
		return nil, err
	}
	result := make([]v1.DraftDocument, 0, len(items))
	for _, item := range items {
		doc, err := r.draftDocument(item)
		if err != nil {
			return nil, err
		}
		result = append(result, *doc)
	}
	return result, nil
}

func (r *SyncRuntime) GetDraft(id, accountID string) (*v1.DraftDocument, error) {
	if r == nil || r.drafts == nil {
		return nil, errors.New("draft service is unavailable")
	}
	item, err := r.drafts.Get(id)
	if err != nil || item == nil {
		return nil, err
	}
	if accountID != "" && item.AccountID != accountID {
		return nil, nil
	}
	return r.draftDocument(item)
}

func (r *SyncRuntime) SaveDraft(id string, mutation v1.DraftMutation) (*v1.DraftDocument, error) {
	if r == nil || r.drafts == nil || r.accounts == nil {
		return nil, errors.New("draft service is unavailable")
	}
	accountID, revision, composed := mutation.AccountID, mutation.Revision, mutation.Message
	acc, err := r.accounts.Get(accountID)
	if err != nil {
		return nil, err
	}
	if acc == nil || !acc.Enabled {
		return nil, errors.New("account is unavailable")
	}
	if mutation.Encrypted && mutation.PGPEncrypted || mutation.Encrypted && len(mutation.EncryptedBody) == 0 || mutation.PGPEncrypted && len(mutation.PGPEncryptedBody) == 0 {
		return nil, v1.InvalidRequest("Encrypted draft payload is incomplete or has conflicting encryption modes", nil)
	}
	if mutation.Encrypted != composed.EncryptMessage || mutation.PGPEncrypted != composed.PGPEncryptMessage {
		return nil, v1.InvalidRequest("Draft encryption flags must match the opaque ciphertext payload", nil)
	}
	if (mutation.Encrypted || mutation.PGPEncrypted) && (composed.HTMLBody != "" || composed.TextBody != "" || len(composed.Attachments) > 0) {
		return nil, v1.InvalidRequest("Encrypted draft content must be sent only as opaque ciphertext", nil)
	}
	if mutation.Encrypted && len(mutation.PGPEncryptedBody) > 0 || mutation.PGPEncrypted && len(mutation.EncryptedBody) > 0 {
		return nil, v1.InvalidRequest("Draft contains ciphertext for a different encryption mode", nil)
	}
	if !mutation.Encrypted && len(mutation.EncryptedBody) > 0 || !mutation.PGPEncrypted && len(mutation.PGPEncryptedBody) > 0 {
		return nil, v1.InvalidRequest("Draft contains ciphertext without its encryption mode", nil)
	}
	if err := r.validateDraftSender(acc, composed); err != nil {
		return nil, err
	}
	if id != "" {
		current, err := r.drafts.Get(id)
		if err != nil {
			return nil, err
		}
		if current == nil || current.AccountID != accountID {
			return nil, draft.ErrDraftNotFound
		}
		if current.StorageVersion != revision {
			return nil, draft.ErrDraftRevisionConflict
		}
		current.ToList, err = jsonAddresses(composed.To)
		if err != nil {
			return nil, err
		}
		current.CcList, err = jsonAddresses(composed.Cc)
		if err != nil {
			return nil, err
		}
		current.BccList, err = jsonAddresses(composed.Bcc)
		if err != nil {
			return nil, err
		}
		current.Subject, current.BodyHTML, current.BodyText = composed.Subject, composed.HTMLBody, composed.TextBody
		current.InReplyToID = composed.InReplyTo
		references, marshalErr := json.Marshal(composed.References)
		err = marshalErr
		if err != nil {
			return nil, err
		}
		current.ReferencesList = string(references)
		current.IdentityID = composed.IdentityID
		current.SignMessage, current.PGPSignMessage = composed.SignMessage, composed.PGPSignMessage
		current.Encrypted, current.EncryptedBody = mutation.Encrypted, mutation.EncryptedBody
		current.PGPEncrypted, current.PGPEncryptedBody = mutation.PGPEncrypted, mutation.PGPEncryptedBody
		current.SyncStatus = draft.SyncStatusPending
		current.AttachmentsData = mutation.AttachmentsData
		if len(current.AttachmentsData) == 0 && !mutation.Encrypted && !mutation.PGPEncrypted {
			current.AttachmentsData, err = json.Marshal(composed.Attachments)
			if err != nil {
				return nil, err
			}
		}
		if err := r.drafts.UpdateIfRevision(current, revision); err != nil {
			return nil, err
		}
		updated, err := r.drafts.Get(id)
		if err != nil {
			return nil, err
		}
		return r.draftDocument(updated)
	}
	toList, err := jsonAddresses(composed.To)
	if err != nil {
		return nil, err
	}
	ccList, err := jsonAddresses(composed.Cc)
	if err != nil {
		return nil, err
	}
	bccList, err := jsonAddresses(composed.Bcc)
	if err != nil {
		return nil, err
	}
	references, err := json.Marshal(composed.References)
	if err != nil {
		return nil, err
	}
	attachments := mutation.AttachmentsData
	if len(attachments) == 0 && !mutation.Encrypted && !mutation.PGPEncrypted {
		attachments, err = json.Marshal(composed.Attachments)
		if err != nil {
			return nil, err
		}
	}
	requestID := strings.TrimSpace(mutation.ID)
	if requestID == "" {
		requestID = uuid.NewString()
	}
	item := &draft.Draft{ID: requestID, AccountID: accountID, ToList: toList, CcList: ccList, BccList: bccList,
		Subject: composed.Subject, BodyHTML: composed.HTMLBody, BodyText: composed.TextBody, InReplyToID: composed.InReplyTo,
		ReferencesList: string(references), IdentityID: composed.IdentityID, SignMessage: composed.SignMessage,
		Encrypted: mutation.Encrypted, EncryptedBody: mutation.EncryptedBody, PGPSignMessage: composed.PGPSignMessage,
		PGPEncrypted: mutation.PGPEncrypted, PGPEncryptedBody: mutation.PGPEncryptedBody,
		AttachmentsData: attachments, SyncStatus: draft.SyncStatusPending}
	if createErr := r.drafts.Create(item); createErr != nil {
		existing, getErr := r.drafts.Get(requestID)
		if getErr != nil {
			return nil, createErr
		}
		if existing == nil || existing.AccountID != accountID || draft.Fingerprint(existing) != draft.Fingerprint(item) {
			return nil, createErr
		}
		return r.draftDocument(existing)
	}
	created, err := r.drafts.Get(item.ID)
	if err != nil {
		return nil, err
	}
	return r.draftDocument(created)
}

func (r *SyncRuntime) DeleteDraft(id, accountID, revision string) error {
	if r == nil || r.drafts == nil {
		return errors.New("draft service is unavailable")
	}
	item, err := r.drafts.Get(id)
	if err != nil {
		return err
	}
	if item == nil || item.AccountID != accountID {
		return draft.ErrDraftNotFound
	}
	if item.StorageVersion != revision {
		return draft.ErrDraftRevisionConflict
	}
	return r.drafts.DeleteIfRevision(id, revision)
}

func (r *SyncRuntime) PrepareReply(req v1.ReplyPreparationRequest) (*smtp.ComposeMessage, error) {
	if r == nil || r.accounts == nil || r.messages == nil {
		return nil, errors.New("reply preparation is unavailable")
	}
	acc, err := r.accounts.Get(req.AccountID)
	if err != nil {
		return nil, err
	}
	if acc == nil {
		return nil, errors.New("account not found")
	}
	original, err := r.messages.Get(req.MessageID)
	if err != nil {
		return nil, err
	}
	if original == nil || original.AccountID != req.AccountID {
		return nil, errors.New("message not found for account")
	}
	if !original.BodyFetched {
		return nil, v1.InvalidRequest("Message body must be explicitly fetched before preparing a reply", nil)
	}
	identities, err := r.accounts.GetIdentities(req.AccountID)
	if err != nil {
		return nil, err
	}
	identity := selectReplyIdentity(identities, original)
	from := smtp.Address{Name: acc.Name, Address: acc.Email}
	if identity != nil {
		from = smtp.Address{Name: identity.Name, Address: identity.Email}
	}
	self := map[string]bool{strings.ToLower(strings.TrimSpace(acc.Email)): true}
	for _, item := range identities {
		self[strings.ToLower(strings.TrimSpace(item.Email))] = true
	}
	fromOriginal := []smtp.Address{{Name: original.FromName, Address: strings.TrimSpace(original.FromEmail)}}
	if strings.TrimSpace(original.ReplyTo) != "" {
		parsed, parseErr := mail.ParseAddressList(original.ReplyTo)
		if parseErr != nil {
			return nil, v1.InvalidRequest("Message Reply-To header is malformed", parseErr)
		}
		fromOriginal = fromOriginal[:0]
		for _, address := range parsed {
			fromOriginal = append(fromOriginal, smtp.Address{Name: address.Name, Address: address.Address})
		}
	}
	to := filterReplyAddresses(fromOriginal, self)
	if req.Mode == "reply" && len(to) == 0 {
		to = fromOriginal
	}
	var cc []smtp.Address
	if req.Mode == "reply-all" {
		originalTo, err := parseReplyAddresses(original.ToList)
		if err != nil {
			return nil, err
		}
		originalCC, err := parseReplyAddresses(original.CcList)
		if err != nil {
			return nil, err
		}
		to = append(to, filterReplyAddresses(originalTo, self)...)
		seen := make(map[string]bool)
		for _, address := range to {
			seen[strings.ToLower(address.Address)] = true
		}
		for _, address := range filterReplyAddresses(originalCC, self) {
			key := strings.ToLower(address.Address)
			if !seen[key] {
				cc = append(cc, address)
				seen[key] = true
			}
		}
		if len(to) == 0 && len(cc) == 0 {
			to = fromOriginal
		}
	}
	subject := original.Subject
	if req.Mode == "forward" {
		if !strings.HasPrefix(strings.ToLower(subject), "fwd:") && !strings.HasPrefix(strings.ToLower(subject), "fw:") {
			subject = "Fwd: " + subject
		}
	} else if !strings.HasPrefix(strings.ToLower(subject), "re:") {
		subject = "Re: " + subject
	}
	quotedHTML := original.BodyHTML
	if quotedHTML == "" && original.BodyText != "" {
		quotedHTML = "<p>" + strings.ReplaceAll(html.EscapeString(original.BodyText), "\n", "<br>") + "</p>"
	}
	quotedHTML = email.BlockRemoteImages(quotedHTML)
	quotedText := original.BodyText
	if strings.TrimSpace(quotedText) == "" && original.BodyHTML != "" {
		quotedText = email.ExtractPlainTextFromHTML(original.BodyHTML)
	}
	dateText := original.Date.Format("Mon, Jan 2 2006 at 3:04:05 PM MST")
	sender := original.FromEmail
	if original.FromName != "" {
		sender = original.FromName + " <" + original.FromEmail + ">"
	}
	citation := "On " + dateText + ", " + sender + " wrote:"
	body := "\n\n" + citation + "\n" + quoteReplyText(quotedText)
	htmlBody := "<p></p><p></p><p>" + html.EscapeString(citation) + "</p><blockquote type=\"cite\">" + quotedHTML + "</blockquote>"
	if req.Mode == "forward" {
		body = "\n\n---------- Forwarded message ----------\nFrom: " + sender + "\nSubject: " + original.Subject + "\nDate: " + dateText + "\nTo: " + original.ToList + "\n\n" + quotedText
		htmlBody = "<p></p><p></p><p>---------- Forwarded message ----------<br>From: " + html.EscapeString(sender) + "<br>Subject: " + html.EscapeString(original.Subject) + "<br>Date: " + html.EscapeString(dateText) + "<br>To: " + html.EscapeString(original.ToList) + "</p><p></p>" + quotedHTML
		to, cc = nil, nil
	}
	var refs []string
	if original.References != "" {
		if err := json.Unmarshal([]byte(original.References), &refs); err != nil {
			return nil, err
		}
	}
	if original.MessageID != "" {
		refs = append(refs, angleMessageID(original.MessageID))
	}
	attachments := r.replyAttachments(original, req.Mode, quotedHTML)
	return &smtp.ComposeMessage{From: from, To: to, Cc: cc, Subject: subject, HTMLBody: htmlBody, TextBody: body,
		InReplyTo: angleMessageID(original.MessageID), References: refs, GmailThreadID: gmailThreadID(original),
		IdentityID: identityID(identity), Attachments: attachments}, nil
}

func (r *SyncRuntime) replyAttachments(original *message.Message, mode, quotedHTML string) []smtp.Attachment {
	if r.attachments == nil || original == nil {
		return nil
	}
	var result []smtp.Attachment
	inline, err := r.attachments.GetInlineByMessage(original.ID)
	if err == nil {
		for cid, dataURL := range inline {
			if mode != "forward" && !strings.Contains(quotedHTML, "cid:"+cid) {
				continue
			}
			contentType, encoded, ok := strings.Cut(strings.TrimPrefix(dataURL, "data:"), ";base64,")
			if !ok || encoded == "" {
				continue
			}
			result = append(result, smtp.Attachment{ContentBase64: encoded, ContentType: contentType, ContentID: cid, Inline: true, Filename: cid})
		}
	}
	if mode != "forward" || r.syncEngine == nil {
		return result
	}
	items, err := r.attachments.GetByMessage(original.ID)
	if err != nil {
		return result
	}
	regular := make([]*message.Attachment, 0, len(items))
	for _, item := range items {
		if item != nil && !item.IsInline {
			regular = append(regular, item)
		}
	}
	if len(regular) == 0 {
		return result
	}
	raw, err := r.syncEngine.FetchRawMessage(r.runtimeContext(), original.AccountID, original.FolderID, original.UID)
	if err != nil {
		return result
	}
	downloader := email.NewAttachmentDownloader("")
	for _, item := range regular {
		content, repaired, err := r.attachments.RepairedContent(item.ID)
		if err == nil && !repaired {
			content, err = downloader.ExtractAttachmentContent(raw, item.Filename)
		}
		if err != nil {
			continue
		}
		result = append(result, smtp.Attachment{Content: content, ContentType: item.ContentType, Filename: item.Filename})
	}
	return result
}

func (r *SyncRuntime) runtimeContext() context.Context {
	r.ctxMu.RLock()
	ctx := r.ctx
	r.ctxMu.RUnlock()
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

func quoteReplyText(value string) string {
	lines := strings.Split(value, "\n")
	for i := range lines {
		lines[i] = "> " + lines[i]
	}
	return strings.Join(lines, "\n")
}

func angleMessageID(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || strings.HasPrefix(value, "<") && strings.HasSuffix(value, ">") {
		return value
	}
	return "<" + value + ">"
}

func gmailThreadID(original *message.Message) string {
	prefix := "gmail:" + original.AccountID + ":"
	if strings.HasPrefix(original.ThreadID, prefix) {
		return strings.TrimPrefix(original.ThreadID, prefix)
	}
	return ""
}

func (r *SyncRuntime) Send(ctx context.Context, req v1.SendRequest, key, hash string) (*v1.SendResponse, error) {
	if r == nil || r.immediateSend == nil || r.validateSender == nil {
		return nil, v1.InvalidRequest("Message submission is unavailable; message not submitted", nil)
	}
	if err := validateSendRequest(req); err != nil {
		return nil, err
	}
	if response, found, err := r.existingSendReceipt(key, hash); err != nil || found {
		return response, err
	}
	if err := r.validateSender(req.AccountID, req.From); err != nil {
		// This check precedes idempotency reservation and provider submission.
		return nil, v1.InvalidRequest("Sender unavailable; message not submitted", err)
	}
	if req.DraftID != "" {
		item, err := r.drafts.Get(req.DraftID)
		if err != nil {
			return nil, err
		}
		if item == nil || item.AccountID != req.AccountID {
			return nil, draft.ErrDraftNotFound
		}
		if item.StorageVersion != req.Revision {
			return nil, draft.ErrDraftRevisionConflict
		}
	}
	operationID := uuid.NewString()
	result, err := r.db.Exec(`INSERT INTO send_api_idempotency (idempotency_key, request_hash, operation_id, state) VALUES (?, ?, ?, 'processing') ON CONFLICT(idempotency_key) DO NOTHING`, key, hash, operationID)
	if err != nil {
		return nil, err
	}
	inserted, err := result.RowsAffected()
	if err != nil {
		return nil, err
	}
	if inserted == 0 {
		response, found, err := r.existingSendReceipt(key, hash)
		if err != nil {
			return nil, err
		}
		if !found {
			return nil, errors.New("send receipt disappeared after idempotency reservation")
		}
		return response, nil
	}
	delivery := scheduledsend.Delivery{AccountID: req.AccountID, From: req.From, To: req.To, RFC822: req.RFC822, GmailThreadID: req.GmailThreadID, DueAt: time.Now()}
	deliveryErr := r.immediateSend(ctx, delivery)
	state, code := v1.OperationSent, ""
	if deliveryErr != nil {
		var classified *scheduledsend.DeliveryError
		if errors.As(deliveryErr, &classified) {
			switch classified.Kind {
			case scheduledsend.OutcomeUnknown:
				state, code = v1.OperationUnknown, "delivery_outcome_unknown"
			case scheduledsend.SafeRetry:
				state, code = v1.OperationRetry, "delivery_not_accepted_retry_safe"
			case scheduledsend.AcceptedWithoutSentCopy:
				state, code = v1.OperationSent, "sent_copy_failed"
			default:
				state, code = v1.OperationFailed, "delivery_failed"
			}
		} else {
			state, code = v1.OperationUnknown, "delivery_outcome_unknown"
		}
	}
	_, persistErr := r.db.Exec(`UPDATE send_api_idempotency SET state = ?, code = ?, updated_at = datetime('now') WHERE idempotency_key = ? AND request_hash = ? AND state = 'processing'`, state, code, key, hash)
	if persistErr != nil {
		return nil, fmt.Errorf("delivery result could not be persisted; query the same idempotency key: %w", persistErr)
	}
	if state == v1.OperationSent && req.DraftID != "" {
		if err := r.drafts.DeleteIfRevision(req.DraftID, req.Revision); err != nil {
			code = "sent_draft_cleanup_failed"
			_, _ = r.db.Exec(`UPDATE send_api_idempotency SET code = ? WHERE idempotency_key = ?`, code, key)
		}
	}
	return &v1.SendResponse{Receipt: v1.OperationReceipt{OperationID: operationID, State: state}, Code: code}, nil
}

func (r *SyncRuntime) existingSendReceipt(key, hash string) (*v1.SendResponse, bool, error) {
	var existingHash, existingID, state, code string
	err := r.db.QueryRow(`SELECT request_hash, operation_id, state, code FROM send_api_idempotency WHERE idempotency_key = ?`, key).Scan(&existingHash, &existingID, &state, &code)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if hash != "" && existingHash != hash {
		return nil, true, v1.ErrSendIdempotencyConflict
	}
	if state == "processing" {
		_, err := r.db.Exec(`UPDATE send_api_idempotency SET state = 'outcome_unknown', code = 'interrupted_delivery', updated_at = datetime('now') WHERE idempotency_key = ? AND state = 'processing' AND updated_at < datetime('now', '-3 minutes')`, key)
		if err != nil {
			return nil, false, err
		}
		if err := r.db.QueryRow(`SELECT state, code FROM send_api_idempotency WHERE idempotency_key = ?`, key).Scan(&state, &code); err != nil {
			return nil, false, err
		}
	}
	if state == "processing" {
		state = string(v1.OperationSending)
	}
	return &v1.SendResponse{Receipt: v1.OperationReceipt{OperationID: existingID, State: v1.OperationState(state), Replayed: true}, Code: code}, true, nil
}

func (r *SyncRuntime) GetSendReceipt(key string) (*v1.SendResponse, bool, error) {
	if r == nil || r.db == nil {
		return nil, false, errors.New("send receipt storage is unavailable")
	}
	return r.existingSendReceipt(strings.TrimSpace(key), "")
}

func validateSendRequest(req v1.SendRequest) error {
	if req.AccountID == "" || req.From == "" || len(req.To) == 0 || len(req.To) > 1000 || len(req.RFC822) == 0 || len(req.RFC822) > 20<<20 {
		return v1.InvalidRequest("Account, sender, recipients, and bounded RFC822 content are required", nil)
	}
	from, err := mail.ParseAddress(req.From)
	if err != nil || !strings.EqualFold(from.Address, req.From) {
		return v1.InvalidRequest("Sender address is invalid", err)
	}
	for _, recipient := range req.To {
		parsed, err := mail.ParseAddress(recipient)
		if err != nil || !strings.EqualFold(parsed.Address, recipient) {
			return v1.InvalidRequest("Recipient address is invalid", err)
		}
	}
	parsedMessage, err := mail.ReadMessage(bytes.NewReader(req.RFC822))
	if err != nil {
		return v1.InvalidRequest("RFC822 message is malformed", err)
	}
	fromHeader, err := mail.ParseAddress(parsedMessage.Header.Get("From"))
	if err != nil || !strings.EqualFold(fromHeader.Address, req.From) {
		return v1.InvalidRequest("RFC822 sender must match the authorized envelope sender", err)
	}
	envelope := make(map[string]bool, len(req.To))
	for _, recipient := range req.To {
		parsed, _ := mail.ParseAddress(recipient)
		envelope[strings.ToLower(parsed.Address)] = true
	}
	if len(parsedMessage.Header[textproto.CanonicalMIMEHeaderKey("Bcc")]) > 0 {
		return v1.InvalidRequest("RFC822 must not expose Bcc recipients in message headers", nil)
	}
	for _, header := range []string{"To", "Cc"} {
		values := parsedMessage.Header[textproto.CanonicalMIMEHeaderKey(header)]
		for _, value := range values {
			addresses, err := mail.ParseAddressList(value)
			if err != nil {
				return v1.InvalidRequest("RFC822 recipient header is malformed", err)
			}
			for _, address := range addresses {
				if !envelope[strings.ToLower(address.Address)] {
					return v1.InvalidRequest("RFC822 recipients must match the SMTP envelope", nil)
				}
			}
		}
	}
	return nil
}

func (r *SyncRuntime) draftDocument(item *draft.Draft) (*v1.DraftDocument, error) {
	var to, cc, bcc []smtp.Address
	if err := json.Unmarshal([]byte(item.ToList), &to); err != nil && item.ToList != "" {
		return nil, err
	}
	if err := json.Unmarshal([]byte(item.CcList), &cc); err != nil && item.CcList != "" {
		return nil, err
	}
	if err := json.Unmarshal([]byte(item.BccList), &bcc); err != nil && item.BccList != "" {
		return nil, err
	}
	var references []string
	if err := json.Unmarshal([]byte(item.ReferencesList), &references); err != nil && item.ReferencesList != "" {
		return nil, err
	}
	var attachments []smtp.Attachment
	if err := json.Unmarshal(item.AttachmentsData, &attachments); err != nil && len(item.AttachmentsData) > 0 {
		return nil, err
	}
	acc, err := r.accounts.Get(item.AccountID)
	if err != nil {
		return nil, err
	}
	from := smtp.Address{Name: acc.Name, Address: acc.Email}
	if item.IdentityID != "" {
		identity, err := r.accounts.GetIdentity(item.IdentityID)
		if err != nil {
			return nil, err
		}
		if identity == nil || identity.AccountID != item.AccountID {
			return nil, errors.New("draft identity is unavailable for its account")
		}
		from = smtp.Address{Name: identity.Name, Address: identity.Email}
	}
	return &v1.DraftDocument{ID: item.ID, AccountID: item.AccountID, Revision: item.StorageVersion, UpdatedAt: item.UpdatedAt.Format(time.RFC3339Nano),
		Encrypted: item.Encrypted, EncryptedBody: item.EncryptedBody, PGPEncrypted: item.PGPEncrypted,
		PGPEncryptedBody: item.PGPEncryptedBody, AttachmentsData: item.AttachmentsData,
		Message: smtp.ComposeMessage{From: from, IdentityID: item.IdentityID, To: to, Cc: cc, Bcc: bcc, Subject: item.Subject,
			HTMLBody: item.BodyHTML, TextBody: item.BodyText, Attachments: attachments, InReplyTo: item.InReplyToID, References: references,
			SignMessage: item.SignMessage, PGPSignMessage: item.PGPSignMessage}}, nil
}

func jsonAddresses(addresses []smtp.Address) (string, error) {
	encoded, err := json.Marshal(addresses)
	return string(encoded), err
}

func (r *SyncRuntime) validateDraftSender(acc *account.Account, composed smtp.ComposeMessage) error {
	if composed.IdentityID != "" {
		identity, err := r.accounts.GetIdentity(composed.IdentityID)
		if err != nil {
			return err
		}
		if identity == nil || identity.AccountID != acc.ID || !strings.EqualFold(identity.Email, composed.From.Address) {
			return v1.InvalidRequest("Draft sender identity does not belong to the account", nil)
		}
		return nil
	}
	if !strings.EqualFold(acc.Email, composed.From.Address) {
		return v1.InvalidRequest("Draft sender must belong to the account", nil)
	}
	return nil
}

func parseReplyAddresses(raw string) ([]smtp.Address, error) {
	var addresses []message.Address
	if raw == "" {
		return nil, nil
	}
	if err := json.Unmarshal([]byte(raw), &addresses); err != nil {
		return nil, err
	}
	result := make([]smtp.Address, 0, len(addresses))
	for _, address := range addresses {
		result = append(result, smtp.Address{Name: address.Name, Address: address.Email})
	}
	return result, nil
}

func filterReplyAddresses(addresses []smtp.Address, self map[string]bool) []smtp.Address {
	result := make([]smtp.Address, 0, len(addresses))
	for _, address := range addresses {
		if address.Address != "" && !self[strings.ToLower(address.Address)] {
			result = append(result, address)
		}
	}
	return result
}

func selectReplyIdentity(identities []*account.Identity, original *message.Message) *account.Identity {
	addresses, _ := parseReplyAddresses(original.ToList)
	cc, _ := parseReplyAddresses(original.CcList)
	bcc, _ := parseReplyAddresses(original.BccList)
	addresses = append(append(addresses, cc...), bcc...)
	for _, address := range addresses {
		for _, identity := range identities {
			if strings.EqualFold(identity.Email, address.Address) {
				return identity
			}
		}
	}
	for _, identity := range identities {
		if identity.IsDefault {
			return identity
		}
	}
	if len(identities) > 0 {
		return identities[0]
	}
	return nil
}

func identityID(identity *account.Identity) string {
	if identity == nil {
		return ""
	}
	return identity.ID
}

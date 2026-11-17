package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/hkdb/aerion/internal/ipc"
	"github.com/hkdb/aerion/internal/platform"
	"github.com/hkdb/aerion/internal/readerbridge"
	wailsRuntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

const readerCallType = "reader_call"

// ReaderConfig fixes the selected thread independently of the main window.
type ReaderConfig struct {
	MailWindow      bool   `json:"mailWindow"`
	FolderName      string `json:"folderName"`
	AccountID       string `json:"accountId"`
	FolderID        string `json:"folderId"`
	ThreadID        string `json:"threadId"`
	ThreadAccountID string `json:"threadAccountId"`
	ThreadFolderID  string `json:"threadFolderId"`
	SearchQuery     string `json:"searchQuery"`
	FolderType      string `json:"folderType"`
	IPCAddress      string `json:"-"`
}

// ReaderApp owns a window and authenticated IPC only: no database or sync engine.
type ReaderApp struct {
	config       ReaderConfig
	ctx          context.Context
	client       ipc.Client
	startupError error
	ready        chan struct{}
}

// NewReaderApp creates a detached reader without accessing mail data.
func NewReaderApp(config ReaderConfig) *ReaderApp {
	return &ReaderApp{config: config, ready: make(chan struct{})}
}

// StartReader authenticates using the parent token passed through stdin, not argv.
func StartReader(r *ReaderApp, ctx context.Context) {
	defer close(r.ready)
	r.ctx = ctx
	token, err := io.ReadAll(io.LimitReader(os.Stdin, 4096))
	if err != nil {
		r.startupError = err
		return
	}
	if strings.TrimSpace(string(token)) == "" {
		r.startupError = errors.New("missing reader authentication")
		return
	}
	r.client = ipc.NewClient(r.config.IPCAddress)
	r.client.OnMessage(func(msg ipc.Message) {
		switch msg.Type {
		case ipc.TypeShutdown:
			wailsRuntime.Quit(ctx)
		case mailWindowEventType:
			event, err := mailWindowEventFromMessage(msg)
			if err == nil {
				wailsRuntime.EventsEmit(ctx, event.Name, event.Data...)
			}
		}
	})
	connectCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	r.startupError = r.client.Connect(connectCtx, strings.TrimSpace(string(token)))
}

// ShutdownReader releases only this reader's IPC connection.
func ShutdownReader(r *ReaderApp) {
	if r.client != nil {
		_ = r.client.Close()
	}
}

// GetConfig returns this window's immutable selection after authentication.
func (r *ReaderApp) GetConfig() (ReaderConfig, error) {
	select {
	case <-r.ready:
		return r.config, r.startupError
	case <-time.After(12 * time.Second):
		return ReaderConfig{}, errors.New("reader connection timed out")
	}
}

// Call forwards an explicitly allowlisted mail operation to the main process.
func (r *ReaderApp) Call(operation string, args json.RawMessage) (any, error) {
	if _, err := r.GetConfig(); err != nil {
		return nil, err
	}
	if r.client == nil {
		return nil, errors.New("reader is not connected")
	}
	callType := readerCallType
	if r.config.MailWindow {
		callType = mailWindowCallType
	}
	request, err := ipc.NewMessage(callType, struct {
		Operation string          `json:"operation"`
		Args      json.RawMessage `json:"args"`
	}{operation, args})
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(r.ctx, 2*time.Minute)
	defer cancel()
	reply, err := r.client.SendAndWait(ctx, request)
	if err != nil {
		return nil, err
	}
	if reply.Error != "" {
		return nil, errors.New(reply.Error)
	}
	var result any
	if len(reply.Payload) > 0 {
		err = json.Unmarshal(reply.Payload, &result)
	}
	return result, err
}

// detachedChildLogFile captures a spawned window's stdout/stderr so failures
// that lose the webview (blank window, early crash) are diagnosable after the
// fact. One file per window kind, truncated per spawn, under the cache dir.
func detachedChildLogFile(cacheDir, kind string, cmd *exec.Cmd) *os.File {
	if cacheDir == "" {
		return nil
	}
	logDir := filepath.Join(cacheDir, "logs")
	if err := os.MkdirAll(logDir, 0o700); err != nil {
		return nil
	}
	f, err := os.OpenFile(filepath.Join(logDir, kind+"-window.log"), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return nil
	}
	cmd.Stdout = f
	cmd.Stderr = f
	return f
}

// OpenReaderWindow opens a selected thread in another native Wails process.
func (a *App) OpenReaderWindow(accountID, folderID, threadID, folderType string) error {
	if a.ipcServer == nil || a.ipcTokenMgr == nil {
		return errors.New("multi-window service unavailable")
	}
	conversation, err := a.GetConversation(threadID, folderID)
	if err != nil {
		return err
	}
	if conversation == nil || len(conversation.Messages) == 0 {
		return errors.New("conversation is not available locally; open it first to download")
	}
	if conversation.Messages[0].AccountID != accountID {
		return errors.New("reader account does not match conversation")
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(executable, "--reader", "--account", accountID, "--reader-folder", folderID, "--reader-thread", threadID, "--reader-folder-type", folderType, "--ipc-address", a.ipcServer.Address())
	var cacheDir string
	if a.paths != nil {
		cacheDir = a.paths.Cache
	}
	childLog := detachedChildLogFile(cacheDir, "reader", cmd)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	if err = cmd.Start(); err != nil {
		_ = stdin.Close()
		if childLog != nil {
			_ = childLog.Close()
		}
		return fmt.Errorf("open reader: %w", err)
	}
	go func() {
		defer recoverPanic("app.reader", "authenticate child")
		defer stdin.Close()
		if _, err := io.WriteString(stdin, a.ipcTokenMgr.GetToken()); err != nil {
			_ = cmd.Process.Kill()
		}
	}()
	go func() {
		defer recoverPanic("app.reader", "wait child")
		defer func() {
			if childLog != nil {
				_ = childLog.Close()
			}
		}()
		_ = cmd.Wait()
	}()
	return nil
}

// GetOpenReaderInTabPreference reports whether macOS's current window-tab
// preference favors opening a thread in the app's tabbed workspace. In the
// "In Full Screen" setting, AppKit only enables tabs while this window is full
// screen.
func (a *App) GetOpenReaderInTabPreference() bool {
	if a == nil || a.ctx == nil {
		return platform.PrefersWindowTabs(false)
	}
	return platform.PrefersWindowTabs(wailsRuntime.WindowIsFullscreen(a.ctx))
}

func (a *App) handleReaderCall(clientID string, msg ipc.Message) {
	const composerMaxBytes = 30 << 20
	var request struct {
		Operation string          `json:"operation"`
		Args      json.RawMessage `json:"args"`
	}
	var result any
	err := msg.ParsePayload(&request)
	if len(msg.Payload) > composerMaxBytes && msg.Type == composerCallType {
		err = errors.New("composer request exceeds the IPC size limit")
	}
	if err == nil {
		methods := a.readerMethods()
		if msg.Type == mailWindowCallType {
			methods = a.mailWindowMethods()
		}
		if msg.Type == composerCallType {
			result, err = readerbridge.DispatchLimit(methods, request.Operation, request.Args, composerMaxBytes)
		} else {
			result, err = readerbridge.Dispatch(methods, request.Operation, request.Args)
		}
	}
	reply, marshalErr := ipc.NewReply(msg, msg.Type, result)
	if marshalErr != nil {
		reply, _ = ipc.NewReply(msg, msg.Type, nil)
		err = marshalErr
	}
	// Stay within the existing IPC frame limit; report oversize data explicitly.
	maxReplyBytes := 9 << 20
	if msg.Type == composerCallType {
		maxReplyBytes = composerMaxBytes
	}
	if len(reply.Payload) > maxReplyBytes {
		reply.Payload = nil
		err = errors.New("thread exceeds reader IPC size limit")
	}
	if err != nil {
		reply.Error = err.Error()
	}
	_ = a.ipcServer.Send(clientID, reply)
}

func (a *App) readerMethods() map[string]any {
	return map[string]any{
		"AddImageAllowlist":             a.AddImageAllowlist,
		"ArchiveWithUndo":               a.ArchiveWithUndo,
		"CopyToFolder":                  a.CopyToFolder,
		"DeletePermanently":             a.DeletePermanently,
		"FetchMessageBody":              a.FetchMessageBody,
		"GetAccount":                    a.GetAccount,
		"GetReadLaterQueue":             a.GetReadLaterQueue,
		"GetReplyLaterQueue":            a.GetReplyLaterQueue,
		"GetSnoozedConversations":       a.GetSnoozedConversations,
		"GetSnoozedCount":               a.GetSnoozedCount,
		"GetIdentities":                 a.GetIdentities,
		"GetOriginalMessage":            a.GetOriginalMessage,
		"GetComposerOriginalMessage":    a.GetComposerOriginalMessage,
		"GetDraft":                      a.GetDraft,
		"GetDraftForEdit":               a.GetDraftForEdit,
		"GetComposerDraft":              a.GetComposerDraft,
		"GetComposerDraftForEdit":       a.GetComposerDraftForEdit,
		"SaveComposerDraft":             a.SaveComposerDraft,
		"DeleteComposerDraft":           a.DeleteComposerDraft,
		"SaveDraft":                     a.SaveDraft,
		"DeleteDraft":                   a.DeleteDraft,
		"PrepareReply":                  a.PrepareReply,
		"PrepareComposerReply":          a.PrepareComposerReply,
		"PrepareComposerSend":           a.PrepareComposerSend,
		"SendPreparedComposerMessage":   a.SendPreparedComposerMessage,
		"GetComposerSendReceipt":        a.GetComposerSendReceipt,
		"SearchContacts":                a.SearchContacts,
		"SendMessage":                   a.SendMessage,
		"ChangeLaterWithUndo":           a.ChangeLaterWithUndo,
		"ChangeReadLaterWithUndo":       a.ChangeReadLaterWithUndo,
		"SnoozeConversationWithUndo":    a.SnoozeConversationWithUndo,
		"UnsnoozeConversationWithUndo":  a.UnsnoozeConversationWithUndo,
		"HasSMIMECertificate":           a.HasSMIMECertificate,
		"GetSMIMECertificateForEmail":   a.GetSMIMECertificateForEmail,
		"GetSMIMESignPolicy":            a.GetSMIMESignPolicy,
		"GetSMIMEEncryptPolicy":         a.GetSMIMEEncryptPolicy,
		"CheckRecipientCerts":           a.CheckRecipientCerts,
		"PickRecipientCertFile":         a.PickRecipientCertFile,
		"ImportRecipientCert":           a.ImportRecipientCert,
		"HasPGPKey":                     a.HasPGPKey,
		"GetPGPKeyForEmail":             a.GetPGPKeyForEmail,
		"GetPGPSignPolicy":              a.GetPGPSignPolicy,
		"GetPGPEncryptPolicy":           a.GetPGPEncryptPolicy,
		"CheckRecipientPGPKeys":         a.CheckRecipientPGPKeys,
		"PickRecipientPGPKeyFile":       a.PickRecipientPGPKeyFile,
		"ImportRecipientPGPKey":         a.ImportRecipientPGPKey,
		"LookupWKD":                     a.LookupWKD,
		"LookupHKP":                     a.LookupHKP,
		"LookupPGPKey":                  a.LookupPGPKey,
		"DraftHistoryCommit":            a.DraftHistoryCommit,
		"DraftHistoryTree":              a.DraftHistoryTree,
		"DraftHistoryState":             a.DraftHistoryState,
		"DraftHistoryCheckout":          a.DraftHistoryCheckout,
		"ClearDraftHistory":             a.ClearDraftHistory,
		"MoveDraftHistory":              a.MoveDraftHistory,
		"PutDraftAttachmentBlob":        a.PutDraftAttachmentBlob,
		"GetDraftAttachmentBlob":        a.GetDraftAttachmentBlob,
		"DraftHistoryCommitBlobs":       a.DraftHistoryCommitBlobs,
		"QueuePendingSend":              a.QueuePendingSend,
		"GetSendLaterSetup":             a.GetSendLaterSetup,
		"InitializeSendLaterClient":     a.InitializeSendLaterClient,
		"SaveSendLaterConfig":           a.SaveSendLaterConfig,
		"ScheduleMessage":               a.ScheduleMessage,
		"ListScheduledMessages":         a.ListScheduledMessages,
		"UpdateScheduledMessage":        a.UpdateScheduledMessage,
		"RescheduleMessage":             a.RescheduleMessage,
		"GetScheduledPage":              a.GetScheduledPage,
		"SendMessageWithDraftGuard":     a.SendMessageWithDraftGuard,
		"GetAccentBarUnread":            a.GetAccentBarUnread,
		"GetAccountProfilePhotos":       a.GetAccountProfilePhotos,
		"GetReaderIdentityEmails":       a.GetReaderIdentityEmails,
		"GetAllAccountIdentities":       a.GetAllAccountIdentities,
		"GetAccounts":                   a.GetAccounts,
		"GetAlwaysLoadImages":           a.GetAlwaysLoadImages,
		"GetStripTrackingParams":        a.GetStripTrackingParams,
		"GetAlwaysShowMessageCheckbox":  a.GetAlwaysShowMessageCheckbox,
		"GetAttachments":                a.GetAttachments,
		"GetAutostart":                  a.GetAutostart,
		"GetComposerFormat":             a.GetComposerFormat,
		"GetComposerMode":               a.GetComposerMode,
		"GetContactPhotos":              a.GetContactPhotos,
		"GetConversation":               a.GetConversation,
		"GetDarkComposerBody":           a.GetDarkComposerBody,
		"GetDarkMailContent":            a.GetDarkMailContent,
		"GetFolders":                    a.GetFolders,
		"GetImageAllowlist":             a.GetImageAllowlist,
		"GetInlineAttachments":          a.GetInlineAttachments,
		"GetLanguage":                   a.GetLanguage,
		"GetMailtoMode":                 a.GetMailtoMode,
		"GetMarkAsReadDelay":            a.GetMarkAsReadDelay,
		"GetMessageListDensity":         a.GetMessageListDensity,
		"GetMessageListSortOrder":       a.GetMessageListSortOrder,
		"GetMessageSource":              a.GetMessageSource,
		"GetNativeTitleBar":             a.GetNativeTitleBar,
		"GetOpenReaderInTabPreference":  a.GetOpenReaderInTabPreference,
		"GetReadReceiptResponsePolicy":  a.GetReadReceiptResponsePolicy,
		"GetReplyContent":               a.GetReplyContent,
		"GetRunBackground":              a.GetRunBackground,
		"GetSenderLogos":                a.GetSenderLogos,
		"GetShowMessageListCircles":     a.GetShowMessageListCircles,
		"GetShowMessageListProfilePics": a.GetShowMessageListProfilePics,
		"GetShowTitleBar":               a.GetShowTitleBar,
		"GetShowViewerCircles":          a.GetShowViewerCircles,
		"GetSpellcheckCustomWords":      a.GetSpellcheckCustomWords,
		"GetSpellcheckEnabled":          a.GetSpellcheckEnabled,
		"GetSpellcheckLanguages":        a.GetSpellcheckLanguages,
		"GetStartHidden":                a.GetStartHidden,
		"GetSystemTheme":                a.GetSystemTheme,
		"GetThemeMode":                  a.GetThemeMode,
		"GetUIState":                    a.GetUIState,
		"IgnoreReadReceipt":             a.IgnoreReadReceipt,
		"MarkAsNotSpamWithUndo":         a.MarkAsNotSpamWithUndo,
		"MarkAsRead":                    a.MarkAsRead,
		"MarkAsSpamWithUndo":            a.MarkAsSpamWithUndo,
		"MarkAsUnread":                  a.MarkAsUnread,
		"MoveToFolderWithUndo":          a.MoveToFolderWithUndo,
		"MoveToInboxWithUndo":           a.MoveToInboxWithUndo,
		"OpenAttachment":                a.OpenAttachment,
		"OpenComposerWindow":            a.OpenComposerWindow,
		"OpenEncryptedAttachment":       a.OpenEncryptedAttachment,
		"OpenFile":                      a.OpenFile,
		"OpenFolder":                    a.OpenFolder,
		"OpenURL":                       a.OpenURL,
		"ProcessPGPMessage":             a.ProcessPGPMessage,
		"ProcessSMIMEMessage":           a.ProcessSMIMEMessage,
		"RemoveFromInboxWithUndo":       a.RemoveFromInboxWithUndo,
		"ResolveLinkViaCloudflareDNS":   a.ResolveLinkViaCloudflareDNS,
		"SaveAllAttachments":            a.SaveAllAttachments,
		"SaveAllEncryptedAttachments":   a.SaveAllEncryptedAttachments,
		"SaveAttachmentAs":              a.SaveAttachmentAs,
		"SaveEncryptedAttachmentAs":     a.SaveEncryptedAttachmentAs,
		"SendReadReceipt":               a.SendReadReceipt,
		"SetAlwaysLoadImages":           a.SetAlwaysLoadImages,
		"SetStripTrackingParams":        a.SetStripTrackingParams,
		"Star":                          a.Star,
		"TrashWithUndo":                 a.TrashWithUndo,
		"UndoOperation":                 a.UndoOperation,
		"Unstar":                        a.Unstar,
	}
}

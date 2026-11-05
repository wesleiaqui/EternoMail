package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"

	"github.com/google/uuid"
	"github.com/hkdb/aerion/internal/folder"
	"github.com/hkdb/aerion/internal/ipc"
	"github.com/hkdb/aerion/internal/logging"
	wailsRuntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

const mailWindowCallType = "mail_window_call"
const mailWindowEventType = "mail_window_event"

var mailWindowEvents = []string{
	"folder:synced", "folder:syncError", "folders:countsChanged", "messages:updated",
	"messages:readChanged", "messages:starredChanged", "messages:moved", "messages:deleted",
	"replyLater:changed",
	"sync:progress", "fts:progress", "fts:complete", "fts:indexing", "contacts:changed",
	"sent:synced", "network:online", "network:offline",
	"virtualfolder:cacheUpdated",
	"virtualfolder:refreshError",
	"virtualfolder:refreshComplete",
}

type mailWindowEvent struct {
	Name string `json:"name"`
	Data []any  `json:"data"`
}

func newMailWindowEvent(name string, data []any) (ipc.Message, error) {
	allowed := false
	for _, candidate := range mailWindowEvents {
		if name == candidate {
			allowed = true
			break
		}
	}
	if !allowed {
		return ipc.Message{}, errors.New("unsupported mail window event")
	}
	msg, err := ipc.NewMessage(mailWindowEventType, mailWindowEvent{Name: name, Data: data})
	if err != nil {
		return ipc.Message{}, err
	}
	if len(msg.Payload) > 256<<10 {
		return ipc.Message{}, errors.New("mail window event too large")
	}
	return msg, nil
}

func (a *App) bridgeMailWindowEvents() {
	log := logging.WithComponent("app.mail-window")
	for _, name := range mailWindowEvents {
		wailsRuntime.EventsOn(a.ctx, name, func(data ...interface{}) {
			msg, err := newMailWindowEvent(name, data)
			if err != nil {
				log.Warn().Err(err).Str("event", name).Msg("Skipped window event")
				return
			}
			if err := a.ipcServer.Broadcast(msg); err != nil {
				log.Debug().Err(err).Str("event", name).Msg("Window event unavailable")
			}
		})
	}
}

func validateMailWindowFolder(accountID string, f *folder.Folder) error {
	if f == nil {
		return errors.New("mail folder unavailable")
	}
	if f.AccountID != accountID {
		return errors.New("mail folder belongs to another account")
	}
	return nil
}

// OpenMailWindow opens an independent mail list using the existing parent services.
// Empty scope opens All Inboxes. It never starts another sync engine or database.
func (a *App) OpenMailWindow(accountID, folderID, folderName, folderType, threadID, threadAccountID, threadFolderID, searchQuery string) error {
	if a.ipcServer == nil || a.ipcTokenMgr == nil {
		return errors.New("multi-window service unavailable")
	}
	if accountID == "" || folderID == "" {
		accountID, folderID, folderName, folderType = "unified", "inbox", "", "inbox"
	}
	switch {
	case folderType == "virtual":
		if a.virtualFolderStore == nil {
			return errors.New("virtual folders unavailable")
		}
		view, err := a.virtualFolderStore.Get(folderID)
		if err != nil {
			return err
		}
		if view == nil {
			return errors.New("virtual folder unavailable")
		}
		folderName = view.Name
	case accountID == "unified":
		switch folderID {
		case "inbox", "sent", "drafts", "trash", "spam", "archive", "all", "starred", "replyLater", "readLater", "snoozed":
		default:
			return errors.New("unknown unified folder")
		}
		folderType = folderID
	default:
		if a.folderStore == nil {
			return errors.New("mail folders unavailable")
		}
		f, err := a.folderStore.Get(folderID)
		if err != nil {
			return err
		}
		if err := validateMailWindowFolder(accountID, f); err != nil {
			return err
		}
		folderName, folderType = f.Name, string(f.Type)
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(executable, "--mail-window", "--account", accountID, "--reader-folder", folderID, "--reader-folder-name", folderName, "--reader-thread", threadID, "--reader-thread-account", threadAccountID, "--reader-thread-folder", threadFolderID, "--reader-search", searchQuery, "--reader-folder-type", folderType, "--ipc-address", a.ipcServer.Address())
	var cacheDir string
	if a.paths != nil {
		cacheDir = a.paths.Cache
	}
	// Each child's log has a unique name so concurrent windows cannot truncate it.
	childLog := detachedChildLogFile(cacheDir, "mail-"+uuid.NewString(), cmd)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		if childLog != nil {
			_ = childLog.Close()
		}
		return err
	}
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		if childLog != nil {
			_ = childLog.Close()
		}
		return fmt.Errorf("open mail window: %w", err)
	}
	go func() {
		defer recoverPanic("app.mail-window", "authenticate child")
		defer stdin.Close()
		if _, err := io.WriteString(stdin, a.ipcTokenMgr.GetToken()); err != nil {
			_ = cmd.Process.Kill()
		}
	}()
	go func() {
		defer recoverPanic("app.mail-window", "wait child")
		log := logging.WithComponent("app.mail-window")
		defer func() {
			if childLog != nil {
				_ = childLog.Close()
			}
		}()
		if err := cmd.Wait(); err != nil {
			log.Debug().Err(err).Msg("Mail window exited")
		}
	}()
	return nil
}

func (a *App) mailWindowMethods() map[string]any {
	methods := a.readerMethods()
	for name, method := range map[string]any{
		"GetMessage":                    a.GetMessage,
		"GetFolderTree":                 a.GetFolderTree,
		"GetConversations":              a.GetConversations,
		"GetConversationCount":          a.GetConversationCount,
		"GetUnifiedFolderConversations": a.GetUnifiedFolderConversations,
		"GetUnifiedFolderCount":         a.GetUnifiedFolderCount,
		"GetVirtualFolders":             a.GetVirtualFolders,
		"GetVirtualFolderUnreadCounts":  a.GetVirtualFolderUnreadCounts,
		"GetReplyLaterQueue":            a.GetReplyLaterQueue,
		"ChangeReplyLaterWithUndo":      a.ChangeReplyLaterWithUndo,
		"GetReplyLaterCount":            a.GetReplyLaterCount,
		"IsReplyLater":                  a.IsReplyLater,
		"AddReplyLater":                 a.AddReplyLater,
		"RemoveReplyLater":              a.RemoveReplyLater,
		"GetVirtualFolderConversations": a.GetVirtualFolderConversations,
		"GetVirtualFolderCount":         a.GetVirtualFolderCount,
		"GetVirtualFolderCachedView":    a.GetVirtualFolderCachedView,
		"CreateVirtualFolder":           a.CreateVirtualFolder,
		"SearchMail":                    a.SearchMail,
		"CancelMailSearch":              a.CancelMailSearch,
		"SearchConversations":           a.SearchConversations,
		"SearchUnifiedFolder":           a.SearchUnifiedFolder,
		"SearchVirtualFolder":           a.SearchVirtualFolder,
		"GetSearchCount":                a.GetSearchCount,
		"GetSearchCountUnifiedFolder":   a.GetSearchCountUnifiedFolder,
		"IMAPSearchFolder":              a.IMAPSearchFolder,
		"IMAPSearchUnifiedInbox":        a.IMAPSearchUnifiedInbox,
		"FetchServerMessage":            a.FetchServerMessage,
		"GetFTSIndexStatus":             a.GetFTSIndexStatus,
		"IsFTSIndexing":                 a.IsFTSIndexing,
		"RefreshViewHeaders":            a.RefreshViewHeaders,
		"SyncVirtualFolder":             a.SyncVirtualFolder,
		"SyncFolder":                    a.SyncFolder,
		"ForceSyncFolder":               a.ForceSyncFolder,
		"CancelFolderSync":              a.CancelFolderSync,
		"SyncUnifiedFolder":             a.SyncUnifiedFolder,
		"EmptyTrash":                    a.EmptyTrash,
		"EmptyUnifiedTrash":             a.EmptyUnifiedTrash,
		"OpenReaderWindow":              a.OpenReaderWindow,
		"OpenMailWindow":                a.OpenMailWindow,
	} {
		methods[name] = method
	}
	return methods
}

func mailWindowEventFromMessage(msg ipc.Message) (mailWindowEvent, error) {
	var event mailWindowEvent
	if len(msg.Payload) > 256<<10 {
		return event, errors.New("mail window event too large")
	}
	if err := json.Unmarshal(msg.Payload, &event); err != nil {
		return event, err
	}
	_, err := newMailWindowEvent(event.Name, event.Data)
	return event, err
}

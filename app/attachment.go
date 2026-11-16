package app

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	automationclient "github.com/hkdb/aerion/internal/automation/client"
	v1 "github.com/hkdb/aerion/internal/automation/v1"
	"github.com/hkdb/aerion/internal/email"
	"github.com/hkdb/aerion/internal/gmail"
	"github.com/hkdb/aerion/internal/logging"
	"github.com/hkdb/aerion/internal/message"
	"github.com/hkdb/aerion/internal/pgp"
	"github.com/hkdb/aerion/internal/platform"
	"github.com/hkdb/aerion/internal/smime"
	wailsRuntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// ============================================================================
// Attachment API - Exposed to frontend via Wails bindings
// ============================================================================

// GetAttachments returns all attachments for a message
func (a *App) GetAttachments(messageID string) ([]*message.Attachment, error) {
	if a.attachmentStore == nil {
		return nil, errors.New("attachment reads are not available from the remote mailbox service")
	}
	return a.attachmentStore.GetByMessage(messageID)
}

// GetMessageAttachments retrieves metadata using the owning account when the
// desktop is connected directly to a remote mailbox helper.
func (a *App) GetMessageAttachments(accountID, messageID string) ([]*message.Attachment, error) {
	if !a.remoteServiceMode {
		// Repair only on the mailbox-owning app. A failed strict fetch/parse
		// leaves every old row intact; never hide attachments on a repair error.
		if a.attachmentStore != nil {
			items, err := a.attachmentStore.GetByMessage(messageID)
			if err != nil {
				return nil, err
			}
			if hasRepeatedAttachmentMetadata(items) {
				_, attempted := a.attachmentRepairAttempts.LoadOrStore(accountID+"\x00"+messageID, true)
				if !attempted {
					if _, err := a.RepairMessageAttachments(accountID, messageID); err != nil {
						log := logging.WithComponent("attachment_repair")
						log.Warn().Err(err).Msg("Attachment cache repair deferred; original cache retained")
					}
				}
			}
		}
		return a.GetAttachments(messageID)
	}
	client, err := a.remoteAttachmentClient(a.accountContext())
	if err != nil {
		return nil, err
	}
	items, err := client.ListAttachments(a.accountContext(), v1.MessageRef{AccountID: accountID, MessageID: messageID})
	if err != nil {
		return nil, err
	}
	result := make([]*message.Attachment, 0, len(items))
	for _, item := range items {
		result = append(result, &message.Attachment{ID: item.ID, MessageID: messageID, Filename: item.Filename,
			ContentType: item.ContentType, Size: item.Size, ContentID: item.ContentID, IsInline: item.IsInline})
	}
	return result, nil
}

// GetAttachment returns a single attachment by ID
func (a *App) GetAttachment(attachmentID string) (*message.Attachment, error) {
	if a.attachmentStore == nil {
		return nil, errors.New("attachment reads are not available from the remote mailbox service")
	}
	return a.attachmentStore.Get(attachmentID)
}

// GetInlineAttachments returns a map of content-id to data URL for all inline attachments
// This is used to resolve cid: references in HTML email bodies
// Content is read from the database (stored during sync) for fast offline access
func (a *App) GetInlineAttachments(messageID string) (map[string]string, error) {
	if a.attachmentStore == nil {
		return nil, errors.New("inline attachment reads are not available from the remote mailbox service")
	}
	log := logging.WithComponent("app")

	log.Info().Str("message_ref", logging.ShortHash(messageID)).Msg("GetInlineAttachments called")

	// Get inline attachments with content from database
	// This is fast and works offline since content is stored during sync
	result, err := a.attachmentStore.GetInlineByMessage(messageID)
	if err != nil {
		log.Error().Err(err).Str("message_ref", logging.ShortHash(messageID)).Msg("Failed to get inline attachments from database")
		return nil, fmt.Errorf("failed to get inline attachments: %w", err)
	}

	log.Info().Int("count", len(result)).Str("message_ref", logging.ShortHash(messageID)).Msg("Returning inline attachments")

	return result, nil
}

// GetInlineAttachmentsForAccount explicitly fetches bounded inline bytes from
// the helper for a remote-only desktop. Non-inline file bytes are never read.
func (a *App) GetInlineAttachmentsForAccount(accountID, messageID string) (map[string]string, error) {
	if !a.remoteServiceMode {
		return a.GetInlineAttachments(messageID)
	}
	ctx := a.accountContext()
	client, err := a.remoteAttachmentClient(ctx)
	if err != nil {
		return nil, err
	}
	ref := v1.MessageRef{AccountID: accountID, MessageID: messageID}
	items, err := client.ListAttachments(ctx, ref)
	if err != nil {
		return nil, err
	}
	inline := make([]v1.AttachmentSummary, 0, len(items))
	totalSize := 0
	const inlineByteLimit = 16 << 20
	for _, item := range items {
		if !item.IsInline || item.ContentID == "" {
			continue
		}
		if item.Size < 0 || item.Size > inlineByteLimit || totalSize > inlineByteLimit-item.Size {
			return nil, errors.New("inline attachment set exceeds the remote rendering limit")
		}
		totalSize += item.Size
		inline = append(inline, item)
		if len(inline) > 32 {
			return nil, errors.New("message has too many inline attachments for remote rendering")
		}
	}
	result := make(map[string]string, len(inline))
	if len(inline) == 0 {
		return result, nil
	}
	actualBytes := 0
	type readResult struct {
		item        v1.AttachmentSummary
		content     []byte
		contentType string
		err         error
	}
	workers := 4
	if len(inline) < workers {
		workers = len(inline)
	}
	jobs := make(chan v1.AttachmentSummary)
	results := make(chan readResult, len(inline))
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for item := range jobs {
				content, contentType, err := client.ReadAttachmentBytes(ctx, ref, item.ID)
				results <- readResult{item: item, content: content, contentType: contentType, err: err}
			}
		}()
	}
	go func() {
		for _, item := range inline {
			jobs <- item
		}
		close(jobs)
		wg.Wait()
		close(results)
	}()
	for read := range results {
		if read.err != nil {
			return nil, fmt.Errorf("read inline attachment %q: %w", read.item.ID, read.err)
		}
		actualBytes += len(read.content)
		if len(read.content) > inlineByteLimit || actualBytes > inlineByteLimit {
			return nil, errors.New("inline attachment bytes exceed the remote rendering limit")
		}
		contentType := read.contentType
		if contentType == "" || contentType == "application/octet-stream" {
			contentType = read.item.ContentType
		}
		if contentType == "" {
			contentType = "application/octet-stream"
		}
		result[read.item.ContentID] = "data:" + contentType + ";base64," + base64.StdEncoding.EncodeToString(read.content)
	}
	return result, nil
}

func (a *App) remoteAttachmentClient(ctx context.Context) (*automationclient.Client, error) {
	if a.mailboxService == nil {
		return nil, errors.New("remote mailbox service is not configured")
	}
	status := a.mailboxService.status(ctx)
	if !status.Connected {
		return nil, fmt.Errorf("remote mailbox service is unavailable: %s", status.Message)
	}
	if !hasStatusCapability(status.Capabilities, string(v1.CapabilityAttachmentRead)) {
		return nil, errors.New("remote mailbox service requires mail.attachment.read authorization")
	}
	return a.mailboxService.getClient()
}

// DownloadAttachment downloads an attachment and saves it to disk
// If savePath is empty, saves to the default attachments directory
// Returns the path where the file was saved
func (a *App) DownloadAttachment(attachmentID, savePath string) (string, error) {
	if a.attachmentStore == nil {
		return "", errors.New("attachment downloads are not available from the remote mailbox service")
	}
	log := logging.WithComponent("app")

	log.Debug().Str("attachment_id", attachmentID).Bool("custom_path", savePath != "").Msg("DownloadAttachment called")

	// Get attachment metadata
	att, err := a.attachmentStore.Get(attachmentID)
	if err != nil {
		log.Error().Err(err).Msg("Failed to get attachment from store")
		return "", fmt.Errorf("failed to get attachment: %w", err)
	}
	if att == nil {
		log.Error().Str("attachmentID", attachmentID).Msg("Attachment not found")
		return "", fmt.Errorf("attachment not found: %s", attachmentID)
	}

	log.Debug().Int("size", att.Size).Msg("Got attachment metadata")

	// Check if already downloaded (only for default location, not custom paths)
	if savePath == "" && att.LocalPath != "" {
		if _, err := os.Stat(att.LocalPath); err == nil {
			log.Debug().Msg("Attachment already downloaded")
			return att.LocalPath, nil
		}
	}

	// Get the message to find folder and UID
	msg, err := a.messageStore.Get(att.MessageID)
	if err != nil {
		log.Error().Err(err).Str("message_ref", logging.ShortHash(att.MessageID)).Msg("Failed to get message")
		return "", fmt.Errorf("failed to get message: %w", err)
	}
	if msg == nil {
		log.Error().Str("message_ref", logging.ShortHash(att.MessageID)).Msg("Message not found")
		return "", fmt.Errorf("message not found: %s", att.MessageID)
	}

	log.Debug().Uint32("uid", msg.UID).Str("folderID", msg.FolderID).Msg("Got message info")

	downloader := email.NewAttachmentDownloader(a.paths.AttachmentsPath())
	content, err := a.syncEngine.FetchGmailAttachment(a.ctx, msg.AccountID, att.ID)
	if errors.Is(err, gmail.ErrNotCapable) {

		// Fetch raw message from IMAP
		raw, err := a.syncEngine.FetchRawMessage(a.ctx, msg.AccountID, msg.FolderID, msg.UID)
		if err != nil {
			log.Error().Err(err).Msg("Failed to fetch raw message from IMAP")
			return "", fmt.Errorf("failed to fetch message: %w", err)
		}

		log.Debug().Int("rawSize", len(raw)).Msg("Fetched raw message from IMAP")

		// Extract attachment content
		content, err = a.storedAttachmentContent(downloader, raw, att)
		if err != nil {
			log.Error().Err(err).Msg("Failed to extract attachment content")
			return "", fmt.Errorf("failed to extract attachment: %w", err)
		}

	} else if err != nil {
		return "", fmt.Errorf("failed to fetch Gmail attachment: %w", err)
	}

	log.Debug().Int("contentSize", len(content)).Msg("Extracted attachment content")

	// Save to disk
	localPath, err := downloader.SaveAttachment(att, content, savePath)
	if err != nil {
		log.Error().Err(err).Bool("custom_path", savePath != "").Msg("Failed to save attachment to disk")
		return "", fmt.Errorf("failed to save attachment: %w", err)
	}

	// Update attachment record with local path (only for default location)
	if savePath == "" {
		if err := a.attachmentStore.UpdateLocalPath(attachmentID, localPath); err != nil {
			log.Warn().Err(err).Msg("Failed to update attachment local path")
		}
	}

	log.Info().Int("size", len(content)).Msg("Attachment downloaded")
	return localPath, nil
}

// DownloadAttachmentForAccount downloads one message attachment with an
// account-qualified helper reference when running in remote-only mode.
func (a *App) DownloadAttachmentForAccount(accountID, messageID, attachmentID, savePath string) (string, error) {
	if !a.remoteServiceMode {
		return a.DownloadAttachment(attachmentID, savePath)
	}
	ctx := a.accountContext()
	selected, content, err := a.readRemoteAttachment(ctx, accountID, messageID, attachmentID)
	if err != nil {
		return "", err
	}
	downloader := email.NewAttachmentDownloader(a.paths.AttachmentsPath())
	return downloader.SaveAttachment(selected, content, savePath)
}

func (a *App) readRemoteAttachment(ctx context.Context, accountID, messageID, attachmentID string) (*message.Attachment, []byte, error) {
	client, err := a.remoteAttachmentClient(ctx)
	if err != nil {
		return nil, nil, err
	}
	ref := v1.MessageRef{AccountID: accountID, MessageID: messageID}
	items, err := client.ListAttachments(ctx, ref)
	if err != nil {
		return nil, nil, err
	}
	var selected *message.Attachment
	for _, item := range items {
		if item.ID == attachmentID {
			selected = &message.Attachment{ID: item.ID, MessageID: messageID, Filename: item.Filename,
				ContentType: item.ContentType, Size: item.Size, ContentID: item.ContentID, IsInline: item.IsInline}
			break
		}
	}
	if selected == nil {
		return nil, nil, fmt.Errorf("attachment %q does not belong to message %q", attachmentID, messageID)
	}
	content, _, err := client.ReadAttachmentBytes(ctx, ref, attachmentID)
	if err != nil {
		return nil, nil, err
	}
	return selected, content, nil
}

// SaveAllAttachmentsForAccount explicitly fetches each selected attachment from
// the helper and writes it through the existing safe attachment downloader.
func (a *App) SaveAllAttachmentsForAccount(accountID, messageID string) (string, error) {
	if !a.remoteServiceMode {
		return a.SaveAllAttachments(messageID)
	}
	attachments, err := a.GetMessageAttachments(accountID, messageID)
	if err != nil {
		return "", err
	}
	if len(attachments) == 0 {
		return "", errors.New("no attachments found for message")
	}
	homeDir, err := os.UserHomeDir()
	if err != nil {
		homeDir = ""
	}
	defaultDir := filepath.Join(homeDir, "Downloads")
	downloader := email.NewAttachmentDownloader(a.paths.AttachmentsPath())
	if platform.IsFlatpak() {
		filenames := make([]string, len(attachments))
		for i, attachment := range attachments {
			filenames[i] = attachment.Filename
		}
		savePaths, err := platform.PortalSaveFiles("Save All Attachments", filenames, defaultDir)
		if err != nil {
			return "", fmt.Errorf("failed to show save dialog: %w", err)
		}
		if len(savePaths) == 0 {
			return "", nil
		}
		for i, attachment := range attachments {
			if i >= len(savePaths) {
				break
			}
			selected, content, err := a.readRemoteAttachment(a.accountContext(), accountID, messageID, attachment.ID)
			if err != nil {
				return filepath.Dir(savePaths[0]), fmt.Errorf("save all attachments stopped after %d files: %w", i, err)
			}
			if _, err := downloader.SaveAttachment(selected, content, savePaths[i]); err != nil {
				return filepath.Dir(savePaths[0]), fmt.Errorf("save all attachments stopped after %d files: %w", i, err)
			}
		}
		return filepath.Dir(savePaths[0]), nil
	}
	saveDir, err := wailsRuntime.OpenDirectoryDialog(a.ctx, wailsRuntime.OpenDialogOptions{
		DefaultDirectory: defaultDir,
		Title:            "Save All Attachments",
	})
	if err != nil || saveDir == "" {
		return "", err
	}
	for i, attachment := range attachments {
		selected, content, err := a.readRemoteAttachment(a.accountContext(), accountID, messageID, attachment.ID)
		if err != nil {
			return saveDir, fmt.Errorf("save all attachments stopped after %d files: %w", i, err)
		}
		if _, err := downloader.SaveAttachmentToDirectory(selected, content, saveDir); err != nil {
			return saveDir, fmt.Errorf("save all attachments stopped after %d files: %w", i, err)
		}
	}
	return saveDir, nil
}

// OpenAttachment downloads (if needed) and opens an attachment with the default application
func (a *App) OpenAttachment(attachmentID string) error {
	// Download if not already downloaded
	localPath, err := a.DownloadAttachment(attachmentID, "")
	if err != nil {
		return err
	}

	// Open with default application using runtime
	return a.openFile(localPath)
}

// OpenAttachmentForAccount opens an attachment after explicitly fetching it
// from its owning helper account when configured for remote-only mode.
func (a *App) OpenAttachmentForAccount(accountID, messageID, attachmentID string) error {
	path, err := a.DownloadAttachmentForAccount(accountID, messageID, attachmentID, "")
	if err != nil {
		return err
	}
	return a.openFile(path)
}

// SaveAttachmentAs shows a Save As dialog and saves the attachment to the user-selected location
// Returns the path where the file was saved, or empty string if cancelled
func (a *App) SaveAttachmentAs(attachmentID string) (string, error) {
	log := logging.WithComponent("app")

	log.Debug().Str("attachmentID", attachmentID).Msg("SaveAttachmentAs called")

	// Get attachment metadata for the filename
	att, err := a.attachmentStore.Get(attachmentID)
	if err != nil {
		log.Error().Err(err).Str("attachmentID", attachmentID).Msg("Failed to get attachment metadata")
		return "", fmt.Errorf("failed to get attachment: %w", err)
	}
	if att == nil {
		log.Error().Str("attachmentID", attachmentID).Msg("Attachment not found in database")
		return "", fmt.Errorf("attachment not found: %s", attachmentID)
	}

	log.Debug().Str("message_ref", logging.ShortHash(att.MessageID)).Msg("Found attachment metadata")

	// Get user's home directory for default save location
	homeDir, err := os.UserHomeDir()
	if err != nil {
		homeDir = ""
	}
	defaultDir := filepath.Join(homeDir, "Downloads")

	// In Flatpak, use portal save dialog (Wails GTK dialog doesn't route through portal)
	if platform.IsFlatpak() {
		savePath, err := platform.PortalSaveFile("Save Attachment", att.Filename, defaultDir)
		if err != nil {
			log.Error().Err(err).Msg("Failed to show portal save dialog")
			return "", fmt.Errorf("failed to show save dialog: %w", err)
		}
		if savePath == "" {
			log.Debug().Msg("User cancelled save dialog")
			return "", nil
		}
		return a.DownloadAttachment(attachmentID, savePath)
	}

	// Native: use Wails dialog
	savePath, err := wailsRuntime.SaveFileDialog(a.ctx, wailsRuntime.SaveDialogOptions{
		DefaultDirectory: defaultDir,
		DefaultFilename:  att.Filename,
		Title:            "Save Attachment",
	})
	if err != nil {
		log.Error().Err(err).Msg("Failed to show save dialog")
		return "", fmt.Errorf("failed to show save dialog: %w", err)
	}

	log.Debug().Str("savePath", savePath).Msg("User selected save path")

	// User cancelled the dialog
	if savePath == "" {
		log.Debug().Msg("User cancelled save dialog")
		return "", nil
	}

	// Download and save to the selected path
	resultPath, err := a.DownloadAttachment(attachmentID, savePath)
	if err != nil {
		log.Error().Err(err).Str("savePath", savePath).Msg("Failed to download attachment")
		return "", err
	}

	log.Info().Str("attachment", att.Filename).Str("path", resultPath).Msg("Attachment saved")
	return resultPath, nil
}

// SaveAttachmentAsForAccount opens the native save destination dialog using
// helper-owned metadata, then fetches only the chosen attachment bytes.
func (a *App) SaveAttachmentAsForAccount(accountID, messageID, attachmentID string) (string, error) {
	if !a.remoteServiceMode {
		return a.SaveAttachmentAs(attachmentID)
	}
	ctx := a.accountContext()
	client, err := a.remoteAttachmentClient(ctx)
	if err != nil {
		return "", err
	}
	items, err := client.ListAttachments(ctx, v1.MessageRef{AccountID: accountID, MessageID: messageID})
	if err != nil {
		return "", err
	}
	var attachment *v1.AttachmentSummary
	for i := range items {
		if items[i].ID == attachmentID {
			attachment = &items[i]
			break
		}
	}
	if attachment == nil {
		return "", fmt.Errorf("attachment %q does not belong to message %q", attachmentID, messageID)
	}
	homeDir, err := os.UserHomeDir()
	if err != nil {
		homeDir = ""
	}
	defaultDir := filepath.Join(homeDir, "Downloads")
	if platform.IsFlatpak() {
		savePath, err := platform.PortalSaveFile("Save Attachment", attachment.Filename, defaultDir)
		if err != nil {
			return "", fmt.Errorf("failed to show save dialog: %w", err)
		}
		if savePath == "" {
			return "", nil
		}
		return a.DownloadAttachmentForAccount(accountID, messageID, attachmentID, savePath)
	}
	savePath, err := wailsRuntime.SaveFileDialog(a.ctx, wailsRuntime.SaveDialogOptions{
		DefaultDirectory: defaultDir,
		DefaultFilename:  attachment.Filename,
		Title:            "Save Attachment",
	})
	if err != nil {
		return "", fmt.Errorf("failed to show save dialog: %w", err)
	}
	if savePath == "" {
		return "", nil
	}
	return a.DownloadAttachmentForAccount(accountID, messageID, attachmentID, savePath)
}

// openFile opens a file with the system default application after validating the path
func (a *App) openFile(path string) error {
	if err := a.validateOpenPath(path); err != nil {
		return err
	}

	if runtime.GOOS == "linux" && platform.IsFlatpak() {
		if platform.IsDocPortalPath(path) {
			wailsRuntime.EventsEmit(a.ctx, "flatpak:filesystem-dialog")
			return nil // Don't open — portal FUSE path is broken for editing
		}
		return platform.PortalOpenFile(path)
	}

	var cmd *exec.Cmd

	switch runtime.GOOS {
	case "linux":
		cmd = exec.Command("xdg-open", path)
	case "darwin":
		cmd = exec.Command("open", path)
	case "windows":
		// ShellExecute, not `cmd /c start`: cmd re-parses `&`/`|`/`^` in the
		// path as command separators, so an attachment filename like
		// `x&calc.pdf` would inject commands (same class as issue #261).
		// ShellExecute passes the path as a single arg with no shell re-parse.
		return platform.OpenPathWindows(path)
	default:
		return fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}

	return cmd.Start()
}

// validateOpenPath checks that the path is under an allowed root directory and
// does not trigger CLI option injection or control character attacks in system openers.
func (a *App) validateOpenPath(path string) error {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" || strings.HasPrefix(trimmed, "-") || strings.ContainsAny(trimmed, "\r\n\x00") {
		return fmt.Errorf("invalid path: %q", path)
	}

	absPath, err := filepath.Abs(trimmed)
	if err != nil {
		return fmt.Errorf("invalid path: %w", err)
	}
	absPath = filepath.Clean(absPath)

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}

	var attachmentsPath, dataPath string
	if a.paths != nil {
		attachmentsPath = a.paths.AttachmentsPath()
		dataPath = a.paths.Data
	}

	allowedRoots := []string{
		filepath.Join(homeDir, "Downloads"),
	}
	if attachmentsPath != "" {
		allowedRoots = append(allowedRoots, attachmentsPath)
	}
	if dataPath != "" {
		allowedRoots = append(allowedRoots, dataPath)
	}

	var matchedRoot string
	for _, root := range allowedRoots {
		cleanRoot := filepath.Clean(root) + string(filepath.Separator)
		if strings.HasPrefix(absPath, cleanRoot) || absPath == filepath.Clean(root) {
			matchedRoot = filepath.Clean(root)
			break
		}
	}
	if matchedRoot == "" {
		return fmt.Errorf("path %q is outside allowed directories", path)
	}

	// Reject any path component beginning with '-' relative to the root to prevent CLI flag injection
	rel, err := filepath.Rel(matchedRoot, absPath)
	if err != nil {
		return fmt.Errorf("invalid path relative to root: %w", err)
	}
	for _, comp := range strings.Split(rel, string(filepath.Separator)) {
		if strings.HasPrefix(comp, "-") {
			return fmt.Errorf("invalid path component %q starting with dash", comp)
		}
	}

	return nil
}

// OpenFile opens a file with the system default application (exposed to frontend)
func (a *App) OpenFile(path string) error {
	return a.openFile(path)
}

// OpenFolder opens the folder containing a file in the system file manager
func (a *App) OpenFolder(path string) error {
	if err := a.validateOpenPath(path); err != nil {
		return err
	}

	// In Flatpak, use the OpenURI portal to resolve sandboxed paths correctly
	if runtime.GOOS == "linux" && platform.IsFlatpak() {
		return platform.PortalOpenDirectory(path)
	}

	var cmd *exec.Cmd

	switch runtime.GOOS {
	case "linux":
		dir := filepath.Dir(path)
		// Ensure resolved directory is validated against option injection
		if strings.HasPrefix(filepath.Clean(dir), "-") || strings.HasPrefix(filepath.Base(dir), "-") {
			return fmt.Errorf("invalid directory path: %q", dir)
		}
		cmd = exec.Command("xdg-open", dir)
	case "darwin":
		// -R reveals the file in Finder
		cmd = exec.Command("open", "-R", path)
	case "windows":
		// /select highlights the file in Explorer
		cmd = exec.Command("explorer", "/select,", path)
	default:
		return fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}

	return cmd.Start()
}

// SaveAllAttachments shows a folder picker and saves all attachments from a message to that folder
// Returns the folder path where files were saved, or empty string if cancelled
func (a *App) SaveAllAttachments(messageID string) (string, error) {
	log := logging.WithComponent("app")

	// Get all attachments for the message
	attachments, err := a.attachmentStore.GetByMessage(messageID)
	if err != nil {
		return "", fmt.Errorf("failed to get attachments: %w", err)
	}
	if len(attachments) == 0 {
		return "", fmt.Errorf("no attachments found for message")
	}

	// Get user's home directory for default save location
	homeDir, err := os.UserHomeDir()
	if err != nil {
		homeDir = ""
	}
	defaultDir := filepath.Join(homeDir, "Downloads")

	// In Flatpak, use portal save dialog (Wails GTK dialog doesn't route through portal)
	if platform.IsFlatpak() {
		return a.saveAllAttachmentsViaPortal(messageID, attachments, defaultDir)
	}

	// Native: use Wails folder dialog
	saveDir, err := wailsRuntime.OpenDirectoryDialog(a.ctx, wailsRuntime.OpenDialogOptions{
		DefaultDirectory: defaultDir,
		Title:            "Save All Attachments",
	})
	if err != nil {
		return "", fmt.Errorf("failed to show folder dialog: %w", err)
	}

	// User cancelled the dialog
	if saveDir == "" {
		return "", nil
	}

	// Get the message to find folder and UID
	msg, err := a.messageStore.Get(messageID)
	if err != nil {
		return "", fmt.Errorf("failed to get message: %w", err)
	}
	if msg == nil {
		return "", fmt.Errorf("message not found: %s", messageID)
	}

	// Fetch raw message from IMAP
	raw, err := a.syncEngine.FetchRawMessage(a.ctx, msg.AccountID, msg.FolderID, msg.UID)
	if err != nil {
		return "", fmt.Errorf("failed to fetch message: %w", err)
	}

	// Save each attachment
	downloader := email.NewAttachmentDownloader(a.paths.AttachmentsPath())
	savedCount := 0

	for _, att := range attachments {
		content, err := a.storedAttachmentContent(downloader, raw, att)
		if err != nil {
			log.Warn().Err(err).Str("filename", att.Filename).Msg("Failed to extract attachment")
			continue
		}

		_, err = downloader.SaveAttachmentToDirectory(att, content, saveDir)
		if err != nil {
			log.Warn().Err(err).Str("filename", att.Filename).Msg("Failed to save attachment")
			continue
		}
		savedCount++
	}

	log.Info().Int("count", savedCount).Str("folder", saveDir).Msg("Saved all attachments")
	return saveDir, nil
}

// decryptMessageBody decrypts an encrypted message's raw body and returns the inner plaintext bytes.
// Handles both S/MIME and PGP, and unwraps any inner signature layer.
func (a *App) decryptMessageBody(msg *message.Message) ([]byte, error) {
	// Determine the recipient identity email for targeted decryption
	recipientEmail := a.findRecipientIdentityEmail(msg)

	// Try S/MIME first
	if msg.HasSMIME {
		rawBody, err := a.messageStore.GetSMIMERawBody(msg.ID)
		if err != nil {
			return nil, fmt.Errorf("failed to get S/MIME raw body: %w", err)
		}
		if rawBody == nil {
			return nil, fmt.Errorf("no S/MIME raw body for message: %s", msg.ID)
		}

		innerBytes := rawBody
		if msg.SMIMEEncrypted {
			decrypted, _, decErr := a.smimeDecryptor.DecryptMessage(msg.AccountID, recipientEmail, rawBody)
			if decErr != nil {
				return nil, fmt.Errorf("S/MIME decryption failed: %w", decErr)
			}
			innerBytes = decrypted
		}

		// Unwrap signature if present
		ct := extractContentType(innerBytes)
		if smime.IsSMIMESigned(ct) {
			_, unwrapped := a.smimeVerifier.VerifyAndUnwrap(innerBytes)
			if unwrapped != nil {
				innerBytes = unwrapped
			}
		}

		return innerBytes, nil
	}

	// Try PGP
	if msg.HasPGP {
		rawBody, err := a.messageStore.GetPGPRawBody(msg.ID)
		if err != nil {
			return nil, fmt.Errorf("failed to get PGP raw body: %w", err)
		}
		if rawBody == nil {
			return nil, fmt.Errorf("no PGP raw body for message: %s", msg.ID)
		}

		innerBytes := rawBody
		if msg.PGPEncrypted {
			decrypted, _, decErr := a.pgpDecryptor.DecryptMessage(msg.AccountID, recipientEmail, rawBody)
			if decErr != nil {
				return nil, fmt.Errorf("PGP decryption failed: %w", decErr)
			}
			innerBytes = decrypted
		}

		// Unwrap signature if present
		ct := extractContentType(innerBytes)
		if pgp.IsPGPSigned(ct) {
			_, unwrapped := a.pgpVerifier.VerifyAndUnwrap(innerBytes)
			if unwrapped != nil {
				innerBytes = unwrapped
			}
		}

		return innerBytes, nil
	}

	return nil, fmt.Errorf("message %s is not encrypted", msg.ID)
}

// DownloadEncryptedAttachment decrypts an encrypted message, extracts a specific attachment,
// and saves it to disk. Returns the path where the file was saved.
func (a *App) DownloadEncryptedAttachment(messageID, filename, savePath string) (string, error) {
	log := logging.WithComponent("app")
	log.Debug().Str("message_ref", logging.ShortHash(messageID)).Msg("DownloadEncryptedAttachment called")

	msg, err := a.messageStore.Get(messageID)
	if err != nil {
		return "", fmt.Errorf("failed to get message: %w", err)
	}
	if msg == nil {
		return "", fmt.Errorf("message not found: %s", messageID)
	}

	// Decrypt and unwrap
	innerBytes, err := a.decryptMessageBody(msg)
	if err != nil {
		return "", fmt.Errorf("failed to decrypt message: %w", err)
	}

	// Extract attachment content from the decrypted body
	downloader := email.NewAttachmentDownloader(a.paths.AttachmentsPath())
	content, err := downloader.ExtractAttachmentContent(innerBytes, filename)
	if err != nil {
		return "", fmt.Errorf("failed to extract attachment from decrypted message: %w", err)
	}

	// Create a temporary attachment record for SaveAttachment
	att := &message.Attachment{
		Filename:    filename,
		ContentType: "application/octet-stream",
		Size:        len(content),
	}

	localPath, err := downloader.SaveAttachment(att, content, savePath)
	if err != nil {
		return "", fmt.Errorf("failed to save attachment: %w", err)
	}

	log.Info().Int("size", len(content)).Msg("Encrypted attachment downloaded")
	return localPath, nil
}

// SaveEncryptedAttachmentAs shows a Save As dialog and saves an attachment from an encrypted message.
// Returns the path where the file was saved, or empty string if cancelled.
func (a *App) SaveEncryptedAttachmentAs(messageID, filename string) (string, error) {
	log := logging.WithComponent("app")
	log.Debug().Str("message_ref", logging.ShortHash(messageID)).Msg("SaveEncryptedAttachmentAs called")

	homeDir, err := os.UserHomeDir()
	if err != nil {
		homeDir = ""
	}
	defaultDir := filepath.Join(homeDir, "Downloads")

	// In Flatpak, use portal save dialog (Wails GTK dialog doesn't route through portal)
	if platform.IsFlatpak() {
		savePath, err := platform.PortalSaveFile("Save Attachment", filename, defaultDir)
		if err != nil {
			return "", fmt.Errorf("failed to show save dialog: %w", err)
		}
		if savePath == "" {
			return "", nil
		}
		return a.DownloadEncryptedAttachment(messageID, filename, savePath)
	}

	// Native: use Wails dialog
	savePath, err := wailsRuntime.SaveFileDialog(a.ctx, wailsRuntime.SaveDialogOptions{
		DefaultDirectory: defaultDir,
		DefaultFilename:  filename,
		Title:            "Save Attachment",
	})
	if err != nil {
		return "", fmt.Errorf("failed to show save dialog: %w", err)
	}
	if savePath == "" {
		return "", nil
	}

	return a.DownloadEncryptedAttachment(messageID, filename, savePath)
}

// OpenEncryptedAttachment decrypts an encrypted message, extracts and opens an attachment.
func (a *App) OpenEncryptedAttachment(messageID, filename string) error {
	localPath, err := a.DownloadEncryptedAttachment(messageID, filename, "")
	if err != nil {
		return err
	}
	return a.openFile(localPath)
}

// SaveAllEncryptedAttachments shows a folder picker and saves all attachments from an encrypted message.
// Returns the folder path where files were saved, or empty string if cancelled.
func (a *App) SaveAllEncryptedAttachments(messageID string) (string, error) {
	log := logging.WithComponent("app")

	msg, err := a.messageStore.Get(messageID)
	if err != nil {
		return "", fmt.Errorf("failed to get message: %w", err)
	}
	if msg == nil {
		return "", fmt.Errorf("message not found: %s", messageID)
	}

	// Decrypt and unwrap
	innerBytes, err := a.decryptMessageBody(msg)
	if err != nil {
		return "", fmt.Errorf("failed to decrypt message: %w", err)
	}

	// Parse to get attachment list
	parsed := a.syncEngine.ParseDecryptedBody(innerBytes, messageID)
	var regularAtts []*message.Attachment
	for _, att := range parsed.Attachments {
		if !att.IsInline {
			regularAtts = append(regularAtts, att)
		}
	}
	if len(regularAtts) == 0 {
		return "", fmt.Errorf("no attachments found in encrypted message")
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		homeDir = ""
	}
	defaultDir := filepath.Join(homeDir, "Downloads")

	// In Flatpak, use portal save dialog (Wails GTK dialog doesn't route through portal)
	if platform.IsFlatpak() {
		return a.saveAllEncryptedAttachmentsViaPortal(regularAtts, innerBytes, defaultDir)
	}

	// Native: use Wails folder dialog
	saveDir, err := wailsRuntime.OpenDirectoryDialog(a.ctx, wailsRuntime.OpenDialogOptions{
		DefaultDirectory: defaultDir,
		Title:            "Save All Attachments",
	})
	if err != nil {
		return "", fmt.Errorf("failed to show folder dialog: %w", err)
	}
	if saveDir == "" {
		return "", nil
	}

	downloader := email.NewAttachmentDownloader(a.paths.AttachmentsPath())
	savedCount := 0

	for _, att := range regularAtts {
		content, err := downloader.ExtractAttachmentContent(innerBytes, att.Filename)
		if err != nil {
			log.Warn().Err(err).Str("filename", att.Filename).Msg("Failed to extract encrypted attachment")
			continue
		}

		_, err = downloader.SaveAttachmentToDirectory(att, content, saveDir)
		if err != nil {
			log.Warn().Err(err).Str("filename", att.Filename).Msg("Failed to save encrypted attachment")
			continue
		}
		savedCount++
	}

	log.Info().Int("count", savedCount).Str("folder", saveDir).Msg("Saved all encrypted attachments")
	return saveDir, nil
}

// saveAllAttachmentsViaPortal saves all attachments using the XDG FileChooser portal.
func (a *App) saveAllAttachmentsViaPortal(messageID string, attachments []*message.Attachment, defaultDir string) (string, error) {
	log := logging.WithComponent("app")

	filenames := make([]string, len(attachments))
	for i, att := range attachments {
		filenames[i] = att.Filename
	}

	savePaths, err := platform.PortalSaveFiles("Save All Attachments", filenames, defaultDir)
	if err != nil {
		return "", fmt.Errorf("failed to show save dialog: %w", err)
	}
	if len(savePaths) == 0 {
		return "", nil
	}

	// Get the message to find folder and UID
	msg, err := a.messageStore.Get(messageID)
	if err != nil {
		return "", fmt.Errorf("failed to get message: %w", err)
	}
	if msg == nil {
		return "", fmt.Errorf("message not found: %s", messageID)
	}

	// Fetch raw message from IMAP
	raw, err := a.syncEngine.FetchRawMessage(a.ctx, msg.AccountID, msg.FolderID, msg.UID)
	if err != nil {
		return "", fmt.Errorf("failed to fetch message: %w", err)
	}

	downloader := email.NewAttachmentDownloader(a.paths.AttachmentsPath())
	savedCount := 0

	for i, att := range attachments {
		if i >= len(savePaths) {
			break
		}

		content, err := a.storedAttachmentContent(downloader, raw, att)
		if err != nil {
			log.Warn().Err(err).Str("filename", att.Filename).Msg("Failed to extract attachment")
			continue
		}

		_, err = downloader.SaveAttachment(att, content, savePaths[i])
		if err != nil {
			log.Warn().Err(err).Str("filename", att.Filename).Msg("Failed to save attachment")
			continue
		}
		savedCount++
	}

	log.Info().Int("count", savedCount).Msg("Saved all attachments via portal")
	return filepath.Dir(savePaths[0]), nil
}

// saveAllEncryptedAttachmentsViaPortal saves all encrypted attachments using the XDG FileChooser portal.
func (a *App) saveAllEncryptedAttachmentsViaPortal(attachments []*message.Attachment, innerBytes []byte, defaultDir string) (string, error) {
	log := logging.WithComponent("app")

	filenames := make([]string, len(attachments))
	for i, att := range attachments {
		filenames[i] = att.Filename
	}

	savePaths, err := platform.PortalSaveFiles("Save All Attachments", filenames, defaultDir)
	if err != nil {
		return "", fmt.Errorf("failed to show save dialog: %w", err)
	}
	if len(savePaths) == 0 {
		return "", nil
	}

	downloader := email.NewAttachmentDownloader(a.paths.AttachmentsPath())
	savedCount := 0

	for i, att := range attachments {
		if i >= len(savePaths) {
			break
		}

		content, err := downloader.ExtractAttachmentContent(innerBytes, att.Filename)
		if err != nil {
			log.Warn().Err(err).Str("filename", att.Filename).Msg("Failed to extract encrypted attachment")
			continue
		}

		_, err = downloader.SaveAttachment(att, content, savePaths[i])
		if err != nil {
			log.Warn().Err(err).Str("filename", att.Filename).Msg("Failed to save encrypted attachment")
			continue
		}
		savedCount++
	}

	log.Info().Int("count", savedCount).Msg("Saved all encrypted attachments via portal")
	return filepath.Dir(savePaths[0]), nil
}

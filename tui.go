package main

import (
	"crypto/md5"
	"encoding/base64"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/atotto/clipboard"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/dustin/go-humanize"
	"outlook-tui/filepicker"
)

type pane int

const (
	paneFolders pane = iota
	paneMessages
	paneDetail
)

type appState int

const (
	stateConfig appState = iota
	stateDeviceAuth
	stateLoading
	stateMain
	stateCompose
	stateAttachments
	stateReplyConfirm
	stateURLSelect
	stateExternalURLSelect
	stateYouTrackInstallPrompt
	stateGitLabInstallPrompt
	stateHelp
	stateFileBrowse
	stateComposeCancelConfirm
	stateDeleteThreadConfirm
	stateYankSelect
)

// ThreadGroup holds a conversation thread: the most-recent message is the
// "header" shown when the group is collapsed. All messages in the thread
// (including the header) are in Members, ordered newest-first.
type ThreadGroup struct {
	ConversationID string
	Subject        string // normalised (Re:/Fwd: stripped)
	Members        []Message
}

// MessageListItem is one entry in the virtual message list used for keyboard
// navigation. It is either a thread-group header or an individual reply.
type MessageListItem struct {
	ThreadIdx int // index into m.threadGroups
	MemberIdx int // -1 = header row; >=0 = member inside the thread
	IsHeader  bool
}

// Messages
type (
	errMsg             error
	foldersFetchedMsg  []MailFolder
	messagesFetchedMsg struct {
		FolderID string
		Messages []Message
	}
	nextMessagesFetchedMsg struct {
		FolderID string
		Messages []Message
	}
	inboxMessagesFetchedMsg struct {
		Messages []Message
	}
	messageDetailFetchedMsg struct {
		Message     *Message
		Attachments []Attachment
	}
	attachmentsFetchedMsg struct {
		MessageID   string
		Attachments []Attachment
	}
	tokenFetchedMsg         TokenCache
	deviceCodeMsg           *DeviceCodeResponse
	statusUpdateMsg         string
	mailSentMsg             struct{}
	mailDeletedMsg          struct{ MessageID string }
	multipleMailsDeletedMsg struct {
		MessageIDs []string
		Errors     []error
	}
	mailRestoredMsg     struct{ MessageID string }
	attachmentSavedMsg  string
	userEmailFetchedMsg string
	editorBodyLoadedMsg string // body text returned after external editor exits
)

type mainModel struct {
	state         appState
	activePane    pane
	width, height int

	// Config state
	config     Config
	configStep int // 0 = Client ID, 1 = Tenant ID
	txtInput   textinput.Model
	statusMsg  string

	// Auth state
	deviceCode *DeviceCodeResponse

	// Graph clients
	authClient  *Authenticator
	graphClient *GraphClient

	// Data models
	folders         []MailFolder
	selectedFolder  int
	messages        []Message
	selectedMessage int
	detailMessage   *Message
	attachments     []Attachment
	selectedAttach  int

	// Thread grouping
	threadGroups     []ThreadGroup
	collapsedThreads map[string]bool   // keyed by ConversationID; true = collapsed
	virtualList      []MessageListItem // flat navigable list
	virtualSelected  int               // index into virtualList

	// Sub-components
	detailViewport viewport.Model
	helpViewport   viewport.Model
	spinner        spinner.Model

	// Compose state
	composeTo         textinput.Model
	composeCc         textinput.Model
	composeSubject    textinput.Model
	composeBody       textarea.Model
	composeStep       int    // 0 = To, 1 = Cc, 2 = Subject, 3 = Body
	composeReplyToID  string // non-empty when composing a reply; holds the original message ID
	composeIsReplyAll bool   // true when replying to all
	contacts          []Contact
	filteredContacts  []Contact
	contactsSelected  int
	contactsStartIdx  int
	composedImages    []PastedImage
	composedFiles     []PendingFile
	filepicker        filepicker.Model

	// Notification tracking
	inboxKnownIDs map[string]bool
	userEmail     string

	// SQLite cache (nil when use_sqlite == 0)
	db *DB

	// Focus state
	appFocused bool

	// URL select state
	extractedURLs   []string
	selectedURLIdx  int
	selectedYankIdx int

	// Thread deletion confirm state
	deleteThreadMsgIDs  []string
	deleteThreadSubject string
}

func initialModel() mainModel {
	cfg, _ := LoadConfig()
	applyTheme(cfg.Theme)

	ti := textinput.New()
	ti.Placeholder = "Enter Microsoft Client ID..."
	ti.Focus()
	ti.CharLimit = 150
	ti.Width = 50

	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color(ColorViolet))

	fp := filepicker.New()
	sortBy, sortOrder, lastDir := LoadFilepickerSettings()
	if lastDir != "" {
		fp.CurrentDirectory = lastDir
	}
	if sortBy == "Datetime" {
		fp.SortBy = filepicker.SortByDatetime
	} else {
		fp.SortBy = filepicker.SortByName
	}
	if sortOrder == "desc" {
		fp.SortOrder = filepicker.SortDescending
	} else {
		fp.SortOrder = filepicker.SortAscending
	}
	fp.Styles = filepicker.DefaultStyles()

	return mainModel{
		state:      stateLoading,
		txtInput:   ti,
		spinner:    s,
		configStep: 0,
		filepicker: fp,
		config:     cfg,
		appFocused: true,
	}
}

func (m mainModel) Init() tea.Cmd {
	if m.config.ClientID == "" {
		return tea.Batch(m.spinner.Tick, func() tea.Msg {
			return statusUpdateMsg("config_needed")
		})
	}
	return tea.Batch(m.spinner.Tick, checkConfigCmd())
}

// Commands
func checkConfigCmd() tea.Cmd {
	return func() tea.Msg {
		// Try loading token
		token, err := LoadToken()
		if err != nil {
			// Needs authentication
			return statusUpdateMsg("auth_needed")
		}

		return tokenFetchedMsg(token)
	}
}

func fetchDeviceCodeCmd(clientID, tenantID string) tea.Cmd {
	return func() tea.Msg {
		code, err := RequestDeviceCode(clientID, tenantID)
		if err != nil {
			return errMsg(err)
		}
		return deviceCodeMsg(code)
	}
}

func pollTokenCmd(clientID, tenantID string, deviceCode *DeviceCodeResponse) tea.Cmd {
	return func() tea.Msg {
		token, err := PollForToken(clientID, tenantID, deviceCode, func(s string) {})
		if err != nil {
			return errMsg(err)
		}
		return tokenFetchedMsg(token)
	}
}

func fetchFoldersCmd(gc *GraphClient) tea.Cmd {
	return func() tea.Msg {
		folders, err := gc.GetFolders()
		if err != nil {
			return errMsg(err)
		}
		return foldersFetchedMsg(folders)
	}
}

func fetchUserEmailCmd(gc *GraphClient) tea.Cmd {
	return func() tea.Msg {
		email, err := gc.GetMe()
		if err != nil {
			return userEmailFetchedMsg("")
		}
		return userEmailFetchedMsg(email)
	}
}

func fetchMessagesCmd(gc *GraphClient, folderID string) tea.Cmd {
	return func() tea.Msg {
		msgs, err := gc.GetMessages(folderID)
		if err != nil {
			return errMsg(err)
		}
		return messagesFetchedMsg{FolderID: folderID, Messages: msgs}
	}
}

func fetchNextMessagesCmd(gc *GraphClient, folderID string, skip int) tea.Cmd {
	return func() tea.Msg {
		msgs, err := gc.GetMessagesPage(folderID, skip)
		if err != nil {
			return errMsg(err)
		}
		return nextMessagesFetchedMsg{FolderID: folderID, Messages: msgs}
	}
}

func fetchInboxMessagesCmd(gc *GraphClient) tea.Cmd {
	return func() tea.Msg {
		msgs, err := gc.GetMessages("inbox")
		if err != nil {
			return nil // ignore background error
		}
		return inboxMessagesFetchedMsg{Messages: msgs}
	}
}

func fetchMessageDetailCmd(gc *GraphClient, msgID string, shouldMarkAsRead bool) tea.Cmd {
	return func() tea.Msg {
		msg, err := gc.GetMessage(msgID)
		if err != nil {
			return errMsg(err)
		}

		// Check if message has attachments or inline images/remote images in its body
		hasInline := msg.Body.ContentType == "html" && regexp.MustCompile(`(?i)src\s*=\s*['"]?cid:`).MatchString(msg.Body.Content)
		hasRemote := msg.Body.ContentType == "html" && regexp.MustCompile(`(?i)<img\b[^>]*src\s*=\s*['"]?https?://`).MatchString(msg.Body.Content)

		// If message has attachments, fetch them
		var atts []Attachment
		if msg.HasAttachments || hasInline {
			atts, _ = gc.GetAttachments(msgID)
		}

		if hasRemote {
			remoteAtts := extractRemoteImages(msg.Body.Content)
			atts = append(atts, remoteAtts...)
		}

		msg.Attachments = atts
		if len(atts) > 0 {
			msg.HasAttachments = true
		}

		// Also mark message as read if unread and requested
		if !msg.IsRead && shouldMarkAsRead {
			_ = gc.MarkAsRead(msgID, true)
			msg.IsRead = true
		}

		return messageDetailFetchedMsg{Message: msg, Attachments: atts}
	}
}

func fetchAttachmentsCmd(gc *GraphClient, msgID string) tea.Cmd {
	return func() tea.Msg {
		atts, err := gc.GetAttachments(msgID)
		if err != nil {
			return errMsg(err)
		}
		return attachmentsFetchedMsg{MessageID: msgID, Attachments: atts}
	}
}

func sendMailCmd(gc *GraphClient, to, cc, subject, body, replyToID string, replyAll bool, images []PastedImage, files []PendingFile) tea.Cmd {
	return func() tea.Msg {
		var err error
		if replyToID != "" {
			// Use the proper Graph reply endpoint so the message is threaded correctly.
			if replyAll {
				err = gc.ReplyAllMessage(replyToID, body, to, cc, images, files)
			} else {
				err = gc.ReplyMessage(replyToID, body, to, images, files)
			}
		} else {
			err = gc.SendMessage(subject, body, to, cc, images, files)
		}
		if err != nil {
			return errMsg(err)
		}
		return mailSentMsg{}
	}
}

func deleteMailCmd(gc *GraphClient, msgID string) tea.Cmd {
	return func() tea.Msg {
		err := gc.DeleteMessage(msgID)
		if err != nil {
			return errMsg(err)
		}
		return mailDeletedMsg{MessageID: msgID}
	}
}

func deleteMultipleMailsCmd(gc *GraphClient, msgIDs []string) tea.Cmd {
	return func() tea.Msg {
		var wg sync.WaitGroup
		errs := make([]error, len(msgIDs))
		for i, id := range msgIDs {
			wg.Add(1)
			go func(idx int, msgID string) {
				defer wg.Done()
				errs[idx] = gc.DeleteMessage(msgID)
			}(i, id)
		}
		wg.Wait()

		var actualErrors []error
		for _, err := range errs {
			if err != nil {
				actualErrors = append(actualErrors, err)
			}
		}

		return multipleMailsDeletedMsg{
			MessageIDs: msgIDs,
			Errors:     actualErrors,
		}
	}
}

func restoreMailCmd(gc *GraphClient, msgID string) tea.Cmd {
	return func() tea.Msg {
		err := gc.MoveMessage(msgID, "inbox")
		if err != nil {
			return errMsg(err)
		}
		return mailRestoredMsg{MessageID: msgID}
	}
}

func hashMsgID(msgID string) string {
	if msgID == "" {
		return ""
	}
	h := md5.Sum([]byte(msgID))
	return fmt.Sprintf("%x", h)
}

func getAndEnsureDownloadDir(configuredDir string) string {
	var downloadDir string
	if configuredDir != "" {
		resolved := configuredDir
		if strings.HasPrefix(resolved, "~/") {
			if home, errHome := os.UserHomeDir(); errHome == nil {
				resolved = filepath.Join(home, resolved[2:])
			}
		} else if resolved == "~" {
			if home, errHome := os.UserHomeDir(); errHome == nil {
				resolved = home
			}
		}
		downloadDir = filepath.Clean(resolved)
	} else {
		if home, errHome := os.UserHomeDir(); errHome == nil {
			downloadDir = filepath.Join(home, "Downloads")
		} else {
			downloadDir = "."
		}
	}

	if errMk := os.MkdirAll(downloadDir, 0755); errMk != nil {
		if home, errHome := os.UserHomeDir(); errHome == nil {
			return home
		}
		return "."
	}
	return downloadDir
}

func saveAttachmentCmd(msgID string, atts []Attachment, selectedIdx int, imageViewer string, attachmentDir string) tea.Cmd {
	return func() tea.Msg {
		if len(atts) == 0 || selectedIdx < 0 || selectedIdx >= len(atts) {
			return errMsg(fmt.Errorf("invalid attachment selection"))
		}

		selectedAtt := atts[selectedIdx]
		extLower := strings.ToLower(filepath.Ext(selectedAtt.Name))
		isImage := extLower == ".png" || extLower == ".jpg" || extLower == ".jpeg" || extLower == ".gif" || extLower == ".bmp" || extLower == ".webp"

		downloadDir := getAndEnsureDownloadDir(attachmentDir)

		// If it's not an image, or no custom image viewer is configured, download only the selected attachment and open with xdg-open.
		if !isImage || imageViewer == "" {
			var fileName string
			if msgID != "" {
				fileName = hashMsgID(msgID) + "_" + selectedAtt.Name
			} else {
				fileName = selectedAtt.Name
			}
			path := filepath.Join(downloadDir, fileName)

			var exists bool
			if msgID != "" {
				if stat, errStat := os.Stat(path); errStat == nil && !stat.IsDir() {
					exists = true
				}
			}

			if !exists {
				var data []byte
				var err error

				if selectedAtt.OdataType == "#outlook-tui.remoteImage" {
					resp, errGet := http.Get(selectedAtt.ContentId)
					if errGet != nil {
						return errMsg(fmt.Errorf("failed to download remote image: %w", errGet))
					}
					defer resp.Body.Close()
					if resp.StatusCode != http.StatusOK {
						return errMsg(fmt.Errorf("failed to download remote image: status %d", resp.StatusCode))
					}
					data, err = io.ReadAll(resp.Body)
					if err != nil {
						return errMsg(fmt.Errorf("failed to read remote image content: %w", err))
					}
				} else {
					data, err = base64.StdEncoding.DecodeString(selectedAtt.ContentBytes)
					if err != nil {
						return errMsg(fmt.Errorf("failed to decode attachment: %w", err))
					}
				}

				if msgID == "" {
					ext := filepath.Ext(selectedAtt.Name)
					base := strings.TrimSuffix(selectedAtt.Name, ext)
					counter := 1
					for {
						if _, errStat := os.Stat(path); os.IsNotExist(errStat) {
							break
						}
						path = filepath.Join(downloadDir, fmt.Sprintf("%s (%d)%s", base, counter, ext))
						counter++
					}
				}

				if err := os.WriteFile(path, data, 0644); err != nil {
					return errMsg(fmt.Errorf("failed to write attachment file: %w", err))
				}
			}

			cmd := exec.Command("xdg-open", path)
			_ = cmd.Start()

			return attachmentSavedMsg(path)
		}

		// Download all image attachments, identifying the selected one.
		var imageAtts []Attachment
		var selectedImageIndex = -1
		for idx, a := range atts {
			aExtLower := strings.ToLower(filepath.Ext(a.Name))
			aIsImage := aExtLower == ".png" || aExtLower == ".jpg" || aExtLower == ".jpeg" || aExtLower == ".gif" || aExtLower == ".bmp" || aExtLower == ".webp"
			if aIsImage {
				if idx == selectedIdx {
					selectedImageIndex = len(imageAtts)
				}
				imageAtts = append(imageAtts, a)
			}
		}

		savedPaths := []string{}
		var selectedSavedPath string
		for i, imgAtt := range imageAtts {
			isCurrent := (i == selectedImageIndex)

			var fileName string
			if msgID != "" {
				fileName = hashMsgID(msgID) + "_" + imgAtt.Name
			} else {
				fileName = imgAtt.Name
			}
			path := filepath.Join(downloadDir, fileName)

			var exists bool
			if msgID != "" {
				if stat, errStat := os.Stat(path); errStat == nil && !stat.IsDir() {
					exists = true
				}
			}

			if exists {
				savedPaths = append(savedPaths, path)
				if isCurrent {
					selectedSavedPath = path
				}
				continue
			}

			var data []byte
			var err error

			if imgAtt.OdataType == "#outlook-tui.remoteImage" {
				resp, errGet := http.Get(imgAtt.ContentId)
				if errGet != nil {
					if isCurrent {
						return errMsg(fmt.Errorf("failed to download remote image: %w", errGet))
					}
					continue
				}
				data, err = io.ReadAll(resp.Body)
				resp.Body.Close()
				if err != nil {
					if isCurrent {
						return errMsg(fmt.Errorf("failed to read remote image content: %w", err))
					}
					continue
				}
			} else {
				data, err = base64.StdEncoding.DecodeString(imgAtt.ContentBytes)
				if err != nil {
					if isCurrent {
						return errMsg(fmt.Errorf("failed to decode attachment: %w", err))
					}
					continue
				}
			}

			if msgID == "" {
				ext := filepath.Ext(imgAtt.Name)
				base := strings.TrimSuffix(imgAtt.Name, ext)
				counter := 1
				for {
					if _, errStat := os.Stat(path); os.IsNotExist(errStat) {
						break
					}
					path = filepath.Join(downloadDir, fmt.Sprintf("%s (%d)%s", base, counter, ext))
					counter++
				}
			}

			if err := os.WriteFile(path, data, 0644); err != nil {
				if isCurrent {
					return errMsg(fmt.Errorf("failed to write attachment file: %w", err))
				}
				continue
			}

			savedPaths = append(savedPaths, path)
			if isCurrent {
				selectedSavedPath = path
			}
		}

		if selectedSavedPath == "" {
			return errMsg(fmt.Errorf("failed to save the selected image attachment"))
		}

		newSelectedIdx := -1
		for idx, p := range savedPaths {
			if p == selectedSavedPath {
				newSelectedIdx = idx
				break
			}
		}

		var cmd *exec.Cmd
		parts := strings.Fields(imageViewer)
		if len(parts) > 0 {
			viewerName := parts[0]
			viewerBase := filepath.Base(viewerName)

			var args []string
			if viewerBase == "sxiv" || viewerBase == "nsxiv" {
				if newSelectedIdx != -1 {
					args = append(parts[1:], "-n", strconv.Itoa(newSelectedIdx+1))
				} else {
					args = parts[1:]
				}
				args = append(args, savedPaths...)
			} else if viewerBase == "feh" {
				if selectedSavedPath != "" {
					args = append(parts[1:], "--start-at", selectedSavedPath)
				} else {
					args = parts[1:]
				}
				args = append(args, savedPaths...)
			} else if viewerBase == "imv" {
				if selectedSavedPath != "" {
					args = append(parts[1:], "-n", selectedSavedPath)
				} else {
					args = parts[1:]
				}
				args = append(args, savedPaths...)
			} else {
				// Reorder so that selectedSavedPath is first, followed by the rest
				var reorderedPaths []string
				if selectedSavedPath != "" {
					reorderedPaths = append(reorderedPaths, selectedSavedPath)
				}
				for idx, p := range savedPaths {
					if idx != newSelectedIdx {
						reorderedPaths = append(reorderedPaths, p)
					}
				}
				args = append(parts[1:], reorderedPaths...)
			}

			cmd = exec.Command(viewerName, args...)
		} else {
			cmd = exec.Command("xdg-open", selectedSavedPath)
		}

		_ = cmd.Start()

		return attachmentSavedMsg(selectedSavedPath)
	}
}

type MsgFileAttached struct {
	Name        string
	ContentType string
	Data        []byte
	Err         error
}

func attachFileFromFilepathCmd(path string) tea.Cmd {
	return func() tea.Msg {
		data, err := os.ReadFile(path)
		if err != nil {
			return MsgFileAttached{Err: err}
		}

		filename := filepath.Base(path)
		contentType := http.DetectContentType(data)

		// DetectContentType can be generic; map common extension overrides for accuracy
		ext := strings.ToLower(filepath.Ext(filename))
		switch ext {
		case ".png":
			contentType = "image/png"
		case ".jpg", ".jpeg":
			contentType = "image/jpeg"
		case ".gif":
			contentType = "image/gif"
		}

		return MsgFileAttached{
			Name:        filename,
			ContentType: contentType,
			Data:        data,
		}
	}
}

// openEditorCmd writes the current compose body to a temp file and opens the
// user's preferred $EDITOR (falling back to $VISUAL, then vi). BubbleTea
// suspends the TUI while the editor is running and restores it on exit. The
// updated file content is returned as editorBodyLoadedMsg.
func openEditorCmd(currentBody string) tea.Cmd {
	// Determine editor binary
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = os.Getenv("VISUAL")
	}
	if editor == "" {
		editor = "vi"
	}

	// Write current body to a temp file
	tmpFile, err := os.CreateTemp("", "outlook-tui-body-*.txt")
	if err != nil {
		return func() tea.Msg { return editorBodyLoadedMsg(currentBody) }
	}
	if _, err := tmpFile.WriteString(currentBody); err != nil {
		_ = tmpFile.Close()
		return func() tea.Msg { return editorBodyLoadedMsg(currentBody) }
	}
	_ = tmpFile.Close()
	tmpPath := tmpFile.Name()

	// Build the editor command. The editor value may contain arguments
	// (e.g. "nvim -u NONE"), so split on whitespace.
	parts := strings.Fields(editor)
	args := append(parts[1:], tmpPath)
	c := exec.Command(parts[0], args...) //nolint:gosec

	return tea.ExecProcess(c, func(err error) tea.Msg {
		defer os.Remove(tmpPath)
		if err != nil {
			// Editor returned non-zero; keep original body
			return editorBodyLoadedMsg(currentBody)
		}
		data, readErr := os.ReadFile(tmpPath)
		if readErr != nil {
			return editorBodyLoadedMsg(currentBody)
		}
		return editorBodyLoadedMsg(string(data))
	})
}

// viewMessageInEditorCmd formats the current detailed message with headers,
// attachments, and stripped plain body, writes it to a temp file, and opens it
// in the user's preferred $EDITOR (falling back to $VISUAL, then vi).
// BubbleTea suspends the TUI while the editor is running and restores it on exit.
func (m mainModel) viewMessageInEditorCmd() tea.Cmd {
	// Determine editor binary
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = os.Getenv("VISUAL")
	}
	if editor == "" {
		editor = "vi"
	}

	// Format the message content
	if m.detailMessage == nil {
		return nil
	}

	var sb strings.Builder
	// Build headers
	formatRecipients := func(recipients []Recipient) string {
		var parts []string
		for _, r := range recipients {
			name := r.EmailAddress.Name
			addr := r.EmailAddress.Address
			if name == "" {
				parts = append(parts, addr)
			} else if addr == "" {
				parts = append(parts, name)
			} else {
				parts = append(parts, fmt.Sprintf("%s <%s>", name, addr))
			}
		}
		return strings.Join(parts, ", ")
	}

	fromVal := fmt.Sprintf("%s <%s>", m.detailMessage.From.EmailAddress.Name, m.detailMessage.From.EmailAddress.Address)
	dateStr := m.detailMessage.ReceivedDateTime.Local().Format("Mon, Jan 2, 2006 at 15:04")

	sb.WriteString("Subject: " + m.detailMessage.Subject + "\n")
	sb.WriteString("From:    " + fromVal + "\n")
	toVal := formatRecipients(m.detailMessage.ToRecipients)
	if toVal != "" {
		sb.WriteString("To:      " + toVal + "\n")
	}
	ccVal := formatRecipients(m.detailMessage.CcRecipients)
	if ccVal != "" {
		sb.WriteString("Cc:      " + ccVal + "\n")
	}
	sb.WriteString("Date:    " + dateStr + "\n")

	if len(m.attachments) > 0 {
		sb.WriteString(fmt.Sprintf("Attachments (📎 %d):\n", len(m.attachments)))
		for _, att := range m.attachments {
			sb.WriteString(fmt.Sprintf("  - %s (%s, %d bytes)\n", att.Name, att.ContentType, att.Size))
		}
	}

	sb.WriteString("\n" + strings.Repeat("-", 80) + "\n\n")

	// Message body content formatted (with HTML stripped, ANSI stripped)
	plainBody := stripANSICodes(formatBodyContent(m.detailMessage.Body.Content))
	sb.WriteString(plainBody)
	sb.WriteString("\n")

	// Write content to a temp file
	tmpFile, err := os.CreateTemp("", "outlook-tui-view-*.txt")
	if err != nil {
		return func() tea.Msg { return nil }
	}
	if _, err := tmpFile.WriteString(sb.String()); err != nil {
		_ = tmpFile.Close()
		return func() tea.Msg { return nil }
	}
	_ = tmpFile.Close()
	tmpPath := tmpFile.Name()

	// Build the editor command. The editor value may contain arguments
	// (e.g. "nvim -u NONE"), so split on whitespace.
	parts := strings.Fields(editor)
	args := append(parts[1:], tmpPath)
	c := exec.Command(parts[0], args...) //nolint:gosec

	return tea.ExecProcess(c, func(err error) tea.Msg {
		defer os.Remove(tmpPath)
		return nil
	})
}

type youtrackTuiFinishedMsg struct {
	Err error
}

// openYouTrackTuiCmd launches the external yt-tui command with the given URL.
func openYouTrackTuiCmd(urlStr string) tea.Cmd {
	c := exec.Command("yt-tui", urlStr)
	return tea.ExecProcess(c, func(err error) tea.Msg {
		return youtrackTuiFinishedMsg{Err: err}
	})
}

type gitlabTuiFinishedMsg struct {
	Err error
}

// openGitLabTuiCmd launches the external gitlab-tui command with the given URL.
func openGitLabTuiCmd(urlStr string) tea.Cmd {
	c := exec.Command("gitlab-tui", urlStr)
	return tea.ExecProcess(c, func(err error) tea.Msg {
		return gitlabTuiFinishedMsg{Err: err}
	})
}

type browserFinishedMsg struct {
	Err error
}

// openBrowserCmd launches the external browser command (configured by browser_command) with the given URL.
func (m mainModel) openBrowserCmd(urlStr string) tea.Cmd {
	return func() tea.Msg {
		browserCmd := m.config.BrowserCommand
		if browserCmd == "" {
			browserCmd = "xdg-open"
		}
		parts := strings.Fields(browserCmd)
		if len(parts) == 0 {
			parts = []string{"xdg-open"}
		}
		args := append(parts[1:], urlStr)
		c := exec.Command(parts[0], args...)
		err := c.Start()
		return browserFinishedMsg{Err: err}
	}
}

// Tick command for background refresh
type tickMsg time.Time

func (m mainModel) tickCmd() tea.Cmd {
	interval := 5 * time.Minute
	if m.config.RefreshTimeMin > 0 {
		interval = time.Duration(m.config.RefreshTimeMin) * time.Minute
	}
	return tea.Tick(interval, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

// normaliseSubject strips common reply/forward prefixes for grouping.
func normaliseSubject(s string) string {
	for {
		lower := strings.ToLower(strings.TrimSpace(s))
		if strings.HasPrefix(lower, "re:") {
			s = strings.TrimSpace(s[3:])
		} else if strings.HasPrefix(lower, "fwd:") {
			s = strings.TrimSpace(s[4:])
		} else if strings.HasPrefix(lower, "fw:") {
			s = strings.TrimSpace(s[3:])
		} else {
			break
		}
	}
	return s
}

// buildThreadGroups groups m.messages into conversation threads.
// Groups with a single message stay as solo threads (no collapse UI).
// Groups with 2+ messages are collapsed by default (unless already tracked).
func (m *mainModel) buildThreadGroups() {
	if m.collapsedThreads == nil {
		m.collapsedThreads = make(map[string]bool)
	}

	// Use an ordered slice to preserve newest-first ordering of threads.
	order := []string{}
	byConv := map[string][]Message{}

	for _, msg := range m.messages {
		cid := msg.ConversationID
		if cid == "" {
			// Fall back: treat each message as its own thread
			cid = msg.ID
		}
		if _, seen := byConv[cid]; !seen {
			order = append(order, cid)
		}
		byConv[cid] = append(byConv[cid], msg)
	}

	groups := make([]ThreadGroup, 0, len(order))
	for _, cid := range order {
		members := byConv[cid]
		subj := normaliseSubject(members[0].Subject)
		groups = append(groups, ThreadGroup{
			ConversationID: cid,
			Subject:        subj,
			Members:        members,
		})
		// Collapse multi-message threads by default (only on first encounter)
		if len(members) > 1 {
			if _, known := m.collapsedThreads[cid]; !known {
				m.collapsedThreads[cid] = true
			}
		}
	}
	m.threadGroups = groups
	m.buildVirtualList()
}

// buildVirtualList rebuilds the flat navigation list from threadGroups and
// collapsed state. Must be called after buildThreadGroups and whenever
// collapsed state changes.
func (m *mainModel) buildVirtualList() {
	var items []MessageListItem
	for ti, tg := range m.threadGroups {
		collapsed := m.collapsedThreads[tg.ConversationID]
		if len(tg.Members) == 1 || collapsed {
			// Show only the header (most recent message)
			items = append(items, MessageListItem{ThreadIdx: ti, MemberIdx: -1, IsHeader: true})
		} else {
			// Header row first, then all members
			items = append(items, MessageListItem{ThreadIdx: ti, MemberIdx: -1, IsHeader: true})
			for mi := range tg.Members {
				items = append(items, MessageListItem{ThreadIdx: ti, MemberIdx: mi, IsHeader: false})
			}
		}
	}
	m.virtualList = items
}

// activeMessage returns the Message currently indicated by virtualSelected,
// or nil if list is empty.
func (m mainModel) activeMessage() *Message {
	if len(m.virtualList) == 0 || m.virtualSelected >= len(m.virtualList) {
		return nil
	}
	item := m.virtualList[m.virtualSelected]
	tg := m.threadGroups[item.ThreadIdx]
	if item.IsHeader || item.MemberIdx < 0 {
		return &tg.Members[0]
	}
	return &tg.Members[item.MemberIdx]
}

// loadCachedFolderMessages loads cached messages from SQLite for the currently
// selected folder and updates the model's message list and thread groups.
// It is a no-op when SQLite is disabled or no cache exists for the folder.
// Returns the (possibly updated) model and the status message to display.
func (m mainModel) loadCachedFolderMessages() (mainModel, string) {
	if m.db == nil || len(m.folders) == 0 {
		status := "Loading messages..."
		if len(m.folders) > 0 {
			status = fmt.Sprintf("Loading messages for %s...", m.folders[m.selectedFolder].DisplayName)
		}
		return m, status
	}
	folderID := m.folders[m.selectedFolder].ID
	if folderID == "favorites" {
		cached, _ := m.db.GetFavoriteMessages()
		m.messages = cached
		m.buildThreadGroups()
		return m, fmt.Sprintf("Favorites: %d messages", len(cached))
	}
	if m.config.UseSQLite == 1 {
		cached, err := m.db.GetMessages(folderID)
		if err == nil && len(cached) > 0 {
			m.messages = cached
			m.buildThreadGroups()
			return m, fmt.Sprintf("Showing %d cached messages, refreshing...", len(cached))
		}
	}
	return m, fmt.Sprintf("Loading messages for %s...", m.folders[m.selectedFolder].DisplayName)
}

// updateFavoritesFolderCounts updates the unread and total item counts for the favorites folder in memory.
func (m *mainModel) updateFavoritesFolderCounts() {
	if len(m.folders) == 0 || m.folders[0].ID != "favorites" {
		return
	}
	if m.db == nil {
		return
	}
	unread, total, err := m.db.GetFavoritesCounts()
	if err == nil {
		m.folders[0].UnreadItemCount = unread
		m.folders[0].TotalItemCount = total
	}
}

// updateFolderUnreadCount adjusts the unread count of the folder associated with a message.
func (m *mainModel) updateFolderUnreadCount(messageID string, isRead bool, wasRead bool) {
	if isRead == wasRead {
		return
	}
	delta := -1
	if !isRead {
		delta = 1
	}

	// Determine the folder ID for this message
	var folderID string
	if len(m.folders) > 0 {
		selectedFolderID := m.folders[m.selectedFolder].ID
		if selectedFolderID != "favorites" {
			folderID = selectedFolderID
		} else if m.db != nil {
			// Query the database to find the original folder ID
			if fID, err := m.db.GetMessageFolderID(messageID); err == nil && fID != "" {
				folderID = fID
			}
		}
	}

	if folderID != "" {
		for i := range m.folders {
			if m.folders[i].ID == folderID {
				m.folders[i].UnreadItemCount = m.folders[i].UnreadItemCount + delta
				if m.folders[i].UnreadItemCount < 0 {
					m.folders[i].UnreadItemCount = 0
				}
				break
			}
		}
	}

	// Always update favorites counts in case it's in favorites
	m.updateFavoritesFolderCounts()
}

// loadMessageDetail loads the message detail (including body and attachments).
// If the message body is already cached in the database or in memory, it renders it instantly.
// If the message is unread, it still triggers fetchMessageDetailCmd to mark it as read.
// Otherwise, it starts the network fetch.
func (m mainModel) loadMessageDetail(am *Message) (mainModel, tea.Cmd) {
	if am == nil {
		m.detailMessage = nil
		m.attachments = nil
		m.detailViewport.SetContent("")
		return m, nil
	}

	// 1. Check if the body content is already loaded in the memory model
	if am.Body.Content != "" {
		m.detailMessage = am
		m.attachments = am.Attachments
		m = m.updateViewportSize()
		m.detailViewport.SetContent(wrapText(formatBodyContent(am.Body.Content), m.detailViewport.Width))
		m.detailViewport.GotoTop()

		hasInline := am.Body.ContentType == "html" && regexp.MustCompile(`(?i)src\s*=\s*['"]?cid:`).MatchString(am.Body.Content)
		hasRemote := am.Body.ContentType == "html" && regexp.MustCompile(`(?i)<img\b[^>]*src\s*=\s*['"]?https?://`).MatchString(am.Body.Content)
		if (am.HasAttachments || hasInline || hasRemote) && len(am.Attachments) == 0 {
			m.statusMsg = "Loading attachments..."
			return m, fetchAttachmentsCmd(m.graphClient, am.ID)
		}

		if am.IsRead {
			m.statusMsg = "Message details loaded"
			return m, nil
		}
		// If unread, fetch from Graph to mark read, but only if app is focused
		if m.appFocused {
			m.statusMsg = "Marking read..."
			return m, fetchMessageDetailCmd(m.graphClient, am.ID, true)
		}
		m.statusMsg = "Message details loaded"
		return m, nil
	}

	// 2. Check if the body content is cached in SQLite
	if m.db != nil {
		var cached *Message
		var err error
		if len(m.folders) > 0 && m.folders[m.selectedFolder].ID == "favorites" {
			cached, err = m.db.GetFavoriteMessage(am.ID)
		} else if m.config.UseSQLite == 1 {
			cached, err = m.db.GetMessage(am.ID)
		}
		if err == nil && cached != nil && cached.Body.Content != "" {
			m.detailMessage = cached
			m.attachments = cached.Attachments
			m = m.updateViewportSize()
			m.detailViewport.SetContent(wrapText(formatBodyContent(cached.Body.Content), m.detailViewport.Width))
			m.detailViewport.GotoTop()

			// Update in-memory collections so they have the loaded body and attachments too
			for i, msg := range m.messages {
				if msg.ID == am.ID {
					m.messages[i].Body = cached.Body
					m.messages[i].Attachments = cached.Attachments
					break
				}
			}
			for ti := range m.threadGroups {
				for mi := range m.threadGroups[ti].Members {
					if m.threadGroups[ti].Members[mi].ID == am.ID {
						m.threadGroups[ti].Members[mi].Body = cached.Body
						m.threadGroups[ti].Members[mi].Attachments = cached.Attachments
						break
					}
				}
			}

			hasInline := cached.Body.ContentType == "html" && regexp.MustCompile(`(?i)src\s*=\s*['"]?cid:`).MatchString(cached.Body.Content)
			hasRemote := cached.Body.ContentType == "html" && regexp.MustCompile(`(?i)<img\b[^>]*src\s*=\s*['"]?https?://`).MatchString(cached.Body.Content)
			if (cached.HasAttachments || hasInline || hasRemote) && len(cached.Attachments) == 0 {
				m.statusMsg = "Loading attachments..."
				return m, fetchAttachmentsCmd(m.graphClient, am.ID)
			}

			if cached.IsRead {
				m.statusMsg = "Message details loaded (cached)"
				return m, nil
			}
			// If unread, fetch from Graph to mark read, but only if app is focused
			if m.appFocused {
				m.statusMsg = "Marking read..."
				return m, fetchMessageDetailCmd(m.graphClient, am.ID, true)
			}
			m.statusMsg = "Message details loaded (cached)"
			return m, nil
		}
	}

	// 3. Fallback: Load from API
	m.detailMessage = am
	m.attachments = nil
	m = m.updateViewportSize()
	m.detailViewport.SetContent("Loading message body...")
	m.statusMsg = "Loading message details..."
	return m, fetchMessageDetailCmd(m.graphClient, am.ID, m.appFocused)
}

// selectFolder changes the currently selected folder to the given index,
// loads cached messages for it, initiates a fetch for the latest messages,
// and loads/clears message detail views appropriately.
func (m mainModel) selectFolder(idx int) (mainModel, tea.Cmd) {
	if idx < 0 || idx >= len(m.folders) {
		return m, nil
	}
	m.selectedFolder = idx
	m.virtualSelected = 0
	m.detailMessage = nil
	m.attachments = nil
	m.detailViewport.SetContent("")

	var detailCmd tea.Cmd
	m, m.statusMsg = m.loadCachedFolderMessages()
	if am := m.activeMessage(); am != nil {
		m, detailCmd = m.loadMessageDetail(am)
	}

	if m.folders[m.selectedFolder].ID == "favorites" {
		return m, detailCmd
	}

	fetchCmd := fetchMessagesCmd(m.graphClient, m.folders[m.selectedFolder].ID)
	if detailCmd != nil {
		return m, tea.Batch(detailCmd, fetchCmd)
	}
	return m, fetchCmd
}

func (m mainModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	// Update filepicker if in filepicker mode (for non-keyboard messages like directory read results)
	if m.state == stateFileBrowse {
		if _, ok := msg.(tea.KeyMsg); !ok {
			var cmd tea.Cmd
			m.filepicker, cmd = m.filepicker.Update(msg)
			if cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Batch(clearKittyImagesCmd(), tea.Quit)
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m = m.updateViewportSize()
		if m.detailMessage != nil {
			m.detailViewport.SetContent(wrapText(formatBodyContent(m.detailMessage.Body.Content), m.detailViewport.Width))
		}
		if m.state == stateHelp {
			m.helpViewport.SetContent(m.renderHelpContent())
		}
		if m.state == stateCompose || m.state == stateComposeCancelConfirm {
			h := m.height - 18
			if h < 3 {
				h = 3
			}
			m.composeBody.SetHeight(h)
		}
		if m.state == stateFileBrowse {
			h := m.height - 17
			if h < 5 {
				h = 5
			}
			m.filepicker.SetHeight(h)
		}

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case tea.FocusMsg:
		m.appFocused = true
		if m.state == stateMain {
			if am := m.activeMessage(); am != nil && !am.IsRead {
				var cmd tea.Cmd
				m, cmd = m.loadMessageDetail(am)
				return m, cmd
			}
		}

	case tea.BlurMsg:
		m.appFocused = false

	case statusUpdateMsg:
		switch msg {
		case "config_needed":
			m.state = stateConfig
			m.configStep = 0
			m.txtInput.Placeholder = "Enter Microsoft Client ID..."
			m.txtInput.SetValue("")
			m.txtInput.Focus()
		case "auth_needed":
			m.state = stateLoading
			m.statusMsg = "Requesting device code..."
			return m, fetchDeviceCodeCmd(m.config.ClientID, m.config.TenantID)
		}

	case errMsg:
		m.statusMsg = fmt.Sprintf("Error: %v", msg)
		if m.state == stateLoading {
			// If error in loading, go back to config
			m.state = stateConfig
			m.configStep = 0
			m.txtInput.Focus()
		}

	case deviceCodeMsg:
		m.state = stateDeviceAuth
		m.deviceCode = msg
		m.statusMsg = "Waiting for user authentication..."
		return m, pollTokenCmd(m.config.ClientID, m.config.TenantID, m.deviceCode)

	case tokenFetchedMsg:
		m.state = stateLoading
		m.statusMsg = "Fetching Outlook folders..."

		// If we entered config parameters manually, save them
		if m.config.ClientID != "" {
			_ = SaveConfig(m.config)
		} else {
			// Loaded from cache, populate model config
			cfg, _ := LoadConfig()
			m.config = cfg
		}
		applyTheme(m.config.Theme)

		// Cache token
		_ = SaveToken(TokenCache(msg))

		// Open SQLite database (always open it so we can use it for internal Favorites)
		if m.db == nil {
			if sqlDB, err := OpenDB(); err == nil {
				m.db = sqlDB
			} else {
				m.statusMsg = fmt.Sprintf("SQLite warning: %v", err)
			}
		}

		m.authClient = NewAuthenticator(m.config.ClientID, m.config.TenantID, TokenCache(msg))
		m.graphClient = NewGraphClient(m.authClient.GetClient())

		return m, tea.Batch(
			fetchFoldersCmd(m.graphClient),
			fetchUserEmailCmd(m.graphClient),
		)

	case foldersFetchedMsg:
		sortedFolders := sortFolders(msg, m.config.ExcludedFolders, m.db)
		if m.state != stateMain {
			// Initial load: set up navigation state
			m.folders = sortedFolders
			m.state = stateMain
			m.activePane = paneFolders
			m.selectedFolder = 0
			if len(m.folders) > 0 {
				var detailCmd tea.Cmd
				firstFolderID := m.folders[0].ID
				if firstFolderID == "favorites" {
					if m.db != nil {
						cached, _ := m.db.GetFavoriteMessages()
						m.messages = cached
						m.buildThreadGroups()
						m.statusMsg = fmt.Sprintf("Favorites: %d messages", len(cached))
						if am := m.activeMessage(); am != nil {
							m, detailCmd = m.loadMessageDetail(am)
						}
					} else {
						m.statusMsg = "Favorites: 0 messages"
					}
				} else if m.config.UseSQLite == 1 && m.db != nil {
					if cached, err := m.db.GetMessages(firstFolderID); err == nil && len(cached) > 0 {
						m.messages = cached
						m.buildThreadGroups()
						m.statusMsg = fmt.Sprintf("Showing %d cached messages, refreshing...", len(cached))
						if am := m.activeMessage(); am != nil {
							m, detailCmd = m.loadMessageDetail(am)
						}
					} else {
						m.statusMsg = fmt.Sprintf("Loading messages for %s...", m.folders[0].DisplayName)
					}
				} else {
					m.statusMsg = fmt.Sprintf("Loading messages for %s...", m.folders[0].DisplayName)
				}
				cmds := []tea.Cmd{
					fetchInboxMessagesCmd(m.graphClient),
					m.tickCmd(),
				}
				if firstFolderID != "favorites" {
					cmds = append(cmds, fetchMessagesCmd(m.graphClient, firstFolderID))
				}
				if detailCmd != nil {
					cmds = append(cmds, detailCmd)
				}
				return m, tea.Batch(cmds...)
			}
			m.statusMsg = "Ready"
		} else {
			// Background refresh: only update folder data (unread counts etc.)
			// Preserve selectedFolder — clamp if folders list shrank
			m.folders = sortedFolders
			if m.selectedFolder >= len(m.folders) {
				m.selectedFolder = max(0, len(m.folders)-1)
			}
		}

	case nextMessagesFetchedMsg:
		if len(m.folders) == 0 || m.folders[m.selectedFolder].ID != msg.FolderID {
			break
		}
		if len(msg.Messages) == 0 {
			m.statusMsg = "No more messages to load"
			break
		}
		// Populate any cached bodies and attachments into the newly fetched message list to avoid losing them in memory
		for i, newMsg := range msg.Messages {
			for _, oldMsg := range m.messages {
				if oldMsg.ID == newMsg.ID && oldMsg.Body.Content != "" {
					msg.Messages[i].Body = oldMsg.Body
					msg.Messages[i].Attachments = oldMsg.Attachments
					break
				}
			}
			if msg.Messages[i].Body.Content == "" && m.config.UseSQLite == 1 && m.db != nil {
				if cached, err := m.db.GetMessage(newMsg.ID); err == nil && cached != nil && cached.Body.Content != "" {
					msg.Messages[i].Body = cached.Body
					msg.Messages[i].Attachments = cached.Attachments
				}
			}
		}

		// Append newly fetched messages to our list
		m.messages = append(m.messages, msg.Messages...)
		if m.config.UseSQLite == 1 && m.db != nil {
			_ = m.db.UpsertMessages(msg.FolderID, m.messages)
		}
		m.statusMsg = fmt.Sprintf("Loaded %d messages", len(m.messages))
		m.buildThreadGroups()

	case messagesFetchedMsg:
		if len(m.folders) == 0 || m.folders[m.selectedFolder].ID != msg.FolderID {
			break
		}
		// Populate any cached bodies and attachments into the newly fetched message list to avoid losing them in memory
		for i, newMsg := range msg.Messages {
			// Check current in-memory messages
			for _, oldMsg := range m.messages {
				if oldMsg.ID == newMsg.ID && oldMsg.Body.Content != "" {
					msg.Messages[i].Body = oldMsg.Body
					msg.Messages[i].Attachments = oldMsg.Attachments
					break
				}
			}
			// Fallback to SQLite check if still empty
			if msg.Messages[i].Body.Content == "" && m.config.UseSQLite == 1 && m.db != nil {
				if cached, err := m.db.GetMessage(newMsg.ID); err == nil && cached != nil && cached.Body.Content != "" {
					msg.Messages[i].Body = cached.Body
					msg.Messages[i].Attachments = cached.Attachments
				}
			}
		}

		// Persist messages to SQLite cache (preserving bodies via ON CONFLICT DO UPDATE)
		if m.config.UseSQLite == 1 && m.db != nil {
			_ = m.db.UpsertMessages(msg.FolderID, msg.Messages)
		}
		// Remember the currently active message ID so we can re-select it
		currentID := ""
		if am := m.activeMessage(); am != nil {
			currentID = am.ID
		}
		m.messages = msg.Messages
		m.statusMsg = fmt.Sprintf("Loaded %d messages", len(m.messages))

		// Rebuild thread groups (preserve collapsed state map)
		m.buildThreadGroups()

		// Try to re-select the same message
		preserved := false
		if currentID != "" {
			for vi, item := range m.virtualList {
				tg := m.threadGroups[item.ThreadIdx]
				var candidate Message
				if item.IsHeader || item.MemberIdx < 0 {
					candidate = tg.Members[0]
				} else {
					candidate = tg.Members[item.MemberIdx]
				}
				if candidate.ID == currentID {
					m.virtualSelected = vi
					preserved = true
					break
				}
			}
		}
		if !preserved || m.detailMessage == nil || m.detailMessage.ID != currentID {
			if len(m.virtualList) > 0 {
				if m.virtualSelected >= len(m.virtualList) {
					m.virtualSelected = len(m.virtualList) - 1
				}
				if m.virtualSelected < 0 {
					m.virtualSelected = 0
				}
				var cmd tea.Cmd
				m, cmd = m.loadMessageDetail(m.activeMessage())
				return m, cmd
			} else {
				m.virtualSelected = 0
				m.detailMessage = nil
				m.attachments = nil
				m.detailViewport.SetContent("")
			}
		}

	case messageDetailFetchedMsg:
		// Make sure it matches selected message
		if am := m.activeMessage(); am != nil && am.ID == msg.Message.ID {
			m.detailMessage = msg.Message
			m.attachments = msg.Attachments
			m.selectedAttach = 0

			m = m.updateViewportSize()
			m.detailViewport.SetContent(wrapText(formatBodyContent(msg.Message.Body.Content), m.detailViewport.Width))
			m.detailViewport.GotoTop()

			// Mark as read and cache body in local UI — update in messages slice and thread groups
			wasRead := true
			if am != nil {
				wasRead = am.IsRead
			}
			for i, em := range m.messages {
				if em.ID == msg.Message.ID {
					wasRead = m.messages[i].IsRead
					m.messages[i].IsRead = msg.Message.IsRead
					m.messages[i].Body = msg.Message.Body
					m.messages[i].Attachments = msg.Attachments
					m.messages[i].HasAttachments = msg.Message.HasAttachments
				}
			}
			for ti := range m.threadGroups {
				for mi := range m.threadGroups[ti].Members {
					if m.threadGroups[ti].Members[mi].ID == msg.Message.ID {
						wasRead = m.threadGroups[ti].Members[mi].IsRead
						m.threadGroups[ti].Members[mi].IsRead = msg.Message.IsRead
						m.threadGroups[ti].Members[mi].Body = msg.Message.Body
						m.threadGroups[ti].Members[mi].Attachments = msg.Attachments
						m.threadGroups[ti].Members[mi].HasAttachments = msg.Message.HasAttachments
					}
				}
			}
			// Upsert message detail (body + read status) into cache
			if m.db != nil && len(m.folders) > 0 {
				if m.folders[m.selectedFolder].ID == "favorites" {
					_ = m.db.UpsertFavoriteMessage(*msg.Message)
				} else if m.config.UseSQLite == 1 {
					_ = m.db.UpsertMessage(m.folders[m.selectedFolder].ID, *msg.Message)
				}
				_ = m.db.UpdateReadStatus(msg.Message.ID, msg.Message.IsRead)
			}
			m.updateFolderUnreadCount(msg.Message.ID, msg.Message.IsRead, wasRead)
			m.statusMsg = "Message details loaded"
		}

	case attachmentsFetchedMsg:
		if am := m.activeMessage(); am != nil && am.ID == msg.MessageID {
			if m.detailMessage != nil && m.detailMessage.ID == msg.MessageID {
				remoteAtts := extractRemoteImages(m.detailMessage.Body.Content)
				msg.Attachments = append(msg.Attachments, remoteAtts...)
			}
			m.attachments = msg.Attachments
			m.selectedAttach = 0
			m = m.updateViewportSize()

			// Update in-memory collections so they have the loaded attachments too
			for i, em := range m.messages {
				if em.ID == msg.MessageID {
					m.messages[i].Attachments = msg.Attachments
					if len(msg.Attachments) > 0 {
						m.messages[i].HasAttachments = true
					}
				}
			}
			for ti := range m.threadGroups {
				for mi := range m.threadGroups[ti].Members {
					if m.threadGroups[ti].Members[mi].ID == msg.MessageID {
						m.threadGroups[ti].Members[mi].Attachments = msg.Attachments
						if len(msg.Attachments) > 0 {
							m.threadGroups[ti].Members[mi].HasAttachments = true
						}
					}
				}
			}
			// Upsert to SQLite cache to save it for future
			if m.db != nil && len(m.folders) > 0 {
				var cached *Message
				var err error
				if m.folders[m.selectedFolder].ID == "favorites" {
					cached, err = m.db.GetFavoriteMessage(msg.MessageID)
				} else if m.config.UseSQLite == 1 {
					cached, err = m.db.GetMessage(msg.MessageID)
				}
				if err == nil && cached != nil {
					cached.Attachments = msg.Attachments
					if len(msg.Attachments) > 0 {
						cached.HasAttachments = true
					}
					if m.folders[m.selectedFolder].ID == "favorites" {
						_ = m.db.UpsertFavoriteMessage(*cached)
					} else if m.config.UseSQLite == 1 {
						_ = m.db.UpsertMessage(m.folders[m.selectedFolder].ID, *cached)
					}
				}
			}
			m.statusMsg = "Attachments loaded"
		}

	case userEmailFetchedMsg:
		m.userEmail = string(msg)

	case inboxMessagesFetchedMsg:
		if m.inboxKnownIDs == nil {
			m.inboxKnownIDs = make(map[string]bool)
			for _, em := range msg.Messages {
				m.inboxKnownIDs[em.ID] = true
			}
			break
		}

		for _, em := range msg.Messages {
			if !m.inboxKnownIDs[em.ID] {
				if !em.IsRead {
					SendSystemNotification(em, m.config.TerminalBell != 0)
				}
				m.inboxKnownIDs[em.ID] = true
			}
		}

		newMap := make(map[string]bool)
		for _, em := range msg.Messages {
			newMap[em.ID] = true
		}
		m.inboxKnownIDs = newMap

	case MsgFileAttached:
		if msg.Err != nil {
			m.statusMsg = "File read error: " + msg.Err.Error()
			return m, nil
		}
		if len(msg.Data) > 50*1024*1024 {
			m.statusMsg = "Error: File exceeds the 50MB limit"
			return m, nil
		}
		m.composedFiles = append(m.composedFiles, PendingFile{
			Name:        msg.Name,
			ContentType: msg.ContentType,
			Data:        msg.Data,
		})
		m.statusMsg = "Attached " + msg.Name
		return m, nil

	case editorBodyLoadedMsg:
		// External editor exited; load returned content into compose body
		bodyStr := string(msg)
		m.composeBody.SetValue(bodyStr)

		// Find where the reply ends and the quoted message starts.
		// We position the cursor right after the user's reply.
		targetLine := -1
		lines := strings.Split(bodyStr, "\n")
		for i, line := range lines {
			if isOriginalMessageStart(line) {
				targetLine = i
				break
			}
		}

		// Move cursor to the very top (Line 0, Col 0)
		for m.composeBody.Line() > 0 || m.composeBody.LineInfo().RowOffset > 0 {
			m.composeBody.CursorUp()
		}
		m.composeBody.CursorStart()

		if targetLine > 0 {
			// Place the cursor on the line immediately before the quote block starts, at its end.
			lastLine := -1
			for m.composeBody.Line() < targetLine-1 {
				currLine := m.composeBody.Line()
				if currLine == lastLine {
					break
				}
				lastLine = currLine
				m.composeBody.CursorDown()
			}
			m.composeBody.CursorEnd()
		} else if targetLine == 0 {
			// Quote block starts at the very first line, keep cursor at top.
		} else {
			// No quote block found, move to the end of the entire document.
			lastLine := -1
			for m.composeBody.Line() < m.composeBody.LineCount()-1 {
				currLine := m.composeBody.Line()
				if currLine == lastLine {
					break
				}
				lastLine = currLine
				m.composeBody.CursorDown()
			}
			m.composeBody.CursorEnd()
		}

		// Switch focus to body field
		m.composeStep = 3
		m.updateComposeFocus()
		m.statusMsg = "Body loaded from editor"

	case mailSentMsg:
		m.state = stateMain
		m.statusMsg = "Email sent successfully!"
		m.composedFiles = nil
		m.composedImages = nil
		// Reload current folder
		if len(m.folders) > 0 {
			if m.folders[m.selectedFolder].ID == "favorites" {
				m, _ = m.loadCachedFolderMessages()
				m.updateFavoritesFolderCounts()
				return m, nil
			}
			return m, fetchMessagesCmd(m.graphClient, m.folders[m.selectedFolder].ID)
		}

	case mailDeletedMsg:
		m.statusMsg = "Message moved to Deleted Items"
		// Find if the deleted message was unread before deleting
		wasUnread := false
		var deletedMsgFolderID string
		for _, em := range m.messages {
			if em.ID == msg.MessageID {
				wasUnread = !em.IsRead
				break
			}
		}
		if !wasUnread {
			for _, tg := range m.threadGroups {
				for _, mem := range tg.Members {
					if mem.ID == msg.MessageID {
						wasUnread = !mem.IsRead
						break
					}
				}
			}
		}
		// Remove from SQLite cache and favorites
		if m.db != nil {
			if fID, err := m.db.GetMessageFolderID(msg.MessageID); err == nil && fID != "" {
				deletedMsgFolderID = fID
			}
			_ = m.db.DeleteMessage(msg.MessageID)
			_ = m.db.RemoveFromFavorites(msg.MessageID)
		}
		// Update folder unread count in memory
		if wasUnread && len(m.folders) > 0 {
			fID := deletedMsgFolderID
			if fID == "" {
				fID = m.folders[m.selectedFolder].ID
			}
			if fID != "favorites" {
				for i := range m.folders {
					if m.folders[i].ID == fID {
						m.folders[i].UnreadItemCount = m.folders[i].UnreadItemCount - 1
						if m.folders[i].UnreadItemCount < 0 {
							m.folders[i].UnreadItemCount = 0
						}
						break
					}
				}
			}
		}
		// Reload messages
		if len(m.folders) > 0 {
			if m.folders[m.selectedFolder].ID == "favorites" {
				m, _ = m.loadCachedFolderMessages()
				m.updateFavoritesFolderCounts()
				return m, nil
			}
			return m, tea.Batch(
				fetchMessagesCmd(m.graphClient, m.folders[m.selectedFolder].ID),
				fetchFoldersCmd(m.graphClient),
			)
		}

	case multipleMailsDeletedMsg:
		if len(msg.Errors) > 0 {
			m.statusMsg = fmt.Sprintf("Deleted thread with %d errors (e.g. %v)", len(msg.Errors), msg.Errors[0])
		} else {
			m.statusMsg = "Thread moved to Deleted Items"
		}

		// Find if any of the deleted messages were unread before deleting
		// and remove them from SQLite cache and favorites
		var deletedMsgFolderIDs []string
		unreadCount := 0

		for _, targetID := range msg.MessageIDs {
			wasUnread := false
			for _, em := range m.messages {
				if em.ID == targetID {
					wasUnread = !em.IsRead
					break
				}
			}
			if !wasUnread {
				for _, tg := range m.threadGroups {
					for _, mem := range tg.Members {
						if mem.ID == targetID {
							wasUnread = !mem.IsRead
							break
						}
					}
				}
			}
			if wasUnread {
				unreadCount++
			}
			if m.db != nil {
				if fID, err := m.db.GetMessageFolderID(targetID); err == nil && fID != "" {
					deletedMsgFolderIDs = append(deletedMsgFolderIDs, fID)
				}
				_ = m.db.DeleteMessage(targetID)
				_ = m.db.RemoveFromFavorites(targetID)
			}
		}

		// Update folder unread count in memory
		if unreadCount > 0 && len(m.folders) > 0 {
			fID := ""
			if len(deletedMsgFolderIDs) > 0 {
				fID = deletedMsgFolderIDs[0]
			}
			if fID == "" {
				fID = m.folders[m.selectedFolder].ID
			}
			if fID != "favorites" {
				for i := range m.folders {
					if m.folders[i].ID == fID {
						m.folders[i].UnreadItemCount = m.folders[i].UnreadItemCount - unreadCount
						if m.folders[i].UnreadItemCount < 0 {
							m.folders[i].UnreadItemCount = 0
						}
						break
					}
				}
			}
		}

		// Reload messages
		if len(m.folders) > 0 {
			if m.folders[m.selectedFolder].ID == "favorites" {
				m, _ = m.loadCachedFolderMessages()
				m.updateFavoritesFolderCounts()
				return m, nil
			}
			return m, tea.Batch(
				fetchMessagesCmd(m.graphClient, m.folders[m.selectedFolder].ID),
				fetchFoldersCmd(m.graphClient),
			)
		}

	case mailRestoredMsg:
		m.statusMsg = "Message restored to Inbox"
		// Remove from SQLite cache (as it is leaving the current folder, e.g. Deleted Items) and favorites
		if m.db != nil {
			_ = m.db.DeleteMessage(msg.MessageID)
			_ = m.db.RemoveFromFavorites(msg.MessageID)
		}
		// Reload messages
		if len(m.folders) > 0 {
			if m.folders[m.selectedFolder].ID == "favorites" {
				m, _ = m.loadCachedFolderMessages()
				m.updateFavoritesFolderCounts()
				return m, nil
			}
			return m, tea.Batch(
				fetchMessagesCmd(m.graphClient, m.folders[m.selectedFolder].ID),
				fetchFoldersCmd(m.graphClient),
			)
		}

	case attachmentSavedMsg:
		m.statusMsg = fmt.Sprintf("Saved attachment to: %s", msg)

	case youtrackTuiFinishedMsg:
		m.state = stateMain
		if msg.Err != nil {
			m.statusMsg = fmt.Sprintf("yt-tui finished with error: %v", msg.Err)
		} else {
			m.statusMsg = "yt-tui closed"
		}
		return m, nil

	case gitlabTuiFinishedMsg:
		m.state = stateMain
		if msg.Err != nil {
			m.statusMsg = fmt.Sprintf("gitlab-tui finished with error: %v", msg.Err)
		} else {
			m.statusMsg = "gitlab-tui closed"
		}
		return m, nil

	case browserFinishedMsg:
		m.state = stateMain
		if msg.Err != nil {
			m.statusMsg = fmt.Sprintf("Failed to launch browser: %v", msg.Err)
		} else {
			m.statusMsg = "Link opened in browser"
		}
		return m, nil

	case tickMsg:
		// Background tick: fetch folders and current messages silently
		if m.state == stateMain && m.graphClient != nil {
			var bgCmds []tea.Cmd
			// Fetch folders to update counts
			bgCmds = append(bgCmds, func() tea.Msg {
				fld, err := m.graphClient.GetFolders()
				if err == nil {
					return foldersFetchedMsg(fld)
				}
				return nil // Ignore background errors to prevent disruptive popups
			})

			// Fetch current folder messages
			if len(m.folders) > 0 {
				folderID := m.folders[m.selectedFolder].ID
				if folderID != "favorites" {
					bgCmds = append(bgCmds, func() tea.Msg {
						msgs, err := m.graphClient.GetMessages(folderID)
						if err == nil {
							return messagesFetchedMsg{FolderID: folderID, Messages: msgs}
						}
						return nil
					})
				}
			}

			// Fetch inbox messages for notification tracking
			bgCmds = append(bgCmds, fetchInboxMessagesCmd(m.graphClient))

			return m, tea.Batch(
				tea.Batch(bgCmds...),
				m.tickCmd(), // Schedule next tick
			)
		}
	}

	// State-specific input handling
	switch m.state {
	case stateConfig:
		var cmd tea.Cmd
		m.txtInput, cmd = m.txtInput.Update(msg)
		cmds = append(cmds, cmd)

		if key, ok := msg.(tea.KeyMsg); ok && key.String() == "enter" {
			val := strings.TrimSpace(m.txtInput.Value())
			if val != "" {
				if m.configStep == 0 {
					m.config.ClientID = val
					m.configStep = 1
					m.txtInput.Placeholder = "Enter Tenant ID (default 'common')..."
					m.txtInput.SetValue("common")
					m.txtInput.Focus()
				} else {
					m.config.TenantID = val
					m.state = stateLoading
					m.statusMsg = "Requesting device code..."
					cmds = append(cmds, fetchDeviceCodeCmd(m.config.ClientID, m.config.TenantID))
				}
			}
		}

	case stateDeviceAuth:
		// Waiting for poll, user can exit or retry config by pressing Esc
		if key, ok := msg.(tea.KeyMsg); ok && key.String() == "esc" {
			m.state = stateConfig
			m.configStep = 0
			m.txtInput.Placeholder = "Enter Microsoft Client ID..."
			m.txtInput.SetValue("")
			m.txtInput.Focus()
		}

	case stateMain:
		key, ok := msg.(tea.KeyMsg)
		if !ok {
			break
		}

		scrollLines := m.config.ScrollLines
		if scrollLines <= 0 {
			scrollLines = 1
		}

		switch key.String() {
		case "q":
			return m, tea.Batch(clearKittyImagesCmd(), tea.Quit)
		case "tab":
			// Switch pane focus
			m.activePane = (m.activePane + 1) % 3
		case "shift+tab":
			m.activePane = (m.activePane - 1 + 3) % 3
		case "left":
			if m.activePane > paneFolders {
				m.activePane--
			}
		case "right":
			if m.activePane < paneDetail {
				m.activePane++
			}
		case "up":
			switch m.activePane {
			case paneFolders:
				if m.selectedFolder > 0 {
					var cmd tea.Cmd
					m, cmd = m.selectFolder(m.selectedFolder - 1)
					if cmd != nil {
						cmds = append(cmds, cmd)
					}
				}
			case paneMessages:
				if m.virtualSelected > 0 {
					m.virtualSelected--
					var cmd tea.Cmd
					m, cmd = m.loadMessageDetail(m.activeMessage())
					if cmd != nil {
						cmds = append(cmds, cmd)
					}
				}
			case paneDetail:
				m.detailViewport.LineUp(scrollLines)
			}
		case "k":
			// vim-key: only navigates lists in folders/messages, scrolls detail
			switch m.activePane {
			case paneFolders:
				if m.selectedFolder > 0 {
					var cmd tea.Cmd
					m, cmd = m.selectFolder(m.selectedFolder - 1)
					if cmd != nil {
						cmds = append(cmds, cmd)
					}
				}
			case paneMessages:
				if m.virtualSelected > 0 {
					m.virtualSelected--
					var cmd tea.Cmd
					m, cmd = m.loadMessageDetail(m.activeMessage())
					if cmd != nil {
						cmds = append(cmds, cmd)
					}
				}
			case paneDetail:
				m.detailViewport.LineUp(scrollLines)
			}
		case "K":
			// Capital K: navigate up in Messages pane if in Folders pane, or scroll up message detail if in Messages pane
			switch m.activePane {
			case paneFolders:
				if m.virtualSelected > 0 {
					m.virtualSelected--
					var cmd tea.Cmd
					m, cmd = m.loadMessageDetail(m.activeMessage())
					if cmd != nil {
						cmds = append(cmds, cmd)
					}
				}
			case paneMessages:
				m.detailViewport.LineUp(scrollLines)
			}
		case "down":
			switch m.activePane {
			case paneFolders:
				if m.selectedFolder < len(m.folders)-1 {
					var cmd tea.Cmd
					m, cmd = m.selectFolder(m.selectedFolder + 1)
					if cmd != nil {
						cmds = append(cmds, cmd)
					}
				}
			case paneMessages:
				if m.virtualSelected < len(m.virtualList)-1 {
					m.virtualSelected++
					var cmd tea.Cmd
					m, cmd = m.loadMessageDetail(m.activeMessage())
					if cmd != nil {
						cmds = append(cmds, cmd)
					}
				}
			case paneDetail:
				m.detailViewport.LineDown(scrollLines)
			}
		case "j":
			// vim-key: only navigates lists in folders/messages, scrolls detail
			switch m.activePane {
			case paneFolders:
				if m.selectedFolder < len(m.folders)-1 {
					var cmd tea.Cmd
					m, cmd = m.selectFolder(m.selectedFolder + 1)
					if cmd != nil {
						cmds = append(cmds, cmd)
					}
				}
			case paneMessages:
				if m.virtualSelected < len(m.virtualList)-1 {
					m.virtualSelected++
					var cmd tea.Cmd
					m, cmd = m.loadMessageDetail(m.activeMessage())
					if cmd != nil {
						cmds = append(cmds, cmd)
					}
				}
			case paneDetail:
				m.detailViewport.LineDown(scrollLines)
			}
		case "J":
			// Capital J: navigate down in Messages pane if in Folders pane, or scroll down message detail if in Messages pane
			switch m.activePane {
			case paneFolders:
				if m.virtualSelected < len(m.virtualList)-1 {
					m.virtualSelected++
					var cmd tea.Cmd
					m, cmd = m.loadMessageDetail(m.activeMessage())
					if cmd != nil {
						cmds = append(cmds, cmd)
					}
				}
			case paneMessages:
				m.detailViewport.LineDown(scrollLines)
			}
		case "pageup":
			if m.activePane == paneDetail {
				m.detailViewport.HalfPageUp()
			}
		case "pagedown":
			if m.activePane == paneDetail {
				m.detailViewport.HalfPageDown()
			}
		case " ":
			// Toggle thread collapse/expand in the Messages or Folders pane
			if (m.activePane == paneMessages || m.activePane == paneFolders) && len(m.virtualList) > 0 && m.virtualSelected < len(m.virtualList) {
				item := m.virtualList[m.virtualSelected]
				tg := m.threadGroups[item.ThreadIdx]
				if len(tg.Members) > 1 {
					cid := tg.ConversationID
					m.collapsedThreads[cid] = !m.collapsedThreads[cid]
					targetThreadIdx := item.ThreadIdx
					// Rebuild virtual list and keep selection on the thread's header row
					m.buildVirtualList()
					found := false
					for i, v := range m.virtualList {
						if v.ThreadIdx == targetThreadIdx && v.IsHeader {
							m.virtualSelected = i
							found = true
							break
						}
					}
					if !found {
						if m.virtualSelected >= len(m.virtualList) {
							m.virtualSelected = len(m.virtualList) - 1
						}
						if m.virtualSelected < 0 {
							m.virtualSelected = 0
						}
					}
				}
			}
		case "n":
			// Compose new email
			m.state = stateCompose
			m.composeStep = 0
			m.composeReplyToID = "" // not a reply
			m.composeIsReplyAll = false
			m.composedImages = nil
			m.composedFiles = nil
			m.loadContacts()

			m.composeTo = textinput.New()
			m.composeTo.Placeholder = "recipient@domain.com"
			m.composeTo.Focus()
			m.composeTo.Width = m.width - 20

			m.composeCc = textinput.New()
			m.composeCc.Placeholder = "cc@domain.com (optional)"
			m.composeCc.Width = m.width - 20

			m.composeSubject = textinput.New()
			m.composeSubject.Placeholder = "Email subject..."
			m.composeSubject.Width = m.width - 20

			m.composeBody = textarea.New()
			m.composeBody.ShowLineNumbers = false
			m.composeBody.Placeholder = "Type email body here..."
			m.composeBody.SetWidth(m.width - 20)
			h := m.height - 18
			if h < 3 {
				h = 3
			}
			m.composeBody.SetHeight(h)
		case "d", "delete":
			// Delete current message
			if am := m.activeMessage(); am != nil {
				m.statusMsg = "Moving message to Deleted Items..."
				cmds = append(cmds, deleteMailCmd(m.graphClient, am.ID))
			}
		case "D":
			// Delete current thread with confirmation
			if am := m.activeMessage(); am != nil {
				item := m.virtualList[m.virtualSelected]
				tg := m.threadGroups[item.ThreadIdx]
				var ids []string
				for _, mem := range tg.Members {
					ids = append(ids, mem.ID)
				}
				if len(ids) > 0 {
					m.deleteThreadMsgIDs = ids
					m.deleteThreadSubject = tg.Subject
					m.state = stateDeleteThreadConfirm
				}
			}
		case "U":
			// Undelete/Restore current message to Inbox
			if am := m.activeMessage(); am != nil {
				m.statusMsg = "Restoring message to Inbox..."
				cmds = append(cmds, restoreMailCmd(m.graphClient, am.ID))
			}
		case "R":
			// Mark message Read/Unread
			if am := m.activeMessage(); am != nil {
				targetState := !am.IsRead
				msgID := am.ID
				wasRead := am.IsRead
				// Update in messages slice
				for i := range m.messages {
					if m.messages[i].ID == msgID {
						m.messages[i].IsRead = targetState
					}
				}
				// Update in thread groups
				for ti := range m.threadGroups {
					for mi := range m.threadGroups[ti].Members {
						if m.threadGroups[ti].Members[mi].ID == msgID {
							m.threadGroups[ti].Members[mi].IsRead = targetState
						}
					}
				}
				m.statusMsg = "Marking message read status..."
				if m.db != nil {
					_ = m.db.UpdateReadStatus(msgID, targetState)
					m.updateFavoritesFolderCounts()
				}
				m.updateFolderUnreadCount(msgID, targetState, wasRead)
				cmds = append(cmds, func() tea.Msg {
					_ = m.graphClient.MarkAsRead(msgID, targetState)
					return nil
				})
			}
		case "f":
			// Toggle Favorite status
			if am := m.activeMessage(); am != nil && m.db != nil {
				isFav, err := m.db.IsFavorite(am.ID)
				if err == nil {
					if isFav {
						_ = m.db.RemoveFromFavorites(am.ID)
						m.statusMsg = "Removed from Favorites"
					} else {
						msgToSave := *am
						if cached, err := m.db.GetMessage(am.ID); err == nil && cached != nil {
							msgToSave = *cached
						}
						_ = m.db.UpsertFavoriteMessage(msgToSave)
						m.statusMsg = "Added to Favorites"
					}
					m.updateFavoritesFolderCounts()
					if m.folders[m.selectedFolder].ID == "favorites" {
						m, _ = m.loadCachedFolderMessages()
						// Adjust selection if we removed the last item
						if m.virtualSelected >= len(m.virtualList) {
							m.virtualSelected = max(0, len(m.virtualList)-1)
						}
						// Refresh detail view for new selection
						var detailCmd tea.Cmd
						if len(m.virtualList) > 0 {
							m, detailCmd = m.loadMessageDetail(m.activeMessage())
						} else {
							m, detailCmd = m.loadMessageDetail(nil)
						}
						if detailCmd != nil {
							cmds = append(cmds, detailCmd)
						}
					}
				}
			}
		case "r":
			// Reload selected folder
			if len(m.folders) > 0 {
				if m.folders[m.selectedFolder].ID == "favorites" {
					m, _ = m.loadCachedFolderMessages()
					m.updateFavoritesFolderCounts()
				} else {
					m.statusMsg = fmt.Sprintf("Reloading messages for %s...", m.folders[m.selectedFolder].DisplayName)
					cmds = append(cmds, fetchMessagesCmd(m.graphClient, m.folders[m.selectedFolder].ID))
				}
			}
		case "M":
			// Load more messages for the selected folder
			if len(m.folders) > 0 {
				if m.folders[m.selectedFolder].ID == "favorites" {
					m.statusMsg = "No more messages to load"
				} else {
					m.statusMsg = fmt.Sprintf("Loading more messages for %s...", m.folders[m.selectedFolder].DisplayName)
					cmds = append(cmds, fetchNextMessagesCmd(m.graphClient, m.folders[m.selectedFolder].ID, len(m.messages)))
				}
			}
		case "a":
			// Open attachments pane if message has attachments
			if m.detailMessage != nil && len(m.attachments) > 0 {
				m.state = stateAttachments
				m.selectedAttach = 0
				cmds = append(cmds, clearKittyImagesCmd())
			}
		case "A":
			// Ask if user wants to reply to sender or all
			if am := m.activeMessage(); am != nil {
				var origTo []Recipient
				var origCc []Recipient
				senderAddr := am.From.EmailAddress.Address
				if m.detailMessage != nil && m.detailMessage.ID == am.ID {
					origTo = m.detailMessage.ToRecipients
					origCc = m.detailMessage.CcRecipients
					if m.detailMessage.From.EmailAddress.Address != "" {
						senderAddr = m.detailMessage.From.EmailAddress.Address
					}
				} else {
					origTo = am.ToRecipients
					origCc = am.CcRecipients
				}

				uniqueOthers := make(map[string]bool)
				userEmailLower := strings.ToLower(strings.TrimSpace(m.userEmail))

				senderAddr = strings.ToLower(strings.TrimSpace(senderAddr))
				if senderAddr != "" && (userEmailLower == "" || senderAddr != userEmailLower) {
					uniqueOthers[senderAddr] = true
				}

				for _, r := range origTo {
					addr := strings.ToLower(strings.TrimSpace(r.EmailAddress.Address))
					if addr != "" && (userEmailLower == "" || addr != userEmailLower) {
						uniqueOthers[addr] = true
					}
				}

				for _, r := range origCc {
					addr := strings.ToLower(strings.TrimSpace(r.EmailAddress.Address))
					if addr != "" && (userEmailLower == "" || addr != userEmailLower) {
						uniqueOthers[addr] = true
					}
				}

				if len(uniqueOthers) <= 1 {
					m.initiateReply(false)
				} else {
					m.state = stateReplyConfirm
					m.statusMsg = "Select reply option (s/a/c)"
				}
			}
		case "y":
			// Show Yank option selection dropdown
			am := m.activeMessage()
			if am == nil {
				m.statusMsg = "No message selected"
				break
			}
			if m.detailMessage == nil || m.detailMessage.ID != am.ID {
				m.statusMsg = "Message details loading, please try again..."
				break
			}
			m.state = stateYankSelect
			m.selectedYankIdx = 0
			m.statusMsg = "Yank: [m] Msg (no quotes), [a] All (with quotes), [u] URLs, [s] Subject"
			m = m.updateViewportSize()
		case "o":
			am := m.activeMessage()
			if am == nil {
				m.statusMsg = "No message selected"
				break
			}
			if m.detailMessage == nil || m.detailMessage.ID != am.ID || m.detailMessage.Body.Content == "" {
				m.statusMsg = "Message details loading, please try again..."
				break
			}
			allURLs := extractAllURLsForOpen(m.detailMessage.Body.Content, m.detailMessage.Subject)
			totalCount := len(allURLs)

			if totalCount == 0 {
				m.statusMsg = "No URLs found in the message"
				break
			}

			if totalCount == 1 {
				targetURL := allURLs[0]
				urlType, normURL := classifyURL(targetURL)
				if urlType == "gitlab" {
					if _, err := exec.LookPath("gitlab-tui"); err == nil {
						m.state = stateLoading
						m.statusMsg = "Launching gitlab-tui..."
						return m, openGitLabTuiCmd(normURL)
					}
				} else if urlType == "youtrack" {
					if _, err := exec.LookPath("yt-tui"); err == nil {
						m.state = stateLoading
						m.statusMsg = "Launching yt-tui..."
						return m, openYouTrackTuiCmd(normURL)
					}
				}

				m.state = stateMain
				m.statusMsg = "Opening link in browser..."
				return m, m.openBrowserCmd(normURL)
			}

			// Multiple URLs: show popup/modal
			m.extractedURLs = allURLs
			m.selectedURLIdx = 0
			m.state = stateExternalURLSelect
		case "ctrl+g":
			am := m.activeMessage()
			if am == nil {
				m.statusMsg = "No message selected"
				break
			}
			if m.detailMessage == nil || m.detailMessage.ID != am.ID || m.detailMessage.Body.Content == "" {
				m.statusMsg = "Message details loading, please try again..."
				break
			}
			m.statusMsg = "Opening external editor…"
			return m, m.viewMessageInEditorCmd()
		case "?":
			m.state = stateHelp
			m = m.updateViewportSize()
			m.helpViewport.SetContent(m.renderHelpContent())
			m.helpViewport.GotoTop()
		}

	case stateCompose:
		key, ok := msg.(tea.KeyMsg)
		if !ok {
			break
		}

		if m.config.UseSQLite != 0 && (m.composeStep == 0 || m.composeStep == 1) && len(m.filteredContacts) > 0 {
			switch key.String() {
			case "down":
				m.contactsSelected = (m.contactsSelected + 1) % len(m.filteredContacts)
				if m.contactsSelected == 0 {
					m.contactsStartIdx = 0
				} else if m.contactsSelected >= m.contactsStartIdx+5 {
					m.contactsStartIdx = m.contactsSelected - 5 + 1
				}
				return m, nil
			case "up":
				m.contactsSelected = (m.contactsSelected - 1 + len(m.filteredContacts)) % len(m.filteredContacts)
				if m.contactsSelected == len(m.filteredContacts)-1 {
					m.contactsStartIdx = len(m.filteredContacts) - 5
					if m.contactsStartIdx < 0 {
						m.contactsStartIdx = 0
					}
				} else if m.contactsSelected < m.contactsStartIdx {
					m.contactsStartIdx = m.contactsSelected
				}
				return m, nil
			case "enter":
				selected := m.filteredContacts[m.contactsSelected]
				var inputToUpdate *textinput.Model
				if m.composeStep == 0 {
					inputToUpdate = &m.composeTo
				} else {
					inputToUpdate = &m.composeCc
				}
				parts := strings.Split(inputToUpdate.Value(), ",")
				if len(parts) > 0 {
					var newAddress string
					if selected.Name != "" {
						newAddress = fmt.Sprintf("%s <%s>", selected.Name, selected.Address)
					} else {
						newAddress = selected.Address
					}
					parts[len(parts)-1] = " " + newAddress
					newValue := strings.TrimLeft(strings.Join(parts, ","), " ")
					inputToUpdate.SetValue(newValue + ", ")
					inputToUpdate.SetCursor(len(inputToUpdate.Value()))
				}
				m.filteredContacts = nil
				m.contactsSelected = 0
				m.contactsStartIdx = 0
				return m, nil
			case "esc":
				m.filteredContacts = nil
				m.contactsSelected = 0
				m.contactsStartIdx = 0
				return m, nil
			}
		}

		switch key.String() {
		case "esc":
			if strings.TrimSpace(m.composeBody.Value()) != "" {
				m.state = stateComposeCancelConfirm
			} else {
				m.state = stateMain
				m.statusMsg = "Compose cancelled"
				m.composedImages = nil
				m.composedFiles = nil
			}
		case "ctrl+f":
			m.state = stateFileBrowse
			sortBy, sortOrder, lastDir := LoadFilepickerSettings()
			if lastDir != "" {
				m.filepicker.CurrentDirectory = lastDir
			}
			if sortBy == "Datetime" {
				m.filepicker.SortBy = filepicker.SortByDatetime
			} else {
				m.filepicker.SortBy = filepicker.SortByName
			}
			if sortOrder == "desc" {
				m.filepicker.SortOrder = filepicker.SortDescending
			} else {
				m.filepicker.SortOrder = filepicker.SortAscending
			}
			h := m.height - 17
			if h < 5 {
				h = 5
			}
			m.filepicker.SetHeight(h)
			return m, m.filepicker.Init()
		case "tab":
			m.composeStep = (m.composeStep + 1) % 4
			m.updateComposeFocus()
		case "shift+tab":
			m.composeStep = (m.composeStep - 1 + 4) % 4
			m.updateComposeFocus()
		case "ctrl+g":
			// Open compose body in external editor ($EDITOR / $VISUAL / vi)
			m.statusMsg = "Opening external editor…"
			return m, openEditorCmd(m.composeBody.Value())
		case "ctrl+v", "ctrl+shift+v", "ctrl+V":
			if m.composeStep == 3 {
				imgBytes, contentType, err := GetClipboardImage()
				if err == nil && len(imgBytes) > 0 {
					m.composedImages = append(m.composedImages, PastedImage{
						Bytes:       imgBytes,
						ContentType: contentType,
					})
					placeholder := fmt.Sprintf("[Image %d]", len(m.composedImages))
					m.composeBody.InsertString(placeholder)
					m.statusMsg = "Image pasted from clipboard"
					return m, nil
				}
			}
			// If not focused on body or if GetClipboardImage fails, fall through to default to let standard text paste work
			var cmd tea.Cmd
			switch m.composeStep {
			case 0:
				m.composeTo, cmd = m.composeTo.Update(msg)
				m.updateFilteredContacts()
			case 1:
				m.composeCc, cmd = m.composeCc.Update(msg)
				m.updateFilteredContacts()
			case 2:
				m.composeSubject, cmd = m.composeSubject.Update(msg)
			case 3:
				m.composeBody, cmd = m.composeBody.Update(msg)
			}
			cmds = append(cmds, cmd)
		case "ctrl+s", "ctrl+S", "ctrl+x":
			// Send!
			m.statusMsg = "Sending email..."
			bodyToSend := m.composeBody.Value()
			// When replying, the compose body is pre-filled with a quoted original
			// message for reference. The Graph API reply endpoint appends its own
			// quoted thread automatically, so we must strip our local quote before
			// sending to avoid the recipient seeing a doubled quotation.
			if m.composeReplyToID != "" {
				if idx := strings.Index(bodyToSend, "\n\nOn "); idx >= 0 {
					bodyToSend = bodyToSend[:idx]
				}
			}
			cmds = append(cmds, sendMailCmd(
				m.graphClient,
				m.composeTo.Value(),
				m.composeCc.Value(),
				m.composeSubject.Value(),
				bodyToSend,
				m.composeReplyToID,
				m.composeIsReplyAll,
				m.composedImages,
				m.composedFiles,
			))
			m.composedImages = nil
			m.composedFiles = nil
		default:
			// Update the focused compose input
			var cmd tea.Cmd
			switch m.composeStep {
			case 0:
				m.composeTo, cmd = m.composeTo.Update(msg)
				m.updateFilteredContacts()
			case 1:
				m.composeCc, cmd = m.composeCc.Update(msg)
				m.updateFilteredContacts()
			case 2:
				m.composeSubject, cmd = m.composeSubject.Update(msg)
			case 3:
				m.composeBody, cmd = m.composeBody.Update(msg)
			}
			cmds = append(cmds, cmd)
		}

	case stateAttachments:
		key, ok := msg.(tea.KeyMsg)
		if !ok {
			break
		}

		switch key.String() {
		case "esc":
			m.state = stateMain
			cmds = append(cmds, clearKittyImagesCmd())
		case "up", "k":
			if m.selectedAttach > 0 {
				m.selectedAttach--
				cmds = append(cmds, clearKittyImagesCmd())
			}
		case "down", "j":
			if m.selectedAttach < len(m.attachments)-1 {
				m.selectedAttach++
				cmds = append(cmds, clearKittyImagesCmd())
			}
		case "enter":
			// Save attachment
			m.statusMsg = "Downloading attachment..."
			var msgID string
			if am := m.activeMessage(); am != nil {
				msgID = am.ID
			}
			cmds = append(cmds, saveAttachmentCmd(msgID, m.attachments, m.selectedAttach, m.config.ImageViewer, m.config.AttachmentDir))
		}

	case stateReplyConfirm:
		key, ok := msg.(tea.KeyMsg)
		if !ok {
			break
		}
		switch key.String() {
		case "esc", "c", "q":
			m.state = stateMain
			m.statusMsg = "Reply cancelled"
		case "s":
			m.initiateReply(false)
		case "a":
			m.initiateReply(true)
		}

	case stateURLSelect:
		key, ok := msg.(tea.KeyMsg)
		if !ok {
			break
		}
		switch key.String() {
		case "esc", "q":
			m.state = stateMain
			m.statusMsg = "URL copy cancelled"
			m = m.updateViewportSize()
		case "up", "k":
			if m.selectedURLIdx > 0 {
				m.selectedURLIdx--
			}
		case "down", "j":
			if m.selectedURLIdx < len(m.extractedURLs)-1 {
				m.selectedURLIdx++
			}
		case "enter":
			url := m.extractedURLs[m.selectedURLIdx]
			if err := clipboard.WriteAll(url); err != nil {
				m.statusMsg = fmt.Sprintf("Failed to copy URL: %v", err)
			} else {
				m.statusMsg = "Copied URL to clipboard!"
			}
			m.state = stateMain
			m = m.updateViewportSize()
		}

	case stateYankSelect:
		key, ok := msg.(tea.KeyMsg)
		if !ok {
			break
		}
		switch key.String() {
		case "esc", "q":
			m.state = stateMain
			m.statusMsg = "Yank cancelled"
			m = m.updateViewportSize()
		case "up", "k":
			if m.selectedYankIdx > 0 {
				m.selectedYankIdx--
			} else {
				m.selectedYankIdx = len(yankOptions) - 1
			}
		case "down", "j":
			if m.selectedYankIdx < len(yankOptions)-1 {
				m.selectedYankIdx++
			} else {
				m.selectedYankIdx = 0
			}
		case "enter":
			if m.selectedYankIdx >= 0 && m.selectedYankIdx < len(yankOptions) {
				m = m.executeYank(yankOptions[m.selectedYankIdx].key)
			}
		case "m":
			m = m.executeYank("m")
		case "a":
			m = m.executeYank("a")
		case "u":
			m = m.executeYank("u")
		case "s":
			m = m.executeYank("s")
		}

	case stateExternalURLSelect:
		key, ok := msg.(tea.KeyMsg)
		if !ok {
			break
		}
		switch key.String() {
		case "esc", "q":
			m.state = stateMain
			m.statusMsg = "Selection cancelled"
		case "up", "k":
			if m.selectedURLIdx > 0 {
				m.selectedURLIdx--
			}
		case "down", "j":
			if m.selectedURLIdx < len(m.extractedURLs)-1 {
				m.selectedURLIdx++
			}
		case "enter":
			if m.selectedURLIdx >= 0 && m.selectedURLIdx < len(m.extractedURLs) {
				urlStr := m.extractedURLs[m.selectedURLIdx]
				urlType, normURL := classifyURL(urlStr)
				if urlType == "gitlab" {
					if _, err := exec.LookPath("gitlab-tui"); err == nil {
						m.state = stateLoading
						m.statusMsg = "Launching gitlab-tui..."
						return m, openGitLabTuiCmd(normURL)
					}
				} else if urlType == "youtrack" {
					if _, err := exec.LookPath("yt-tui"); err == nil {
						m.state = stateLoading
						m.statusMsg = "Launching yt-tui..."
						return m, openYouTrackTuiCmd(normURL)
					}
				}

				m.state = stateMain
				m.statusMsg = "Opening link in browser..."
				return m, m.openBrowserCmd(normURL)
			}
		}

	case stateYouTrackInstallPrompt:
		key, ok := msg.(tea.KeyMsg)
		if !ok {
			break
		}
		switch key.String() {
		case "esc", "q", "enter":
			m.state = stateMain
			m.statusMsg = "Ready"
		}

	case stateGitLabInstallPrompt:
		key, ok := msg.(tea.KeyMsg)
		if !ok {
			break
		}
		switch key.String() {
		case "esc", "q", "enter":
			m.state = stateMain
			m.statusMsg = "Ready"
		}

	case stateHelp:
		key, ok := msg.(tea.KeyMsg)
		if !ok {
			break
		}
		scrollLines := m.config.ScrollLines
		if scrollLines <= 0 {
			scrollLines = 1
		}
		switch key.String() {
		case "esc", "q", "?":
			m.state = stateMain
			m.statusMsg = "Ready"
		case "up", "k":
			m.helpViewport.LineUp(scrollLines)
		case "down", "j":
			m.helpViewport.LineDown(scrollLines)
		case "pageup":
			m.helpViewport.HalfPageUp()
		case "pagedown":
			m.helpViewport.HalfPageDown()
		}

	case stateFileBrowse:
		key, ok := msg.(tea.KeyMsg)
		if !ok {
			break
		}

		switch key.String() {
		case "esc", "q":
			m.state = stateCompose
			return m, nil
		}

		var cmd tea.Cmd
		m.filepicker, cmd = m.filepicker.Update(msg)

		if key.String() == "s" || key.String() == "ctrl+s" || key.String() == "o" || key.String() == "ctrl+o" {
			_ = SaveFilepickerSettings(m.filepicker.SortBy.String(), m.filepicker.SortOrder.String(), m.filepicker.CurrentDirectory)
		}

		if didSelect, path := m.filepicker.DidSelectFile(msg); didSelect {
			_ = SaveFilepickerSettings(m.filepicker.SortBy.String(), m.filepicker.SortOrder.String(), m.filepicker.CurrentDirectory)
			m.state = stateCompose
			return m, tea.Batch(cmd, attachFileFromFilepathCmd(path))
		}

		cmds = append(cmds, cmd)

	case stateComposeCancelConfirm:
		key, ok := msg.(tea.KeyMsg)
		if !ok {
			break
		}
		switch key.String() {
		case "y", "Y":
			m.state = stateMain
			m.statusMsg = "Compose cancelled"
			m.composedImages = nil
			m.composedFiles = nil
		case "n", "N", "esc":
			m.state = stateCompose
		}

	case stateDeleteThreadConfirm:
		key, ok := msg.(tea.KeyMsg)
		if !ok {
			break
		}
		switch key.String() {
		case "y", "Y":
			m.state = stateMain
			if len(m.deleteThreadMsgIDs) > 0 {
				m.statusMsg = fmt.Sprintf("Moving %d message(s) in thread to Deleted Items...", len(m.deleteThreadMsgIDs))
				cmds = append(cmds, deleteMultipleMailsCmd(m.graphClient, m.deleteThreadMsgIDs))
			}
			m.deleteThreadMsgIDs = nil
			m.deleteThreadSubject = ""
		case "n", "N", "esc":
			m.state = stateMain
			m.deleteThreadMsgIDs = nil
			m.deleteThreadSubject = ""
		}
	}

	if m.state == stateCompose || m.state == stateComposeCancelConfirm {
		m.updateComposeBodyHeight()
	}

	return m, tea.Batch(cmds...)
}

func (m *mainModel) initiateReply(replyAll bool) {
	origMsgPtr := m.activeMessage()
	if origMsgPtr == nil {
		m.state = stateMain
		return
	}
	origMsg := *origMsgPtr

	var bodyText string
	senderName := origMsg.From.EmailAddress.Name
	senderAddr := origMsg.From.EmailAddress.Address
	receivedTime := origMsg.ReceivedDateTime

	if m.detailMessage != nil && m.detailMessage.ID == origMsg.ID {
		bodyText = m.detailMessage.Body.Content
		if m.detailMessage.From.EmailAddress.Address != "" {
			senderName = m.detailMessage.From.EmailAddress.Name
			senderAddr = m.detailMessage.From.EmailAddress.Address
			receivedTime = m.detailMessage.ReceivedDateTime
		}
	} else {
		bodyText = origMsg.BodyPreview
	}

	m.state = stateCompose
	m.composeStep = 3               // Focus body field
	m.composeReplyToID = origMsg.ID // remember original message so we use the reply endpoint
	m.composeIsReplyAll = replyAll
	m.composedImages = nil
	m.composedFiles = nil
	m.loadContacts()

	m.composeTo = textinput.New()
	m.composeTo.Placeholder = "recipient@domain.com"
	m.composeTo.Width = m.width - 20

	var recipients []string
	if senderAddr != "" {
		if senderName != "" {
			recipients = append(recipients, fmt.Sprintf("%s <%s>", senderName, senderAddr))
		} else {
			recipients = append(recipients, senderAddr)
		}
	}

	var ccRecipients []string

	if replyAll {
		var origTo []Recipient
		var origCc []Recipient
		if m.detailMessage != nil && m.detailMessage.ID == origMsg.ID {
			origTo = m.detailMessage.ToRecipients
			origCc = m.detailMessage.CcRecipients
		} else {
			origTo = origMsg.ToRecipients
			origCc = origMsg.CcRecipients
		}

		hasEmail := func(addr string, list []string) bool {
			addr = strings.ToLower(strings.TrimSpace(addr))
			if addr == "" {
				return true
			}
			if m.userEmail != "" && strings.ToLower(m.userEmail) == addr {
				return true
			}
			for _, r := range list {
				checkAddr := strings.ToLower(strings.TrimSpace(r))
				if strings.Contains(checkAddr, "<") && strings.Contains(checkAddr, ">") {
					start := strings.Index(checkAddr, "<")
					end := strings.Index(checkAddr, ">")
					if start < end {
						checkAddr = checkAddr[start+1 : end]
					}
				}
				if checkAddr == addr {
					return true
				}
			}
			return false
		}

		for _, r := range origTo {
			addr := r.EmailAddress.Address
			name := r.EmailAddress.Name
			if !hasEmail(addr, recipients) {
				if name != "" {
					recipients = append(recipients, fmt.Sprintf("%s <%s>", name, addr))
				} else {
					recipients = append(recipients, addr)
				}
			}
		}

		for _, r := range origCc {
			addr := r.EmailAddress.Address
			name := r.EmailAddress.Name
			if !hasEmail(addr, recipients) && !hasEmail(addr, ccRecipients) {
				if name != "" {
					ccRecipients = append(ccRecipients, fmt.Sprintf("%s <%s>", name, addr))
				} else {
					ccRecipients = append(ccRecipients, addr)
				}
			}
		}
	}

	m.composeTo.SetValue(strings.Join(recipients, ", "))

	m.composeCc = textinput.New()
	m.composeCc.Placeholder = "cc@domain.com (optional)"
	m.composeCc.Width = m.width - 20
	m.composeCc.SetValue(strings.Join(ccRecipients, ", "))

	subject := origMsg.Subject
	if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(subject)), "re:") {
		subject = "Re: " + subject
	}
	m.composeSubject = textinput.New()
	m.composeSubject.Placeholder = "Email subject..."
	m.composeSubject.SetValue(subject)
	m.composeSubject.Width = m.width - 20

	m.composeBody = textarea.New()
	m.composeBody.ShowLineNumbers = false
	m.composeBody.Placeholder = "Type email body here..."
	m.composeBody.SetWidth(m.width - 20)
	h := m.height - 18
	if h < 3 {
		h = 3
	}
	m.composeBody.SetHeight(h)

	var quotedBody strings.Builder
	quotedBody.WriteString("\n\n")
	formattedTime := receivedTime.Local().Format("Mon, Jan 2, 2006 at 15:04")
	if senderName != "" {
		quotedBody.WriteString(fmt.Sprintf("On %s, %s <%s> wrote:\n", formattedTime, senderName, senderAddr))
	} else {
		quotedBody.WriteString(fmt.Sprintf("On %s, %s wrote:\n", formattedTime, senderAddr))
	}

	plainBody := stripANSICodes(formatBodyContent(bodyText))
	lines := strings.Split(plainBody, "\n")
	for _, line := range lines {
		quotedBody.WriteString("> " + line + "\n")
	}

	m.composeBody.SetValue(quotedBody.String())
	for m.composeBody.Line() > 0 || m.composeBody.LineInfo().RowOffset > 0 {
		m.composeBody.CursorUp()
	}
	m.composeBody.CursorStart()
	m.updateComposeFocus()
}

func (m *mainModel) updateComposeFocus() {
	m.composeTo.Blur()
	m.composeCc.Blur()
	m.composeSubject.Blur()
	m.composeBody.Blur()
	switch m.composeStep {
	case 0:
		m.composeTo.Focus()
	case 1:
		m.composeCc.Focus()
	case 2:
		m.composeSubject.Focus()
	case 3:
		m.composeBody.Focus()
	}
}

func (m *mainModel) updateComposeBodyHeight() {
	// Base height deduction:
	// Title/Header (5) + To/Cc/Subject fields (9) + Body label/ending (3) + Footer (1) = 18 lines
	deduction := 18

	// Add deduction for To dropdown if open
	if m.config.UseSQLite != 0 && m.composeStep == 0 && len(m.filteredContacts) > 0 {
		popupLines := len(m.filteredContacts)
		if popupLines > 5 {
			popupLines = 5 + 1 // 5 contacts + 1 "more" line
		}
		deduction += popupLines + 2
	}

	// Add deduction for Cc dropdown if open
	if m.config.UseSQLite != 0 && m.composeStep == 1 && len(m.filteredContacts) > 0 {
		popupLines := len(m.filteredContacts)
		if popupLines > 5 {
			popupLines = 5 + 1 // 5 contacts + 1 "more" line
		}
		deduction += popupLines + 2
	}

	// Pasted images adds 2 lines
	if len(m.composedImages) > 0 {
		deduction += 2
	}

	// Composed files adds len(files) + 2 lines
	if len(m.composedFiles) > 0 {
		deduction += len(m.composedFiles) + 2
	}

	h := m.height - deduction
	if h < 3 {
		h = 3
	}
	m.composeBody.SetHeight(h)
}

func (m *mainModel) loadContacts() {
	m.contacts = nil
	m.filteredContacts = nil
	m.contactsSelected = 0
	m.contactsStartIdx = 0
	if m.config.UseSQLite != 0 && m.db != nil {
		if contacts, err := m.db.GetContacts(); err == nil {
			m.contacts = contacts
		}
	}
}

func (m *mainModel) updateFilteredContacts() {
	if m.config.UseSQLite == 0 || len(m.contacts) == 0 {
		m.filteredContacts = nil
		m.contactsSelected = 0
		m.contactsStartIdx = 0
		return
	}

	var val string
	if m.composeStep == 0 {
		val = m.composeTo.Value()
	} else if m.composeStep == 1 {
		val = m.composeCc.Value()
	} else {
		m.filteredContacts = nil
		m.contactsSelected = 0
		m.contactsStartIdx = 0
		return
	}

	parts := strings.Split(val, ",")
	if len(parts) == 0 {
		m.filteredContacts = nil
		m.contactsSelected = 0
		m.contactsStartIdx = 0
		return
	}
	query := strings.ToLower(strings.TrimSpace(parts[len(parts)-1]))

	// Don't show dropdown for empty query
	if query == "" {
		m.filteredContacts = nil
		m.contactsSelected = 0
		m.contactsStartIdx = 0
		return
	}

	var filtered []Contact
	for _, c := range m.contacts {
		if strings.Contains(strings.ToLower(c.Name), query) || strings.Contains(strings.ToLower(c.Address), query) {
			filtered = append(filtered, c)
		}
	}
	if len(m.filteredContacts) != len(filtered) {
		m.contactsStartIdx = 0
	}
	m.filteredContacts = filtered

	// Clamp selected index
	if m.contactsSelected >= len(m.filteredContacts) {
		m.contactsSelected = 0
		m.contactsStartIdx = 0
	}
	if m.contactsSelected < 0 {
		m.contactsSelected = 0
		m.contactsStartIdx = 0
	}
}

func (m mainModel) updateViewportSize() mainModel {
	m.helpViewport.Width = m.width - 6
	if m.helpViewport.Width < 20 {
		m.helpViewport.Width = 20
	}
	helpHeight := m.height - 12
	if helpHeight < 3 {
		helpHeight = 3
	}
	m.helpViewport.Height = helpHeight

	if m.config.Layout == 2 {
		return m.updateViewportSizeLayout2()
	}
	return m.updateViewportSizeLayout1()
}

func (m mainModel) updateViewportSizeLayout1() mainModel {
	// Empirically measured Lipgloss v1.1.0 semantics:
	//   Width(n) with Padding(0,1)+Border → outer = n+2  (Width includes padding; border adds 2)
	//   Height(n) with Border             → outer = n+2  (Height is inner content)
	//   fView outer=25, mView outer=35 → dView outer must = m.width-60 → Width = m.width-62
	//   Content area inside padding = Width - 2 = m.width-64 (viewport width)
	// View() line budget: 1 (title) + 2 (\n\n) + paneHeight+2 (borders) + 1 (trailing \n) + 1 (\n before footer) + 2 (footer status + keys) = paneHeight+9
	// So paneHeight = m.height - 9.
	paneHeight := m.height - 9 // inner content; outer = paneHeight+2 (border top+bottom)
	if paneHeight < 5 {
		paneHeight = 5
	}

	detailWidth := m.width - 64 // viewport content area = pane Width(m.width-62) - 2 padding
	if detailWidth < 20 {
		detailWidth = 20
	}

	metaHeight := 4 // fallback
	if m.detailMessage != nil {
		metaBlock := m.renderMetaBlock(detailWidth)
		metaLines := strings.Split(metaBlock, "\n")
		metaHeight = len(metaLines) - 1
	}

	viewportHeight := paneHeight - metaHeight
	if viewportHeight < 3 {
		viewportHeight = 3
	}

	m.detailViewport.Width = detailWidth
	m.detailViewport.Height = viewportHeight
	return m
}

func (m mainModel) updateViewportSizeLayout2() mainModel {
	// Layout 2: left column = Folders (top ~30%) stacked above Messages (~70%).
	//           Right column = Detail pane (full height).
	//
	// Left column: leftColInner=46 → leftColOuter=50 (inner + 2 padding + 2 border)
	// Right detail pane content width = m.width - leftColOuter - 4 (its own pad+border) = m.width - 54
	// Same budget as Layout1: paneHeight+9 total lines → totalHeight = m.height - 9.
	totalHeight := m.height - 9
	if totalHeight < 10 {
		totalHeight = 10
	}

	detailWidth := m.width - 54
	if detailWidth < 20 {
		detailWidth = 20
	}

	metaHeight := 4
	if m.detailMessage != nil {
		metaBlock := m.renderMetaBlock(detailWidth)
		metaLines := strings.Split(metaBlock, "\n")
		metaHeight = len(metaLines) - 1
	}

	viewportHeight := totalHeight - 2 - metaHeight
	if viewportHeight < 3 {
		viewportHeight = 3
	}

	m.detailViewport.Width = detailWidth
	m.detailViewport.Height = viewportHeight
	return m
}

// Rendering Views
// Colors (Catppuccin Mocha palette adapted from yt-tui)
var (
	ColorBg      = "#1E1E2E"
	ColorText    = "#CDD6F4"
	ColorSubtext = "#A6ADC8"
	ColorViolet  = "#CBA6F7"             // Primary accent
	ColorCyan    = "#89B4FA"             // Secondary accent
	ColorGreen   = "#A6E3A1"             // Success
	ColorYellow  = "#F9E2AF"             // Warning
	ColorRed     = "#F38BA8"             // Error
	ColorSurface = "#313244"             // Panel background
	ColorOverlay = "#45475A"             // Highlight border
	BgSeq        = "\x1b[48;2;49;50;68m" // truecolor escape for ColorSurface background
)

func applyTheme(themeName string) {
	switch strings.ToLower(themeName) {
	case "teams":
		ColorBg = "#1E1E1E"
		ColorText = "#FFFFFF"
		ColorSubtext = "#888888"
		ColorViolet = "#00D75F"
		ColorCyan = "#00D7D7"
		ColorGreen = "#00D75F"
		ColorYellow = "#FFD700"
		ColorRed = "#FF4444"
		ColorSurface = "#202020"
		ColorOverlay = "#303030"
	default:
		// Catppuccin Mocha (default)
		ColorBg = "#1E1E2E"
		ColorText = "#CDD6F4"
		ColorSubtext = "#A6ADC8"
		ColorViolet = "#CBA6F7"
		ColorCyan = "#89B4FA"
		ColorGreen = "#A6E3A1"
		ColorYellow = "#F9E2AF"
		ColorRed = "#F38BA8"
		ColorSurface = "#313244"
		ColorOverlay = "#45475A"
	}

	// Re-initialize global style variables
	titleStyle = lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color(ColorBg)).
		Background(lipgloss.Color(ColorViolet)).
		Padding(0, 2).
		Height(1)

	headerStyle = lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color(ColorViolet)).
		PaddingLeft(1)

	paneNormalStyle = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(ColorOverlay)).
		Padding(0, 1)

	paneActiveStyle = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(ColorViolet)).
		Padding(0, 1)

	selectedItemStyle = lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color(ColorBg)).
		Background(lipgloss.Color(ColorCyan))

	unreadStyle = lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color(ColorViolet))

	dimStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color(ColorSubtext))

	imagePlaceholderStyle = lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color(ColorViolet))

	statusStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color(ColorText)).
		Background(lipgloss.Color(ColorSurface)).
		Padding(0, 1)

	// Update BgSeq
	var r, g, b int
	if len(ColorSurface) == 7 && ColorSurface[0] == '#' {
		_, _ = fmt.Sscanf(ColorSurface, "#%02x%02x%02x", &r, &g, &b)
	}
	BgSeq = fmt.Sprintf("\x1b[48;2;%d;%d;%dm", r, g, b)
}

var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color(ColorBg)).
			Background(lipgloss.Color(ColorViolet)).
			Padding(0, 2).
			Height(1)

	headerStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color(ColorViolet)).
			PaddingLeft(1)

	paneNormalStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color(ColorOverlay)).
			Padding(0, 1)

	paneActiveStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color(ColorViolet)).
			Padding(0, 1)

	selectedItemStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color(ColorBg)).
				Background(lipgloss.Color(ColorCyan))

	unreadStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color(ColorViolet))

	dimStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color(ColorSubtext))

	imagePlaceholderStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color(ColorViolet))

	statusStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color(ColorText)).
			Background(lipgloss.Color(ColorSurface)).
			Padding(0, 1)
)

func (m mainModel) View() string {
	var s strings.Builder

	if m.state == stateAttachments {
		// Clear any previous Kitty image previews from the screen
		s.WriteString("\033_Ga=d,d=A\033\\")
	}

	// Top Title Bar
	s.WriteString(titleStyle.Render(fmt.Sprintf("OUTLOOK TUI v1.0 | %d x %d", m.width, m.height)))
	s.WriteString("\n\n")

	switch m.state {
	case stateReplyConfirm:
		s.WriteString("   " + headerStyle.Render("REPLY OPTIONS") + "\n\n")
		s.WriteString("   Do you want to reply to the sender only or reply all?\n\n")
		s.WriteString("   " + lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(ColorCyan)).Render("[s]") + " Reply to Sender Only\n")
		s.WriteString("   " + lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(ColorViolet)).Render("[a]") + " Reply All\n\n")
		s.WriteString("   " + lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(ColorOverlay)).Render("[c]") + " Cancel / Go Back\n")

	case stateConfig:
		s.WriteString("   " + headerStyle.Render("OUTLOOK CONFIGURATION") + "\n\n")
		s.WriteString("   To build this app, we register a client in Microsoft Azure Entra.\n")
		s.WriteString("   Make sure the app allows Public Client Flows (Device Code flow enabled).\n\n")
		s.WriteString("   " + m.txtInput.View() + "\n\n")
		s.WriteString("   [Enter] Next / Save  |  [Ctrl+C] Quit\n")

	case stateDeviceAuth:
		s.WriteString("   " + headerStyle.Render("MICROSOFT GRAPH AUTHENTICATION") + "\n\n")
		if m.deviceCode != nil {
			s.WriteString("   1. Open the following URL in your browser:\n")
			s.WriteString("      " + lipgloss.NewStyle().Foreground(lipgloss.Color(ColorCyan)).Underline(true).Render(m.deviceCode.VerificationURI) + "\n\n")
			s.WriteString("   2. Enter the following activation code:\n")
			s.WriteString("      " + lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(ColorYellow)).Render(m.deviceCode.UserCode) + "\n\n")
			s.WriteString("   " + m.spinner.View() + " " + m.statusMsg + "\n\n")
		} else {
			s.WriteString("   " + m.spinner.View() + " Preparing device authentication...\n\n")
		}
		s.WriteString("   [Esc] Go Back to Config  |  [Ctrl+C] Quit\n")

	case stateLoading:
		s.WriteString("\n\n   " + m.spinner.View() + " " + m.statusMsg + "\n")

	case stateMain, stateYankSelect, stateURLSelect, stateExternalURLSelect, stateDeleteThreadConfirm, stateAttachments:
		if m.config.Layout == 2 {
			s.WriteString(m.renderLayout2())
		} else {
			s.WriteString(m.renderLayout1())
		}

	case stateCompose, stateComposeCancelConfirm:
		s.WriteString("   " + headerStyle.Render("COMPOSE NEW EMAIL") + "\n\n")

		toBorder := lipgloss.NewStyle().Foreground(lipgloss.Color(ColorOverlay))
		ccBorder := lipgloss.NewStyle().Foreground(lipgloss.Color(ColorOverlay))
		subjBorder := lipgloss.NewStyle().Foreground(lipgloss.Color(ColorOverlay))
		bodyBorder := lipgloss.NewStyle().Foreground(lipgloss.Color(ColorOverlay))

		switch m.composeStep {
		case 0:
			toBorder = lipgloss.NewStyle().Foreground(lipgloss.Color(ColorViolet))
		case 1:
			ccBorder = lipgloss.NewStyle().Foreground(lipgloss.Color(ColorViolet))
		case 2:
			subjBorder = lipgloss.NewStyle().Foreground(lipgloss.Color(ColorViolet))
		case 3:
			bodyBorder = lipgloss.NewStyle().Foreground(lipgloss.Color(ColorViolet))
		}

		s.WriteString("   To:\n   " + toBorder.Render(m.composeTo.View()) + "\n")
		if m.config.UseSQLite != 0 && m.composeStep == 0 && len(m.filteredContacts) > 0 {
			popupContent := m.renderContactsPopup()
			lines := strings.Split(popupContent, "\n")
			for _, line := range lines {
				if strings.TrimSpace(line) != "" {
					s.WriteString("   " + line + "\n")
				}
			}
			s.WriteString("\n")
		} else {
			s.WriteString("\n")
		}

		s.WriteString("   Cc:\n   " + ccBorder.Render(m.composeCc.View()) + "\n")
		if m.config.UseSQLite != 0 && m.composeStep == 1 && len(m.filteredContacts) > 0 {
			popupContent := m.renderContactsPopup()
			lines := strings.Split(popupContent, "\n")
			for _, line := range lines {
				if strings.TrimSpace(line) != "" {
					s.WriteString("   " + line + "\n")
				}
			}
			s.WriteString("\n")
		} else {
			s.WriteString("\n")
		}

		s.WriteString("   Subject:\n   " + subjBorder.Render(m.composeSubject.View()) + "\n\n")
		{
			renderedBody := bodyBorder.Render(m.composeBody.View())
			bodyLines := strings.Split(renderedBody, "\n")
			s.WriteString("   Body:\n")
			for _, bl := range bodyLines {
				s.WriteString("   " + bl + "\n")
			}
			s.WriteString("\n")
		}
		if len(m.composedImages) > 0 {
			s.WriteString(fmt.Sprintf("   Pasted images: %d\n\n", len(m.composedImages)))
		}
		if len(m.composedFiles) > 0 {
			s.WriteString(fmt.Sprintf("   Attachments: %d\n", len(m.composedFiles)))
			for _, file := range m.composedFiles {
				sizeStr := strings.Replace(humanize.Bytes(uint64(len(file.Data))), " ", "", 1)
				s.WriteString(fmt.Sprintf("     - %s (%s)\n", file.Name, sizeStr))
			}
			s.WriteString("\n")
		}

		s.WriteString("   [Tab] Switch Fields  |  [Ctrl+v] Paste Image  |  [Ctrl+f] Add Attachment  |  [Ctrl+g] Edit Body in $EDITOR  |  [Ctrl+s/x] Send  |  [Esc] Cancel\n")

	case stateYouTrackInstallPrompt:
		s.WriteString("   " + headerStyle.Render("YOUTRACK TUI NOT INSTALLED") + "\n\n")
		s.WriteString("   The yt-tui binary could not be found in your system's PATH.\n")
		s.WriteString("   To use this integration, please install yt-tui first:\n\n")
		s.WriteString("   " + lipgloss.NewStyle().Foreground(lipgloss.Color(ColorCyan)).Underline(true).Render("https://github.com/nospor/yt-tui") + "\n\n")
		s.WriteString("   [Esc/q/Enter] Back to Main View\n")

	case stateGitLabInstallPrompt:
		s.WriteString("   " + headerStyle.Render("GITLAB TUI NOT INSTALLED") + "\n\n")
		s.WriteString("   The gitlab-tui binary could not be found in your system's PATH.\n")
		s.WriteString("   To use this integration, please install gitlab-tui first:\n\n")
		s.WriteString("   " + lipgloss.NewStyle().Foreground(lipgloss.Color(ColorCyan)).Underline(true).Render("https://github.com/nospor/gitlab-tui") + "\n\n")
		s.WriteString("   [Esc/q/Enter] Back to Main View\n")

	case stateHelp:
		s.WriteString("   " + headerStyle.Render("OUTLOOK TUI HELP & KEYBINDINGS") + "\n\n")
		s.WriteString(paneActiveStyle.Width(m.width-4).Height(m.helpViewport.Height).Render(m.helpViewport.View()) + "\n\n")
		s.WriteString("   " + dimStyle.Render("[Esc/q/?] Close Help  |  [Up/Down/j/k] Scroll  |  [Ctrl+C] Quit") + "\n")

	case stateFileBrowse:
		s.WriteString(m.renderFilePickerPopup(m.width-4, m.height-10))
	}

	// Bottom Status/Keybinds Bar
	if m.state == stateMain || m.state == stateYankSelect || m.state == stateURLSelect || m.state == stateExternalURLSelect || m.state == stateDeleteThreadConfirm || m.state == stateAttachments {
		s.WriteString("\n")
		statusText := fmt.Sprintf("Status: %s", m.statusMsg)
		s.WriteString(statusStyle.Width(m.width).Render(statusText) + "\n")

		var keysText string
		if m.state == stateYankSelect {
			keysText = "  [Esc/q] Cancel | [Up/Down/j/k] Select | [Enter] Confirm | [m] original | [a] all | [u] URLs | [s] subject"
		} else if m.state == stateURLSelect {
			keysText = "  [Esc/q] Cancel | [Up/Down/j/k] Select URL | [Enter] Copy to Clipboard"
		} else if m.state == stateExternalURLSelect {
			keysText = "  [Esc/q] Cancel | [Up/Down/j/k] Select URL | [Enter] Open in TUI"
		} else if m.state == stateDeleteThreadConfirm {
			keysText = "  [y] Yes, delete thread | [n/Esc] No, cancel"
		} else if m.state == stateAttachments {
			keysText = "  [Up/Down/j/k] Select Attachment | [Enter] Save / Open | [Esc] Back"
		} else {
			if m.width >= 160 {
				keysText = "  [Tab] Switch Pane | [Space] Thread | [n] Compose | [A] Reply | [d] Delete | [U] Undelete | [r] Reload | [M] More | [R] Read | [f] Favorite | [a] Attach | [y] Yank | [o] Open TUI | [?] Help | [q] Quit"
			} else if m.width >= 130 {
				keysText = "  [Tab] Pane | [Space] Thread | [n] Compose | [A] Reply | [d] Delete | [U] Undelete | [r] Reload | [M] More | [R] Read | [f] Fav | [y] Yank | [o] Open TUI | [?] Help | [q] Quit"
			} else if m.width >= 95 {
				keysText = "  [Tab] Pane | [Space] Thread | [n] Compose | [d] Delete | [f] Fav | [r] Reload | [M] More | [?] Help | [q] Quit"
			} else {
				keysText = "  [Tab] Pane | [Space] Thread | [d] Del | [f] Fav | [?] Help | [q] Quit"
			}
		}
		s.WriteString(dimStyle.Render(keysText))
	} else if m.state != stateDeviceAuth && m.state != stateLoading {
		if m.statusMsg != "" {
			s.WriteString("\n" + statusStyle.Width(m.width).Render(m.statusMsg))
		}
	}

	baseView := s.String()
	if m.state == stateYankSelect {
		modalWidth := 46
		modalHeight := 7
		dropdownView := m.renderYankDropdown(modalWidth)

		x := (m.width - modalWidth) / 2
		y := (m.height - modalHeight) / 2
		if x < 0 {
			x = 0
		}
		if y < 0 {
			y = 0
		}
		baseView = overlayLines(baseView, dropdownView, x, y)
	} else if m.state == stateDeleteThreadConfirm {
		modalWidth := 60
		if modalWidth > m.width-6 {
			modalWidth = m.width - 6
		}
		if modalWidth < 30 {
			modalWidth = 30
		}
		modalHeight := 11
		dropdownView := m.renderDeleteThreadConfirmPopup(modalWidth)

		x := (m.width - modalWidth) / 2
		y := (m.height - modalHeight) / 2
		if x < 0 {
			x = 0
		}
		if y < 0 {
			y = 0
		}
		baseView = overlayLines(baseView, dropdownView, x, y)
	} else if m.state == stateAttachments {
		// Calculate modal size based on longest attachment name
		longestAttach := 0
		for _, att := range m.attachments {
			sizeKB := att.Size / 1024
			inlineStr := ""
			if att.IsInline {
				inlineStr = " [inline]"
			}
			lineLen := len(fmt.Sprintf("   %s (%d KB) [%s]%s", att.Name, sizeKB, att.ContentType, inlineStr))
			if lineLen > longestAttach {
				longestAttach = lineLen
			}
		}
		modalWidth := longestAttach + 8
		if modalWidth < 46 {
			modalWidth = 46
		}
		if modalWidth > m.width-6 {
			modalWidth = m.width - 6
		}
		if modalWidth < 20 {
			modalWidth = 20
		}

		maxVisible := 10
		totalAttach := len(m.attachments)
		visibleCount := totalAttach
		if visibleCount > maxVisible {
			visibleCount = maxVisible
		}

		startIdx := 0
		if totalAttach > maxVisible {
			startIdx = m.selectedAttach - maxVisible/2
			if startIdx < 0 {
				startIdx = 0
			}
			endIdx := startIdx + maxVisible
			if endIdx > totalAttach {
				startIdx = totalAttach - maxVisible
			}
		}

		contentLines := 2 + visibleCount // Header + empty spacer + visible items
		if startIdx > 0 {
			contentLines++
		}
		if startIdx+visibleCount < totalAttach {
			contentLines++
		}
		modalHeight := contentLines + 2 // borders

		dropdownView := m.renderAttachmentsDropdown(modalWidth)

		x := (m.width - modalWidth) / 2
		y := (m.height - modalHeight) / 2
		if x < 0 {
			x = 0
		}
		if y < 0 {
			y = 0
		}
		baseView = overlayLines(baseView, dropdownView, x, y)
	} else if m.state == stateURLSelect {
		// Calculate modal size
		longestURL := 0
		for _, url := range m.extractedURLs {
			if len(url) > longestURL {
				longestURL = len(url)
			}
		}
		modalWidth := longestURL + 8
		if modalWidth < 46 {
			modalWidth = 46
		}
		if modalWidth > m.width-6 {
			modalWidth = m.width - 6
		}
		if modalWidth < 20 {
			modalWidth = 20
		}

		maxVisible := 10
		totalURLs := len(m.extractedURLs)
		visibleCount := totalURLs
		if visibleCount > maxVisible {
			visibleCount = maxVisible
		}

		startIdx := 0
		if totalURLs > maxVisible {
			startIdx = m.selectedURLIdx - maxVisible/2
			if startIdx < 0 {
				startIdx = 0
			}
			endIdx := startIdx + maxVisible
			if endIdx > totalURLs {
				startIdx = totalURLs - maxVisible
			}
		}

		contentLines := 1 + visibleCount
		if startIdx > 0 {
			contentLines++
		}
		if startIdx+visibleCount < totalURLs {
			contentLines++
		}
		modalHeight := contentLines + 2 // borders

		dropdownView := m.renderURLDropdown(modalWidth)

		x := (m.width - modalWidth) / 2
		y := (m.height - modalHeight) / 2
		if x < 0 {
			x = 0
		}
		if y < 0 {
			y = 0
		}
		baseView = overlayLines(baseView, dropdownView, x, y)
	} else if m.state == stateExternalURLSelect {
		// Calculate modal size
		longestURL := 0
		for _, urlStr := range m.extractedURLs {
			prefix := "[YouTrack] "
			if strings.Contains(urlStr, "/merge_requests/") || strings.Contains(urlStr, "/pipelines/") || strings.Contains(urlStr, "/jobs/") {
				prefix = "[GitLab]   "
			}
			lineLen := len(prefix) + len(urlStr)
			if lineLen > longestURL {
				longestURL = lineLen
			}
		}
		modalWidth := longestURL + 8
		if modalWidth < 46 {
			modalWidth = 46
		}
		if modalWidth > m.width-6 {
			modalWidth = m.width - 6
		}
		if modalWidth < 20 {
			modalWidth = 20
		}

		maxVisible := 10
		totalURLs := len(m.extractedURLs)
		visibleCount := totalURLs
		if visibleCount > maxVisible {
			visibleCount = maxVisible
		}

		startIdx := 0
		if totalURLs > maxVisible {
			startIdx = m.selectedURLIdx - maxVisible/2
			if startIdx < 0 {
				startIdx = 0
			}
			endIdx := startIdx + maxVisible
			if endIdx > totalURLs {
				startIdx = totalURLs - maxVisible
			}
		}

		contentLines := 1 + visibleCount
		if startIdx > 0 {
			contentLines++
		}
		if startIdx+visibleCount < totalURLs {
			contentLines++
		}
		modalHeight := contentLines + 2 // borders

		dropdownView := m.renderExternalURLDropdown(modalWidth)

		x := (m.width - modalWidth) / 2
		y := (m.height - modalHeight) / 2
		if x < 0 {
			x = 0
		}
		if y < 0 {
			y = 0
		}
		baseView = overlayLines(baseView, dropdownView, x, y)
	} else if m.state == stateComposeCancelConfirm {
		modalWidth := 60
		if modalWidth > m.width-6 {
			modalWidth = m.width - 6
		}
		if modalWidth < 30 {
			modalWidth = 30
		}
		modalHeight := 9
		dropdownView := m.renderComposeCancelConfirmPopup(modalWidth)

		x := (m.width - modalWidth) / 2
		y := (m.height - modalHeight) / 2
		if x < 0 {
			x = 0
		}
		if y < 0 {
			y = 0
		}
		baseView = overlayLines(baseView, dropdownView, x, y)
	}

	// Guarantee exactly m.height - 1 output lines so BubbleTea's cursor tracking
	// is never off and doesn't scroll the terminal. Clip if too tall, pad with blank lines if too short.
	if m.height > 0 && (m.state == stateMain || m.state == stateHelp || m.state == stateFileBrowse || m.state == stateYankSelect || m.state == stateURLSelect || m.state == stateExternalURLSelect || m.state == stateDeleteThreadConfirm || m.state == stateAttachments) {
		lines := strings.Split(baseView, "\n")
		targetHeight := m.height - 1
		for len(lines) < targetHeight {
			lines = append(lines, "")
		}
		if len(lines) > targetHeight {
			lines = lines[:targetHeight]
		}
		return strings.Join(lines, "\n")
	}
	return baseView
}

func (m mainModel) renderContactsPopup() string {
	if len(m.filteredContacts) == 0 {
		return ""
	}

	var rows []string

	// Display up to 5 matching contacts
	maxContacts := 5
	if len(m.filteredContacts) < maxContacts {
		maxContacts = len(m.filteredContacts)
	}

	for i := 0; i < maxContacts; i++ {
		contact := m.filteredContacts[i]

		var line string
		if contact.Name != "" {
			line = fmt.Sprintf(" %s <%s>", contact.Name, contact.Address)
		} else {
			line = fmt.Sprintf(" %s", contact.Address)
		}

		// pad the line to align with popup width
		width := m.width - 26
		if width < 40 {
			width = 40
		}
		if len(line) < width {
			line = line + strings.Repeat(" ", width-len(line))
		} else if len(line) > width {
			line = line[:width-3] + "..."
		}

		if i == m.contactsSelected {
			line = lipgloss.NewStyle().
				Foreground(lipgloss.Color(ColorBg)).
				Background(lipgloss.Color(ColorCyan)).
				Render(line)
		} else {
			line = lipgloss.NewStyle().
				Foreground(lipgloss.Color(ColorText)).
				Render(line)
		}
		rows = append(rows, line)
	}

	if len(m.filteredContacts) > maxContacts {
		moreText := fmt.Sprintf(" ... and %d more ...", len(m.filteredContacts)-maxContacts)
		width := m.width - 26
		if width < 40 {
			width = 40
		}
		if len(moreText) < width {
			moreText = moreText + strings.Repeat(" ", width-len(moreText))
		}
		rows = append(rows, lipgloss.NewStyle().Foreground(lipgloss.Color(ColorSubtext)).Render(moreText))
	}

	joined := strings.Join(rows, "\n")

	popupStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(ColorCyan)).
		Padding(0, 1)

	return popupStyle.Render(joined)
}

func (m mainModel) renderFilePickerPopup(w, h int) string {
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ColorSubtext))
	title := lipgloss.NewStyle().Foreground(lipgloss.Color(ColorCyan)).Bold(true).Render("Select File to Attach")

	currentDir := lipgloss.NewStyle().Foreground(lipgloss.Color(ColorText)).Bold(true).Render("Directory: " + m.filepicker.CurrentDirectory)
	sortMode := lipgloss.NewStyle().Foreground(lipgloss.Color(ColorYellow)).Render(fmt.Sprintf("Sorted by: %s (%s)", m.filepicker.SortBy.String(), m.filepicker.SortOrder.String()))

	var lines []string
	lines = append(lines, title, currentDir, sortMode, "")

	// Render the filepicker component
	lines = append(lines, m.filepicker.View())

	footer := dimStyle.Italic(true).Render("j/k or ↑/↓: Navigate • s: Change Sort • o: Change Order • Enter: Attach • Esc / q: Cancel")
	lines = append(lines, "", footer)

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(ColorGreen)).
		Padding(1, 2).
		Width(w).Height(h).
		Render(strings.Join(lines, "\n"))
}

// renderLayout1 renders the default side-by-side three-pane layout:
//
//	[Folders | Messages | Detail]
func (m mainModel) renderLayout1() string {
	paneHeight := m.height - 9
	if paneHeight < 5 {
		paneHeight = 5
	}

	foldersView := m.renderFoldersView(paneHeight)
	messagesView := m.renderMessagesView(paneHeight)
	detailView := m.renderDetailView()

	var fStyle, mStyle, dStyle lipgloss.Style
	if m.activePane == paneFolders {
		fStyle = paneActiveStyle
	} else {
		fStyle = paneNormalStyle
	}
	if m.activePane == paneMessages {
		mStyle = paneActiveStyle
	} else {
		mStyle = paneNormalStyle
	}
	if m.activePane == paneDetail {
		dStyle = paneActiveStyle
	} else {
		dStyle = paneNormalStyle
	}

	fView := fStyle.Width(23).Height(paneHeight).Render(cropLines(foldersView, paneHeight))
	mView := mStyle.Width(33).Height(paneHeight).Render(cropLines(messagesView, paneHeight))
	// Width(23) outer=25, Width(33) outer=35; dView outer = m.width-60 → Width = m.width-62
	dView := dStyle.Width(m.width - 62).Height(paneHeight).Render(cropLines(detailView, paneHeight))

	fView = applyPaneTitle(fView, "FOLDERS", m.activePane == paneFolders)
	mView = applyPaneTitle(mView, "MESSAGES", m.activePane == paneMessages)
	dView = applyPaneTitle(dView, "MESSAGE DETAIL", m.activePane == paneDetail)

	fView = cropLines(fView, paneHeight+2)
	mView = cropLines(mView, paneHeight+2)
	dView = cropLines(dView, paneHeight+2)

	return lipgloss.JoinHorizontal(lipgloss.Top, fView, mView, dView) + "\n"
}

// renderLayout2 renders the alternative stacked layout:
//
//	Left column: [Folders (~30%)] stacked above [Messages (~70%)]
//	Right column: [Detail] (full height)
//
// Left column inner width = 46 → outer = 50 (2 padding + 2 border on each side).
// Right column inner width = m.width - 54.
func (m mainModel) renderLayout2() string {
	totalHeight := m.height - 9
	if totalHeight < 10 {
		totalHeight = 10
	}

	// Heights for the two left panes (inner content, border not counted here)
	foldersHeight := totalHeight * 30 / 100
	if foldersHeight < 4 {
		foldersHeight = 4
	}
	messagesHeight := totalHeight - foldersHeight - 4 // -4 for the two border pairs
	if messagesHeight < 4 {
		messagesHeight = 4
	}

	leftColInner := 46 // inner content width of each left pane

	foldersView := m.renderFoldersViewWide(foldersHeight, leftColInner)
	messagesView := m.renderMessagesViewWide(messagesHeight, leftColInner)
	detailView := m.renderDetailView()

	var fStyle, mStyle, dStyle lipgloss.Style
	if m.activePane == paneFolders {
		fStyle = paneActiveStyle
	} else {
		fStyle = paneNormalStyle
	}
	if m.activePane == paneMessages {
		mStyle = paneActiveStyle
	} else {
		mStyle = paneNormalStyle
	}
	if m.activePane == paneDetail {
		dStyle = paneActiveStyle
	} else {
		dStyle = paneNormalStyle
	}

	// Stack folders above messages in the left column
	fView := fStyle.Width(leftColInner).Height(foldersHeight).Render(cropLines(foldersView, foldersHeight))
	mView := mStyle.Width(leftColInner).Height(messagesHeight).Render(cropLines(messagesView, messagesHeight))

	fView = applyPaneTitle(fView, "FOLDERS", m.activePane == paneFolders)
	mView = applyPaneTitle(mView, "MESSAGES", m.activePane == paneMessages)

	fView = cropLines(fView, foldersHeight+2)
	mView = cropLines(mView, messagesHeight+2)

	leftCol := lipgloss.JoinVertical(lipgloss.Left, fView, mView)
	leftCol = cropLines(leftCol, totalHeight)

	// Right detail pane spans the full height; outer = totalHeight + 2 (borders)
	// left col outer = leftColInner+4=50; right pane Width = m.width - 50 - 4 = m.width - 54
	// dView outer height must match left column outer height (= totalHeight).
	// .Height(n) sets inner content; outer = n+2 (borders). So use totalHeight-2.
	dView := dStyle.Width(m.width - 54).Height(totalHeight - 2).Render(cropLines(detailView, totalHeight-2))
	dView = applyPaneTitle(dView, "MESSAGE DETAIL", m.activePane == paneDetail)
	dView = cropLines(dView, totalHeight)

	return lipgloss.JoinHorizontal(lipgloss.Top, leftCol, dView) + "\n"
}

func (m mainModel) renderHelpContent() string {
	var s strings.Builder

	// Description
	desc := "Outlook TUI is a fast, terminal-based client for Microsoft Outlook 365.\n" +
		"Manage folders, read messages, handle attachments, copy URLs, and compose emails."
	s.WriteString(dimStyle.Render(desc) + "\n\n")

	// Left column: Navigation & Views
	col1Title := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(ColorCyan)).Render("NAVIGATION & VIEWS")
	col1Lines := []string{
		col1Title,
		"",
		"  [Tab] / [Shift+Tab] Switch pane focus",
		"  [Left] / [Right]    Switch pane focus",
		"  [Up] / [Down]       Select items / scroll message",
		"  [j] / [k]           Select items / scroll message",
		"  [J] / [K]           Navigate next/prev pane / scroll",
		"  [PageUp]/[PageDown] Scroll detail view half page",
		"  [Space]             Toggle collapse/expand thread",
		"  [r]                 Reload current folder",
		"  [M]                 Load more messages (paginate)",
	}

	// Right column: Actions & Compose
	col2Title := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(ColorCyan)).Render("MAIL ACTIONS & COMPOSE")
	col2Lines := []string{
		col2Title,
		"",
		"  [n]                 Compose new email",
		"  [A]                 Reply or Reply All to message",
		"  [R]                 Toggle Read / Unread status",
		"  [f]                 Toggle Favorite status (local)",
		"  [d] / [Delete]      Move message to Deleted Items",
		"  [D]                 Move entire thread to Deleted Items",
		"  [U]                 Undelete / Restore to Inbox",
		"  [a]                 Open attachments pane (if any)",
		"  [y]                 Yank/Copy options (msg, subject, URLs)",
		"  [o]                 Open YouTrack issue or GitLab MR in TUI",
		"  [Ctrl+g]            View message in external editor",
	}

	// Compose Shortcuts Section
	composeTitle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(ColorCyan)).Render("COMPOSE MODE KEYBOARD SHORTCUTS")
	composeLines := []string{
		composeTitle,
		"",
		"  [Tab] / [Shift+Tab] Navigate compose fields (To, Cc, Subject, Body)",
		"  [Ctrl+v]            Paste image from clipboard",
		"  [Ctrl+g]            Open external editor ($EDITOR / $VISUAL / vi)",
		"  [Ctrl+s] / [Ctrl+x] Send email",
		"  [Up] / [Down]       Navigate autocomplete suggestions",
		"  [Enter]             Select contact suggestion",
		"  [Esc]               Cancel composing / Close suggestions",
	}

	// Build two column section dynamically based on width
	if m.helpViewport.Width >= 130 {
		maxLen := len(col1Lines)
		if len(col2Lines) > maxLen {
			maxLen = len(col2Lines)
		}
		for len(col1Lines) < maxLen {
			col1Lines = append(col1Lines, "")
		}
		for len(col2Lines) < maxLen {
			col2Lines = append(col2Lines, "")
		}

		var col1Text, col2Text strings.Builder
		for i := 0; i < maxLen; i++ {
			col1Text.WriteString(col1Lines[i] + "\n")
			col2Text.WriteString(col2Lines[i] + "\n")
		}

		col1Style := lipgloss.NewStyle().Width(65)
		col2Style := lipgloss.NewStyle().Width(65)

		columns := lipgloss.JoinHorizontal(
			lipgloss.Top,
			col1Style.Render(col1Text.String()),
			col2Style.Render(col2Text.String()),
		)
		s.WriteString(columns + "\n")
	} else {
		for _, l := range col1Lines {
			s.WriteString(l + "\n")
		}
		s.WriteString("\n")
		for _, l := range col2Lines {
			s.WriteString(l + "\n")
		}
		s.WriteString("\n")
	}

	// Compose section
	for _, l := range composeLines {
		s.WriteString(l + "\n")
	}

	return s.String()
}

// renderFoldersViewWide is a variant of renderFoldersView that uses a wider display name limit.
func (m mainModel) renderFoldersViewWide(availHeight, availWidth int) string {
	var s strings.Builder

	if len(m.folders) == 0 {
		s.WriteString(dimStyle.Render(" No folders"))
		return s.String()
	}

	maxName := availWidth - 8
	if maxName < 5 {
		maxName = 5
	}

	start := 0
	end := len(m.folders)
	maxItems := availHeight
	if maxItems < 1 {
		maxItems = 1
	}

	if len(m.folders) > maxItems {
		start = m.selectedFolder - (maxItems / 2)
		if start < 0 {
			start = 0
		}
		end = start + maxItems
		if end > len(m.folders) {
			end = len(m.folders)
			start = end - maxItems
			if start < 0 {
				start = 0
			}
		}
	}

	for i := start; i < end; i++ {
		f := m.folders[i]
		displayName := f.DisplayName
		if len(displayName) > maxName {
			displayName = displayName[:maxName-2] + ".."
		}
		var countStr string
		if f.UnreadItemCount > 0 {
			countStr = fmt.Sprintf(" (%d)", f.UnreadItemCount)
		}
		line := fmt.Sprintf(" %s%s", displayName, countStr)
		if i == m.selectedFolder {
			s.WriteString(selectedItemStyle.Copy().Width(availWidth-2).Render(line) + "\n")
		} else if f.UnreadItemCount > 0 {
			s.WriteString(unreadStyle.Render(line) + "\n")
		} else {
			s.WriteString(" " + line[1:] + "\n")
		}
	}
	return s.String()
}

// renderMessagesViewWide is a variant of renderMessagesView that fits a wider column.
func (m mainModel) renderMessagesViewWide(availHeight, availWidth int) string {
	var s strings.Builder

	if len(m.virtualList) == 0 {
		s.WriteString(dimStyle.Render(" No messages"))
		return s.String()
	}

	// Each item takes 3 lines (header or member row)
	maxItems := availHeight / 3
	if maxItems < 1 {
		maxItems = 1
	}

	start := 0
	end := len(m.virtualList)
	if len(m.virtualList) > maxItems {
		start = m.virtualSelected - (maxItems / 2)
		if start < 0 {
			start = 0
		}
		end = start + maxItems
		if end > len(m.virtualList) {
			end = len(m.virtualList)
			start = end - maxItems
			if start < 0 {
				start = 0
			}
		}
	}

	maxFrom := availWidth - 6
	if maxFrom < 8 {
		maxFrom = 8
	}
	maxSubj := availWidth - 6
	if maxSubj < 8 {
		maxSubj = 8
	}

	for vi := start; vi < end; vi++ {
		item := m.virtualList[vi]
		tg := m.threadGroups[item.ThreadIdx]
		isSelected := vi == m.virtualSelected

		if item.IsHeader {
			msg := tg.Members[0]
			// Thread collapse indicator
			var threadIndicator string
			var countBadge string
			if len(tg.Members) > 1 {
				if m.collapsedThreads[tg.ConversationID] {
					threadIndicator = "▶ "
				} else {
					threadIndicator = "▼ "
				}
				countBadge = fmt.Sprintf(" [%d]", len(tg.Members))
			} else {
				threadIndicator = "  "
			}
			fromName := msg.From.EmailAddress.Name
			if fromName == "" {
				fromName = msg.From.EmailAddress.Address
			}
			unreadMarker := " "
			if !msg.IsRead {
				unreadMarker = "●"
			}
			attachMarker := " "
			if msg.HasAttachments {
				attachMarker = "@"
			}
			subj := tg.Subject
			if subj == "" {
				subj = "(No Subject)"
			}
			dateStr := msg.ReceivedDateTime.Local().Format("Jan 2 15:04")
			// Truncate to fit
			// line1 format: threadIndicator + unreadMarker + " " + fromName + countBadge + "  " + dateStr
			leftOverhead := len(threadIndicator) + len(unreadMarker) + 1 + len(countBadge)
			rightOverhead := 2 + len(dateStr)
			maxFN := (availWidth - 2) - leftOverhead - rightOverhead
			if maxFN < 4 {
				maxFN = 4
			}
			if len(fromName) > maxFN {
				fromName = fromName[:maxFN-2] + ".."
			}
			if len(subj) > maxSubj {
				subj = subj[:maxSubj-2] + ".."
			}
			leftPart := fmt.Sprintf("%s%s %s%s", threadIndicator, unreadMarker, fromName, countBadge)
			rightPart := dateStr
			spaceCount := (availWidth - 2) - lipgloss.Width(leftPart) - lipgloss.Width(rightPart)
			if spaceCount < 2 {
				spaceCount = 2
			}
			line1 := leftPart + strings.Repeat(" ", spaceCount) + rightPart
			line2 := fmt.Sprintf("  %s %s", attachMarker, subj)
			if isSelected {
				s.WriteString(selectedItemStyle.Copy().Width(availWidth-2).Render(line1) + "\n" + dimStyle.Render(line2) + "\n\n")
			} else if !msg.IsRead {
				s.WriteString(unreadStyle.Render(line1) + "\n" + dimStyle.Render(line2) + "\n\n")
			} else {
				s.WriteString(line1 + "\n" + dimStyle.Render(line2) + "\n\n")
			}
		} else {
			// Indented reply row
			msg := tg.Members[item.MemberIdx]
			fromName := msg.From.EmailAddress.Name
			if fromName == "" {
				fromName = msg.From.EmailAddress.Address
			}
			unreadMarker := " "
			if !msg.IsRead {
				unreadMarker = "●"
			}
			dateStr := msg.ReceivedDateTime.Local().Format("Jan 2 15:04")
			leftOverhead := 6 // "  └ " (4) + unreadMarker (1) + " " (1)
			rightOverhead := 2 + len(dateStr)
			maxFN2 := (availWidth - 2) - leftOverhead - rightOverhead
			if maxFN2 < 4 {
				maxFN2 = 4
			}
			if len(fromName) > maxFN2 {
				fromName = fromName[:maxFN2-2] + ".."
			}
			leftPart := fmt.Sprintf("  └ %s %s", unreadMarker, fromName)
			rightPart := dateStr
			spaceCount := (availWidth - 2) - lipgloss.Width(leftPart) - lipgloss.Width(rightPart)
			if spaceCount < 2 {
				spaceCount = 2
			}
			line1 := leftPart + strings.Repeat(" ", spaceCount) + rightPart
			preview := strings.Join(strings.Fields(msg.BodyPreview), " ")
			line2 := fmt.Sprintf("    %s", preview)
			if len(line2) > availWidth-2 {
				line2 = line2[:availWidth-5] + "..."
			}
			if isSelected {
				s.WriteString(selectedItemStyle.Copy().Width(availWidth-2).Render(line1) + "\n" + dimStyle.Render(line2) + "\n\n")
			} else if !msg.IsRead {
				s.WriteString(unreadStyle.Render(line1) + "\n" + dimStyle.Render(line2) + "\n\n")
			} else {
				s.WriteString(dimStyle.Render(line1) + "\n" + dimStyle.Render(line2) + "\n\n")
			}
		}
	}
	return s.String()
}

func (m mainModel) renderFoldersView(availHeight int) string {
	var s strings.Builder

	if len(m.folders) == 0 {
		s.WriteString(dimStyle.Render(" No folders"))
		return s.String()
	}

	start := 0
	end := len(m.folders)

	maxItems := availHeight
	if maxItems < 1 {
		maxItems = 1
	}

	if len(m.folders) > maxItems {
		start = m.selectedFolder - (maxItems / 2)
		if start < 0 {
			start = 0
		}
		end = start + maxItems
		if end > len(m.folders) {
			end = len(m.folders)
			start = end - maxItems
			if start < 0 {
				start = 0
			}
		}
	}

	for i := start; i < end; i++ {
		f := m.folders[i]
		displayName := f.DisplayName
		if len(displayName) > 17 {
			displayName = displayName[:15] + ".."
		}

		var countStr string
		if f.UnreadItemCount > 0 {
			countStr = fmt.Sprintf(" (%d)", f.UnreadItemCount)
		}

		line := fmt.Sprintf(" %s%s", displayName, countStr)

		if i == m.selectedFolder {
			s.WriteString(selectedItemStyle.Copy().Width(21).Render(line) + "\n")
		} else if f.UnreadItemCount > 0 {
			s.WriteString(unreadStyle.Render(line) + "\n")
		} else {
			s.WriteString(" " + line[1:] + "\n")
		}
	}
	return s.String()
}

func (m mainModel) renderMessagesView(availHeight int) string {
	var s strings.Builder

	if len(m.virtualList) == 0 {
		s.WriteString(dimStyle.Render(" No messages"))
		return s.String()
	}

	// Each item takes 3 lines
	maxItems := availHeight / 3
	if maxItems < 1 {
		maxItems = 1
	}

	start := 0
	end := len(m.virtualList)
	if len(m.virtualList) > maxItems {
		start = m.virtualSelected - (maxItems / 2)
		if start < 0 {
			start = 0
		}
		end = start + maxItems
		if end > len(m.virtualList) {
			end = len(m.virtualList)
			start = end - maxItems
			if start < 0 {
				start = 0
			}
		}
	}

	// Narrow pane: tighter truncation
	maxFrom := 14
	maxSubj := 18

	for vi := start; vi < end; vi++ {
		item := m.virtualList[vi]
		tg := m.threadGroups[item.ThreadIdx]
		isSelected := vi == m.virtualSelected

		if item.IsHeader {
			msg := tg.Members[0]
			var threadIndicator string
			var countBadge string
			if len(tg.Members) > 1 {
				if m.collapsedThreads[tg.ConversationID] {
					threadIndicator = "▶"
				} else {
					threadIndicator = "▼"
				}
				countBadge = fmt.Sprintf("(%d)", len(tg.Members))
			}
			fromName := msg.From.EmailAddress.Name
			if fromName == "" {
				fromName = msg.From.EmailAddress.Address
			}
			if len(fromName) > maxFrom {
				fromName = fromName[:maxFrom-2] + ".."
			}
			unreadMarker := " "
			if !msg.IsRead {
				unreadMarker = "●"
			}
			attachMarker := " "
			if msg.HasAttachments {
				attachMarker = "@"
			}
			subj := tg.Subject
			if subj == "" {
				subj = "(No Subject)"
			}
			if len(subj) > maxSubj {
				subj = subj[:maxSubj-2] + ".."
			}
			// Build compact lines for narrow pane
			var line1 string
			if threadIndicator != "" {
				line1 = fmt.Sprintf("%s%s %s %s", threadIndicator, unreadMarker, fromName, countBadge)
			} else {
				line1 = fmt.Sprintf("%s %s", unreadMarker, fromName)
			}
			line2 := fmt.Sprintf("  %s %s", attachMarker, subj)
			if isSelected {
				s.WriteString(selectedItemStyle.Copy().Width(31).Render(line1) + "\n" + dimStyle.Render(line2) + "\n\n")
			} else if !msg.IsRead {
				s.WriteString(unreadStyle.Render(line1) + "\n" + dimStyle.Render(line2) + "\n\n")
			} else {
				s.WriteString(line1 + "\n" + dimStyle.Render(line2) + "\n\n")
			}
		} else {
			// Indented reply row (narrow)
			msg := tg.Members[item.MemberIdx]
			fromName := msg.From.EmailAddress.Name
			if fromName == "" {
				fromName = msg.From.EmailAddress.Address
			}
			if len(fromName) > 10 {
				fromName = fromName[:8] + ".."
			}
			unreadMarker := " "
			if !msg.IsRead {
				unreadMarker = "●"
			}
			line1 := fmt.Sprintf(" └%s%s", unreadMarker, fromName)
			line2 := fmt.Sprintf("   %s", msg.ReceivedDateTime.Local().Format("Jan 2"))
			if isSelected {
				s.WriteString(selectedItemStyle.Copy().Width(31).Render(line1) + "\n" + dimStyle.Render(line2) + "\n\n")
			} else if !msg.IsRead {
				s.WriteString(unreadStyle.Render(line1) + "\n" + dimStyle.Render(line2) + "\n\n")
			} else {
				s.WriteString(dimStyle.Render(line1) + "\n" + dimStyle.Render(line2) + "\n\n")
			}
		}
	}
	return s.String()
}

type cell struct {
	char  rune
	style string
}

func parseANSILine(line string) []cell {
	var cells []cell
	var currentStyle strings.Builder
	runes := []rune(line)
	inEscape := false

	for i := 0; i < len(runes); i++ {
		r := runes[i]
		if r == '\x1b' {
			inEscape = true
			currentStyle.WriteRune(r)
			continue
		}
		if inEscape {
			currentStyle.WriteRune(r)
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				inEscape = false
			}
			continue
		}
		cells = append(cells, cell{
			char:  r,
			style: currentStyle.String(),
		})
	}
	return cells
}

func cellsToString(cells []cell) string {
	var sb strings.Builder
	var lastStyle string
	for _, c := range cells {
		if c.style != lastStyle {
			if lastStyle != "" {
				sb.WriteString("\x1b[0m")
			}
			sb.WriteString(c.style)
			lastStyle = c.style
		}
		sb.WriteRune(c.char)
	}
	sb.WriteString("\x1b[0m")
	return sb.String()
}

func overlayLines(base, overlay string, x, y int) string {
	baseLines := strings.Split(base, "\n")
	overlayLines := strings.Split(overlay, "\n")

	for i, oLine := range overlayLines {
		targetY := y + i
		if targetY < 0 || targetY >= len(baseLines) {
			continue
		}

		bLine := baseLines[targetY]
		bCells := parseANSILine(bLine)
		oCells := parseANSILine(oLine)

		if len(bCells) < x {
			padding := make([]cell, x-len(bCells))
			for p := range padding {
				padding[p] = cell{char: ' '}
			}
			bCells = append(bCells, padding...)
		}

		for j, oCell := range oCells {
			pos := x + j
			bgSeq := BgSeq
			if oCell.style == "" {
				oCell.style = bgSeq
			} else {
				oCell.style = strings.ReplaceAll(oCell.style, "\x1b[0m", "\x1b[0m"+bgSeq)
				oCell.style = bgSeq + oCell.style
			}

			if pos >= len(bCells) {
				bCells = append(bCells, oCell)
			} else {
				bCells[pos] = oCell
			}
		}

		baseLines[targetY] = cellsToString(bCells)
	}

	return strings.Join(baseLines, "\n")
}

type yankOption struct {
	key         string
	name        string
	description string
}

var yankOptions = []yankOption{
	{key: "m", name: "Copy original message", description: "no quoting"},
	{key: "a", name: "Copy all message", description: "with quoting"},
	{key: "u", name: "Yank URL(s)", description: "extract & copy URLs"},
	{key: "s", name: "Copy subject", description: "copy subject text"},
}

func (m mainModel) renderYankDropdown(width int) string {
	dropdownWidth := width - 4
	if dropdownWidth < 20 {
		dropdownWidth = 20
	}

	var rows []string
	rows = append(rows, lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(ColorViolet)).Render(" SELECT YANK ACTION: "))

	for i, opt := range yankOptions {
		indicator := "  "
		if i == m.selectedYankIdx {
			indicator = "> "
		}

		line := fmt.Sprintf("%s y%s: %s (%s)", indicator, opt.key, opt.name, opt.description)

		// Pad/crop line
		if len(line) < dropdownWidth-2 {
			line = line + strings.Repeat(" ", dropdownWidth-2-len(line))
		} else if len(line) > dropdownWidth-2 {
			line = line[:dropdownWidth-5] + "..."
		}

		if i == m.selectedYankIdx {
			line = lipgloss.NewStyle().
				Foreground(lipgloss.Color(ColorBg)).
				Background(lipgloss.Color(ColorViolet)).
				Bold(true).
				Render(line)
		} else {
			line = lipgloss.NewStyle().
				Foreground(lipgloss.Color(ColorText)).
				Render(line)
		}
		rows = append(rows, line)
	}

	joined := strings.Join(rows, "\n")

	popupStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(ColorViolet)).
		Padding(0, 1)

	return popupStyle.Render(joined)
}

func (m mainModel) renderDeleteThreadConfirmPopup(width int) string {
	dropdownWidth := width - 4
	if dropdownWidth < 20 {
		dropdownWidth = 20
	}

	var rows []string
	headerText := " DELETE THREAD? "
	if len(headerText) < dropdownWidth-2 {
		headerText = headerText + strings.Repeat(" ", dropdownWidth-2-len(headerText))
	}
	rows = append(rows, lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(ColorRed)).Render(headerText))
	rows = append(rows, strings.Repeat(" ", dropdownWidth-2))

	line1 := fmt.Sprintf("You are about to delete all %d message(s) in the thread:", len(m.deleteThreadMsgIDs))
	if len(line1) < dropdownWidth-2 {
		line1 = line1 + strings.Repeat(" ", dropdownWidth-2-len(line1))
	} else if len(line1) > dropdownWidth-2 {
		line1 = line1[:dropdownWidth-5] + "..."
	}
	rows = append(rows, line1)

	subjText := fmt.Sprintf("  \"%s\"", m.deleteThreadSubject)
	if len(subjText) < dropdownWidth-2 {
		subjText = subjText + strings.Repeat(" ", dropdownWidth-2-len(subjText))
	} else if len(subjText) > dropdownWidth-2 {
		subjText = subjText[:dropdownWidth-5] + "..."
	}
	rows = append(rows, lipgloss.NewStyle().Foreground(lipgloss.Color(ColorSubtext)).Render(subjText))
	rows = append(rows, strings.Repeat(" ", dropdownWidth-2))

	line2 := "Do you really want to delete this entire thread?"
	if len(line2) < dropdownWidth-2 {
		line2 = line2 + strings.Repeat(" ", dropdownWidth-2-len(line2))
	} else if len(line2) > dropdownWidth-2 {
		line2 = line2[:dropdownWidth-5] + "..."
	}
	rows = append(rows, line2)
	rows = append(rows, strings.Repeat(" ", dropdownWidth-2))

	btnYesRaw := "  [y] Yes, delete thread"
	paddingYes := dropdownWidth - 2 - len(btnYesRaw)
	if paddingYes < 0 {
		paddingYes = 0
	}
	btnYesRendered := "  " + lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(ColorRed)).Render("[y]") + " Yes, delete thread" + strings.Repeat(" ", paddingYes)
	rows = append(rows, btnYesRendered)

	btnNoRaw := "  [n] No, keep thread / Go Back"
	paddingNo := dropdownWidth - 2 - len(btnNoRaw)
	if paddingNo < 0 {
		paddingNo = 0
	}
	btnNoRendered := "  " + lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(ColorGreen)).Render("[n]") + " No, keep thread / Go Back" + strings.Repeat(" ", paddingNo)
	rows = append(rows, btnNoRendered)

	joined := strings.Join(rows, "\n")

	popupStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(ColorRed)).
		Padding(0, 1)

	return popupStyle.Render(joined)
}

func (m mainModel) renderComposeCancelConfirmPopup(width int) string {
	dropdownWidth := width - 4
	if dropdownWidth < 20 {
		dropdownWidth = 20
	}

	var rows []string
	headerText := " DISCARD EMAIL? "
	if len(headerText) < dropdownWidth-2 {
		headerText = headerText + strings.Repeat(" ", dropdownWidth-2-len(headerText))
	}
	rows = append(rows, lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(ColorRed)).Render(headerText))
	rows = append(rows, strings.Repeat(" ", dropdownWidth-2))

	line1 := "You have draft content in the body."
	if len(line1) < dropdownWidth-2 {
		line1 = line1 + strings.Repeat(" ", dropdownWidth-2-len(line1))
	} else if len(line1) > dropdownWidth-2 {
		line1 = line1[:dropdownWidth-5] + "..."
	}
	rows = append(rows, line1)

	line2 := "Do you really want to quit composing?"
	if len(line2) < dropdownWidth-2 {
		line2 = line2 + strings.Repeat(" ", dropdownWidth-2-len(line2))
	} else if len(line2) > dropdownWidth-2 {
		line2 = line2[:dropdownWidth-5] + "..."
	}
	rows = append(rows, line2)
	rows = append(rows, strings.Repeat(" ", dropdownWidth-2))

	btnYesRaw := "  [y] Yes, discard changes"
	paddingYes := dropdownWidth - 2 - len(btnYesRaw)
	if paddingYes < 0 {
		paddingYes = 0
	}
	btnYesRendered := "  " + lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(ColorRed)).Render("[y]") + " Yes, discard changes" + strings.Repeat(" ", paddingYes)
	rows = append(rows, btnYesRendered)

	btnNoRaw := "  [n] No, keep editing / Go Back"
	paddingNo := dropdownWidth - 2 - len(btnNoRaw)
	if paddingNo < 0 {
		paddingNo = 0
	}
	btnNoRendered := "  " + lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(ColorGreen)).Render("[n]") + " No, keep editing / Go Back" + strings.Repeat(" ", paddingNo)
	rows = append(rows, btnNoRendered)

	joined := strings.Join(rows, "\n")

	popupStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(ColorRed)).
		Padding(0, 1)

	return popupStyle.Render(joined)
}

func (m mainModel) renderAttachmentsDropdown(width int) string {
	dropdownWidth := width - 4
	if dropdownWidth < 20 {
		dropdownWidth = 20
	}

	var rows []string
	headerText := " SELECT ATTACHMENT: "
	if len(headerText) < dropdownWidth-2 {
		headerText = headerText + strings.Repeat(" ", dropdownWidth-2-len(headerText))
	}
	rows = append(rows, lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(ColorViolet)).Render(headerText))
	rows = append(rows, strings.Repeat(" ", dropdownWidth-2))

	maxVisible := 10
	totalAttach := len(m.attachments)

	startIdx := 0
	endIdx := totalAttach

	if totalAttach > maxVisible {
		startIdx = m.selectedAttach - maxVisible/2
		if startIdx < 0 {
			startIdx = 0
		}
		endIdx = startIdx + maxVisible
		if endIdx > totalAttach {
			endIdx = totalAttach
			startIdx = endIdx - maxVisible
		}
	}

	if startIdx > 0 {
		upText := "  ▲ ... more attachments above ..."
		if len(upText) < dropdownWidth-2 {
			upText = upText + strings.Repeat(" ", dropdownWidth-2-len(upText))
		}
		rows = append(rows, lipgloss.NewStyle().Foreground(lipgloss.Color(ColorOverlay)).Render(upText))
	}

	for i := startIdx; i < endIdx; i++ {
		att := m.attachments[i]
		indicator := "  "
		if i == m.selectedAttach {
			indicator = "> "
		}

		sizeKB := att.Size / 1024
		inlineStr := ""
		if att.IsInline {
			inlineStr = " [inline]"
		}
		line := fmt.Sprintf("%s%s (%d KB) [%s]%s", indicator, att.Name, sizeKB, att.ContentType, inlineStr)

		// Pad/crop line
		if len(line) < dropdownWidth-2 {
			line = line + strings.Repeat(" ", dropdownWidth-2-len(line))
		} else if len(line) > dropdownWidth-2 {
			line = line[:dropdownWidth-5] + "..."
		}

		if i == m.selectedAttach {
			line = lipgloss.NewStyle().
				Foreground(lipgloss.Color(ColorBg)).
				Background(lipgloss.Color(ColorViolet)).
				Bold(true).
				Render(line)
		} else {
			line = lipgloss.NewStyle().
				Foreground(lipgloss.Color(ColorText)).
				Render(line)
		}
		rows = append(rows, line)
	}

	if endIdx < totalAttach {
		downText := "  ▼ ... more attachments below ..."
		if len(downText) < dropdownWidth-2 {
			downText = downText + strings.Repeat(" ", dropdownWidth-2-len(downText))
		}
		rows = append(rows, lipgloss.NewStyle().Foreground(lipgloss.Color(ColorOverlay)).Render(downText))
	}

	joined := strings.Join(rows, "\n")

	popupStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(ColorViolet)).
		Padding(0, 1)

	return popupStyle.Render(joined)
}

func (m mainModel) renderURLDropdown(width int) string {
	dropdownWidth := width - 4
	if dropdownWidth < 20 {
		dropdownWidth = 20
	}

	var rows []string
	rows = append(rows, lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(ColorViolet)).Render(" SELECT URL TO COPY: "))

	maxVisible := 10
	totalURLs := len(m.extractedURLs)

	startIdx := 0
	endIdx := totalURLs

	if totalURLs > maxVisible {
		startIdx = m.selectedURLIdx - maxVisible/2
		if startIdx < 0 {
			startIdx = 0
		}
		endIdx = startIdx + maxVisible
		if endIdx > totalURLs {
			endIdx = totalURLs
			startIdx = endIdx - maxVisible
		}
	}

	if startIdx > 0 {
		rows = append(rows, lipgloss.NewStyle().Foreground(lipgloss.Color(ColorOverlay)).Render("  ▲ ... more URLs above ..."))
	}

	for i := startIdx; i < endIdx; i++ {
		url := m.extractedURLs[i]
		indicator := "  "
		if i == m.selectedURLIdx {
			indicator = "> "
		}

		line := fmt.Sprintf("%s%s", indicator, url)

		// Pad/crop line
		if len(line) < dropdownWidth-2 {
			line = line + strings.Repeat(" ", dropdownWidth-2-len(line))
		} else if len(line) > dropdownWidth-2 {
			line = line[:dropdownWidth-5] + "..."
		}

		if i == m.selectedURLIdx {
			line = lipgloss.NewStyle().
				Foreground(lipgloss.Color(ColorBg)).
				Background(lipgloss.Color(ColorViolet)).
				Bold(true).
				Render(line)
		} else {
			line = lipgloss.NewStyle().
				Foreground(lipgloss.Color(ColorText)).
				Render(line)
		}
		rows = append(rows, line)
	}

	if endIdx < totalURLs {
		rows = append(rows, lipgloss.NewStyle().Foreground(lipgloss.Color(ColorOverlay)).Render("  ▼ ... more URLs below ..."))
	}

	joined := strings.Join(rows, "\n")

	popupStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(ColorViolet)).
		Padding(0, 1)

	return popupStyle.Render(joined)
}

func (m mainModel) executeYank(key string) mainModel {
	am := m.activeMessage()
	if am == nil {
		m.statusMsg = "No message selected"
		m.state = stateMain
		return m.updateViewportSize()
	}
	if m.detailMessage == nil || m.detailMessage.ID != am.ID {
		m.statusMsg = "Message details loading, please try again..."
		m.state = stateMain
		return m.updateViewportSize()
	}

	switch key {
	case "m":
		text := extractCleanText(m.detailMessage.Body.Content, true)
		if text == "" {
			m.statusMsg = "Original message body is empty"
		} else if err := clipboard.WriteAll(text); err != nil {
			m.statusMsg = fmt.Sprintf("Failed to copy message: %v", err)
		} else {
			m.statusMsg = "Copied original message (no quoting) to clipboard!"
		}
		m.state = stateMain

	case "a":
		text := extractCleanText(m.detailMessage.Body.Content, false)
		if text == "" {
			m.statusMsg = "Message body is empty"
		} else if err := clipboard.WriteAll(text); err != nil {
			m.statusMsg = fmt.Sprintf("Failed to copy message: %v", err)
		} else {
			m.statusMsg = "Copied all message (with quoting) to clipboard!"
		}
		m.state = stateMain

	case "u":
		urls := extractURLsFromMainMessage(m.detailMessage.Body.Content, m.detailMessage.Subject)
		if len(urls) == 0 {
			m.statusMsg = "No URLs found in the main message"
			m.state = stateMain
		} else if len(urls) == 1 {
			if err := clipboard.WriteAll(urls[0]); err != nil {
				m.statusMsg = fmt.Sprintf("Failed to copy URL: %v", err)
			} else {
				m.statusMsg = "Copied URL to clipboard!"
			}
			m.state = stateMain
		} else {
			m.extractedURLs = urls
			m.selectedURLIdx = 0
			m.state = stateURLSelect
			m.statusMsg = "Select URL to copy"
		}

	case "s":
		subject := m.detailMessage.Subject
		if subject == "" {
			m.statusMsg = "Subject is empty"
		} else if err := clipboard.WriteAll(subject); err != nil {
			m.statusMsg = fmt.Sprintf("Failed to copy subject: %v", err)
		} else {
			m.statusMsg = "Copied subject to clipboard!"
		}
		m.state = stateMain
	}

	return m.updateViewportSize()
}

func extractCleanText(htmlContent string, excludeQuoting bool) string {
	htmlContent = strings.ReplaceAll(htmlContent, "\r", "")
	res := replaceAnchorTags(htmlContent, false)

	// Replace <img> tags with a simple "[image]" placeholder or "[image: alt/cid]"
	res = regexp.MustCompile(`(?i)<img\b[^>]*>`).ReplaceAllStringFunc(res, func(match string) string {
		altReg := regexp.MustCompile(`(?i)alt\s*=\s*['"]([^'"]+)['"]`)
		altMatches := altReg.FindStringSubmatch(match)
		if len(altMatches) > 1 {
			return fmt.Sprintf("[image: %s]", altMatches[1])
		}

		srcReg := regexp.MustCompile(`(?i)src\s*=\s*['"]?cid:([^'"\s>]+)['"]?`)
		srcMatches := srcReg.FindStringSubmatch(match)
		if len(srcMatches) > 1 {
			cid := srcMatches[1]
			if idx := strings.Index(cid, "@"); idx >= 0 {
				cid = cid[:idx]
			}
			return fmt.Sprintf("[image: %s]", cid)
		}
		return "[image]"
	})

	// Simple tags stripping
	res = regexp.MustCompile(`(?i)<br(?:\s*\/)?>`).ReplaceAllString(res, "\n")
	res = regexp.MustCompile(`(?i)<p(?:\s+[^>]*)?>`).ReplaceAllString(res, "\n\n")
	res = strings.ReplaceAll(res, "</p>", "\n\n")
	res = regexp.MustCompile(`(?i)<div(?:\s+[^>]*)?>`).ReplaceAllString(res, "\n")
	res = strings.ReplaceAll(res, "</div>", "\n")
	res = regexp.MustCompile(`(?i)<li(?:\s+[^>]*)?>`).ReplaceAllString(res, "\n• ")
	res = regexp.MustCompile(`(?i)<h[1-6](?:\s+[^>]*)?>`).ReplaceAllString(res, "\n\n")
	res = regexp.MustCompile(`(?i)</h[1-6]>`).ReplaceAllString(res, "\n\n")
	res = regexp.MustCompile(`(?i)<tr(?:\s+[^>]*)?>`).ReplaceAllString(res, "\n")
	res = regexp.MustCompile(`(?i)<td(?:\s+[^>]*)?>`).ReplaceAllString(res, " ")

	// Strip all other HTML tags
	var builder strings.Builder
	inTag := false
	runes := []rune(res)
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		if r == '<' {
			isTag := false
			if i+1 < len(runes) {
				next := runes[i+1]
				if (next >= 'a' && next <= 'z') || (next >= 'A' && next <= 'Z') || next == '!' || next == '?' {
					isTag = true
				} else if next == '/' && i+2 < len(runes) {
					next2 := runes[i+2]
					if (next2 >= 'a' && next2 <= 'z') || (next2 >= 'A' && next2 <= 'Z') {
						isTag = true
					}
				}
			}
			if isTag {
				inTag = true
				continue
			}
		}
		if r == '>' {
			if inTag {
				inTag = false
				continue
			}
		}
		if !inTag {
			builder.WriteRune(r)
		}
	}

	unescaped := html.UnescapeString(builder.String())
	unescaped = strings.ReplaceAll(unescaped, "\u00a0", " ")

	lines := strings.Split(unescaped, "\n")
	var cleaned []string
	inOriginal := false
	for _, l := range lines {
		plainLine := stripANSICodes(l)
		trimmedPlain := strings.TrimSpace(plainLine)

		if !inOriginal && isOriginalMessageStart(trimmedPlain) {
			inOriginal = true
		}

		if excludeQuoting {
			if inOriginal || strings.HasPrefix(trimmedPlain, ">") {
				continue
			}
		}

		if trimmedPlain != "" || (len(cleaned) > 0 && cleaned[len(cleaned)-1] != "") {
			cleaned = append(cleaned, plainLine)
		}
	}

	result := strings.Join(cleaned, "\n")
	return strings.TrimSpace(result)
}

func (m mainModel) renderDetailView() string {
	var s strings.Builder

	if m.detailMessage == nil {
		s.WriteString(dimStyle.Render(" Select a message to view details"))
		return s.String()
	}

	// Layout 1: left 60 cols used by Folders+Messages panes (outer 25+35)
	// Layout 2: left 50 cols used by the stacked left column
	var detailWidth int
	if m.config.Layout == 2 {
		detailWidth = m.width - 54 // m.width - leftColOuter(50) - ownPad(4)
	} else {
		detailWidth = m.width - 64 // m.width - outerF(25) - outerM(35) - ownPad(4)
	}
	if detailWidth < 20 {
		detailWidth = 20
	}

	s.WriteString(m.renderMetaBlock(detailWidth))
	s.WriteString(m.detailViewport.View())

	return s.String()
}

func (m mainModel) renderMetaBlock(width int) string {
	if m.detailMessage == nil {
		return ""
	}
	var s strings.Builder

	// Helper to format recipients list
	formatRecipients := func(recipients []Recipient) string {
		var parts []string
		for _, r := range recipients {
			name := r.EmailAddress.Name
			addr := r.EmailAddress.Address
			if name == "" {
				parts = append(parts, addr)
			} else if addr == "" {
				parts = append(parts, name)
			} else {
				parts = append(parts, fmt.Sprintf("%s <%s>", name, addr))
			}
		}
		return strings.Join(parts, ", ")
	}

	fromVal := fmt.Sprintf("%s <%s>", m.detailMessage.From.EmailAddress.Name, m.detailMessage.From.EmailAddress.Address)
	dateStr := m.detailMessage.ReceivedDateTime.Local().Format("Mon, Jan 2, 2006 at 15:04")

	s.WriteString(wrapText(lipgloss.NewStyle().Bold(true).Render("Subject: "+m.detailMessage.Subject), width) + "\n")
	s.WriteString(wrapText(lipgloss.NewStyle().Bold(true).Render("From:    ")+fromVal, width) + "\n")

	toVal := formatRecipients(m.detailMessage.ToRecipients)
	if toVal != "" {
		s.WriteString(wrapText(lipgloss.NewStyle().Bold(true).Render("To:      ")+toVal, width) + "\n")
	}

	ccVal := formatRecipients(m.detailMessage.CcRecipients)
	if ccVal != "" {
		s.WriteString(wrapText(lipgloss.NewStyle().Bold(true).Render("Cc:      ")+ccVal, width) + "\n")
	}

	s.WriteString(wrapText(lipgloss.NewStyle().Bold(true).Render("Date:    ")+dateStr, width) + "\n")

	if len(m.attachments) > 0 {
		attStr := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(ColorViolet)).Render(fmt.Sprintf("Attachments (📎 %d): ", len(m.attachments))) +
			dimStyle.Render("Press [a] to view/download attachments")
		s.WriteString(wrapText(attStr, width) + "\n")
	}

	sep := strings.Repeat("-", width-2)
	s.WriteString(dimStyle.Render(sep) + "\n")

	return s.String()
}

// formatBodyContent strips/cleans up HTML email bodies to readable plain text
func formatBodyContent(htmlContent string) string {
	htmlContent = strings.ReplaceAll(htmlContent, "\r", "")

	// Replace pre and code tags with state markers before any tag stripping
	res := regexp.MustCompile(`(?i)<pre(?:\s+[^>]*)?>`).ReplaceAllString(htmlContent, "\x01PRE_START\x01")
	res = regexp.MustCompile(`(?i)</pre>`).ReplaceAllString(res, "\x01PRE_END\x01")
	res = regexp.MustCompile(`(?i)<code(?:\s+[^>]*)?>`).ReplaceAllString(res, "\x01CODE_START\x01")
	res = regexp.MustCompile(`(?i)</code>`).ReplaceAllString(res, "\x01CODE_END\x01")

	// First, replace <a> tags so that URLs are preserved before tag stripping
	res = replaceAnchorTags(res, true)

	// Replace <img> tags with a styled "[image]" placeholder
	res = regexp.MustCompile(`(?i)<img\b[^>]*>`).ReplaceAllStringFunc(res, func(match string) string {
		altReg := regexp.MustCompile(`(?i)alt\s*=\s*['"]([^'"]+)['"]`)
		altMatches := altReg.FindStringSubmatch(match)
		if len(altMatches) > 1 {
			return imagePlaceholderStyle.Render(fmt.Sprintf("[image: %s]", altMatches[1]))
		}

		srcReg := regexp.MustCompile(`(?i)src\s*=\s*['"]?cid:([^'"\s>]+)['"]?`)
		srcMatches := srcReg.FindStringSubmatch(match)
		if len(srcMatches) > 1 {
			cid := srcMatches[1]
			if idx := strings.Index(cid, "@"); idx >= 0 {
				cid = cid[:idx]
			}
			return imagePlaceholderStyle.Render(fmt.Sprintf("[image: %s]", cid))
		}
		return imagePlaceholderStyle.Render("[image]")
	})

	// Convert HTML inline styles (colors, background-colors) to ANSI escape sequences
	res = convertInlineStylesToANSI(res)

	// Convert formatting tags to ANSI escape sequences before stripping HTML tags
	res = regexp.MustCompile(`(?i)<(b|strong)(?:\s+[^>]*)?>`).ReplaceAllString(res, "\x1b[1m")
	res = regexp.MustCompile(`(?i)</(b|strong)\s*>`).ReplaceAllString(res, "\x1b[22m")

	res = regexp.MustCompile(`(?i)<(i|em)(?:\s+[^>]*)?>`).ReplaceAllString(res, "\x1b[3m")
	res = regexp.MustCompile(`(?i)</(i|em)\s*>`).ReplaceAllString(res, "\x1b[23m")

	res = regexp.MustCompile(`(?i)<u(?:\s+[^>]*)?>`).ReplaceAllString(res, "\x1b[4m")
	res = regexp.MustCompile(`(?i)</u\s*>`).ReplaceAllString(res, "\x1b[24m")

	// Style inline code blocks (Yellow: #F9E2AF)
	res = strings.ReplaceAll(res, "\x01CODE_START\x01", "\x1b[38;2;249;226;175m")
	res = strings.ReplaceAll(res, "\x01CODE_END\x01", "\x1b[39m")

	// Simple tags stripping
	// In a complete implementation, a real HTML-to-text parser would be used.
	// We'll replace simple tags to preserve readability.
	res = regexp.MustCompile(`(?i)<br(?:\s*\/)?>`).ReplaceAllString(res, "\n")
	res = regexp.MustCompile(`(?i)<p(?:\s+[^>]*)?>`).ReplaceAllString(res, "\n\n")
	res = strings.ReplaceAll(res, "</p>", "\n\n")
	res = regexp.MustCompile(`(?i)<div(?:\s+[^>]*)?>`).ReplaceAllString(res, "\n")
	res = strings.ReplaceAll(res, "</div>", "\n")
	res = regexp.MustCompile(`(?i)<li(?:\s+[^>]*)?>`).ReplaceAllString(res, "\n• ")
	res = regexp.MustCompile(`(?i)<h[1-6](?:\s+[^>]*)?>`).ReplaceAllString(res, "\n\n")
	res = regexp.MustCompile(`(?i)</h[1-6]>`).ReplaceAllString(res, "\n\n")
	res = regexp.MustCompile(`(?i)<tr(?:\s+[^>]*)?>`).ReplaceAllString(res, "\n")
	res = regexp.MustCompile(`(?i)<td(?:\s+[^>]*)?>`).ReplaceAllString(res, " ")

	// Strip all other HTML tags
	var builder strings.Builder
	inTag := false
	runes := []rune(res)
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		if r == '<' {
			// Check if this '<' actually starts a tag/comment/processing instruction.
			// It must be followed by at least one character, which must be:
			// - a letter (a-z, A-Z)
			// - '/' followed by a letter (a-z, A-Z)
			// - '!' (e.g., comment or doctype)
			// - '?' (e.g., processing instruction)
			isTag := false
			if i+1 < len(runes) {
				next := runes[i+1]
				if (next >= 'a' && next <= 'z') || (next >= 'A' && next <= 'Z') || next == '!' || next == '?' {
					isTag = true
				} else if next == '/' && i+2 < len(runes) {
					next2 := runes[i+2]
					if (next2 >= 'a' && next2 <= 'z') || (next2 >= 'A' && next2 <= 'Z') {
						isTag = true
					}
				}
			}
			if isTag {
				inTag = true
				continue
			}
		}
		if r == '>' {
			if inTag {
				inTag = false
				continue
			}
		}
		if !inTag {
			builder.WriteRune(r)
		}
	}

	unescaped := html.UnescapeString(builder.String())
	// Replace non-breaking spaces (\u00a0) with regular spaces to prevent display issues in the terminal
	unescaped = strings.ReplaceAll(unescaped, "\u00a0", " ")

	// Replace various Unicode space characters with regular spaces to prevent display issues
	for _, spaceChar := range []string{
		"\u2000", "\u2001", "\u2002", "\u2003", "\u2004", "\u2005",
		"\u2006", "\u2007", "\u2008", "\u2009", "\u200a", "\u202f",
		"\u205f", "\u3000",
	} {
		unescaped = strings.ReplaceAll(unescaped, spaceChar, " ")
	}

	// Strip zero-width and invisible formatting characters that break line wrapping and terminal layout
	for _, formatChar := range []string{
		"\u00ad", "\u034f", "\u200b", "\u200c", "\u200d", "\u200e",
		"\u200f", "\ufeff",
	} {
		unescaped = strings.ReplaceAll(unescaped, formatChar, "")
	}

	// Clean up whitespace and apply dimming/URL styling to lines
	lines := strings.Split(unescaped, "\n")
	var cleaned []string
	inOriginal := false
	inPre := false
	var pendingANSI string

	var addRx = regexp.MustCompile(`(?i)^\s*(?:\d+\s+)?(?:\d+\s+)?\+(?:\s|$|[^+])`)
	var delRx = regexp.MustCompile(`(?i)^\s*(?:\d+\s+)?(?:\d+\s+)?-(?:\s|$|[^-])`)
	var hunkRx = regexp.MustCompile(`^\s*@@`)
	var fileRx = regexp.MustCompile(`^\s*(?:---|\+\+\+)\s`)
	var gitDiffRx = regexp.MustCompile(`^(?:diff --git|index\s[0-9a-fA-F]{7,}\b|similarity index\s|rename from\s|rename to\s)`)

	for _, l := range lines {
		hasStart := strings.Contains(l, "\x01PRE_START\x01")
		hasEnd := strings.Contains(l, "\x01PRE_END\x01")

		if hasStart {
			inPre = true
			l = strings.ReplaceAll(l, "\x01PRE_START\x01", "")
		}
		lineIsInPre := inPre
		if hasEnd {
			inPre = false
			l = strings.ReplaceAll(l, "\x01PRE_END\x01", "")
		}

		trimmed := strings.TrimSpace(l)
		if (hasStart || hasEnd) && trimmed == "" {
			continue
		}

		// If a line contains only ANSI escape codes, accumulate them and don't output a blank line
		if trimmed != "" && stripANSICodes(trimmed) == "" {
			pendingANSI += l
			continue
		}

		if trimmed != "" || (len(cleaned) > 0 && cleaned[len(cleaned)-1] != "") {
			if pendingANSI != "" {
				l = pendingANSI + l
				pendingANSI = ""
			}

			plainLine := stripANSICodes(l)
			if !inOriginal && isOriginalMessageStart(plainLine) {
				inOriginal = true
			}

			isDimmed := l != "" && (inOriginal || strings.HasPrefix(strings.TrimSpace(plainLine), ">"))

			// Apply URL styling
			lineWithURLs := styleURLs(l, isDimmed)

			// Replace link markers with ANSI escape codes
			startCode := "\x1b[38;2;137;180;250;4m"
			var endCode string
			if isDimmed {
				endCode = "\x1b[24;38;2;166;173;200m"
			} else {
				endCode = "\x1b[24;39m"
			}
			lineWithURLs = replaceLinkMarkers(lineWithURLs, startCode, endCode)

			// Diff & code highlighting
			isAdd := addRx.MatchString(plainLine)
			isDel := delRx.MatchString(plainLine)
			isHunk := hunkRx.MatchString(plainLine)
			isFile := fileRx.MatchString(plainLine)
			isGitDiff := gitDiffRx.MatchString(plainLine)

			if isAdd {
				lineWithURLs = "\x1b[38;2;166;227;161m" + plainLine + "\x1b[39m"
			} else if isDel {
				lineWithURLs = "\x1b[38;2;243;139;168m" + plainLine + "\x1b[39m"
			} else if isHunk {
				lineWithURLs = "\x1b[38;2;137;180;250m" + plainLine + "\x1b[39m"
			} else if isFile || isGitDiff {
				lineWithURLs = "\x1b[38;2;249;226;175m" + plainLine + "\x1b[39m"
			} else if lineIsInPre {
				lineWithURLs = "\x1b[38;2;203;166;247m" + plainLine + "\x1b[39m"
			}

			if isDimmed && !isAdd && !isDel && !isHunk && !isFile && !isGitDiff && !lineIsInPre {
				cleaned = append(cleaned, dimStyle.Render(lineWithURLs))
			} else {
				cleaned = append(cleaned, lineWithURLs)
			}
		}
	}
	if pendingANSI != "" && len(cleaned) > 0 {
		cleaned[len(cleaned)-1] += pendingANSI
	}
	return strings.Join(cleaned, "\n")
}

// extractRemoteImages parses the HTML body content to find all <img> tags with remote http/https src URLs,
// and returns them as virtual Attachment objects that can be downloaded/viewed.
func extractRemoteImages(htmlContent string) []Attachment {
	var atts []Attachment
	imgReg := regexp.MustCompile(`(?i)<img\b([^>]*)>`)
	srcReg := regexp.MustCompile(`(?i)src\s*=\s*['"]([^'"]+)['"]`)
	altReg := regexp.MustCompile(`(?i)alt\s*=\s*['"]([^'"]+)['"]`)

	matches := imgReg.FindAllStringSubmatch(htmlContent, -1)
	seenUrls := make(map[string]bool)

	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		attrs := match[1]
		srcMatch := srcReg.FindStringSubmatch(attrs)
		if len(srcMatch) < 2 {
			continue
		}
		src := srcMatch[1]

		// Only handle http/https URLs
		if !strings.HasPrefix(strings.ToLower(src), "http://") && !strings.HasPrefix(strings.ToLower(src), "https://") {
			continue
		}

		// Avoid duplicates
		if seenUrls[src] {
			continue
		}
		seenUrls[src] = true

		// Get a name for the attachment
		name := ""
		altMatch := altReg.FindStringSubmatch(attrs)
		if len(altMatch) > 1 {
			name = altMatch[1]
		}

		// If alt is not present or clean, derive name from URL path
		if name == "" {
			parsedURL, err := url.Parse(src)
			if err == nil {
				path := parsedURL.Path
				if path != "" && path != "/" {
					name = filepath.Base(path)
				}
			}
		}

		// If still empty, give a generic name
		if name == "" || name == "." {
			name = "remote_image"
		}

		// Clean up name and ensure it has an extension (default to .png/jpg if missing)
		name = cleanFilename(name)
		ext := filepath.Ext(name)
		extLower := strings.ToLower(ext)
		if extLower != ".png" && extLower != ".jpg" && extLower != ".jpeg" && extLower != ".gif" && extLower != ".bmp" && extLower != ".webp" {
			name = name + ".png" // default extension for rendering/viewing
		}

		atts = append(atts, Attachment{
			OdataType:   "#outlook-tui.remoteImage",
			Name:        name,
			ContentType: "image/png", // default fallback
			IsInline:    true,
			ContentId:   src, // Store the remote URL in ContentId
		})
	}
	return atts
}

func cleanFilename(s string) string {
	// replace illegal characters with underscores
	reg := regexp.MustCompile(`[^a-zA-Z0-9._-]`)
	cleaned := reg.ReplaceAllString(s, "_")
	if cleaned == "" {
		cleaned = "image"
	}
	return cleaned
}

// replaceAnchorTags finds <a> tags with hrefs and replaces them in-place.
// If forDisplay is true:
//   - If the text and URL are the same, returns the URL wrapped in blue/cyan link styling.
//   - If they are different, returns the text wrapped in blue/cyan link styling, hiding the URL.
//
// If forDisplay is false:
//   - Returns "text (url)" if text and url are substantially different.
//   - Returns "url" if they are the same or if text is empty.
func replaceAnchorTags(htmlContent string, forDisplay bool) string {
	anchorRx := regexp.MustCompile(`(?i)<a\s+[^>]*href=["']([^"']*)["'][^>]*>([\s\S]*?)</a>`)
	return anchorRx.ReplaceAllStringFunc(htmlContent, func(match string) string {
		submatches := anchorRx.FindStringSubmatch(match)
		if len(submatches) < 3 {
			return match
		}
		url := strings.TrimSpace(submatches[1])
		text := strings.TrimSpace(submatches[2])

		// Handle empty or self-referential links
		if url == "" {
			return text
		}
		if text == "" {
			if forDisplay {
				lowerURL := strings.ToLower(url)
				if strings.HasPrefix(lowerURL, "http://") || strings.HasPrefix(lowerURL, "https://") {
					return url
				}
				return "\x01" + url + "\x02"
			}
			return url
		}

		// Strip mailto: or tel: prefixes for clean display
		displayURL := url
		if strings.HasPrefix(strings.ToLower(displayURL), "mailto:") {
			displayURL = displayURL[7:]
		} else if strings.HasPrefix(strings.ToLower(displayURL), "tel:") {
			displayURL = displayURL[4:]
		}

		// Compare cleaned versions to avoid redundancy (e.g. <a href="http://google.com">google.com</a>)
		cleanText := strings.TrimSuffix(strings.TrimPrefix(strings.TrimPrefix(strings.ToLower(text), "https://"), "http://"), "/")
		cleanURL := strings.TrimSuffix(strings.TrimPrefix(strings.TrimPrefix(strings.ToLower(displayURL), "https://"), "http://"), "/")

		if cleanText == cleanURL {
			if forDisplay {
				lowerDisplayURL := strings.ToLower(displayURL)
				if strings.HasPrefix(lowerDisplayURL, "http://") || strings.HasPrefix(lowerDisplayURL, "https://") {
					return displayURL
				}
				return "\x01" + displayURL + "\x02"
			}
			return displayURL
		}

		if forDisplay {
			return "\x01" + text + "\x02 "
		}

		// Append a trailing space so that text immediately following </a> (with no
		// whitespace) cannot be absorbed into the URL by the greedy URL regex used
		// in styleURLs / extractURLsFromMainMessage.  E.g. without the space,
		// `<a href="…/SR-14">SR-14</a>Created` → `SR-14 (…/SR-14)Created` which
		// makes the regex capture `…/SR-14)Created` as the URL.
		return fmt.Sprintf("%s (%s) ", text, displayURL)
	})
}

// replaceLinkMarkers replaces '\x01' and '\x02' with startCode and endCode.
// Within the link boundaries, if any style reset sequence (like \x1b[0m, \x1b[m, \x1b[39m, \x1b[24m)
// is encountered, it is immediately followed by startCode to restore the link's styling.
func replaceLinkMarkers(line string, startCode, endCode string) string {
	var buf strings.Builder
	inLink := false
	runes := []rune(line)
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		if r == '\x01' {
			buf.WriteString(startCode)
			inLink = true
		} else if r == '\x02' {
			buf.WriteString(endCode)
			inLink = false
		} else if inLink && r == '\x1b' && i+1 < len(runes) && runes[i+1] == '[' {
			// Find the termination of this ANSI escape sequence (usually 'm')
			j := i + 2
			for j < len(runes) && (runes[j] == ';' || (runes[j] >= '0' && runes[j] <= '9')) {
				j++
			}
			if j < len(runes) && runes[j] == 'm' {
				params := string(runes[i+2 : j])
				isReset := params == "0" || params == "" || params == "39" || params == "24"
				buf.WriteString(string(runes[i : j+1]))
				if isReset {
					buf.WriteString(startCode)
				}
				i = j
			} else {
				buf.WriteRune(r)
			}
		} else {
			buf.WriteRune(r)
		}
	}
	return buf.String()
}

// styleURLs finds URLs in a string and colors them in Cyan/Blue with underline.
// It restores the correct style at the end of each URL depending on whether the line is dimmed.
func styleURLs(line string, isDimmed bool) string {
	urlRx := regexp.MustCompile(`https?://[^\s<>"\x1b\x01\x02]+`)

	// Start code: Cyan foreground (#89B4FA) and Underline (4)
	startCode := "\x1b[38;2;137;180;250;4m"

	// End code: Turn off underline (24), and restore correct color
	var endCode string
	if isDimmed {
		// Restore subtext/dim color (#A6ADC8)
		endCode = "\x1b[24;38;2;166;173;200m"
	} else {
		// Revert to default foreground
		endCode = "\x1b[24;39m"
	}

	return urlRx.ReplaceAllStringFunc(line, func(match string) string {
		trimmed := match
		var trailing string
		for len(trimmed) > 0 {
			last := trimmed[len(trimmed)-1]
			if last == '.' || last == ',' || last == ')' || last == ']' || last == '}' || last == '!' || last == '?' || last == ':' || last == ';' {
				// Special check: if it's a closing parenthesis, only trim it if there is no matching opening parenthesis in the URL.
				if last == ')' && strings.Count(trimmed, "(") > strings.Count(trimmed, ")")-1 {
					break
				}
				if last == ']' && strings.Count(trimmed, "[") > strings.Count(trimmed, "]")-1 {
					break
				}
				if last == '}' && strings.Count(trimmed, "{") > strings.Count(trimmed, "}")-1 {
					break
				}
				trailing = string(last) + trailing
				trimmed = trimmed[:len(trimmed)-1]
			} else {
				break
			}
		}
		return startCode + trimmed + endCode + trailing
	})
}

// isOriginalMessageStart returns true if the plain text line indicates the start of an original or forwarded email block.
func isOriginalMessageStart(line string) bool {
	trimmed := strings.TrimSpace(line)
	lower := strings.ToLower(trimmed)

	// Check for common headers with colons
	if strings.HasPrefix(lower, "from:") ||
		strings.HasPrefix(lower, "von:") ||
		strings.HasPrefix(lower, "de:") {
		return true
	}

	// Check for common email thread split markers
	if strings.Contains(lower, "original message") ||
		strings.Contains(lower, "forwarded message") {
		return true
	}

	// Check for line divider (typically used by Outlook web/desktop)
	if strings.HasPrefix(trimmed, "________________________________") {
		return true
	}

	// Check for "On ... wrote:" pattern
	if strings.HasPrefix(lower, "on ") && strings.HasSuffix(lower, "wrote:") {
		return true
	}

	return false
}

// stripANSICodes removes ANSI escape sequences from a string
func stripANSICodes(s string) string {
	re := regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)
	s = re.ReplaceAllString(s, "")
	s = strings.ReplaceAll(s, "\x01", "")
	s = strings.ReplaceAll(s, "\x02", "")
	return s
}

func sortFolders(folders []MailFolder, excluded []string, db *DB) []MailFolder {
	var inbox *MailFolder
	var sentItems *MailFolder
	var others []MailFolder

	for _, f := range folders {
		if isExcluded(f, excluded) {
			continue
		}
		lowerName := strings.ToLower(f.DisplayName)
		lowerWellKnown := strings.ToLower(f.WellKnownName)
		if lowerWellKnown == "inbox" || lowerName == "inbox" {
			fCopy := f
			inbox = &fCopy
		} else if lowerWellKnown == "sentitems" || lowerName == "sent items" || lowerName == "sentitems" || lowerName == "sent" {
			fCopy := f
			sentItems = &fCopy
		} else {
			others = append(others, f)
		}
	}

	// Always create Favorites on top
	favFolder := MailFolder{
		ID:          "favorites",
		DisplayName: "Favorites",
	}
	if db != nil {
		unread, total, err := db.GetFavoritesCounts()
		if err == nil {
			favFolder.UnreadItemCount = unread
			favFolder.TotalItemCount = total
		}
	}

	result := make([]MailFolder, 0, len(folders)+1)
	result = append(result, favFolder)

	if inbox != nil {
		result = append(result, *inbox)
	}
	if sentItems != nil {
		result = append(result, *sentItems)
	}
	result = append(result, others...)
	return result
}

func isExcluded(folder MailFolder, excluded []string) bool {
	for _, excl := range excluded {
		exclLower := strings.ToLower(strings.TrimSpace(excl))
		if exclLower == "" {
			continue
		}
		if strings.ToLower(folder.DisplayName) == exclLower || strings.ToLower(folder.WellKnownName) == exclLower {
			return true
		}
	}
	return false
}

func wrapText(text string, width int) string {
	if width <= 0 {
		return text
	}
	wrapped := lipgloss.NewStyle().Width(width).Render(text)
	lines := strings.Split(wrapped, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " ")
	}
	return strings.Join(lines, "\n")
}

func applyPaneTitle(rendered string, title string, active bool) string {
	lines := strings.Split(rendered, "\n")
	if len(lines) == 0 {
		return rendered
	}
	// Get visual width of the first line (the top border)
	width := lipgloss.Width(lines[0])
	if width <= 0 {
		return rendered
	}
	lines[0] = renderTopBorderWithTitle(width, title, active)
	return strings.Join(lines, "\n")
}

func renderTopBorderWithTitle(width int, title string, active bool) string {
	topLeft := "┌"
	topRight := "┐"
	horiz := "─"

	borderColor := ColorOverlay
	if active {
		borderColor = ColorViolet
	}
	borderLipglossStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(borderColor))

	var titleStyle lipgloss.Style
	if active {
		titleStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(ColorViolet))
	} else {
		titleStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(ColorSubtext))
	}

	titleLen := len(title)
	if width < titleLen+6 {
		if width < 6 {
			if width > 0 {
				return borderLipglossStyle.Render(topLeft + strings.Repeat(horiz, max(0, width-2)) + topRight)
			}
			return ""
		}
		allowedTitleLen := width - 6
		if allowedTitleLen > 2 {
			title = title[:allowedTitleLen-2] + ".."
		} else {
			title = ""
		}
		titleLen = len(title)
	}

	var leftPart, middleText, rightPart string
	leftPart = borderLipglossStyle.Render(topLeft+horiz) + " "
	if title != "" {
		middleText = titleStyle.Render(title)
	}
	rightDashesCount := width - 3 - titleLen - 1 - 1 // 3 for leftPart, titleLen for middle, 1 for space, 1 for topRight
	if title == "" {
		rightDashesCount = width - 2 // just the corners and dashes
		return borderLipglossStyle.Render(topLeft + strings.Repeat(horiz, rightDashesCount) + topRight)
	}
	if rightDashesCount < 1 {
		rightDashesCount = 1
	}
	rightPart = " " + borderLipglossStyle.Render(strings.Repeat(horiz, rightDashesCount)+topRight)

	return leftPart + middleText + rightPart
}

// isForwarded returns true if the email is a forwarded message.
func isForwarded(subject string, htmlContent string) bool {
	s := strings.TrimSpace(subject)
	lowerSubj := strings.ToLower(s)

	// If subject starts with reply prefix, it's a reply, not a forward.
	// E.g., "Re: Fwd: ..." is a reply.
	if strings.HasPrefix(lowerSubj, "re:") || strings.HasPrefix(lowerSubj, "aw:") || strings.HasPrefix(lowerSubj, "antw:") {
		return false
	}

	// Common forward prefixes in various languages (case-insensitive)
	forwardPrefixes := []string{"fwd:", "fw:", "wg:", "tr:", "rv:", "pd:", "fv:", "vs:"}
	for _, pref := range forwardPrefixes {
		if strings.HasPrefix(lowerSubj, pref) {
			return true
		}
	}

	// Fallback to body content check: if subject doesn't explicitly start with a reply prefix
	// and the body contains forwarded message headers or markers, we consider it forwarded.
	lowerBody := strings.ToLower(htmlContent)
	if strings.Contains(lowerBody, "forwarded message") ||
		strings.Contains(lowerBody, "---------- forwarded") ||
		strings.Contains(lowerBody, "__________ forwarded") {
		return true
	}

	return false
}

// extractURLs returns a list of unique URLs found in the body content.
// If allBody is true, it extracts from the entire body; otherwise it ignores
// original message blocks/quoted blocks.
func extractURLs(htmlContent string, allBody bool) []string {
	// 1. Replace anchor tags to make href values visible.
	res := replaceAnchorTags(htmlContent, false)

	// 2. Convert HTML line breaks to real newlines to preserve message structure.
	res = regexp.MustCompile(`(?i)<br(?:\s*\/)?>`).ReplaceAllString(res, "\n")
	res = regexp.MustCompile(`(?i)<p(?:\s+[^>]*)?>`).ReplaceAllString(res, "\n\n")
	res = strings.ReplaceAll(res, "</p>", "\n\n")
	res = regexp.MustCompile(`(?i)<div(?:\s+[^>]*)?>`).ReplaceAllString(res, "\n")
	res = strings.ReplaceAll(res, "</div>", "\n")
	res = regexp.MustCompile(`(?i)<li(?:\s+[^>]*)?>`).ReplaceAllString(res, "\n• ")
	res = regexp.MustCompile(`(?i)<h[1-6](?:\s+[^>]*)?>`).ReplaceAllString(res, "\n\n")
	res = regexp.MustCompile(`(?i)</h[1-6]>`).ReplaceAllString(res, "\n\n")
	res = regexp.MustCompile(`(?i)<tr(?:\s+[^>]*)?>`).ReplaceAllString(res, "\n")
	res = regexp.MustCompile(`(?i)<td(?:\s+[^>]*)?>`).ReplaceAllString(res, " ")

	// 3. Strip other HTML tags.
	var builder strings.Builder
	inTag := false
	for _, r := range res {
		if r == '<' {
			inTag = true
			continue
		}
		if r == '>' {
			if inTag {
				inTag = false
				continue
			}
		}
		if !inTag {
			builder.WriteRune(r)
		}
	}

	unescaped := html.UnescapeString(builder.String())
	unescaped = strings.ReplaceAll(unescaped, "\u00a0", " ")

	lines := strings.Split(unescaped, "\n")
	var urls []string
	seen := make(map[string]bool)

	urlRx := regexp.MustCompile(`https?://[^\s<>"\x1b]+`)
	inOriginal := false

	for _, l := range lines {
		trimmed := strings.TrimSpace(l)
		if trimmed == "" {
			continue
		}

		plainLine := stripANSICodes(l)
		trimmedPlain := strings.TrimSpace(plainLine)

		if !allBody {
			if !inOriginal && isOriginalMessageStart(trimmedPlain) {
				inOriginal = true
			}

			if inOriginal || strings.HasPrefix(trimmedPlain, ">") {
				continue
			}
		}

		// Find URLs in the plain line
		matches := urlRx.FindAllString(trimmedPlain, -1)
		for _, m := range matches {
			url := m
			// Trim trailing punctuation like styleURLs does
			for len(url) > 0 {
				last := url[len(url)-1]
				if last == '.' || last == ',' || last == ')' || last == ']' || last == '}' || last == '!' || last == '?' || last == ':' || last == ';' {
					if last == ')' && strings.Count(url, "(") > strings.Count(url, ")")-1 {
						break
					}
					if last == ']' && strings.Count(url, "[") > strings.Count(url, "]")-1 {
						break
					}
					if last == '}' && strings.Count(url, "{") > strings.Count(url, "}")-1 {
						break
					}
					url = url[:len(url)-1]
				} else {
					break
				}
			}
			if url != "" && !seen[url] {
				seen[url] = true
				urls = append(urls, url)
			}
		}
	}
	return urls
}

// extractURLsFromMainMessage returns a list of unique URLs found in the body content,
// excluding any URLs in quoted lines or block dividers/original message blocks,
// unless it is detected as a forwarded message.
func extractURLsFromMainMessage(htmlContent string, subject string) []string {
	allBody := isForwarded(subject, htmlContent)
	return extractURLs(htmlContent, allBody)
}

func cropLines(s string, maxLines int) string {
	if maxLines <= 0 {
		return ""
	}
	s = strings.ReplaceAll(s, "\r", "")
	s = strings.TrimSuffix(s, "\n")
	lines := strings.Split(s, "\n")
	if len(lines) > maxLines {
		return strings.Join(lines[:maxLines], "\n")
	}
	return strings.Join(lines, "\n")
}

func extractYouTrackURLs(htmlContent string, subject string) []string {
	allURLs := extractURLsFromMainMessage(htmlContent, subject)
	var ytURLs []string
	seen := make(map[string]bool)
	issueRx := regexp.MustCompile(`(?i)/(?:issue|projects/[^/]+/issues)/([a-zA-Z0-9]+-[0-9]+)`)
	for _, uStr := range allURLs {
		parsed, err := url.Parse(uStr)
		if err != nil {
			continue
		}
		host := strings.ToLower(parsed.Host)
		if !strings.Contains(host, "youtrack") {
			continue
		}
		matches := issueRx.FindStringSubmatch(parsed.Path)
		if len(matches) < 2 {
			continue
		}
		issueID := matches[1]
		normalized := fmt.Sprintf("%s://%s/issue/%s", parsed.Scheme, parsed.Host, issueID)
		if !seen[normalized] {
			seen[normalized] = true
			ytURLs = append(ytURLs, normalized)
		}
	}
	return ytURLs
}

func extractGitLabURLs(htmlContent string, subject string) []string {
	allURLs := extractURLsFromMainMessage(htmlContent, subject)
	var gitlabURLs []string
	seen := make(map[string]bool)
	mrRx := regexp.MustCompile(`(?i)https?://[^/]+/(.+?)/(?:-/)?merge_requests/([0-9]+)`)
	pipeRx := regexp.MustCompile(`(?i)https?://[^/]+/(.+?)/(?:-/)?pipelines/([0-9]+)`)
	jobRx := regexp.MustCompile(`(?i)https?://[^/]+/(.+?)/(?:-/)?jobs/([0-9]+)`)
	for _, uStr := range allURLs {
		parsed, err := url.Parse(uStr)
		if err != nil {
			continue
		}
		if matches := mrRx.FindStringSubmatch(uStr); len(matches) >= 3 {
			projectPath := matches[1]
			mrNum := matches[2]
			normalized := fmt.Sprintf("%s://%s/%s/-/merge_requests/%s", parsed.Scheme, parsed.Host, projectPath, mrNum)
			if !seen[normalized] {
				seen[normalized] = true
				gitlabURLs = append(gitlabURLs, normalized)
			}
		} else if matches := pipeRx.FindStringSubmatch(uStr); len(matches) >= 3 {
			projectPath := matches[1]
			pipeNum := matches[2]
			normalized := fmt.Sprintf("%s://%s/%s/-/pipelines/%s", parsed.Scheme, parsed.Host, projectPath, pipeNum)
			if !seen[normalized] {
				seen[normalized] = true
				gitlabURLs = append(gitlabURLs, normalized)
			}
		} else if matches := jobRx.FindStringSubmatch(uStr); len(matches) >= 3 {
			projectPath := matches[1]
			jobNum := matches[2]
			normalized := fmt.Sprintf("%s://%s/%s/-/jobs/%s", parsed.Scheme, parsed.Host, projectPath, jobNum)
			if !seen[normalized] {
				seen[normalized] = true
				gitlabURLs = append(gitlabURLs, normalized)
			}
		}
	}
	return gitlabURLs
}

func classifyURL(uStr string) (string, string) {
	parsed, err := url.Parse(uStr)
	if err != nil {
		return "normal", uStr
	}

	// 1. Check GitLab
	mrRx := regexp.MustCompile(`(?i)https?://[^/]+/(.+?)/(?:-/)?merge_requests/([0-9]+)`)
	pipeRx := regexp.MustCompile(`(?i)https?://[^/]+/(.+?)/(?:-/)?pipelines/([0-9]+)`)
	jobRx := regexp.MustCompile(`(?i)https?://[^/]+/(.+?)/(?:-/)?jobs/([0-9]+)`)

	if matches := mrRx.FindStringSubmatch(uStr); len(matches) >= 3 {
		projectPath := matches[1]
		mrNum := matches[2]
		normalized := fmt.Sprintf("%s://%s/%s/-/merge_requests/%s", parsed.Scheme, parsed.Host, projectPath, mrNum)
		return "gitlab", normalized
	}
	if matches := pipeRx.FindStringSubmatch(uStr); len(matches) >= 3 {
		projectPath := matches[1]
		pipeNum := matches[2]
		normalized := fmt.Sprintf("%s://%s/%s/-/pipelines/%s", parsed.Scheme, parsed.Host, projectPath, pipeNum)
		return "gitlab", normalized
	}
	if matches := jobRx.FindStringSubmatch(uStr); len(matches) >= 3 {
		projectPath := matches[1]
		jobNum := matches[2]
		normalized := fmt.Sprintf("%s://%s/%s/-/jobs/%s", parsed.Scheme, parsed.Host, projectPath, jobNum)
		return "gitlab", normalized
	}

	// 2. Check YouTrack
	host := strings.ToLower(parsed.Host)
	if strings.Contains(host, "youtrack") {
		issueRx := regexp.MustCompile(`(?i)/(?:issue|projects/[^/]+/issues)/([a-zA-Z0-9]+-[0-9]+)`)
		matches := issueRx.FindStringSubmatch(parsed.Path)
		if len(matches) >= 2 {
			issueID := matches[1]
			normalized := fmt.Sprintf("%s://%s/issue/%s", parsed.Scheme, parsed.Host, issueID)
			return "youtrack", normalized
		}
	}

	return "normal", uStr
}

func extractAllURLsForOpen(htmlContent string, subject string) []string {
	allURLs := extractURLsFromMainMessage(htmlContent, subject)
	var recognized []string
	var others []string
	seen := make(map[string]bool)

	for _, uStr := range allURLs {
		urlType, normalized := classifyURL(uStr)
		if !seen[normalized] {
			seen[normalized] = true
			if urlType == "gitlab" || urlType == "youtrack" {
				recognized = append(recognized, normalized)
			} else {
				others = append(others, normalized)
			}
		}
	}
	return append(recognized, others...)
}

func (m mainModel) renderExternalURLDropdown(width int) string {
	dropdownWidth := width - 4
	if dropdownWidth < 20 {
		dropdownWidth = 20
	}

	var rows []string
	rows = append(rows, lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(ColorViolet)).Render(" SELECT LINK TO OPEN: "))

	maxVisible := 10
	totalURLs := len(m.extractedURLs)

	startIdx := 0
	endIdx := totalURLs

	if totalURLs > maxVisible {
		startIdx = m.selectedURLIdx - maxVisible/2
		if startIdx < 0 {
			startIdx = 0
		}
		endIdx = startIdx + maxVisible
		if endIdx > totalURLs {
			endIdx = totalURLs
			startIdx = endIdx - maxVisible
		}
	}

	if startIdx > 0 {
		rows = append(rows, lipgloss.NewStyle().Foreground(lipgloss.Color(ColorOverlay)).Render("  ▲ ... more URLs above ..."))
	}

	for i := startIdx; i < endIdx; i++ {
		urlStr := m.extractedURLs[i]
		indicator := "  "
		if i == m.selectedURLIdx {
			indicator = "> "
		}

		urlType, _ := classifyURL(urlStr)
		prefix := "[Link]     "
		if urlType == "gitlab" {
			prefix = "[GitLab]   "
		} else if urlType == "youtrack" {
			prefix = "[YouTrack] "
		}

		line := fmt.Sprintf("%s%s%s", indicator, prefix, urlStr)

		// Pad/crop line
		if len(line) < dropdownWidth-2 {
			line = line + strings.Repeat(" ", dropdownWidth-2-len(line))
		} else if len(line) > dropdownWidth-2 {
			line = line[:dropdownWidth-5] + "..."
		}

		if i == m.selectedURLIdx {
			line = lipgloss.NewStyle().
				Foreground(lipgloss.Color(ColorBg)).
				Background(lipgloss.Color(ColorViolet)).
				Bold(true).
				Render(line)
		} else {
			line = lipgloss.NewStyle().
				Foreground(lipgloss.Color(ColorText)).
				Render(line)
		}
		rows = append(rows, line)
	}

	if endIdx < totalURLs {
		rows = append(rows, lipgloss.NewStyle().Foreground(lipgloss.Color(ColorOverlay)).Render("  ▼ ... more URLs below ..."))
	}

	joined := strings.Join(rows, "\n")

	popupStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(ColorViolet)).
		Padding(0, 1)

	return popupStyle.Render(joined)
}

var cssColors = map[string][3]int{
	"black":                {0, 0, 0},
	"silver":               {192, 192, 192},
	"gray":                 {128, 128, 128},
	"grey":                 {128, 128, 128},
	"white":                {255, 255, 255},
	"maroon":               {128, 0, 0},
	"red":                  {255, 0, 0},
	"purple":               {128, 0, 128},
	"fuchsia":              {255, 0, 255},
	"green":                {0, 128, 0},
	"lime":                 {0, 255, 0},
	"olive":                {128, 128, 0},
	"yellow":               {255, 255, 0},
	"navy":                 {0, 0, 128},
	"blue":                 {0, 0, 255},
	"teal":                 {0, 128, 128},
	"aqua":                 {0, 255, 255},
	"orange":               {255, 165, 0},
	"aliceblue":            {240, 248, 255},
	"antiquewhite":         {250, 235, 215},
	"aquamarine":           {127, 255, 212},
	"azure":                {240, 255, 255},
	"beige":                {245, 245, 220},
	"bisque":               {255, 228, 196},
	"blanchedalmond":       {255, 235, 205},
	"blueviolet":           {138, 43, 226},
	"brown":                {165, 42, 42},
	"burlywood":            {222, 184, 135},
	"cadetblue":            {95, 158, 160},
	"chartreuse":           {127, 255, 0},
	"chocolate":            {210, 105, 30},
	"coral":                {255, 127, 80},
	"cornflowerblue":       {100, 149, 237},
	"cornsilk":             {255, 248, 220},
	"crimson":              {220, 20, 60},
	"cyan":                 {0, 255, 255},
	"darkblue":             {0, 0, 139},
	"darkcyan":             {0, 139, 139},
	"darkgoldenrod":        {184, 134, 11},
	"darkgray":             {169, 169, 169},
	"darkgreen":            {0, 100, 0},
	"darkgrey":             {169, 169, 169},
	"darkkhaki":            {189, 183, 107},
	"darkmagenta":          {139, 0, 139},
	"darkolivegreen":       {85, 107, 47},
	"darkorange":           {255, 140, 0},
	"darkorchid":           {153, 50, 204},
	"darkred":              {139, 0, 0},
	"darksalmon":           {233, 150, 122},
	"darkseagreen":         {143, 188, 143},
	"darkslateanchor":      {72, 61, 139},
	"darkslateblue":        {72, 61, 139},
	"darkslategray":        {47, 79, 79},
	"darkslategrey":        {47, 79, 79},
	"darkturquoise":        {0, 206, 209},
	"darkviolet":           {94, 0, 211},
	"deeppink":             {255, 20, 147},
	"deepskyblue":          {0, 191, 255},
	"dimgray":              {105, 105, 105},
	"dimgrey":              {105, 105, 105},
	"dodgerblue":           {30, 144, 255},
	"firebrick":            {178, 34, 34},
	"floralwhite":          {255, 250, 240},
	"forestgreen":          {34, 139, 34},
	"gainsboro":            {220, 220, 220},
	"ghostwhite":           {248, 248, 255},
	"gold":                 {255, 215, 0},
	"goldenrod":            {218, 165, 32},
	"greenyellow":          {173, 255, 47},
	"honeydew":             {240, 255, 240},
	"hotpink":              {255, 105, 180},
	"indianred":            {205, 92, 92},
	"indigo":               {75, 0, 130},
	"ivory":                {255, 255, 240},
	"khaki":                {240, 230, 140},
	"lavender":             {230, 230, 250},
	"lavenderblush":        {255, 240, 245},
	"lawngreen":            {124, 252, 0},
	"lemonchiffon":         {255, 250, 205},
	"lightblue":            {173, 216, 230},
	"lightcoral":           {240, 128, 128},
	"lightcyan":            {224, 255, 255},
	"lightgoldenrodyellow": {250, 250, 210},
	"lightgray":            {211, 211, 211},
	"lightgreen":           {144, 238, 144},
	"lightgrey":            {211, 211, 211},
	"lightpink":            {255, 182, 193},
	"lightsalmon":          {255, 160, 122},
	"lightseagreen":        {32, 178, 170},
	"lightskyblue":         {135, 206, 250},
	"lightslategray":       {119, 136, 153},
	"lightslategrey":       {119, 136, 153},
	"lightsteelblue":       {176, 196, 222},
	"lightyellow":          {255, 255, 224},
	"limegreen":            {50, 205, 50},
	"linen":                {250, 240, 230},
	"magenta":              {255, 0, 255},
	"mediumaquamarine":     {102, 205, 170},
	"mediumblue":           {0, 0, 205},
	"mediumorchid":         {186, 85, 211},
	"mediumpurple":         {147, 112, 219},
	"mediumseagreen":       {60, 179, 113},
	"mediumslateanchor":    {123, 104, 238},
	"mediumslateblue":      {123, 104, 238},
	"mediumspringgreen":    {0, 250, 154},
	"mediumturquoise":      {72, 209, 204},
	"mediumvioletred":      {199, 21, 133},
	"midnightblue":         {25, 25, 112},
	"mintcream":            {245, 255, 250},
	"mistyrose":            {255, 228, 225},
	"moccasin":             {255, 228, 181},
	"navajowhite":          {255, 222, 173},
	"oldlace":              {253, 245, 230},
	"olivedrab":            {107, 142, 35},
	"orangered":            {255, 69, 0},
	"orchid":               {218, 112, 214},
	"palegoldenrod":        {238, 232, 170},
	"palegreen":            {152, 251, 152},
	"paleturquoise":        {175, 238, 238},
	"palevioletred":        {219, 112, 147},
	"papayawhip":           {255, 239, 213},
	"peachpuff":            {255, 218, 185},
	"peru":                 {205, 133, 63},
	"pink":                 {255, 192, 203},
	"plum":                 {221, 160, 221},
	"powderblue":           {176, 224, 230},
	"rosybrown":            {188, 143, 143},
	"royalblue":            {65, 105, 225},
	"saddlebrown":          {139, 69, 19},
	"salmon":               {250, 128, 114},
	"sandybrown":           {244, 164, 96},
	"seagreen":             {46, 139, 87},
	"seashell":             {255, 245, 238},
	"sienna":               {160, 82, 45},
	"skyblue":              {135, 206, 235},
	"slateanchor":          {106, 90, 205},
	"slateblue":            {106, 90, 205},
	"slategray":            {112, 128, 144},
	"slategrey":            {112, 128, 144},
	"snow":                 {255, 250, 250},
	"springgreen":          {0, 255, 127},
	"steelblue":            {70, 130, 180},
	"tan":                  {210, 180, 140},
	"thistle":              {216, 191, 216},
	"tomato":               {255, 99, 71},
	"turquoise":            {64, 224, 208},
	"violet":               {238, 130, 238},
	"wheat":                {245, 222, 179},
	"whitesmoke":           {245, 245, 245},
	"yellowgreen":          {154, 205, 50},
}

func parseHexColor(s string) (r, g, b int, ok bool) {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "#") {
		return 0, 0, 0, false
	}
	s = s[1:]
	if len(s) == 3 {
		rVal, err1 := strconv.ParseInt(string([]byte{s[0], s[0]}), 16, 32)
		gVal, err2 := strconv.ParseInt(string([]byte{s[1], s[1]}), 16, 32)
		bVal, err3 := strconv.ParseInt(string([]byte{s[2], s[2]}), 16, 32)
		if err1 == nil && err2 == nil && err3 == nil {
			return int(rVal), int(gVal), int(bVal), true
		}
	} else if len(s) == 6 {
		rVal, err1 := strconv.ParseInt(s[0:2], 16, 32)
		gVal, err2 := strconv.ParseInt(s[2:4], 16, 32)
		bVal, err3 := strconv.ParseInt(s[4:6], 16, 32)
		if err1 == nil && err2 == nil && err3 == nil {
			return int(rVal), int(gVal), int(bVal), true
		}
	}
	return 0, 0, 0, false
}

func cssColorToRGB(val string) (int, int, int, bool) {
	val = strings.TrimSpace(strings.ToLower(val))
	if val == "" {
		return 0, 0, 0, false
	}
	if strings.HasPrefix(val, "#") {
		return parseHexColor(val)
	}
	rgbRx := regexp.MustCompile(`^rgba?\s*\(\s*(\d+)\s*,\s*(\d+)\s*,\s*(\d+)(?:\s*,|\s*\))`)
	matches := rgbRx.FindStringSubmatch(val)
	if len(matches) >= 4 {
		r, _ := strconv.Atoi(matches[1])
		g, _ := strconv.Atoi(matches[2])
		b, _ := strconv.Atoi(matches[3])
		return r, g, b, true
	}
	if rgb, ok := cssColors[val]; ok {
		return rgb[0], rgb[1], rgb[2], true
	}
	return 0, 0, 0, false
}

func parseStyleAttr(styleStr string) (fg, bg string) {
	parts := strings.Split(styleStr, ";")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		kv := strings.SplitN(part, ":", 2)
		if len(kv) != 2 {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(kv[0]))
		val := strings.ToLower(strings.TrimSpace(kv[1]))
		val = strings.TrimSuffix(val, "!important")
		val = strings.TrimSpace(val)

		if key == "color" {
			fg = val
		} else if key == "background-color" || key == "background" {
			bg = val
		}
	}
	return fg, bg
}

func isThemeDark() bool {
	if r, g, b, ok := parseHexColor(ColorBg); ok {
		lum := 0.299*float64(r) + 0.587*float64(g) + 0.114*float64(b)
		return lum < 128
	}
	return true // default to dark
}

func convertInlineStylesToANSI(htmlContent string) string {
	var result strings.Builder
	pos := 0

	type styledTag struct {
		name string
	}
	var styledStack []styledTag

	tagRx := regexp.MustCompile(`(?i)</?([a-zA-Z0-9]+)\b([^>]*)>`)
	matches := tagRx.FindAllStringSubmatchIndex(htmlContent, -1)

	darkTheme := isThemeDark()

	for _, m := range matches {
		startIdx := m[0]
		endIdx := m[1]

		result.WriteString(htmlContent[pos:startIdx])
		pos = endIdx

		tagText := htmlContent[startIdx:endIdx]
		isClosing := strings.HasPrefix(tagText, "</")
		tagName := strings.ToLower(htmlContent[m[2]:m[3]])

		if isClosing {
			isStyledClose := false
			if len(styledStack) > 0 && styledStack[len(styledStack)-1].name == tagName {
				isStyledClose = true
				styledStack = styledStack[:len(styledStack)-1]
			} else {
				for i := len(styledStack) - 1; i >= 0; i-- {
					if styledStack[i].name == tagName {
						isStyledClose = true
						styledStack = styledStack[:i]
						break
					}
				}
			}

			if isStyledClose {
				result.WriteString("\x1b[39m" + tagText)
			} else {
				result.WriteString(tagText)
			}
		} else {
			attrs := htmlContent[m[4]:m[5]]
			var fgColor, bgColor string

			styleRx := regexp.MustCompile(`(?i)\bstyle\s*=\s*['"]([^'"]+)['"]`)
			styleMatches := styleRx.FindStringSubmatch(attrs)
			if len(styleMatches) > 1 {
				fgColor, bgColor = parseStyleAttr(styleMatches[1])
			}

			if fgColor == "" {
				colorRx := regexp.MustCompile(`(?i)\bcolor\s*=\s*['"]([^'"]+)['"]`)
				colorMatches := colorRx.FindStringSubmatch(attrs)
				if len(colorMatches) > 1 {
					fgColor = colorMatches[1]
				}
			}

			if bgColor == "" {
				bgcolorRx := regexp.MustCompile(`(?i)\bbgcolor\s*=\s*['"]([^'"]+)['"]`)
				bgcolorMatches := bgcolorRx.FindStringSubmatch(attrs)
				if len(bgcolorMatches) > 1 {
					bgColor = bgcolorMatches[1]
				}
			}

			// If background color is specified but foreground is not, map background to foreground highlight
			if fgColor == "" && bgColor != "" {
				fgColor = bgColor
			}

			hasStyle := false
			var ansiCodes strings.Builder
			if fgColor != "" {
				if r, g, b, ok := cssColorToRGB(fgColor); ok {
					keepColor := true
					if darkTheme {
						// On dark themes, ignore colors that are too dark (dark gray, black, etc.)
						if r < 110 && g < 110 && b < 110 {
							keepColor = false
						}
					} else {
						// On light themes, ignore colors that are too light (white, very light yellow, etc.)
						if r > 150 && g > 150 && b > 150 {
							keepColor = false
						}
					}

					if keepColor {
						ansiCodes.WriteString(fmt.Sprintf("\x1b[38;2;%d;%d;%dm", r, g, b))
						hasStyle = true
					}
				}
			}

			if hasStyle {
				styledStack = append(styledStack, styledTag{name: tagName})
				result.WriteString(ansiCodes.String() + tagText)
			} else {
				result.WriteString(tagText)
			}
		}
	}

	if pos < len(htmlContent) {
		result.WriteString(htmlContent[pos:])
	}

	return result.String()
}

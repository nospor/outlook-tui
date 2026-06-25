package main

import (
	"encoding/base64"
	"fmt"
	"html"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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
	ThreadIdx  int  // index into m.threadGroups
	MemberIdx  int  // -1 = header row; >=0 = member inside the thread
	IsHeader   bool
}

// Messages
type (
	errMsg                error
	foldersFetchedMsg     []MailFolder
	messagesFetchedMsg    struct {
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
	tokenFetchedMsg     TokenCache
	deviceCodeMsg       *DeviceCodeResponse
	statusUpdateMsg     string
	mailSentMsg         struct{}
	mailDeletedMsg      struct{ MessageID string }
	attachmentSavedMsg  string
	userEmailFetchedMsg string
)

type mainModel struct {
	state       appState
	activePane  pane
	width, height int

	// Config state
	config       Config
	configStep   int // 0 = Client ID, 1 = Tenant ID
	txtInput     textinput.Model
	statusMsg    string

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
	threadGroups    []ThreadGroup
	collapsedThreads map[string]bool // keyed by ConversationID; true = collapsed
	virtualList     []MessageListItem // flat navigable list
	virtualSelected int               // index into virtualList

	// Sub-components
	detailViewport viewport.Model
	spinner        spinner.Model

	// Compose state
	composeTo       textinput.Model
	composeSubject  textinput.Model
	composeBody     textarea.Model
	composeStep     int // 0 = To, 1 = Subject, 2 = Body

	// Notification tracking
	inboxKnownIDs map[string]bool
	userEmail     string

	// SQLite cache (nil when use_sqlite == 0)
	db *DB
}

func initialModel() mainModel {
	ti := textinput.New()
	ti.Placeholder = "Enter Microsoft Client ID..."
	ti.Focus()
	ti.CharLimit = 150
	ti.Width = 50

	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color(ColorViolet))

	return mainModel{
		state:      stateLoading,
		txtInput:   ti,
		spinner:    s,
		configStep: 0,
	}
}

func (m mainModel) Init() tea.Cmd {
	return tea.Batch(m.spinner.Tick, checkConfigCmd())
}

// Commands
func checkConfigCmd() tea.Cmd {
	return func() tea.Msg {
		cfg, err := LoadConfig()
		if err != nil {
			return errMsg(err)
		}
		if cfg.ClientID == "" {
			return statusUpdateMsg("config_needed")
		}

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

func fetchInboxMessagesCmd(gc *GraphClient) tea.Cmd {
	return func() tea.Msg {
		msgs, err := gc.GetMessages("inbox")
		if err != nil {
			return nil // ignore background error
		}
		return inboxMessagesFetchedMsg{Messages: msgs}
	}
}

func fetchMessageDetailCmd(gc *GraphClient, msgID string) tea.Cmd {
	return func() tea.Msg {
		msg, err := gc.GetMessage(msgID)
		if err != nil {
			return errMsg(err)
		}
		
		// If message has attachments, fetch them
		var atts []Attachment
		if msg.HasAttachments {
			atts, _ = gc.GetAttachments(msgID)
		}

		// Also mark message as read if unread
		if !msg.IsRead {
			_ = gc.MarkAsRead(msgID, true)
		}

		return messageDetailFetchedMsg{Message: msg, Attachments: atts}
	}
}

func sendMailCmd(gc *GraphClient, to, subject, body string) tea.Cmd {
	return func() tea.Msg {
		err := gc.SendMessage(subject, body, to)
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

func saveAttachmentCmd(att Attachment) tea.Cmd {
	return func() tea.Msg {
		data, err := base64.StdEncoding.DecodeString(att.ContentBytes)
		if err != nil {
			return errMsg(fmt.Errorf("failed to decode attachment: %w", err))
		}
		
		home, err := os.UserHomeDir()
		var downloadDir string
		if err == nil {
			downloadDir = filepath.Join(home, "Downloads")
			// Fallback to home or current dir if Downloads folder doesn't exist
			if _, err := os.Stat(downloadDir); os.IsNotExist(err) {
				downloadDir = home
			}
		} else {
			downloadDir = "."
		}
		
		path := filepath.Join(downloadDir, att.Name)
		// Ensure unique filename if exists
		ext := filepath.Ext(att.Name)
		base := strings.TrimSuffix(att.Name, ext)
		counter := 1
		for {
			if _, err := os.Stat(path); os.IsNotExist(err) {
				break
			}
			path = filepath.Join(downloadDir, fmt.Sprintf("%s (%d)%s", base, counter, ext))
			counter++
		}
		
		if err := os.WriteFile(path, data, 0644); err != nil {
			return errMsg(fmt.Errorf("failed to write attachment file: %w", err))
		}
		
		// Open the file with xdg-open in the background
		_ = exec.Command("xdg-open", path).Start()
		
		return attachmentSavedMsg(path)
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
	cached, err := m.db.GetMessages(folderID)
	if err == nil && len(cached) > 0 {
		m.messages = cached
		m.buildThreadGroups()
		return m, fmt.Sprintf("Showing %d cached messages, refreshing...", len(cached))
	}
	return m, fmt.Sprintf("Loading messages for %s...", m.folders[m.selectedFolder].DisplayName)
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
		m.attachments = nil
		m = m.updateViewportSize()
		m.detailViewport.SetContent(wrapText(formatBodyContent(am.Body.Content), m.detailViewport.Width))
		m.detailViewport.GotoTop()

		if am.IsRead {
			m.statusMsg = "Message details loaded"
			return m, nil
		}
		// If unread, fetch from Graph to mark read
		m.statusMsg = "Marking read..."
		return m, fetchMessageDetailCmd(m.graphClient, am.ID)
	}

	// 2. Check if the body content is cached in SQLite
	if m.db != nil {
		if cached, err := m.db.GetMessage(am.ID); err == nil && cached != nil && cached.Body.Content != "" {
			m.detailMessage = cached
			m.attachments = nil
			m = m.updateViewportSize()
			m.detailViewport.SetContent(wrapText(formatBodyContent(cached.Body.Content), m.detailViewport.Width))
			m.detailViewport.GotoTop()

			// Update in-memory collections so they have the loaded body too
			for i, msg := range m.messages {
				if msg.ID == am.ID {
					m.messages[i].Body = cached.Body
					break
				}
			}
			for ti := range m.threadGroups {
				for mi := range m.threadGroups[ti].Members {
					if m.threadGroups[ti].Members[mi].ID == am.ID {
						m.threadGroups[ti].Members[mi].Body = cached.Body
						break
					}
				}
			}

			if cached.IsRead {
				m.statusMsg = "Message details loaded (cached)"
				return m, nil
			}
			// If unread, fetch from Graph to mark read
			m.statusMsg = "Marking read..."
			return m, fetchMessageDetailCmd(m.graphClient, am.ID)
		}
	}

	// 3. Fallback: Load from API
	m.detailMessage = nil
	m.attachments = nil
	m.detailViewport.SetContent("")
	m.statusMsg = "Loading message details..."
	return m, fetchMessageDetailCmd(m.graphClient, am.ID)
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

	fetchCmd := fetchMessagesCmd(m.graphClient, m.folders[m.selectedFolder].ID)
	if detailCmd != nil {
		return m, tea.Batch(detailCmd, fetchCmd)
	}
	return m, fetchCmd
}

func (m mainModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m = m.updateViewportSize()
		if m.detailMessage != nil {
			m.detailViewport.SetContent(wrapText(formatBodyContent(m.detailMessage.Body.Content), m.detailViewport.Width))
		}
		if m.state == stateCompose {
			h := m.height - 18
			if h < 3 {
				h = 3
			}
			m.composeBody.SetHeight(h)
		}

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

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
		
		// Cache token
		_ = SaveToken(TokenCache(msg))

		// Open SQLite cache if enabled
		if m.config.UseSQLite == 1 && m.db == nil {
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
		sortedFolders := sortFolders(msg)
		if m.state != stateMain {
			// Initial load: set up navigation state
			m.folders = sortedFolders
			m.state = stateMain
			m.activePane = paneFolders
			m.selectedFolder = 0
			if len(m.folders) > 0 {
				// Load from SQLite cache first for instant display
				var detailCmd tea.Cmd
				if m.db != nil {
					if cached, err := m.db.GetMessages(m.folders[0].ID); err == nil && len(cached) > 0 {
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
					fetchMessagesCmd(m.graphClient, m.folders[0].ID),
					fetchInboxMessagesCmd(m.graphClient),
					m.tickCmd(),
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

	case messagesFetchedMsg:
		if len(m.folders) == 0 || m.folders[m.selectedFolder].ID != msg.FolderID {
			break
		}
		// Populate any cached bodies into the newly fetched message list to avoid losing them in memory
		for i, newMsg := range msg.Messages {
			// Check current in-memory messages
			for _, oldMsg := range m.messages {
				if oldMsg.ID == newMsg.ID && oldMsg.Body.Content != "" {
					msg.Messages[i].Body = oldMsg.Body
					break
				}
			}
			// Fallback to SQLite check if still empty
			if msg.Messages[i].Body.Content == "" && m.db != nil {
				if cached, err := m.db.GetMessage(newMsg.ID); err == nil && cached != nil && cached.Body.Content != "" {
					msg.Messages[i].Body = cached.Body
				}
			}
		}

		// Persist messages to SQLite cache (preserving bodies via ON CONFLICT DO UPDATE)
		if m.db != nil {
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
			for i, em := range m.messages {
				if em.ID == msg.Message.ID {
					m.messages[i].IsRead = true
					m.messages[i].Body = msg.Message.Body
				}
			}
			for ti := range m.threadGroups {
				for mi := range m.threadGroups[ti].Members {
					if m.threadGroups[ti].Members[mi].ID == msg.Message.ID {
						m.threadGroups[ti].Members[mi].IsRead = true
						m.threadGroups[ti].Members[mi].Body = msg.Message.Body
					}
				}
			}
			// Upsert message detail (body + read status) into cache
			if m.db != nil && len(m.folders) > 0 {
				_ = m.db.UpsertMessage(m.folders[m.selectedFolder].ID, *msg.Message)
				_ = m.db.UpdateReadStatus(msg.Message.ID, true)
			}
			m.statusMsg = "Message details loaded"
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
					SendSystemNotification(em)
				}
				m.inboxKnownIDs[em.ID] = true
			}
		}

		newMap := make(map[string]bool)
		for _, em := range msg.Messages {
			newMap[em.ID] = true
		}
		m.inboxKnownIDs = newMap

	case mailSentMsg:
		m.state = stateMain
		m.statusMsg = "Email sent successfully!"
		// Reload current folder
		if len(m.folders) > 0 {
			return m, fetchMessagesCmd(m.graphClient, m.folders[m.selectedFolder].ID)
		}

	case mailDeletedMsg:
		m.statusMsg = "Message moved to Deleted Items"
		// Remove from SQLite cache
		if m.db != nil {
			_ = m.db.DeleteMessage(msg.MessageID)
		}
		// Reload messages
		if len(m.folders) > 0 {
			return m, fetchMessagesCmd(m.graphClient, m.folders[m.selectedFolder].ID)
		}

	case attachmentSavedMsg:
		m.statusMsg = fmt.Sprintf("Saved attachment to: %s", msg)
		m.state = stateMain

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
				bgCmds = append(bgCmds, func() tea.Msg {
					msgs, err := m.graphClient.GetMessages(folderID)
					if err == nil {
						return messagesFetchedMsg{FolderID: folderID, Messages: msgs}
					}
					return nil
				})
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

		switch key.String() {
		case "q":
			return m, tea.Quit
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
				m.detailViewport.LineUp(1)
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
				m.detailViewport.LineUp(1)
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
				m.detailViewport.LineUp(1)
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
				m.detailViewport.LineDown(1)
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
				m.detailViewport.LineDown(1)
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
				m.detailViewport.LineDown(1)
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
					// Rebuild virtual list; clamp virtualSelected
					m.buildVirtualList()
					if m.virtualSelected >= len(m.virtualList) {
						m.virtualSelected = len(m.virtualList) - 1
					}
					if m.virtualSelected < 0 {
						m.virtualSelected = 0
					}
				}
			}
		case "n":
			// Compose new email
			m.state = stateCompose
			m.composeStep = 0
			
			m.composeTo = textinput.New()
			m.composeTo.Placeholder = "recipient@domain.com"
			m.composeTo.Focus()
			m.composeTo.Width = m.width - 20
			
			m.composeSubject = textinput.New()
			m.composeSubject.Placeholder = "Email subject..."
			m.composeSubject.Width = m.width - 20
			
			m.composeBody = textarea.New()
			m.composeBody.Placeholder = "Type email body here..."
			m.composeBody.SetWidth(m.width - 20)
			m.composeBody.SetHeight(10)
		case "d", "delete":
			// Delete current message
			if am := m.activeMessage(); am != nil {
				m.statusMsg = "Moving message to Deleted Items..."
				cmds = append(cmds, deleteMailCmd(m.graphClient, am.ID))
			}
		case "r":
			// Mark message Read/Unread
			if am := m.activeMessage(); am != nil {
				targetState := !am.IsRead
				msgID := am.ID
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
				cmds = append(cmds, func() tea.Msg {
					_ = m.graphClient.MarkAsRead(msgID, targetState)
					return nil
				})
			}
		case "a":
			// Open attachments pane if message has attachments
			if m.detailMessage != nil && len(m.attachments) > 0 {
				m.state = stateAttachments
				m.selectedAttach = 0
			}
		case "A":
			// Ask if user wants to reply to sender or all
			if m.activeMessage() != nil {
				m.state = stateReplyConfirm
				m.statusMsg = "Select reply option (s/a/c)"
			}
		}

	case stateCompose:
		key, ok := msg.(tea.KeyMsg)
		if !ok {
			break
		}

		switch key.String() {
		case "esc":
			m.state = stateMain
			m.statusMsg = "Compose cancelled"
		case "tab":
			m.composeStep = (m.composeStep + 1) % 3
			m.updateComposeFocus()
		case "shift+tab":
			m.composeStep = (m.composeStep - 1 + 3) % 3
			m.updateComposeFocus()
		case "ctrl+s":
			// Send!
			m.statusMsg = "Sending email..."
			cmds = append(cmds, sendMailCmd(
				m.graphClient,
				m.composeTo.Value(),
				m.composeSubject.Value(),
				m.composeBody.Value(),
			))
		default:
			// Update the focused compose input
			var cmd tea.Cmd
			switch m.composeStep {
			case 0:
				m.composeTo, cmd = m.composeTo.Update(msg)
			case 1:
				m.composeSubject, cmd = m.composeSubject.Update(msg)
			case 2:
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
		case "up", "k":
			if m.selectedAttach > 0 {
				m.selectedAttach--
			}
		case "down", "j":
			if m.selectedAttach < len(m.attachments)-1 {
				m.selectedAttach++
			}
		case "enter":
			// Save attachment
			m.statusMsg = "Downloading attachment..."
			cmds = append(cmds, saveAttachmentCmd(m.attachments[m.selectedAttach]))
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
	m.composeStep = 2 // Focus body field
	
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

		hasEmail := func(addr string) bool {
			addr = strings.ToLower(strings.TrimSpace(addr))
			if addr == "" {
				return true
			}
			if m.userEmail != "" && strings.ToLower(m.userEmail) == addr {
				return true
			}
			for _, r := range recipients {
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
			if !hasEmail(addr) {
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
			if !hasEmail(addr) {
				if name != "" {
					recipients = append(recipients, fmt.Sprintf("%s <%s>", name, addr))
				} else {
					recipients = append(recipients, addr)
				}
			}
		}
	}

	m.composeTo.SetValue(strings.Join(recipients, ", "))
	
	subject := origMsg.Subject
	if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(subject)), "re:") {
		subject = "Re: " + subject
	}
	m.composeSubject = textinput.New()
	m.composeSubject.Placeholder = "Email subject..."
	m.composeSubject.SetValue(subject)
	m.composeSubject.Width = m.width - 20
	
	m.composeBody = textarea.New()
	m.composeBody.Placeholder = "Type email body here..."
	m.composeBody.SetWidth(m.width - 20)
	m.composeBody.SetHeight(10)
	
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
	for i := 0; i < m.composeBody.LineCount(); i++ {
		m.composeBody.CursorUp()
	}
	m.composeBody.CursorStart()
	m.updateComposeFocus()
}

func (m *mainModel) updateComposeFocus() {
	m.composeTo.Blur()
	m.composeSubject.Blur()
	m.composeBody.Blur()
	switch m.composeStep {
	case 0:
		m.composeTo.Focus()
	case 1:
		m.composeSubject.Focus()
	case 2:
		m.composeBody.Focus()
	}
}

func (m mainModel) updateViewportSize() mainModel {
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
	paneHeight := m.height - 6 // inner content; outer = paneHeight+2 (border top+bottom)
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
	totalHeight := m.height - 6
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
const (
	ColorBg      = "#1E1E2E"
	ColorText    = "#CDD6F4"
	ColorSubtext = "#A6ADC8"
	ColorViolet  = "#CBA6F7" // Primary accent
	ColorCyan    = "#89B4FA" // Secondary accent
	ColorGreen   = "#A6E3A1" // Success
	ColorYellow  = "#F9E2AF" // Warning
	ColorRed     = "#F38BA8" // Error
	ColorSurface = "#313244" // Panel background
	ColorOverlay = "#45475A" // Highlight border
)

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

	statusStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color(ColorText)).
			Background(lipgloss.Color(ColorSurface)).
			Padding(0, 1)
)

func (m mainModel) View() string {
	var s strings.Builder

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

	case stateMain:
		if m.config.Layout == 2 {
			s.WriteString(m.renderLayout2())
		} else {
			s.WriteString(m.renderLayout1())
		}

	case stateCompose:
		s.WriteString("   " + headerStyle.Render("COMPOSE NEW EMAIL") + "\n\n")
		
		toBorder := lipgloss.NewStyle().Foreground(lipgloss.Color(ColorOverlay))
		subjBorder := lipgloss.NewStyle().Foreground(lipgloss.Color(ColorOverlay))
		bodyBorder := lipgloss.NewStyle().Foreground(lipgloss.Color(ColorOverlay))

		switch m.composeStep {
		case 0:
			toBorder = lipgloss.NewStyle().Foreground(lipgloss.Color(ColorViolet))
		case 1:
			subjBorder = lipgloss.NewStyle().Foreground(lipgloss.Color(ColorViolet))
		case 2:
			bodyBorder = lipgloss.NewStyle().Foreground(lipgloss.Color(ColorViolet))
		}

		s.WriteString("   To:\n   " + toBorder.Render(m.composeTo.View()) + "\n\n")
		s.WriteString("   Subject:\n   " + subjBorder.Render(m.composeSubject.View()) + "\n\n")
		s.WriteString("   Body:\n   " + bodyBorder.Render(m.composeBody.View()) + "\n\n")
		
		s.WriteString("   [Tab] Switch Fields  |  [Ctrl+S] Send  |  [Esc] Cancel\n")

	case stateAttachments:
		s.WriteString("   " + headerStyle.Render("ATTACHMENTS IN CURRENT EMAIL") + "\n\n")
		for i, att := range m.attachments {
			indicator := "  "
			if i == m.selectedAttach {
				indicator = "> "
			}
			
			sizeKB := att.Size / 1024
			line := fmt.Sprintf("%s %s (%d KB) [%s]", indicator, att.Name, sizeKB, att.ContentType)
			if i == m.selectedAttach {
				s.WriteString("   " + selectedItemStyle.Render(line) + "\n")
			} else {
				s.WriteString("   " + line + "\n")
			}
		}
		s.WriteString("\n   [Up/Down] Select Attachment  |  [Enter] Save to Downloads  |  [Esc] Back\n")
	}

	// Bottom Status/Keybinds Bar
	if m.state == stateMain {
		s.WriteString("\n")
		statusText := fmt.Sprintf("Status: %s", m.statusMsg)
		keysText := "[Tab] Switch Pane | [Space] Expand/Collapse Thread | [n] Compose | [A] Reply | [d] Delete | [r] Read/Unread | [a] Attachments | [q] Quit"
		
		availableWidth := m.width - lipgloss.Width(keysText) - 4
		if availableWidth > 5 {
			s.WriteString(statusStyle.Width(availableWidth).Render(statusText) + "  " + dimStyle.Render(keysText))
		} else {
			s.WriteString(statusStyle.Width(m.width).Render(statusText))
		}
	} else if m.state != stateCompose && m.state != stateConfig && m.state != stateDeviceAuth && m.state != stateAttachments {
		s.WriteString("\n" + statusStyle.Width(m.width).Render(m.statusMsg))
	}

	// Guarantee exactly m.height output lines so BubbleTea's cursor tracking
	// is never off. Clip if too tall, pad with blank lines if too short.
	if m.height > 0 && m.state == stateMain {
		lines := strings.Split(s.String(), "\n")
		for len(lines) < m.height {
			lines = append(lines, "")
		}
		if len(lines) > m.height {
			lines = lines[:m.height]
		}
		return strings.Join(lines, "\n")
	}
	return s.String()
}

// renderLayout1 renders the default side-by-side three-pane layout:
//   [Folders | Messages | Detail]
func (m mainModel) renderLayout1() string {
	paneHeight := m.height - 6
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

	fView := fStyle.Width(23).Height(paneHeight).Render(foldersView)
	mView := mStyle.Width(33).Height(paneHeight).Render(messagesView)
	// Width(23) outer=25, Width(33) outer=35; dView outer = m.width-60 → Width = m.width-62
	dView := dStyle.Width(m.width - 62).Height(paneHeight).Render(detailView)

	fView = applyPaneTitle(fView, "FOLDERS", m.activePane == paneFolders)
	mView = applyPaneTitle(mView, "MESSAGES", m.activePane == paneMessages)
	dView = applyPaneTitle(dView, "MESSAGE DETAIL", m.activePane == paneDetail)

	return lipgloss.JoinHorizontal(lipgloss.Top, fView, mView, dView) + "\n"
}

// renderLayout2 renders the alternative stacked layout:
//   Left column: [Folders (~30%)] stacked above [Messages (~70%)]
//   Right column: [Detail] (full height)
//
// Left column inner width = 46 → outer = 50 (2 padding + 2 border on each side).
// Right column inner width = m.width - 54.
func (m mainModel) renderLayout2() string {
	totalHeight := m.height - 6
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
	fView := fStyle.Width(leftColInner).Height(foldersHeight).Render(foldersView)
	mView := mStyle.Width(leftColInner).Height(messagesHeight).Render(messagesView)

	fView = applyPaneTitle(fView, "FOLDERS", m.activePane == paneFolders)
	mView = applyPaneTitle(mView, "MESSAGES", m.activePane == paneMessages)

	leftCol := lipgloss.JoinVertical(lipgloss.Left, fView, mView)

	// Right detail pane spans the full height; outer = totalHeight + 2 (borders)
	// left col outer = leftColInner+4=50; right pane Width = m.width - 50 - 4 = m.width - 54
	// dView outer height must match left column outer height (= totalHeight).
	// .Height(n) sets inner content; outer = n+2 (borders). So use totalHeight-2.
	dView := dStyle.Width(m.width - 54).Height(totalHeight - 2).Render(detailView)
	dView = applyPaneTitle(dView, "MESSAGE DETAIL", m.activePane == paneDetail)

	return lipgloss.JoinHorizontal(lipgloss.Top, leftCol, dView) + "\n"
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
			s.WriteString(selectedItemStyle.Copy().Width(availWidth - 2).Render(line) + "\n")
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
			// Truncate to fit
			maxFN := maxFrom - len(threadIndicator) - len(countBadge)
			if maxFN < 4 {
				maxFN = 4
			}
			if len(fromName) > maxFN {
				fromName = fromName[:maxFN-2] + ".."
			}
			if len(subj) > maxSubj {
				subj = subj[:maxSubj-2] + ".."
			}
			line1 := fmt.Sprintf("%s%s %s%s", threadIndicator, unreadMarker, fromName, countBadge)
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
			dateStr := msg.ReceivedDateTime.Local().Format("Jan 2")
			maxFN2 := maxFrom - 8
			if maxFN2 < 4 {
				maxFN2 = 4
			}
			if len(fromName) > maxFN2 {
				fromName = fromName[:maxFN2-2] + ".."
			}
			line1 := fmt.Sprintf("  └ %s %s  %s", unreadMarker, fromName, dateStr)
			line2 := fmt.Sprintf("    %s", msg.BodyPreview)
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

	s.WriteString(wrapText(lipgloss.NewStyle().Bold(true).Render("Subject: ") + m.detailMessage.Subject, width) + "\n")
	s.WriteString(wrapText(lipgloss.NewStyle().Bold(true).Render("From:    ") + fromVal, width) + "\n")

	toVal := formatRecipients(m.detailMessage.ToRecipients)
	if toVal != "" {
		s.WriteString(wrapText(lipgloss.NewStyle().Bold(true).Render("To:      ") + toVal, width) + "\n")
	}

	ccVal := formatRecipients(m.detailMessage.CcRecipients)
	if ccVal != "" {
		s.WriteString(wrapText(lipgloss.NewStyle().Bold(true).Render("Cc:      ") + ccVal, width) + "\n")
	}

	s.WriteString(wrapText(lipgloss.NewStyle().Bold(true).Render("Date:    ") + dateStr, width) + "\n")

	if len(m.attachments) > 0 {
		attStr := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(ColorViolet)).Render(fmt.Sprintf("Attachments (📎 %d): ", len(m.attachments))) +
			dimStyle.Render("Press [a] to view/download attachments")
		s.WriteString(wrapText(attStr, width) + "\n")
	}

	sep := strings.Repeat("-", width - 2)
	s.WriteString(dimStyle.Render(sep) + "\n")

	return s.String()
}

// formatBodyContent strips/cleans up HTML email bodies to readable plain text
func formatBodyContent(htmlContent string) string {
	// First, replace <a> tags so that URLs are preserved before tag stripping
	res := replaceAnchorTags(htmlContent)

	// Convert formatting tags to ANSI escape sequences before stripping HTML tags
	res = regexp.MustCompile(`(?i)<(b|strong)(?:\s+[^>]*)?>`).ReplaceAllString(res, "\x1b[1m")
	res = regexp.MustCompile(`(?i)</(b|strong)\s*>`).ReplaceAllString(res, "\x1b[22m")
	
	res = regexp.MustCompile(`(?i)<(i|em)(?:\s+[^>]*)?>`).ReplaceAllString(res, "\x1b[3m")
	res = regexp.MustCompile(`(?i)</(i|em)\s*>`).ReplaceAllString(res, "\x1b[23m")
	
	res = regexp.MustCompile(`(?i)<u(?:\s+[^>]*)?>`).ReplaceAllString(res, "\x1b[4m")
	res = regexp.MustCompile(`(?i)</u\s*>`).ReplaceAllString(res, "\x1b[24m")

	// Simple tags stripping
	// In a complete implementation, a real HTML-to-text parser would be used.
	// We'll replace simple tags to preserve readability.
	res = strings.ReplaceAll(res, "<br>", "\n")
	res = strings.ReplaceAll(res, "<br/>", "\n")
	res = strings.ReplaceAll(res, "</p>", "\n\n")
	res = strings.ReplaceAll(res, "</div>", "\n")
	
	// Strip all other HTML tags
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
	// Replace non-breaking spaces (\u00a0) with regular spaces to prevent display issues in the terminal
	unescaped = strings.ReplaceAll(unescaped, "\u00a0", " ")
	
	// Clean up whitespace and apply dimming/URL styling to lines
	lines := strings.Split(unescaped, "\n")
	var cleaned []string
	inOriginal := false
	for _, l := range lines {
		trimmed := strings.TrimSpace(l)
		if trimmed != "" || (len(cleaned) > 0 && cleaned[len(cleaned)-1] != "") {
			plainLine := stripANSICodes(l)
			if !inOriginal && isOriginalMessageStart(plainLine) {
				inOriginal = true
			}
			
			isDimmed := l != "" && (inOriginal || strings.HasPrefix(strings.TrimSpace(plainLine), ">"))
			
			// Apply URL styling
			lineWithURLs := styleURLs(l, isDimmed)
			
			if isDimmed {
				cleaned = append(cleaned, dimStyle.Render(lineWithURLs))
			} else {
				cleaned = append(cleaned, lineWithURLs)
			}
		}
	}
	return strings.Join(cleaned, "\n")
}

// replaceAnchorTags finds <a> tags with hrefs and replaces them in-place with:
// - "text (url)" if text and url are substantially different.
// - "url" if they are the same or if text is empty.
func replaceAnchorTags(htmlContent string) string {
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
			return displayURL
		}

		return fmt.Sprintf("%s (%s)", text, displayURL)
	})
}

// styleURLs finds URLs in a string and colors them in Cyan/Blue with underline.
// It restores the correct style at the end of each URL depending on whether the line is dimmed.
func styleURLs(line string, isDimmed bool) string {
	urlRx := regexp.MustCompile(`https?://[^\s<>"\x1b]+`)
	
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
	return re.ReplaceAllString(s, "")
}

func sortFolders(folders []MailFolder) []MailFolder {
	var inbox *MailFolder
	var sentItems *MailFolder
	var others []MailFolder

	for _, f := range folders {
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

	result := make([]MailFolder, 0, len(folders))
	if inbox != nil {
		result = append(result, *inbox)
	}
	if sentItems != nil {
		result = append(result, *sentItems)
	}
	result = append(result, others...)
	return result
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

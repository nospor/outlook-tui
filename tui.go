package main

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
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
)

// Messages
type (
	errMsg                error
	foldersFetchedMsg     []MailFolder
	messagesFetchedMsg    struct {
		FolderID string
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

	// Sub-components
	detailViewport viewport.Model
	spinner        spinner.Model

	// Compose state
	composeTo       textinput.Model
	composeSubject  textinput.Model
	composeBody     textarea.Model
	composeStep     int // 0 = To, 1 = Subject, 2 = Body
}

func initialModel() mainModel {
	ti := textinput.New()
	ti.Placeholder = "Enter Microsoft Client ID..."
	ti.Focus()
	ti.CharLimit = 150
	ti.Width = 50

	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("#3b82f6"))

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

func fetchMessagesCmd(gc *GraphClient, folderID string) tea.Cmd {
	return func() tea.Msg {
		msgs, err := gc.GetMessages(folderID)
		if err != nil {
			return errMsg(err)
		}
		return messagesFetchedMsg{FolderID: folderID, Messages: msgs}
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
		
		return attachmentSavedMsg(path)
	}
}

// Tick command for background refresh every 30s
type tickMsg time.Time
func tickCmd() tea.Cmd {
	return tea.Tick(30*time.Second, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
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
			m.detailViewport.SetContent(formatBodyContent(m.detailMessage.Body.Content))
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
		
		m.authClient = NewAuthenticator(m.config.ClientID, m.config.TenantID, TokenCache(msg))
		m.graphClient = NewGraphClient(m.authClient.GetClient())
		
		return m, fetchFoldersCmd(m.graphClient)

	case foldersFetchedMsg:
		if m.state != stateMain {
			// Initial load: set up navigation state
			m.folders = msg
			m.state = stateMain
			m.activePane = paneFolders
			m.selectedFolder = 0
			if len(m.folders) > 0 {
				m.statusMsg = fmt.Sprintf("Loading messages for %s...", m.folders[0].DisplayName)
				return m, tea.Batch(
					fetchMessagesCmd(m.graphClient, m.folders[0].ID),
					tickCmd(),
				)
			}
			m.statusMsg = "Ready"
		} else {
			// Background refresh: only update folder data (unread counts etc.)
			// Preserve selectedFolder — clamp if folders list shrank
			m.folders = msg
			if m.selectedFolder >= len(m.folders) {
				m.selectedFolder = max(0, len(m.folders)-1)
			}
		}

	case messagesFetchedMsg:
		if len(m.folders) == 0 || m.folders[m.selectedFolder].ID != msg.FolderID {
			break
		}
		// Preserve the currently selected message if it still exists in the new list
		currentID := ""
		if m.selectedMessage < len(m.messages) {
			currentID = m.messages[m.selectedMessage].ID
		}
		m.messages = msg.Messages
		m.statusMsg = fmt.Sprintf("Loaded %d messages", len(m.messages))

		preserved := false
		if currentID != "" {
			for i, em := range m.messages {
				if em.ID == currentID {
					m.selectedMessage = i
					preserved = true
					break
				}
			}
		}
		if !preserved {
			// Previously selected message gone — try to keep the same index (clamped to the new list)
			if len(m.messages) > 0 {
				if m.selectedMessage >= len(m.messages) {
					m.selectedMessage = len(m.messages) - 1
				}
				if m.selectedMessage < 0 {
					m.selectedMessage = 0
				}
				m.detailMessage = nil
				m.attachments = nil
				m.detailViewport.SetContent("")
				m.statusMsg = "Loading message details..."
				return m, fetchMessageDetailCmd(m.graphClient, m.messages[m.selectedMessage].ID)
			} else {
				m.selectedMessage = 0
				m.detailMessage = nil
				m.attachments = nil
				m.detailViewport.SetContent("")
			}
		}

	case messageDetailFetchedMsg:
		// Make sure it matches selected message
		if len(m.messages) > 0 && m.messages[m.selectedMessage].ID == msg.Message.ID {
			m.detailMessage = msg.Message
			m.attachments = msg.Attachments
			m.selectedAttach = 0
			
			m = m.updateViewportSize()
			m.detailViewport.SetContent(formatBodyContent(msg.Message.Body.Content))
			m.detailViewport.GotoTop()
			
			// Mark as read in local UI
			m.messages[m.selectedMessage].IsRead = true
			m.statusMsg = "Message details loaded"
		}

	case mailSentMsg:
		m.state = stateMain
		m.statusMsg = "Email sent successfully!"
		// Reload current folder
		if len(m.folders) > 0 {
			return m, fetchMessagesCmd(m.graphClient, m.folders[m.selectedFolder].ID)
		}

	case mailDeletedMsg:
		m.statusMsg = "Message moved to Deleted Items"
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
			
			return m, tea.Batch(
				tea.Batch(bgCmds...),
				tickCmd(), // Schedule next tick
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
					m.selectedFolder--
					m.selectedMessage = 0
					m.detailMessage = nil
					m.attachments = nil
					m.detailViewport.SetContent("")
					m.statusMsg = fmt.Sprintf("Loading messages for %s...", m.folders[m.selectedFolder].DisplayName)
					cmds = append(cmds, fetchMessagesCmd(m.graphClient, m.folders[m.selectedFolder].ID))
				}
			case paneMessages:
				if m.selectedMessage > 0 {
					m.selectedMessage--
					m.detailMessage = nil
					m.statusMsg = "Loading message details..."
					cmds = append(cmds, fetchMessageDetailCmd(m.graphClient, m.messages[m.selectedMessage].ID))
				}
			case paneDetail:
				m.detailViewport.LineUp(1)
			}
		case "k":
			// vim-key: only navigates lists in folders/messages, scrolls detail
			switch m.activePane {
			case paneFolders:
				if m.selectedFolder > 0 {
					m.selectedFolder--
					m.selectedMessage = 0
					m.detailMessage = nil
					m.attachments = nil
					m.detailViewport.SetContent("")
					m.statusMsg = fmt.Sprintf("Loading messages for %s...", m.folders[m.selectedFolder].DisplayName)
					cmds = append(cmds, fetchMessagesCmd(m.graphClient, m.folders[m.selectedFolder].ID))
				}
			case paneMessages:
				if m.selectedMessage > 0 {
					m.selectedMessage--
					m.detailMessage = nil
					m.statusMsg = "Loading message details..."
					cmds = append(cmds, fetchMessageDetailCmd(m.graphClient, m.messages[m.selectedMessage].ID))
				}
			case paneDetail:
				m.detailViewport.LineUp(1)
			}
		case "down":
			switch m.activePane {
			case paneFolders:
				if m.selectedFolder < len(m.folders)-1 {
					m.selectedFolder++
					m.selectedMessage = 0
					m.detailMessage = nil
					m.attachments = nil
					m.detailViewport.SetContent("")
					m.statusMsg = fmt.Sprintf("Loading messages for %s...", m.folders[m.selectedFolder].DisplayName)
					cmds = append(cmds, fetchMessagesCmd(m.graphClient, m.folders[m.selectedFolder].ID))
				}
			case paneMessages:
				if m.selectedMessage < len(m.messages)-1 {
					m.selectedMessage++
					m.detailMessage = nil
					m.statusMsg = "Loading message details..."
					cmds = append(cmds, fetchMessageDetailCmd(m.graphClient, m.messages[m.selectedMessage].ID))
				}
			case paneDetail:
				m.detailViewport.LineDown(1)
			}
		case "j":
			// vim-key: only navigates lists in folders/messages, scrolls detail
			switch m.activePane {
			case paneFolders:
				if m.selectedFolder < len(m.folders)-1 {
					m.selectedFolder++
					m.selectedMessage = 0
					m.detailMessage = nil
					m.attachments = nil
					m.detailViewport.SetContent("")
					m.statusMsg = fmt.Sprintf("Loading messages for %s...", m.folders[m.selectedFolder].DisplayName)
					cmds = append(cmds, fetchMessagesCmd(m.graphClient, m.folders[m.selectedFolder].ID))
				}
			case paneMessages:
				if m.selectedMessage < len(m.messages)-1 {
					m.selectedMessage++
					m.detailMessage = nil
					m.statusMsg = "Loading message details..."
					cmds = append(cmds, fetchMessageDetailCmd(m.graphClient, m.messages[m.selectedMessage].ID))
				}
			case paneDetail:
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
			if len(m.messages) > 0 && m.selectedMessage < len(m.messages) {
				m.statusMsg = "Moving message to Deleted Items..."
				cmds = append(cmds, deleteMailCmd(m.graphClient, m.messages[m.selectedMessage].ID))
			}
		case "r":
			// Mark message Read/Unread
			if len(m.messages) > 0 && m.selectedMessage < len(m.messages) {
				targetState := !m.messages[m.selectedMessage].IsRead
				m.messages[m.selectedMessage].IsRead = targetState
				m.statusMsg = fmt.Sprintf("Marking message read status...")
				cmds = append(cmds, func() tea.Msg {
					_ = m.graphClient.MarkAsRead(m.messages[m.selectedMessage].ID, targetState)
					return nil
				})
			}
		case "a":
			// Open attachments pane if message has attachments
			if m.detailMessage != nil && len(m.attachments) > 0 {
				m.state = stateAttachments
				m.selectedAttach = 0
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
	}

	return m, tea.Batch(cmds...)
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

	metaHeight := 6 // header(2) + Subject+From+Date(3) + separator(1)
	if m.detailMessage != nil && len(m.attachments) > 0 {
		metaHeight = 7
	}

	viewportHeight := paneHeight - metaHeight
	if viewportHeight < 3 {
		viewportHeight = 3
	}
	
	m.detailViewport.Width = detailWidth
	m.detailViewport.Height = viewportHeight
	return m
}

// Rendering Views
var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#ffffff")).
			Background(lipgloss.Color("#0f6cbd")).
			Padding(0, 2).
			Height(1)

	headerStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#3b82f6")).
			PaddingLeft(1).
			Underline(true)

	paneNormalStyle = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder()).
			BorderForeground(lipgloss.Color("#3f3f46")).
			Padding(0, 1)

	paneActiveStyle = lipgloss.NewStyle().
			Border(lipgloss.DoubleBorder()).
			BorderForeground(lipgloss.Color("#3b82f6")).
			Padding(0, 1)

	selectedItemStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("#ffffff")).
				Background(lipgloss.Color("#2563eb"))

	unreadStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#22d3ee"))

	dimStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#a1a1aa"))

	statusStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#e4e4e7")).
			Background(lipgloss.Color("#27272a")).
			Padding(0, 1)
)

func (m mainModel) View() string {
	var s strings.Builder

	// Top Title Bar
	s.WriteString(titleStyle.Render(fmt.Sprintf("OUTLOOK TUI v1.0 | %d x %d", m.width, m.height)))
	s.WriteString("\n\n")

	switch m.state {
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
			s.WriteString("      " + lipgloss.NewStyle().Foreground(lipgloss.Color("#38bdf8")).Underline(true).Render(m.deviceCode.VerificationURI) + "\n\n")
			s.WriteString("   2. Enter the following activation code:\n")
			s.WriteString("      " + lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#f59e0b")).Render(m.deviceCode.UserCode) + "\n\n")
			s.WriteString("   " + m.spinner.View() + " " + m.statusMsg + "\n\n")
		} else {
			s.WriteString("   " + m.spinner.View() + " Preparing device authentication...\n\n")
		}
		s.WriteString("   [Esc] Go Back to Config  |  [Ctrl+C] Quit\n")

	case stateLoading:
		s.WriteString("\n\n   " + m.spinner.View() + " " + m.statusMsg + "\n")

	case stateMain:
		paneHeight := m.height - 6
		if paneHeight < 5 {
			paneHeight = 5
		}

		// Left: Folders (paneHeight = inner content area, header handled inside)
		foldersView := m.renderFoldersView(paneHeight)
		// Middle: Messages
		messagesView := m.renderMessagesView(paneHeight)
		// Right: Details
		detailView := m.renderDetailView()

		// Layout
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

		s.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, fView, mView, dView))
		s.WriteString("\n")

	case stateCompose:
		s.WriteString("   " + headerStyle.Render("COMPOSE NEW EMAIL") + "\n\n")
		
		toBorder := lipgloss.NewStyle().Foreground(lipgloss.Color("#3f3f46"))
		subjBorder := lipgloss.NewStyle().Foreground(lipgloss.Color("#3f3f46"))
		bodyBorder := lipgloss.NewStyle().Foreground(lipgloss.Color("#3f3f46"))

		switch m.composeStep {
		case 0:
			toBorder = lipgloss.NewStyle().Foreground(lipgloss.Color("#3b82f6"))
		case 1:
			subjBorder = lipgloss.NewStyle().Foreground(lipgloss.Color("#3b82f6"))
		case 2:
			bodyBorder = lipgloss.NewStyle().Foreground(lipgloss.Color("#3b82f6"))
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
		keysText := "[Tab] Switch Pane | [N] Compose | [D] Delete/Trash | [R] Read/Unread | [A] Attachments | [Q] Quit"
		
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

func (m mainModel) renderFoldersView(availHeight int) string {
	var s strings.Builder
	s.WriteString(headerStyle.Render("FOLDERS") + "\n\n")
	
	if len(m.folders) == 0 {
		s.WriteString(dimStyle.Render(" No folders"))
		return s.String()
	}

	start := 0
	end := len(m.folders)
	
	maxItems := availHeight - 2
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
			s.WriteString(selectedItemStyle.Render(line) + "\n")
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
	s.WriteString(headerStyle.Render("MESSAGES") + "\n\n")

	if len(m.messages) == 0 {
		s.WriteString(dimStyle.Render(" No messages"))
		return s.String()
	}

	start := 0
	end := len(m.messages)

	// Each message takes 3 lines
	maxItems := (availHeight - 2) / 3
	if maxItems < 1 {
		maxItems = 1
	}

	if len(m.messages) > maxItems {
		start = m.selectedMessage - (maxItems / 2)
		if start < 0 {
			start = 0
		}
		end = start + maxItems
		if end > len(m.messages) {
			end = len(m.messages)
			start = end - maxItems
			if start < 0 {
				start = 0
			}
		}
	}

	for i := start; i < end; i++ {
		msg := m.messages[i]
		fromName := msg.From.EmailAddress.Name
		if fromName == "" {
			fromName = msg.From.EmailAddress.Address
		}
		if len(fromName) > 16 {
			fromName = fromName[:14] + ".."
		}

		subject := msg.Subject
		if subject == "" {
			subject = "(No Subject)"
		}
		if len(subject) > 20 {
			subject = subject[:18] + ".."
		}

		unreadMarker := " "
		if !msg.IsRead {
			unreadMarker = "●"
		}
		
		attachMarker := " "
		if msg.HasAttachments {
			attachMarker = "@"
		}

		line1 := fmt.Sprintf("%s %s", unreadMarker, fromName)
		line2 := fmt.Sprintf("  %s %s", attachMarker, subject)

		if i == m.selectedMessage {
			s.WriteString(selectedItemStyle.Render(fmt.Sprintf("%-28s\n%-28s", line1, line2)) + "\n\n")
		} else {
			if !msg.IsRead {
				s.WriteString(unreadStyle.Render(line1) + "\n" + dimStyle.Render(line2) + "\n\n")
			} else {
				s.WriteString(line1 + "\n" + dimStyle.Render(line2) + "\n\n")
			}
		}
	}
	return s.String()
}

func (m mainModel) renderDetailView() string {
	var s strings.Builder
	s.WriteString(headerStyle.Render("MESSAGE DETAIL") + "\n\n")

	if m.detailMessage == nil {
		s.WriteString(dimStyle.Render(" Select a message to view details"))
		return s.String()
	}

	// Meta info block
	fromVal := fmt.Sprintf("%s <%s>", m.detailMessage.From.EmailAddress.Name, m.detailMessage.From.EmailAddress.Address)
	dateStr := m.detailMessage.ReceivedDateTime.Local().Format("Mon, Jan 2, 2006 at 15:04")
	
	s.WriteString(lipgloss.NewStyle().Bold(true).Render("Subject: ") + m.detailMessage.Subject + "\n")
	s.WriteString(lipgloss.NewStyle().Bold(true).Render("From:    ") + fromVal + "\n")
	s.WriteString(lipgloss.NewStyle().Bold(true).Render("Date:    ") + dateStr + "\n")
	
	if len(m.attachments) > 0 {
		s.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#3b82f6")).Render(fmt.Sprintf("Attachments (📎 %d): ", len(m.attachments))))
		s.WriteString(dimStyle.Render("Press [A] to view/download attachments\n"))
	}
	
	s.WriteString(dimStyle.Render("--------------------------------------------------------------------") + "\n")
	s.WriteString(m.detailViewport.View())

	return s.String()
}

// formatBodyContent strips/cleans up HTML email bodies to readable plain text
func formatBodyContent(htmlContent string) string {
	// Simple tags stripping
	// In a complete implementation, a real HTML-to-text parser would be used.
	// We'll replace simple tags to preserve readability.
	res := htmlContent
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
			inTag = false
			continue
		}
		if !inTag {
			builder.WriteRune(r)
		}
	}
	
	// Clean up whitespace
	lines := strings.Split(builder.String(), "\n")
	var cleaned []string
	for _, l := range lines {
		trimmed := strings.TrimSpace(l)
		if trimmed != "" || (len(cleaned) > 0 && cleaned[len(cleaned)-1] != "") {
			cleaned = append(cleaned, l)
		}
	}
	return strings.Join(cleaned, "\n")
}

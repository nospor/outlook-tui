package main

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type searchFetchedMsg struct {
	Seq      int
	Query    string
	FolderID string
	Messages []Message
	NextLink string
	Append   bool
	Err      error
}

func (m mainModel) inSearchMode() bool {
	return m.searchQuery != ""
}

func (m mainModel) suppressFolderMessageRefresh() bool {
	return m.searchQuery != "" || m.searchLoading || m.state == stateSearch
}

func (m mainModel) messagesPaneTitle() string {
	if m.inSearchMode() {
		return "[2] SEARCH"
	}
	return "[2] MESSAGES"
}

func (m mainModel) messagesEmptyText() string {
	if m.inSearchMode() {
		return " No search results"
	}
	return " No messages"
}

func (m mainModel) currentFolderName() string {
	if len(m.folders) == 0 || m.selectedFolder < 0 || m.selectedFolder >= len(m.folders) {
		return ""
	}
	return m.folders[m.selectedFolder].DisplayName
}

func (m mainModel) currentFolderID() string {
	if len(m.folders) == 0 || m.selectedFolder < 0 || m.selectedFolder >= len(m.folders) {
		return ""
	}
	return m.folders[m.selectedFolder].ID
}

func (m mainModel) clearSearchState() mainModel {
	m.searchQuery = ""
	m.searchFolderID = ""
	m.searchNextLink = ""
	m.searchLoading = false
	m.searchSeq++
	return m
}

func newSearchInput() textinput.Model {
	si := textinput.New()
	si.Placeholder = "invoice, from:bob, subject:weekly"
	si.CharLimit = 200
	si.Width = 48
	si.Prompt = "> "
	return si
}

func (m mainModel) openSearchPrompt() (mainModel, tea.Cmd) {
	if len(m.folders) == 0 {
		m.statusMsg = "No folder selected"
		return m, nil
	}
	m.state = stateSearch
	if m.searchInput.Placeholder == "" {
		m.searchInput = newSearchInput()
	}
	m.searchInput.SetValue(m.searchQuery)
	m.searchInput.CursorEnd()
	m.searchInput.Focus()
	m.statusMsg = "Enter a search phrase"
	return m, textinput.Blink
}

func (m mainModel) cancelSearchPrompt() mainModel {
	m.searchInput.Blur()
	m.state = stateMain
	if m.inSearchMode() {
		m.statusMsg = fmt.Sprintf("Search %q (%d results)", m.searchQuery, len(m.messages))
	} else {
		m.statusMsg = "Search cancelled"
	}
	return m
}

func (m mainModel) exitSearchMode() (mainModel, tea.Cmd) {
	m.searchInput.Blur()
	m.state = stateMain
	m = m.clearSearchState()
	if len(m.folders) == 0 {
		m.statusMsg = "Search closed"
		return m, nil
	}
	var detailCmd tea.Cmd
	m, m.statusMsg = m.loadCachedFolderMessages()
	if am := m.activeMessage(); am != nil {
		m, detailCmd = m.loadMessageDetail(am)
	} else {
		m.detailMessage = nil
		m.attachments = nil
		m.detailViewport.SetContent("")
	}
	folderID := m.folders[m.selectedFolder].ID
	if folderID == "favorites" {
		m.updateFavoritesFolderCounts()
		return m, detailCmd
	}
	if m.graphClient == nil {
		return m, detailCmd
	}
	fetchCmd := fetchMessagesCmd(m.graphClient, folderID)
	if detailCmd != nil {
		return m, tea.Batch(detailCmd, fetchCmd)
	}
	return m, fetchCmd
}

func (m mainModel) submitSearch() (mainModel, tea.Cmd) {
	query := strings.TrimSpace(m.searchInput.Value())
	if query == "" {
		m.statusMsg = "Enter a search phrase"
		return m, nil
	}
	folderID := m.currentFolderID()
	if folderID == "" {
		m.statusMsg = "No folder selected"
		m.state = stateMain
		return m, nil
	}

	m.searchInput.Blur()
	m.state = stateMain

	if folderID == "favorites" {
		return m.applyLocalFavoriteSearch(query)
	}
	if m.graphClient == nil {
		m.statusMsg = "Not connected to Microsoft Graph"
		return m, nil
	}

	m.searchSeq++
	m.searchLoading = true
	m.searchFolderID = folderID
	m.statusMsg = fmt.Sprintf("Searching %s for %q...", m.currentFolderName(), query)
	return m, fetchSearchCmd(m.graphClient, m.searchSeq, folderID, query, "", false)
}

func (m mainModel) loadMoreSearchResults() (mainModel, tea.Cmd) {
	if !m.inSearchMode() {
		return m, nil
	}
	if m.currentFolderID() == "favorites" {
		m.statusMsg = "No more search results"
		return m, nil
	}
	if m.searchNextLink == "" {
		m.statusMsg = "No more search results"
		return m, nil
	}
	if m.graphClient == nil {
		m.statusMsg = "Not connected to Microsoft Graph"
		return m, nil
	}
	m.searchLoading = true
	m.statusMsg = "Loading more search results..."
	return m, fetchSearchCmd(m.graphClient, m.searchSeq, m.searchFolderID, m.searchQuery, m.searchNextLink, true)
}

func fetchSearchCmd(gc *GraphClient, seq int, folderID, query, nextLink string, appendResults bool) tea.Cmd {
	return func() tea.Msg {
		var page messageListPage
		var err error
		if appendResults {
			page, err = gc.SearchMessagesNext(nextLink)
		} else {
			page, err = gc.SearchMessages(folderID, query)
		}
		return searchFetchedMsg{
			Seq:      seq,
			Query:    query,
			FolderID: folderID,
			Messages: page.Messages,
			NextLink: page.NextLink,
			Append:   appendResults,
			Err:      err,
		}
	}
}

func (m mainModel) applyLocalFavoriteSearch(query string) (mainModel, tea.Cmd) {
	var cached []Message
	if m.db != nil {
		cached, _ = m.db.GetFavoriteMessages()
	} else {
		cached = m.messages
	}
	m.searchQuery = query
	m.searchFolderID = "favorites"
	m.searchNextLink = ""
	m.searchLoading = false
	m.messages = filterMessagesByQuery(cached, query)
	m.virtualSelected = 0
	m.buildThreadGroups()
	m, cmd := m.loadSearchSelectionDetail()
	m.statusMsg = fmt.Sprintf("Favorites search %q: %d results (local)", query, len(m.messages))
	return m, cmd
}

func (m mainModel) applySearchFetched(msg searchFetchedMsg) (mainModel, tea.Cmd) {
	if msg.Seq != m.searchSeq {
		return m, nil
	}
	m.searchLoading = false
	if msg.Err != nil {
		if msg.Append {
			m.statusMsg = fmt.Sprintf("Failed to load more search results: %v", msg.Err)
		} else {
			m.statusMsg = fmt.Sprintf("Search failed: %v", msg.Err)
		}
		return m, nil
	}

	msgs := m.hydrateMessageBodies(msg.Messages)
	msgs = m.messagesExcludingPendingDeletes(msgs)

	if msg.Append {
		if len(msgs) == 0 && msg.NextLink == "" {
			m.searchNextLink = ""
			m.statusMsg = "No more search results"
			return m, nil
		}
		seen := make(map[string]bool, len(m.messages))
		for _, existing := range m.messages {
			seen[existing.ID] = true
		}
		added := 0
		for _, found := range msgs {
			if found.ID == "" || seen[found.ID] {
				continue
			}
			m.messages = append(m.messages, found)
			seen[found.ID] = true
			added++
		}
		m.searchNextLink = msg.NextLink
		m.buildThreadGroups()
		m.statusMsg = fmt.Sprintf("Search %q: %d results", m.searchQuery, len(m.messages))
		if added == 0 && msg.NextLink == "" {
			m.statusMsg = "No more search results"
		}
		return m, nil
	}

	m.searchQuery = msg.Query
	m.searchFolderID = msg.FolderID
	m.searchNextLink = msg.NextLink
	m.messages = msgs
	m.virtualSelected = 0
	m.buildThreadGroups()
	folderName := m.currentFolderName()
	if folderName == "" {
		folderName = "folder"
	}
	m, cmd := m.loadSearchSelectionDetail()
	m.statusMsg = fmt.Sprintf("Search %q in %s: %d results", msg.Query, folderName, len(m.messages))
	return m, cmd
}

func (m mainModel) loadSearchSelectionDetail() (mainModel, tea.Cmd) {
	if len(m.virtualList) == 0 {
		m.detailMessage = nil
		m.attachments = nil
		m.detailViewport.SetContent("")
		return m, nil
	}
	return m.loadMessageDetail(m.activeMessage())
}

func (m mainModel) hydrateMessageBodies(msgs []Message) []Message {
	for i, newMsg := range msgs {
		for _, oldMsg := range m.messages {
			if oldMsg.ID == newMsg.ID && oldMsg.Body.Content != "" {
				msgs[i].Body = oldMsg.Body
				msgs[i].Attachments = oldMsg.Attachments
				break
			}
		}
		if msgs[i].Body.Content == "" && m.config.UseSQLite == 1 && m.db != nil {
			if cached, err := m.db.GetMessage(newMsg.ID); err == nil && cached != nil && cached.Body.Content != "" {
				msgs[i].Body = cached.Body
				msgs[i].Attachments = cached.Attachments
			}
		}
	}
	return msgs
}

func (m mainModel) handleSearchKey(msg tea.KeyMsg) (mainModel, tea.Cmd) {
	switch msg.String() {
	case "esc":
		return m.cancelSearchPrompt(), nil
	case "enter":
		return m.submitSearch()
	}

	var cmd tea.Cmd
	m.searchInput, cmd = m.searchInput.Update(msg)
	return m, cmd
}

func (m mainModel) applyFavoriteSearchAfterToggle() mainModel {
	if !m.inSearchMode() || m.currentFolderID() != "favorites" {
		return m
	}
	updated, _ := m.applyLocalFavoriteSearch(m.searchQuery)
	return updated
}

func filterMessagesByQuery(msgs []Message, query string) []Message {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		out := make([]Message, len(msgs))
		copy(out, msgs)
		return out
	}
	var matches []Message
	for _, msg := range msgs {
		if messageMatchesQuery(msg, q) {
			matches = append(matches, msg)
		}
	}
	return matches
}

func messageMatchesQuery(msg Message, q string) bool {
	fields := []string{
		msg.Subject,
		msg.BodyPreview,
		msg.From.EmailAddress.Name,
		msg.From.EmailAddress.Address,
	}
	for _, r := range msg.ToRecipients {
		fields = append(fields, r.EmailAddress.Name, r.EmailAddress.Address)
	}
	for _, r := range msg.CcRecipients {
		fields = append(fields, r.EmailAddress.Name, r.EmailAddress.Address)
	}
	if msg.Body.Content != "" {
		fields = append(fields, msg.Body.Content)
	}
	for _, field := range fields {
		if strings.Contains(strings.ToLower(field), q) {
			return true
		}
	}
	return false
}

func (m mainModel) renderSearchPopup(width int) string {
	innerWidth := width - 4
	if innerWidth < 24 {
		innerWidth = 24
	}

	si := m.searchInput
	si.Width = innerWidth - 4

	folderName := m.currentFolderName()
	if folderName == "" {
		folderName = "current folder"
	}

	scope := fmt.Sprintf("Folder: %s", folderName)
	hint := "Microsoft Graph $search (KQL). Favorites search locally."
	if m.currentFolderID() == "favorites" {
		hint = "Favorites is local-only; Graph is not queried."
	}

	header := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(ColorViolet)).Render(" SEARCH MAIL ")
	scopeLine := lipgloss.NewStyle().Foreground(lipgloss.Color(ColorText)).Render(scope)
	hintLine := lipgloss.NewStyle().Foreground(lipgloss.Color(ColorSubtext)).Render(hint)
	footer := lipgloss.NewStyle().Foreground(lipgloss.Color(ColorSubtext)).Render("[Enter] Search  |  [Esc] Cancel")

	pad := func(s string) string {
		plain := s
		if w := lipgloss.Width(plain); w < innerWidth-2 {
			return plain + strings.Repeat(" ", innerWidth-2-w)
		}
		return plain
	}

	rows := []string{
		pad(header),
		strings.Repeat(" ", innerWidth-2),
		pad(scopeLine),
		pad(hintLine),
		strings.Repeat(" ", innerWidth-2),
		pad(si.View()),
		strings.Repeat(" ", innerWidth-2),
		pad(footer),
	}

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(ColorViolet)).
		Padding(0, 1).
		Render(strings.Join(rows, "\n"))
}

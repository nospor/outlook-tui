package main

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const (
	attendeeListEditStepName    = 0
	attendeeListEditStepMembers = 1
)

// AttendeeSuggestion is a unified autocomplete entry for contacts or attendee lists.
type AttendeeSuggestion struct {
	List    *AttendeeList
	Contact *Contact
}

func parseContactField(s string) []Contact {
	var contacts []Contact
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		name := ""
		addr := part
		if strings.Contains(part, "<") && strings.Contains(part, ">") {
			start := strings.Index(part, "<")
			end := strings.Index(part, ">")
			if start < end {
				name = strings.TrimSpace(part[:start])
				addr = strings.TrimSpace(part[start+1 : end])
			}
		}
		if IsValidAttendeeEmail(addr) {
			contacts = append(contacts, Contact{Name: name, Address: addr})
		}
	}
	return contacts
}

func formatContactAddress(c Contact) string {
	name := strings.TrimSpace(c.Name)
	addr := strings.TrimSpace(c.Address)
	if name != "" {
		return fmt.Sprintf("%s <%s>", name, addr)
	}
	return addr
}

func attendeeEmailsInField(val string) map[string]bool {
	existing := make(map[string]bool)
	for _, a := range ParseAttendeeField(val, "required") {
		existing[strings.ToLower(strings.TrimSpace(a.Address))] = true
	}
	return existing
}

// appendContactsToAttendeeInput adds contacts to a comma-separated attendee field, deduping by email.
func appendContactsToAttendeeInput(input *textinput.Model, toAdd []Contact) {
	val := input.Value()
	parts := strings.Split(val, ",")
	var prefix string
	if len(parts) > 1 {
		prefix = strings.Join(parts[:len(parts)-1], ",") + ","
	}

	existing := attendeeEmailsInField(val)
	var additions []string
	for _, c := range toAdd {
		addr := strings.ToLower(strings.TrimSpace(c.Address))
		if addr == "" || existing[addr] {
			continue
		}
		existing[addr] = true
		additions = append(additions, formatContactAddress(c))
	}

	newVal := strings.TrimSpace(prefix)
	if newVal != "" && len(additions) > 0 {
		newVal += " "
	}
	newVal += strings.Join(additions, ", ")
	if len(additions) > 0 {
		newVal += ", "
	}
	input.SetValue(newVal)
	input.SetCursor(len(newVal))
}

func filterAttendeeSuggestions(query string, contacts []Contact, lists []AttendeeList, includeLists bool) []AttendeeSuggestion {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return nil
	}

	var out []AttendeeSuggestion

	if includeLists {
		for i := range lists {
			if strings.Contains(strings.ToLower(lists[i].Name), query) {
				out = append(out, AttendeeSuggestion{List: &lists[i]})
			}
		}
	}

	for i := range contacts {
		c := contacts[i]
		if strings.Contains(strings.ToLower(c.Name), query) || strings.Contains(strings.ToLower(c.Address), query) {
			out = append(out, AttendeeSuggestion{Contact: &contacts[i]})
		}
	}
	return out
}

func (m *mainModel) loadAttendeeLists() {
	m.attendeeLists = nil
	if m.db != nil {
		if lists, err := m.db.GetAttendeeLists(); err == nil {
			m.attendeeLists = lists
		}
	}
}

func (m *mainModel) clearAttendeeSuggestions() {
	m.attendeeSuggestions = nil
	m.attendeeSuggestionsSelected = 0
	m.contactsStartIdx = 0
}

func (m *mainModel) attendeeSuggestionQueryFromInput(val string) string {
	parts := strings.Split(val, ",")
	if len(parts) == 0 {
		return ""
	}
	return strings.TrimSpace(parts[len(parts)-1])
}

func attendeeSuggestionQueryFromSingleInput(val string) string {
	return strings.TrimSpace(val)
}

func parseSingleContactInput(s string) (Contact, bool) {
	members := parseContactField(s)
	if len(members) == 0 {
		return Contact{}, false
	}
	return members[0], true
}

func (m *mainModel) attendeeListEditHasMember(addr string) bool {
	key := strings.ToLower(strings.TrimSpace(addr))
	for _, c := range m.attendeeListEditMemberRows {
		if strings.ToLower(strings.TrimSpace(c.Address)) == key {
			return true
		}
	}
	return false
}

func (m *mainModel) addAttendeeListEditMember(c Contact) bool {
	addr := strings.TrimSpace(c.Address)
	if !IsValidAttendeeEmail(addr) || m.attendeeListEditHasMember(addr) {
		return false
	}
	m.attendeeListEditMemberRows = append(m.attendeeListEditMemberRows, Contact{
		Name:    strings.TrimSpace(c.Name),
		Address: addr,
	})
	return true
}

func (m *mainModel) addAttendeeListEditMemberFromInput() bool {
	c, ok := parseSingleContactInput(m.attendeeListEditMembers.Value())
	if !ok {
		return false
	}
	if !m.addAttendeeListEditMember(c) {
		return false
	}
	m.attendeeListEditMembers.SetValue("")
	m.attendeeListEditMembers.SetCursor(0)
	m.clearAttendeeSuggestions()
	return true
}

func (m *mainModel) removeAttendeeListEditMember(idx int) {
	if idx < 0 || idx >= len(m.attendeeListEditMemberRows) {
		return
	}
	m.attendeeListEditMemberRows = append(
		m.attendeeListEditMemberRows[:idx],
		m.attendeeListEditMemberRows[idx+1:]...,
	)
	if len(m.attendeeListEditMemberRows) == 0 {
		m.attendeeListEditMemberSelected = 0
		m.attendeeListEditMembersListFocus = false
		return
	}
	if m.attendeeListEditMemberSelected >= len(m.attendeeListEditMemberRows) {
		m.attendeeListEditMemberSelected = len(m.attendeeListEditMemberRows) - 1
	}
}

func (m *mainModel) applyAttendeeSuggestionToListEdit(idx int) {
	if idx < 0 || idx >= len(m.attendeeSuggestions) {
		return
	}
	sug := m.attendeeSuggestions[idx]
	if sug.Contact != nil {
		if m.addAttendeeListEditMember(*sug.Contact) {
			m.attendeeListEditMembers.SetValue("")
			m.attendeeListEditMembers.SetCursor(0)
		}
	}
	m.clearAttendeeSuggestions()
}

func (m mainModel) renderAttendeeSuggestionsList(listWidth int) string {
	if len(m.attendeeSuggestions) == 0 {
		return ""
	}
	if listWidth <= 0 {
		listWidth = m.width - 26
	}
	if listWidth < 30 {
		listWidth = 30
	}

	maxItems := 5
	if len(m.attendeeSuggestions) < maxItems {
		maxItems = len(m.attendeeSuggestions)
	}
	start := m.contactsStartIdx
	if start < 0 {
		start = 0
	}
	if start > len(m.attendeeSuggestions)-maxItems {
		start = len(m.attendeeSuggestions) - maxItems
	}
	end := start + maxItems

	listStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ColorCyan)).Bold(true)

	var rows []string
	if start > 0 {
		rows = append(rows, dimStyle.Render(fmt.Sprintf("  … %d more above", start)))
	}
	for i := start; i < end; i++ {
		sug := m.attendeeSuggestions[i]
		var text string
		if sug.List != nil {
			text = fmt.Sprintf("List: %s (%d)", sug.List.Name, len(sug.List.Members))
		} else if sug.Contact != nil {
			text = formatContactAddress(*sug.Contact)
		}
		prefix := "  "
		if i == m.attendeeSuggestionsSelected {
			prefix = "› "
		}
		line := prefix + text
		if lipgloss.Width(line) > listWidth {
			for len(text) > 3 && lipgloss.Width(prefix+text) > listWidth-1 {
				text = text[:len(text)-1]
			}
			line = prefix + text + "…"
		}
		if i == m.attendeeSuggestionsSelected {
			rows = append(rows, selectedItemStyle.Render(line))
		} else if sug.List != nil {
			rows = append(rows, listStyle.Render(line))
		} else {
			rows = append(rows, line)
		}
	}
	if end < len(m.attendeeSuggestions) {
		rows = append(rows, dimStyle.Render(fmt.Sprintf("  … %d more below", len(m.attendeeSuggestions)-end)))
	}
	return strings.Join(rows, "\n")
}

func (m *mainModel) applyAttendeeSuggestion(idx int, input *textinput.Model) {
	if idx < 0 || idx >= len(m.attendeeSuggestions) {
		return
	}
	sug := m.attendeeSuggestions[idx]
	if sug.List != nil {
		appendContactsToAttendeeInput(input, sug.List.Members)
	} else if sug.Contact != nil {
		parts := strings.Split(input.Value(), ",")
		if len(parts) > 0 {
			parts[len(parts)-1] = " " + formatContactAddress(*sug.Contact)
			newValue := strings.TrimLeft(strings.Join(parts, ","), " ")
			input.SetValue(newValue + ", ")
			input.SetCursor(len(input.Value()))
		}
	}
	m.clearAttendeeSuggestions()
}

func (m *mainModel) handleAttendeeSuggestionKeys(msg tea.KeyMsg) bool {
	if len(m.attendeeSuggestions) == 0 {
		return false
	}
	switch msg.String() {
	case "up", "k":
		m.attendeeSuggestionsSelected = (m.attendeeSuggestionsSelected - 1 + len(m.attendeeSuggestions)) % len(m.attendeeSuggestions)
		if m.attendeeSuggestionsSelected == len(m.attendeeSuggestions)-1 {
			m.contactsStartIdx = len(m.attendeeSuggestions) - 5
			if m.contactsStartIdx < 0 {
				m.contactsStartIdx = 0
			}
		} else if m.attendeeSuggestionsSelected < m.contactsStartIdx {
			m.contactsStartIdx = m.attendeeSuggestionsSelected
		}
		return true
	case "down", "j":
		m.attendeeSuggestionsSelected = (m.attendeeSuggestionsSelected + 1) % len(m.attendeeSuggestions)
		if m.attendeeSuggestionsSelected == 0 {
			m.contactsStartIdx = 0
		} else if m.attendeeSuggestionsSelected >= m.contactsStartIdx+5 {
			m.contactsStartIdx = m.attendeeSuggestionsSelected - 5 + 1
		}
		return true
	case "enter":
		return false // caller provides the active input
	case "esc":
		m.clearAttendeeSuggestions()
		return true
	}
	return false
}

func (m *mainModel) initAttendeeListEditForm(listID string) {
	w := m.width - 20
	if w < 40 {
		w = 40
	}
	if w > 80 {
		w = 80
	}

	m.attendeeListEditID = listID
	m.attendeeListEditStep = attendeeListEditStepName

	m.attendeeListEditName = textinput.New()
	m.attendeeListEditName.Placeholder = "List name (e.g. Weekly standup)"
	m.attendeeListEditName.Width = w
	m.attendeeListEditName.CharLimit = 120

	m.attendeeListEditMembers = textinput.New()
	m.attendeeListEditMembers.Placeholder = "email@domain.com or name <email@domain.com>"
	m.attendeeListEditMembers.Width = w
	m.attendeeListEditMembers.CharLimit = 200

	m.attendeeListEditMemberRows = nil
	m.attendeeListEditMemberSelected = 0
	m.attendeeListEditMembersListFocus = false

	if listID != "" {
		for _, list := range m.attendeeLists {
			if list.ID == listID {
				m.attendeeListEditName.SetValue(list.Name)
				m.attendeeListEditMemberRows = append([]Contact{}, list.Members...)
				break
			}
		}
	} else {
		m.attendeeListEditName.SetValue("")
	}
	m.attendeeListEditMembers.SetValue("")

	m.updateAttendeeListEditFocus()
	m.clearAttendeeSuggestions()
	m.loadContacts()
}

func (m *mainModel) updateAttendeeListEditFocus() {
	m.attendeeListEditName.Blur()
	m.attendeeListEditMembers.Blur()
	switch m.attendeeListEditStep {
	case attendeeListEditStepMembers:
		if m.attendeeListEditMembersListFocus && len(m.attendeeListEditMemberRows) > 0 {
			// member list navigation — input stays blurred
		} else {
			m.attendeeListEditMembersListFocus = false
			m.attendeeListEditMembers.Focus()
		}
	default:
		m.attendeeListEditStep = attendeeListEditStepName
		m.attendeeListEditMembersListFocus = false
		m.attendeeListEditName.Focus()
	}
}

func (m *mainModel) updateAttendeeListEditFilteredSuggestions() {
	if m.state != stateAttendeeListEdit || m.attendeeListEditStep != attendeeListEditStepMembers || m.attendeeListEditMembersListFocus {
		m.clearAttendeeSuggestions()
		return
	}
	query := attendeeSuggestionQueryFromSingleInput(m.attendeeListEditMembers.Value())
	if query == "" || m.config.UseSQLite == 0 || len(m.contacts) == 0 {
		m.clearAttendeeSuggestions()
		return
	}
	m.attendeeSuggestions = filterAttendeeSuggestions(query, m.contacts, nil, false)
	if m.attendeeSuggestionsSelected >= len(m.attendeeSuggestions) {
		m.attendeeSuggestionsSelected = 0
		m.contactsStartIdx = 0
	}
}

func (m *mainModel) saveAttendeeListEdit() error {
	if m.db == nil {
		return fmt.Errorf("database not available")
	}
	name := strings.TrimSpace(m.attendeeListEditName.Value())
	members := m.attendeeListEditMemberRows
	if len(members) == 0 {
		return fmt.Errorf("at least one member is required")
	}

	var savedID string
	if m.attendeeListEditID == "" {
		id, err := m.db.CreateAttendeeList(name, members)
		if err != nil {
			return err
		}
		savedID = id
	} else {
		if err := m.db.UpdateAttendeeList(m.attendeeListEditID, name, members); err != nil {
			return err
		}
		savedID = m.attendeeListEditID
	}

	m.loadAttendeeLists()
	for i, list := range m.attendeeLists {
		if list.ID == savedID {
			m.attendeeListSelected = i
			break
		}
	}
	return nil
}

func (m *mainModel) handleAttendeeListsUpdate(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch m.state {
		case stateAttendeeLists:
			return m.handleAttendeeListsBrowseKeys(msg)
		case stateAttendeeListEdit:
			return m.handleAttendeeListEditKeys(msg)
		case stateAttendeeListDeleteConfirm:
			return m.handleAttendeeListDeleteConfirmKeys(msg)
		}
	}
	return m, nil
}

func (m *mainModel) handleAttendeeListsBrowseKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "q":
		m.state = stateCalendar
		m.statusMsg = "Ready"
		return m, nil
	case "up", "k":
		if len(m.attendeeLists) > 0 && m.attendeeListSelected > 0 {
			m.attendeeListSelected--
		}
	case "down", "j":
		if len(m.attendeeLists) > 0 && m.attendeeListSelected < len(m.attendeeLists)-1 {
			m.attendeeListSelected++
		}
	case "n":
		m.initAttendeeListEditForm("")
		m.state = stateAttendeeListEdit
		m.statusMsg = "New attendee list — Enter: add member | Tab: fields | Ctrl+s: save | Esc: cancel"
	case "e":
		if len(m.attendeeLists) > 0 && m.attendeeListSelected < len(m.attendeeLists) {
			m.initAttendeeListEditForm(m.attendeeLists[m.attendeeListSelected].ID)
			m.state = stateAttendeeListEdit
			m.statusMsg = "Edit attendee list — Enter: add member | Tab: fields | Ctrl+s: save | Esc: cancel"
		}
	case "d":
		if len(m.attendeeLists) > 0 && m.attendeeListSelected < len(m.attendeeLists) {
			list := m.attendeeLists[m.attendeeListSelected]
			m.attendeeListDeleteID = list.ID
			m.attendeeListDeleteName = list.Name
			m.state = stateAttendeeListDeleteConfirm
		}
	}
	return m, nil
}

func (m *mainModel) handleAttendeeListEditKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.attendeeListEditStep == attendeeListEditStepMembers && m.attendeeListEditMembersListFocus && len(m.attendeeListEditMemberRows) > 0 {
		switch msg.String() {
		case "up", "k":
			if m.attendeeListEditMemberSelected > 0 {
				m.attendeeListEditMemberSelected--
			}
			return m, nil
		case "down", "j":
			if m.attendeeListEditMemberSelected < len(m.attendeeListEditMemberRows)-1 {
				m.attendeeListEditMemberSelected++
			}
			return m, nil
		case "d", "delete", "backspace":
			m.removeAttendeeListEditMember(m.attendeeListEditMemberSelected)
			return m, nil
		case "tab":
			m.attendeeListEditMembersListFocus = false
			m.updateAttendeeListEditFocus()
			return m, nil
		case "esc":
			m.state = stateAttendeeLists
			m.statusMsg = "Attendee lists"
			return m, nil
		}
	}

	if m.attendeeListEditStep == attendeeListEditStepMembers && !m.attendeeListEditMembersListFocus && len(m.attendeeSuggestions) > 0 {
		if m.handleAttendeeSuggestionKeys(msg) {
			return m, nil
		}
		if msg.String() == "enter" {
			m.applyAttendeeSuggestionToListEdit(m.attendeeSuggestionsSelected)
			return m, nil
		}
	}

	switch msg.String() {
	case "esc":
		m.state = stateAttendeeLists
		m.statusMsg = "Attendee lists"
		return m, nil
	case "tab":
		if m.attendeeListEditStep == attendeeListEditStepName {
			m.attendeeListEditStep = attendeeListEditStepMembers
			m.attendeeListEditMembersListFocus = false
		} else if !m.attendeeListEditMembersListFocus && len(m.attendeeListEditMemberRows) > 0 {
			m.attendeeListEditMembersListFocus = true
		} else {
			m.attendeeListEditStep = attendeeListEditStepName
			m.attendeeListEditMembersListFocus = false
		}
		m.updateAttendeeListEditFocus()
		m.clearAttendeeSuggestions()
		return m, nil
	case "shift+tab":
		if m.attendeeListEditStep == attendeeListEditStepMembers && m.attendeeListEditMembersListFocus {
			m.attendeeListEditMembersListFocus = false
		} else if m.attendeeListEditStep == attendeeListEditStepMembers {
			m.attendeeListEditStep = attendeeListEditStepName
			m.attendeeListEditMembersListFocus = false
		} else {
			m.attendeeListEditStep = attendeeListEditStepMembers
			m.attendeeListEditMembersListFocus = len(m.attendeeListEditMemberRows) > 0
		}
		m.updateAttendeeListEditFocus()
		m.clearAttendeeSuggestions()
		return m, nil
	case "ctrl+s", "ctrl+x":
		if err := m.saveAttendeeListEdit(); err != nil {
			m.statusMsg = fmt.Sprintf("Save failed: %v", err)
			return m, nil
		}
		m.state = stateAttendeeLists
		m.statusMsg = "Attendee list saved"
		return m, nil
	case "enter":
		if m.attendeeListEditStep == attendeeListEditStepMembers && !m.attendeeListEditMembersListFocus {
			if m.addAttendeeListEditMemberFromInput() {
				m.statusMsg = "Member added"
			} else if strings.TrimSpace(m.attendeeListEditMembers.Value()) != "" {
				m.statusMsg = "Invalid or duplicate email"
			}
			return m, nil
		}
	}

	if msg.String() == "ctrl+c" {
		return m, tea.Quit
	}

	var cmd tea.Cmd
	if m.attendeeListEditStep == attendeeListEditStepMembers && !m.attendeeListEditMembersListFocus {
		m.attendeeListEditMembers, cmd = m.attendeeListEditMembers.Update(msg)
		m.updateAttendeeListEditFilteredSuggestions()
	} else if m.attendeeListEditStep == attendeeListEditStepName {
		m.attendeeListEditName, cmd = m.attendeeListEditName.Update(msg)
	}
	return m, cmd
}

func (m *mainModel) handleAttendeeListDeleteConfirmKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "Y":
		if m.db != nil && m.attendeeListDeleteID != "" {
			_ = m.db.DeleteAttendeeList(m.attendeeListDeleteID)
			m.loadAttendeeLists()
			if m.attendeeListSelected >= len(m.attendeeLists) {
				if len(m.attendeeLists) > 0 {
					m.attendeeListSelected = len(m.attendeeLists) - 1
				} else {
					m.attendeeListSelected = 0
				}
			}
		}
		m.attendeeListDeleteID = ""
		m.attendeeListDeleteName = ""
		m.state = stateAttendeeLists
		m.statusMsg = "Attendee list deleted"
	case "n", "N", "esc":
		m.attendeeListDeleteID = ""
		m.attendeeListDeleteName = ""
		m.state = stateAttendeeLists
		m.statusMsg = "Delete cancelled"
	}
	return m, nil
}

func (m mainModel) renderAttendeeListsView() string {
	var s strings.Builder
	s.WriteString("   " + headerStyle.Render("👥  ATTENDEE LISTS") + "\n\n")

	listWidth := 42
	if m.width < 90 {
		listWidth = m.width / 3
	}
	detailWidth := m.width - listWidth - 6
	if detailWidth < 20 {
		detailWidth = 20
	}
	listHeight := m.height - 10
	if listHeight < 5 {
		listHeight = 5
	}

	if len(m.attendeeLists) == 0 {
		empty := paneNormalStyle.Copy().Width(m.width - 8).Height(listHeight).Render(
			dimStyle.Render("No attendee lists yet. Press [n] to create one."),
		)
		s.WriteString(empty + "\n")
		return s.String()
	}

	var listBuf strings.Builder
	for i, list := range m.attendeeLists {
		marker := " "
		nameStyle := lipgloss.NewStyle()
		if i == m.attendeeListSelected {
			marker = "›"
			nameStyle = selectedItemStyle
		}
		line := fmt.Sprintf("%s %s (%d)", marker, list.Name, len(list.Members))
		maxLen := listWidth - 4
		if lipgloss.Width(line) > maxLen && maxLen > 3 {
			for lipgloss.Width(line) > maxLen-1 {
				line = line[:len(line)-1]
			}
			line += "…"
		}
		if i == m.attendeeListSelected {
			listBuf.WriteString(nameStyle.Copy().Width(listWidth-2).Render(line) + "\n")
		} else {
			listBuf.WriteString(line + "\n")
		}
	}

	leftPane := paneActiveStyle.Copy().Width(listWidth).Height(listHeight).Render(listBuf.String())
	leftPane = applyPaneTitle(leftPane, "LISTS", true)

	selected := m.attendeeLists[m.attendeeListSelected]
	var detailBuf strings.Builder
	cyan := lipgloss.NewStyle().Foreground(lipgloss.Color(ColorCyan)).Bold(true)
	detailBuf.WriteString(cyan.Render(selected.Name) + "\n")
	detailBuf.WriteString(dimStyle.Render(fmt.Sprintf("%d member(s)", len(selected.Members))) + "\n")
	for _, c := range selected.Members {
		detailBuf.WriteString("  • " + formatContactAddress(c) + "\n")
	}

	rightPane := paneNormalStyle.Copy().Width(detailWidth).Height(listHeight).Render(detailBuf.String())
	rightPane = applyPaneTitle(rightPane, "MEMBERS", false)

	s.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, leftPane, "  ", rightPane) + "\n")
	return s.String()
}

func (m mainModel) renderAttendeeListEditView() string {
	var s strings.Builder
	title := "👥  NEW ATTENDEE LIST"
	if m.attendeeListEditID != "" {
		title = "👥  EDIT ATTENDEE LIST"
	}
	s.WriteString("   " + headerStyle.Render(title) + "\n\n")

	activeLabel := lipgloss.NewStyle().Foreground(lipgloss.Color(ColorViolet)).Bold(true)
	inactiveLabel := lipgloss.NewStyle().Foreground(lipgloss.Color(ColorSubtext))

	var lines []string
	nameMarker := "  "
	nameLbl := inactiveLabel
	if m.attendeeListEditStep == attendeeListEditStepName {
		nameMarker = "  ▸ "
		nameLbl = activeLabel
	}
	nameVal := m.attendeeListEditName.Value()
	if m.attendeeListEditStep == attendeeListEditStepName {
		nameVal = m.attendeeListEditName.View()
	} else if strings.TrimSpace(nameVal) == "" {
		nameVal = dimStyle.Render("(empty)")
	}
	lines = append(lines, nameMarker+nameLbl.Render("Name:")+" "+nameVal)

	memActive := m.attendeeListEditStep == attendeeListEditStepMembers
	memMarker := "  "
	memLbl := inactiveLabel
	if memActive {
		memMarker = "  ▸ "
		memLbl = activeLabel
	}
	lines = append(lines, memMarker+memLbl.Render("Members:"))

	addMarker := "    "
	if memActive && !m.attendeeListEditMembersListFocus {
		addMarker = "    ▸ "
	}
	addVal := m.attendeeListEditMembers.Value()
	if memActive && !m.attendeeListEditMembersListFocus {
		addVal = m.attendeeListEditMembers.View()
	} else if strings.TrimSpace(addVal) == "" {
		addVal = dimStyle.Render("(type email, Enter to add)")
	}
	lines = append(lines, addMarker+inactiveLabel.Render("Add:")+" "+addVal)

	if memActive && !m.attendeeListEditMembersListFocus && len(m.attendeeSuggestions) > 0 {
		lines = append(lines, m.renderAttendeeSuggestionsList(m.width-20))
	}

	if len(m.attendeeListEditMemberRows) == 0 {
		lines = append(lines, dimStyle.Render("    (no members yet)"))
	} else {
		for i, c := range m.attendeeListEditMemberRows {
			text := "• " + formatContactAddress(c)
			if memActive && m.attendeeListEditMembersListFocus && i == m.attendeeListEditMemberSelected {
				lines = append(lines, selectedItemStyle.Render("    › "+text))
			} else {
				lines = append(lines, "    "+text)
			}
		}
	}

	hint := "  Tab/Shift+Tab: fields | Enter: add member"
	if len(m.attendeeListEditMemberRows) > 0 {
		hint += " | Tab: member list | d/Delete: remove selected"
	}
	lines = append(lines, dimStyle.Render(hint))

	content := strings.Join(lines, "\n")
	boxW := m.width - 8
	if boxW < 40 {
		boxW = 40
	}
	s.WriteString(paneActiveStyle.Copy().Width(boxW).Render(content) + "\n")
	return s.String()
}

func (m mainModel) renderAttendeeListDeleteConfirmPopup(width int) string {
	dropdownWidth := width - 4
	if dropdownWidth < 20 {
		dropdownWidth = 20
	}

	var rows []string
	headerText := " DELETE LIST? "
	if len(headerText) < dropdownWidth-2 {
		headerText = headerText + strings.Repeat(" ", dropdownWidth-2-len(headerText))
	}
	rows = append(rows, lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(ColorYellow)).Render(headerText))
	rows = append(rows, strings.Repeat(" ", dropdownWidth-2))

	line1 := "You are about to delete this attendee list:"
	if len(line1) < dropdownWidth-2 {
		line1 = line1 + strings.Repeat(" ", dropdownWidth-2-len(line1))
	} else if len(line1) > dropdownWidth-2 {
		line1 = line1[:dropdownWidth-5] + "..."
	}
	rows = append(rows, line1)

	nameText := fmt.Sprintf("  \"%s\"", m.attendeeListDeleteName)
	if len(nameText) < dropdownWidth-2 {
		nameText = nameText + strings.Repeat(" ", dropdownWidth-2-len(nameText))
	} else if len(nameText) > dropdownWidth-2 {
		nameText = nameText[:dropdownWidth-5] + "..."
	}
	rows = append(rows, lipgloss.NewStyle().Foreground(lipgloss.Color(ColorSubtext)).Render(nameText))
	rows = append(rows, strings.Repeat(" ", dropdownWidth-2))

	line2 := "Do you really want to delete?"
	if len(line2) < dropdownWidth-2 {
		line2 = line2 + strings.Repeat(" ", dropdownWidth-2-len(line2))
	}
	rows = append(rows, line2)
	rows = append(rows, strings.Repeat(" ", dropdownWidth-2))

	btnYesRaw := "  [y] Yes, delete list"
	paddingYes := dropdownWidth - 2 - len(btnYesRaw)
	if paddingYes < 0 {
		paddingYes = 0
	}
	btnYesRendered := "  " + lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(ColorYellow)).Render("[y]") + " Yes, delete list" + strings.Repeat(" ", paddingYes)
	rows = append(rows, btnYesRendered)

	btnNoRaw := "  [n] No, go back"
	paddingNo := dropdownWidth - 2 - len(btnNoRaw)
	if paddingNo < 0 {
		paddingNo = 0
	}
	btnNoRendered := "  " + lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(ColorGreen)).Render("[n]") + " No, go back" + strings.Repeat(" ", paddingNo)
	rows = append(rows, btnNoRendered)

	joined := strings.Join(rows, "\n")

	popupStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(ColorYellow)).
		Padding(0, 1)

	return popupStyle.Render(joined)
}

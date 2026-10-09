package main

import (
	"errors"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type zoxideDirsLoadedMsg struct {
	Query string
	Paths []string
	Err   error
}

func loadZoxideDirsCmd(query string) tea.Cmd {
	return func() tea.Msg {
		paths, err := QueryZoxideDirs(query)
		return zoxideDirsLoadedMsg{Query: query, Paths: paths, Err: err}
	}
}

func (m *mainModel) clampFilePickerZoxideSelection() {
	n := len(m.filePickerZoxidePaths)
	if n == 0 {
		m.filePickerZoxideSelected = 0
		return
	}
	if m.filePickerZoxideSelected >= n {
		m.filePickerZoxideSelected = n - 1
	}
	if m.filePickerZoxideSelected < 0 {
		m.filePickerZoxideSelected = 0
	}
}

func (m mainModel) openFilePickerZoxide() (mainModel, tea.Cmd) {
	m.filePickerZoxideMode = true
	m.filePickerZoxideLoading = true
	m.filePickerZoxideSelected = 0
	m.filePickerZoxidePaths = nil
	m.filePickerZoxideError = ""
	m.zoxideInput.SetValue("")
	m.zoxideInput.Focus()
	return m, loadZoxideDirsCmd("")
}

func (m mainModel) closeFilePickerZoxide() mainModel {
	m.filePickerZoxideMode = false
	m.filePickerZoxideLoading = false
	m.filePickerZoxideError = ""
	m.zoxideInput.Blur()
	return m
}

func (m mainModel) handleFilePickerZoxideKey(msg tea.KeyMsg) (mainModel, tea.Cmd) {
	switch msg.String() {
	case "esc":
		return m.closeFilePickerZoxide(), nil
	case "down":
		if len(m.filePickerZoxidePaths) > 0 {
			m.filePickerZoxideSelected++
			m.clampFilePickerZoxideSelection()
		}
		return m, nil
	case "up":
		if len(m.filePickerZoxidePaths) > 0 {
			m.filePickerZoxideSelected--
			m.clampFilePickerZoxideSelection()
		}
		return m, nil
	case "enter":
		if len(m.filePickerZoxidePaths) == 0 {
			return m, nil
		}
		path := m.filePickerZoxidePaths[m.filePickerZoxideSelected]
		return m.jumpFilePickerToDirectory(path)
	}

	oldVal := m.zoxideInput.Value()
	var cmd tea.Cmd
	m.zoxideInput, cmd = m.zoxideInput.Update(msg)
	if m.zoxideInput.Value() != oldVal {
		m.filePickerZoxideLoading = true
		m.filePickerZoxideSelected = 0
		return m, tea.Batch(cmd, loadZoxideDirsCmd(m.zoxideInput.Value()))
	}
	return m, cmd
}

func (m mainModel) jumpFilePickerToDirectory(path string) (mainModel, tea.Cmd) {
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		m.filePickerZoxideError = "directory not found: " + path
		return m, nil
	}
	m = m.closeFilePickerZoxide()
	m.filepicker.JumpToDirectory(path)
	_ = SaveFilepickerSettings(m.filepicker.SortBy.String(), m.filepicker.SortOrder.String(), m.filepicker.CurrentDirectory)
	return m, m.filepicker.Init()
}

func (m mainModel) applyZoxideDirsLoaded(msg zoxideDirsLoadedMsg) mainModel {
	if !m.filePickerZoxideMode {
		return m
	}
	if msg.Query != m.zoxideInput.Value() {
		return m
	}
	m.filePickerZoxideLoading = false
	if msg.Err != nil {
		m.filePickerZoxidePaths = nil
		if errors.Is(msg.Err, ErrZoxideNotInstalled) {
			m.filePickerZoxideError = "zoxide is not installed (no zoxide binary in PATH)"
		} else {
			m.filePickerZoxideError = msg.Err.Error()
		}
	} else {
		m.filePickerZoxideError = ""
		m.filePickerZoxidePaths = msg.Paths
	}
	m.clampFilePickerZoxideSelection()
	return m
}

func fitZoxideLine(s string, width int) string {
	if width <= 0 {
		return ""
	}
	s = strings.ReplaceAll(s, "\n", " ")
	if lipgloss.Width(s) <= width {
		return s
	}
	ellipsis := "…"
	runes := []rune(s)
	for len(runes) > 0 && lipgloss.Width(string(runes)+ellipsis) > width {
		runes = runes[:len(runes)-1]
	}
	if len(runes) == 0 {
		return ellipsis
	}
	return string(runes) + ellipsis
}

func (m mainModel) renderFilePickerZoxidePopup(w, h int) string {
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ColorSubtext))
	title := lipgloss.NewStyle().Foreground(lipgloss.Color(ColorCyan)).Bold(true).Render("Jump with zoxide")

	innerW := w - 4
	if innerW < 1 {
		innerW = 1
	}
	inputW := innerW - 2
	if inputW < 10 {
		inputW = 10
	}
	zi := m.zoxideInput
	zi.Width = inputW

	innerH := h - 4
	if innerH < 4 {
		innerH = 4
	}
	listH := innerH - 4
	if listH < 1 {
		listH = 1
	}

	var lines []string
	lines = append(lines, fitZoxideLine(title, innerW), "")
	lines = append(lines, fitZoxideLine(zi.View(), innerW), "")

	if m.filePickerZoxideLoading {
		lines = append(lines, fitZoxideLine(dimStyle.Render("Loading…"), innerW))
	} else if m.filePickerZoxideError != "" {
		errStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ColorRed))
		lines = append(lines, fitZoxideLine(errStyle.Render(m.filePickerZoxideError), innerW))
	} else if len(m.filePickerZoxidePaths) == 0 {
		lines = append(lines, fitZoxideLine(dimStyle.Render("No matching directories"), innerW))
	} else {
		start := 0
		sel := m.filePickerZoxideSelected
		if sel >= listH {
			start = sel - listH + 1
		}
		end := start + listH
		if end > len(m.filePickerZoxidePaths) {
			end = len(m.filePickerZoxidePaths)
		}
		selStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ColorGreen)).Bold(true)
		for i := start; i < end; i++ {
			path := m.filePickerZoxidePaths[i]
			if i == sel {
				lines = append(lines, fitZoxideLine(selStyle.Render("▸ "+path), innerW))
			} else {
				lines = append(lines, fitZoxideLine("  "+path, innerW))
			}
		}
	}

	for len(lines) < innerH-1 {
		lines = append(lines, "")
	}

	footer := dimStyle.Italic(true).Render("Type to filter • ↑/↓: move list • Enter: go to directory • Esc: back to browser")
	lines = append(lines, fitZoxideLine(footer, innerW))

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(ColorYellow)).
		Padding(1, 2).
		Width(w).Height(h).
		Render(strings.Join(lines, "\n"))
}

func (m mainModel) renderFilePickerOverlay(w, h int) string {
	if m.filePickerZoxideMode {
		return m.renderFilePickerZoxidePopup(w, h)
	}
	return m.renderFilePickerPopup(w, h)
}

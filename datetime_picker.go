package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type pickerFocus int

const (
	pickerFocusCalendar pickerFocus = iota
	pickerFocusHour
	pickerFocusMinute
	pickerFocusText
)

const pickerMinuteStep = 15

// DateTimePicker is an interactive calendar + time adjuster for event start/end fields.
type DateTimePicker struct {
	selected  time.Time
	viewMonth time.Time
	focus     pickerFocus
	showTime  bool
	textInput textinput.Model
	active    bool
}

func NewDateTimePicker(t time.Time) DateTimePicker {
	t = t.In(time.Local)
	p := DateTimePicker{
		selected:  t,
		viewMonth: time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.Local),
		showTime:  true,
		focus:     pickerFocusCalendar,
	}
	p.textInput = textinput.New()
	p.textInput.Placeholder = "YYYY-MM-DD HH:MM"
	p.textInput.Width = 20
	return p
}

func (p *DateTimePicker) SetTime(t time.Time) {
	t = t.In(time.Local)
	p.selected = t
	p.viewMonth = time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.Local)
}

func (p DateTimePicker) Time() time.Time {
	return p.selected.In(time.Local)
}

func (p DateTimePicker) Summary() string {
	return FormatEventDateTime(p.selected)
}

func (p *DateTimePicker) Focus() {
	p.active = true
	if p.focus != pickerFocusText {
		p.focus = pickerFocusCalendar
	}
	p.textInput.Blur()
}

func (p *DateTimePicker) Blur() {
	if p.focus == pickerFocusText {
		p.leaveTextMode(true)
	}
	p.active = false
	p.textInput.Blur()
}

func (p *DateTimePicker) SetShowTime(show bool) {
	p.showTime = show
	if !show && p.focus != pickerFocusCalendar && p.focus != pickerFocusText {
		p.focus = pickerFocusCalendar
	}
}

// AdvanceSubFocus moves calendar → hour → minute. Returns false when already on minute.
func (p *DateTimePicker) AdvanceSubFocus() bool {
	if p.focus == pickerFocusText {
		p.leaveTextMode(true)
		if p.showTime {
			p.focus = pickerFocusHour
		}
		return true
	}
	switch p.focus {
	case pickerFocusCalendar:
		if p.showTime {
			p.focus = pickerFocusHour
			return true
		}
		return false
	case pickerFocusHour:
		p.focus = pickerFocusMinute
		return true
	default:
		return false
	}
}

// RetreatSubFocus moves minute → hour → calendar. Returns false when already on calendar.
func (p *DateTimePicker) RetreatSubFocus() bool {
	if p.focus == pickerFocusText {
		p.leaveTextMode(true)
		return false
	}
	switch p.focus {
	case pickerFocusMinute:
		p.focus = pickerFocusHour
		return true
	case pickerFocusHour:
		p.focus = pickerFocusCalendar
		return true
	default:
		return false
	}
}

func (p *DateTimePicker) monthGridStart() time.Time {
	first := time.Date(p.viewMonth.Year(), p.viewMonth.Month(), 1, 0, 0, 0, 0, time.Local)
	offset := (int(first.Weekday()) + 6) % 7
	return first.AddDate(0, 0, -offset)
}

func (p *DateTimePicker) cellDate(row, col int) time.Time {
	return p.monthGridStart().AddDate(0, 0, row*7+col)
}

func (p *DateTimePicker) gridPosForSelected() (row, col int) {
	start := p.monthGridStart()
	diff := int(p.selected.Truncate(24*time.Hour).Sub(start).Hours() / 24)
	if diff < 0 {
		diff = 0
	}
	if diff > 41 {
		diff = 41
	}
	return diff / 7, diff % 7
}

func (p *DateTimePicker) moveDay(deltaDays int) {
	p.selected = p.selected.AddDate(0, 0, deltaDays)
	p.viewMonth = time.Date(p.selected.Year(), p.selected.Month(), 1, 0, 0, 0, 0, time.Local)
}

func (p *DateTimePicker) addMonths(delta int) {
	t := p.selected.AddDate(0, delta, 0)
	p.selected = time.Date(
		t.Year(), t.Month(), t.Day(),
		p.selected.Hour(), p.selected.Minute(), 0, 0,
		time.Local,
	)
	p.viewMonth = time.Date(p.selected.Year(), p.selected.Month(), 1, 0, 0, 0, 0, time.Local)
}

func (p *DateTimePicker) adjustHour(delta int) {
	p.selected = p.selected.Add(time.Duration(delta) * time.Hour)
}

func (p *DateTimePicker) adjustMinute(delta int) {
	p.selected = p.selected.Add(time.Duration(delta*pickerMinuteStep) * time.Minute)
}

func (p *DateTimePicker) enterTextMode() {
	p.focus = pickerFocusText
	p.textInput.SetValue(FormatEventDateTime(p.selected))
	p.textInput.Width = 20
	p.textInput.Focus()
}

func (p *DateTimePicker) leaveTextMode(apply bool) {
	if apply {
		if t, err := ParseEventDateTime(p.textInput.Value()); err == nil {
			p.SetTime(t)
		}
	}
	p.textInput.Blur()
	p.focus = pickerFocusCalendar
}

// CommitTextMode applies typed text (when valid) and exits text entry mode.
func (p *DateTimePicker) CommitTextMode() {
	if p.focus == pickerFocusText {
		p.leaveTextMode(true)
	}
}

func (p DateTimePicker) InTextMode() bool {
	return p.focus == pickerFocusText
}

func (p *DateTimePicker) CancelTextMode() {
	p.leaveTextMode(false)
}

func (p DateTimePicker) Update(msg tea.Msg) (DateTimePicker, tea.Cmd) {
	if !p.active {
		return p, nil
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		if p.focus == pickerFocusText {
			switch msg.String() {
			case "esc":
				p.leaveTextMode(false)
				return p, nil
			case "enter", "tab":
				p.leaveTextMode(true)
				return p, nil
			}
			var cmd tea.Cmd
			p.textInput, cmd = p.textInput.Update(msg)
			return p, cmd
		}

		switch msg.String() {
		case "i", "I":
			p.enterTextMode()
			return p, nil
		case "enter":
			if p.focus == pickerFocusCalendar && p.showTime {
				p.focus = pickerFocusHour
				return p, nil
			}
		case ",", "<":
			if p.focus == pickerFocusCalendar {
				p.addMonths(-1)
				return p, nil
			}
		case ".", ">":
			if p.focus == pickerFocusCalendar {
				p.addMonths(1)
				return p, nil
			}
		case "left", "h":
			switch p.focus {
			case pickerFocusCalendar:
				p.moveDay(-1)
			case pickerFocusMinute:
				p.focus = pickerFocusHour
			}
			return p, nil
		case "right", "l":
			switch p.focus {
			case pickerFocusCalendar:
				p.moveDay(1)
			case pickerFocusHour:
				p.focus = pickerFocusMinute
			}
			return p, nil
		case "up", "k", "+", "=":
			switch p.focus {
			case pickerFocusCalendar:
				p.moveDay(-7)
			case pickerFocusHour:
				p.adjustHour(1)
			case pickerFocusMinute:
				p.adjustMinute(1)
			}
			return p, nil
		case "down", "j", "-", "_":
			switch p.focus {
			case pickerFocusCalendar:
				p.moveDay(7)
			case pickerFocusHour:
				p.adjustHour(-1)
			case pickerFocusMinute:
				p.adjustMinute(-1)
			}
			return p, nil
		}
	}
	return p, nil
}

func (p DateTimePicker) focusLabel() string {
	switch p.focus {
	case pickerFocusHour:
		return "hour"
	case pickerFocusMinute:
		return "minute"
	default:
		return "date"
	}
}

func (p DateTimePicker) View() string {
	if p.focus == pickerFocusText {
		var b strings.Builder
		b.WriteString(dimStyle.Render("Type date/time:"))
		b.WriteString("\n ")
		b.WriteString(p.textInput.View())
		b.WriteString("\n")
		b.WriteString(dimStyle.Render("Enter/Tab apply · Esc cancel"))
		return b.String()
	}

	selectedStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color(ColorBg)).
		Background(lipgloss.Color(ColorViolet))
	cursorStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color(ColorBg)).
		Background(lipgloss.Color(ColorCyan))
	todayStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color(ColorCyan))
	otherMonthStyle := dimStyle
	normalStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ColorText))

	monthTitle := fmt.Sprintf("%s %d", p.viewMonth.Format("January"), p.viewMonth.Year())
	monthNav := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(ColorViolet)).Render(
		"◀ " + monthTitle + " ▶",
	)
	if p.focus == pickerFocusCalendar {
		monthNav = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(ColorBg)).Background(lipgloss.Color(ColorViolet)).Render(
			"◀ " + monthTitle + " ▶",
		)
	}

	var lines []string
	lines = append(lines, "  "+monthNav)
	lines = append(lines, "  "+dimStyle.Render("Mo Tu We Th Fr Sa Su"))

	cursorRow, cursorCol := p.gridPosForSelected()
	now := time.Now()
	for row := 0; row < 6; row++ {
		var cells []string
		for col := 0; col < 7; col++ {
			day := p.cellDate(row, col)
			label := fmt.Sprintf("%2d", day.Day())
			inMonth := day.Month() == p.viewMonth.Month() && day.Year() == p.viewMonth.Year()
			isSelected := day.Year() == p.selected.Year() &&
				day.Month() == p.selected.Month() &&
				day.Day() == p.selected.Day()
			isToday := day.Year() == now.Year() &&
				day.Month() == now.Month() &&
				day.Day() == now.Day()
			isCursor := row == cursorRow && col == cursorCol && p.focus == pickerFocusCalendar

			cell := label
			switch {
			case isCursor:
				cell = cursorStyle.Render(label)
			case isSelected:
				cell = selectedStyle.Render(label)
			case isToday:
				cell = todayStyle.Render(label)
			case !inMonth:
				cell = otherMonthStyle.Render(label)
			default:
				cell = normalStyle.Render(label)
			}
			cells = append(cells, cell)
		}
		lines = append(lines, "  "+strings.Join(cells, " "))
	}

	if p.showTime {
		hourStr := fmt.Sprintf("%02d", p.selected.Hour())
		minStr := fmt.Sprintf("%02d", p.selected.Minute())
		hourStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ColorText))
		minStyle := hourStyle
		if p.focus == pickerFocusHour {
			hourStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(ColorBg)).Background(lipgloss.Color(ColorViolet))
		}
		if p.focus == pickerFocusMinute {
			minStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(ColorBg)).Background(lipgloss.Color(ColorViolet))
		}
		timeLine := "  Time: " + hourStyle.Render(hourStr) + " : " + minStyle.Render(minStr)
		lines = append(lines, timeLine)
	}

	lines = append(lines, dimStyle.Render(fmt.Sprintf(
		"  Focus: %s · Tab next · ,/. month · ↑↓ adjust · ←→ switch · i type",
		p.focusLabel(),
	)))
	return strings.Join(lines, "\n")
}

package main

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func TestDateTimePickerSetTimeRoundTrip(t *testing.T) {
	want := time.Date(2026, 3, 15, 14, 30, 0, 0, time.Local)
	p := NewDateTimePicker(want)
	got := p.Time()
	if !got.Equal(want) {
		t.Fatalf("Time() = %v, want %v", got, want)
	}
	if p.Summary() != FormatEventDateTime(want) {
		t.Fatalf("Summary() = %q, want %q", p.Summary(), FormatEventDateTime(want))
	}
}

func TestDateTimePickerMoveDayAcrossMonth(t *testing.T) {
	p := NewDateTimePicker(time.Date(2026, 1, 31, 10, 0, 0, 0, time.Local))
	p.Focus()

	p, _ = p.Update(tea.KeyMsg{Type: tea.KeyRight})
	if p.Time().Month() != time.February || p.Time().Day() != 1 {
		t.Fatalf("moving right from Jan 31 = %v, want Feb 1", p.Time())
	}
}

func TestDateTimePickerAdjustMinuteRollover(t *testing.T) {
	p := NewDateTimePicker(time.Date(2026, 6, 1, 23, 45, 0, 0, time.Local))
	p.Focus()
	p.focus = pickerFocusMinute

	p, _ = p.Update(tea.KeyMsg{Type: tea.KeyUp})
	if p.Time().Hour() != 0 || p.Time().Minute() != 0 || p.Time().Day() != 2 {
		t.Fatalf("minute rollover = %v, want Jun 2 00:00", p.Time())
	}
}

func TestDateTimePickerAdjustHour(t *testing.T) {
	p := NewDateTimePicker(time.Date(2026, 6, 1, 10, 30, 0, 0, time.Local))
	p.Focus()
	p.focus = pickerFocusHour

	p, _ = p.Update(tea.KeyMsg{Type: tea.KeyUp})
	if p.Time().Hour() != 11 {
		t.Fatalf("hour after up = %d, want 11", p.Time().Hour())
	}
}

func TestDateTimePickerMonthNavigation(t *testing.T) {
	p := NewDateTimePicker(time.Date(2026, 6, 15, 10, 0, 0, 0, time.Local))
	p.Focus()

	p, _ = p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'.'}})
	if p.viewMonth.Month() != time.July || p.Time().Month() != time.July {
		t.Fatalf("after . = view %v selected %v, want July", p.viewMonth, p.Time())
	}

	p, _ = p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{','}})
	if p.viewMonth.Month() != time.June || p.Time().Month() != time.June {
		t.Fatalf("after , = view %v selected %v, want June", p.viewMonth, p.Time())
	}
}

func TestDateTimePickerTextModeApply(t *testing.T) {
	p := NewDateTimePicker(time.Date(2026, 6, 1, 10, 0, 0, 0, time.Local))
	p.Focus()

	p, _ = p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	if !p.InTextMode() {
		t.Fatal("expected text mode after i")
	}
	p.textInput.SetValue("2026-12-25 09:15")
	p, _ = p.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if p.InTextMode() {
		t.Fatal("expected to leave text mode after enter")
	}
	want := time.Date(2026, 12, 25, 9, 15, 0, 0, time.Local)
	if !p.Time().Equal(want) {
		t.Fatalf("Time() after text apply = %v, want %v", p.Time(), want)
	}
}

func TestDateTimePickerSubFocus(t *testing.T) {
	p := NewDateTimePicker(time.Date(2026, 6, 1, 10, 0, 0, 0, time.Local))
	p.Focus()

	if !p.AdvanceSubFocus() || p.focus != pickerFocusHour {
		t.Fatalf("first advance = focus %v, want hour", p.focus)
	}
	if !p.AdvanceSubFocus() || p.focus != pickerFocusMinute {
		t.Fatalf("second advance = focus %v, want minute", p.focus)
	}
	if p.AdvanceSubFocus() {
		t.Fatal("third advance should return false on minute")
	}
	if !p.RetreatSubFocus() || p.focus != pickerFocusHour {
		t.Fatalf("retreat from minute = focus %v, want hour", p.focus)
	}
}

func TestDateTimePickerTextModeTabPreservesMinutes(t *testing.T) {
	p := NewDateTimePicker(time.Date(2026, 6, 1, 14, 0, 0, 0, time.Local))
	p.Focus()

	p, _ = p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	p.textInput.SetValue("2026-06-01 14:10")
	if !p.AdvanceSubFocus() {
		t.Fatal("expected AdvanceSubFocus from text mode to succeed")
	}
	if p.Time().Minute() != 10 {
		t.Fatalf("minute after tab apply = %d, want 10", p.Time().Minute())
	}
	if p.focus != pickerFocusHour {
		t.Fatalf("focus after tab apply = %v, want hour", p.focus)
	}
}

func TestDateTimePickerTextModeBlurPreservesMinutes(t *testing.T) {
	p := NewDateTimePicker(time.Date(2026, 6, 1, 14, 0, 0, 0, time.Local))
	p.Focus()

	p, _ = p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	p.textInput.SetValue("2026-06-01 14:10")
	p.Blur()
	if p.Time().Minute() != 10 {
		t.Fatalf("minute after blur apply = %d, want 10", p.Time().Minute())
	}
}

func TestDateTimePickerEnterMovesToHour(t *testing.T) {
	p := NewDateTimePicker(time.Date(2026, 6, 1, 10, 0, 0, 0, time.Local))
	p.Focus()

	p, _ = p.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if p.focus != pickerFocusHour {
		t.Fatalf("focus after enter = %v, want hour", p.focus)
	}
}

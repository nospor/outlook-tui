package main

import (
	"path/filepath"
	"testing"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"outlook-tui/filepicker"
)

func testZoxideModel() mainModel {
	zi := textinput.New()
	zi.Placeholder = "Filter zoxide directories..."
	return mainModel{
		state:       stateFileBrowse,
		filepicker:  filepicker.New(),
		zoxideInput: zi,
		width:       80,
		height:      24,
	}
}

func TestFilePickerZoxideOpenAndClose(t *testing.T) {
	m := testZoxideModel()

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'z'}})
	m = updated.(mainModel)
	if cmd == nil {
		t.Fatal("expected load cmd")
	}
	if !m.filePickerZoxideMode {
		t.Fatal("expected zoxide mode")
	}
	if m.state != stateFileBrowse {
		t.Fatalf("state = %v, want file browse", m.state)
	}

	updated, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(mainModel)
	if cmd != nil {
		t.Fatalf("expected no cmd on close, got %v", cmd)
	}
	if m.filePickerZoxideMode {
		t.Fatal("expected zoxide mode off")
	}
	if m.state != stateFileBrowse {
		t.Fatal("file picker should stay open")
	}
}

func TestJumpFilePickerToDirectory(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	m := testZoxideModel()
	m.filePickerZoxideMode = true

	m, cmd := m.jumpFilePickerToDirectory(dir)
	if cmd == nil {
		t.Fatal("expected readDir cmd")
	}
	if m.filePickerZoxideMode {
		t.Fatal("zoxide overlay should close")
	}
	if m.filepicker.CurrentDirectory != dir {
		t.Fatalf("directory = %q", m.filepicker.CurrentDirectory)
	}
}

func TestJumpFilePickerToMissingDirectory(t *testing.T) {
	m := testZoxideModel()
	m.filePickerZoxideMode = true

	m, cmd := m.jumpFilePickerToDirectory(filepath.Join(t.TempDir(), "missing"))
	if cmd != nil {
		t.Fatal("expected no cmd")
	}
	if !m.filePickerZoxideMode {
		t.Fatal("overlay should stay open on error")
	}
	if m.filePickerZoxideError == "" {
		t.Fatal("expected error message")
	}
}

func TestApplyZoxideDirsLoaded(t *testing.T) {
	m := testZoxideModel()
	m.filePickerZoxideMode = true
	m.filePickerZoxideLoading = true
	m.zoxideInput.SetValue("proj")

	updated, _ := m.Update(zoxideDirsLoadedMsg{
		Query: "proj",
		Paths: []string{"/tmp/a", "/tmp/b"},
	})
	m = updated.(mainModel)
	if m.filePickerZoxideLoading {
		t.Fatal("expected loading off")
	}
	if len(m.filePickerZoxidePaths) != 2 {
		t.Fatalf("paths = %v", m.filePickerZoxidePaths)
	}
	if m.filePickerZoxideError != "" {
		t.Fatalf("unexpected error %q", m.filePickerZoxideError)
	}

	m.filePickerZoxideLoading = true
	updated, _ = m.Update(zoxideDirsLoadedMsg{
		Query: "stale",
		Paths: []string{"/tmp/other"},
	})
	m = updated.(mainModel)
	if !m.filePickerZoxideLoading {
		t.Fatal("stale result should be ignored")
	}
	if len(m.filePickerZoxidePaths) != 2 {
		t.Fatalf("paths changed by stale result: %v", m.filePickerZoxidePaths)
	}
}

func TestFilePickerZoxideQTypesIntoFilter(t *testing.T) {
	m := testZoxideModel()
	m.filePickerZoxideMode = true

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	m = updated.(mainModel)
	if m.state != stateFileBrowse {
		t.Fatal("q should not close the file picker while zoxide overlay is open")
	}
	if !m.filePickerZoxideMode {
		t.Fatal("overlay should stay open")
	}
	if m.zoxideInput.Value() != "q" {
		t.Fatalf("filter = %q", m.zoxideInput.Value())
	}
}

func TestFilePickerZoxideEnterJumps(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	m := testZoxideModel()
	m.filePickerZoxideMode = true
	m.filePickerZoxidePaths = []string{dir}
	m.filePickerZoxideSelected = 0

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(mainModel)
	if cmd == nil {
		t.Fatal("expected readDir cmd")
	}
	if m.filePickerZoxideMode {
		t.Fatal("overlay should close")
	}
	if m.filepicker.CurrentDirectory != dir {
		t.Fatalf("directory = %q", m.filepicker.CurrentDirectory)
	}
}

package main

import (
	"testing"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

func TestSortFolders(t *testing.T) {
	folders := []MailFolder{
		{ID: "1", DisplayName: "Drafts", WellKnownName: "drafts"},
		{ID: "2", DisplayName: "Inbox", WellKnownName: "inbox"},
		{ID: "3", DisplayName: "Archive", WellKnownName: "archive"},
		{ID: "4", DisplayName: "Sent Items", WellKnownName: "sentitems"},
		{ID: "5", DisplayName: "Junk Email", WellKnownName: "junkemail"},
	}

	sorted := sortFolders(folders)

	if len(sorted) != len(folders) {
		t.Fatalf("expected %d folders, got %d", len(folders), len(sorted))
	}

	if sorted[0].DisplayName != "Inbox" {
		t.Errorf("expected first folder to be 'Inbox', got '%s'", sorted[0].DisplayName)
	}

	if sorted[1].DisplayName != "Sent Items" {
		t.Errorf("expected second folder to be 'Sent Items', got '%s'", sorted[1].DisplayName)
	}

	expectedRest := []string{"Drafts", "Archive", "Junk Email"}
	for i, name := range expectedRest {
		actualName := sorted[i+2].DisplayName
		if actualName != name {
			t.Errorf("expected folder at index %d to be '%s', got '%s'", i+2, name, actualName)
		}
	}
}

func TestSortFolders_NoInboxOrSent(t *testing.T) {
	folders := []MailFolder{
		{ID: "1", DisplayName: "Drafts", WellKnownName: "drafts"},
		{ID: "3", DisplayName: "Archive", WellKnownName: "archive"},
	}

	sorted := sortFolders(folders)

	if len(sorted) != 2 {
		t.Fatalf("expected 2 folders, got %d", len(sorted))
	}

	if sorted[0].DisplayName != "Drafts" || sorted[1].DisplayName != "Archive" {
		t.Errorf("expected original order, got %v", sorted)
	}
}

func TestSortFolders_CaseInsensitiveAndFallback(t *testing.T) {
	folders := []MailFolder{
		{ID: "1", DisplayName: "drafts", WellKnownName: ""},
		{ID: "2", DisplayName: "INBOX", WellKnownName: ""},
		{ID: "3", DisplayName: "Boîte d'envoi", WellKnownName: "sentitems"}, // localized but wellKnownName is sentitems
	}

	sorted := sortFolders(folders)

	if len(sorted) != 3 {
		t.Fatalf("expected 3 folders, got %d", len(sorted))
	}

	if sorted[0].DisplayName != "INBOX" {
		t.Errorf("expected first folder to be 'INBOX', got '%s'", sorted[0].DisplayName)
	}

	if sorted[1].DisplayName != "Boîte d'envoi" {
		t.Errorf("expected second folder to be 'Boîte d'envoi', got '%s'", sorted[1].DisplayName)
	}

	if sorted[2].DisplayName != "drafts" {
		t.Errorf("expected third folder to be 'drafts', got '%s'", sorted[2].DisplayName)
	}
}

func TestFormatBodyContent(t *testing.T) {
	// Force Lipgloss to output ANSI colors in headless test environments
	lipgloss.SetColorProfile(termenv.TrueColor)

	tests := []struct {
		input    string
		expected string
	}{
		{
			input:    "ofpo <b>dfdfd</b>&nbsp; ddfdf",
			expected: "ofpo \x1b[1mdfdfd\x1b[22m  ddfdf",
		},
		{
			input:    "<p>Line 1</p><br/>Line 2 &amp; Line 3",
			expected: "Line 1\n\nLine 2 & Line 3",
		},
		{
			input:    "&lt;tag&gt; \"quote\" and 'apos' &#39;",
			expected: "<tag> \"quote\" and 'apos' '",
		},
		{
			input:    "Some <STRONG style=\"font-weight:bold;\">bold</strong   > and <EM>italicized</EM> text with <u class='underline'>underline</u>.",
			expected: "Some \x1b[1mbold\x1b[22m and \x1b[3mitalicized\x1b[23m text with \x1b[4munderline\x1b[24m.",
		},
		{
			input:    "How does Ben specify that?\n\nFrom: Dana Glaser &lt;dana.glaser@adwanted.com&gt;\nSent: 24 June 2026 20:50\nTo: Robert Nodzewski\n\nHi All,\nI spoke to Ben",
			expected: "How does Ben specify that?\n\n\x1b[38;2;166;173;200mFrom: Dana Glaser <dana.glaser@adwanted.com>\x1b[0m\n\x1b[38;2;166;173;200mSent: 24 June 2026 20:50\x1b[0m\n\x1b[38;2;166;173;200mTo: Robert Nodzewski\x1b[0m\n\n\x1b[38;2;166;173;200mHi All,\x1b[0m\n\x1b[38;2;166;173;200mI spoke to Ben\x1b[0m",
		},
		{
			input:    "New line\n&gt; Quoted line\nAnother new line",
			expected: "New line\n\x1b[38;2;166;173;200m> Quoted line\x1b[0m\nAnother new line",
		},
		{
			input:    "Visit https://example.com/foo for more info.",
			expected: "Visit \x1b[38;2;137;180;250;4mhttps://example.com/foo\x1b[24;39m for more info.",
		},
		{
			input:    "Check (https://google.com) or go to http://yahoo.com.",
			expected: "Check (\x1b[38;2;137;180;250;4mhttps://google.com\x1b[24;39m) or go to \x1b[38;2;137;180;250;4mhttp://yahoo.com\x1b[24;39m.",
		},
		{
			input:    "<a href=\"https://github.com\">GitHub website</a>",
			expected: "GitHub website (\x1b[38;2;137;180;250;4mhttps://github.com\x1b[24;39m)",
		},
		{
			input:    "<a href=\"https://github.com\">https://github.com</a>",
			expected: "\x1b[38;2;137;180;250;4mhttps://github.com\x1b[24;39m",
		},
		{
			input:    "<a href=\"mailto:test@example.com\">Email Us</a>",
			expected: "Email Us (test@example.com)",
		},
		{
			input:    "> Please visit https://example.com/.",
			expected: "\x1b[38;2;166;173;200m> Please visit \x1b[38;2;137;180;250;4mhttps://example.com/\x1b[24;38;2;166;173;200m.\x1b[0m",
		},
	}

	for _, tt := range tests {
		actual := formatBodyContent(tt.input)
		if actual != tt.expected {
			t.Errorf("formatBodyContent(%q) = %q; expected %q", tt.input, actual, tt.expected)
		}
	}
}

func TestStripANSICodes(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{
			input:    "Some \x1b[1mbold\x1b[22m and \x1b[3mitalicized\x1b[23m text",
			expected: "Some bold and italicized text",
		},
		{
			input:    "Plain text",
			expected: "Plain text",
		},
	}

	for _, tt := range tests {
		actual := stripANSICodes(tt.input)
		if actual != tt.expected {
			t.Errorf("stripANSICodes(%q) = %q; expected %q", tt.input, actual, tt.expected)
		}
	}
}

func TestLipglossWrap(t *testing.T) {
	text := "Hi All,\nI spoke to Ben, but there does not seem to be a simple place in Java DMS or ADAM where you can see who our paid advertisers are, so I have attached the list we spoke about earlier."
	wrapped := wrapText(text, 40)
	t.Logf("Wrapped:\n%q", wrapped)

	// Test with ANSI code
	ansiText := "Some \x1b[1mbold\x1b[22m text that is also quite long and should wrap properly."
	ansiWrapped := wrapText(ansiText, 20)
	t.Logf("ANSI Wrapped:\n%q", ansiWrapped)
}

func TestJKNavigation(t *testing.T) {
	// Initialize a basic mainModel
	msg1 := Message{
		ID:             "msg1ID",
		ConversationID: "conv1ID",
		Subject:        "Hello",
		IsRead:         true,
		Body:           ItemBody{Content: "Body 1"},
	}
	msg2 := Message{
		ID:             "msg2ID",
		ConversationID: "conv1ID",
		Subject:        "Re: Hello",
		IsRead:         true,
		Body:           ItemBody{Content: "Body 2"},
	}

	m := mainModel{
		state:      stateMain,
		activePane: paneFolders,
		virtualList: []MessageListItem{
			{ThreadIdx: 0, MemberIdx: 0, IsHeader: false},
			{ThreadIdx: 0, MemberIdx: 1, IsHeader: false},
		},
		threadGroups: []ThreadGroup{
			{
				ConversationID: "conv1ID",
				Members:        []Message{msg1, msg2},
			},
		},
		virtualSelected: 0,
		width:           100,
		height:          30,
	}

	// 1. In paneFolders, pressing K should do nothing since virtualSelected is already 0
	updatedModelInterface, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("K")})
	updatedModel := updatedModelInterface.(mainModel)
	if updatedModel.virtualSelected != 0 {
		t.Errorf("expected virtualSelected to remain 0, got %d", updatedModel.virtualSelected)
	}

	// 2. In paneFolders, pressing J should navigate down in Messages pane (to index 1)
	updatedModelInterface, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("J")})
	updatedModel = updatedModelInterface.(mainModel)
	if updatedModel.virtualSelected != 1 {
		t.Errorf("expected virtualSelected to be 1, got %d", updatedModel.virtualSelected)
	}

	// 3. In paneMessages, pressing K or J should scroll message detail in Details pane
	// Let's first move focus to paneMessages
	m.activePane = paneMessages
	m.virtualSelected = 0

	// Initialize viewport with some content so it is scrollable
	m.detailViewport.Width = 20
	m.detailViewport.Height = 5
	m.detailViewport.SetContent("1\n2\n3\n4\n5\n6\n7\n8\n9\n10\n")
	m.detailViewport.YOffset = 3

	// Pressing K (scroll up)
	updatedModelInterface, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("K")})
	updatedModel = updatedModelInterface.(mainModel)
	// We expect the viewport to scroll up by 1 line, meaning YOffset decreases by 1
	if updatedModel.detailViewport.YOffset != 2 {
		t.Errorf("expected detailViewport.YOffset to be 2, got %d", updatedModel.detailViewport.YOffset)
	}

	// Pressing J (scroll down)
	updatedModelInterface, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("J")})
	updatedModel = updatedModelInterface.(mainModel)
	// We expect the viewport to scroll down by 1 line, meaning YOffset increases by 1
	if updatedModel.detailViewport.YOffset != 4 {
		t.Errorf("expected detailViewport.YOffset to be 4, got %d", updatedModel.detailViewport.YOffset)
	}

	// 4. In paneFolders, pressing Space should toggle collapsed state of the thread
	m.activePane = paneFolders
	m.collapsedThreads = make(map[string]bool)
	m.collapsedThreads["conv1ID"] = true

	updatedModelInterface, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(" ")})
	updatedModel = updatedModelInterface.(mainModel)
	if updatedModel.collapsedThreads["conv1ID"] {
		t.Errorf("expected thread conv1ID to be uncollapsed, but it remains collapsed")
	}
}

func TestComposeContactSuggestions(t *testing.T) {
	m := mainModel{
		state:       stateCompose,
		composeStep: 0,
		contacts: []Contact{
			{Name: "Alice Smith", Address: "alice@example.com"},
			{Name: "Bob Jones", Address: "bob@example.com"},
			{Name: "Charlie Brown", Address: "charlie@example.com"},
		},
		config: Config{UseSQLite: 1},
	}
	m.composeTo = textinput.New()
	
	// Simulate typing "b"
	m.composeTo.SetValue("b")
	m.updateFilteredContacts()

	if len(m.filteredContacts) != 2 {
		t.Fatalf("expected 2 filtered contacts (Bob, Charlie), got %d", len(m.filteredContacts))
	}
	if m.filteredContacts[0].Name != "Bob Jones" {
		t.Errorf("expected first matching contact to be Bob Jones, got %s", m.filteredContacts[0].Name)
	}

	// Pressing Down should move selection to second option (Charlie)
	updatedInterface, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	updated := updatedInterface.(mainModel)
	if updated.contactsSelected != 1 {
		t.Errorf("expected contactsSelected to be 1, got %d", updated.contactsSelected)
	}

	// Pressing Enter should autofill Charlie
	updatedInterface, _ = updated.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated = updatedInterface.(mainModel)
	if updated.composeTo.Value() != "Charlie Brown <charlie@example.com>, " {
		t.Errorf("expected autofilled value 'Charlie Brown <charlie@example.com>, ', got %q", updated.composeTo.Value())
	}
	if len(updated.filteredContacts) != 0 {
		t.Errorf("expected suggestions list to be cleared after selection, but got %d items", len(updated.filteredContacts))
	}
}

func TestComposeCcContactSuggestions(t *testing.T) {
	m := mainModel{
		state:       stateCompose,
		composeStep: 1, // CC field
		contacts: []Contact{
			{Name: "Alice Smith", Address: "alice@ex.com"},
			{Name: "Bob Jones", Address: "bob@ex.com"},
			{Name: "Charlie Brown", Address: "charlie@ex.com"},
		},
		config: Config{UseSQLite: 1},
	}
	m.composeCc = textinput.New()
	
	// Simulate typing "a"
	m.composeCc.SetValue("a")
	m.updateFilteredContacts()

	if len(m.filteredContacts) != 2 {
		t.Fatalf("expected 2 filtered contacts (Alice, Charlie), got %d", len(m.filteredContacts))
	}
	if m.filteredContacts[0].Name != "Alice Smith" {
		t.Errorf("expected first matching contact to be Alice Smith, got %s", m.filteredContacts[0].Name)
	}

	// Pressing Down should move selection to second option (Charlie)
	updatedInterface, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	updated := updatedInterface.(mainModel)
	if updated.contactsSelected != 1 {
		t.Errorf("expected contactsSelected to be 1, got %d", updated.contactsSelected)
	}

	// Pressing Enter should autofill Charlie into CC
	updatedInterface, _ = updated.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated = updatedInterface.(mainModel)
	if updated.composeCc.Value() != "Charlie Brown <charlie@ex.com>, " {
		t.Errorf("expected autofilled value 'Charlie Brown <charlie@ex.com>, ', got %q", updated.composeCc.Value())
	}
	if len(updated.filteredContacts) != 0 {
		t.Errorf("expected suggestions list to be cleared after selection, but got %d items", len(updated.filteredContacts))
	}
}

func TestExtractURLsFromMainMessage(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:     "No URLs",
			input:    "Hello world, this is a message with no links.",
			expected: nil,
		},
		{
			name:     "Single plain URL",
			input:    "Check this out: https://example.com/foo. It's cool.",
			expected: []string{"https://example.com/foo"},
		},
		{
			name:     "Anchor tag URL",
			input:    "Visit <a href=\"https://github.com\">GitHub</a> website.",
			expected: []string{"https://github.com"},
		},
		{
			name:     "Multiple URLs with duplicates",
			input:    "Link 1: https://google.com, Link 2: http://yahoo.com, Link 3: https://google.com",
			expected: []string{"https://google.com", "http://yahoo.com"},
		},
		{
			name:     "Quoted lines prefixed with >",
			input:    "Please use this link: https://active.com\n\n> Old conversation:\n> Go to https://quoted.com",
			expected: []string{"https://active.com"},
		},
		{
			name:     "Original message block headers",
			input:    "Main message: https://new.com\n\n-----Original Message-----\nFrom: test@example.com\nSent: 2026\nTo: user\n\nQuoted: https://old.com",
			expected: []string{"https://new.com"},
		},
		{
			name:     "Punctuation trimming check",
			input:    "Urls: (https://a.com), [https://b.com], {https://c.com}, https://d.com/!",
			expected: []string{"https://a.com", "https://b.com", "https://c.com", "https://d.com/"},
		},
		{
			name:     "HTML divs structure",
			input:    "Main link: https://new.com<div>-----Original Message-----</div><div>From: test@example.com</div><div>Sent: 2026</div><div>To: user</div><div>Quoted: https://old.com</div>",
			expected: []string{"https://new.com"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual := extractURLsFromMainMessage(tt.input)
			if len(actual) != len(tt.expected) {
				t.Fatalf("expected %d urls, got %d: %v", len(tt.expected), len(actual), actual)
			}
			for i := range actual {
				if actual[i] != tt.expected[i] {
					t.Errorf("at index %d: expected %q, got %q", i, tt.expected[i], actual[i])
				}
			}
		})
	}
}




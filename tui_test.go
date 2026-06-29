package main

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

func TestSortFolders_InboxSentFirst(t *testing.T) {
	folders := []MailFolder{
		{ID: "1", DisplayName: "Drafts", WellKnownName: "drafts"},
		{ID: "2", DisplayName: "Inbox", WellKnownName: "inbox"},
		{ID: "3", DisplayName: "Archive", WellKnownName: "archive"},
		{ID: "4", DisplayName: "Sent Items", WellKnownName: "sentitems"},
		{ID: "5", DisplayName: "Junk Email", WellKnownName: "junkemail"},
	}

	sorted := sortFolders(folders, nil, nil)

	if len(sorted) != len(folders)+1 {
		t.Fatalf("expected %d folders, got %d", len(folders)+1, len(sorted))
	}

	if sorted[0].DisplayName != "Favorites" {
		t.Errorf("expected first folder to be 'Favorites', got '%s'", sorted[0].DisplayName)
	}

	if sorted[1].DisplayName != "Inbox" {
		t.Errorf("expected second folder to be 'Inbox', got '%s'", sorted[1].DisplayName)
	}

	if sorted[2].DisplayName != "Sent Items" {
		t.Errorf("expected third folder to be 'Sent Items', got '%s'", sorted[2].DisplayName)
	}

	expectedRest := []string{"Drafts", "Archive", "Junk Email"}
	for i, name := range expectedRest {
		actualName := sorted[i+3].DisplayName
		if actualName != name {
			t.Errorf("expected folder at index %d to be '%s', got '%s'", i+3, name, actualName)
		}
	}
}

func TestSortFolders_NoInboxOrSent(t *testing.T) {
	folders := []MailFolder{
		{ID: "1", DisplayName: "Drafts", WellKnownName: "drafts"},
		{ID: "3", DisplayName: "Archive", WellKnownName: "archive"},
	}

	sorted := sortFolders(folders, nil, nil)

	if len(sorted) != 3 {
		t.Fatalf("expected 3 folders, got %d", len(sorted))
	}

	if sorted[0].DisplayName != "Favorites" {
		t.Errorf("expected first folder to be 'Favorites', got '%s'", sorted[0].DisplayName)
	}

	if sorted[1].DisplayName != "Drafts" || sorted[2].DisplayName != "Archive" {
		t.Errorf("expected original order starting from index 1, got %v", sorted)
	}
}

func TestSortFolders_CaseInsensitiveAndFallback(t *testing.T) {
	folders := []MailFolder{
		{ID: "1", DisplayName: "drafts", WellKnownName: ""},
		{ID: "2", DisplayName: "INBOX", WellKnownName: ""},
		{ID: "3", DisplayName: "Boîte d'envoi", WellKnownName: "sentitems"}, // localized but wellKnownName is sentitems
	}

	sorted := sortFolders(folders, nil, nil)

	if len(sorted) != 4 {
		t.Fatalf("expected 4 folders, got %d", len(sorted))
	}

	if sorted[0].DisplayName != "Favorites" {
		t.Errorf("expected first folder to be 'Favorites', got '%s'", sorted[0].DisplayName)
	}

	if sorted[1].DisplayName != "INBOX" {
		t.Errorf("expected second folder to be 'INBOX', got '%s'", sorted[1].DisplayName)
	}

	if sorted[2].DisplayName != "Boîte d'envoi" {
		t.Errorf("expected third folder to be 'Boîte d'envoi', got '%s'", sorted[2].DisplayName)
	}

	if sorted[3].DisplayName != "drafts" {
		t.Errorf("expected fourth folder to be 'drafts', got '%s'", sorted[3].DisplayName)
	}
}

func TestSortFolders_Excluded(t *testing.T) {
	folders := []MailFolder{
		{ID: "1", DisplayName: "Drafts", WellKnownName: "drafts"},
		{ID: "2", DisplayName: "Inbox", WellKnownName: "inbox"},
		{ID: "3", DisplayName: "Junk Email", WellKnownName: "junkemail"},
		{ID: "4", DisplayName: "RSS Feeds", WellKnownName: "rssfeeds"},
	}

	excluded := []string{"Junk Email", "rssfeeds"}
	sorted := sortFolders(folders, excluded, nil)

	if len(sorted) != 3 {
		t.Fatalf("expected 3 folders, got %d", len(sorted))
	}

	if sorted[0].DisplayName != "Favorites" {
		t.Errorf("expected first folder to be 'Favorites', got '%s'", sorted[0].DisplayName)
	}

	if sorted[1].DisplayName != "Inbox" || sorted[2].DisplayName != "Drafts" {
		t.Errorf("expected Favorites, Inbox, and Drafts, got %v", sorted)
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
			expected: "GitHub website (\x1b[38;2;137;180;250;4mhttps://github.com\x1b[24;39m) ",
		},
		{
			input:    "<a href=\"https://github.com\">https://github.com</a>",
			expected: "\x1b[38;2;137;180;250;4mhttps://github.com\x1b[24;39m",
		},
		{
			input:    "<a href=\"mailto:test@example.com\">Email Us</a>",
			expected: "Email Us (test@example.com) ",
		},
		{
			input:    "> Please visit https://example.com/.",
			expected: "\x1b[38;2;166;173;200m> Please visit \x1b[38;2;137;180;250;4mhttps://example.com/\x1b[24;38;2;166;173;200m.\x1b[0m",
		},
		{
			input:    "<img src=\"cid:image001.png@01D7\">",
			expected: "\x1b[1;38;2;203;166;247m[image: image001.png]\x1b[0m",
		},
		{
			input:    "<img src=\"cid:image002.jpg\" alt=\"Landscape Image\">",
			expected: "\x1b[1;38;2;203;166;247m[image: Landscape Image]\x1b[0m",
		},
		{
			input:    "<img src=\"https://example.com/external.png\">",
			expected: "\x1b[1;38;2;203;166;247m[image]\x1b[0m",
		},
		{
			input:    "Cronic detected failure or error output for the command:\nbash /opt/deploy/check_error_log.sh /var/log/nginx/barb\n\nRESULT CODE: 1\n\nERROR OUTPUT:\n< 2026/06/29 10:05:58 [error] 10792#10792: *5205807 FastCGI sent in stderr",
			expected: "Cronic detected failure or error output for the command:\nbash /opt/deploy/check_error_log.sh /var/log/nginx/barb\n\nRESULT CODE: 1\n\nERROR OUTPUT:\n< 2026/06/29 10:05:58 [error] 10792#10792: *5205807 FastCGI sent in stderr",
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

func TestThreadToggleSelectionPreservation(t *testing.T) {
	msg1 := Message{ID: "msg1", ConversationID: "conv1ID"}
	msg2 := Message{ID: "msg2", ConversationID: "conv1ID"}

	m := mainModel{
		state:            stateMain,
		activePane:       paneMessages,
		collapsedThreads: make(map[string]bool),
		threadGroups: []ThreadGroup{
			{
				ConversationID: "conv1ID",
				Members:        []Message{msg1, msg2},
			},
		},
		width:  100,
		height: 30,
	}

	// 1. Initially, build the virtual list and select one of the members (scrolled down in the thread)
	// (Note: collapsedThreads["conv1ID"] is false by default, so it's expanded)
	m.buildVirtualList()

	// Check the virtual list contents:
	// Index 0: Header of Thread 0 (IsHeader: true, ThreadIdx: 0, MemberIdx: -1)
	// Index 1: Member 0 of Thread 0 (IsHeader: false, ThreadIdx: 0, MemberIdx: 0)
	// Index 2: Member 1 of Thread 0 (IsHeader: false, ThreadIdx: 0, MemberIdx: 1)
	if len(m.virtualList) != 3 {
		t.Fatalf("expected virtualList to have 3 items, got %d", len(m.virtualList))
	}

	// Select the second member (index 2)
	m.virtualSelected = 2

	// Press Space to toggle (collapse) the thread
	updatedModelInterface, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(" ")})
	updatedModel := updatedModelInterface.(mainModel)

	// Check if the thread is collapsed
	if !updatedModel.collapsedThreads["conv1ID"] {
		t.Errorf("expected thread to be collapsed")
	}

	// The virtual list should now only contain the header row (length 1)
	if len(updatedModel.virtualList) != 1 {
		t.Fatalf("expected collapsed virtualList to have 1 item, got %d", len(updatedModel.virtualList))
	}

	// The selection should have moved to the header row (index 0) of the thread
	if updatedModel.virtualSelected != 0 {
		t.Errorf("expected virtualSelected to be 0 (the header row), got %d", updatedModel.virtualSelected)
	}

	// Press Space again to expand the thread
	updatedModelInterface2, _ := updatedModel.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(" ")})
	updatedModel2 := updatedModelInterface2.(mainModel)

	// Check if the thread is expanded
	if updatedModel2.collapsedThreads["conv1ID"] {
		t.Errorf("expected thread to be expanded")
	}

	// Selection should remain on the header row (index 0)
	if updatedModel2.virtualSelected != 0 {
		t.Errorf("expected virtualSelected to stay at 0 after expanding, got %d", updatedModel2.virtualSelected)
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

func TestExtractYouTrackURLs(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:     "No YouTrack URLs",
			input:    "Hello world, check https://google.com and https://github.com",
			expected: nil,
		},
		{
			name:     "One YouTrack URL",
			input:    "Please check this issue: https://youtrack.adwanted.com/issue/MTEL-21797. Thanks!",
			expected: []string{"https://youtrack.adwanted.com/issue/MTEL-21797"},
		},
		{
			name:     "Multiple YouTrack URLs with duplicates, params, and non-issues",
			input: `
				Check:
				1. https://srds.youtrack.cloud/issue/SR-15
				2. https://srds.youtrack.cloud/api/files/12-4?sign=MTc4
				3. https://srds.youtrack.cloud/issue/SR-15#focus=Comments-7-5.0-0
				4. https://srds.youtrack.cloud/issue/SR-15?replyTo=7-5
				5. https://srds.youtrack.cloud/api/unsubscribe?token=MTc4ND
				6. https://srds.youtrack.cloud/users/58b4dd7b-9851?tab=notifications
				7. https://youtrack.adwanted.com/issue/MTEL-21797
				8. https://srds.youtrack.cloud/issue/SR-15/logos
			`,
			expected: []string{"https://srds.youtrack.cloud/issue/SR-15", "https://youtrack.adwanted.com/issue/MTEL-21797"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual := extractYouTrackURLs(tt.input)
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

func TestLayout2Heights(t *testing.T) {
	m := mainModel{
		state:      stateMain,
		activePane: paneMessages,
		config:     Config{Layout: 2},
		width:      100,
		height:     30,
	}
	m.folders = []MailFolder{{DisplayName: "Inbox", ID: "inbox"}}
	msg := Message{
		ID:             "msg1ID",
		ConversationID: "conv1ID",
		Subject:        "Hello",
		IsRead:         true,
		Body:           ItemBody{Content: "Body 1"},
	}
	m.threadGroups = []ThreadGroup{
		{
			ConversationID: "conv1ID",
			Members:        []Message{msg},
		},
	}
	m.virtualList = []MessageListItem{
		{ThreadIdx: 0, MemberIdx: 0, IsHeader: true},
	}
	m.virtualSelected = 0

	// We want to test under three conditions:
	// 1. detailMessage is nil
	// 2. detailMessage has a short subject
	// 3. detailMessage has a long subject
	// 4. Virtual list has different sizes (1, 5, 10 items)

	runLayoutCheck := func(desc string, detailMsg *Message, listSize int) (int, int, int) {
		m.detailMessage = detailMsg
		
		// Setup virtual list of specified size
		m.virtualList = nil
		for i := 0; i < listSize; i++ {
			m.virtualList = append(m.virtualList, MessageListItem{ThreadIdx: 0, MemberIdx: 0, IsHeader: true})
		}
		
		m = m.updateViewportSize()
		
		// Replicate layout calculations from renderLayout2
		totalHeight := m.height - 6
		if totalHeight < 10 {
			totalHeight = 10
		}
		foldersHeight := totalHeight * 30 / 100
		if foldersHeight < 4 {
			foldersHeight = 4
		}
		messagesHeight := totalHeight - foldersHeight - 4
		if messagesHeight < 4 {
			messagesHeight = 4
		}
		leftColInner := 46

		foldersView := m.renderFoldersViewWide(foldersHeight, leftColInner)
		messagesView := m.renderMessagesViewWide(messagesHeight, leftColInner)
		detailView := m.renderDetailView()

		fStyle := paneNormalStyle
		mStyle := paneNormalStyle
		dStyle := paneNormalStyle

		fView := fStyle.Width(leftColInner).Height(foldersHeight).Render(foldersView)
		mView := mStyle.Width(leftColInner).Height(messagesHeight).Render(messagesView)
		dView := dStyle.Width(m.width - 54).Height(totalHeight - 2).Render(cropLines(detailView, totalHeight-2))

		fH := lipgloss.Height(fView)
		mH := lipgloss.Height(mView)
		dH := lipgloss.Height(dView)
		
		nlCount := strings.Count(dView, "\n")
		
		t.Logf("%s (list size %d): fView=%d, mView=%d, dView=%d, dView newlines=%d",
			desc, listSize, fH, mH, dH, nlCount)
		return fH, mH, dH
	}

	// 1. Nil message, list size 1
	fH1, mH1, dH1 := runLayoutCheck("nil msg", nil, 1)

	// 2. Short subject message, list size 5
	fH2, mH2, dH2 := runLayoutCheck("short msg", &msg, 5)

	// 3. Long subject message, list size 10
	longMsg := msg
	longMsg.Subject = "This is an extremely long subject line that will wrap to multiple lines and increase the height of the metadata block significantly in the detail view."
	fH3, mH3, dH3 := runLayoutCheck("long msg", &longMsg, 10)

	if fH1 != fH2 || fH2 != fH3 || mH1 != mH2 || mH2 != mH3 || dH1 != dH2 || dH2 != dH3 {
		t.Errorf("Heights are not constant across selections/list sizes!")
	}
}

func TestComposeCtrlS(t *testing.T) {
	m := mainModel{
		state:       stateCompose,
		composeStep: 3,
		config:      Config{UseSQLite: 0},
	}
	m.composeTo = textinput.New()
	m.composeCc = textinput.New()
	m.composeSubject = textinput.New()
	m.composeBody = textarea.New()

	m.composeTo.SetValue("test@example.com")
	m.composeCc.SetValue("")
	m.composeSubject.SetValue("Test Subject")
	m.composeBody.SetValue("Test Body")

	// KeyMsg representing ctrl+s
	keyMsg := tea.KeyMsg{
		Type:  tea.KeyCtrlS,
		Runes: []rune("\x13"),
	}

	updatedModelInterface, cmd := m.Update(keyMsg)
	updated := updatedModelInterface.(mainModel)

	if updated.statusMsg != "Sending email..." {
		t.Errorf("expected statusMsg to be 'Sending email...', got %q", updated.statusMsg)
	}

	if cmd == nil {
		t.Fatalf("expected a command to be returned, got nil")
	}
}

func TestReloadFolderKey(t *testing.T) {
	m := mainModel{
		state: stateMain,
		folders: []MailFolder{
			{ID: "folder1", DisplayName: "Inbox"},
		},
		selectedFolder: 0,
	}

	keyMsg := tea.KeyMsg{
		Type:  tea.KeyRunes,
		Runes: []rune("r"),
	}

	updatedModelInterface, cmd := m.Update(keyMsg)
	updated := updatedModelInterface.(mainModel)

	expectedMsg := "Reloading messages for Inbox..."
	if updated.statusMsg != expectedMsg {
		t.Errorf("expected statusMsg to be %q, got %q", expectedMsg, updated.statusMsg)
	}

	if cmd == nil {
		t.Fatalf("expected a command to be returned, got nil")
	}
}

func TestParseAddressStringToRecipients(t *testing.T) {
	// Test empty address string returns non-nil slice
	resEmpty := parseAddressStringToRecipients("")
	if resEmpty == nil {
		t.Errorf("expected non-nil slice for empty string, got nil")
	}
	if len(resEmpty) != 0 {
		t.Errorf("expected length of 0 for empty string, got %d", len(resEmpty))
	}

	// Test whitespaces
	resSpaces := parseAddressStringToRecipients("   ,  ")
	if resSpaces == nil {
		t.Errorf("expected non-nil slice for spaces, got nil")
	}
	if len(resSpaces) != 0 {
		t.Errorf("expected length of 0 for spaces, got %d", len(resSpaces))
	}

	// Test valid address parsing
	resValid := parseAddressStringToRecipients("John Doe <john@example.com>, jane@example.com")
	if len(resValid) != 2 {
		t.Fatalf("expected 2 recipients, got %d", len(resValid))
	}
	if resValid[0].EmailAddress.Name != "John Doe" || resValid[0].EmailAddress.Address != "john@example.com" {
		t.Errorf("incorrect parse for recipient 0: %v", resValid[0])
	}
	if resValid[1].EmailAddress.Name != "" || resValid[1].EmailAddress.Address != "jane@example.com" {
		t.Errorf("incorrect parse for recipient 1: %v", resValid[1])
	}
}

func TestUndeleteKey(t *testing.T) {
	m := mainModel{
		state: stateMain,
		folders: []MailFolder{
			{ID: "deleteditems", DisplayName: "Deleted Items", WellKnownName: "deleteditems"},
		},
		selectedFolder:  0,
		virtualSelected: 0,
		virtualList: []MessageListItem{
			{ThreadIdx: 0, MemberIdx: 0, IsHeader: false},
		},
		threadGroups: []ThreadGroup{
			{
				Members: []Message{
					{ID: "msg123", Subject: "Trash mail"},
				},
			},
		},
	}

	keyMsg := tea.KeyMsg{
		Type:  tea.KeyRunes,
		Runes: []rune("U"),
	}

	updatedModelInterface, cmd := m.Update(keyMsg)
	updated := updatedModelInterface.(mainModel)

	expectedMsg := "Restoring message to Inbox..."
	if updated.statusMsg != expectedMsg {
		t.Errorf("expected statusMsg to be %q, got %q", expectedMsg, updated.statusMsg)
	}

	if cmd == nil {
		t.Fatalf("expected a command to be returned, got nil")
	}
}

func TestReplyKeyBypassConfirm(t *testing.T) {
	m := mainModel{
		state:     stateMain,
		userEmail: "me@example.com",
		virtualSelected: 0,
		virtualList: []MessageListItem{
			{ThreadIdx: 0, MemberIdx: 0, IsHeader: false},
		},
		threadGroups: []ThreadGroup{
			{
				Members: []Message{
					{
						ID: "msg1",
						From: Recipient{
							EmailAddress: EmailAddress{
								Name:    "Sender Person",
								Address: "sender@example.com",
							},
						},
						ToRecipients: []Recipient{
							{
								EmailAddress: EmailAddress{
									Name:    "Me",
									Address: "me@example.com",
								},
							},
						},
					},
				},
			},
		},
	}

	keyMsg := tea.KeyMsg{
		Type:  tea.KeyRunes,
		Runes: []rune("A"),
	}

	updatedModelInterface, _ := m.Update(keyMsg)
	updated := updatedModelInterface.(mainModel)

	// Since there is only one other person (sender@example.com) besides me@example.com,
	// it should bypass stateReplyConfirm and go straight to stateCompose.
	if updated.state != stateCompose {
		t.Errorf("expected state to be stateCompose, got %v", updated.state)
	}

	if updated.composeIsReplyAll {
		t.Error("expected composeIsReplyAll to be false")
	}

	if !strings.Contains(updated.composeTo.Value(), "sender@example.com") {
		t.Errorf("expected composeTo to contain sender@example.com, got %q", updated.composeTo.Value())
	}
}

func TestReplyKeyShowConfirm(t *testing.T) {
	m := mainModel{
		state:     stateMain,
		userEmail: "me@example.com",
		virtualSelected: 0,
		virtualList: []MessageListItem{
			{ThreadIdx: 0, MemberIdx: 0, IsHeader: false},
		},
		threadGroups: []ThreadGroup{
			{
				Members: []Message{
					{
						ID: "msg2",
						From: Recipient{
							EmailAddress: EmailAddress{
								Name:    "Sender Person",
								Address: "sender@example.com",
							},
						},
						ToRecipients: []Recipient{
							{
								EmailAddress: EmailAddress{
									Name:    "Me",
									Address: "me@example.com",
								},
							},
							{
								EmailAddress: EmailAddress{
									Name:    "Other Person",
									Address: "other@example.com",
								},
							},
						},
					},
				},
			},
		},
	}

	keyMsg := tea.KeyMsg{
		Type:  tea.KeyRunes,
		Runes: []rune("A"),
	}

	updatedModelInterface, _ := m.Update(keyMsg)
	updated := updatedModelInterface.(mainModel)

	// Since there are multiple other people (sender@example.com and other@example.com),
	// it should prompt the user (stateReplyConfirm).
	if updated.state != stateReplyConfirm {
		t.Errorf("expected state to be stateReplyConfirm, got %v", updated.state)
	}
}

func TestHelpKey(t *testing.T) {
	m := mainModel{
		state:  stateMain,
		width:  120,
		height: 40,
	}

	// 1. Pressing '?' should enter stateHelp
	keyMsgHelp := tea.KeyMsg{
		Type:  tea.KeyRunes,
		Runes: []rune("?"),
	}

	updatedModelInterface, _ := m.Update(keyMsgHelp)
	updated := updatedModelInterface.(mainModel)

	if updated.state != stateHelp {
		t.Errorf("expected state to be stateHelp, got %v", updated.state)
	}

	// View rendering should contain HELP & KEYBINDINGS
	renderedView := updated.View()
	if !strings.Contains(renderedView, "HELP & KEYBINDINGS") {
		t.Errorf("expected rendered view to contain 'HELP & KEYBINDINGS', got:\n%s", renderedView)
	}

	// 2. Pressing 'esc' in stateHelp should return to stateMain
	keyMsgEsc := tea.KeyMsg{
		Type: tea.KeyEsc,
	}

	closedModelInterface, _ := updated.Update(keyMsgEsc)
	closed := closedModelInterface.(mainModel)

	if closed.state != stateMain {
		t.Errorf("expected state to return to stateMain, got %v", closed.state)
	}
}

func TestRenderMessagesViewWide_StripNewlines(t *testing.T) {
	m := mainModel{
		state:      stateMain,
		activePane: paneMessages,
		config:     Config{Layout: 2},
		width:      100,
		height:     30,
	}
	m.folders = []MailFolder{{DisplayName: "Inbox", ID: "inbox"}}
	msg := Message{
		ID:             "msg1ID",
		ConversationID: "conv1ID",
		Subject:        "Hello",
		IsRead:         true,
		BodyPreview:    "Line 1\r\nLine 2\nLine 3",
		From:           Recipient{EmailAddress: EmailAddress{Name: "Sender", Address: "sender@example.com"}},
	}
	m.threadGroups = []ThreadGroup{
		{
			ConversationID: "conv1ID",
			Members:        []Message{msg, msg},
		},
	}
	m.virtualList = []MessageListItem{
		{ThreadIdx: 0, MemberIdx: -1, IsHeader: true},
		{ThreadIdx: 0, MemberIdx: 1, IsHeader: false},
	}
	m.virtualSelected = 1

	messagesView := m.renderMessagesViewWide(20, 80)

	if strings.Contains(messagesView, "Line 1\nLine 2") || strings.Contains(messagesView, "Line 1\r\nLine 2") {
		t.Errorf("expected view to have newlines stripped from preview, but found raw newlines. View:\n%s", messagesView)
	}

	if !strings.Contains(messagesView, "Line 1 Line 2 Line 3") {
		t.Errorf("expected view to contain stripped preview 'Line 1 Line 2 Line 3', got:\n%s", messagesView)
	}
}

func TestUpdateFolderUnreadCount(t *testing.T) {
	// Create temporary database for testing favorites / folder ID lookup
	tempDir := t.TempDir()
	t.Setenv("HOME", tempDir)
	tempDB, err := OpenDB()
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}
	defer tempDB.Close()

	// Insert a message so we can test the lookup
	msg := Message{
		ID:             "msg_123",
		ConversationID: "conv_123",
		Subject:        "Test Subject",
		IsRead:         false,
	}
	if err := tempDB.UpsertMessage("inbox", msg); err != nil {
		t.Fatalf("failed to insert test message: %v", err)
	}

	m := mainModel{
		state: stateMain,
		db:    tempDB,
		folders: []MailFolder{
			{ID: "favorites", DisplayName: "Favorites", UnreadItemCount: 0},
			{ID: "inbox", DisplayName: "Inbox", UnreadItemCount: 5},
		},
		selectedFolder: 1, // selected Inbox
	}

	// 1. Mark an unread message as read in a regular folder (Inbox)
	m.updateFolderUnreadCount("msg_123", true, false)
	if m.folders[1].UnreadItemCount != 4 {
		t.Errorf("expected Inbox unread count to be 4, got %d", m.folders[1].UnreadItemCount)
	}

	// 2. Mark a read message as unread in a regular folder (Inbox)
	m.updateFolderUnreadCount("msg_123", false, true)
	if m.folders[1].UnreadItemCount != 5 {
		t.Errorf("expected Inbox unread count to be 5, got %d", m.folders[1].UnreadItemCount)
	}

	// 3. Select the favorites folder and test lookup of original folder
	m.selectedFolder = 0 // selected Favorites
	m.updateFolderUnreadCount("msg_123", true, false)
	if m.folders[1].UnreadItemCount != 4 {
		t.Errorf("expected Inbox unread count to be 4 after reading in Favorites, got %d", m.folders[1].UnreadItemCount)
	}
}

func TestComposeEscapeConfirmation(t *testing.T) {
	// Setup a mainModel in stateCompose
	m := mainModel{
		state:       stateCompose,
		composeStep: 3,
	}
	m.composeBody = textarea.New()

	// 1. If body is empty, pressing esc should go directly to stateMain
	m.composeBody.SetValue("")
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	updated, ok := m2.(mainModel)
	if !ok {
		t.Fatalf("expected mainModel")
	}
	if updated.state != stateMain {
		t.Errorf("expected state to be stateMain when body is empty, got %v", updated.state)
	}

	// 2. If body is filled, pressing esc should go to stateComposeCancelConfirm
	m.composeBody.SetValue("   some draft content   ")
	m3, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	updated2, ok := m3.(mainModel)
	if !ok {
		t.Fatalf("expected mainModel")
	}
	if updated2.state != stateComposeCancelConfirm {
		t.Errorf("expected state to be stateComposeCancelConfirm when body is filled, got %v", updated2.state)
	}

	// 3. From stateComposeCancelConfirm, pressing 'n' should go back to stateCompose
	updated2.state = stateComposeCancelConfirm
	m4, _ := updated2.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	updated3, ok := m4.(mainModel)
	if !ok {
		t.Fatalf("expected mainModel")
	}
	if updated3.state != stateCompose {
		t.Errorf("expected state to go back to stateCompose when pressing 'n', got %v", updated3.state)
	}

	// 4. From stateComposeCancelConfirm, pressing 'esc' should go back to stateCompose
	updated2.state = stateComposeCancelConfirm
	m5, _ := updated2.Update(tea.KeyMsg{Type: tea.KeyEsc})
	updated4, ok := m5.(mainModel)
	if !ok {
		t.Fatalf("expected mainModel")
	}
	if updated4.state != stateCompose {
		t.Errorf("expected state to go back to stateCompose when pressing 'esc', got %v", updated4.state)
	}

	// 5. From stateComposeCancelConfirm, pressing 'y' should go to stateMain
	updated2.state = stateComposeCancelConfirm
	m6, _ := updated2.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	updated5, ok := m6.(mainModel)
	if !ok {
		t.Fatalf("expected mainModel")
	}
	if updated5.state != stateMain {
		t.Errorf("expected state to go to stateMain when pressing 'y', got %v", updated5.state)
	}
}

func TestAttachmentSavedState(t *testing.T) {
	m := mainModel{
		state: stateAttachments,
	}

	updatedModelInterface, _ := m.Update(attachmentSavedMsg("/path/to/attachment"))
	updated := updatedModelInterface.(mainModel)

	if updated.state != stateAttachments {
		t.Errorf("expected state to remain stateAttachments, got %v", updated.state)
	}
}






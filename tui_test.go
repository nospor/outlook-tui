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
			expected: "\x1b[38;2;137;180;250;4mGitHub website\x1b[24;39m ",
		},
		{
			input:    "<a href=\"https://github.com\">https://github.com</a>",
			expected: "\x1b[38;2;137;180;250;4mhttps://github.com\x1b[24;39m",
		},
		{
			input:    "<a href=\"mailto:test@example.com\">Email Us</a>",
			expected: "\x1b[38;2;137;180;250;4mEmail Us\x1b[24;39m ",
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
		{
			input:    "<ul><li>Item 1</li><li>Item 2</li></ul>",
			expected: "• Item 1\n• Item 2",
		},
		{
			input:    "<h1>My Header</h1>Some content",
			expected: "My Header\n\nSome content",
		},
		{
			input:    "<table><tr><td>A</td><td>B</td></tr><tr><td>C</td><td>D</td></tr></table>",
			expected: " A B\n C D",
		},
		{
			input:    `<a href="https://adwantedintl.sharepoint.com/:p:/s/HR/IQAbvgiWAYTRTaDwie8L0ze9AfbCDwDoidEwS4Na7NrzWRg?e=YoFq52"><img src="cid:4c7fa2e7-e1e0-4830-a608-ec1a3f7d455e">Employment Rights Act 2025 - Performance Management - End to End - Managers Copy.pptx</a>`,
			expected: "\x1b[38;2;137;180;250;4m\x1b[1;38;2;203;166;247m[image: 4c7fa2e7-e1e0-4830-a608-ec1a3f7d455e]\x1b[0m\x1b[38;2;137;180;250;4mEmployment Rights Act 2025 - Performance Management - End to End - Managers Copy.pptx\x1b[24;39m ",
		},
		{
			input:    "<code>fmt.Println(\"Hello\")</code>",
			expected: "\x1b[38;2;249;226;175mfmt.Println(\"Hello\")\x1b[39m",
		},
		{
			input:    "<pre>line 1\nline 2</pre>",
			expected: "\x1b[38;2;203;166;247mline 1\x1b[39m\n\x1b[38;2;203;166;247mline 2\x1b[39m",
		},
		{
			input:    "+ added line",
			expected: "\x1b[38;2;166;227;161m+ added line\x1b[39m",
		},
		{
			input:    "- deleted line",
			expected: "\x1b[38;2;243;139;168m- deleted line\x1b[39m",
		},
		{
			input:    "@@ -1,5 +1,6 @@",
			expected: "\x1b[38;2;137;180;250m@@ -1,5 +1,6 @@\x1b[39m",
		},
		{
			input:    "102 <pre>code line\n</pre>",
			expected: "\x1b[38;2;203;166;247m102 code line\x1b[39m",
		},
		{
			input:    `<span>In Test</span> <span style="margin-left:5px">→ </span><span style="background-color:#ffffc4; text-decoration:none!important; font-family:sans-serif; font-size:13px">Awaiting LIVE deploy</span>`,
			expected: "In Test → \x1b[38;2;255;255;196mAwaiting LIVE deploy\x1b[39m",
		},
		{
			input:    `<font color="red">Attention</font>`,
			expected: "\x1b[38;2;255;0;0mAttention\x1b[39m",
		},
		{
			input:    `<td bgcolor="yellow">Alert</td>`,
			expected: "\x1b[38;2;255;255;0m Alert\x1b[39m",
		},
		{
			input:    `Before <div style="display:none">Hidden text</div> After`,
			expected: "Before  After",
		},
		{
			input:    `<div style="display:none; font-size:0px">Outer <span>nested <b>bold</b></span> text</div>Visible`,
			expected: "Visible",
		},
		{
			input:    `Line 1<br><span style="max-height:0px; overflow:hidden">Hidden Line<br></span>Line 2`,
			expected: "Line 1\nLine 2",
		},
		{
			input:    `<span style="font-size:10px">Not hidden</span>`,
			expected: "Not hidden",
		},
		{
			input:    `Hello <span style="mso-hide: all">hidden</span> world`,
			expected: "Hello  world",
		},
	}

	for _, tt := range tests {
		actual := formatBodyContent(tt.input)
		if actual != tt.expected {
			t.Errorf("formatBodyContent(%q) = %q; expected %q", tt.input, actual, tt.expected)
		}
	}
}

func TestGreetingPersonalization(t *testing.T) {
	// Test getRecipientFirstName
	recipients := []Recipient{
		{EmailAddress: EmailAddress{Name: "Robert Nodzewski", Address: "robert.nodzewski@uk.adwanted.com"}},
	}
	firstName := getRecipientFirstName(recipients, "robert.nodzewski@uk.adwanted.com")
	if firstName != "Robert" {
		t.Errorf("expected 'Robert', got %q", firstName)
	}

	// Test fallback to first recipient name when userEmail is empty
	firstNameEmptyEmail := getRecipientFirstName(recipients, "")
	if firstNameEmptyEmail != "Robert" {
		t.Errorf("expected 'Robert' with empty user email, got %q", firstNameEmptyEmail)
	}

	// Test insertRecipientGreeting
	tests := []struct {
		input     string
		firstName string
		expected  string
	}{
		{
			input:     "Hi,",
			firstName: "Robert",
			expected:  "Hi, Robert",
		},
		{
			input:     "Hi, \n\nSome body text",
			firstName: "Robert",
			expected:  "Hi, Robert\n\nSome body text",
		},
		{
			input:     "Hello\n",
			firstName: "Robert",
			expected:  "Hello, Robert\n",
		},
		{
			input:     "Dear,",
			firstName: "Robert",
			expected:  "Dear, Robert",
		},
		{
			input:     "Hi, how are you?",
			firstName: "Robert",
			expected:  "Hi, how are you?",
		},
	}

	for _, tt := range tests {
		actual := insertRecipientGreeting(tt.input, tt.firstName)
		if actual != tt.expected {
			t.Errorf("insertRecipientGreeting(%q, %q) = %q; expected %q", tt.input, tt.firstName, actual, tt.expected)
		}
	}

	// Test integration with formatBodyContent
	htmlInput := "Hi, "
	formatted := formatBodyContent(htmlInput, "Robert")
	expectedFormatted := "Hi, Robert"
	if formatted != expectedFormatted {
		t.Errorf("formatBodyContent(%q, 'Robert') = %q; expected %q", htmlInput, formatted, expectedFormatted)
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

func TestComposeContactSuggestionsScrolling(t *testing.T) {
	m := mainModel{
		state:       stateCompose,
		composeStep: 0,
		contacts: []Contact{
			{Name: "C1", Address: "c1@ex.com"},
			{Name: "C2", Address: "c2@ex.com"},
			{Name: "C3", Address: "c3@ex.com"},
			{Name: "C4", Address: "c4@ex.com"},
			{Name: "C5", Address: "c5@ex.com"},
			{Name: "C6", Address: "c6@ex.com"},
			{Name: "C7", Address: "c7@ex.com"},
			{Name: "C8", Address: "c8@ex.com"},
		},
		config: Config{UseSQLite: 1},
	}
	m.composeTo = textinput.New()

	// Simulate typing "c" (matches all 8)
	m.composeTo.SetValue("c")
	m.updateFilteredContacts()

	if len(m.filteredContacts) != 8 {
		t.Fatalf("expected 8 filtered contacts, got %d", len(m.filteredContacts))
	}

	// Initially start index should be 0
	if m.contactsStartIdx != 0 {
		t.Errorf("expected contactsStartIdx to be 0 initially, got %d", m.contactsStartIdx)
	}
	popupInit := m.renderContactsPopup()
	if !strings.Contains(popupInit, "C1") || !strings.Contains(popupInit, "C5") {
		t.Errorf("expected initially to contain C1 and C5, got: %q", popupInit)
	}
	if strings.Contains(popupInit, "C6") {
		t.Errorf("expected initially NOT to contain C6, got: %q", popupInit)
	}
	if !strings.Contains(popupInit, "and 3 more") {
		t.Errorf("expected initially to show 'and 3 more', got: %q", popupInit)
	}

	// Move down 4 times (contactsSelected = 4)
	curr := m
	for i := 0; i < 4; i++ {
		updatedInterface, _ := curr.Update(tea.KeyMsg{Type: tea.KeyDown})
		curr = updatedInterface.(mainModel)
	}
	if curr.contactsSelected != 4 {
		t.Errorf("expected contactsSelected to be 4, got %d", curr.contactsSelected)
	}
	if curr.contactsStartIdx != 0 {
		t.Errorf("expected contactsStartIdx to remain 0, got %d", curr.contactsStartIdx)
	}

	// Move down 1 more time (contactsSelected = 5) -> should scroll startIdx to 1
	updatedInterface, _ := curr.Update(tea.KeyMsg{Type: tea.KeyDown})
	curr = updatedInterface.(mainModel)
	if curr.contactsSelected != 5 {
		t.Errorf("expected contactsSelected to be 5, got %d", curr.contactsSelected)
	}
	if curr.contactsStartIdx != 1 {
		t.Errorf("expected contactsStartIdx to scroll to 1, got %d", curr.contactsStartIdx)
	}
	popupScroll := curr.renderContactsPopup()
	if strings.Contains(popupScroll, "C1") {
		t.Errorf("expected C1 to be scrolled out, got: %q", popupScroll)
	}
	if !strings.Contains(popupScroll, "C2") || !strings.Contains(popupScroll, "C6") {
		t.Errorf("expected to contain C2 through C6, got: %q", popupScroll)
	}
	if strings.Contains(popupScroll, "C7") {
		t.Errorf("expected NOT to contain C7 yet, got: %q", popupScroll)
	}
	if !strings.Contains(popupScroll, "and 1 more") {
		t.Errorf("expected top indicator 'and 1 more', got: %q", popupScroll)
	}
	if !strings.Contains(popupScroll, "and 2 more") {
		t.Errorf("expected bottom indicator 'and 2 more', got: %q", popupScroll)
	}

	// Move down 2 more times (contactsSelected = 7) -> should scroll startIdx to 3
	for i := 0; i < 2; i++ {
		updatedInterface, _ = curr.Update(tea.KeyMsg{Type: tea.KeyDown})
		curr = updatedInterface.(mainModel)
	}
	if curr.contactsSelected != 7 {
		t.Errorf("expected contactsSelected to be 7, got %d", curr.contactsSelected)
	}
	if curr.contactsStartIdx != 3 {
		t.Errorf("expected contactsStartIdx to scroll to 3, got %d", curr.contactsStartIdx)
	}
	popupEnd := curr.renderContactsPopup()
	if strings.Contains(popupEnd, "C3") {
		t.Errorf("expected C3 to be scrolled out, got: %q", popupEnd)
	}
	if !strings.Contains(popupEnd, "C4") || !strings.Contains(popupEnd, "C8") {
		t.Errorf("expected to contain C4 through C8, got: %q", popupEnd)
	}
	if !strings.Contains(popupEnd, "and 3 more") {
		t.Errorf("expected top indicator 'and 3 more', got: %q", popupEnd)
	}

	// Move down once more -> wrap around to 0, startIdx resets to 0
	updatedInterface, _ = curr.Update(tea.KeyMsg{Type: tea.KeyDown})
	curr = updatedInterface.(mainModel)
	if curr.contactsSelected != 0 {
		t.Errorf("expected contactsSelected to wrap to 0, got %d", curr.contactsSelected)
	}
	if curr.contactsStartIdx != 0 {
		t.Errorf("expected contactsStartIdx to reset to 0, got %d", curr.contactsStartIdx)
	}

	// Move UP -> wrap to 7, startIdx should scroll to 3 (8 - 5 = 3)
	updatedInterface, _ = curr.Update(tea.KeyMsg{Type: tea.KeyUp})
	curr = updatedInterface.(mainModel)
	if curr.contactsSelected != 7 {
		t.Errorf("expected contactsSelected to wrap to 7, got %d", curr.contactsSelected)
	}
	if curr.contactsStartIdx != 3 {
		t.Errorf("expected contactsStartIdx to scroll to 3, got %d", curr.contactsStartIdx)
	}

	// Move UP 5 times (contactsSelected = 2) -> should scroll startIdx to 2
	for i := 0; i < 5; i++ {
		updatedInterface, _ = curr.Update(tea.KeyMsg{Type: tea.KeyUp})
		curr = updatedInterface.(mainModel)
	}
	if curr.contactsSelected != 2 {
		t.Errorf("expected contactsSelected to be 2, got %d", curr.contactsSelected)
	}
	if curr.contactsStartIdx != 2 {
		t.Errorf("expected contactsStartIdx to scroll to 2, got %d", curr.contactsStartIdx)
	}
}

func TestExtractURLsFromMainMessage(t *testing.T) {
	tests := []struct {
		name     string
		subject  string
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
		{
			name:     "Forwarded message via Subject Fwd:",
			subject:  "Fwd: Check this out",
			input:    "Main message: https://new.com\n\n-----Original Message-----\nFrom: test@example.com\n\nQuoted: https://old.com",
			expected: []string{"https://new.com", "https://old.com"},
		},
		{
			name:     "Forwarded message via Subject Fw:",
			subject:  "  Fw: Hello",
			input:    "Main message: https://new.com\n\n> Quoted: https://old.com",
			expected: []string{"https://new.com", "https://old.com"},
		},
		{
			name:     "Forwarded message via Body Fallback",
			subject:  "No prefix subject",
			input:    "Please read this: https://new.com\n\n---------- Forwarded message ---------\nFrom: test@example.com\n\nQuoted: https://old.com",
			expected: []string{"https://new.com", "https://old.com"},
		},
		{
			name:     "Reply to forward should NOT extract from quoted blocks",
			subject:  "Re: Fwd: Check this out",
			input:    "Main message: https://new.com\n\n-----Original Message-----\nFrom: test@example.com\n\nQuoted: https://old.com",
			expected: []string{"https://new.com"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual := extractURLsFromMainMessage(tt.input, tt.subject)
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

func TestClassifyURL(t *testing.T) {
	tests := []struct {
		name         string
		urlStr       string
		expectedType string
		expectedNorm string
	}{
		{
			name:         "Normal URL",
			urlStr:       "https://google.com/search?q=test",
			expectedType: "normal",
			expectedNorm: "https://google.com/search?q=test",
		},
		{
			name:         "GitLab Merge Request URL",
			urlStr:       "https://gitlab.example.com/group/project/merge_requests/42",
			expectedType: "gitlab",
			expectedNorm: "https://gitlab.example.com/group/project/-/merge_requests/42",
		},
		{
			name:         "GitLab Merge Request URL with hyphen",
			urlStr:       "https://gitlab.example.com/group/project/-/merge_requests/42",
			expectedType: "gitlab",
			expectedNorm: "https://gitlab.example.com/group/project/-/merge_requests/42",
		},
		{
			name:         "YouTrack Issue URL",
			urlStr:       "https://youtrack.example.com/issue/AB-123",
			expectedType: "youtrack",
			expectedNorm: "https://youtrack.example.com/issue/AB-123",
		},
		{
			name:         "YouTrack Issue URL with Projects path",
			urlStr:       "https://youtrack.example.com/projects/PROJ/issues/PROJ-123",
			expectedType: "youtrack",
			expectedNorm: "https://youtrack.example.com/issue/PROJ-123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			urlType, norm := classifyURL(tt.urlStr)
			if urlType != tt.expectedType {
				t.Errorf("expected type %q, got %q", tt.expectedType, urlType)
			}
			if norm != tt.expectedNorm {
				t.Errorf("expected normalized %q, got %q", tt.expectedNorm, norm)
			}
		})
	}
}

func TestExtractAllURLsForOpen(t *testing.T) {
	input := `
		And some normal link: https://news.ycombinator.com
		Check this GitLab MR: https://gitlab.example.com/foo/bar/merge_requests/123
		And YouTrack: https://youtrack.adwanted.com/issue/MTEL-999
	`
	expected := []string{
		"https://gitlab.example.com/foo/bar/-/merge_requests/123",
		"https://youtrack.adwanted.com/issue/MTEL-999",
		"https://news.ycombinator.com",
	}

	actual := extractAllURLsForOpen(input, "")
	if len(actual) != len(expected) {
		t.Fatalf("expected %d URLs, got %d: %v", len(expected), len(actual), actual)
	}
	for i := range expected {
		if actual[i] != expected[i] {
			t.Errorf("at index %d: expected %q, got %q", i, expected[i], actual[i])
		}
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
			name:     "YouTrack URL with Projects format",
			input:    "Please check this issue: https://youtrack.example.com/projects/PROJ/issues/PROJ-123. Thanks!",
			expected: []string{"https://youtrack.example.com/issue/PROJ-123"},
		},
		{
			name: "Multiple YouTrack URLs with duplicates, params, and non-issues",
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
			actual := extractYouTrackURLs(tt.input, "")
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

func TestExtractGitLabURLs(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:     "No GitLab URLs",
			input:    "Hello world, check https://google.com and https://github.com",
			expected: nil,
		},
		{
			name:     "One GitLab MR URL",
			input:    "Please check this MR: https://gitlab.mediatel.co.uk/audio/audiotrack-admin-hub/-/merge_requests/25. Thanks!",
			expected: []string{"https://gitlab.mediatel.co.uk/audio/audiotrack-admin-hub/-/merge_requests/25"},
		},
		{
			name:     "One GitLab MR URL without hyphen",
			input:    "Please check this MR: https://gitlab.mediatel.co.uk/audio/audiotrack-admin-hub/merge_requests/25. Thanks!",
			expected: []string{"https://gitlab.mediatel.co.uk/audio/audiotrack-admin-hub/-/merge_requests/25"},
		},
		{
			name:     "One GitLab pipeline URL",
			input:    "Please check this pipeline: https://gitlab.mediatel.co.uk/adwanted/srds/-/pipelines/33780. Thanks!",
			expected: []string{"https://gitlab.mediatel.co.uk/adwanted/srds/-/pipelines/33780"},
		},
		{
			name:     "One GitLab pipeline URL without hyphen",
			input:    "Please check this pipeline: https://gitlab.mediatel.co.uk/adwanted/srds/pipelines/33780. Thanks!",
			expected: []string{"https://gitlab.mediatel.co.uk/adwanted/srds/-/pipelines/33780"},
		},
		{
			name:     "One GitLab job URL",
			input:    "Please check this job: https://gitlab.mediatel.co.uk/adwanted/srds/-/jobs/155933. Thanks!",
			expected: []string{"https://gitlab.mediatel.co.uk/adwanted/srds/-/jobs/155933"},
		},
		{
			name:     "One GitLab job URL without hyphen",
			input:    "Please check this job: https://gitlab.mediatel.co.uk/adwanted/srds/jobs/155933. Thanks!",
			expected: []string{"https://gitlab.mediatel.co.uk/adwanted/srds/-/jobs/155933"},
		},
		{
			name: "Multiple GitLab MR, pipeline, and job URLs with duplicates and query parameters",
			input: `
				Check:
				1. https://gitlab.com/group/subgroup/project/-/merge_requests/123
				2. https://gitlab.com/group/subgroup/project/-/merge_requests/123?diff=parallel
				3. https://gitlab.mediatel.co.uk/adwanted/srds/-/pipelines/33780
				4. https://gitlab.mediatel.co.uk/adwanted/srds/-/pipelines/33780?some_param=value
				5. https://gitlab.mediatel.co.uk/adwanted/srds/-/jobs/155933
				6. https://gitlab.mediatel.co.uk/adwanted/srds/jobs/155933?test=true
				7. https://gitlab.com/other-group/project/-/merge_requests/1
			`,
			expected: []string{
				"https://gitlab.com/group/subgroup/project/-/merge_requests/123",
				"https://gitlab.mediatel.co.uk/adwanted/srds/-/pipelines/33780",
				"https://gitlab.mediatel.co.uk/adwanted/srds/-/jobs/155933",
				"https://gitlab.com/other-group/project/-/merge_requests/1",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual := extractGitLabURLs(tt.input, "")
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
		state:           stateMain,
		userEmail:       "me@example.com",
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
		state:           stateMain,
		userEmail:       "me@example.com",
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
		width:       80,
		height:      24,
	}
	m.composeTo = textinput.New()
	m.composeCc = textinput.New()
	m.composeSubject = textinput.New()
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
	viewStr := updated2.View()
	if !strings.Contains(viewStr, "COMPOSE NEW EMAIL") {
		t.Errorf("expected view to contain compose layout, got:\n%s", viewStr)
	}
	if !strings.Contains(viewStr, "DISCARD EMAIL?") {
		t.Errorf("expected view to contain discard warning modal, got:\n%s", viewStr)
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

func TestExtractCleanText(t *testing.T) {
	htmlInput := `Hello standard user.
Please view <a href="https://example.com">this site</a>.
<img alt="smile" src="cid:123"/>
<br>
And here is more text.
-----Original Message-----
From: someone@example.com
Sent: yesterday
To: user
Quoted content: https://google.com
> inside a blockquote
`

	t.Run("without quoting", func(t *testing.T) {
		got := extractCleanText(htmlInput, true)
		expected := "Hello standard user.\nPlease view this site (https://example.com) .\n[image: smile]\n\nAnd here is more text."
		if got != expected {
			t.Errorf("expected:\n%q\ngot:\n%q", expected, got)
		}
	})

	t.Run("with quoting", func(t *testing.T) {
		got := extractCleanText(htmlInput, false)
		expected := "Hello standard user.\nPlease view this site (https://example.com) .\n[image: smile]\n\nAnd here is more text.\n-----Original Message-----\nFrom: someone@example.com\nSent: yesterday\nTo: user\nQuoted content: https://google.com\n> inside a blockquote"
		if got != expected {
			t.Errorf("expected:\n%q\ngot:\n%q", expected, got)
		}
	})
}

func TestYankMenuTransitions(t *testing.T) {
	// Initialize a mainModel with loaded detailMessage
	msg := &Message{
		ID:      "123",
		Subject: "Test Subject",
		Body: ItemBody{
			Content: "Hello world!",
		},
	}
	m := mainModel{
		state:         stateMain,
		detailMessage: msg,
		messages:      []Message{*msg},
		virtualList: []MessageListItem{
			{ThreadIdx: 0, MemberIdx: 0, IsHeader: false},
		},
		threadGroups: []ThreadGroup{
			{ConversationID: "abc", Members: []Message{*msg}},
		},
	}

	// 1. Pressing 'y' should transition to stateYankSelect
	m1Interface, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	m1 := m1Interface.(mainModel)
	if m1.state != stateYankSelect {
		t.Errorf("expected state to transition to stateYankSelect, got %v", m1.state)
	}

	// 2. Pressing 'esc' in stateYankSelect should go back to stateMain
	m2Interface, _ := m1.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m2 := m2Interface.(mainModel)
	if m2.state != stateMain {
		t.Errorf("expected state to return to stateMain, got %v", m2.state)
	}

	// 3. Pressing 'j' / 'down' should change selectedYankIdx
	m3Interface, _ := m1.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	m3 := m3Interface.(mainModel)
	if m3.selectedYankIdx != 1 {
		t.Errorf("expected selectedYankIdx to be 1, got %d", m3.selectedYankIdx)
	}

	// 4. Pressing 'k' / 'up' should wrap around to 3
	m4Interface, _ := m1.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("k")})
	m4 := m4Interface.(mainModel)
	if m4.selectedYankIdx != 3 {
		t.Errorf("expected selectedYankIdx to wrap to 3, got %d", m4.selectedYankIdx)
	}

	// 5. Pressing 's' in stateYankSelect should copy subject and return to stateMain
	m5Interface, _ := m1.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("s")})
	m5 := m5Interface.(mainModel)
	if m5.state != stateMain {
		t.Errorf("expected state to return to stateMain after yank, got %v", m5.state)
	}
}

func TestOverlayLines_StyleLeak(t *testing.T) {
	base := "hello world"
	overlay := "POP"

	result := overlayLines(base, overlay, 6, 0)
	expectedSuffix := "POP\x1b[0mld"
	if !strings.Contains(result, expectedSuffix) {
		t.Errorf("expected result to contain %q, but got %q", expectedSuffix, result)
	}
}

func TestFocusReportingAndReadMarking(t *testing.T) {
	m := initialModel()
	if !m.appFocused {
		t.Error("expected appFocused to be true by default")
	}

	// 1. Blur event
	updatedBlur, _ := m.Update(tea.BlurMsg{})
	mBlur := updatedBlur.(mainModel)
	if mBlur.appFocused {
		t.Error("expected appFocused to be false after BlurMsg")
	}

	// 2. Focus event
	updatedFocus, _ := mBlur.Update(tea.FocusMsg{})
	mFocus := updatedFocus.(mainModel)
	if !mFocus.appFocused {
		t.Error("expected appFocused to be true after FocusMsg")
	}

	// Test message read behavior based on focus
	msg := Message{
		ID:     "msg-123",
		IsRead: false,
		Body:   ItemBody{Content: "some body"},
	}

	// Case A: App is blurred
	mBlur.state = stateMain
	mBlur.virtualList = []MessageListItem{{ThreadIdx: 0, MemberIdx: -1, IsHeader: true}}
	mBlur.virtualSelected = 0
	mBlur.threadGroups = []ThreadGroup{
		{
			ConversationID: "conv-123",
			Members:        []Message{msg},
		},
	}
	_, cmdBlur := mBlur.loadMessageDetail(&msg)
	if cmdBlur != nil {
		t.Error("expected no fetch message detail command since body is cached and app is blurred")
	}

	// Case B: App is focused
	mFocus.state = stateMain
	mFocus.virtualList = []MessageListItem{{ThreadIdx: 0, MemberIdx: -1, IsHeader: true}}
	mFocus.virtualSelected = 0
	mFocus.threadGroups = []ThreadGroup{
		{
			ConversationID: "conv-123",
			Members:        []Message{msg},
		},
	}
	_, cmdFocus := mFocus.loadMessageDetail(&msg)
	if cmdFocus == nil {
		t.Error("expected fetch message detail command to mark read because app is focused")
	}
}

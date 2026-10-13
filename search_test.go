package main

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
)

func TestFilterMessagesByQuery(t *testing.T) {
	msgs := []Message{
		{
			ID:          "1",
			Subject:     "Q3 Invoice",
			BodyPreview: "Please pay by Friday",
			From:        Recipient{EmailAddress: EmailAddress{Name: "Alice", Address: "alice@contoso.com"}},
		},
		{
			ID:          "2",
			Subject:     "Lunch plans",
			BodyPreview: "Sushi?",
			From:        Recipient{EmailAddress: EmailAddress{Name: "Bob", Address: "bob@contoso.com"}},
			ToRecipients: []Recipient{
				{EmailAddress: EmailAddress{Name: "Dana", Address: "dana@contoso.com"}},
			},
		},
	}

	hits := filterMessagesByQuery(msgs, "invoice")
	if len(hits) != 1 || hits[0].ID != "1" {
		t.Fatalf("subject match: %+v", hits)
	}

	hits = filterMessagesByQuery(msgs, "BOB@CONTOSO.COM")
	if len(hits) != 1 || hits[0].ID != "2" {
		t.Fatalf("from match: %+v", hits)
	}

	hits = filterMessagesByQuery(msgs, "dana")
	if len(hits) != 1 || hits[0].ID != "2" {
		t.Fatalf("to match: %+v", hits)
	}

	hits = filterMessagesByQuery(msgs, "missing")
	if len(hits) != 0 {
		t.Fatalf("expected no hits, got %+v", hits)
	}

	hits = filterMessagesByQuery(msgs, "")
	if len(hits) != 2 {
		t.Fatalf("empty query should return all, got %d", len(hits))
	}
}

func TestOpenSearchPrompt(t *testing.T) {
	m := mainModel{
		state:          stateMain,
		searchInput:    newSearchInput(),
		folders:        []MailFolder{{ID: "inbox", DisplayName: "Inbox"}},
		detailViewport: viewport.New(40, 10),
	}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	got := updated.(mainModel)
	if got.state != stateSearch {
		t.Fatalf("state = %v, want stateSearch", got.state)
	}

	updated, _ = got.Update(tea.KeyMsg{Type: tea.KeyEsc})
	got = updated.(mainModel)
	if got.state != stateMain {
		t.Fatalf("esc state = %v, want stateMain", got.state)
	}
	if got.searchQuery != "" {
		t.Fatalf("cancelled search should not set searchQuery, got %q", got.searchQuery)
	}
}

func TestSubmitEmptySearchStaysInPrompt(t *testing.T) {
	m := mainModel{
		state:       stateSearch,
		searchInput: newSearchInput(),
		folders:     []MailFolder{{ID: "inbox", DisplayName: "Inbox"}},
	}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	got := updated.(mainModel)
	if got.state != stateSearch {
		t.Fatalf("state = %v, want stateSearch", got.state)
	}
	if !strings.Contains(got.statusMsg, "Enter a search phrase") {
		t.Fatalf("status = %q", got.statusMsg)
	}
}

func TestFavoriteSearchIsLocal(t *testing.T) {
	m := mainModel{
		state:       stateSearch,
		searchInput: newSearchInput(),
		folders: []MailFolder{
			{ID: "favorites", DisplayName: "Favorites"},
		},
		selectedFolder: 0,
		messages: []Message{
			{ID: "1", Subject: "Keep this invoice"},
			{ID: "2", Subject: "Unrelated lunch"},
		},
		detailViewport: viewport.New(40, 10),
	}
	m.searchInput.SetValue("invoice")
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	got := updated.(mainModel)
	if got.state != stateMain {
		t.Fatalf("state = %v, want stateMain", got.state)
	}
	if got.searchQuery != "invoice" {
		t.Fatalf("searchQuery = %q", got.searchQuery)
	}
	if len(got.messages) != 1 || got.messages[0].ID != "1" {
		t.Fatalf("messages = %+v", got.messages)
	}
}

func TestMessagesFetchedIgnoredDuringSearch(t *testing.T) {
	m := mainModel{
		state:          stateMain,
		searchQuery:    "invoice",
		folders:        []MailFolder{{ID: "inbox", DisplayName: "Inbox"}},
		selectedFolder: 0,
		messages:       []Message{{ID: "hit-1", Subject: "Invoice 1"}},
	}
	m.buildThreadGroups()

	updated, _ := m.Update(messagesFetchedMsg{
		FolderID: "inbox",
		Messages: []Message{{ID: "other", Subject: "Unrelated"}},
	})
	got := updated.(mainModel)
	if len(got.messages) != 1 || got.messages[0].ID != "hit-1" {
		t.Fatalf("search results were overwritten: %+v", got.messages)
	}
}

func TestExitSearchModeOnEsc(t *testing.T) {
	m := mainModel{
		state:          stateMain,
		searchQuery:    "invoice",
		searchSeq:      3,
		searchLoading:  true,
		folders:        []MailFolder{{ID: "inbox", DisplayName: "Inbox"}},
		selectedFolder: 0,
		messages:       []Message{{ID: "hit-1", Subject: "Invoice 1"}},
		detailViewport: viewport.New(40, 10),
	}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	got := updated.(mainModel)
	if got.searchQuery != "" {
		t.Fatalf("searchQuery still set: %q", got.searchQuery)
	}
	if got.searchLoading {
		t.Fatal("searchLoading should be false after exit")
	}
	if got.searchSeq == 3 {
		t.Fatal("searchSeq should increment so in-flight results are ignored")
	}
}

func TestStaleSearchResultsIgnored(t *testing.T) {
	m := mainModel{
		state:          stateMain,
		searchSeq:      2,
		searchQuery:    "",
		folders:        []MailFolder{{ID: "inbox", DisplayName: "Inbox"}},
		selectedFolder: 0,
		messages:       []Message{{ID: "orig", Subject: "Original"}},
	}
	updated, _ := m.Update(searchFetchedMsg{
		Seq:      1,
		Query:    "old",
		FolderID: "inbox",
		Messages: []Message{{ID: "stale", Subject: "Stale"}},
	})
	got := updated.(mainModel)
	if len(got.messages) != 1 || got.messages[0].ID != "orig" {
		t.Fatalf("stale search applied: %+v", got.messages)
	}
}

func TestApplySearchResultsDoesNotNeedCacheWrite(t *testing.T) {
	m := mainModel{
		state:          stateMain,
		searchSeq:      1,
		folders:        []MailFolder{{ID: "inbox", DisplayName: "Inbox"}},
		selectedFolder: 0,
		detailViewport: viewport.New(40, 10),
	}
	updated, _ := m.Update(searchFetchedMsg{
		Seq:      1,
		Query:    "invoice",
		FolderID: "inbox",
		Messages: []Message{{ID: "hit-1", Subject: "Invoice"}},
		NextLink: "https://graph.example/next",
	})
	got := updated.(mainModel)
	if !got.inSearchMode() {
		t.Fatal("expected search mode")
	}
	if got.searchNextLink != "https://graph.example/next" {
		t.Fatalf("nextLink = %q", got.searchNextLink)
	}
	if got.messagesPaneTitle() != "[2] SEARCH" {
		t.Fatalf("title = %q", got.messagesPaneTitle())
	}
}

func TestMainKeyHintsIncludeSearch(t *testing.T) {
	m := mainModel{activePane: paneMessages}
	if !hintsContain(m.mainKeyHints(), "[/] Search") {
		t.Fatalf("expected [/] Search in hints, got %v", m.mainKeyHints())
	}
	m.searchQuery = "invoice"
	if !hintsContain(m.mainKeyHints(), "[Esc] Exit search") {
		t.Fatalf("expected exit-search hint, got %v", m.mainKeyHints())
	}
}

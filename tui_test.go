package main

import (
	"testing"
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

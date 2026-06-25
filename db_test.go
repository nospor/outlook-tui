package main

import (
	"testing"
	"time"
)

func TestDBCache(t *testing.T) {
	// Set HOME to a temporary directory so db is created in a temp dir
	tempDir := t.TempDir()
	t.Setenv("HOME", tempDir)

	db, err := OpenDB()
	if err != nil {
		t.Fatalf("failed to open test database: %v", err)
	}
	defer db.Close()

	// 1. Create a dummy message
	msg1 := Message{
		ID:               "msg-1",
		ConversationID:   "conv-1",
		Subject:          "Test Subject",
		BodyPreview:      "This is a preview...",
		ReceivedDateTime: time.Now().Truncate(time.Second),
		IsRead:           false,
		HasAttachments:   true,
		From: Recipient{
			EmailAddress: EmailAddress{
				Name:    "Sender Name",
				Address: "sender@example.com",
			},
		},
		ToRecipients: []Recipient{
			{
				EmailAddress: EmailAddress{
					Name:    "Recipient Name",
					Address: "recipient@example.com",
				},
			},
		},
		CcRecipients: []Recipient{},
		Body: ItemBody{
			ContentType: "Text",
			Content:     "Full body content here.",
		},
	}

	// 2. Upsert single message
	err = db.UpsertMessage("inbox", msg1)
	if err != nil {
		t.Fatalf("failed to upsert message: %v", err)
	}

	// 3. Get messages for folder
	msgs, err := db.GetMessages("inbox")
	if err != nil {
		t.Fatalf("failed to get messages: %v", err)
	}

	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}

	retrieved := msgs[0]
	if retrieved.ID != msg1.ID {
		t.Errorf("expected ID %q, got %q", msg1.ID, retrieved.ID)
	}
	if retrieved.ConversationID != msg1.ConversationID {
		t.Errorf("expected ConversationID %q, got %q", msg1.ConversationID, retrieved.ConversationID)
	}
	if retrieved.Subject != msg1.Subject {
		t.Errorf("expected Subject %q, got %q", msg1.Subject, retrieved.Subject)
	}
	if retrieved.BodyPreview != msg1.BodyPreview {
		t.Errorf("expected BodyPreview %q, got %q", msg1.BodyPreview, retrieved.BodyPreview)
	}
	if !retrieved.ReceivedDateTime.Equal(msg1.ReceivedDateTime) {
		t.Errorf("expected ReceivedDateTime %v, got %v", msg1.ReceivedDateTime, retrieved.ReceivedDateTime)
	}
	if retrieved.IsRead != msg1.IsRead {
		t.Errorf("expected IsRead %v, got %v", msg1.IsRead, retrieved.IsRead)
	}
	if retrieved.HasAttachments != msg1.HasAttachments {
		t.Errorf("expected HasAttachments %v, got %v", msg1.HasAttachments, retrieved.HasAttachments)
	}
	if retrieved.From.EmailAddress.Name != msg1.From.EmailAddress.Name {
		t.Errorf("expected From.Name %q, got %q", msg1.From.EmailAddress.Name, retrieved.From.EmailAddress.Name)
	}
	if len(retrieved.ToRecipients) != 1 || retrieved.ToRecipients[0].EmailAddress.Address != "recipient@example.com" {
		t.Errorf("unexpected ToRecipients: %v", retrieved.ToRecipients)
	}

	// 4. Update read status
	err = db.UpdateReadStatus(msg1.ID, true)
	if err != nil {
		t.Fatalf("failed to update read status: %v", err)
	}

	msgs, err = db.GetMessages("inbox")
	if err != nil {
		t.Fatalf("failed to get messages: %v", err)
	}
	if !msgs[0].IsRead {
		t.Error("expected message to be marked read")
	}

	// 5. Delete message
	err = db.DeleteMessage(msg1.ID)
	if err != nil {
		t.Fatalf("failed to delete message: %v", err)
	}

	msgs, err = db.GetMessages("inbox")
	if err != nil {
		t.Fatalf("failed to get messages: %v", err)
	}
	if len(msgs) != 0 {
		t.Errorf("expected 0 messages, got %d", len(msgs))
	}
}

func TestDBUpsertMessagesTransaction(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("HOME", tempDir)

	db, err := OpenDB()
	if err != nil {
		t.Fatalf("failed to open test database: %v", err)
	}
	defer db.Close()

	msgs := []Message{
		{
			ID:               "msg-1",
			ReceivedDateTime: time.Now().Add(-10 * time.Minute),
			Subject:          "Msg 1",
		},
		{
			ID:               "msg-2",
			ReceivedDateTime: time.Now(),
			Subject:          "Msg 2",
		},
	}

	err = db.UpsertMessages("archive", msgs)
	if err != nil {
		t.Fatalf("failed transaction upsert: %v", err)
	}

	retrieved, err := db.GetMessages("archive")
	if err != nil {
		t.Fatalf("failed get: %v", err)
	}

	if len(retrieved) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(retrieved))
	}

	// Should be ordered by received datetime desc (newest first)
	if retrieved[0].ID != "msg-2" {
		t.Errorf("expected newest message first, got %s", retrieved[0].ID)
	}
}

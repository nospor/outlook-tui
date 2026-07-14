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
		Attachments: []Attachment{
			{
				ID:           "att-1",
				Name:         "file.txt",
				ContentType:  "text/plain",
				Size:         123,
				IsInline:     false,
				ContentBytes: "SGVsbG8gV29ybGQ=", // Base64 for Hello World
			},
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

	// Retrieve full message to verify attachments are fetched
	fullMsg, err := db.GetMessage(msg1.ID)
	if err != nil {
		t.Fatalf("failed to get full message: %v", err)
	}
	if len(fullMsg.Attachments) != 1 || fullMsg.Attachments[0].Name != "file.txt" {
		t.Errorf("expected 1 attachment named file.txt, got: %v", fullMsg.Attachments)
	}

	// Test GetMessageFolderID
	folderID, err := db.GetMessageFolderID(msg1.ID)
	if err != nil {
		t.Fatalf("failed to get message folder ID: %v", err)
	}
	if folderID != "inbox" {
		t.Errorf("expected folder ID 'inbox', got %q", folderID)
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

	// Test pruning: upserting only msg-2 should delete msg-1
	err = db.UpsertMessages("archive", []Message{msgs[1]})
	if err != nil {
		t.Fatalf("failed transaction upsert with prune: %v", err)
	}

	retrieved, err = db.GetMessages("archive")
	if err != nil {
		t.Fatalf("failed get: %v", err)
	}

	if len(retrieved) != 1 {
		t.Fatalf("expected 1 message after pruning, got %d", len(retrieved))
	}
	if retrieved[0].ID != "msg-2" {
		t.Errorf("expected msg-2 to remain, got %s", retrieved[0].ID)
	}
}

func TestDBGetContacts(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("HOME", tempDir)

	db, err := OpenDB()
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer db.Close()

	msg := Message{
		ID:               "msg-1",
		ReceivedDateTime: time.Now(),
		From: Recipient{
			EmailAddress: EmailAddress{
				Name:    "Alice Smith",
				Address: "alice@example.com",
			},
		},
		ToRecipients: []Recipient{
			{
				EmailAddress: EmailAddress{
					Name:    "Bob Jones",
					Address: "bob@example.com",
				},
			},
		},
		CcRecipients: []Recipient{
			{
				EmailAddress: EmailAddress{
					Name:    "Charlie Brown",
					Address: "charlie@example.com",
				},
			},
		},
	}

	err = db.UpsertMessage("inbox", msg)
	if err != nil {
		t.Fatalf("failed to upsert message: %v", err)
	}

	contacts, err := db.GetContacts()
	if err != nil {
		t.Fatalf("failed to get contacts: %v", err)
	}

	// We expect 3 contacts: Alice, Bob, Charlie
	if len(contacts) != 3 {
		t.Fatalf("expected 3 contacts, got %d", len(contacts))
	}

	// Order by name ASC: Alice Smith, Bob Jones, Charlie Brown
	expected := []struct {
		Name string
		Addr string
	}{
		{"Alice Smith", "alice@example.com"},
		{"Bob Jones", "bob@example.com"},
		{"Charlie Brown", "charlie@example.com"},
	}

	for i, exp := range expected {
		if contacts[i].Name != exp.Name || contacts[i].Address != exp.Addr {
			t.Errorf("expected contacts[%d] to be %s <%s>, got %s <%s>", i, exp.Name, exp.Addr, contacts[i].Name, contacts[i].Address)
		}
	}
}

func TestDBFavorites(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("HOME", tempDir)

	db, err := OpenDB()
	if err != nil {
		t.Fatalf("failed to open test database: %v", err)
	}
	defer db.Close()

	msg := Message{
		ID:               "msg-fav-1",
		ConversationID:   "conv-1",
		Subject:          "Favorite Subject",
		BodyPreview:      "This is a favorite preview...",
		ReceivedDateTime: time.Now().Truncate(time.Second),
		IsRead:           false,
		HasAttachments:   false,
		From: Recipient{
			EmailAddress: EmailAddress{
				Name:    "Sender Name",
				Address: "sender@example.com",
			},
		},
	}

	// Test IsFavorite (should be false initially)
	isFav, err := db.IsFavorite(msg.ID)
	if err != nil {
		t.Fatalf("IsFavorite failed: %v", err)
	}
	if isFav {
		t.Errorf("expected isFav to be false initially")
	}

	// Test UpsertFavoriteMessage
	err = db.UpsertFavoriteMessage(msg)
	if err != nil {
		t.Fatalf("UpsertFavoriteMessage failed: %v", err)
	}

	// Test IsFavorite (should be true now)
	isFav, err = db.IsFavorite(msg.ID)
	if err != nil {
		t.Fatalf("IsFavorite failed: %v", err)
	}
	if !isFav {
		t.Errorf("expected isFav to be true")
	}

	// Test GetFavoritesCounts (unread=1, total=1)
	unread, total, err := db.GetFavoritesCounts()
	if err != nil {
		t.Fatalf("GetFavoritesCounts failed: %v", err)
	}
	if unread != 1 || total != 1 {
		t.Errorf("expected unread=1 total=1, got unread=%d total=%d", unread, total)
	}

	// Test GetFavoriteMessages
	favs, err := db.GetFavoriteMessages()
	if err != nil {
		t.Fatalf("GetFavoriteMessages failed: %v", err)
	}
	if len(favs) != 1 || favs[0].ID != msg.ID {
		t.Errorf("expected 1 favorite with ID %q, got %d items", msg.ID, len(favs))
	}

	// Test GetFavoriteMessage
	retrieved, err := db.GetFavoriteMessage(msg.ID)
	if err != nil {
		t.Fatalf("GetFavoriteMessage failed: %v", err)
	}
	if retrieved.Subject != msg.Subject {
		t.Errorf("expected Subject %q, got %q", msg.Subject, retrieved.Subject)
	}

	// Test UpdateReadStatus
	err = db.UpdateReadStatus(msg.ID, true)
	if err != nil {
		t.Fatalf("UpdateReadStatus failed: %v", err)
	}
	unread, total, err = db.GetFavoritesCounts()
	if err != nil {
		t.Fatalf("GetFavoritesCounts failed: %v", err)
	}
	if unread != 0 || total != 1 {
		t.Errorf("expected unread=0 total=1 after read, got unread=%d total=%d", unread, total)
	}

	// Test RemoveFromFavorites
	err = db.RemoveFromFavorites(msg.ID)
	if err != nil {
		t.Fatalf("RemoveFromFavorites failed: %v", err)
	}
	isFav, err = db.IsFavorite(msg.ID)
	if err != nil {
		t.Fatalf("IsFavorite failed: %v", err)
	}
	if isFav {
		t.Errorf("expected isFav to be false after removal")
	}
}

func TestDBCalendar(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("HOME", tempDir)

	db, err := OpenDB()
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer db.Close()

	now := time.Now().Truncate(time.Second)

	// 1. Create mock events
	ev1 := CalendarEvent{
		ID:      "ev-1",
		Subject: "Meeting 1",
		Start: CalendarDateTime{
			DateTime: now.Format("2006-01-02T15:04:05"),
			TimeZone: "UTC",
		},
		End: CalendarDateTime{
			DateTime: now.Add(1 * time.Hour).Format("2006-01-02T15:04:05"),
			TimeZone: "UTC",
		},
		Location: struct{ DisplayName string }{DisplayName: "Conference Room A"},
		Organizer: Recipient{
			EmailAddress: EmailAddress{Name: "Organizer", Address: "organizer@example.com"},
		},
		Attendees: []CalendarEventAttendee{
			{
				EmailAddress: EmailAddress{Name: "Attendee 1", Address: "att1@example.com"},
				Type:         "required",
			},
		},
		IsAllDay:        false,
		IsCancelled:     false,
		IsOnlineMeeting: true,
		OnlineMeeting: &struct {
			JoinURL string `json:"joinUrl"`
		}{JoinURL: "https://teams.microsoft.com/join/1"},
		ShowAs:            "busy",
		ResponseRequested: true,
	}
	ev1.ResponseStatus.Response = "none"

	ev2 := CalendarEvent{
		ID:      "ev-2",
		Subject: "Meeting 2",
		Start: CalendarDateTime{
			DateTime: now.Add(2 * time.Hour).Format("2006-01-02T15:04:05"),
			TimeZone: "UTC",
		},
		End: CalendarDateTime{
			DateTime: now.Add(3 * time.Hour).Format("2006-01-02T15:04:05"),
			TimeZone: "UTC",
		},
		Location: struct{ DisplayName string }{DisplayName: "Online"},
		Organizer: Recipient{
			EmailAddress: EmailAddress{Name: "Organizer", Address: "organizer@example.com"},
		},
		Attendees: []CalendarEventAttendee{},
	}
	ev2.ResponseStatus.Response = "accepted"

	startRange := now.Add(-1 * time.Hour)
	endRange := now.Add(5 * time.Hour)

	// Test UpsertCalendarEvents
	err = db.UpsertCalendarEvents([]CalendarEvent{ev1, ev2}, startRange, endRange)
	if err != nil {
		t.Fatalf("failed to upsert calendar events: %v", err)
	}

	// Test GetCalendarEvents
	events, err := db.GetCalendarEvents(startRange, endRange)
	if err != nil {
		t.Fatalf("failed to get calendar events: %v", err)
	}

	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}

	retrievedEv1 := events[0]
	if retrievedEv1.ID != ev1.ID || retrievedEv1.Subject != ev1.Subject {
		t.Errorf("expected event 1, got %+v", retrievedEv1)
	}
	if !retrievedEv1.IsOnlineMeeting || retrievedEv1.OnlineMeeting == nil || retrievedEv1.OnlineMeeting.JoinURL != ev1.OnlineMeeting.JoinURL {
		t.Errorf("expected online meeting join url %q, got %+v", ev1.OnlineMeeting.JoinURL, retrievedEv1.OnlineMeeting)
	}
	if len(retrievedEv1.Attendees) != 1 || retrievedEv1.Attendees[0].EmailAddress.Address != "att1@example.com" {
		t.Errorf("expected attendee address att1@example.com, got %+v", retrievedEv1.Attendees)
	}

	// Test UpdateCalendarResponseStatus
	err = db.UpdateCalendarResponseStatus(ev1.ID, "accepted")
	if err != nil {
		t.Fatalf("failed to update response status: %v", err)
	}

	events2, err := db.GetCalendarEvents(startRange, endRange)
	if err != nil {
		t.Fatalf("failed to get calendar events: %v", err)
	}
	if events2[0].ResponseStatus.Response != "accepted" {
		t.Errorf("expected response status 'accepted', got %q", events2[0].ResponseStatus.Response)
	}

	// Test pruning: upsert only ev2 in range, ev1 should be deleted from range
	err = db.UpsertCalendarEvents([]CalendarEvent{ev2}, startRange, endRange)
	if err != nil {
		t.Fatalf("failed to upsert and prune: %v", err)
	}

	events3, err := db.GetCalendarEvents(startRange, endRange)
	if err != nil {
		t.Fatalf("failed to get calendar events: %v", err)
	}
	if len(events3) != 1 || events3[0].ID != ev2.ID {
		t.Errorf("expected only ev2 to remain, got %d items: %+v", len(events3), events3)
	}
}

func TestDBReminders(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("HOME", tempDir)

	db, err := OpenDB()
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer db.Close()

	eventID := "test-event-1"
	reminderMin := 30

	// 1. Initially false
	sent, err := db.HasReminderBeenSent(eventID, reminderMin)
	if err != nil {
		t.Fatalf("HasReminderBeenSent failed: %v", err)
	}
	if sent {
		t.Errorf("expected reminder sent to be false")
	}

	// 2. Mark as sent
	err = db.MarkReminderAsSent(eventID, reminderMin)
	if err != nil {
		t.Fatalf("MarkReminderAsSent failed: %v", err)
	}

	// 3. Now should be true
	sent, err = db.HasReminderBeenSent(eventID, reminderMin)
	if err != nil {
		t.Fatalf("HasReminderBeenSent failed: %v", err)
	}
	if !sent {
		t.Errorf("expected reminder sent to be true")
	}

	// 4. Test pruning. Let's insert a reminder that is old (sent_at = 25 hours ago)
	oldEventID := "old-event"
	oldReminderMin := 15
	cutoffTime := time.Now().Add(-25 * time.Hour).UTC().Format(time.RFC3339Nano)
	_, err = db.db.Exec(`INSERT INTO sent_reminders (event_id, reminder_min, sent_at) VALUES (?, ?, ?)`,
		oldEventID, oldReminderMin, cutoffTime)
	if err != nil {
		t.Fatalf("failed to insert old reminder: %v", err)
	}

	// Verify old reminder exists
	sent, err = db.HasReminderBeenSent(oldEventID, oldReminderMin)
	if err != nil || !sent {
		t.Fatalf("expected old reminder to exist before prune")
	}

	// Prune
	err = db.PruneSentReminders()
	if err != nil {
		t.Fatalf("PruneSentReminders failed: %v", err)
	}

	// Verify old reminder is gone
	sent, err = db.HasReminderBeenSent(oldEventID, oldReminderMin)
	if err != nil {
		t.Fatalf("HasReminderBeenSent failed after prune: %v", err)
	}
	if sent {
		t.Errorf("expected old reminder to be pruned")
	}

	// Verify new reminder is still there
	sent, err = db.HasReminderBeenSent(eventID, reminderMin)
	if err != nil {
		t.Fatalf("HasReminderBeenSent failed for new reminder: %v", err)
	}
	if !sent {
		t.Errorf("expected new reminder to remain after prune")
	}
}


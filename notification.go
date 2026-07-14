package main

import (
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// SendSystemNotification triggers a system desktop notification using notify-send.
// If playBell is true, it also outputs a terminal bell character (\a).
func SendSystemNotification(msg Message, playBell bool) {
	sender := msg.From.EmailAddress.Name
	if sender == "" {
		sender = msg.From.EmailAddress.Address
	}
	if sender == "" {
		sender = "Unknown Sender"
	}

	title := fmt.Sprintf("New Email from %s", sender)

	subject := msg.Subject
	if subject == "" {
		subject = "(No Subject)"
	}

	body := subject
	if msg.BodyPreview != "" {
		body = fmt.Sprintf("%s\n\n%s", subject, msg.BodyPreview)
	}

	// Trigger system notification using notify-send.
	// -a flag sets the application name to "Outlook TUI" so the system categorizes it correctly.
	cmd := exec.Command("notify-send", "-a", "Outlook TUI", title, body)
	_ = cmd.Run()

	if playBell {
		fmt.Print("\a")
	}
}

// SendCalendarEventReminder triggers a system desktop notification for an upcoming calendar event.
func SendCalendarEventReminder(eventSubject string, startOriginal string, minutesLeft int, playBell bool) {
	formattedTime := formatCalendarTime(startOriginal)
	var title, body string
	if minutesLeft == 0 {
		title = fmt.Sprintf("Event Starting Now: %s", eventSubject)
		body = fmt.Sprintf("Started at %s", formattedTime)
	} else {
		title = fmt.Sprintf("Event Reminder: %s", eventSubject)
		body = fmt.Sprintf("Starts in %d minute(s) (at %s)", minutesLeft, formattedTime)
	}

	// Trigger system notification using notify-send.
	cmd := exec.Command("notify-send", "-a", "Outlook TUI", title, body)
	_ = cmd.Run()

	if playBell {
		fmt.Print("\a")
	}
}

// formatCalendarTime parses raw date-time strings into a clean "YYYY-MM-DD HH:MM" format.
func formatCalendarTime(dateTimeStr string) string {
	formats := []string{
		"2006-01-02T15:04:05.9999999",
		"2006-01-02T15:04:05.999999",
		"2006-01-02T15:04:05.999",
		"2006-01-02T15:04:05",
		"2006-01-02T15:04:05Z07:00",
		"2006-01-02 15:04:05",
		"2006-01-02 15:04",
	}
	for _, f := range formats {
		if t, err := time.Parse(f, dateTimeStr); err == nil {
			return t.Format("2006-01-02 15:04")
		}
	}
	if len(dateTimeStr) >= 16 {
		return strings.Replace(dateTimeStr[:16], "T", " ", 1)
	}
	return dateTimeStr
}


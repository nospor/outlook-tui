package main

import (
	"fmt"
	"os/exec"
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

package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const graphBaseURL = "https://graph.microsoft.com/v1.0"

type MailFolder struct {
	ID               string `json:"id"`
	DisplayName      string `json:"displayName"`
	ParentFolderID   string `json:"parentFolderId"`
	ChildFolderCount int    `json:"childFolderCount"`
	UnreadItemCount  int    `json:"unreadItemCount"`
	TotalItemCount   int    `json:"totalItemCount"`
	WellKnownName    string `json:"wellKnownName"`
}

type Message struct {
	ID               string       `json:"id"`
	ConversationID   string       `json:"conversationId"`
	Subject          string       `json:"subject"`
	BodyPreview      string       `json:"bodyPreview"`
	ReceivedDateTime time.Time    `json:"receivedDateTime"`
	IsRead           bool         `json:"isRead"`
	HasAttachments   bool         `json:"hasAttachments"`
	From             Recipient    `json:"from"`
	ToRecipients     []Recipient  `json:"toRecipients"`
	CcRecipients     []Recipient  `json:"ccRecipients"`
	Body             ItemBody     `json:"body"`
	Attachments      []Attachment `json:"attachments,omitempty"`
}

type Recipient struct {
	EmailAddress EmailAddress `json:"emailAddress"`
}

type EmailAddress struct {
	Name    string `json:"name"`
	Address string `json:"address"`
}

type ItemBody struct {
	ContentType string `json:"contentType"`
	Content     string `json:"content"`
}

type Attachment struct {
	ID           string `json:"id,omitempty"`
	OdataType    string `json:"@odata.type,omitempty"`
	Name         string `json:"name"`
	ContentType  string `json:"contentType"`
	Size         int    `json:"size,omitempty"`
	IsInline     bool   `json:"isInline"`
	ContentId    string `json:"contentId,omitempty"`
	ContentBytes string `json:"contentBytes"` // Base64 encoded payload
}

type GraphClient struct {
	client *http.Client
}

func NewGraphClient(client *http.Client) *GraphClient {
	return &GraphClient{client: client}
}

func (gc *GraphClient) GetFolders() ([]MailFolder, error) {
	reqURL := fmt.Sprintf("%s/me/mailFolders?$top=100", graphBaseURL)
	resp, err := gc.client.Get(reqURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to get folders: status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var result struct {
		Value []MailFolder `json:"value"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return result.Value, nil
}

func (gc *GraphClient) GetMessagesPage(folderID string, skip int) ([]Message, error) {
	reqURL := fmt.Sprintf("%s/me/mailFolders/%s/messages?$select=id,conversationId,subject,bodyPreview,receivedDateTime,isRead,hasAttachments,from,toRecipients,ccRecipients&$top=50&$skip=%d&$orderby=receivedDateTime%%20desc", graphBaseURL, url.PathEscape(folderID), skip)
	resp, err := gc.client.Get(reqURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to get messages: status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var result struct {
		Value []Message `json:"value"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return result.Value, nil
}

func (gc *GraphClient) GetMessages(folderID string) ([]Message, error) {
	return gc.GetMessagesPage(folderID, 0)
}

func (gc *GraphClient) GetMessage(messageID string) (*Message, error) {
	reqURL := fmt.Sprintf("%s/me/messages/%s?$select=id,subject,body,bodyPreview,receivedDateTime,isRead,hasAttachments,from,toRecipients,ccRecipients", graphBaseURL, url.PathEscape(messageID))
	resp, err := gc.client.Get(reqURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to get message detail: status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var msg Message
	if err := json.NewDecoder(resp.Body).Decode(&msg); err != nil {
		return nil, err
	}

	return &msg, nil
}

func (gc *GraphClient) GetAttachments(messageID string) ([]Attachment, error) {
	reqURL := fmt.Sprintf("%s/me/messages/%s/attachments", graphBaseURL, url.PathEscape(messageID))
	resp, err := gc.client.Get(reqURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to get attachments: status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var result struct {
		Value []Attachment `json:"value"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return result.Value, nil
}

func parseAddressStringToRecipients(addressStr string) []Recipient {
	recipients := []Recipient{}
	emails := strings.Split(addressStr, ",")
	for _, email := range emails {
		email = strings.TrimSpace(email)
		if email == "" {
			continue
		}
		addr := email
		name := ""
		if strings.Contains(email, "<") && strings.Contains(email, ">") {
			start := strings.Index(email, "<")
			end := strings.Index(email, ">")
			if start < end {
				name = strings.TrimSpace(email[:start])
				addr = strings.TrimSpace(email[start+1 : end])
			}
		}
		recipients = append(recipients, Recipient{
			EmailAddress: EmailAddress{
				Name:    name,
				Address: addr,
			},
		})
	}
	return recipients
}

func makeImageAttachments(images []PastedImage) []Attachment {
	var atts []Attachment
	for i, img := range images {
		name := fmt.Sprintf("pasted-image-%d.png", i+1)
		if strings.Contains(img.ContentType, "jpeg") {
			name = fmt.Sprintf("pasted-image-%d.jpg", i+1)
		}
		atts = append(atts, Attachment{
			OdataType:    "#microsoft.graph.fileAttachment",
			Name:         name,
			ContentType:  img.ContentType,
			ContentBytes: base64.StdEncoding.EncodeToString(img.Bytes),
			ContentId:    fmt.Sprintf("image%d", i+1),
			IsInline:     true,
		})
	}
	return atts
}

// PendingFile represents a file to be attached to an email.
type PendingFile struct {
	Name        string
	ContentType string
	Data        []byte
}

func makeFileAttachments(files []PendingFile) []Attachment {
	var atts []Attachment
	for _, f := range files {
		atts = append(atts, Attachment{
			OdataType:    "#microsoft.graph.fileAttachment",
			Name:         f.Name,
			ContentType:  f.ContentType,
			ContentBytes: base64.StdEncoding.EncodeToString(f.Data),
			IsInline:     false,
		})
	}
	return atts
}

func (gc *GraphClient) SendMessage(subject, bodyText, recipientAddress, ccAddress string, images []PastedImage, files []PendingFile) error {
	reqURL := fmt.Sprintf("%s/me/sendMail", graphBaseURL)

	sendReq := struct {
		Message struct {
			Subject      string       `json:"subject"`
			Body         ItemBody     `json:"body"`
			ToRecipients []Recipient  `json:"toRecipients"`
			CcRecipients []Recipient  `json:"ccRecipients"`
			Attachments  []Attachment `json:"attachments,omitempty"`
		} `json:"message"`
		SaveToSentItems string `json:"saveToSentItems"`
	}{}

	sendReq.Message.Subject = subject

	contentType := "Text"
	bodyContent := bodyText
	if len(images) > 0 {
		contentType = "HTML"
		escaped := html.EscapeString(bodyText)
		htmlBody := strings.ReplaceAll(escaped, "\n", "<br />")

		reImg := regexp.MustCompile(`(?i)\[image\s+(\d+)\]`)
		htmlBody = reImg.ReplaceAllStringFunc(htmlBody, func(match string) string {
			sub := reImg.FindStringSubmatch(match)
			if len(sub) < 2 {
				return match
			}
			return fmt.Sprintf(`<img src="cid:image%s" />`, sub[1])
		})
		bodyContent = htmlBody
	}

	sendReq.Message.Body = ItemBody{
		ContentType: contentType,
		Content:     bodyContent,
	}

	sendReq.Message.ToRecipients = parseAddressStringToRecipients(recipientAddress)
	sendReq.Message.CcRecipients = parseAddressStringToRecipients(ccAddress)
	sendReq.SaveToSentItems = "true"

	if len(images) > 0 || len(files) > 0 {
		var attachments []Attachment
		if len(images) > 0 {
			attachments = append(attachments, makeImageAttachments(images)...)
		}
		if len(files) > 0 {
			attachments = append(attachments, makeFileAttachments(files)...)
		}
		sendReq.Message.Attachments = attachments
	}

	jsonBytes, err := json.Marshal(sendReq)
	if err != nil {
		return err
	}

	resp, err := gc.client.Post(reqURL, "application/json", bytes.NewBuffer(jsonBytes))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted && resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to send message: status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	return nil
}

// ReplyMessage sends a reply to a specific message, linking it to the original thread.
// It calls POST /me/messages/{id}/reply on the Graph API.
func (gc *GraphClient) ReplyMessage(messageID, bodyText, toAddress string, images []PastedImage, files []PendingFile) error {
	reqURL := fmt.Sprintf("%s/me/messages/%s/reply", graphBaseURL, url.PathEscape(messageID))

	type ReplyReq struct {
		Message struct {
			ToRecipients []Recipient  `json:"toRecipients,omitempty"`
			Body         *ItemBody    `json:"body,omitempty"`
			Attachments  []Attachment `json:"attachments,omitempty"`
		} `json:"message"`
	}
	var replyReq ReplyReq

	var attachments []Attachment
	if len(images) > 0 {
		attachments = append(attachments, makeImageAttachments(images)...)
	}
	if len(files) > 0 {
		attachments = append(attachments, makeFileAttachments(files)...)
	}
	if len(attachments) > 0 {
		replyReq.Message.Attachments = attachments
	}

	escaped := html.EscapeString(bodyText)
	htmlBody := strings.ReplaceAll(escaped, "\n", "<br />")

	if len(images) > 0 {
		reImg := regexp.MustCompile(`(?i)\[image\s+(\d+)\]`)
		htmlBody = reImg.ReplaceAllStringFunc(htmlBody, func(match string) string {
			sub := reImg.FindStringSubmatch(match)
			if len(sub) < 2 {
				return match
			}
			return fmt.Sprintf(`<img src="cid:image%s" />`, sub[1])
		})
	}

	replyReq.Message.Body = &ItemBody{
		ContentType: "HTML",
		Content:     htmlBody,
	}

	if toAddress != "" {
		replyReq.Message.ToRecipients = parseAddressStringToRecipients(toAddress)
	}

	jsonBytes, err := json.Marshal(replyReq)
	if err != nil {
		return err
	}

	resp, err := gc.client.Post(reqURL, "application/json", bytes.NewBuffer(jsonBytes))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted && resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to reply to message: status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	return nil
}

// ReplyAllMessage sends a reply-all to a specific message, linking it to the original thread.
// It calls POST /me/messages/{id}/replyAll on the Graph API.
func (gc *GraphClient) ReplyAllMessage(messageID, bodyText, toAddress, ccAddress string, images []PastedImage, files []PendingFile) error {
	reqURL := fmt.Sprintf("%s/me/messages/%s/replyAll", graphBaseURL, url.PathEscape(messageID))

	type ReplyReq struct {
		Message struct {
			ToRecipients []Recipient  `json:"toRecipients,omitempty"`
			CcRecipients []Recipient  `json:"ccRecipients,omitempty"`
			Body         *ItemBody    `json:"body,omitempty"`
			Attachments  []Attachment `json:"attachments,omitempty"`
		} `json:"message"`
	}
	var replyReq ReplyReq

	var attachments []Attachment
	if len(images) > 0 {
		attachments = append(attachments, makeImageAttachments(images)...)
	}
	if len(files) > 0 {
		attachments = append(attachments, makeFileAttachments(files)...)
	}
	if len(attachments) > 0 {
		replyReq.Message.Attachments = attachments
	}

	escaped := html.EscapeString(bodyText)
	htmlBody := strings.ReplaceAll(escaped, "\n", "<br />")

	if len(images) > 0 {
		reImg := regexp.MustCompile(`(?i)\[image\s+(\d+)\]`)
		htmlBody = reImg.ReplaceAllStringFunc(htmlBody, func(match string) string {
			sub := reImg.FindStringSubmatch(match)
			if len(sub) < 2 {
				return match
			}
			return fmt.Sprintf(`<img src="cid:image%s" />`, sub[1])
		})
	}

	replyReq.Message.Body = &ItemBody{
		ContentType: "HTML",
		Content:     htmlBody,
	}

	if toAddress != "" {
		replyReq.Message.ToRecipients = parseAddressStringToRecipients(toAddress)
	}
	if ccAddress != "" {
		replyReq.Message.CcRecipients = parseAddressStringToRecipients(ccAddress)
	}

	jsonBytes, err := json.Marshal(replyReq)
	if err != nil {
		return err
	}

	resp, err := gc.client.Post(reqURL, "application/json", bytes.NewBuffer(jsonBytes))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted && resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to reply-all to message: status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	return nil
}

func (gc *GraphClient) GetMe() (string, error) {
	reqURL := fmt.Sprintf("%s/me", graphBaseURL)
	resp, err := gc.client.Get(reqURL)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("failed to get user info: status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var result struct {
		Mail              string `json:"mail"`
		UserPrincipalName string `json:"userPrincipalName"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}

	email := result.Mail
	if email == "" {
		email = result.UserPrincipalName
	}
	return email, nil
}

func (gc *GraphClient) MoveMessage(messageID, destinationID string) error {
	reqURL := fmt.Sprintf("%s/me/messages/%s/move", graphBaseURL, url.PathEscape(messageID))

	moveReq := struct {
		DestinationID string `json:"destinationId"`
	}{
		DestinationID: destinationID,
	}

	jsonBytes, err := json.Marshal(moveReq)
	if err != nil {
		return err
	}

	resp, err := gc.client.Post(reqURL, "application/json", bytes.NewBuffer(jsonBytes))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to move message: status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	return nil
}

func (gc *GraphClient) DeleteMessage(messageID string) error {
	return gc.MoveMessage(messageID, "deleteditems")
}

func (gc *GraphClient) MarkAsRead(messageID string, isRead bool) error {
	reqURL := fmt.Sprintf("%s/me/messages/%s", graphBaseURL, url.PathEscape(messageID))

	patchReq := struct {
		IsRead bool `json:"isRead"`
	}{
		IsRead: isRead,
	}

	jsonBytes, err := json.Marshal(patchReq)
	if err != nil {
		return err
	}

	req, err := http.NewRequest("PATCH", reqURL, bytes.NewBuffer(jsonBytes))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := gc.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to mark message read status: status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	return nil
}

// ─── Calendar Types ───────────────────────────────────────────────────────────

// CalendarEventAttendee represents a meeting attendee.
type CalendarEventAttendee struct {
	EmailAddress EmailAddress `json:"emailAddress"`
	Type         string       `json:"type"`    // required, optional, resource
	Status       struct {
		Response string `json:"response"` // none, accepted, tentativelyAccepted, declined, notResponded
	} `json:"status"`
}

// CalendarEvent represents a single Outlook calendar event.
type CalendarEvent struct {
	ID               string                  `json:"id"`
	Subject          string                  `json:"subject"`
	Start            CalendarDateTime        `json:"start"`
	End              CalendarDateTime        `json:"end"`
	Location         struct{ DisplayName string } `json:"location"`
	Organizer        Recipient               `json:"organizer"`
	Attendees        []CalendarEventAttendee `json:"attendees"`
	IsAllDay         bool                    `json:"isAllDay"`
	IsCancelled      bool                    `json:"isCancelled"`
	IsOnlineMeeting  bool                    `json:"isOnlineMeeting"`
	OnlineMeeting    *struct {
		JoinURL string `json:"joinUrl"`
	} `json:"onlineMeeting"`
	ShowAs           string                  `json:"showAs"` // free, tentative, busy, oof, workingElsewhere, unknown
	ResponseRequested bool                   `json:"responseRequested"`
	ResponseStatus   struct {
		Response string `json:"response"` // none, accepted, tentativelyAccepted, declined, notResponded
	} `json:"responseStatus"`
	BodyPreview string `json:"bodyPreview"`
}

// CalendarDateTime holds an ISO-8601 datetime string and its timezone.
type CalendarDateTime struct {
	DateTime string `json:"dateTime"`
	TimeZone string `json:"timeZone"`
}

// Time returns the CalendarDateTime parsed as a time.Time in UTC.
func (cdt CalendarDateTime) Time() time.Time {
	loc := time.UTC
	if cdt.TimeZone != "" {
		if l, err := time.LoadLocation(cdt.TimeZone); err == nil {
			loc = l
		}
	}
	formats := []string{
		"2006-01-02T15:04:05.9999999",
		"2006-01-02T15:04:05",
		"2006-01-02T15:04:05Z07:00",
	}
	for _, f := range formats {
		if t, err := time.ParseInLocation(f, cdt.DateTime, loc); err == nil {
			return t.UTC()
		}
	}
	return time.Time{}
}

// ─── Calendar API Methods ─────────────────────────────────────────────────────

// GetCalendarEventsForRange fetches the user's calendar events within a specific start and end time.
func (gc *GraphClient) GetCalendarEventsForRange(start time.Time, end time.Time) ([]CalendarEvent, error) {
	startStr := start.Format("2006-01-02T15:04:05Z")
	endStr := end.Format("2006-01-02T15:04:05Z")

	reqURL := fmt.Sprintf(
		"%s/me/calendarView?startDateTime=%s&endDateTime=%s&$select=id,subject,start,end,location,organizer,attendees,isAllDay,isCancelled,isOnlineMeeting,onlineMeeting,showAs,responseRequested,responseStatus,bodyPreview&$orderby=start/dateTime&$top=100",
		graphBaseURL, url.QueryEscape(startStr), url.QueryEscape(endStr),
	)

	resp, err := gc.client.Get(reqURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to get calendar events: status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var result struct {
		Value []CalendarEvent `json:"value"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return result.Value, nil
}

// GetCalendarEvents fetches the user's calendar events from 7 days ago to N days in the future.
func (gc *GraphClient) GetCalendarEvents(days int) ([]CalendarEvent, error) {
	if days <= 0 {
		days = 30
	}
	start := time.Now().AddDate(0, 0, -7).UTC()
	end := time.Now().AddDate(0, 0, days).UTC()
	return gc.GetCalendarEventsForRange(start, end)
}


// EventResponse is one of the allowed response actions for a calendar event.
type EventResponse string

const (
	EventResponseAccept    EventResponse = "accept"
	EventResponseTentative EventResponse = "tentativelyAccept"
	EventResponseDecline   EventResponse = "decline"
)

// RespondToCalendarEvent sends an accept/tentativelyAccept/decline response to a
// calendar event. Set sendResponse=true to notify the organiser by email.
func (gc *GraphClient) RespondToCalendarEvent(eventID string, response EventResponse, comment string, sendResponse bool) error {
	reqURL := fmt.Sprintf("%s/me/events/%s/%s", graphBaseURL, url.PathEscape(eventID), string(response))

	body := struct {
		Comment      string `json:"comment,omitempty"`
		SendResponse bool   `json:"sendResponse"`
	}{
		Comment:      comment,
		SendResponse: sendResponse,
	}

	jsonBytes, err := json.Marshal(body)
	if err != nil {
		return err
	}

	resp, err := gc.client.Post(reqURL, "application/json", bytes.NewBuffer(jsonBytes))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted && resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to respond to event: status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	return nil
}


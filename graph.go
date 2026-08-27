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
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

var graphBaseURL = "https://graph.microsoft.com/v1.0"

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

const messagePageSize = 50
const deleteConcurrency = 10
const maxFolderScanPages = 200
const maxConversationDeleteWaves = 3

// GetMessageIDsPage fetches a page of message IDs from a folder (lightweight).
func (gc *GraphClient) GetMessageIDsPage(folderID string, skip int) ([]string, error) {
	reqURL := fmt.Sprintf("%s/me/mailFolders/%s/messages?$select=id&$top=%d&$skip=%d&$orderby=receivedDateTime%%20desc",
		graphBaseURL, url.PathEscape(folderID), messagePageSize, skip)
	resp, err := gc.client.Get(reqURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to get message IDs: status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var result struct {
		Value []struct {
			ID string `json:"id"`
		} `json:"value"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	ids := make([]string, len(result.Value))
	for i, v := range result.Value {
		ids[i] = v.ID
	}
	return ids, nil
}

func escapeODataString(s string) string {
	return strings.ReplaceAll(s, "'", "''")
}

type messageListPage struct {
	Messages []Message
	NextLink string
}

func (gc *GraphClient) getMessageListPage(reqURL string) (messageListPage, error) {
	resp, err := gc.client.Get(reqURL)
	if err != nil {
		return messageListPage{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return messageListPage{}, fmt.Errorf("failed to get messages: status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var result struct {
		Value    []Message `json:"value"`
		NextLink string    `json:"@odata.nextLink"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return messageListPage{}, err
	}
	return messageListPage{Messages: result.Value, NextLink: result.NextLink}, nil
}

func messageMatchesConversation(msg Message, conversationID string) bool {
	cid := msg.ConversationID
	if cid == "" {
		cid = msg.ID
	}
	return cid == conversationID
}

func (gc *GraphClient) paginateMessageIDList(initialURL string) ([]string, error) {
	idSet := make(map[string]bool)
	seenURLs := make(map[string]bool)
	reqURL := initialURL
	for page := 0; reqURL != "" && page < maxFolderScanPages; page++ {
		if seenURLs[reqURL] {
			break
		}
		seenURLs[reqURL] = true

		pageResult, err := gc.getMessageListPage(reqURL)
		if err != nil {
			return nil, err
		}
		for _, msg := range pageResult.Messages {
			if msg.ID != "" {
				idSet[msg.ID] = true
			}
		}
		reqURL = pageResult.NextLink
	}

	ids := make([]string, 0, len(idSet))
	for id := range idSet {
		ids = append(ids, id)
	}
	return ids, nil
}

func (gc *GraphClient) getConversationMessageIDsFiltered(folderID, conversationID string) ([]string, error) {
	filter := fmt.Sprintf("conversationId eq '%s'", escapeODataString(conversationID))
	params := url.Values{}
	params.Set("$filter", filter)
	params.Set("$select", "id")
	params.Set("$top", fmt.Sprintf("%d", messagePageSize))
	reqURL := fmt.Sprintf("%s/me/mailFolders/%s/messages?%s", graphBaseURL, url.PathEscape(folderID), params.Encode())
	return gc.paginateMessageIDList(reqURL)
}

func (gc *GraphClient) scanFolderForConversationIDs(folderID, conversationID string) ([]string, error) {
	reqURL := fmt.Sprintf("%s/me/mailFolders/%s/messages?$select=id,conversationId&$top=%d&$orderby=receivedDateTime%%20desc",
		graphBaseURL, url.PathEscape(folderID), messagePageSize)

	idSet := make(map[string]bool)
	seenURLs := make(map[string]bool)
	for page := 0; reqURL != "" && page < maxFolderScanPages; page++ {
		if seenURLs[reqURL] {
			break
		}
		seenURLs[reqURL] = true

		pageResult, err := gc.getMessageListPage(reqURL)
		if err != nil {
			return nil, err
		}
		for _, msg := range pageResult.Messages {
			if messageMatchesConversation(msg, conversationID) {
				idSet[msg.ID] = true
			}
		}
		reqURL = pageResult.NextLink
	}

	ids := make([]string, 0, len(idSet))
	for id := range idSet {
		ids = append(ids, id)
	}
	return ids, nil
}

// GetConversationMessageIDs returns all message IDs in a conversation within a folder.
// It tries a Graph $filter query first (fast), then falls back to a bounded folder scan.
func (gc *GraphClient) GetConversationMessageIDs(folderID, conversationID string) ([]string, error) {
	if ids, err := gc.getConversationMessageIDsFiltered(folderID, conversationID); err == nil && len(ids) > 0 {
		return ids, nil
	}
	return gc.scanFolderForConversationIDs(folderID, conversationID)
}

// DeleteConversationMessages deletes every message in a conversation within a folder.
// seedIDs are deleted immediately; up to maxConversationDeleteWaves verification scans
// catch any messages the filter or local list missed.
func (gc *GraphClient) DeleteConversationMessages(folderID, conversationID string, seedIDs []string, hardDelete bool) (succeeded, failed []string, errs []error) {
	succeededSet := make(map[string]bool)
	failedSet := make(map[string]bool)

	deleteBatch := func(ids []string) int {
		if len(ids) == 0 {
			return 0
		}
		perErrs := gc.deleteMessagesConcurrent(ids, hardDelete)
		successCount := 0
		for i, id := range ids {
			if perErrs[i] != nil {
				failedSet[id] = true
				errs = append(errs, perErrs[i])
			} else {
				succeededSet[id] = true
				delete(failedSet, id)
				successCount++
			}
		}
		return successCount
	}

	deleteBatch(seedIDs)

	for wave := 0; wave < maxConversationDeleteWaves; wave++ {
		ids, err := gc.GetConversationMessageIDs(folderID, conversationID)
		if err != nil {
			errs = append(errs, err)
			break
		}
		var remaining []string
		for _, id := range ids {
			if !succeededSet[id] {
				remaining = append(remaining, id)
			}
		}
		if len(remaining) == 0 {
			break
		}
		if deleteBatch(remaining) == 0 {
			break
		}
	}

	for id := range succeededSet {
		succeeded = append(succeeded, id)
	}
	for id := range failedSet {
		failed = append(failed, id)
	}
	return succeeded, failed, errs
}

// getAllMessageIDs paginates through all messages in a folder and returns their IDs.
func (gc *GraphClient) getAllMessageIDs(folderID string) ([]string, error) {
	var allIDs []string
	skip := 0
	for {
		page, err := gc.GetMessageIDsPage(folderID, skip)
		if err != nil {
			return nil, err
		}
		if len(page) == 0 {
			break
		}
		allIDs = append(allIDs, page...)
		skip += len(page)
		if len(page) < messagePageSize {
			break
		}
	}
	return allIDs, nil
}

// deleteMessagesConcurrent deletes messages with a bounded worker pool.
// The returned slice has one entry per input ID (nil means success).
func (gc *GraphClient) deleteMessagesConcurrent(ids []string, hardDelete bool) []error {
	if len(ids) == 0 {
		return nil
	}
	errs := make([]error, len(ids))
	sem := make(chan struct{}, deleteConcurrency)
	var wg sync.WaitGroup
	for i, id := range ids {
		wg.Add(1)
		go func(idx int, msgID string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			if hardDelete {
				errs[idx] = gc.HardDeleteMessage(msgID)
			} else {
				errs[idx] = gc.DeleteMessage(msgID)
			}
		}(i, id)
	}
	wg.Wait()
	return errs
}

// EmptyFolderMessages deletes all messages in a folder, re-fetching in waves until empty.
func (gc *GraphClient) EmptyFolderMessages(folderID string, hardDelete bool) (deletedCount int, errs []error, err error) {
	for {
		ids, fetchErr := gc.getAllMessageIDs(folderID)
		if fetchErr != nil {
			return deletedCount, errs, fetchErr
		}
		if len(ids) == 0 {
			break
		}
		waveErrs := gc.deleteMessagesConcurrent(ids, hardDelete)
		for _, err := range waveErrs {
			if err != nil {
				errs = append(errs, err)
			} else {
				deletedCount++
			}
		}
	}
	return deletedCount, errs, nil
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

// HardDeleteMessage permanently deletes a message via the Graph API
// (HTTP DELETE). This should be used when the message is already in the
// Deleted Items folder so that it is fully removed rather than moved again.
func (gc *GraphClient) HardDeleteMessage(messageID string) error {
	reqURL := fmt.Sprintf("%s/me/messages/%s", graphBaseURL, url.PathEscape(messageID))
	req, err := http.NewRequest("DELETE", reqURL, nil)
	if err != nil {
		return err
	}
	resp, err := gc.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to permanently delete message: status %d: %s", resp.StatusCode, string(bodyBytes))
	}
	return nil
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
	WebLink          string                  `json:"webLink"` // Outlook Web (OWA) deep link to the event; opens authenticated in browser
	ResponseStatus   struct {
		Response string `json:"response"` // none, accepted, tentativelyAccepted, declined, notResponded
	} `json:"responseStatus"`
	Body             ItemBody `json:"body"`
	BodyPreview      string   `json:"bodyPreview"`
	IsReminderOn     bool     `json:"isReminderOn"`
	ReminderMinutesBeforeStart int `json:"reminderMinutesBeforeStart"`
}

// CalendarDateTime holds an ISO-8601 datetime string and its timezone.
type CalendarDateTime struct {
	DateTime string `json:"dateTime"`
	TimeZone string `json:"timeZone"`
}

// Time returns the CalendarDateTime parsed as a time.Time in UTC.
func (cdt CalendarDateTime) Time() time.Time {
	loc := graphTimeZoneLocation(cdt.TimeZone)
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

// windowsTimeZoneToIANA maps common Microsoft Graph Windows timezone names to IANA IDs.
var windowsTimeZoneToIANA = map[string]string{
	"GMT Standard Time":              "Europe/London",
	"Greenwich Standard Time":        "Etc/GMT",
	"W. Europe Standard Time":        "Europe/Berlin",
	"Central European Standard Time": "Europe/Budapest",
	"Romance Standard Time":          "Europe/Paris",
	"Eastern Standard Time":          "America/New_York",
	"Central Standard Time":          "America/Chicago",
	"Mountain Standard Time":         "America/Denver",
	"Pacific Standard Time":          "America/Los_Angeles",
}

func graphTimeZoneLocation(tz string) *time.Location {
	if tz != "" {
		if loc, err := time.LoadLocation(tz); err == nil {
			return loc
		}
		if iana, ok := windowsTimeZoneToIANA[tz]; ok {
			if loc, err := time.LoadLocation(iana); err == nil {
				return loc
			}
		}
	}
	if loc, err := time.LoadLocation(localTimeZone()); err == nil {
		return loc
	}
	return time.UTC
}

// ─── Calendar API Methods ─────────────────────────────────────────────────────

// GetCalendarEventsForRange fetches the user's calendar events within a specific start and end time.
func (gc *GraphClient) GetCalendarEventsForRange(start time.Time, end time.Time) ([]CalendarEvent, error) {
	startStr := start.Format("2006-01-02T15:04:05Z")
	endStr := end.Format("2006-01-02T15:04:05Z")

	reqURL := fmt.Sprintf(
		"%s/me/calendarView?startDateTime=%s&endDateTime=%s&$select=id,subject,start,end,location,organizer,attendees,isAllDay,isCancelled,isOnlineMeeting,onlineMeeting,showAs,responseRequested,responseStatus,bodyPreview,webLink&$orderby=start/dateTime&$top=100",
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

// GetCalendarEvents fetches the user's calendar events from the start of today to N days in the future.
func (gc *GraphClient) GetCalendarEvents(days int) ([]CalendarEvent, error) {
	if days <= 0 {
		days = 30
	}
	now := time.Now()
	// Start at midnight local time today so past events are excluded in list view.
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).UTC()
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

// ─── Calendar Event Creation Types ───────────────────────────────────────────

// ParsedAttendee holds a parsed attendee email and Graph attendee type.
type ParsedAttendee struct {
	Address string
	Type    string // required, optional, resource
}

// ParseAttendeeList parses a comma-separated attendee string.
// Prefix ? marks optional, ! marks resource; default is required.
func ParseAttendeeList(s string) []ParsedAttendee {
	var out []ParsedAttendee
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		attType := "required"
		if strings.HasPrefix(part, "?") {
			attType = "optional"
			part = strings.TrimSpace(part[1:])
		} else if strings.HasPrefix(part, "!") {
			attType = "resource"
			part = strings.TrimSpace(part[1:])
		}
		addr := part
		if strings.Contains(part, "<") && strings.Contains(part, ">") {
			start := strings.Index(part, "<")
			end := strings.Index(part, ">")
			if start < end {
				addr = strings.TrimSpace(part[start+1 : end])
			}
		}
		if addr != "" {
			out = append(out, ParsedAttendee{Address: addr, Type: attType})
		}
	}
	return out
}

// ParseAttendeeField parses comma-separated addresses using defaultType unless ! marks a resource.
func ParseAttendeeField(s string, defaultType string) []ParsedAttendee {
	var out []ParsedAttendee
	for _, a := range ParseAttendeeList(s) {
		if a.Type == "resource" {
			out = append(out, a)
		} else {
			out = append(out, ParsedAttendee{Address: a.Address, Type: defaultType})
		}
	}
	return out
}

// IsValidAttendeeEmail reports whether s looks like a complete email address.
func IsValidAttendeeEmail(s string) bool {
	s = strings.TrimSpace(s)
	at := strings.LastIndex(s, "@")
	return at > 0 && at < len(s)-1
}

// FilterValidAttendees returns only attendees with plausible email addresses.
func FilterValidAttendees(attendees []ParsedAttendee) []ParsedAttendee {
	var out []ParsedAttendee
	for _, a := range attendees {
		if IsValidAttendeeEmail(a.Address) {
			out = append(out, a)
		}
	}
	return out
}

// RecurrenceSettings holds user-configured recurrence for event creation.
type RecurrenceSettings struct {
	Enabled       bool
	PatternType   string   // daily, weekly, absoluteMonthly, relativeMonthly, absoluteYearly, relativeYearly
	Interval      int
	DaysOfWeek    []string // monday..sunday for weekly
	DayOfMonth    int      // 1-31 for absoluteMonthly/absoluteYearly
	Index         string   // first, second, third, fourth, last for relative*
	DayOfWeek     string   // monday..sunday for relative*
	RangeType     string   // noEnd, endDate, numbered
	StartDate     string   // YYYY-MM-DD
	EndDate       string   // YYYY-MM-DD for endDate range
	NumberedCount int      // for numbered range
}

// BuildRecurrencePattern converts RecurrenceSettings to Graph patternedRecurrence JSON.
func BuildRecurrencePattern(r RecurrenceSettings) map[string]interface{} {
	if !r.Enabled {
		return nil
	}
	interval := r.Interval
	if interval <= 0 {
		interval = 1
	}
	pattern := map[string]interface{}{
		"type":     r.PatternType,
		"interval": interval,
	}
	switch r.PatternType {
	case "weekly":
		if len(r.DaysOfWeek) > 0 {
			pattern["daysOfWeek"] = r.DaysOfWeek
		} else {
			pattern["daysOfWeek"] = []string{"monday"}
		}
	case "absoluteMonthly":
		day := r.DayOfMonth
		if day <= 0 {
			day = 1
		}
		pattern["dayOfMonth"] = day
	case "relativeMonthly", "relativeYearly":
		idx := r.Index
		if idx == "" {
			idx = "first"
		}
		dow := r.DayOfWeek
		if dow == "" {
			dow = "monday"
		}
		pattern["index"] = idx
		pattern["daysOfWeek"] = []string{dow}
		if r.PatternType == "relativeYearly" {
			month := 1
			if r.DayOfMonth > 0 && r.DayOfMonth <= 12 {
				month = r.DayOfMonth
			}
			pattern["month"] = month
		}
	case "absoluteYearly":
		day := r.DayOfMonth
		if day <= 0 {
			day = 1
		}
		pattern["dayOfMonth"] = day
		month := 1
		if r.Interval > 0 && r.Interval <= 12 {
			month = r.Interval
		}
		pattern["month"] = month
	}
	rangeType := r.RangeType
	if rangeType == "" {
		rangeType = "noEnd"
	}
	recRange := map[string]interface{}{
		"type":      rangeType,
		"startDate": r.StartDate,
	}
	switch rangeType {
	case "endDate":
		recRange["endDate"] = r.EndDate
	case "numbered":
		count := r.NumberedCount
		if count <= 0 {
			count = 10
		}
		recRange["numberOfOccurrences"] = count
	}
	return map[string]interface{}{
		"pattern": pattern,
		"range":   recRange,
	}
}

// RecurrencePreview returns a human-readable recurrence summary.
func RecurrencePreview(r RecurrenceSettings) string {
	if !r.Enabled {
		return "None"
	}
	interval := r.Interval
	if interval <= 0 {
		interval = 1
	}
	var base string
	switch r.PatternType {
	case "daily":
		if interval == 1 {
			base = "Every day"
		} else {
			base = fmt.Sprintf("Every %d days", interval)
		}
	case "weekly":
		days := strings.Join(r.DaysOfWeek, ", ")
		if days == "" {
			days = "monday"
		}
		if interval == 1 {
			base = fmt.Sprintf("Every week on %s", days)
		} else {
			base = fmt.Sprintf("Every %d weeks on %s", interval, days)
		}
	case "absoluteMonthly":
		day := r.DayOfMonth
		if day <= 0 {
			day = 1
		}
		base = fmt.Sprintf("Monthly on day %d", day)
	case "relativeMonthly":
		idx, dow := r.Index, r.DayOfWeek
		if idx == "" {
			idx = "first"
		}
		if dow == "" {
			dow = "monday"
		}
		base = fmt.Sprintf("Monthly on the %s %s", idx, dow)
	case "absoluteYearly":
		base = "Yearly"
	case "relativeYearly":
		idx, dow := r.Index, r.DayOfWeek
		if idx == "" {
			idx = "first"
		}
		if dow == "" {
			dow = "monday"
		}
		base = fmt.Sprintf("Yearly on the %s %s", idx, dow)
	default:
		base = r.PatternType
	}
	switch r.RangeType {
	case "endDate":
		return base + " until " + r.EndDate
	case "numbered":
		return fmt.Sprintf("%s, %d times", base, r.NumberedCount)
	}
	return base
}

// CreateEventRequest holds all fields for creating a calendar event.
type CreateEventRequest struct {
	Subject               string
	Body                  string
	Start                 time.Time
	End                   time.Time
	Location              string
	Attendees             []ParsedAttendee
	IsAllDay              bool
	ShowAs                string
	IsOnlineMeeting       bool
	IsReminderOn          bool
	ReminderMinutesBefore int
	Recurrence            RecurrenceSettings
}

func localTimeZone() string {
	// Graph requires IANA (e.g. "Europe/London") or Windows names — not abbreviations like "BST".
	if tz := os.Getenv("TZ"); tz != "" {
		tz = strings.TrimPrefix(tz, ":")
		if tz != "" && tz != "Local" {
			return tz
		}
	}
	if data, err := os.ReadFile("/etc/timezone"); err == nil {
		if tz := strings.TrimSpace(string(data)); tz != "" {
			return tz
		}
	}
	if link, err := os.Readlink("/etc/localtime"); err == nil {
		if tz := ianaFromZoneinfoPath(link); tz != "" {
			return tz
		}
	}
	if name := time.Now().Location().String(); name != "" && name != "Local" {
		return name
	}
	return "UTC"
}

func ianaFromZoneinfoPath(path string) string {
	const marker = "zoneinfo/"
	if idx := strings.Index(path, marker); idx >= 0 {
		return path[idx+len(marker):]
	}
	const prefix = "/usr/share/zoneinfo/"
	if strings.HasPrefix(path, prefix) {
		return strings.TrimPrefix(path, prefix)
	}
	return ""
}

func toGraphDateTime(t time.Time) CalendarDateTime {
	local := t.In(time.Local)
	return CalendarDateTime{
		DateTime: local.Format("2006-01-02T15:04:05"),
		TimeZone: localTimeZone(),
	}
}

func buildCalendarEventPayload(req CreateEventRequest) map[string]interface{} {
	body := map[string]interface{}{
		"subject": req.Subject,
		"body": map[string]string{
			"contentType": "Text",
			"content":     req.Body,
		},
		"start":    toGraphDateTime(req.Start),
		"end":      toGraphDateTime(req.End),
		"isAllDay": req.IsAllDay,
	}
	if req.Location != "" {
		body["location"] = map[string]string{"displayName": req.Location}
	}
	showAs := req.ShowAs
	if showAs == "" {
		showAs = "busy"
	}
	body["showAs"] = showAs
	if req.IsReminderOn {
		body["isReminderOn"] = true
		mins := req.ReminderMinutesBefore
		if mins <= 0 {
			mins = 15
		}
		body["reminderMinutesBeforeStart"] = mins
	} else {
		body["isReminderOn"] = false
	}
	if req.IsOnlineMeeting {
		body["isOnlineMeeting"] = true
		body["onlineMeetingProvider"] = "teamsForBusiness"
	} else {
		body["isOnlineMeeting"] = false
	}
	if len(req.Attendees) > 0 {
		var atts []map[string]interface{}
		for _, a := range req.Attendees {
			attType := a.Type
			if attType == "" {
				attType = "required"
			}
			atts = append(atts, map[string]interface{}{
				"emailAddress": map[string]string{"address": a.Address},
				"type":         attType,
			})
		}
		body["attendees"] = atts
	}
	if rec := BuildRecurrencePattern(req.Recurrence); rec != nil {
		body["recurrence"] = rec
	}
	return body
}

// GetCalendarEvent fetches a single event with fields needed for editing.
func (gc *GraphClient) GetCalendarEvent(eventID string) (*CalendarEvent, error) {
	reqURL := fmt.Sprintf(
		"%s/me/events/%s?$select=id,subject,body,bodyPreview,start,end,location,organizer,attendees,isAllDay,isCancelled,isOnlineMeeting,onlineMeeting,showAs,isReminderOn,reminderMinutesBeforeStart,responseRequested,responseStatus,webLink",
		graphBaseURL, url.PathEscape(eventID),
	)
	resp, err := gc.client.Get(reqURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to get event: status %d: %s", resp.StatusCode, string(bodyBytes))
	}
	var ev CalendarEvent
	if err := json.NewDecoder(resp.Body).Decode(&ev); err != nil {
		return nil, err
	}
	return &ev, nil
}

// CreateCalendarEvent creates a new calendar event via POST /me/events.
func (gc *GraphClient) CreateCalendarEvent(req CreateEventRequest) (*CalendarEvent, error) {
	body := buildCalendarEventPayload(req)
	if !req.IsReminderOn {
		delete(body, "isReminderOn")
	}
	if !req.IsOnlineMeeting {
		delete(body, "isOnlineMeeting")
	}
	if len(req.Attendees) == 0 {
		delete(body, "attendees")
	}

	jsonBytes, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	reqURL := fmt.Sprintf("%s/me/events", graphBaseURL)
	httpReq, err := http.NewRequest(http.MethodPost, reqURL, bytes.NewBuffer(jsonBytes))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Prefer", fmt.Sprintf(`outlook.timezone="%s"`, localTimeZone()))

	resp, err := gc.client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to create event: status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var ev CalendarEvent
	if err := json.NewDecoder(resp.Body).Decode(&ev); err != nil {
		return nil, err
	}
	return &ev, nil
}

// UpdateCalendarEvent updates an existing calendar event via PATCH /me/events/{id}.
func (gc *GraphClient) UpdateCalendarEvent(eventID string, req CreateEventRequest) (*CalendarEvent, error) {
	body := buildCalendarEventPayload(req)
	if len(req.Attendees) == 0 {
		body["attendees"] = []map[string]interface{}{}
	}
	jsonBytes, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	reqURL := fmt.Sprintf("%s/me/events/%s", graphBaseURL, url.PathEscape(eventID))
	httpReq, err := http.NewRequest(http.MethodPatch, reqURL, bytes.NewBuffer(jsonBytes))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Prefer", fmt.Sprintf(`outlook.timezone="%s"`, localTimeZone()))

	resp, err := gc.client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to update event: status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var ev CalendarEvent
	if err := json.NewDecoder(resp.Body).Decode(&ev); err != nil {
		return nil, err
	}
	return &ev, nil
}

// DeleteCalendarEvent permanently deletes a calendar event via DELETE /me/events/{id}.
func (gc *GraphClient) DeleteCalendarEvent(eventID string) error {
	reqURL := fmt.Sprintf("%s/me/events/%s", graphBaseURL, url.PathEscape(eventID))
	req, err := http.NewRequest(http.MethodDelete, reqURL, nil)
	if err != nil {
		return err
	}
	resp, err := gc.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to delete event: status %d: %s", resp.StatusCode, string(bodyBytes))
	}
	return nil
}

// ScheduleInformation holds free/busy data for one mailbox from getSchedule.
type ScheduleInformation struct {
	ScheduleID       string `json:"scheduleId"`
	AvailabilityView string `json:"availabilityView"`
	WorkingHours     *struct {
		StartTime string `json:"startTime"`
		EndTime   string `json:"endTime"`
	} `json:"workingHours"`
}

// GetAttendeeSchedule fetches free/busy via POST /me/calendar/getSchedule.
func (gc *GraphClient) GetAttendeeSchedule(schedules []string, start, end time.Time, intervalMin int) ([]ScheduleInformation, error) {
	if len(schedules) == 0 {
		return nil, nil
	}
	if intervalMin <= 0 {
		intervalMin = 30
	}
	reqBody := map[string]interface{}{
		"schedules":                schedules,
		"startTime":                toGraphDateTime(start),
		"endTime":                  toGraphDateTime(end),
		"availabilityViewInterval": intervalMin,
	}
	jsonBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	reqURL := fmt.Sprintf("%s/me/calendar/getSchedule", graphBaseURL)
	httpReq, err := http.NewRequest(http.MethodPost, reqURL, bytes.NewBuffer(jsonBytes))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Prefer", fmt.Sprintf(`outlook.timezone="%s"`, localTimeZone()))

	resp, err := gc.client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to get schedule: status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var result struct {
		Value []ScheduleInformation `json:"value"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return result.Value, nil
}

// MeetingTimeSuggestion is one suggested meeting slot from findMeetingTimes.
type MeetingTimeSuggestion struct {
	Confidence float64
	Start      time.Time
	End        time.Time
	Reason     string
}

// MeetingTimeSuggestionsResult holds findMeetingTimes output.
type MeetingTimeSuggestionsResult struct {
	Suggestions            []MeetingTimeSuggestion
	EmptySuggestionsReason string
}

// FindMeetingTimesRequest holds parameters for findMeetingTimes.
type FindMeetingTimesRequest struct {
	Attendees      []ParsedAttendee
	Duration       time.Duration
	SearchStart    time.Time
	SearchEnd      time.Time
	ActivityDomain string
}

// FindMeetingTimes suggests meeting times via POST /me/findMeetingTimes.
func (gc *GraphClient) FindMeetingTimes(req FindMeetingTimesRequest) (*MeetingTimeSuggestionsResult, error) {
	domain := req.ActivityDomain
	if domain == "" {
		domain = "work"
	}
	durMins := int(req.Duration.Minutes())
	if durMins <= 0 {
		durMins = 60
	}
	durationISO := fmt.Sprintf("PT%dM", durMins)

	var attendees []map[string]interface{}
	for _, a := range req.Attendees {
		attendees = append(attendees, map[string]interface{}{
			"type":         a.Type,
			"emailAddress": map[string]string{"address": a.Address},
		})
	}

	reqBody := map[string]interface{}{
		"attendees": attendees,
		"timeConstraint": map[string]interface{}{
			"activityDomain": domain,
			"timeSlots": []map[string]interface{}{
				{
					"start": toGraphDateTime(req.SearchStart),
					"end":   toGraphDateTime(req.SearchEnd),
				},
			},
		},
		"meetingDuration":         durationISO,
		"returnSuggestionReasons": true,
	}

	jsonBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	reqURL := fmt.Sprintf("%s/me/findMeetingTimes", graphBaseURL)
	httpReq, err := http.NewRequest(http.MethodPost, reqURL, bytes.NewBuffer(jsonBytes))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Prefer", fmt.Sprintf(`outlook.timezone="%s"`, localTimeZone()))

	resp, err := gc.client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to find meeting times: status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var raw struct {
		EmptySuggestionsReason string `json:"emptySuggestionsReason"`
		MeetingTimeSuggestions []struct {
			Confidence      float64 `json:"confidence"`
			MeetingTimeSlot struct {
				Start CalendarDateTime `json:"start"`
				End   CalendarDateTime `json:"end"`
			} `json:"meetingTimeSlot"`
			SuggestionReason string `json:"suggestionReason"`
		} `json:"meetingTimeSuggestions"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}

	out := &MeetingTimeSuggestionsResult{
		EmptySuggestionsReason: raw.EmptySuggestionsReason,
	}
	for _, s := range raw.MeetingTimeSuggestions {
		out.Suggestions = append(out.Suggestions, MeetingTimeSuggestion{
			Confidence: s.Confidence,
			Start:      s.MeetingTimeSlot.Start.Time(),
			End:        s.MeetingTimeSlot.End.Time(),
			Reason:     s.SuggestionReason,
		})
	}
	return out, nil
}

// filterAndSortMeetingSuggestions drops past slots and prefers the event day.
func filterAndSortMeetingSuggestions(suggestions []MeetingTimeSuggestion, preferDay, now time.Time) []MeetingTimeSuggestion {
	now = now.In(time.Local)
	py, pm, pd := preferDay.In(time.Local).Date()
	filtered := make([]MeetingTimeSuggestion, 0, len(suggestions))
	for _, s := range suggestions {
		if s.Start.Local().Before(now) {
			continue
		}
		filtered = append(filtered, s)
	}
	sort.SliceStable(filtered, func(i, j int) bool {
		si := filtered[i].Start.Local()
		sj := filtered[j].Start.Local()
		yi, mi, di := si.Date()
		yj, mj, dj := sj.Date()
		sameI := yi == py && mi == pm && di == pd
		sameJ := yj == py && mj == pm && dj == pd
		if sameI != sameJ {
			return sameI
		}
		return si.Before(sj)
	})
	return filtered
}

// formatMeetingConfidence formats Graph findMeetingTimes confidence (0–1 or 0–100).
func formatMeetingConfidence(c float64) string {
	if c > 1 {
		return fmt.Sprintf("%.0f%%", c)
	}
	return fmt.Sprintf("%.0f%%", c*100)
}

// AvailabilitySlotStatus maps getSchedule availabilityView digit to a label.
func AvailabilitySlotStatus(code byte) string {
	switch code {
	case '0':
		return "free"
	case '1':
		return "tentative"
	case '2':
		return "busy"
	case '3':
		return "oof"
	case '4':
		return "workingElsewhere"
	default:
		return "unknown"
	}
}

// AvailabilitySymbol returns a single-char symbol for timeline rendering.
func AvailabilitySymbol(code byte) string {
	switch code {
	case '0':
		return "░"
	case '1':
		return "▒"
	case '2':
		return "█"
	case '3':
		return "▓"
	case '4':
		return "▒"
	default:
		return "?"
	}
}

// CountScheduleConflicts counts attendees busy/OOF during the proposed window.
// scheduleQueryStart is when availabilityView slot 0 begins (the getSchedule request start).
func CountScheduleConflicts(schedules []ScheduleInformation, scheduleQueryStart, windowStart, windowEnd time.Time, intervalMin int) int {
	if intervalMin <= 0 {
		intervalMin = 30
	}
	slotDur := time.Duration(intervalMin) * time.Minute
	conflicts := 0
	for _, sch := range schedules {
		if sch.AvailabilityView == "" {
			continue
		}
		hasConflict := false
		for i := 0; i < len(sch.AvailabilityView); i++ {
			slotStart := scheduleQueryStart.Add(time.Duration(i) * slotDur)
			slotEnd := slotStart.Add(slotDur)
			if !slotEnd.After(windowStart) || !slotStart.Before(windowEnd) {
				continue
			}
			code := sch.AvailabilityView[i]
			if code == '2' || code == '3' {
				hasConflict = true
				break
			}
		}
		if hasConflict {
			conflicts++
		}
	}
	return conflicts
}

// ParseEventDateTime parses "YYYY-MM-DD HH:MM" in local timezone.
func ParseEventDateTime(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	formats := []string{
		"2006-01-02 15:04",
		"2006-01-02T15:04",
		"2006-01-02 15:04:05",
	}
	for _, f := range formats {
		if t, err := time.ParseInLocation(f, s, time.Local); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("invalid date/time: %q (use YYYY-MM-DD HH:MM)", s)
}

// FormatEventDateTime formats a time for the create-event form.
func FormatEventDateTime(t time.Time) string {
	return t.In(time.Local).Format("2006-01-02 15:04")
}

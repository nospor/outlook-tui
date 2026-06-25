package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
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
	ID               string      `json:"id"`
	Subject          string      `json:"subject"`
	BodyPreview      string      `json:"bodyPreview"`
	ReceivedDateTime time.Time   `json:"receivedDateTime"`
	IsRead           bool        `json:"isRead"`
	HasAttachments   bool        `json:"hasAttachments"`
	From             Recipient   `json:"from"`
	ToRecipients     []Recipient `json:"toRecipients"`
	CcRecipients     []Recipient `json:"ccRecipients"`
	Body             ItemBody    `json:"body"`
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
	ID           string `json:"id"`
	Name         string `json:"name"`
	ContentType  string `json:"contentType"`
	Size         int    `json:"size"`
	IsInline     bool   `json:"isInline"`
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

func (gc *GraphClient) GetMessages(folderID string) ([]Message, error) {
	reqURL := fmt.Sprintf("%s/me/mailFolders/%s/messages?$select=id,subject,bodyPreview,receivedDateTime,isRead,hasAttachments,from,toRecipients,ccRecipients&$top=50&$orderby=receivedDateTime%%20desc", graphBaseURL, url.PathEscape(folderID))
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

func (gc *GraphClient) SendMessage(subject, bodyText, recipientAddress string) error {
	reqURL := fmt.Sprintf("%s/me/sendMail", graphBaseURL)

	sendReq := struct {
		Message struct {
			Subject      string      `json:"subject"`
			Body         ItemBody    `json:"body"`
			ToRecipients []Recipient `json:"toRecipients"`
		} `json:"message"`
		SaveToSentItems string `json:"saveToSentItems"`
	}{}

	sendReq.Message.Subject = subject
	sendReq.Message.Body = ItemBody{
		ContentType: "Text",
		Content:     bodyText,
	}
	sendReq.Message.ToRecipients = []Recipient{
		{
			EmailAddress: EmailAddress{
				Address: recipientAddress,
			},
		},
	}
	sendReq.SaveToSentItems = "true"

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

func (gc *GraphClient) DeleteMessage(messageID string) error {
	reqURL := fmt.Sprintf("%s/me/messages/%s/move", graphBaseURL, url.PathEscape(messageID))

	moveReq := struct {
		DestinationID string `json:"destinationId"`
	}{
		DestinationID: "deleteditems",
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
		return fmt.Errorf("failed to delete message: status %d: %s", resp.StatusCode, string(bodyBytes))
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

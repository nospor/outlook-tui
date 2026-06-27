package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// DB wraps a SQLite database used to cache mail data locally.
type DB struct {
	db *sql.DB
}

// GetCacheDir returns (and creates) ~/.cache/outlook-tui.
func GetCacheDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".cache", "outlook-tui")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	return dir, nil
}

// OpenDB opens (or creates) the SQLite database at ~/.cache/outlook-tui/db.db.
// We use the default DELETE journal mode (no WAL) so the database is always a
// single db.db file with no -wal/-shm sidecars. This means a dirty shutdown
// (e.g. laptop restart while the app is open) can never leave a stale WAL that
// causes duplicate or corrupted data on next startup.
func OpenDB() (*DB, error) {
	dir, err := GetCacheDir()
	if err != nil {
		return nil, fmt.Errorf("db: cannot resolve cache dir: %w", err)
	}

	path := filepath.Join(dir, "db.db")
	sqlDB, err := sql.Open("sqlite3", path+"?_foreign_keys=on")
	if err != nil {
		return nil, fmt.Errorf("db: open: %w", err)
	}

	d := &DB{db: sqlDB}
	if err := d.migrate(); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("db: migrate: %w", err)
	}
	return d, nil
}

// Close closes the underlying database connection.
func (d *DB) Close() error {
	return d.db.Close()
}

// migrate creates the schema on first run (idempotent).
func (d *DB) migrate() error {
	_, err := d.db.Exec(`
		CREATE TABLE IF NOT EXISTS messages (
			id                TEXT PRIMARY KEY,
			folder_id         TEXT NOT NULL,
			conversation_id   TEXT NOT NULL DEFAULT '',
			subject           TEXT NOT NULL DEFAULT '',
			body_preview      TEXT NOT NULL DEFAULT '',
			received_datetime TEXT NOT NULL DEFAULT '',
			is_read           INTEGER NOT NULL DEFAULT 0,
			has_attachments   INTEGER NOT NULL DEFAULT 0,
			from_name         TEXT NOT NULL DEFAULT '',
			from_address      TEXT NOT NULL DEFAULT '',
			to_recipients     TEXT NOT NULL DEFAULT '[]',
			cc_recipients     TEXT NOT NULL DEFAULT '[]',
			body_content_type TEXT NOT NULL DEFAULT '',
			body_content      TEXT NOT NULL DEFAULT '',
			fetched_at        TEXT NOT NULL DEFAULT ''
		);
		CREATE INDEX IF NOT EXISTS idx_messages_folder ON messages(folder_id);
		CREATE INDEX IF NOT EXISTS idx_messages_received ON messages(folder_id, received_datetime DESC);
	`)
	return err
}

// recipientsJSON marshals a []Recipient to a JSON string (never errors in practice).
func recipientsJSON(rs []Recipient) string {
	b, _ := json.Marshal(rs)
	return string(b)
}

// parseRecipients unmarshals a JSON string back to []Recipient.
func parseRecipients(s string) []Recipient {
	var rs []Recipient
	_ = json.Unmarshal([]byte(s), &rs)
	return rs
}

// UpsertMessage inserts or replaces a message in the cache.
func (d *DB) UpsertMessage(folderID string, msg Message) error {
	_, err := d.db.Exec(`
		INSERT OR REPLACE INTO messages (
			id, folder_id, conversation_id, subject, body_preview,
			received_datetime, is_read, has_attachments,
			from_name, from_address,
			to_recipients, cc_recipients,
			body_content_type, body_content,
			fetched_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		msg.ID,
		folderID,
		msg.ConversationID,
		msg.Subject,
		msg.BodyPreview,
		msg.ReceivedDateTime.UTC().Format(time.RFC3339Nano),
		boolToInt(msg.IsRead),
		boolToInt(msg.HasAttachments),
		msg.From.EmailAddress.Name,
		msg.From.EmailAddress.Address,
		recipientsJSON(msg.ToRecipients),
		recipientsJSON(msg.CcRecipients),
		msg.Body.ContentType,
		msg.Body.Content,
		time.Now().UTC().Format(time.RFC3339Nano),
	)
	return err
}

// UpsertMessages upserts a slice of messages in a single transaction, preserving cached body content.
func (d *DB) UpsertMessages(folderID string, msgs []Message) error {
	tx, err := d.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	stmt, err := tx.Prepare(`
		INSERT INTO messages (
			id, folder_id, conversation_id, subject, body_preview,
			received_datetime, is_read, has_attachments,
			from_name, from_address,
			to_recipients, cc_recipients,
			body_content_type, body_content,
			fetched_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			folder_id = excluded.folder_id,
			conversation_id = excluded.conversation_id,
			subject = excluded.subject,
			body_preview = excluded.body_preview,
			received_datetime = excluded.received_datetime,
			is_read = excluded.is_read,
			has_attachments = excluded.has_attachments,
			from_name = excluded.from_name,
			from_address = excluded.from_address,
			to_recipients = excluded.to_recipients,
			cc_recipients = excluded.cc_recipients,
			fetched_at = excluded.fetched_at
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	now := time.Now().UTC().Format(time.RFC3339Nano)
	for _, msg := range msgs {
		_, err := stmt.Exec(
			msg.ID,
			folderID,
			msg.ConversationID,
			msg.Subject,
			msg.BodyPreview,
			msg.ReceivedDateTime.UTC().Format(time.RFC3339Nano),
			boolToInt(msg.IsRead),
			boolToInt(msg.HasAttachments),
			msg.From.EmailAddress.Name,
			msg.From.EmailAddress.Address,
			recipientsJSON(msg.ToRecipients),
			recipientsJSON(msg.CcRecipients),
			msg.Body.ContentType,
			msg.Body.Content,
			now,
		)
		if err != nil {
			return err
		}
	}

	// Delete any cached messages in this folder that are not in the new messages list
	if len(msgs) == 0 {
		_, err = tx.Exec(`DELETE FROM messages WHERE folder_id = ?`, folderID)
		if err != nil {
			return err
		}
	} else {
		placeholders := make([]string, len(msgs))
		args := make([]interface{}, len(msgs)+1)
		args[0] = folderID
		for i, msg := range msgs {
			placeholders[i] = "?"
			args[i+1] = msg.ID
		}
		query := fmt.Sprintf(`DELETE FROM messages WHERE folder_id = ? AND id NOT IN (%s)`, strings.Join(placeholders, ","))
		_, err = tx.Exec(query, args...)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

// GetMessages retrieves all cached messages for a folder, ordered by received_datetime desc.
func (d *DB) GetMessages(folderID string) ([]Message, error) {
	rows, err := d.db.Query(`
		SELECT id, conversation_id, subject, body_preview,
		       received_datetime, is_read, has_attachments,
		       from_name, from_address,
		       to_recipients, cc_recipients,
		       body_content_type, body_content
		FROM messages
		WHERE folder_id = ?
		ORDER BY received_datetime DESC`, folderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var msgs []Message
	for rows.Next() {
		var m Message
		var receivedStr string
		var isRead, hasAttachments int
		var fromName, fromAddress string
		var toJSON, ccJSON string
		var bodyType, bodyContent string

		if err := rows.Scan(
			&m.ID, &m.ConversationID, &m.Subject, &m.BodyPreview,
			&receivedStr, &isRead, &hasAttachments,
			&fromName, &fromAddress,
			&toJSON, &ccJSON,
			&bodyType, &bodyContent,
		); err != nil {
			return nil, err
		}

		m.ReceivedDateTime, _ = time.Parse(time.RFC3339Nano, receivedStr)
		m.IsRead = isRead != 0
		m.HasAttachments = hasAttachments != 0
		m.From = Recipient{EmailAddress: EmailAddress{Name: fromName, Address: fromAddress}}
		m.ToRecipients = parseRecipients(toJSON)
		m.CcRecipients = parseRecipients(ccJSON)
		m.Body = ItemBody{ContentType: bodyType, Content: bodyContent}

		msgs = append(msgs, m)
	}
	return msgs, rows.Err()
}

// GetMessage retrieves a single cached message by ID.
func (d *DB) GetMessage(messageID string) (*Message, error) {
	row := d.db.QueryRow(`
		SELECT id, conversation_id, subject, body_preview,
		       received_datetime, is_read, has_attachments,
		       from_name, from_address,
		       to_recipients, cc_recipients,
		       body_content_type, body_content
		FROM messages
		WHERE id = ?`, messageID)

	var m Message
	var receivedStr string
	var isRead, hasAttachments int
	var fromName, fromAddress string
	var toJSON, ccJSON string
	var bodyType, bodyContent string

	err := row.Scan(
		&m.ID, &m.ConversationID, &m.Subject, &m.BodyPreview,
		&receivedStr, &isRead, &hasAttachments,
		&fromName, &fromAddress,
		&toJSON, &ccJSON,
		&bodyType, &bodyContent,
	)
	if err != nil {
		return nil, err
	}

	m.ReceivedDateTime, _ = time.Parse(time.RFC3339Nano, receivedStr)
	m.IsRead = isRead != 0
	m.HasAttachments = hasAttachments != 0
	m.From = Recipient{EmailAddress: EmailAddress{Name: fromName, Address: fromAddress}}
	m.ToRecipients = parseRecipients(toJSON)
	m.CcRecipients = parseRecipients(ccJSON)
	m.Body = ItemBody{ContentType: bodyType, Content: bodyContent}

	return &m, nil
}

// DeleteMessage removes a message from the cache by ID.
func (d *DB) DeleteMessage(messageID string) error {
	_, err := d.db.Exec(`DELETE FROM messages WHERE id = ?`, messageID)
	return err
}

// UpdateReadStatus updates the is_read flag for a message in the cache.
func (d *DB) UpdateReadStatus(messageID string, isRead bool) error {
	_, err := d.db.Exec(`UPDATE messages SET is_read = ? WHERE id = ?`, boolToInt(isRead), messageID)
	return err
}

// Contact represents a name and email address pair.
type Contact struct {
	Name    string
	Address string
}

// GetContacts retrieves all unique contacts from the messages table.
func (d *DB) GetContacts() ([]Contact, error) {
	// Try full query with JSON extraction
	rows, err := d.db.Query(`
		SELECT DISTINCT name, address FROM (
			SELECT from_name AS name, from_address AS address FROM messages WHERE from_address != '' AND from_address IS NOT NULL
			UNION
			SELECT json_extract(value, '$.emailAddress.name') AS name, json_extract(value, '$.emailAddress.address') AS address 
			FROM messages, json_each(to_recipients)
			UNION
			SELECT json_extract(value, '$.emailAddress.name') AS name, json_extract(value, '$.emailAddress.address') AS address 
			FROM messages, json_each(cc_recipients)
		) WHERE address != '' AND address IS NOT NULL
		ORDER BY name ASC, address ASC
	`)
	if err != nil {
		// Fallback to simple from_name/from_address query if JSON1 extension is missing/errors
		rows, err = d.db.Query(`
			SELECT DISTINCT from_name AS name, from_address AS address 
			FROM messages 
			WHERE from_address != '' AND from_address IS NOT NULL
			ORDER BY name ASC, address ASC
		`)
		if err != nil {
			return nil, err
		}
	}
	defer rows.Close()

	var contacts []Contact
	for rows.Next() {
		var c Contact
		var name, address sql.NullString
		if err := rows.Scan(&name, &address); err != nil {
			return nil, err
		}
		if address.Valid && address.String != "" {
			c.Address = address.String
			if name.Valid {
				c.Name = name.String
			}
			contacts = append(contacts, c)
		}
	}
	return contacts, rows.Err()
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}


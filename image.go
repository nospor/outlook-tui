package main

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	_ "image/png"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// isImageAttachment returns true if the attachment appears to be an image.
func isImageAttachment(att Attachment) bool {
	ct := strings.ToLower(att.ContentType)
	if strings.HasPrefix(ct, "image/") {
		return true
	}
	ext := strings.ToLower(filepath.Ext(att.Name))
	switch ext {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp":
		return true
	}
	return false
}

// renderKittyImagePreview converts the image to PNG, base64-encodes it,
// and returns the Kitty Graphics Protocol escape sequence to display it
// with the specified height in terminal rows.
func renderKittyImagePreview(att Attachment, rows int) string {
	if att.ContentBytes == "" {
		return ""
	}

	rawBytes, err := base64.StdEncoding.DecodeString(att.ContentBytes)
	if err != nil {
		return fmt.Sprintf("   [Preview Error: failed to decode base64: %v]", err)
	}

	// Decode the image (any format registered: png, jpeg, gif)
	img, _, err := image.Decode(bytes.NewReader(rawBytes))
	if err != nil {
		return fmt.Sprintf("   [Preview Error: failed to decode image: %v]", err)
	}

	// Re-encode to PNG
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return fmt.Sprintf("   [Preview Error: failed to encode PNG: %v]", err)
	}

	pngBase64 := base64.StdEncoding.EncodeToString(buf.Bytes())

	// Build the Kitty escape sequence. We chunk the payload to avoid terminal buffer issues.
	var sb strings.Builder
	chunkSize := 4096
	n := len(pngBase64)

	for i := 0; i < n; i += chunkSize {
		end := i + chunkSize
		m := 1
		if end >= n {
			end = n
			m = 0
		}
		chunk := pngBase64[i:end]
		if i == 0 {
			// First chunk: specify action=Transmit and display (a=T), format=PNG (f=100), height in rows (r)
			sb.WriteString(fmt.Sprintf("\033_Ga=T,f=100,r=%d,m=%d;%s\033\\", rows, m, chunk))
		} else {
			sb.WriteString(fmt.Sprintf("\033_Gm=%d;%s\033\\", m, chunk))
		}
	}

	return sb.String()
}

// clearKittyImagesCmd returns a tea.Cmd that writes the Kitty graphics clear command to stdout.
func clearKittyImagesCmd() tea.Cmd {
	return func() tea.Msg {
		os.Stdout.Write([]byte("\033_Ga=d,d=A\033\\"))
		return nil
	}
}

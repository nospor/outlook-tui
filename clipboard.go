package main

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

// PastedImage represents an image retrieved from the clipboard.
type PastedImage struct {
	Bytes       []byte
	ContentType string // e.g. "image/png", "image/jpeg"
}

// GetClipboardImage tries to read an image from the clipboard.
// It supports Linux (wl-paste, xclip), macOS (osascript), and Windows (powershell).
func GetClipboardImage() ([]byte, string, error) {
	switch runtime.GOOS {
	case "linux":
		return getLinuxClipboardImage()
	case "darwin":
		return getMacClipboardImage()
	case "windows":
		return getWindowsClipboardImage()
	default:
		return nil, "", fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}
}

func getLinuxClipboardImage() ([]byte, string, error) {
	// 1. Try wl-paste (Wayland)
	if _, err := exec.LookPath("wl-paste"); err == nil {
		// Try PNG
		cmd := exec.Command("wl-paste", "-t", "image/png")
		var out bytes.Buffer
		cmd.Stdout = &out
		if err := cmd.Run(); err == nil && out.Len() > 0 {
			return out.Bytes(), "image/png", nil
		}
		// Try JPEG
		cmd = exec.Command("wl-paste", "-t", "image/jpeg")
		out.Reset()
		cmd.Stdout = &out
		if err := cmd.Run(); err == nil && out.Len() > 0 {
			return out.Bytes(), "image/jpeg", nil
		}
	}

	// 2. Try xclip (X11)
	if _, err := exec.LookPath("xclip"); err == nil {
		// Try PNG
		cmd := exec.Command("xclip", "-selection", "clipboard", "-t", "image/png", "-o")
		var out bytes.Buffer
		cmd.Stdout = &out
		if err := cmd.Run(); err == nil && out.Len() > 0 {
			return out.Bytes(), "image/png", nil
		}
		// Try JPEG
		cmd = exec.Command("xclip", "-selection", "clipboard", "-t", "image/jpeg", "-o")
		out.Reset()
		cmd.Stdout = &out
		if err := cmd.Run(); err == nil && out.Len() > 0 {
			return out.Bytes(), "image/jpeg", nil
		}
	}

	return nil, "", errors.New("no clipboard image found or required CLI tools (wl-paste, xclip) are missing")
}

func getMacClipboardImage() ([]byte, string, error) {
	// Try PNG using osascript
	cmd := exec.Command("osascript", "-e", "get the clipboard as «class PNGf»")
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err == nil {
		s := strings.TrimSpace(out.String())
		// Output is format like: «data PNGf89504E47...»
		s = strings.TrimPrefix(s, "«data PNGf")
		s = strings.TrimSuffix(s, "»")
		if data, err := hex.DecodeString(s); err == nil && len(data) > 0 {
			return data, "image/png", nil
		}
	}

	// Try JPEG/TIFF as fallback
	cmd = exec.Command("osascript", "-e", "get the clipboard as «class JPEG»")
	out.Reset()
	cmd.Stdout = &out
	if err := cmd.Run(); err == nil {
		s := strings.TrimSpace(out.String())
		s = strings.TrimPrefix(s, "«data JPEG")
		s = strings.TrimSuffix(s, "»")
		if data, err := hex.DecodeString(s); err == nil && len(data) > 0 {
			return data, "image/jpeg", nil
		}
	}

	return nil, "", errors.New("no clipboard image found on macOS")
}

func getWindowsClipboardImage() ([]byte, string, error) {
	psCmd := "[void][System.Reflection.Assembly]::LoadWithPartialName('System.Windows.Forms'); " +
		"$img = [System.Windows.Forms.Clipboard]::GetImage(); " +
		"if ($img -ne $null) { " +
		"  $ms = New-Object System.IO.MemoryStream; " +
		"  $img.Save($ms, [System.Drawing.Imaging.ImageFormat]::Png); " +
		"  [System.BitConverter]::ToString($ms.ToArray()) -replace '-','' " +
		"}"

	cmd := exec.Command("powershell", "-NoProfile", "-Command", psCmd)
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return nil, "", err
	}

	s := strings.TrimSpace(out.String())
	if len(s) == 0 {
		return nil, "", errors.New("no clipboard image found on Windows")
	}

	data, err := hex.DecodeString(s)
	if err != nil {
		return nil, "", err
	}

	return data, "image/png", nil
}

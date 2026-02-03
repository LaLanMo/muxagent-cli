package auth

import (
	"fmt"
	"io"
	"net/url"
	"strings"

	"github.com/skip2/go-qrcode"
)

// QRTerminalOutput generates and prints a QR code to the terminal.
func QRTerminalOutput(w io.Writer, data string) error {
	qr, err := qrcode.New(data, qrcode.Medium)
	if err != nil {
		return fmt.Errorf("failed to generate QR code: %w", err)
	}

	// Get the bitmap
	bitmap := qr.Bitmap()

	// Convert to terminal output using Unicode block characters
	// Each character represents 2 vertical pixels
	var sb strings.Builder

	// Add top border
	sb.WriteString("\n")

	for y := 0; y < len(bitmap); y += 2 {
		// Add left margin
		sb.WriteString("  ")

		for x := 0; x < len(bitmap[0]); x++ {
			top := bitmap[y][x]
			bottom := false
			if y+1 < len(bitmap) {
				bottom = bitmap[y+1][x]
			}

			// Use Unicode block characters to represent 2 vertical pixels
			// true = black (module), false = white (background)
			switch {
			case top && bottom:
				sb.WriteString("\u2588") // Full block (both black)
			case top && !bottom:
				sb.WriteString("\u2580") // Upper half block
			case !top && bottom:
				sb.WriteString("\u2584") // Lower half block
			default:
				sb.WriteString(" ") // Space (both white)
			}
		}
		sb.WriteString("\n")
	}

	sb.WriteString("\n")

	_, err = fmt.Fprint(w, sb.String())
	return err
}

// QRTerminalOutputInverted generates a QR code with inverted colors (white on black).
// This often works better on dark terminal backgrounds.
func QRTerminalOutputInverted(w io.Writer, data string) error {
	qr, err := qrcode.New(data, qrcode.Medium)
	if err != nil {
		return fmt.Errorf("failed to generate QR code: %w", err)
	}

	bitmap := qr.Bitmap()

	var sb strings.Builder
	sb.WriteString("\n")

	for y := 0; y < len(bitmap); y += 2 {
		sb.WriteString("  ")

		for x := 0; x < len(bitmap[0]); x++ {
			// Invert: black becomes white, white becomes black
			top := !bitmap[y][x]
			bottom := true
			if y+1 < len(bitmap) {
				bottom = !bitmap[y+1][x]
			}

			switch {
			case top && bottom:
				sb.WriteString("\u2588")
			case top && !bottom:
				sb.WriteString("\u2580")
			case !top && bottom:
				sb.WriteString("\u2584")
			default:
				sb.WriteString(" ")
			}
		}
		sb.WriteString("\n")
	}

	sb.WriteString("\n")

	_, err = fmt.Fprint(w, sb.String())
	return err
}

// BuildAuthURL constructs the muxagent:// URL for QR code scanning.
func BuildAuthURL(requestID, relayURL string) string {
	return fmt.Sprintf("muxagent://auth?id=%s&relay=%s", url.QueryEscape(requestID), url.QueryEscape(relayURL))
}

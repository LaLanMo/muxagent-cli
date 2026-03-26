package tasktui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

type dialogAction int

const (
	dialogActionNone dialogAction = iota
	dialogActionClose
	dialogActionQuit
)

type dialogModel interface {
	ID() string
	HandleKey(tea.KeyPressMsg) dialogAction
	View(surfaceRect) string
}

func (m *Model) openDialog(dialog dialogModel) tea.Cmd {
	m.dialog = dialog
	return m.syncInputFocus()
}

func (m *Model) closeDialog() tea.Cmd {
	m.dialog = nil
	return m.syncInputFocus()
}

func (m Model) renderDialogOverlay(base string) string {
	if m.dialog == nil {
		return base
	}
	width, height := m.viewportSize()
	overlay := m.dialog.View(surfaceRect{Width: width, Height: height})
	return composeOverlay(base, overlay, width, height)
}

func composeOverlay(base, overlay string, width, height int) string {
	if overlay == "" || width <= 0 || height <= 0 {
		return base
	}

	baseLines := strings.Split(base, "\n")
	for len(baseLines) < height {
		baseLines = append(baseLines, strings.Repeat(" ", width))
	}
	if len(baseLines) > height {
		baseLines = baseLines[:height]
	}

	overlayLines := strings.Split(overlay, "\n")
	overlayHeight := min(height, len(overlayLines))
	overlayWidth := min(width, lipgloss.Width(overlay))
	if overlayHeight <= 0 || overlayWidth <= 0 {
		return base
	}

	top := max(0, (height-overlayHeight)/2)
	left := max(0, (width-overlayWidth)/2)

	for i := 0; i < overlayHeight; i++ {
		row := top + i
		if row < 0 || row >= len(baseLines) {
			continue
		}
		baseLine := fitLine(baseLines[row], width)
		overlayLine := ansi.Truncate(overlayLines[i], overlayWidth, "")
		prefix := ansi.Cut(baseLine, 0, left)
		suffixStart := min(width, left+overlayWidth)
		suffix := ansi.Cut(baseLine, suffixStart, width)
		baseLines[row] = prefix + overlayLine + suffix
	}

	return strings.Join(baseLines, "\n")
}

package tasktui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

const quitDialogID = "quit"

type quitDialog struct {
	selectedIndex int
}

func newQuitDialog() *quitDialog {
	return &quitDialog{}
}

func (*quitDialog) ID() string {
	return quitDialogID
}

func (q *quitDialog) HandleKey(msg tea.KeyPressMsg) dialogAction {
	switch msg.String() {
	case "ctrl+c":
		return dialogActionQuit
	case "esc":
		return dialogActionClose
	case "tab", "left", "right":
		q.selectedIndex = 1 - q.selectedIndex
		return dialogActionNone
	case "enter":
		if q.selectedIndex == 1 {
			return dialogActionQuit
		}
		return dialogActionClose
	default:
		return dialogActionNone
	}
}

func (q *quitDialog) View(surface surfaceRect) string {
	cardWidth := boundedPreferredWidth(max(1, surface.Width-8), 54, 32, 58)
	innerWidth := max(1, cardWidth-2)
	bodyWidth := max(1, innerWidth-4)

	contentLines := []string{
		dialogSurfaceLine("", bodyWidth),
	}
	for _, line := range dialogTextBlock(tuiTheme.Dialog.Title, "Quit muxagent?", bodyWidth) {
		contentLines = append(contentLines, dialogContentLine(line, bodyWidth))
	}
	contentLines = append(contentLines, dialogSurfaceLine("", bodyWidth))
	for _, line := range dialogTextBlock(
		tuiTheme.Dialog.Body,
		"This exits the task TUI and closes the current muxagent session.",
		bodyWidth,
	) {
		contentLines = append(contentLines, dialogContentLine(line, bodyWidth))
	}
	contentLines = append(contentLines, dialogSurfaceLine("", bodyWidth))
	contentLines = append(contentLines, dialogContentLine(q.renderButtons(bodyWidth), bodyWidth))
	contentLines = append(contentLines, dialogSurfaceLine("", bodyWidth))
	for _, line := range dialogTextBlock(tuiTheme.Dialog.Hint, "Tab switch  Enter confirm  Esc cancel", bodyWidth) {
		contentLines = append(contentLines, dialogContentLine(line, bodyWidth))
	}
	contentLines = append(contentLines, dialogSurfaceLine("", bodyWidth))

	return dialogCard(contentLines, innerWidth)
}

func (q *quitDialog) renderButtons(rowWidth int) string {
	cancelStyle := tuiTheme.Dialog.Button
	quitStyle := tuiTheme.Dialog.Button
	if q.selectedIndex == 0 {
		cancelStyle = tuiTheme.Dialog.ButtonActive
	} else {
		quitStyle = tuiTheme.Dialog.ButtonDanger
	}
	cancel := cancelStyle.Padding(0, 3).Render("Cancel")
	quit := quitStyle.Padding(0, 3).Render("Quit")
	gap := dialogSurfaceFill(" ")
	group := lipgloss.JoinHorizontal(lipgloss.Top, cancel, gap, quit)
	groupWidth := lipgloss.Width(group)
	leftPad := max(0, (rowWidth-groupWidth)/2)
	rightPad := max(0, rowWidth-groupWidth-leftPad)
	return dialogSurfaceFill(strings.Repeat(" ", leftPad)) +
		group +
		dialogSurfaceFill(strings.Repeat(" ", rightPad))
}

func dialogCard(lines []string, innerWidth int) string {
	moat := dialogSurfaceFill(" ")
	top := moat + tuiTheme.Dialog.Border.Render("╭"+strings.Repeat("─", innerWidth)+"╮") + moat
	bottom := moat + tuiTheme.Dialog.Border.Render("╰"+strings.Repeat("─", innerWidth)+"╯") + moat
	body := make([]string, 0, len(lines))
	leftBorder := tuiTheme.Dialog.Border.Render("│")
	rightBorder := tuiTheme.Dialog.Border.Render("│")
	for _, line := range lines {
		body = append(body, moat+leftBorder+dialogNormalizeSurfaceLine(line, innerWidth)+rightBorder+moat)
	}
	return lipgloss.JoinVertical(lipgloss.Left, append([]string{top}, append(body, bottom)...)...)
}

func dialogContentLine(content string, bodyWidth int) string {
	side := dialogSurfaceFill("  ")
	return side + dialogNormalizeSurfaceLine(content, bodyWidth) + side
}

func dialogSurfaceLine(text string, width int) string {
	if text == "" {
		return dialogSurfaceFill(strings.Repeat(" ", width+4))
	}
	return dialogContentLine(dialogSurfaceFill(text), width)
}

func dialogTextBlock(style lipgloss.Style, text string, width int) []string {
	if width <= 0 {
		return []string{""}
	}
	lines := strings.Split(style.Width(width).MaxWidth(width).Render(text), "\n")
	for i := range lines {
		lines[i] = dialogNormalizeSurfaceLine(lines[i], width)
	}
	return lines
}

func dialogNormalizeSurfaceLine(line string, width int) string {
	line = ansi.Truncate(line, width, "")
	padding := width - ansi.StringWidth(line)
	if padding <= 0 {
		return line
	}
	return line + dialogSurfaceFill(strings.Repeat(" ", padding))
}

func dialogSurfaceFill(text string) string {
	return tuiTheme.Dialog.Surface.Render(text)
}

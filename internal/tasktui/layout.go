package tasktui

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// DetailTab selects which full-screen tab is active on the detail screen.
type DetailTab int

const (
	DetailTabTimeline DetailTab = iota
	DetailTabArtifacts
)

const (
	detailTitleMaxWidth = 120
	detailBodyMaxWidth  = 100
	detailFormMaxWidth  = 96
)

func detailContentWidth(innerWidth int, activeTab DetailTab) int {
	_ = activeTab
	return innerWidth
}

func detailTitleMeasureWidth(available int) int {
	return boundedPreferredWidth(available, available, 24, detailTitleMaxWidth)
}

func detailBodyMeasureWidth(available int) int {
	return boundedPreferredWidth(available, available, 24, detailBodyMaxWidth)
}

func detailFormMeasureWidth(available int) int {
	return boundedPreferredWidth(available, available, 18, detailFormMaxWidth)
}

func renderCanvas(width, height int, header, body, footer string) string {
	contentWidth, contentHeight := innerSize(width, height)
	bodyHeight := max(1, contentHeight-lipgloss.Height(header)-lipgloss.Height(footer))
	body = lipgloss.Place(contentWidth, bodyHeight, lipgloss.Left, lipgloss.Top, body)
	page := lipgloss.JoinVertical(lipgloss.Left, header, body, footer)
	return tuiTheme.App.Canvas.Width(width).Height(height).Render(page)
}

func innerSize(width, height int) (int, int) {
	return max(20, width-4), max(10, height-2)
}

func fitLine(line string, width int) string {
	if width <= 0 {
		return ""
	}
	line = ansi.Truncate(line, width, "")
	padding := width - ansi.StringWidth(line)
	if padding < 0 {
		padding = 0
	}
	return line + strings.Repeat(" ", padding)
}

func joinHorizontal(left, right string, width int) string {
	if width <= 0 {
		return ""
	}
	if right == "" {
		return fitLine(left, width)
	}
	right = ansi.Truncate(right, width, "")
	rightWidth := ansi.StringWidth(right)
	if rightWidth >= width {
		return fitLine(right, width)
	}
	leftWidth := width - rightWidth - 1
	if leftWidth <= 0 {
		return fitLine(right, width)
	}
	left = ansi.Truncate(left, leftWidth, "")
	spaceCount := width - ansi.StringWidth(left) - rightWidth
	if spaceCount < 1 {
		spaceCount = 1
	}
	return fitLine(left+strings.Repeat(" ", spaceCount)+right, width)
}

func clamp(value, minValue, maxValue int) int {
	if maxValue < minValue {
		maxValue = minValue
	}
	if value < minValue {
		return minValue
	}
	if value > maxValue {
		return maxValue
	}
	return value
}

func boundedPreferredWidth(available, preferred, minPreferred, maxPreferred int) int {
	if available <= 0 {
		return 1
	}
	preferred = clamp(preferred, minPreferred, maxPreferred)
	return min(available, preferred)
}

package tasktui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

type artifactSidebarRowKind int

const (
	artifactSidebarRowHeader artifactSidebarRowKind = iota
	artifactSidebarRowItem
)

type artifactSidebarRow struct {
	kind      artifactSidebarRowKind
	itemIndex int
	text      string
}

func (m *Model) syncArtifactPreview(surface surfaceRect) {
	contentWidth := max(12, surface.Width)
	previewHeight := max(1, surface.Height)
	m.artifactPreview.SetWidth(contentWidth)
	m.artifactPreview.SetHeight(previewHeight)
	if len(m.artifactItems) == 0 || m.artifactIndex >= len(m.artifactItems) {
		m.artifactPreview.SetContent(tuiTheme.Artifact.Empty.Render("No artifacts yet."))
		m.artifactPreviewPath = ""
		m.artifactPreview.GotoTop()
		return
	}
	item := &m.artifactItems[m.artifactIndex]
	previousPath := m.artifactPreviewPath
	content := item.Preview
	if content == "" {
		content = "No preview available."
	}
	m.artifactPreview.SetContent(item.renderedContent(contentWidth))
	m.artifactPreviewPath = item.Path
	if item.Path != previousPath {
		m.artifactPreview.GotoTop()
	}
}

// artifactPaneSidebarWidth computes the file sidebar width for the horizontal split.
func artifactPaneSidebarWidth(totalWidth int) int {
	sidebar := totalWidth * 30 / 100
	return clamp(sidebar, 20, min(40, totalWidth/2))
}

func artifactPanePreviewRect(totalWidth, totalHeight int) surfaceRect {
	if totalWidth < 20 || totalHeight < 4 {
		return surfaceRect{}
	}
	sidebarWidth := artifactPaneSidebarWidth(totalWidth)
	previewWidth := max(12, totalWidth-sidebarWidth-1)
	return surfaceRect{
		Width:  max(10, previewWidth-2),
		Height: max(1, totalHeight-1),
	}
}

func (m Model) artifactPaneLineStyle(focused bool) lipgloss.Style {
	style := tuiTheme.Artifact.Divider
	if focused {
		style = style.Foreground(tuiTheme.Color.Text)
	}
	return style
}

func (m Model) renderArtifactPaneTitle(title string, focused bool) string {
	style := tuiTheme.Artifact.BlockTitle
	if focused {
		style = style.Foreground(tuiTheme.Color.Awaiting)
	}
	return style.Render(title)
}

func (m Model) renderArtifactsPane(surface artifactSurface) string {
	width := surface.Rect.Width
	height := surface.Rect.Height
	if width < 20 || height < 4 {
		return lipgloss.Place(width, height, lipgloss.Left, lipgloss.Top,
			tuiTheme.Artifact.Empty.Render("Terminal too small for artifact view."))
	}

	sidebarWidth := artifactPaneSidebarWidth(width)
	previewWidth := max(12, width-sidebarWidth-1) // 1 for divider

	// Render file sidebar
	fileLines := m.renderArtifactFileLines(max(12, sidebarWidth-4), max(1, height-2))
	filesContent := m.renderArtifactFilesColumn(sidebarWidth, height, fileLines)

	// Render preview pane
	previewContent := m.renderArtifactPreviewColumn(previewWidth, height)

	// Vertical divider
	divider := strings.TrimRight(strings.Repeat(m.artifactPaneLineStyle(m.focusRegion == FocusRegionArtifactPreview).Render("│")+"\n", max(1, height)), "\n")
	divider = lipgloss.Place(1, height, lipgloss.Left, lipgloss.Top, divider)

	return lipgloss.JoinHorizontal(lipgloss.Top, filesContent, divider, previewContent)
}

func (m Model) renderArtifactFilesColumn(width, height int, lines []string) string {
	innerWidth := max(10, width-2)
	titleText := fmt.Sprintf("Files (%d)", len(m.artifactItems))
	title := m.renderArtifactPaneTitle(titleText, m.focusRegion == FocusRegionArtifactFiles)

	bodyHeight := max(1, height-1)
	body := lipgloss.Place(innerWidth, bodyHeight, lipgloss.Left, lipgloss.Top, strings.Join(lines, "\n"))

	content := lipgloss.JoinVertical(lipgloss.Left, title, body)

	style := lipgloss.NewStyle().
		Width(width).
		Height(height).
		PaddingLeft(1).
		BorderLeft(true).
		BorderStyle(lipgloss.Border{Left: "│"}).
		BorderForeground(m.artifactPaneLineStyle(m.focusRegion == FocusRegionArtifactFiles).GetForeground()).
		Width(width)
	return style.Render(content)
}

func (m Model) renderArtifactPreviewColumn(width, height int) string {
	title := "Preview"
	if len(m.artifactItems) > 0 && m.artifactIndex < len(m.artifactItems) {
		title = fmt.Sprintf("Preview · %s", m.artifactItems[m.artifactIndex].PreviewTitle)
	}
	header := m.renderArtifactPaneTitle(title, m.focusRegion == FocusRegionArtifactPreview)

	innerWidth := max(10, width-2)
	lines := []string{header}
	bodyHeight := max(1, height-1)
	if strings.TrimSpace(m.artifactErrorText) != "" {
		lines = append(lines, fitLine(tuiTheme.Status.Failed.Render("× "+m.artifactErrorText), innerWidth))
		bodyHeight = max(1, bodyHeight-1)
	}
	bodyContent := lipgloss.Place(innerWidth, bodyHeight, lipgloss.Left, lipgloss.Top, m.artifactPreview.View())
	lines = append(lines, bodyContent)

	style := lipgloss.NewStyle().Width(width).Height(height).PaddingLeft(1)
	return style.Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
}

func artifactVisibleCapacity(total int) int {
	if total <= 0 {
		return 1
	}
	return min(total, 3)
}

func formatArtifactFileLabel(item artifactItem, width int) string {
	width = max(1, width)
	return truncateLeftToWidth(artifactRowPath(item), width)
}

func formatArtifactGroupLabel(label string, width int) string {
	return ansi.Truncate(strings.TrimSpace(label), max(1, width), "…")
}

func artifactRowPath(item artifactItem) string {
	switch {
	case item.DisplayPath != "":
		return item.DisplayPath
	case item.Label != "":
		return item.Label
	default:
		return item.Path
	}
}

func truncateLeftToWidth(s string, width int) string {
	if width <= 0 || s == "" {
		return ""
	}
	if ansi.StringWidth(s) <= width {
		return s
	}
	const prefix = "…"
	trimWidth := ansi.StringWidth(s) - width + ansi.StringWidth(prefix)
	return ansi.TruncateLeft(s, trimWidth, prefix)
}

func buildArtifactSidebarRows(items []artifactItem, selectedIndex, width int) ([]artifactSidebarRow, int) {
	if len(items) == 0 {
		return nil, 0
	}
	selectedIndex = clamp(selectedIndex, 0, len(items)-1)
	rows := make([]artifactSidebarRow, 0, len(items)*2)
	selectedRow := 0
	lastGroup := ""
	for i, item := range items {
		group := strings.TrimSpace(item.SourceLabel)
		if group != "" && group != lastGroup {
			rows = append(rows, artifactSidebarRow{
				kind:      artifactSidebarRowHeader,
				itemIndex: -1,
				text:      formatArtifactGroupLabel(group, width),
			})
			lastGroup = group
		}
		rows = append(rows, artifactSidebarRow{
			kind:      artifactSidebarRowItem,
			itemIndex: i,
			text:      formatArtifactFileLabel(item, max(1, width-2)),
		})
		if i == selectedIndex {
			selectedRow = len(rows) - 1
		}
	}
	return rows, selectedRow
}

func normalizeArtifactSidebarWindow(rows []artifactSidebarRow, start, end, maxRows int) (int, int) {
	if len(rows) == 0 || maxRows < 2 {
		return start, end
	}
	if start > 0 && rows[start].kind == artifactSidebarRowItem && rows[start-1].kind == artifactSidebarRowHeader {
		start--
	}
	if end-start > maxRows {
		if end < len(rows) {
			end = start + maxRows
		} else {
			start = max(0, end-maxRows)
		}
	}
	if end > start && rows[end-1].kind == artifactSidebarRowHeader {
		if end < len(rows) {
			end++
		} else {
			end--
		}
	}
	if end-start > maxRows {
		if start > 0 {
			start++
		} else if end < len(rows) {
			end--
		}
	}
	return start, min(len(rows), end)
}

func artifactSidebarHiddenFileCount(rows []artifactSidebarRow) int {
	count := 0
	for _, row := range rows {
		if row.kind == artifactSidebarRowItem {
			count++
		}
	}
	return count
}

func (m Model) renderArtifactFileLines(width, rows int) []string {
	if len(m.artifactItems) == 0 {
		return []string{tuiTheme.Artifact.Empty.Render("No artifacts yet.")}
	}
	sidebarRows, selectedRow := buildArtifactSidebarRows(m.artifactItems, m.artifactIndex, width)
	start, end := selectionWindow(len(sidebarRows), selectedRow, rows)
	start, end = normalizeArtifactSidebarWindow(sidebarRows, start, end, rows)
	lines := make([]string, 0, rows+2)
	if hidden := artifactSidebarHiddenFileCount(sidebarRows[:start]); hidden > 0 {
		lines = append(lines, tuiTheme.Artifact.Hint.Render(fmt.Sprintf("… %d earlier file(s)", hidden)))
	}
	for i := start; i < end; i++ {
		row := sidebarRows[i]
		switch row.kind {
		case artifactSidebarRowHeader:
			lines = append(lines, tuiTheme.Artifact.GroupHeader.Render(row.text))
		case artifactSidebarRowItem:
			if row.itemIndex == m.artifactIndex {
				lines = append(lines, tuiTheme.Artifact.FileActive.Render("> "+row.text))
				continue
			}
			lines = append(lines, tuiTheme.Artifact.FileInactive.Render("  "+row.text))
		}
	}
	if hidden := artifactSidebarHiddenFileCount(sidebarRows[end:]); hidden > 0 {
		lines = append(lines, tuiTheme.Artifact.Hint.Render(fmt.Sprintf("… %d more file(s)", hidden)))
	}
	if len(lines) == 0 {
		return []string{tuiTheme.Artifact.Empty.Render("No artifacts yet.")}
	}
	return lines
}

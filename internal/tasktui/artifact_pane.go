package tasktui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

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
	fileLines := m.renderArtifactFileLines(max(12, sidebarWidth-4), min(len(m.artifactItems), max(1, height-2)))
	filesContent := m.renderArtifactFilesColumn(sidebarWidth, height, fileLines)

	// Render preview pane
	previewContent := m.renderArtifactPreviewColumn(previewWidth, height)

	// Vertical divider
	divider := strings.Repeat(tuiTheme.divider.Render("│")+"\n", max(1, height))
	divider = lipgloss.Place(1, height, lipgloss.Left, lipgloss.Top, divider)

	return lipgloss.JoinHorizontal(lipgloss.Top, filesContent, divider, previewContent)
}

func (m Model) renderArtifactFilesColumn(width, height int, lines []string) string {
	innerWidth := max(10, width-2)
	titleText := fmt.Sprintf("Files (%d)", len(m.artifactItems))
	title := tuiTheme.Artifact.BlockTitle.Render(titleText)

	bodyHeight := max(1, height-1)
	body := lipgloss.Place(innerWidth, bodyHeight, lipgloss.Left, lipgloss.Top, strings.Join(lines, "\n"))

	content := lipgloss.JoinVertical(lipgloss.Left, title, body)

	style := lipgloss.NewStyle().Width(width).Height(height).PaddingLeft(1)
	if m.focusRegion == FocusRegionArtifactFiles {
		style = style.BorderLeft(true).
			BorderStyle(lipgloss.Border{Left: "│"}).
			BorderForeground(tuiTheme.halfMuted).
			Width(width)
	}
	return style.Render(content)
}

func (m Model) renderArtifactPreviewColumn(width, height int) string {
	title := "Preview"
	if len(m.artifactItems) > 0 && m.artifactIndex < len(m.artifactItems) {
		title = fmt.Sprintf("Preview · %s", m.artifactItems[m.artifactIndex].PreviewTitle)
	}
	header := tuiTheme.Artifact.BlockTitle.Render(title)

	contentHeight := max(1, height-1)
	innerWidth := max(10, width-2)
	bodyContent := lipgloss.Place(innerWidth, contentHeight, lipgloss.Left, lipgloss.Top, m.artifactPreview.View())

	content := lipgloss.JoinVertical(lipgloss.Left, header, bodyContent)

	style := lipgloss.NewStyle().Width(width).Height(height).PaddingLeft(1)
	if m.focusRegion == FocusRegionArtifactPreview {
		style = style.BorderLeft(true).
			BorderStyle(lipgloss.Border{Left: "│"}).
			BorderForeground(tuiTheme.halfMuted).
			Width(width)
	}
	return style.Render(content)
}

func artifactPaneLayout(bodyHeight int, fileLineCount int) (fileBlockHeight, previewBlockHeight int) {
	available := max(0, bodyHeight-1)
	if available == 0 {
		return 0, 0
	}
	if available < 7 {
		if available == 1 {
			return 1, 0
		}
		fileBlockHeight = max(1, available/2)
		previewBlockHeight = max(1, available-fileBlockHeight)
		return
	}
	maxFileHeight := min(6, available-4)
	fileBlockHeight = clamp(fileLineCount+1, 3, maxFileHeight)
	previewBlockHeight = max(4, available-fileBlockHeight)
	return
}

func artifactVisibleCapacity(total int) int {
	if total <= 0 {
		return 1
	}
	return min(total, 3)
}

func (m Model) renderArtifactFileLines(width, rows int) []string {
	if len(m.artifactItems) == 0 {
		return []string{tuiTheme.Artifact.Empty.Render("No artifacts yet.")}
	}
	start, end := selectionWindow(len(m.artifactItems), m.artifactIndex, rows)
	lines := make([]string, 0, rows+2)
	if start > 0 {
		lines = append(lines, tuiTheme.Artifact.Hint.Render(fmt.Sprintf("… %d earlier file(s)", start)))
	}
	for i := start; i < end; i++ {
		label := ansi.Truncate(m.artifactItems[i].Label, max(10, width-2), "…")
		if i == m.artifactIndex {
			lines = append(lines, tuiTheme.Artifact.FileActive.Render("> "+label))
			continue
		}
		lines = append(lines, tuiTheme.Artifact.FileInactive.Render("  "+label))
	}
	if end < len(m.artifactItems) {
		lines = append(lines, tuiTheme.Artifact.Hint.Render(fmt.Sprintf("… %d more file(s)", len(m.artifactItems)-end)))
	}
	return lines
}

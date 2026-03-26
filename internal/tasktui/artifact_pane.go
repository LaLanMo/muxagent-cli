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

func (m Model) renderArtifactsPane(surface artifactSurface) string {
	width := surface.Rect.Width
	height := surface.Rect.Height
	contentWidth := max(18, width-2)
	fileLines := m.renderArtifactFileLines(max(18, contentWidth-4), artifactVisibleCapacity(len(m.artifactItems)))
	fileBlockHeight, previewBlockHeight := artifactPaneLayout(height, len(fileLines))
	header := joinHorizontal(
		tuiTheme.Artifact.Header.Render(fmt.Sprintf("Artifacts (%d)", len(m.artifactItems))),
		tuiTheme.Artifact.Hint.Render("Tab next pane"),
		contentWidth,
	)
	files := m.renderArtifactFilesBlock(contentWidth, fileBlockHeight, fileLines)
	preview := m.renderArtifactPreviewBlock(contentWidth, previewBlockHeight)
	content := lipgloss.JoinVertical(lipgloss.Left, header, files, preview)
	inner := lipgloss.Place(contentWidth, max(1, height), lipgloss.Left, lipgloss.Top, content)
	return tuiTheme.Artifact.Pane.Width(width).Height(height).Render(inner)
}

func artifactLauncherSurfaceHeight(topBodyHeight int) int {
	switch {
	case topBodyHeight >= 12:
		return 5
	case topBodyHeight >= 10:
		return 4
	case topBodyHeight >= 8:
		return 3
	default:
		return 0
	}
}

func (m Model) renderArtifactLauncher(surface surfaceRect) string {
	width := surface.Width
	height := max(1, surface.Height)
	title := joinHorizontal(
		tuiTheme.Artifact.Header.Render(fmt.Sprintf("Artifacts (%d)", len(m.artifactItems))),
		tuiTheme.Artifact.Hint.Render("Enter open"),
		width,
	)
	body := tuiTheme.Text.Muted.Render("Inspect files and preview in a dedicated artifact view.")
	if len(m.artifactItems) > 0 && m.artifactIndex < len(m.artifactItems) {
		current := ansi.Truncate(m.artifactItems[m.artifactIndex].Label, max(12, width-2), "…")
		body = tuiTheme.Text.Muted.Render("Current: " + current)
	}
	hint := renderFooterHintText("Tab artifacts  Enter open  Esc detail")
	if m.focusRegion == FocusRegionArtifactLauncher {
		hint = renderFooterHintText("Enter open  Tab next focus  Esc detail")
	}
	lines := []string{title, body, hint}
	if height >= 5 {
		lines = []string{title, "", body, "", hint}
	} else if height == 4 {
		lines = []string{title, "", body, hint}
	}
	content := lipgloss.JoinVertical(lipgloss.Left, lines...)
	return tuiTheme.Artifact.Block.Width(width).Height(height).Render(content)
}

func (m Model) renderDetailWithArtifactLauncher(timeline, launcher surfaceRect) string {
	detail := lipgloss.Place(timeline.Width, timeline.Height, lipgloss.Left, lipgloss.Top, m.detailViewport.View())
	if launcher.Height <= 0 {
		return detail
	}
	launcherView := m.renderArtifactLauncher(launcher)
	return lipgloss.JoinVertical(lipgloss.Left, detail, "", launcherView)
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

func (m Model) renderArtifactFilesBlock(width, height int, lines []string) string {
	titleText := "Files"
	hintText := "Tab focus"
	if m.focusRegion == FocusRegionArtifactFiles {
		titleText = "Files · focused"
		hintText = "↑↓ browse"
	}
	title := joinHorizontal(
		tuiTheme.Artifact.BlockTitle.Render(titleText),
		tuiTheme.Artifact.Hint.Render(hintText),
		width,
	)
	body := lipgloss.Place(width-2, max(1, height-1), lipgloss.Left, lipgloss.Top, strings.Join(lines, "\n"))
	return tuiTheme.Artifact.Block.Width(width).Height(height).Render(lipgloss.JoinVertical(lipgloss.Left, title, body))
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

func (m Model) renderArtifactPreviewBlock(width, height int) string {
	title := "Preview"
	if len(m.artifactItems) > 0 && m.artifactIndex < len(m.artifactItems) {
		title = fmt.Sprintf("Preview · %s", m.artifactItems[m.artifactIndex].PreviewTitle)
	}
	hintText := "Tab focus"
	if m.focusRegion == FocusRegionArtifactPreview {
		hintText = "↑↓ scroll"
	}
	header := joinHorizontal(
		tuiTheme.Artifact.BlockTitle.Render(title),
		tuiTheme.Artifact.Hint.Render(hintText),
		width,
	)
	contentHeight := max(1, height-2)
	innerWidth := max(10, width-4)
	bodyContent := lipgloss.Place(innerWidth, contentHeight, lipgloss.Left, lipgloss.Top, m.artifactPreview.View())
	body := lipgloss.NewStyle().Width(width - 2).PaddingLeft(1).Render(bodyContent)
	return tuiTheme.Artifact.Block.Width(width).Height(height).Render(lipgloss.JoinVertical(lipgloss.Left, header, body))
}

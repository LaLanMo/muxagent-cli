package tasktui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func (m *Model) syncArtifactPreview(paneWidth, bodyHeight int, collapsed bool) {
	fileLines := m.renderArtifactFileLines(max(18, paneWidth-6), artifactVisibleCapacity(len(m.artifactItems)))
	_, previewBlockHeight := artifactPaneLayout(bodyHeight, collapsed, len(fileLines))
	contentWidth := max(12, paneWidth-4)
	previewHeight := max(3, previewBlockHeight-2)
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
	collapsed := surface.Collapsed
	if collapsed {
		return m.renderCollapsedArtifactRail(width, height)
	}
	contentWidth := max(18, width-2)
	fileLines := m.renderArtifactFileLines(max(18, contentWidth-4), artifactVisibleCapacity(len(m.artifactItems)))
	fileBlockHeight, previewBlockHeight := artifactPaneLayout(height, false, len(fileLines))
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

func artifactLauncherSurfaceHeight(surface surfaceRect) int {
	return 5
}

func (m Model) renderArtifactLauncher(surface surfaceRect) string {
	width := surface.Width
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
	content := lipgloss.JoinVertical(lipgloss.Left, title, "", body, "", hint)
	return tuiTheme.Artifact.Block.Width(width).Render(content)
}

func (m Model) renderDetailWithArtifactLauncher(timeline, launcher surfaceRect) string {
	launcherView := m.renderArtifactLauncher(launcher)
	detail := lipgloss.Place(timeline.Width, timeline.Height, lipgloss.Left, lipgloss.Top, m.detailViewport.View())
	return lipgloss.JoinVertical(lipgloss.Left, detail, "", launcherView)
}

func artifactPaneLayout(bodyHeight int, collapsed bool, fileLineCount int) (fileBlockHeight, previewBlockHeight int) {
	if collapsed {
		return 0, 0
	}
	innerHeight := max(10, bodyHeight)
	fileBlockHeight = clamp(fileLineCount+1, 3, 6)
	previewBlockHeight = max(8, innerHeight-fileBlockHeight-1)
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
	contentHeight := max(3, height-2)
	body := lipgloss.Place(width-2, contentHeight, lipgloss.Left, lipgloss.Top, m.artifactPreview.View())
	return tuiTheme.Artifact.Block.Width(width).Height(height).Render(lipgloss.JoinVertical(lipgloss.Left, header, body))
}

func (m Model) renderCollapsedArtifactRail(width, height int) string {
	top := lipgloss.JoinVertical(
		lipgloss.Center,
		tuiTheme.Artifact.RailBadge.Render(fmt.Sprintf("%d", len(m.artifactItems))),
		"",
		tuiTheme.Artifact.RailDots.Render("·\n·\n·"),
	)
	bottom := lipgloss.JoinVertical(
		lipgloss.Center,
		tuiTheme.Artifact.RailHint.Render("◀"),
		tuiTheme.Artifact.RailHint.Render("Tab"),
	)
	gapHeight := max(1, height-lipgloss.Height(top)-lipgloss.Height(bottom)-2)
	content := lipgloss.JoinVertical(
		lipgloss.Center,
		top,
		strings.Repeat("\n", gapHeight),
		bottom,
	)
	return tuiTheme.Artifact.Rail.Width(width).Height(height).Render(content)
}

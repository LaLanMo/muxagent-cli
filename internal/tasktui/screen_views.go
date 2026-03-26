package tasktui

import (
	"charm.land/lipgloss/v2"
)

func (m Model) renderScreen() string {
	base := m.renderBaseScreen()
	return m.renderDialogOverlay(base)
}

func (m Model) renderBaseScreen() string {
	width, height := m.viewportSize()
	switch m.screen {
	case ScreenNewTask:
		return m.renderNewTaskScreen(width, height)
	case ScreenRunning, ScreenApproval, ScreenClarification, ScreenFailed, ScreenComplete:
		return m.renderDetailScreen(width, height)
	default:
		return m.renderTaskListScreen(width, height)
	}
}

func (m Model) viewportSize() (int, int) {
	width := m.width
	height := m.height
	if width <= 0 {
		width = 100
	}
	if height <= 0 {
		height = 30
	}
	return width, height
}

func (m Model) renderTaskListScreen(width, height int) string {
	metrics := m.computeScreenMetrics()
	header := m.renderTaskListHeader(metrics.innerWidth)
	footer := m.renderTaskListFooter(surfaceRect{Width: metrics.innerWidth})
	layout := m.computeTaskListScreenLayout(header, footer)
	bodySurface := m.computeTaskListBodySurface(layout)
	body := lipgloss.Place(bodySurface.Width, bodySurface.Height, lipgloss.Left, lipgloss.Top, m.taskList.View())
	return renderCanvasLayout(layout.screenMetrics, layout.bodyHeight, header, body, footer)
}

func (m Model) renderNewTaskScreen(width, height int) string {
	metrics := m.computeScreenMetrics()
	header := m.renderAppHeader(metrics.innerWidth)
	footer := renderFooterHintBar(metrics.innerWidth, m.newTaskModalHint())
	layout := m.computeNewTaskScreenLayout(header, footer)
	body := lipgloss.Place(layout.innerWidth, layout.bodyHeight, lipgloss.Center, lipgloss.Center, m.renderNewTaskModal(layout))
	return renderCanvasLayout(layout.screenMetrics, layout.bodyHeight, header, body, footer)
}

func (m Model) renderDetailScreen(width, height int) string {
	snapshot := m.computeDetailLayoutSnapshot()
	frame := snapshot.Frame
	surfaces := snapshot.Surfaces
	header := snapshot.Header
	footer := snapshot.Footer
	panel := snapshot.PanelView.View

	var bodyContent string
	switch m.activeDetailTab {
	case DetailTabArtifacts:
		if len(m.artifactItems) > 0 {
			bodyContent = lipgloss.Place(frame.contentWidth, surfaces.Body.topBodyHeight, lipgloss.Left, lipgloss.Top, m.renderArtifactsPane(surfaces.Artifact))
		} else {
			bodyContent = lipgloss.Place(frame.contentWidth, surfaces.Body.topBodyHeight, lipgloss.Left, lipgloss.Top, m.detailViewport.View())
		}
	default:
		bodyContent = lipgloss.Place(frame.contentWidth, surfaces.Body.topBodyHeight, lipgloss.Left, lipgloss.Top, m.detailViewport.View())
	}

	if panel != "" {
		bodyContent = lipgloss.JoinVertical(lipgloss.Left, bodyContent, "", panel)
	}
	centeredHeader := lipgloss.Place(frame.innerWidth, frame.headerHeight, lipgloss.Center, lipgloss.Top, header)
	centeredBody := lipgloss.Place(frame.innerWidth, frame.bodyHeight, lipgloss.Center, lipgloss.Top, bodyContent)
	centeredFooter := lipgloss.Place(frame.innerWidth, frame.footerHeight, lipgloss.Center, lipgloss.Top, footer)
	page := lipgloss.JoinVertical(lipgloss.Left, centeredHeader, centeredBody, centeredFooter)
	return tuiTheme.canvas.Width(frame.viewportWidth).Height(frame.viewportHeight).Render(page)
}

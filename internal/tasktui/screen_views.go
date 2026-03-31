package tasktui

import (
	"strings"

	"charm.land/lipgloss/v2"
)

func (m Model) renderScreen() string {
	base := m.renderBaseScreen()
	return m.renderDialogOverlay(base)
}

func (m Model) renderBaseScreen() string {
	width, height := m.viewportSize()
	switch m.screen {
	case ScreenTaskConfigs:
		return m.renderTaskConfigListScreen(width, height)
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

func (m Model) renderTaskConfigListScreen(width, height int) string {
	metrics := m.computeScreenMetrics()
	header := m.renderTaskConfigListHeader(metrics.innerWidth)
	footer := m.renderTaskConfigListFooter(surfaceRect{Width: metrics.innerWidth})
	layout := m.computeTaskListScreenLayout(header, footer)
	bodySurface := m.computeTaskListBodySurface(layout)
	body := lipgloss.Place(bodySurface.Width, bodySurface.Height, lipgloss.Left, lipgloss.Top, m.configList.View())
	page := renderCanvasLayout(layout.screenMetrics, layout.bodyHeight, header, body, footer)
	if overlay := m.renderTaskConfigOverlay(metrics.innerWidth, metrics.innerHeight, page); overlay != "" {
		return overlay
	}
	return page
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
		if surfaces.TimelineSplit {
			timeline := lipgloss.Place(surfaces.Timeline.Width, surfaces.Timeline.Height, lipgloss.Left, lipgloss.Top, m.detailViewport.View())
			divider := strings.Repeat(m.artifactPaneLineStyle(m.focusRegion == FocusRegionDetail).Render("│")+"\n", max(1, surfaces.Body.topBodyHeight))
			divider = lipgloss.Place(1, surfaces.Body.topBodyHeight, lipgloss.Left, lipgloss.Top, divider)
			output := m.renderLiveOutputPane(surfaces.LiveOutputPane)
			bodyContent = lipgloss.Place(frame.contentWidth, surfaces.Body.topBodyHeight, lipgloss.Left, lipgloss.Top, lipgloss.JoinHorizontal(lipgloss.Top, timeline, divider, output))
		} else {
			bodyContent = lipgloss.Place(frame.contentWidth, surfaces.Body.topBodyHeight, lipgloss.Left, lipgloss.Top, m.detailViewport.View())
		}
	}

	if panel != "" {
		bodyContent = lipgloss.JoinVertical(lipgloss.Left, bodyContent, "", panel)
	}
	centeredHeader := lipgloss.Place(frame.innerWidth, frame.headerHeight, lipgloss.Center, lipgloss.Top, header)
	centeredBody := lipgloss.Place(frame.innerWidth, frame.bodyHeight, lipgloss.Center, lipgloss.Top, bodyContent)
	centeredFooter := lipgloss.Place(frame.innerWidth, frame.footerHeight, lipgloss.Center, lipgloss.Top, footer)
	page := lipgloss.JoinVertical(lipgloss.Left, centeredHeader, centeredBody, centeredFooter)
	return tuiTheme.App.Canvas.Width(frame.viewportWidth).Height(frame.viewportHeight).Render(page)
}

package tasktui

import "charm.land/lipgloss/v2"

type screenMetrics struct {
	viewportWidth  int
	viewportHeight int
	innerWidth     int
	innerHeight    int
}

type taskListScreenLayout struct {
	screenMetrics
	headerHeight int
	footerHeight int
	bodyHeight   int
}

type newTaskScreenLayout struct {
	screenMetrics
	headerHeight    int
	footerHeight    int
	bodyHeight      int
	modalWidth      int
	modalInnerWidth int
	editorRows      int
}

type detailFrameLayout struct {
	screenMetrics
	contentWidth int
	layoutMode   artifactLayoutMode
	headerHeight int
	footerHeight int
	bodyHeight   int
}

type detailBodyLayout struct {
	frame          detailFrameLayout
	panelHeight    int
	topBodyHeight  int
	detailWidth    int
	detailHeight   int
	artifactWidth  int
	previewWidth   int
	gap            int
	launcherHeight int
}

func (m Model) computeScreenMetrics() screenMetrics {
	width, height := m.viewportSize()
	innerWidth, innerHeight := innerSize(width, height)
	return screenMetrics{
		viewportWidth:  width,
		viewportHeight: height,
		innerWidth:     innerWidth,
		innerHeight:    innerHeight,
	}
}

func (m Model) computeTaskListScreenLayout(header, footer string) taskListScreenLayout {
	metrics := m.computeScreenMetrics()
	headerHeight := lipgloss.Height(header)
	footerHeight := lipgloss.Height(footer)
	return taskListScreenLayout{
		screenMetrics: metrics,
		headerHeight:  headerHeight,
		footerHeight:  footerHeight,
		bodyHeight:    max(1, metrics.innerHeight-headerHeight-footerHeight),
	}
}

func (m Model) computeNewTaskScreenLayout(header, footer string) newTaskScreenLayout {
	metrics := m.computeScreenMetrics()
	headerHeight := lipgloss.Height(header)
	footerHeight := lipgloss.Height(footer)
	modalWidth := boundedPreferredWidth(metrics.innerWidth, metrics.innerWidth-8, 24, 64)
	return newTaskScreenLayout{
		screenMetrics:   metrics,
		headerHeight:    headerHeight,
		footerHeight:    footerHeight,
		bodyHeight:      max(1, metrics.innerHeight-headerHeight-footerHeight),
		modalWidth:      modalWidth,
		modalInnerWidth: max(1, modalWidth-tuiTheme.modal.GetHorizontalPadding()),
		editorRows:      clamp(max(4, (max(1, metrics.innerHeight-headerHeight-footerHeight))/3), 4, 8),
	}
}

func (m Model) computeDetailFrameLayout(contentWidth int, header, footer string) detailFrameLayout {
	metrics := m.computeScreenMetrics()
	if contentWidth <= 0 {
		contentWidth = detailContentWidth(metrics.innerWidth)
	}
	headerHeight := lipgloss.Height(header)
	footerHeight := lipgloss.Height(footer)
	return detailFrameLayout{
		screenMetrics: metrics,
		contentWidth:  contentWidth,
		layoutMode:    m.currentArtifactLayoutMode(),
		headerHeight:  headerHeight,
		footerHeight:  footerHeight,
		bodyHeight:    max(1, metrics.innerHeight-headerHeight-footerHeight),
	}
}

func (m Model) computeDetailBodyLayout(frame detailFrameLayout, panel string) detailBodyLayout {
	panelHeight := 0
	topBodyHeight := frame.bodyHeight
	if panel != "" {
		panelHeight = lipgloss.Height(panel)
		topBodyHeight = max(1, frame.bodyHeight-panelHeight-1)
	}

	layout := detailBodyLayout{
		frame:         frame,
		panelHeight:   panelHeight,
		topBodyHeight: topBodyHeight,
		detailWidth:   frame.contentWidth,
		detailHeight:  topBodyHeight,
		previewWidth:  frame.contentWidth,
	}

	switch {
	case m.artifactDrillInVisible():
		return layout
	case frame.layoutMode == artifactLayoutSplit:
		leftWidth, rightWidth, gap := detailPaneWidths(frame.contentWidth)
		layout.detailWidth = leftWidth
		layout.artifactWidth = rightWidth
		layout.previewWidth = rightWidth
		layout.gap = gap
	case frame.layoutMode == artifactLayoutLauncher && !m.artifactDrillIn:
		layout.launcherHeight = artifactLauncherSurfaceHeight(surfaceRect{Width: frame.contentWidth})
		layout.detailHeight = max(1, topBodyHeight-layout.launcherHeight-1)
	}

	return layout
}

func renderCanvasLayout(metrics screenMetrics, bodyHeight int, header, body, footer string) string {
	body = lipgloss.Place(metrics.innerWidth, bodyHeight, lipgloss.Left, lipgloss.Top, body)
	page := lipgloss.JoinVertical(lipgloss.Left, header, body, footer)
	return tuiTheme.canvas.Width(metrics.viewportWidth).Height(metrics.viewportHeight).Render(page)
}

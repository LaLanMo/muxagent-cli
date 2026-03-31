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
	headerHeight int
	footerHeight int
	bodyHeight   int
}

type detailBodyLayout struct {
	frame         detailFrameLayout
	panelHeight   int
	topBodyHeight int
	detailWidth   int
	detailHeight  int
	previewWidth  int
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
		modalInnerWidth: max(1, modalWidth-tuiTheme.Modal.Frame.GetHorizontalPadding()),
		editorRows:      clamp(max(4, (max(1, metrics.innerHeight-headerHeight-footerHeight))/3), 4, 8),
	}
}

func newDetailFrameLayout(metrics screenMetrics, contentWidth, headerHeight, footerHeight int) detailFrameLayout {
	return detailFrameLayout{
		screenMetrics: metrics,
		contentWidth:  contentWidth,
		headerHeight:  headerHeight,
		footerHeight:  footerHeight,
		bodyHeight:    max(1, metrics.innerHeight-headerHeight-footerHeight),
	}
}

func (m Model) computeDetailBodyLayout(frame detailFrameLayout, panel string) detailBodyLayout {
	return m.computeDetailBodyLayoutWithPanelHeight(frame, lipgloss.Height(panel))
}

func (m Model) computeDetailBodyLayoutWithPanelHeight(frame detailFrameLayout, panelHeight int) detailBodyLayout {
	topBodyHeight := frame.bodyHeight
	if panelHeight > 0 {
		topBodyHeight = max(1, frame.bodyHeight-panelHeight-1)
	}
	return detailBodyLayout{
		frame:         frame,
		panelHeight:   panelHeight,
		topBodyHeight: topBodyHeight,
		detailWidth:   frame.contentWidth,
		detailHeight:  topBodyHeight,
		previewWidth:  frame.contentWidth,
	}
}

func (m Model) computeDetailLayoutSnapshot() detailLayoutSnapshot {
	metrics := m.computeScreenMetrics()
	contentWidth := detailContentWidth(metrics.innerWidth, m.activeDetailTab)
	header := m.renderDetailHeader(contentWidth)
	footerHeight := m.detailFooterReservedHeight()

	frame := newDetailFrameLayout(metrics, contentWidth, lipgloss.Height(header), footerHeight)
	panelSurface := m.computeDetailPanelSurface(frame)
	editorSpec := m.detailEditorSurfaceSpec(panelSurface)
	panel := m.buildDetailPanelForSurface(panelSurface, editorSpec)
	panelSurface.Rect.Height = lipgloss.Height(panel.View)
	body := m.computeDetailBodyLayoutWithPanelHeight(frame, panelSurface.Rect.Height)
	surfaces := m.computeDetailScreenSurfacesWithPanel(frame, body, panelSurface)

	snapshot := detailLayoutSnapshot{
		Header:       header,
		Frame:        frame,
		Body:         body,
		Surfaces:     surfaces,
		PanelView:    panel,
		Editor:       editorSpec,
		ContentWidth: contentWidth,
	}
	snapshot.Footer = m.renderDetailFooter(surfaceRect{Width: contentWidth})
	return snapshot
}

func renderCanvasLayout(metrics screenMetrics, bodyHeight int, header, body, footer string) string {
	body = lipgloss.Place(metrics.innerWidth, bodyHeight, lipgloss.Left, lipgloss.Top, body)
	page := lipgloss.JoinVertical(lipgloss.Left, header, body, footer)
	return tuiTheme.App.Canvas.Width(metrics.viewportWidth).Height(metrics.viewportHeight).Render(page)
}

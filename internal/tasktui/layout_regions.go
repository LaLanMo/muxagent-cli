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

func detailMinTimelineHeight(mode artifactLayoutMode) int {
	switch mode {
	case artifactLayoutSplit:
		return 6
	case artifactLayoutLauncher:
		return 5
	default:
		return 4
	}
}

func detailMinTopBodyHeight(mode artifactLayoutMode, artifactDrillIn bool) int {
	switch {
	case artifactDrillInVisibleForLayout(mode, artifactDrillIn):
		return 8
	case mode == artifactLayoutSplit:
		return 8
	case mode == artifactLayoutLauncher:
		return 9
	default:
		return 4
	}
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
	mode := artifactLayoutHidden
	if len(m.artifactItems) > 0 {
		mode = m.preferredDetailArtifactLayoutMode(metrics)
	}
	return detailFrameLayout{
		screenMetrics: metrics,
		contentWidth:  contentWidth,
		layoutMode:    mode,
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

	layout := detailBodyLayout{
		frame:         frame,
		panelHeight:   panelHeight,
		topBodyHeight: topBodyHeight,
		detailWidth:   frame.contentWidth,
		detailHeight:  topBodyHeight,
		previewWidth:  frame.contentWidth,
	}

	switch {
	case artifactDrillInVisibleForLayout(frame.layoutMode, m.artifactDrillIn):
		return layout
	case frame.layoutMode == artifactLayoutSplit:
		leftWidth, rightWidth, gap := detailPaneWidths(frame.contentWidth)
		layout.detailWidth = leftWidth
		layout.artifactWidth = rightWidth
		layout.previewWidth = rightWidth
		layout.gap = gap
	case frame.layoutMode == artifactLayoutLauncher && !m.artifactDrillIn:
		layout.launcherHeight = artifactLauncherSurfaceHeight(topBodyHeight)
		if layout.launcherHeight > 0 {
			layout.detailHeight = max(1, topBodyHeight-layout.launcherHeight-1)
		}
	}

	return layout
}

func (m Model) detailLayoutCandidateModes() []artifactLayoutMode {
	if !m.isDetailScreen() || len(m.artifactItems) == 0 {
		return []artifactLayoutMode{artifactLayoutHidden}
	}
	preferred := m.preferredDetailArtifactLayoutMode(m.computeScreenMetrics())
	if m.artifactDrillIn {
		return []artifactLayoutMode{artifactLayoutLauncher, artifactLayoutHidden}
	}
	if preferred == artifactLayoutSplit {
		return []artifactLayoutMode{artifactLayoutSplit, artifactLayoutLauncher, artifactLayoutHidden}
	}
	return []artifactLayoutMode{artifactLayoutLauncher, artifactLayoutHidden}
}

func (m Model) preferredDetailArtifactLayoutMode(metrics screenMetrics) artifactLayoutMode {
	if !m.isDetailScreen() || len(m.artifactItems) == 0 {
		return artifactLayoutHidden
	}
	if metrics.innerWidth >= 110 && metrics.innerHeight >= 26 {
		return artifactLayoutSplit
	}
	return artifactLayoutLauncher
}

func newDetailFrameLayout(metrics screenMetrics, contentWidth, headerHeight, footerHeight int, mode artifactLayoutMode) detailFrameLayout {
	return detailFrameLayout{
		screenMetrics: metrics,
		contentWidth:  contentWidth,
		layoutMode:    mode,
		headerHeight:  headerHeight,
		footerHeight:  footerHeight,
		bodyHeight:    max(1, metrics.innerHeight-headerHeight-footerHeight),
	}
}

func (m Model) buildDetailLayoutSnapshotForMode(metrics screenMetrics, contentWidth int, header string, footerHeight int, mode artifactLayoutMode) (detailLayoutSnapshot, bool) {
	frame := newDetailFrameLayout(metrics, contentWidth, lipgloss.Height(header), footerHeight, mode)
	panelSurface := m.computeDetailPanelSurfaceForMode(frame, mode)
	editorSpec := m.detailEditorSurfaceSpec(panelSurface)
	panel := m.buildDetailPanelForSurface(panelSurface, editorSpec)
	panelSurface.Rect.Height = lipgloss.Height(panel.View)
	body := m.computeDetailBodyLayoutWithPanelHeight(frame, panelSurface.Rect.Height)
	if !m.detailLayoutFits(frame, body) {
		return detailLayoutSnapshot{}, false
	}
	surfaces := m.computeDetailScreenSurfacesWithPanel(frame, body, panelSurface)
	return detailLayoutSnapshot{
		Header:       header,
		Frame:        frame,
		Body:         body,
		Surfaces:     surfaces,
		PanelView:    panel,
		Editor:       editorSpec,
		ContentWidth: contentWidth,
	}, true
}

func (m Model) computeDetailLayoutSnapshot() detailLayoutSnapshot {
	metrics := m.computeScreenMetrics()
	contentWidth := detailContentWidth(metrics.innerWidth)
	header := m.renderDetailHeader(contentWidth)
	footerHeight := m.detailFooterReservedHeight()

	for _, mode := range m.detailLayoutCandidateModes() {
		snapshot, ok := m.buildDetailLayoutSnapshotForMode(metrics, contentWidth, header, footerHeight, mode)
		if !ok {
			continue
		}
		snapshot.Footer = m.renderDetailFooterForLayout(surfaceRect{Width: snapshot.Frame.contentWidth}, snapshot.Frame.layoutMode)
		return snapshot
	}

	snapshot, _ := m.buildDetailLayoutSnapshotForMode(metrics, contentWidth, header, footerHeight, artifactLayoutHidden)
	snapshot.Footer = m.renderDetailFooterForLayout(surfaceRect{Width: snapshot.Frame.contentWidth}, snapshot.Frame.layoutMode)
	return snapshot
}

func (m Model) detailLayoutFits(frame detailFrameLayout, body detailBodyLayout) bool {
	switch {
	case artifactDrillInVisibleForLayout(frame.layoutMode, m.artifactDrillIn):
		return body.topBodyHeight >= 8
	case frame.layoutMode == artifactLayoutSplit:
		return body.topBodyHeight >= 8 && body.detailHeight >= detailMinTimelineHeight(frame.layoutMode) && body.artifactWidth > 0
	case frame.layoutMode == artifactLayoutLauncher:
		return body.launcherHeight > 0 && body.detailHeight >= detailMinTimelineHeight(frame.layoutMode)
	default:
		return body.detailHeight >= detailMinTimelineHeight(frame.layoutMode)
	}
}

func renderCanvasLayout(metrics screenMetrics, bodyHeight int, header, body, footer string) string {
	body = lipgloss.Place(metrics.innerWidth, bodyHeight, lipgloss.Left, lipgloss.Top, body)
	page := lipgloss.JoinVertical(lipgloss.Left, header, body, footer)
	return tuiTheme.canvas.Width(metrics.viewportWidth).Height(metrics.viewportHeight).Render(page)
}

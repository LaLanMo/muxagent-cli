package tasktui

import "charm.land/lipgloss/v2"

type surfaceRect struct {
	Width  int
	Height int
}

type panelSurface struct {
	Rect      surfaceRect
	MaxHeight int
}

type artifactSurface struct {
	Rect surfaceRect
}

type detailLayoutSnapshot struct {
	Header       string
	Footer       string
	Frame        detailFrameLayout
	Body         detailBodyLayout
	Surfaces     detailScreenSurfaces
	PanelView    builtPanel
	Editor       editorSurfaceSpec
	ContentWidth int
}

type detailScreenSurfaces struct {
	Frame    detailFrameLayout
	Body     detailBodyLayout
	Timeline surfaceRect
	Panel    panelSurface
	Artifact artifactSurface
	Preview  surfaceRect
	Footer   surfaceRect
}

func (m Model) computeTaskListBodySurface(layout taskListScreenLayout) surfaceRect {
	return surfaceRect{Width: layout.innerWidth, Height: layout.bodyHeight}
}

func (m Model) computeNewTaskModalSurface(layout newTaskScreenLayout) surfaceRect {
	return surfaceRect{Width: layout.modalWidth, Height: layout.bodyHeight}
}

func (m Model) computeDetailPanelSurface(frame detailFrameLayout) panelSurface {
	bodyHeight := frame.bodyHeight
	minTopBodyHeight := 4
	if minTopBodyHeight >= bodyHeight {
		minTopBodyHeight = max(1, bodyHeight-1)
	}
	preferred := bodyHeight - minTopBodyHeight - 1
	maxPanelHeight := max(1, bodyHeight-1)
	if preferred < 3 {
		preferred = min(3, maxPanelHeight)
	}
	return panelSurface{
		Rect:      surfaceRect{Width: frame.contentWidth},
		MaxHeight: clamp(preferred, 1, maxPanelHeight),
	}
}

func (m Model) computeDetailScreenSurfaces(frame detailFrameLayout, panel string) detailScreenSurfaces {
	panelSurface := m.computeDetailPanelSurface(frame)
	panelSurface.Rect.Height = 0
	if panel != "" {
		panelSurface.Rect.Height = lipgloss.Height(panel)
	}
	return m.computeDetailScreenSurfacesWithPanel(frame, m.computeDetailBodyLayout(frame, panel), panelSurface)
}

func (m Model) computeDetailScreenSurfacesWithPanel(frame detailFrameLayout, body detailBodyLayout, panel panelSurface) detailScreenSurfaces {
	surfaces := detailScreenSurfaces{
		Frame:    frame,
		Body:     body,
		Timeline: surfaceRect{Width: body.detailWidth, Height: body.detailHeight},
		Panel:    panel,
		Artifact: artifactSurface{
			Rect: surfaceRect{Width: frame.contentWidth, Height: body.topBodyHeight},
		},
		Preview: surfaceRect{Width: body.previewWidth, Height: 0},
		Footer:  surfaceRect{Width: frame.contentWidth, Height: frame.footerHeight},
	}
	if fileLines := m.renderArtifactFileLines(max(18, body.previewWidth-6), artifactVisibleCapacity(len(m.artifactItems))); body.previewWidth > 0 && body.topBodyHeight > 0 {
		_, previewBlockHeight := artifactPaneLayout(body.topBodyHeight, len(fileLines))
		surfaces.Preview = surfaceRect{
			Width:  max(12, body.previewWidth-6),
			Height: max(1, previewBlockHeight-2),
		}
	}
	return surfaces
}

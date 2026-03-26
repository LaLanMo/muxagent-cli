package tasktui

type surfaceRect struct {
	Width  int
	Height int
}

type panelSurface struct {
	Rect      surfaceRect
	MaxHeight int
}

type artifactSurface struct {
	Rect      surfaceRect
	Collapsed bool
}

type detailScreenSurfaces struct {
	Frame    detailFrameLayout
	Body     detailBodyLayout
	Timeline surfaceRect
	Panel    panelSurface
	Artifact artifactSurface
	Launcher surfaceRect
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
	minTopBodyHeight := 6
	if m.currentArtifactLayoutMode() != artifactLayoutHidden {
		minTopBodyHeight = 8
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
	body := m.computeDetailBodyLayout(frame, panel)
	surfaces := detailScreenSurfaces{
		Frame:    frame,
		Body:     body,
		Timeline: surfaceRect{Width: body.detailWidth, Height: body.detailHeight},
		Panel:    m.computeDetailPanelSurface(frame),
		Artifact: artifactSurface{
			Rect:      surfaceRect{Width: body.artifactWidth, Height: body.topBodyHeight},
			Collapsed: frame.layoutMode == artifactLayoutCollapsedRail,
		},
		Launcher: surfaceRect{Width: frame.contentWidth, Height: body.launcherHeight},
		Footer:   surfaceRect{Width: frame.contentWidth, Height: frame.footerHeight},
	}
	if m.artifactDrillInVisible() {
		surfaces.Artifact.Rect = surfaceRect{Width: frame.contentWidth, Height: body.topBodyHeight}
		surfaces.Artifact.Collapsed = false
	}
	return surfaces
}

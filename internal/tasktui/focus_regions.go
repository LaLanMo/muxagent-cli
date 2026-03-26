package tasktui

type FocusRegion int

const (
	FocusRegionNone FocusRegion = iota
	FocusRegionDetail
	FocusRegionArtifactLauncher
	FocusRegionArtifactFiles
	FocusRegionArtifactPreview
	FocusRegionChoices
	FocusRegionActionPanel
	FocusRegionComposer
)

func (m Model) isDetailScreen() bool {
	switch m.screen {
	case ScreenRunning, ScreenApproval, ScreenClarification, ScreenFailed, ScreenComplete:
		return true
	default:
		return false
	}
}

func (m Model) detailComposerVisible() bool {
	return m.currentEditorBindingSpec().Visible && m.screen != ScreenNewTask
}

func (m Model) composerRegionVisible() bool {
	return m.currentEditorBindingSpec().Visible
}

func (m Model) shouldFocusDetailComposer() bool {
	return m.focusRegion == FocusRegionComposer && m.detailComposerVisible()
}

func (m Model) artifactPaneExpandable() bool {
	if !m.isDetailScreen() || len(m.artifactItems) == 0 {
		return false
	}
	width, height := m.viewportSize()
	innerWidth, innerHeight := innerSize(width, height)
	return innerWidth >= 110 && innerHeight >= 26
}

func (m Model) currentArtifactLayoutMode() artifactLayoutMode {
	if !m.isDetailScreen() || len(m.artifactItems) == 0 {
		return artifactLayoutHidden
	}
	if m.artifactPaneExpandable() {
		return artifactLayoutSplit
	}
	return artifactLayoutLauncher
}

func (m Model) artifactPaneVisible() bool {
	switch m.currentArtifactLayoutMode() {
	case artifactLayoutSplit:
		return true
	case artifactLayoutLauncher:
		return m.artifactDrillIn
	default:
		return false
	}
}

func (m Model) artifactLauncherVisible() bool {
	return m.currentArtifactLayoutMode() == artifactLayoutLauncher && !m.artifactDrillIn
}

func (m Model) artifactDrillInVisible() bool {
	return m.currentArtifactLayoutMode() == artifactLayoutLauncher && m.artifactDrillIn
}

func (m Model) availableFocusRegions() []FocusRegion {
	if m.artifactDrillInVisible() {
		return []FocusRegion{FocusRegionArtifactFiles, FocusRegionArtifactPreview}
	}
	switch m.screen {
	case ScreenNewTask:
		return []FocusRegion{FocusRegionComposer}
	case ScreenApproval:
		regions := []FocusRegion{FocusRegionActionPanel}
		if m.composerRegionVisible() {
			regions = append(regions, FocusRegionComposer)
		}
		regions = append(regions, FocusRegionDetail)
		if m.artifactPaneVisible() {
			regions = append(regions, FocusRegionArtifactFiles, FocusRegionArtifactPreview)
		} else if m.artifactLauncherVisible() {
			regions = append(regions, FocusRegionArtifactLauncher)
		}
		return regions
	case ScreenClarification:
		regions := []FocusRegion{FocusRegionChoices, FocusRegionComposer, FocusRegionActionPanel, FocusRegionDetail}
		if m.artifactPaneVisible() {
			regions = append(regions, FocusRegionArtifactFiles, FocusRegionArtifactPreview)
		} else if m.artifactLauncherVisible() {
			regions = append(regions, FocusRegionArtifactLauncher)
		}
		return regions
	case ScreenFailed:
		regions := make([]FocusRegion, 0, 4)
		if len(m.availableFailureActions()) > 0 {
			regions = append(regions, FocusRegionActionPanel)
		}
		regions = append(regions, FocusRegionDetail)
		if m.artifactPaneVisible() {
			regions = append(regions, FocusRegionArtifactFiles, FocusRegionArtifactPreview)
		} else if m.artifactLauncherVisible() {
			regions = append(regions, FocusRegionArtifactLauncher)
		}
		return regions
	case ScreenRunning, ScreenComplete:
		regions := []FocusRegion{FocusRegionDetail}
		if m.artifactPaneVisible() {
			regions = append(regions, FocusRegionArtifactFiles, FocusRegionArtifactPreview)
		} else if m.artifactLauncherVisible() {
			regions = append(regions, FocusRegionArtifactLauncher)
		}
		return regions
	default:
		return nil
	}
}

func (m Model) defaultFocusRegion() FocusRegion {
	if m.artifactDrillInVisible() {
		return FocusRegionArtifactFiles
	}
	switch m.screen {
	case ScreenNewTask:
		return FocusRegionComposer
	case ScreenApproval:
		return FocusRegionActionPanel
	case ScreenClarification:
		return FocusRegionChoices
	case ScreenFailed:
		if len(m.availableFailureActions()) > 0 {
			return FocusRegionActionPanel
		}
		return FocusRegionDetail
	case ScreenRunning, ScreenComplete:
		return FocusRegionDetail
	default:
		return FocusRegionNone
	}
}

func focusRegionIndex(regions []FocusRegion, target FocusRegion) int {
	for i, region := range regions {
		if region == target {
			return i
		}
	}
	return -1
}

func (m *Model) normalizeFocusRegion() {
	regions := m.availableFocusRegions()
	if len(regions) == 0 {
		m.focusRegion = FocusRegionNone
		return
	}
	if focusRegionIndex(regions, m.focusRegion) >= 0 {
		return
	}
	m.focusRegion = m.defaultFocusRegion()
	if focusRegionIndex(regions, m.focusRegion) < 0 {
		m.focusRegion = regions[0]
	}
}

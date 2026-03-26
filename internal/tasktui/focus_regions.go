package tasktui

type FocusRegion int

const (
	FocusRegionNone FocusRegion = iota
	FocusRegionDetail
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

func (m Model) artifactTabActive() bool {
	return m.activeDetailTab == DetailTabArtifacts && len(m.artifactItems) > 0
}

func (m Model) availableFocusRegions() []FocusRegion {
	switch m.screen {
	case ScreenNewTask:
		return []FocusRegion{FocusRegionComposer}
	case ScreenApproval:
		regions := []FocusRegion{FocusRegionActionPanel}
		if m.composerRegionVisible() {
			regions = append(regions, FocusRegionComposer)
		}
		if m.artifactTabActive() {
			regions = append(regions, FocusRegionArtifactFiles, FocusRegionArtifactPreview)
		} else {
			regions = append(regions, FocusRegionDetail)
		}
		return regions
	case ScreenClarification:
		regions := []FocusRegion{FocusRegionChoices, FocusRegionComposer, FocusRegionActionPanel}
		if m.artifactTabActive() {
			regions = append(regions, FocusRegionArtifactFiles, FocusRegionArtifactPreview)
		} else {
			regions = append(regions, FocusRegionDetail)
		}
		return regions
	case ScreenFailed:
		regions := make([]FocusRegion, 0, 4)
		if len(m.availableFailureActions()) > 0 {
			regions = append(regions, FocusRegionActionPanel)
		}
		if m.artifactTabActive() {
			regions = append(regions, FocusRegionArtifactFiles, FocusRegionArtifactPreview)
		} else {
			regions = append(regions, FocusRegionDetail)
		}
		return regions
	case ScreenRunning, ScreenComplete:
		if m.artifactTabActive() {
			return []FocusRegion{FocusRegionArtifactFiles, FocusRegionArtifactPreview}
		}
		return []FocusRegion{FocusRegionDetail}
	default:
		return nil
	}
}

func (m Model) defaultFocusRegion() FocusRegion {
	if m.artifactTabActive() {
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

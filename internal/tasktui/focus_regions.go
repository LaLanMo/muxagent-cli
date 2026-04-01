package tasktui

type FocusRegion int

const (
	FocusRegionNone FocusRegion = iota
	FocusRegionDetail
	FocusRegionArtifactFiles
	FocusRegionArtifactPreview
	FocusRegionChoices
	FocusRegionActionPanel
	FocusRegionFormEditor
)

func (m Model) isDetailScreen() bool {
	switch m.screen {
	case ScreenRunning, ScreenApproval, ScreenClarification, ScreenFailed, ScreenComplete:
		return true
	default:
		return false
	}
}

func (m Model) artifactTabActive() bool {
	return m.activeDetailTab == DetailTabArtifacts && len(m.artifactItems) > 0
}

func (m Model) artifactTabFocusRegions() []FocusRegion {
	if !m.artifactTabActive() {
		return nil
	}
	switch m.screen {
	case ScreenApproval:
		return []FocusRegion{FocusRegionArtifactFiles, FocusRegionArtifactPreview, FocusRegionActionPanel}
	case ScreenClarification:
		return []FocusRegion{FocusRegionArtifactFiles, FocusRegionArtifactPreview, FocusRegionChoices}
	case ScreenComplete:
		if m.completeFollowUpVisible() {
			return []FocusRegion{FocusRegionArtifactFiles, FocusRegionArtifactPreview, FocusRegionActionPanel}
		}
		return []FocusRegion{FocusRegionArtifactFiles, FocusRegionArtifactPreview}
	default:
		return []FocusRegion{FocusRegionArtifactFiles, FocusRegionArtifactPreview}
	}
}

func (m Model) availableFocusRegions() []FocusRegion {
	if regions := m.artifactTabFocusRegions(); len(regions) > 0 {
		return regions
	}
	switch m.screen {
	case ScreenTaskConfigs:
		if m.taskConfigs.form != nil {
			return []FocusRegion{FocusRegionFormEditor}
		}
		return nil
	case ScreenNewTask:
		return []FocusRegion{FocusRegionFormEditor}
	case ScreenApproval:
		return []FocusRegion{FocusRegionActionPanel, FocusRegionDetail}
	case ScreenClarification:
		return []FocusRegion{FocusRegionChoices, FocusRegionDetail}
	case ScreenFailed:
		regions := make([]FocusRegion, 0, 4)
		if len(m.availableFailureActions()) > 0 {
			regions = append(regions, FocusRegionActionPanel)
		}
		regions = append(regions, FocusRegionDetail)
		return regions
	case ScreenComplete:
		if m.completeFollowUpVisible() {
			return []FocusRegion{FocusRegionDetail, FocusRegionActionPanel}
		}
		return []FocusRegion{FocusRegionDetail}
	case ScreenRunning:
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
	case ScreenTaskConfigs:
		if m.taskConfigs.form != nil {
			return FocusRegionFormEditor
		}
		return FocusRegionNone
	case ScreenNewTask:
		return FocusRegionFormEditor
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

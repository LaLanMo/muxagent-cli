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

func artifactPaneVisibleForLayout(mode artifactLayoutMode, artifactDrillIn bool) bool {
	switch mode {
	case artifactLayoutSplit:
		return true
	case artifactLayoutLauncher:
		return artifactDrillIn
	default:
		return false
	}
}

func artifactLauncherVisibleForLayout(mode artifactLayoutMode, artifactDrillIn bool) bool {
	return mode == artifactLayoutLauncher && !artifactDrillIn
}

func artifactDrillInVisibleForLayout(mode artifactLayoutMode, artifactDrillIn bool) bool {
	return mode == artifactLayoutLauncher && artifactDrillIn
}

func (m Model) artifactPaneExpandable() bool {
	return m.currentArtifactLayoutMode() == artifactLayoutSplit
}

func (m Model) currentArtifactLayoutMode() artifactLayoutMode {
	if !m.isDetailScreen() {
		return artifactLayoutHidden
	}
	return m.computeDetailLayoutSnapshot().Frame.layoutMode
}

func (m Model) artifactPaneVisible() bool {
	return artifactPaneVisibleForLayout(m.currentArtifactLayoutMode(), m.artifactDrillIn)
}

func (m Model) artifactLauncherVisible() bool {
	return artifactLauncherVisibleForLayout(m.currentArtifactLayoutMode(), m.artifactDrillIn)
}

func (m Model) artifactDrillInVisible() bool {
	return artifactDrillInVisibleForLayout(m.currentArtifactLayoutMode(), m.artifactDrillIn)
}

func (m Model) availableFocusRegionsForLayout(mode artifactLayoutMode) []FocusRegion {
	if artifactDrillInVisibleForLayout(mode, m.artifactDrillIn) {
		return []FocusRegion{FocusRegionArtifactFiles, FocusRegionArtifactPreview}
	}
	artifactPaneVisible := artifactPaneVisibleForLayout(mode, m.artifactDrillIn)
	artifactLauncherVisible := artifactLauncherVisibleForLayout(mode, m.artifactDrillIn)
	switch m.screen {
	case ScreenNewTask:
		return []FocusRegion{FocusRegionComposer}
	case ScreenApproval:
		regions := []FocusRegion{FocusRegionActionPanel}
		if m.composerRegionVisible() {
			regions = append(regions, FocusRegionComposer)
		}
		regions = append(regions, FocusRegionDetail)
		if artifactPaneVisible {
			regions = append(regions, FocusRegionArtifactFiles, FocusRegionArtifactPreview)
		} else if artifactLauncherVisible {
			regions = append(regions, FocusRegionArtifactLauncher)
		}
		return regions
	case ScreenClarification:
		regions := []FocusRegion{FocusRegionChoices, FocusRegionComposer, FocusRegionActionPanel, FocusRegionDetail}
		if artifactPaneVisible {
			regions = append(regions, FocusRegionArtifactFiles, FocusRegionArtifactPreview)
		} else if artifactLauncherVisible {
			regions = append(regions, FocusRegionArtifactLauncher)
		}
		return regions
	case ScreenFailed:
		regions := make([]FocusRegion, 0, 4)
		if len(m.availableFailureActions()) > 0 {
			regions = append(regions, FocusRegionActionPanel)
		}
		regions = append(regions, FocusRegionDetail)
		if artifactPaneVisible {
			regions = append(regions, FocusRegionArtifactFiles, FocusRegionArtifactPreview)
		} else if artifactLauncherVisible {
			regions = append(regions, FocusRegionArtifactLauncher)
		}
		return regions
	case ScreenRunning, ScreenComplete:
		regions := []FocusRegion{FocusRegionDetail}
		if artifactPaneVisible {
			regions = append(regions, FocusRegionArtifactFiles, FocusRegionArtifactPreview)
		} else if artifactLauncherVisible {
			regions = append(regions, FocusRegionArtifactLauncher)
		}
		return regions
	default:
		return nil
	}
}

func (m Model) defaultFocusRegionForLayout(mode artifactLayoutMode) FocusRegion {
	if artifactDrillInVisibleForLayout(mode, m.artifactDrillIn) {
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

func (m Model) availableFocusRegions() []FocusRegion {
	return m.availableFocusRegionsForLayout(m.currentArtifactLayoutMode())
}

func (m Model) defaultFocusRegion() FocusRegion {
	return m.defaultFocusRegionForLayout(m.currentArtifactLayoutMode())
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

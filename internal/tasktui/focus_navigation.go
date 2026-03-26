package tasktui

import tea "charm.land/bubbletea/v2"

func (m *Model) handleFocusNavigationKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	if !keyMatches(msg, m.keys.nextFocus) {
		return nil, false
	}
	if m.screen == ScreenTaskList {
		return nil, false
	}
	regions := m.availableFocusRegions()
	if len(regions) <= 1 {
		return nil, false
	}
	index := focusRegionIndex(regions, m.focusRegion)
	if index < 0 {
		m.focusRegion = regions[0]
	} else {
		m.focusRegion = regions[(index+1)%len(regions)]
	}
	return m.syncInputFocus(), true
}

func (m *Model) handleFocusedRegionKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	if cmd, handled := m.handleDetailPaneKey(msg); handled {
		return cmd, true
	}
	if cmd, handled := m.handleArtifactPaneKey(msg); handled {
		return cmd, true
	}
	if cmd, handled := m.handleFailureActionPaneKey(msg); handled {
		return cmd, true
	}
	return nil, false
}

func (m *Model) handleFailureActionPaneKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	if m.focusRegion != FocusRegionActionPanel || m.screen != ScreenFailed {
		return nil, false
	}
	if len(m.availableFailureActions()) == 0 {
		return nil, false
	}
	switch {
	case keyMatches(msg, m.keys.up):
		m.selectNextFailureAction(-1)
		return nil, true
	case keyMatches(msg, m.keys.down):
		m.selectNextFailureAction(1)
		return nil, true
	case keyMatches(msg, m.keys.confirm):
		return m.triggerSelectedFailureAction(), true
	default:
		return nil, false
	}
}

func (m *Model) handleDetailPaneKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	if m.focusRegion != FocusRegionDetail || !m.isDetailScreen() {
		return nil, false
	}
	switch {
	case keyMatches(msg, m.keys.up), keyMatches(msg, m.keys.down):
		nextViewport, cmd := m.detailViewport.Update(msg)
		m.detailViewport = nextViewport
		return cmd, true
	default:
		return nil, false
	}
}

func (m *Model) handleArtifactPaneKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	switch m.focusRegion {
	case FocusRegionArtifactFiles:
		if !m.artifactTabActive() {
			return nil, false
		}
		switch {
		case keyMatches(msg, m.keys.up):
			m.artifactIndex = moveSelection(m.artifactIndex, -1, len(m.artifactItems))
			return nil, true
		case keyMatches(msg, m.keys.down):
			m.artifactIndex = moveSelection(m.artifactIndex, 1, len(m.artifactItems))
			return nil, true
		}
	case FocusRegionArtifactPreview:
		if !m.artifactTabActive() {
			return nil, false
		}
		switch {
		case keyMatches(msg, m.keys.up), keyMatches(msg, m.keys.down):
			nextPreview, cmd := m.artifactPreview.Update(msg)
			m.artifactPreview = nextPreview
			return cmd, true
		}
	}
	return nil, false
}

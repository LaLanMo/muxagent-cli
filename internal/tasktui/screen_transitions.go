package tasktui

import tea "charm.land/bubbletea/v2"

func detailArtifactCollapsedDefault(screen Screen) bool {
	switch screen {
	case ScreenComplete:
		return true
	case ScreenRunning, ScreenApproval, ScreenClarification, ScreenFailed:
		return false
	default:
		return true
	}
}

func (m *Model) openNewTask() tea.Cmd {
	if m.screen != ScreenNewTask {
		m.returnScreen = m.screen
	}
	m.setScreen(ScreenNewTask)
	m.editor.ClearSlot(editorSlotNewTask)
	m.syncComponents()
	return m.syncInputFocus()
}

func (m *Model) closeNewTask() {
	m.editor.ClearSlot(editorSlotNewTask)
	if m.current != nil && (m.activeTaskID != "" || m.hasPendingStartTask()) {
		m.setDetailScreen(detailScreenForActiveTask(m.current, m.currentInput), true)
		return
	}
	m.setScreen(m.returnScreen)
	if m.screen == ScreenNewTask {
		m.setScreen(ScreenTaskList)
	}
}

func (m *Model) setScreen(screen Screen) {
	if m.screen != screen {
		m.focusRegion = FocusRegionNone
	}
	if screen != ScreenFailed {
		m.failure.action = failureActionNone
	}
	m.screen = screen
}

func (m *Model) setDetailScreen(screen Screen, resetArtifacts bool) {
	m.setScreen(screen)
	if resetArtifacts {
		m.artifactCollapsed = detailArtifactCollapsedDefault(screen)
		m.artifactDrillIn = false
	}
}

func (m *Model) returnToTaskList() tea.Cmd {
	m.clearActiveTask()
	m.setScreen(ScreenTaskList)
	m.syncComponents()
	return tea.Batch(m.loadTasksCmd(), m.syncInputFocus())
}

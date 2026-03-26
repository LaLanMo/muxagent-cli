package tasktui

import tea "charm.land/bubbletea/v2"

func (m Model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case m.dialog != nil:
		return m.handleDialogKey(msg)
	case keyMatches(msg, m.keys.quit):
		cmd := m.openDialog(newQuitDialog())
		m.syncComponents()
		return m, cmd
	}

	if cmd, handled := m.handleFocusNavigationKey(msg); handled {
		m.syncComponents()
		return m, cmd
	}
	if cmd, handled := m.handleFocusedRegionKey(msg); handled {
		m.syncComponents()
		return m, cmd
	}

	return m.handleScreenKey(msg)
}

func (m Model) handleScreenKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch m.screen {
	case ScreenTaskList:
		return m.handleTaskListKey(msg)
	case ScreenNewTask:
		return m.handleNewTaskKey(msg)
	case ScreenApproval:
		return m.handleApprovalKey(msg)
	case ScreenClarification:
		return m.handleClarificationKey(msg)
	default:
		return m.handleDetailKey(msg)
	}
}

// forwardToActiveInput routes non-key messages (e.g. clipboard paste results)
// to whichever textarea is currently active.
func (m Model) forwardToActiveInput(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.dialog != nil {
		return m, nil
	}
	if m.focusRegion == FocusRegionComposer && m.activeEditorSlot() != "" {
		cmd := m.editor.Update(msg)
		m.syncComponents()
		return m, cmd
	}
	return m, nil
}

func (m Model) handleDialogKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.dialog == nil {
		return m, nil
	}
	switch m.dialog.HandleKey(msg) {
	case dialogActionQuit:
		return m, tea.Quit
	case dialogActionClose:
		cmd := m.closeDialog()
		m.syncComponents()
		return m, cmd
	default:
		m.syncComponents()
		return m, nil
	}
}

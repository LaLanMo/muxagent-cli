package tasktui

import tea "charm.land/bubbletea/v2"

func (m *Model) syncInputFocus() tea.Cmd {
	var cmds []tea.Cmd
	if m.dialog != nil {
		m.newTaskInput.Blur()
		m.detailInput.Blur()
		return nil
	}
	if m.screen == ScreenNewTask && m.focusRegion == FocusRegionComposer {
		cmds = append(cmds, m.newTaskInput.Focus())
	} else {
		m.newTaskInput.Blur()
	}
	if m.shouldFocusDetailComposer() {
		cmds = append(cmds, m.detailInput.Focus())
	} else {
		m.detailInput.Blur()
	}
	return tea.Batch(cmds...)
}

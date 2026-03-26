package tasktui

import tea "charm.land/bubbletea/v2"

func (m Model) handleTaskListKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if keyMatches(msg, m.keys.open) {
		item, ok := m.taskList.SelectedItem().(taskListItem)
		if !ok {
			return m, nil
		}
		if item.action == taskListActionNewTask {
			cmd := m.openNewTask()
			m.syncComponents()
			return m, cmd
		}
		return m, m.openTaskCmd(item.view.Task.ID)
	}

	nextList, cmd := m.taskList.Update(msg)
	m.taskList = nextList
	m.syncComponents()
	return m, cmd
}

func (m Model) handleNewTaskKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.focusRegion == FocusRegionActionPanel {
		switch {
		case keyMatches(msg, m.keys.back):
			m.closeNewTask()
			m.syncComponents()
			return m, m.syncInputFocus()
		case keyMatches(msg, m.keys.confirm):
			cmd := m.submitNewTask()
			return m, cmd
		default:
			return m, nil
		}
	}
	switch {
	case keyMatches(msg, m.keys.back):
		m.closeNewTask()
		m.syncComponents()
		return m, m.syncInputFocus()
	default:
		cmd := m.newTaskInput.Update(msg)
		m.syncComponents()
		return m, cmd
	}
}

func (m Model) handleDetailKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if keyMatches(msg, m.keys.back) {
		return m, m.returnToTaskList()
	}

	nextViewport, cmd := m.detailViewport.Update(msg)
	m.detailViewport = nextViewport
	return m, cmd
}

func keyMatches(msg tea.KeyPressMsg, binding interface{ Keys() []string }) bool {
	for _, candidate := range binding.Keys() {
		if msg.String() == candidate {
			return true
		}
	}
	return false
}

package tasktui

import (
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

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
	switch {
	case keyMatches(msg, m.keys.back):
		m.closeNewTask()
		m.syncComponents()
		return m, m.syncInputFocus()
	case keyMatches(msg, m.keys.nextFocus):
		if strings.TrimSpace(m.editor.Value()) == "" {
			return m, nil
		}
		return m, m.submitNewTask()
	default:
		cmd := m.editor.Update(msg)
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

func keyMatches(msg tea.KeyPressMsg, binding key.Binding) bool {
	return key.Matches(msg, binding)
}

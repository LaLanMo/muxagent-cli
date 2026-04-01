package tasktui

import (
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

func (m Model) handleTaskListKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if keyMatches(msg, m.keys.open) || msg.Code == tea.KeyEnter {
		item, ok := selectedTaskListItem(m.taskList)
		if !ok {
			if len(m.tasks) > 0 {
				return m, m.openTaskCmd(m.tasks[0].Task.ID)
			}
			cmd := m.openNewTask()
			m.syncComponents()
			return m, cmd
		}
		if item.action == taskListActionNewTask {
			return m, m.openNewTask()
		}
		if item.action == taskListActionManageConfigs {
			return m, m.openTaskConfigs()
		}
		return m, m.openTaskCmd(item.view.Task.ID)
	}

	nextList, cmd := m.taskList.Update(msg)
	m.taskList = nextList
	m.syncComponents()
	return m, cmd
}

func (m Model) handleTaskConfigListKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.taskConfigs.form != nil {
		return m.handleTaskConfigFormKey(msg)
	}
	if m.taskConfigs.confirm != nil {
		return m.handleTaskConfigConfirmKey(msg)
	}
	switch {
	case keyMatches(msg, m.keys.back):
		cmd := m.closeTaskConfigs()
		m.syncComponents()
		return m, cmd
	case keyMatches(msg, m.keys.confirm):
		cmd := m.submitSetDefaultTaskConfig()
		m.syncComponents()
		return m, cmd
	case keyMatches(msg, m.keys.toggleDetailTab):
		cmd := m.toggleSelectedTaskConfigRuntime()
		m.syncComponents()
		return m, cmd
	case keyMatches(msg, m.keys.renameConfig):
		cmd := m.openRenameTaskConfigForm()
		m.syncComponents()
		return m, cmd
	case keyMatches(msg, m.keys.deleteConfig):
		cmd := m.openDeleteTaskConfigConfirm()
		m.syncComponents()
		return m, cmd
	}
	nextList, cmd := m.configList.Update(msg)
	m.configList = nextList
	if selected, ok := selectedTaskConfigListItem(m.configList); ok {
		if m.taskConfigs.selectedAlias != selected.summary.Alias {
			m.taskConfigs.statusText = ""
		}
		m.taskConfigs.selectedAlias = selected.summary.Alias
	}
	m.syncComponents()
	return m, cmd
}

func (m Model) handleTaskConfigFormKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case keyMatches(msg, m.keys.back):
		m.closeTaskConfigForm()
		m.syncComponents()
		return m, m.syncInputFocus()
	case keyMatches(msg, m.keys.confirm):
		cmd := m.submitTaskConfigForm()
		m.syncComponents()
		return m, cmd
	default:
		cmd := m.editor.Update(msg)
		m.syncComponents()
		return m, cmd
	}
}

func (m Model) handleTaskConfigConfirmKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case keyMatches(msg, m.keys.back):
		m.taskConfigs.confirm = nil
		m.syncComponents()
		return m, m.syncInputFocus()
	case keyMatches(msg, m.keys.confirm):
		cmd := m.submitDeleteTaskConfig()
		m.syncComponents()
		return m, cmd
	default:
		return m, nil
	}
}

func (m Model) handleNewTaskKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case keyMatches(msg, m.keys.back):
		m.closeNewTask()
		m.syncComponents()
		return m, m.syncInputFocus()
	case keyMatches(msg, m.keys.prevConfig):
		if m.cycleTaskConfig(-1) {
			m.syncComponents()
		}
		return m, nil
	case keyMatches(msg, m.keys.nextConfig):
		if m.cycleTaskConfig(1) {
			m.syncComponents()
		}
		return m, nil
	case keyMatches(msg, m.keys.toggleWorktree):
		if m.toggleNewTaskWorktree() {
			m.syncComponents()
		}
		return m, nil
	case keyMatches(msg, m.keys.confirm):
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

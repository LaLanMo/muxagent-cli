package tasktui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/LaLanMo/muxagent-cli/internal/taskdomain"
	"github.com/LaLanMo/muxagent-cli/internal/taskruntime"
)

const approvalPlanNodeName = "approve_plan"

func (m *Model) openNewTask() tea.Cmd {
	if m.screen != ScreenNewTask {
		m.returnScreen = m.screen
	}
	m.setScreen(ScreenNewTask)
	m.focusRegion = FocusRegionFormEditor
	m.newTask.useWorktree = m.defaultNewTaskUseWorktree()
	m.editor.ClearSlot(editorSlotNewTask)
	m.syncComponents()
	focusCmd := m.editor.Focus()
	return tea.Batch(focusCmd, m.syncInputFocus())
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
		m.activeDetailTab = m.defaultDetailTab(screen)
	}
}

func (m Model) defaultDetailTab(screen Screen) DetailTab {
	if shouldDefaultApprovalToArtifacts(screen, m.current, m.currentInput) {
		return DetailTabArtifacts
	}
	return DetailTabTimeline
}

func shouldDefaultApprovalToArtifacts(screen Screen, current *taskdomain.TaskView, input *taskruntime.InputRequest) bool {
	if screen != ScreenApproval || input == nil {
		return false
	}
	if input.Kind != taskruntime.InputKindHumanNode {
		return false
	}
	if input.NodeName != approvalPlanNodeName {
		return false
	}
	return artifactPaneHasVisibleArtifacts(current, input)
}

func (m *Model) returnToTaskList() tea.Cmd {
	selectedTaskID := ""
	if m.current != nil {
		selectedTaskID = m.current.Task.ID
	}
	m.clearActiveTask()
	m.setScreen(ScreenTaskList)
	m.syncComponents()
	if selectedTaskID != "" {
		selectTaskListTask(&m.taskList, selectedTaskID)
	}
	return tea.Batch(m.loadTasksCmd(), m.syncInputFocus())
}

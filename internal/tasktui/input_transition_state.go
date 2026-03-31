package tasktui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/LaLanMo/muxagent-cli/internal/taskdomain"
	"github.com/LaLanMo/muxagent-cli/internal/taskruntime"
)

func (m *Model) resetInputState() {
	m.approval.choice = 0
	m.clarification.question = 0
	m.clarification.option = 0
	m.clarification.headerSelection = 0
	m.clarification.answers = nil
	m.clarification.other = nil
	m.submittingInput = false
	m.focusRegion = FocusRegionNone
	m.editor.ClearAll()
}

func (m *Model) activateClarificationOtherInput(question taskdomain.ClarificationQuestion) tea.Cmd {
	_ = question
	m.focusRegion = FocusRegionComposer
	m.syncComponents()
	return m.syncInputFocus()
}

func (m Model) shouldClearSubmittedInput(event taskruntime.RunEvent) bool {
	if !m.submittingInput || m.currentInput == nil {
		return false
	}
	switch event.Type {
	case taskruntime.EventTaskCompleted, taskruntime.EventTaskFailed:
		return true
	case taskruntime.EventNodeStarted, taskruntime.EventNodeProgress, taskruntime.EventNodeCompleted, taskruntime.EventNodeFailed:
		return event.NodeRunID != "" && event.NodeRunID == m.currentInput.NodeRunID
	default:
		return event.TaskView != nil && event.TaskView.Status != taskdomain.TaskStatusAwaitingUser
	}
}

func (m Model) currentAwaitingRunID() string {
	if m.current == nil {
		return ""
	}
	for i := len(m.current.NodeRuns) - 1; i >= 0; i-- {
		run := m.current.NodeRuns[i]
		if run.Status == taskdomain.NodeRunAwaitingUser {
			return run.ID
		}
	}
	if m.currentInput != nil {
		return m.currentInput.NodeRunID
	}
	return ""
}

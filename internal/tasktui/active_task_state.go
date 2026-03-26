package tasktui

import (
	"github.com/LaLanMo/muxagent-cli/internal/taskconfig"
	"github.com/LaLanMo/muxagent-cli/internal/taskdomain"
	"github.com/LaLanMo/muxagent-cli/internal/taskruntime"
)

func (m *Model) activateTask(view taskdomain.TaskView, cfg *taskconfig.Config, input *taskruntime.InputRequest) {
	m.activeTaskID = view.Task.ID
	m.current = &view
	if cfg != nil {
		m.currentConfig = cfg
	} else if m.currentConfig == nil {
		m.currentConfig = m.launchConfig
	}
	m.currentInput = input
	m.resetInputState()
	m.setDetailScreen(detailScreenForActiveTask(m.current, m.currentInput), true)
}

func (m *Model) clearActiveTask() {
	m.activeTaskID = ""
	m.pendingRuntimeCmd = nil
	m.current = nil
	m.currentConfig = nil
	m.currentInput = nil
	m.startupText = ""
	m.errorText = ""
	m.failure.action = failureActionNone
}

func (m Model) isActiveTask(taskID string) bool {
	return taskID != "" && m.activeTaskID != "" && m.activeTaskID == taskID
}

func (m Model) hasPendingRuntimeCommand() bool {
	return m.pendingRuntimeCmd != nil
}

func (m Model) hasPendingStartTask() bool {
	return m.pendingRuntimeCmd != nil && m.pendingRuntimeCmd.kind == pendingRuntimeCommandStartTask
}

func (m Model) shouldFollowPendingRuntimeCommand(event taskruntime.RunEvent) bool {
	if m.pendingRuntimeCmd == nil {
		return false
	}
	switch event.Type {
	case taskruntime.EventTaskCreated:
		return m.pendingRuntimeCmd.kind == pendingRuntimeCommandStartTask
	case taskruntime.EventCommandError:
		return true
	default:
		return m.pendingRuntimeCmd.taskID != "" && m.pendingRuntimeCmd.taskID == event.TaskID
	}
}

func (m *Model) syncPendingRuntimeCommandTask(taskID string) {
	if m.pendingRuntimeCmd == nil || taskID == "" {
		return
	}
	m.pendingRuntimeCmd.taskID = taskID
}

func (m *Model) clearPendingRuntimeCommandIfSettled(event taskruntime.RunEvent) {
	if m.pendingRuntimeCmd == nil {
		return
	}
	switch event.Type {
	case taskruntime.EventTaskCreated:
		if m.pendingRuntimeCmd.kind == pendingRuntimeCommandStartTask {
			m.syncPendingRuntimeCommandTask(event.TaskID)
		}
	case taskruntime.EventCommandError:
		m.pendingRuntimeCmd = nil
	case taskruntime.EventNodeStarted,
		taskruntime.EventNodeProgress,
		taskruntime.EventNodeCompleted,
		taskruntime.EventNodeFailed,
		taskruntime.EventInputRequested,
		taskruntime.EventTaskCompleted,
		taskruntime.EventTaskFailed:
		if m.pendingRuntimeCmd.taskID == "" || m.pendingRuntimeCmd.taskID == event.TaskID {
			m.pendingRuntimeCmd = nil
		}
	}
}

func (m Model) shouldFollowEvent(event taskruntime.RunEvent) bool {
	return m.shouldFollowPendingRuntimeCommand(event) || m.isActiveTask(event.TaskID)
}

func detailScreenForActiveTask(current *taskdomain.TaskView, input *taskruntime.InputRequest) Screen {
	if input != nil {
		switch input.Kind {
		case taskruntime.InputKindHumanNode:
			return ScreenApproval
		case taskruntime.InputKindClarification:
			return ScreenClarification
		}
	}
	if current == nil {
		return ScreenRunning
	}
	switch current.Status {
	case taskdomain.TaskStatusFailed:
		return ScreenFailed
	case taskdomain.TaskStatusDone:
		return ScreenComplete
	default:
		return ScreenRunning
	}
}

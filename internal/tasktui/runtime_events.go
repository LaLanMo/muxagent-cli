package tasktui

import (
	"strings"

	"github.com/LaLanMo/muxagent-cli/internal/taskdomain"
	"github.com/LaLanMo/muxagent-cli/internal/taskruntime"
)

func (m *Model) handleEvent(event taskruntime.RunEvent) {
	if event.TaskView != nil {
		m.hydrateRunSessionIDs(*event.TaskView)
		m.upsertTask(*event.TaskView)
	}
	if event.Progress != nil {
		m.applyProgressEvent(event)
	}
	if !m.shouldFollowEvent(event) {
		return
	}
	if event.TaskView != nil {
		view := *event.TaskView
		if m.hasPendingStartTask() && event.Type == taskruntime.EventTaskCreated {
			m.editor.ClearSlot(editorSlotNewTask)
			m.activateTask(view, event.Config, nil)
			m.syncPendingRuntimeCommandTask(event.TaskID)
		} else {
			m.current = &view
			if event.Config != nil {
				m.currentConfig = event.Config
			}
		}
	}
	if event.Error != nil {
		m.errorText = event.Error.Message
	}
	if m.shouldClearSubmittedInput(event) {
		m.currentInput = nil
		m.submittingInput = false
	}
	if event.InputRequest != nil {
		m.currentInput = event.InputRequest
		m.resetInputState()
		m.autoScrollDetail = true
		if m.screen != ScreenNewTask {
			m.setDetailScreen(detailScreenForActiveTask(m.current, m.currentInput), true)
		}
		return
	}
	switch event.Type {
	case taskruntime.EventCommandError:
		m.submittingInput = false
		m.restoreCommandFailureState()
	}
	if m.screen == ScreenNewTask {
		m.clearPendingRuntimeCommandIfSettled(event)
		return
	}
	switch event.Type {
	case taskruntime.EventNodeStarted:
		m.startupText = ""
		m.setScreen(detailScreenForActiveTask(m.current, m.currentInput))
		m.autoScrollDetail = true
	case taskruntime.EventNodeCompleted:
		m.clearRunProgress(event.NodeRunID)
		m.startupText = ""
		m.setScreen(detailScreenForActiveTask(m.current, m.currentInput))
		m.autoScrollDetail = true
	case taskruntime.EventNodeFailed:
		m.clearRunProgress(event.NodeRunID)
		m.startupText = ""
		if m.current != nil {
			m.setScreen(detailScreenForActiveTask(m.current, m.currentInput))
		}
		m.autoScrollDetail = true
	case taskruntime.EventNodeProgress:
		m.startupText = ""
		m.setScreen(detailScreenForActiveTask(m.current, m.currentInput))
		m.autoScrollDetail = true
	case taskruntime.EventTaskCompleted:
		m.clearTaskProgress(event.TaskView)
		m.startupText = ""
		m.setDetailScreen(ScreenComplete, true)
		m.autoScrollDetail = true
	case taskruntime.EventTaskFailed:
		m.clearTaskProgress(event.TaskView)
		m.startupText = ""
		m.currentInput = nil
		if m.current != nil {
			m.setDetailScreen(detailScreenForActiveTask(m.current, m.currentInput), true)
		} else {
			m.setDetailScreen(ScreenFailed, true)
		}
		m.autoScrollDetail = true
	case taskruntime.EventCommandError:
		m.startupText = ""
	}
	m.clearPendingRuntimeCommandIfSettled(event)
}

func (m *Model) restoreCommandFailureState() {
	if m.pendingRuntimeCmd == nil {
		return
	}
	m.startupText = ""
	switch m.pendingRuntimeCmd.kind {
	case pendingRuntimeCommandStartTask:
		if m.pendingRuntimeCmd.taskID != "" {
			return
		}
		m.activeTaskID = ""
		m.current = nil
		m.currentConfig = nil
		m.currentInput = nil
		m.setScreen(ScreenNewTask)
		m.autoScrollDetail = false
	case pendingRuntimeCommandRetry, pendingRuntimeCommandForceRetry, pendingRuntimeCommandContinueBlocked:
		screen := m.pendingRuntimeCmd.restoreScreen
		if screen == ScreenTaskList {
			screen = ScreenFailed
		}
		m.setDetailScreen(screen, true)
		m.failure.action = m.pendingRuntimeCmd.restoreFailureAction
		m.normalizeFailureAction()
		m.autoScrollDetail = true
	}
}

func (m *Model) applyProgressEvent(event taskruntime.RunEvent) {
	if event.Progress == nil || event.NodeRunID == "" {
		return
	}
	if event.Progress.SessionID != "" {
		m.sessionByRun[event.NodeRunID] = event.Progress.SessionID
	}
	if event.Progress.Message == "" {
		return
	}
	messages := append([]string(nil), m.progressByRun[event.NodeRunID]...)
	messages = appendProgressMessage(messages, event.Progress.Message)
	m.progressByRun[event.NodeRunID] = messages
}

func (m *Model) hydrateRunSessionIDs(view taskdomain.TaskView) {
	for _, run := range view.NodeRuns {
		if run.SessionID != "" {
			m.sessionByRun[run.ID] = run.SessionID
		}
		if run.Status != taskdomain.NodeRunRunning {
			delete(m.progressByRun, run.ID)
		}
	}
}

func (m *Model) clearRunProgress(nodeRunID string) {
	if nodeRunID == "" {
		return
	}
	delete(m.progressByRun, nodeRunID)
}

func (m *Model) clearTaskProgress(view *taskdomain.TaskView) {
	if view == nil {
		return
	}
	for _, run := range view.NodeRuns {
		delete(m.progressByRun, run.ID)
	}
}

func appendProgressMessage(messages []string, raw string) []string {
	for _, item := range strings.Split(raw, "\n") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if len(messages) > 0 && messages[len(messages)-1] == item {
			continue
		}
		messages = append(messages, item)
	}
	if len(messages) > 4 {
		messages = append([]string(nil), messages[len(messages)-4:]...)
	}
	return messages
}

func (m *Model) upsertTask(view taskdomain.TaskView) {
	for i := range m.tasks {
		if m.tasks[i].Task.ID == view.Task.ID {
			m.tasks[i] = view
			m.taskEventVersion++
			return
		}
	}
	m.tasks = append([]taskdomain.TaskView{view}, m.tasks...)
	m.taskEventVersion++
}

func (m Model) currentRecoveryTarget() *taskdomain.RecoveryTarget {
	if m.current == nil || m.currentConfig == nil {
		return nil
	}
	return taskdomain.RecoveryTargetForTask(m.currentConfig, currentTaskRuns(*m.current), m.current.BlockedSteps)
}

func currentTaskRuns(view taskdomain.TaskView) []taskdomain.NodeRun {
	runs := make([]taskdomain.NodeRun, 0, len(view.NodeRuns))
	for _, run := range view.NodeRuns {
		runs = append(runs, run.NodeRun)
	}
	return runs
}

package tasktui

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/LaLanMo/muxagent-cli/internal/taskdomain"
	"github.com/LaLanMo/muxagent-cli/internal/taskruntime"
)

func (m *Model) submitNewTask() tea.Cmd {
	desc := strings.TrimSpace(m.editor.Value())
	if desc == "" {
		return nil
	}
	entry, err := m.launchTaskConfigEntry()
	if err != nil {
		m.errorText = err.Error()
		m.syncComponents()
		return nil
	}
	m.clearActiveTask()
	m.pendingRuntimeCmd = &pendingRuntimeCommand{
		kind: pendingRuntimeCommandStartTask,
	}
	m.startupText = "Starting task…"
	m.errorText = ""
	m.current = &taskdomain.TaskView{
		Task: taskdomain.Task{
			Description: desc,
			ConfigAlias: entry.Alias,
			ConfigPath:  entry.Path,
			WorkDir:     m.workDir,
		},
		Status: taskdomain.TaskStatusRunning,
	}
	m.currentConfig = entry.Config
	m.currentInput = nil
	m.setDetailScreen(ScreenRunning, true)
	m.syncComponents()
	return m.dispatchCmd(taskruntime.RunCommand{
		Type:        taskruntime.CommandStartTask,
		Description: desc,
		ConfigAlias: entry.Alias,
		WorkDir:     m.workDir,
		ConfigPath:  entry.Path,
		Runtime:     m.effectiveLaunchRuntime(),
	})
}

func (m *Model) applyRetry(force bool) tea.Cmd {
	recovery := m.currentRecoveryTarget()
	if recovery == nil || m.current == nil {
		return nil
	}
	if !force && !recovery.RetryAllowed {
		return nil
	}
	if recovery.Kind == taskdomain.RecoveryTargetBlockedStep && !force {
		return nil
	}
	pendingKind := pendingRuntimeCommandRetry
	switch {
	case recovery.Kind == taskdomain.RecoveryTargetBlockedStep:
		pendingKind = pendingRuntimeCommandContinueBlocked
	case force:
		pendingKind = pendingRuntimeCommandForceRetry
	}
	m.pendingRuntimeCmd = &pendingRuntimeCommand{
		kind:                 pendingKind,
		taskID:               m.current.Task.ID,
		restoreScreen:        ScreenFailed,
		restoreFailureAction: m.failure.action,
	}
	m.currentInput = nil
	switch recovery.Kind {
	case taskdomain.RecoveryTargetBlockedStep:
		m.startupText = "Continuing " + recovery.NodeName + "…"
	default:
		m.startupText = "Retrying " + recovery.NodeName + "…"
	}
	m.errorText = ""
	m.autoScrollDetail = true
	m.setDetailScreen(ScreenRunning, true)
	m.syncComponents()
	cmd := taskruntime.RunCommand{
		Type:   taskruntime.CommandRetryNode,
		TaskID: m.current.Task.ID,
		Force:  force,
	}
	if recovery.Kind == taskdomain.RecoveryTargetBlockedStep && recovery.BlockedStep != nil {
		cmd.Type = taskruntime.CommandContinueBlocked
		cmd.Force = false
	} else if recovery.Run != nil {
		cmd.NodeRunID = recovery.Run.ID
	}
	return m.dispatchCmd(cmd)
}

func (m Model) triggerRetry(force bool) (tea.Model, tea.Cmd) {
	cmd := m.applyRetry(force)
	return m, cmd
}

func (m Model) submitCurrentInput(payload map[string]interface{}) (tea.Model, tea.Cmd) {
	if m.currentInput == nil || m.current == nil || m.submittingInput {
		return m, nil
	}
	command := taskruntime.RunCommand{
		Type:      taskruntime.CommandSubmitInput,
		TaskID:    m.current.Task.ID,
		NodeRunID: m.currentAwaitingRunID(),
		Payload:   payload,
	}
	if command.TaskID == "" || command.NodeRunID == "" {
		return m, nil
	}
	m.submittingInput = true
	m.errorText = ""
	m.syncComponents()
	return m, tea.Batch(
		m.syncInputFocus(),
		m.dispatchCmd(command),
	)
}

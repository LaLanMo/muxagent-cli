package tasktui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/LaLanMo/muxagent-cli/internal/taskdomain"
)

type failureAction int

const (
	failureActionNone failureAction = iota
	failureActionRetry
	failureActionForceRetry
	failureActionForceContinue
)

func (m Model) availableFailureActions() []failureAction {
	recovery := m.currentRecoveryTarget()
	if recovery == nil {
		return nil
	}
	switch {
	case recovery.Kind == taskdomain.RecoveryTargetBlockedStep && recovery.ForceRetryAllowed:
		return []failureAction{failureActionForceContinue}
	case recovery.RetryAllowed:
		return []failureAction{failureActionRetry}
	case recovery.ForceRetryAllowed:
		return []failureAction{failureActionForceRetry}
	default:
		return nil
	}
}

func failureActionLabel(action failureAction) string {
	switch action {
	case failureActionRetry:
		return "Retry step"
	case failureActionForceRetry:
		return "Force retry"
	case failureActionForceContinue:
		return "Force continue"
	default:
		return ""
	}
}

func (m *Model) normalizeFailureAction() {
	actions := m.availableFailureActions()
	if len(actions) == 0 {
		m.failure.action = failureActionNone
		return
	}
	for _, action := range actions {
		if action == m.failure.action {
			return
		}
	}
	m.failure.action = actions[0]
}

func (m *Model) selectNextFailureAction(delta int) {
	actions := m.availableFailureActions()
	if len(actions) == 0 {
		m.failure.action = failureActionNone
		return
	}
	index := 0
	for i, action := range actions {
		if action == m.failure.action {
			index = i
			break
		}
	}
	index = moveSelection(index, delta, len(actions))
	m.failure.action = actions[index]
}

func (m *Model) triggerSelectedFailureAction() tea.Cmd {
	switch m.failure.action {
	case failureActionRetry:
		return m.applyRetry(false)
	case failureActionForceRetry, failureActionForceContinue:
		return m.applyRetry(true)
	default:
		return nil
	}
}

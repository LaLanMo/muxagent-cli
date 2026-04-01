package tasktui

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/LaLanMo/muxagent-cli/internal/taskruntime"
)

const (
	followUpRowInput = iota
	followUpRowSubmit
	followUpRowCount
)

func followUpEditorSlot(taskID string) string {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return ""
	}
	return "followup:" + taskID
}

func (m Model) completeFollowUpVisible() bool {
	return m.screen == ScreenComplete && m.current != nil
}

func (m Model) followUpInputActive() bool {
	return m.focusRegion == FocusRegionActionPanel && m.followUp.choice == followUpRowInput
}

func (m Model) followUpPending() bool {
	return m.pendingRuntimeCmd != nil && m.pendingRuntimeCmd.kind == pendingRuntimeCommandStartFollowUp
}

func (m Model) followUpRequestText() string {
	return strings.TrimSpace(m.editor.Value())
}

func (m Model) canSubmitFollowUp() bool {
	return m.completeFollowUpVisible() && !m.followUpPending() && m.followUpRequestText() != ""
}

func (m Model) followUpSubmitLabel() string {
	switch {
	case m.followUpPending():
		return "Starting follow-up…"
	case m.canSubmitFollowUp():
		return "Start follow-up task"
	default:
		return "Write what should happen next"
	}
}

func (m *Model) selectFollowUpRow(row int) tea.Cmd {
	m.followUp.choice = clamp(row, 0, followUpRowCount-1)
	m.syncComponents()
	return m.syncInputFocus()
}

func (m *Model) submitFollowUpTask() tea.Cmd {
	if !m.canSubmitFollowUp() || m.current == nil {
		return nil
	}
	m.pendingRuntimeCmd = &pendingRuntimeCommand{
		kind:          pendingRuntimeCommandStartFollowUp,
		taskID:        m.current.Task.ID,
		restoreScreen: ScreenComplete,
	}
	m.errorText = ""
	m.syncComponents()
	return m.dispatchCmd(taskruntimeCommandStartFollowUp(m.current.Task.ID, m.followUpRequestText()))
}

func taskruntimeCommandStartFollowUp(parentTaskID, description string) taskruntime.RunCommand {
	return taskruntime.RunCommand{
		Type:         taskruntime.CommandStartFollowUp,
		ParentTaskID: parentTaskID,
		Description:  description,
	}
}

func (m Model) handleCompleteKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.focusRegion == FocusRegionActionPanel {
		return m.handleFollowUpPanelKey(msg)
	}
	return m.handleDetailKey(msg)
}

func (m Model) handleFollowUpPanelKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case keyMatches(msg, m.keys.back):
		if m.followUpInputActive() {
			cmd := m.selectFollowUpRow(followUpRowSubmit)
			return m, cmd
		}
		m.focusRegion = FocusRegionDetail
		m.syncComponents()
		return m, m.syncInputFocus()
	case m.followUpPending():
		return m, nil
	case keyMatches(msg, m.keys.up):
		cmd := m.selectFollowUpRow(moveSelection(m.followUp.choice, -1, followUpRowCount))
		return m, cmd
	case keyMatches(msg, m.keys.down):
		cmd := m.selectFollowUpRow(moveSelection(m.followUp.choice, 1, followUpRowCount))
		return m, cmd
	case m.followUpInputActive() && keyMatches(msg, m.keys.confirm):
		cmd := m.editor.Update(msg)
		m.syncComponents()
		return m, cmd
	case keyMatches(msg, m.keys.confirm):
		if m.followUp.choice == followUpRowSubmit {
			if !m.canSubmitFollowUp() {
				cmd := m.selectFollowUpRow(followUpRowInput)
				return m, cmd
			}
			return m, m.submitFollowUpTask()
		}
		cmd := m.editor.Update(msg)
		m.syncComponents()
		return m, cmd
	default:
		if !m.followUpInputActive() {
			return m, nil
		}
		cmd := m.editor.Update(msg)
		m.syncComponents()
		return m, cmd
	}
}

func (m Model) renderCompleteFooter(surface surfaceRect) string {
	if m.current == nil {
		return m.renderStatsFooter(surface, "", "", m.detailHint("Esc back"))
	}
	left := taskSummaryLeft(m.current, m.currentConfig)
	right := taskSummaryRight(m.current)
	if m.focusRegion != FocusRegionActionPanel {
		return m.renderStatsFooter(surface, left, right, m.detailHint("Esc back"))
	}
	action := "Enter submit"
	if m.followUpInputActive() {
		action = "Enter newline"
	}
	if m.followUpPending() {
		action = "Starting follow-up…"
	}
	return m.renderStatsFooter(surface, left, right, m.detailHint(joinHintParts("↑↓ select", action, "Esc back")))
}

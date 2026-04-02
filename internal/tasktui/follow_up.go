package tasktui

import (
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/LaLanMo/muxagent-cli/internal/taskruntime"
)

const (
	followUpRowInput = 0
)

func followUpEditorSlot(taskID string) string {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return ""
	}
	return "followup:" + taskID
}

func (m Model) completeFollowUpAvailable() bool {
	return m.screen == ScreenComplete && m.current != nil
}

func (m Model) completeFollowUpVisible() bool {
	return m.completeFollowUpAvailable() && !m.followUp.hidden
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

func (m Model) completeFollowUpToggleHint() string {
	if !m.completeFollowUpAvailable() {
		return ""
	}
	if m.completeFollowUpVisible() {
		return "Ctrl+X hide"
	}
	return "Ctrl+X continue"
}

func (m *Model) selectFollowUpRow(row int) tea.Cmd {
	m.followUp.choice = row
	m.syncComponents()
	return m.syncInputFocus()
}

func (m *Model) toggleCompleteFollowUpVisibility() tea.Cmd {
	if !m.completeFollowUpAvailable() {
		return nil
	}
	m.followUp.hidden = !m.followUp.hidden
	if m.followUp.hidden && m.focusRegion == FocusRegionActionPanel {
		m.focusRegion = FocusRegionDetail
	}
	m.syncComponents()
	return m.syncInputFocus()
}

func (m *Model) submitFollowUpTask() tea.Cmd {
	if !m.canSubmitFollowUp() || m.current == nil {
		return nil
	}
	config, err := m.followUpLaunchConfigSelection()
	if err != nil {
		m.errorText = err.Error()
		m.syncComponents()
		return nil
	}
	m.pendingRuntimeCmd = &pendingRuntimeCommand{
		kind:          pendingRuntimeCommandStartFollowUp,
		taskID:        m.current.Task.ID,
		restoreScreen: ScreenComplete,
	}
	m.errorText = ""
	m.syncComponents()
	return m.dispatchCmd(taskruntimeCommandStartFollowUp(m.current.Task.ID, m.followUpRequestText(), config.Alias, config.Path))
}

func taskruntimeCommandStartFollowUp(parentTaskID, description, configAlias, configPath string) taskruntime.RunCommand {
	return taskruntime.RunCommand{
		Type:         taskruntime.CommandStartFollowUp,
		ParentTaskID: parentTaskID,
		Description:  description,
		ConfigAlias:  configAlias,
		ConfigPath:   configPath,
	}
}

func (m *Model) handleCompleteToggleKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	if !m.completeFollowUpAvailable() || !followUpToggleKey(msg, m.keys.toggleFollowUp) {
		return nil, false
	}
	return m.toggleCompleteFollowUpVisibility(), true
}

func followUpToggleKey(msg tea.KeyPressMsg, binding key.Binding) bool {
	return keyMatches(msg, binding) ||
		msg.Keystroke() == "ctrl+x" ||
		msg.String() == "ctrl+x" ||
		(msg.Code == 'x' && msg.Mod&tea.ModCtrl != 0) ||
		msg.Code == 0x18
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
		m.focusRegion = FocusRegionDetail
		m.syncComponents()
		return m, m.syncInputFocus()
	case m.followUpPending():
		return m, nil
	case keyMatches(msg, m.keys.prevConfig):
		if m.cycleFollowUpConfig(-1) {
			m.errorText = ""
			m.syncComponents()
		}
		return m, nil
	case keyMatches(msg, m.keys.nextConfig):
		if m.cycleFollowUpConfig(1) {
			m.errorText = ""
			m.syncComponents()
		}
		return m, nil
	case keyMatches(msg, m.keys.confirm):
		if !m.canSubmitFollowUp() {
			cmd := m.selectFollowUpRow(followUpRowInput)
			return m, cmd
		}
		return m, m.submitFollowUpTask()
	default:
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
	if m.followUpPending() {
		action = "Starting follow-up…"
	}
	parts := []string{action}
	if len(m.followUpConfigOptions()) > 1 {
		parts = append(parts, "Ctrl+P/N config")
	}
	parts = append(parts, "Ctrl+J newline", "Esc back")
	return m.renderStatsFooter(surface, left, right, m.detailHint(joinHintParts(parts...)))
}

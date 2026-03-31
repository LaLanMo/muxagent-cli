package tasktui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
)

const (
	approvalRowApprove = iota
	approvalRowReject
	approvalRowFeedback
	approvalRowCount
)

func (m Model) handleApprovalKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.submittingInput {
		return m, nil
	}
	switch {
	case keyMatches(msg, m.keys.back):
		return m, m.returnToTaskList()
	case m.focusRegion != FocusRegionActionPanel:
		return m, nil
	case keyMatches(msg, m.keys.up):
		m.approval.choice = moveSelection(m.approval.choice, -1, approvalRowCount)
		m.syncComponents()
		return m, m.syncInputFocus()
	case keyMatches(msg, m.keys.down):
		m.approval.choice = moveSelection(m.approval.choice, 1, approvalRowCount)
		m.syncComponents()
		return m, m.syncInputFocus()
	case approvalFeedbackRowActive(m) && keyMatches(msg, m.keys.confirm):
		cmd := m.editor.Update(msg)
		m.syncComponents()
		return m, cmd
	case keyMatches(msg, m.keys.confirm):
		if m.currentInput == nil || m.current == nil {
			return m, nil
		}
		switch m.approval.choice {
		case approvalRowApprove:
			return m.submitCurrentInput(m.approvalPayload(true))
		case approvalRowReject:
			return m.submitCurrentInput(m.approvalPayload(false))
		default:
			cmd := m.editor.Update(msg)
			m.syncComponents()
			return m, cmd
		}
	default:
		if !approvalFeedbackRowActive(m) {
			return m, nil
		}
		cmd := m.editor.Update(msg)
		m.syncComponents()
		return m, cmd
	}
}

func (m Model) approvalPayload(approved bool) map[string]interface{} {
	payload := map[string]interface{}{
		"approved": approved,
	}
	if feedback := strings.TrimSpace(m.editor.Value()); feedback != "" {
		payload["feedback"] = feedback
	}
	return payload
}

func approvalFeedbackRowActive(m Model) bool {
	return m.focusRegion == FocusRegionActionPanel && m.approval.choice == approvalRowFeedback
}

func approvalHasFeedbackText(text string) bool {
	return strings.TrimSpace(text) != ""
}

func (m Model) approvalActionLabel(approved bool) string {
	if approvalHasFeedbackText(m.editor.Value()) {
		if approved {
			return "Approve with feedback"
		}
		return "Reject with feedback"
	}
	if approved {
		return "Approve"
	}
	return "Reject"
}

func (m Model) renderApprovalFooter(surface surfaceRect) string {
	if m.focusRegion != FocusRegionActionPanel {
		return renderFooterHintBar(surface.Width, m.detailHint("Esc back"))
	}
	action := "Enter submit"
	if m.approval.choice == approvalRowFeedback {
		action = "Enter newline"
	}
	return renderFooterHintBar(surface.Width, m.detailHint(joinHintParts("↑↓ select", action, "Esc back")))
}

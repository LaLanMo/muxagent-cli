package tasktui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
)

func (m Model) handleApprovalKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.submittingInput {
		return m, nil
	}
	if m.focusRegion == FocusRegionComposer {
		switch {
		case keyMatches(msg, m.keys.back):
			m.focusRegion = FocusRegionActionPanel
			m.syncComponents()
			return m, m.syncInputFocus()
		default:
			cmd := m.editor.Update(msg)
			m.syncComponents()
			return m, cmd
		}
	}
	switch {
	case keyMatches(msg, m.keys.back):
		return m, m.returnToTaskList()
	case keyMatches(msg, m.keys.up):
		m.approval.choice = moveSelection(m.approval.choice, -1, 2)
		m.syncComponents()
		return m, m.syncInputFocus()
	case keyMatches(msg, m.keys.down):
		m.approval.choice = moveSelection(m.approval.choice, 1, 2)
		m.syncComponents()
		return m, m.syncInputFocus()
	case keyMatches(msg, m.keys.confirm):
		if m.currentInput == nil || m.current == nil {
			return m, nil
		}
		payload := map[string]interface{}{
			"approved": m.approval.choice == 0,
		}
		if m.approval.choice == 1 {
			feedback := strings.TrimSpace(m.editor.Value())
			if feedback != "" {
				payload["feedback"] = feedback
			}
		}
		return m.submitCurrentInput(payload)
	default:
		return m, nil
	}
}

func (m Model) renderApprovalFooter(surface surfaceRect) string {
	return renderFooterHintBar(surface.Width, m.detailHint(joinHintParts("↑↓ select", "Enter confirm", "Esc back")))
}

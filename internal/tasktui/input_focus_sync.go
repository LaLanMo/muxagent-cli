package tasktui

import tea "charm.land/bubbletea/v2"

func (m Model) editorNewlineMode() EditorNewlineMode {
	switch m.screen {
	case ScreenNewTask, ScreenComplete, ScreenApproval, ScreenClarification:
		return EditorNewlineModeCtrlJ
	default:
		return EditorNewlineModeEnter
	}
}

func (m *Model) syncInputFocus() tea.Cmd {
	m.editor.SetNewlineMode(m.editorNewlineMode())
	var cmds []tea.Cmd
	if m.dialog != nil {
		m.editor.Blur()
		return nil
	}
	if m.shouldFocusActiveEditor() && m.activeEditorSlot() != "" {
		cmds = append(cmds, m.editor.Focus())
	} else {
		m.editor.Blur()
	}
	return tea.Batch(cmds...)
}

func (m Model) shouldFocusActiveEditor() bool {
	if m.activeEditorSlot() == "" {
		return false
	}
	switch m.screen {
	case ScreenApproval:
		return m.focusRegion == FocusRegionActionPanel && m.approval.choice == approvalRowFeedback
	case ScreenClarification:
		question := m.currentClarificationQuestion()
		if question == nil {
			return false
		}
		return m.focusRegion == FocusRegionChoices && m.clarification.option == clarificationOtherRowIndex(*question)
	case ScreenComplete:
		return m.followUpInputActive()
	default:
		return m.focusRegion == FocusRegionFormEditor
	}
}

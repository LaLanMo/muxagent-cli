package tasktui

import "github.com/LaLanMo/muxagent-cli/internal/taskdomain"

func (m Model) activeEditorSlot() string {
	switch m.screen {
	case ScreenNewTask:
		return editorSlotNewTask
	case ScreenApproval:
		if m.approval.choice == 1 {
			return approvalEditorSlot(m.currentInput)
		}
	case ScreenClarification:
		if m.currentInput != nil && len(m.currentInput.Questions) > 0 {
			return clarificationEditorSlot(m.currentInput, m.clarification.question)
		}
	}
	return ""
}

func (m Model) activeEditorPlaceholder() string {
	switch m.screen {
	case ScreenNewTask:
		return "Describe your task..."
	case ScreenApproval:
		return "Explain what needs to change…"
	case ScreenClarification:
		return "Write your own answer…"
	default:
		return "Type here..."
	}
}

func (m Model) activeEditorSeedValue(slot string) string {
	switch slot {
	case editorSlotNewTask:
		return ""
	default:
		if m.screen == ScreenClarification {
			question := m.currentClarificationQuestion()
			if question != nil {
				return clarificationCustomAnswer(*question, clarificationAnswerAt(m.clarification.answers, m.clarification.question))
			}
		}
		return ""
	}
}

func (m *Model) syncEditorState() {
	slot := m.activeEditorSlot()
	if slot == "" {
		m.editor.SetSlot("")
		m.editor.SetPlaceholder("Type here...")
		return
	}
	m.editor.EnsureSlot(slot, m.activeEditorSeedValue(slot))
	m.editor.SetSlot(slot)
	m.editor.SetPlaceholder(m.activeEditorPlaceholder())
}

func (m Model) currentClarificationQuestion() *taskdomain.ClarificationQuestion {
	if m.currentInput == nil || m.clarification.question < 0 || m.clarification.question >= len(m.currentInput.Questions) {
		return nil
	}
	return &m.currentInput.Questions[m.clarification.question]
}

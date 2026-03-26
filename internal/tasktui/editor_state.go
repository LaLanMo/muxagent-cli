package tasktui

import "github.com/LaLanMo/muxagent-cli/internal/taskdomain"

func (m Model) activeEditorSlot() string {
	return m.currentEditorBindingSpec().Slot
}

func (m Model) activeEditorPlaceholder() string {
	spec := m.currentEditorBindingSpec()
	if spec.Placeholder == "" {
		return "Type here..."
	}
	return spec.Placeholder
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
	spec := m.currentEditorBindingSpec()
	if spec.Slot == "" {
		m.editor.SetSlot("")
		m.editor.SetPlaceholder("Type here...")
		return
	}
	m.editor.EnsureSlot(spec.Slot, m.activeEditorSeedValue(spec.Slot))
	m.editor.SetSlot(spec.Slot)
	m.editor.SetPlaceholder(spec.Placeholder)
}

func (m Model) currentClarificationQuestion() *taskdomain.ClarificationQuestion {
	if m.currentInput == nil || m.clarification.question < 0 || m.clarification.question >= len(m.currentInput.Questions) {
		return nil
	}
	return &m.currentInput.Questions[m.clarification.question]
}

package tasktui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/LaLanMo/muxagent-cli/internal/taskdomain"
)

func (m Model) handleClarificationKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.currentInput == nil || len(m.currentInput.Questions) == 0 {
		return m, nil
	}
	if m.submittingInput {
		return m, nil
	}
	question := m.currentInput.Questions[m.clarification.question]
	if m.focusRegion == FocusRegionComposer {
		switch {
		case keyMatches(msg, m.keys.back):
			m.focusRegion = FocusRegionChoices
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
	case m.focusRegion == FocusRegionChoices && keyMatches(msg, m.keys.up):
		m.clarification.option = moveSelection(m.clarification.option, -1, clarificationOptionCount(question))
		m.syncComponents()
		return m, m.syncInputFocus()
	case m.focusRegion == FocusRegionChoices && keyMatches(msg, m.keys.down):
		m.clarification.option = moveSelection(m.clarification.option, 1, clarificationOptionCount(question))
		m.syncComponents()
		return m, m.syncInputFocus()
	case m.focusRegion == FocusRegionComposer && keyMatches(msg, m.keys.confirm):
		cmd := m.editor.Update(msg)
		m.syncComponents()
		return m, cmd
	case m.focusRegion == FocusRegionChoices && keyMatches(msg, m.keys.confirm):
		if question.MultiSelect {
			answer := question.Options[m.clarification.option].Label
			m.clarification.answers = toggleClarificationMultiSelectAnswer(m.clarification.answers, m.clarification.question, answer)
			m.syncComponents()
			return m, nil
		}
		answer := question.Options[m.clarification.option].Label
		m.clarification.answers = appendOrReplaceAnswer(m.clarification.answers, m.clarification.question, answer)
		m.focusRegion = FocusRegionActionPanel
		m.syncComponents()
		return m, m.syncInputFocus()
	case m.focusRegion == FocusRegionActionPanel && (keyMatches(msg, m.keys.up) || keyMatches(msg, m.keys.down)):
		m.focusRegion = FocusRegionChoices
		m.syncComponents()
		return m, m.syncInputFocus()
	case m.focusRegion == FocusRegionActionPanel && keyMatches(msg, m.keys.confirm):
		return m.advanceClarificationOrSubmit()
	default:
		return m, nil
	}
}

func (m Model) renderClarificationFooter(surface surfaceRect) string {
	return m.renderClarificationFooterForLayout(surface, m.currentArtifactLayoutMode())
}

func (m Model) renderClarificationFooterForLayout(surface surfaceRect, mode artifactLayoutMode) string {
	width := surface.Width
	if m.currentInput == nil || len(m.currentInput.Questions) == 0 {
		return m.renderStatsFooter(surface, "", "", "Esc back")
	}
	question := m.currentInput.Questions[m.clarification.question]
	switch m.focusRegion {
	case FocusRegionChoices:
		action := "Enter choose"
		if question.MultiSelect {
			action = "Enter toggle"
		}
		return renderFooterHintBar(width, m.detailHintForLayout(joinHintParts("↑↓ select", action, "Esc back"), mode))
	case FocusRegionComposer:
		return renderFooterHintBar(width, m.detailHintForLayout("Esc choices", mode))
	case FocusRegionActionPanel:
		return renderFooterHintBar(width, m.detailHintForLayout(joinHintParts("Enter continue", "Esc back"), mode))
	default:
		return renderFooterHintBar(width, m.detailHintForLayout("Esc back", mode))
	}
}

func (m Model) advanceClarificationOrSubmit() (tea.Model, tea.Cmd) {
	if m.currentInput == nil || m.current == nil {
		return m, nil
	}
	question := m.currentInput.Questions[m.clarification.question]
	resolved, ok := resolveClarificationAnswer(question, clarificationAnswerAt(m.clarification.answers, m.clarification.question), m.editor.Value())
	if !ok {
		return m, nil
	}
	m.clarification.answers = appendOrReplaceAnswer(m.clarification.answers, m.clarification.question, resolved)
	m.clarification.other = false
	m.clarification.option = 0
	m.focusRegion = FocusRegionChoices
	if m.clarification.question < len(m.currentInput.Questions)-1 {
		m.clarification.question++
		m.syncComponents()
		return m, m.syncInputFocus()
	}
	return m.submitCurrentInput(buildClarificationPayload(m.clarification.answers))
}

func (m Model) canAdvanceClarification(question taskdomain.ClarificationQuestion) bool {
	_, ok := resolveClarificationAnswer(question, clarificationAnswerAt(m.clarification.answers, m.clarification.question), m.editor.Value())
	return ok
}

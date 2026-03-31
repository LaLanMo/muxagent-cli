package tasktui

import (
	"strings"

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
	switch {
	case keyMatches(msg, m.keys.back):
		return m, m.returnToTaskList()
	case m.focusRegion != FocusRegionChoices:
		return m, nil
	case clarificationOtherRowActive(m, question) && (keyMatches(msg, m.keys.left) || keyMatches(msg, m.keys.right) || keyMatches(msg, m.keys.confirm)):
		if !question.MultiSelect {
			m.setClarificationOtherSelected(m.clarification.question, true)
		}
		cmd := m.editor.Update(msg)
		m.syncComponents()
		return m, cmd
	case keyMatches(msg, m.keys.up):
		m.selectClarificationHeaderQuestion()
		m.errorText = ""
		m.clarification.option = moveSelection(m.clarification.option, -1, m.clarificationRowCount(question))
		m.syncComponents()
		return m, m.syncInputFocus()
	case keyMatches(msg, m.keys.down):
		m.selectClarificationHeaderQuestion()
		m.errorText = ""
		m.clarification.option = moveSelection(m.clarification.option, 1, m.clarificationRowCount(question))
		m.syncComponents()
		return m, m.syncInputFocus()
	case keyMatches(msg, m.keys.prevQuestion):
		return m.moveClarificationHeaderSelection(-1)
	case keyMatches(msg, m.keys.nextQuestion):
		return m.moveClarificationHeaderSelection(1)
	case keyMatches(msg, m.keys.left):
		return m.moveClarificationHeaderSelection(-1)
	case keyMatches(msg, m.keys.right):
		return m.moveClarificationHeaderSelection(1)
	case m.clarificationSubmitSelected() && keyMatches(msg, m.keys.confirm):
		return m.submitClarificationIfComplete()
	case m.clarification.option < len(question.Options) && clarificationToggleKey(msg):
		m.selectClarificationHeaderQuestion()
		m.errorText = ""
		answer := question.Options[m.clarification.option].Label
		if question.MultiSelect {
			m.clarification.answers = toggleClarificationMultiSelectAnswer(m.clarification.answers, m.clarification.question, answer)
			m.syncComponents()
			return m, nil
		}
		m.clarification.answers = appendOrReplaceAnswer(m.clarification.answers, m.clarification.question, answer)
		m.setClarificationOtherSelected(m.clarification.question, false)
		return m.handleClarificationSingleSelectChoice()
	case !m.clarificationHasQuestionNavigator() && m.clarification.option == clarificationContinueRowIndex(question) && keyMatches(msg, m.keys.confirm):
		return m.submitClarificationIfComplete()
	default:
		if m.clarification.option != clarificationOtherRowIndex(question) {
			return m, nil
		}
		if !question.MultiSelect {
			m.setClarificationOtherSelected(m.clarification.question, true)
		}
		m.selectClarificationHeaderQuestion()
		m.errorText = ""
		cmd := m.editor.Update(msg)
		m.syncComponents()
		return m, cmd
	}
}

func clarificationToggleKey(msg tea.KeyPressMsg) bool {
	if msg.Text == " " || msg.Code == ' ' {
		return true
	}
	return msg.Code == tea.KeyEnter
}

func (m Model) renderClarificationFooter(surface surfaceRect) string {
	width := surface.Width
	if m.currentInput == nil || len(m.currentInput.Questions) == 0 {
		return m.renderStatsFooter(surface, "", "", "Esc back")
	}
	if m.focusRegion != FocusRegionChoices {
		return renderFooterHintBar(width, m.detailHint("Esc back"))
	}
	question := m.currentInput.Questions[m.clarification.question]
	action := "Enter continue"
	switch {
	case m.clarificationSubmitSelected():
		action = "Enter submit"
	case m.clarification.option < len(question.Options):
		if question.MultiSelect {
			action = "Enter/Space toggle"
		} else {
			action = "Enter choose"
		}
	case m.clarification.option == clarificationOtherRowIndex(question):
		action = "Enter newline"
	}
	hints := []string{"↑↓ select"}
	if m.clarificationHasQuestionNavigator() {
		if clarificationOtherRowActive(m, question) {
			hints = append(hints, "←→ cursor", "Ctrl+P/N questions")
		} else {
			hints = append(hints, "←→ questions")
		}
	}
	hints = append(hints, action, "Esc back")
	return renderFooterHintBar(width, m.detailHint(joinHintParts(hints...)))
}

func (m Model) handleClarificationSingleSelectChoice() (tea.Model, tea.Cmd) {
	if m.currentInput == nil {
		return m, nil
	}
	if m.clarificationHasQuestionNavigator() {
		if m.clarification.question < len(m.currentInput.Questions)-1 {
			return m.navigateClarificationQuestion(m.clarification.question + 1)
		}
		m.syncComponents()
		return m, nil
	}
	return m.submitClarificationIfComplete()
}

func (m Model) submitClarificationIfComplete() (tea.Model, tea.Cmd) {
	if m.currentInput == nil || m.current == nil {
		return m, nil
	}
	answers, ok := m.buildResolvedClarificationAnswers()
	if !ok {
		return m.focusFirstIncompleteClarification("Answer every question before submitting.")
	}
	m.clarification.answers = answers
	m.errorText = ""
	return m.submitCurrentInput(buildClarificationPayload(answers))
}

func (m Model) canAdvanceClarification(question taskdomain.ClarificationQuestion) bool {
	_ = question
	_, ok := m.resolveClarificationAnswerAt(m.clarification.question)
	return ok
}

func (m Model) resolveCurrentClarificationAnswer(question taskdomain.ClarificationQuestion) (interface{}, bool) {
	_ = question
	return m.resolveClarificationAnswerAt(m.clarification.question)
}

func (m Model) resolveClarificationAnswerAt(questionIndex int) (interface{}, bool) {
	if m.currentInput == nil || questionIndex < 0 || questionIndex >= len(m.currentInput.Questions) {
		return nil, false
	}
	question := m.currentInput.Questions[questionIndex]
	answer := clarificationAnswerAt(m.clarification.answers, questionIndex)
	otherText := strings.TrimSpace(m.clarificationOtherText(questionIndex, question))
	if question.MultiSelect {
		return resolveClarificationAnswer(question, answer, otherText)
	}
	if m.clarificationOtherSelectedAt(questionIndex, question) {
		if otherText == "" {
			return nil, false
		}
		return otherText, true
	}
	if selected := clarificationSelectedOption(question, answer); selected != "" {
		return selected, true
	}
	return nil, false
}

func (m Model) buildResolvedClarificationAnswers() ([]taskdomain.ClarificationAnswer, bool) {
	if m.currentInput == nil {
		return nil, false
	}
	answers := make([]taskdomain.ClarificationAnswer, 0, len(m.currentInput.Questions))
	for questionIndex := range m.currentInput.Questions {
		resolved, ok := m.resolveClarificationAnswerAt(questionIndex)
		if !ok {
			return nil, false
		}
		answers = append(answers, taskdomain.ClarificationAnswer{Selected: resolved})
	}
	return answers, true
}

func (m Model) canSubmitClarification() bool {
	return m.firstIncompleteClarificationQuestion() == -1
}

func (m Model) firstIncompleteClarificationQuestion() int {
	if m.currentInput == nil {
		return -1
	}
	for questionIndex := range m.currentInput.Questions {
		if _, ok := m.resolveClarificationAnswerAt(questionIndex); !ok {
			return questionIndex
		}
	}
	return -1
}

func (m Model) focusFirstIncompleteClarification(message string) (tea.Model, tea.Cmd) {
	index := m.firstIncompleteClarificationQuestion()
	if index >= 0 {
		m.selectClarificationQuestion(index)
	}
	m.errorText = message
	m.syncComponents()
	return m, m.syncInputFocus()
}

func (m Model) navigateClarificationQuestion(index int) (tea.Model, tea.Cmd) {
	if m.currentInput == nil || len(m.currentInput.Questions) == 0 {
		return m, nil
	}
	m.selectClarificationQuestion(index)
	m.errorText = ""
	m.syncComponents()
	return m, m.syncInputFocus()
}

func (m *Model) selectClarificationQuestion(index int) {
	if m.currentInput == nil || len(m.currentInput.Questions) == 0 {
		m.clarification.question = 0
		m.clarification.option = 0
		m.clarification.headerSelection = 0
		return
	}
	index = clamp(index, 0, len(m.currentInput.Questions)-1)
	m.clarification.question = index
	m.clarification.option = m.defaultClarificationOption(index)
	m.clarification.headerSelection = index
}

func (m Model) moveClarificationHeaderSelection(delta int) (tea.Model, tea.Cmd) {
	if !m.clarificationHasQuestionNavigator() {
		return m, nil
	}
	maxIndex := m.clarificationSubmitHeaderIndex()
	next := clamp(m.clarification.headerSelection+delta, 0, maxIndex)
	if next == m.clarification.headerSelection {
		return m, nil
	}
	m.clarification.headerSelection = next
	if next < len(m.currentInput.Questions) {
		m.clarification.question = next
		m.clarification.option = m.defaultClarificationOption(next)
	}
	m.errorText = ""
	m.syncComponents()
	return m, m.syncInputFocus()
}

func (m Model) clarificationHasQuestionNavigator() bool {
	return m.currentInput != nil && len(m.currentInput.Questions) > 1
}

func (m Model) clarificationSubmitHeaderIndex() int {
	if m.currentInput == nil {
		return 0
	}
	return len(m.currentInput.Questions)
}

func (m Model) clarificationSubmitSelected() bool {
	return m.clarificationHasQuestionNavigator() && m.clarification.headerSelection == m.clarificationSubmitHeaderIndex()
}

func (m *Model) selectClarificationHeaderQuestion() {
	if m.currentInput == nil || len(m.currentInput.Questions) == 0 {
		m.clarification.headerSelection = 0
		return
	}
	m.clarification.headerSelection = clamp(m.clarification.question, 0, len(m.currentInput.Questions)-1)
}

func (m Model) clarificationRowCount(question taskdomain.ClarificationQuestion) int {
	count := len(question.Options) + 1
	if !m.clarificationHasQuestionNavigator() {
		count++
	}
	return count
}

func clarificationOtherRowIndex(question taskdomain.ClarificationQuestion) int {
	return len(question.Options)
}

func clarificationContinueRowIndex(question taskdomain.ClarificationQuestion) int {
	return len(question.Options) + 1
}

func clarificationOtherRowActive(m Model, question taskdomain.ClarificationQuestion) bool {
	return m.focusRegion == FocusRegionChoices && m.clarification.option == clarificationOtherRowIndex(question)
}

func (m *Model) setClarificationOtherSelected(questionIndex int, selected bool) {
	if m.clarification.other == nil {
		m.clarification.other = map[int]bool{}
	}
	if !selected {
		delete(m.clarification.other, questionIndex)
		return
	}
	m.clarification.other[questionIndex] = true
}

func (m Model) clarificationOtherText(questionIndex int, question taskdomain.ClarificationQuestion) string {
	if m.currentInput != nil {
		slot := clarificationEditorSlot(m.currentInput, questionIndex)
		if value, ok := m.editor.DraftValue(slot); ok {
			return value
		}
	}
	return clarificationCustomAnswer(question, clarificationAnswerAt(m.clarification.answers, questionIndex))
}

func (m Model) clarificationOtherSelected(question taskdomain.ClarificationQuestion) bool {
	return m.clarificationOtherSelectedAt(m.clarification.question, question)
}

func (m Model) clarificationOtherSelectedAt(questionIndex int, question taskdomain.ClarificationQuestion) bool {
	if question.MultiSelect {
		return strings.TrimSpace(m.clarificationOtherText(questionIndex, question)) != ""
	}
	if m.clarification.other != nil && m.clarification.other[questionIndex] {
		return true
	}
	return clarificationCustomAnswer(question, clarificationAnswerAt(m.clarification.answers, questionIndex)) != ""
}

func (m Model) clarificationQuestionAnswered(questionIndex int) bool {
	_, ok := m.resolveClarificationAnswerAt(questionIndex)
	return ok
}

func (m Model) defaultClarificationOption(questionIndex int) int {
	if m.currentInput == nil || questionIndex < 0 || questionIndex >= len(m.currentInput.Questions) {
		return 0
	}
	question := m.currentInput.Questions[questionIndex]
	if m.clarificationOtherSelectedAt(questionIndex, question) {
		return clarificationOtherRowIndex(question)
	}
	answer := clarificationAnswerAt(m.clarification.answers, questionIndex)
	if question.MultiSelect {
		for index, option := range question.Options {
			if clarificationAnswerContains(m.clarification.answers, questionIndex, option.Label) {
				return index
			}
		}
	}
	selected := clarificationSelectedOption(question, answer)
	for index, option := range question.Options {
		if option.Label == selected {
			return index
		}
	}
	return 0
}

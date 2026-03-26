package tasktui

import (
	"strings"

	"github.com/LaLanMo/muxagent-cli/internal/taskdomain"
)

func appendOrReplaceAnswer(answers []taskdomain.ClarificationAnswer, index int, selected interface{}) []taskdomain.ClarificationAnswer {
	for len(answers) <= index {
		answers = append(answers, taskdomain.ClarificationAnswer{})
	}
	answers[index] = taskdomain.ClarificationAnswer{Selected: selected}
	return answers
}

func clarificationAnswerAt(answers []taskdomain.ClarificationAnswer, index int) taskdomain.ClarificationAnswer {
	if index < 0 || index >= len(answers) {
		return taskdomain.ClarificationAnswer{}
	}
	return answers[index]
}

func clarificationAnswerValues(answer taskdomain.ClarificationAnswer) []string {
	switch selected := answer.Selected.(type) {
	case string:
		text := strings.TrimSpace(selected)
		if text == "" {
			return nil
		}
		return []string{text}
	case []string:
		values := make([]string, 0, len(selected))
		for _, item := range selected {
			item = strings.TrimSpace(item)
			if item == "" {
				continue
			}
			values = append(values, item)
		}
		return values
	case []interface{}:
		values := make([]string, 0, len(selected))
		for _, item := range selected {
			text, ok := item.(string)
			if !ok {
				continue
			}
			text = strings.TrimSpace(text)
			if text == "" {
				continue
			}
			values = append(values, text)
		}
		return values
	default:
		return nil
	}
}

func clarificationOptionCount(question taskdomain.ClarificationQuestion) int {
	return len(question.Options)
}

func clarificationAnswerContains(answers []taskdomain.ClarificationAnswer, index int, value string) bool {
	for _, candidate := range clarificationAnswerValues(clarificationAnswerAt(answers, index)) {
		if candidate == value {
			return true
		}
	}
	return false
}

func clarificationCustomAnswer(question taskdomain.ClarificationQuestion, answer taskdomain.ClarificationAnswer) string {
	for _, candidate := range clarificationAnswerValues(answer) {
		if !clarificationQuestionHasOption(question, candidate) {
			return candidate
		}
	}
	return ""
}

func clarificationQuestionHasOption(question taskdomain.ClarificationQuestion, value string) bool {
	for _, option := range question.Options {
		if option.Label == value {
			return true
		}
	}
	return false
}

func clarificationSelectedOption(question taskdomain.ClarificationQuestion, answer taskdomain.ClarificationAnswer) string {
	for _, candidate := range clarificationAnswerValues(answer) {
		if clarificationQuestionHasOption(question, candidate) {
			return candidate
		}
	}
	return ""
}

func toggleClarificationMultiSelectAnswer(answers []taskdomain.ClarificationAnswer, index int, value string) []taskdomain.ClarificationAnswer {
	current := clarificationAnswerValues(clarificationAnswerAt(answers, index))
	next := make([]string, 0, len(current)+1)
	found := false
	for _, candidate := range current {
		if candidate == value {
			found = true
			continue
		}
		next = append(next, candidate)
	}
	if !found {
		next = append(next, value)
	}
	return appendOrReplaceAnswer(answers, index, next)
}

func setClarificationMultiSelectOtherAnswer(answers []taskdomain.ClarificationAnswer, index int, question taskdomain.ClarificationQuestion, value string) []taskdomain.ClarificationAnswer {
	value = strings.TrimSpace(value)
	current := clarificationAnswerValues(clarificationAnswerAt(answers, index))
	next := make([]string, 0, len(current)+1)
	for _, candidate := range current {
		if clarificationQuestionHasOption(question, candidate) {
			next = append(next, candidate)
		}
	}
	if value != "" {
		next = append(next, value)
	}
	return appendOrReplaceAnswer(answers, index, next)
}

func resolveClarificationAnswer(question taskdomain.ClarificationQuestion, answer taskdomain.ClarificationAnswer, otherText string) (interface{}, bool) {
	otherText = strings.TrimSpace(otherText)
	if question.MultiSelect {
		values := make([]string, 0, len(question.Options)+1)
		for _, candidate := range clarificationAnswerValues(answer) {
			if clarificationQuestionHasOption(question, candidate) {
				values = append(values, candidate)
			}
		}
		if otherText != "" {
			values = append(values, otherText)
		}
		if len(values) == 0 {
			return nil, false
		}
		return values, true
	}

	if otherText != "" {
		return otherText, true
	}
	if selected := clarificationSelectedOption(question, answer); selected != "" {
		return selected, true
	}
	return nil, false
}

func buildClarificationPayload(answers []taskdomain.ClarificationAnswer) map[string]interface{} {
	payloadAnswers := make([]interface{}, 0, len(answers))
	for _, answer := range answers {
		payloadAnswers = append(payloadAnswers, map[string]interface{}{"selected": answer.Selected})
	}
	return map[string]interface{}{
		"answers": payloadAnswers,
	}
}

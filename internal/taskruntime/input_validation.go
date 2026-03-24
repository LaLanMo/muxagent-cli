package taskruntime

import (
	"errors"
	"fmt"
	"strings"

	"github.com/LaLanMo/muxagent-cli/internal/taskdomain"
)

func parseClarificationResponse(request taskdomain.ClarificationRequest, payload map[string]interface{}) (*taskdomain.ClarificationResponse, error) {
	rawAnswers, ok := payload["answers"].([]interface{})
	if !ok {
		return nil, errors.New("clarification payload must contain answers array")
	}
	if len(rawAnswers) != len(request.Questions) {
		return nil, fmt.Errorf("clarification payload must contain %d answers", len(request.Questions))
	}

	response := &taskdomain.ClarificationResponse{Answers: make([]taskdomain.ClarificationAnswer, 0, len(rawAnswers))}
	for i, rawAnswer := range rawAnswers {
		answerMap, ok := rawAnswer.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("clarification answer %d must be an object", i)
		}
		selected, ok := answerMap["selected"]
		if !ok {
			return nil, fmt.Errorf("clarification answer %d must contain selected", i)
		}
		normalized, err := normalizeClarificationAnswer(request.Questions[i], selected)
		if err != nil {
			return nil, fmt.Errorf("clarification answer %d: %w", i, err)
		}
		response.Answers = append(response.Answers, taskdomain.ClarificationAnswer{
			Selected: normalized,
		})
	}
	return response, nil
}

func normalizeClarificationAnswer(question taskdomain.ClarificationQuestion, selected interface{}) (interface{}, error) {
	if question.MultiSelect {
		values, err := selectedStringSlice(selected)
		if err != nil {
			return nil, err
		}
		if len(values) == 0 {
			return nil, errors.New("must select at least one option")
		}
		out := make([]interface{}, 0, len(values))
		for _, value := range values {
			out = append(out, value)
		}
		return out, nil
	}

	text, ok := selected.(string)
	if !ok {
		return nil, errors.New("must select a single string value")
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, errors.New("must not be empty")
	}
	return text, nil
}

func selectedStringSlice(selected interface{}) ([]string, error) {
	switch values := selected.(type) {
	case []interface{}:
		out := make([]string, 0, len(values))
		for _, value := range values {
			text, ok := value.(string)
			if !ok {
				return nil, errors.New("multi-select answers must be arrays of strings")
			}
			text = strings.TrimSpace(text)
			if text == "" {
				return nil, errors.New("multi-select answers must not contain empty strings")
			}
			out = append(out, text)
		}
		return out, nil
	case []string:
		out := make([]string, 0, len(values))
		for _, value := range values {
			value = strings.TrimSpace(value)
			if value == "" {
				return nil, errors.New("multi-select answers must not contain empty strings")
			}
			out = append(out, value)
		}
		return out, nil
	default:
		return nil, errors.New("must be an array for multi-select questions")
	}
}

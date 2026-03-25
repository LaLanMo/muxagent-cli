package taskexecutor

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/LaLanMo/muxagent-cli/internal/taskconfig"
	"github.com/LaLanMo/muxagent-cli/internal/taskdomain"
)

func BuildOutputSchema(req Request) map[string]interface{} {
	resultSchema := schemaMap(req.ResultSchema)
	if !ClarificationConfigured(req) {
		return map[string]interface{}{
			"type":                 "object",
			"additionalProperties": false,
			"required":             []string{"kind", "result"},
			"properties": map[string]interface{}{
				"kind": map[string]interface{}{
					"type": "string",
					"enum": []string{string(ResultKindResult)},
				},
				"result": resultSchema,
			},
		}
	}
	if !ClarificationRemaining(req) {
		return map[string]interface{}{
			"type":                 "object",
			"additionalProperties": false,
			"required":             []string{"kind", "result", "clarification"},
			"properties": map[string]interface{}{
				"kind": map[string]interface{}{
					"type": "string",
					"enum": []string{string(ResultKindResult)},
				},
				"result": resultSchema,
				"clarification": map[string]interface{}{
					"type": "null",
				},
			},
		}
	}
	return map[string]interface{}{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []string{"kind", "result", "clarification"},
		"properties": map[string]interface{}{
			"kind": map[string]interface{}{
				"type": "string",
				"enum": []string{
					string(ResultKindResult),
					string(ResultKindClarification),
				},
			},
			"result":        nullableObjectSchema(resultSchema),
			"clarification": nullableObjectSchema(buildClarificationEnvelopeSchema(req.ClarificationConfig)),
		},
	}
}

func WriteSchema(path string, schema interface{}) error {
	data, err := json.MarshalIndent(schema, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func ClarificationConfigured(req Request) bool {
	return req.NodeDefinition.MaxClarificationRounds > 0
}

func ClarificationRemaining(req Request) bool {
	return ClarificationConfigured(req) && len(req.NodeRun.Clarifications) < req.NodeDefinition.MaxClarificationRounds
}

func AppendOutputContract(req Request) string {
	var builder strings.Builder
	builder.WriteString(req.Prompt)
	builder.WriteString("\n\nOutput contract:\n")
	builder.WriteString("- Return exactly one JSON object matching the provided schema.\n")
	if !ClarificationConfigured(req) {
		builder.WriteString("- Clarification is disabled for this node. You must return {\"kind\":\"result\",\"result\":<payload matching the node result schema>}.\n")
		builder.WriteString("- Do not return any clarification payload.\n")
		builder.WriteString("- Do not return bare result fields or bare questions at the top level.\n")
		return builder.String()
	}
	if ClarificationRemaining(req) {
		builder.WriteString("- When the node is complete, return {\"kind\":\"result\",\"result\":<payload matching the node result schema>,\"clarification\":null}.\n")
		builder.WriteString("- For kind=result, set clarification to null.\n")
		builder.WriteString("- If you need user clarification before you can finish, return {\"kind\":\"clarification\",\"result\":null,\"clarification\":{\"questions\":[...]}}.\n")
		builder.WriteString("- Each clarification question must include question, why_it_matters, options, and multi_select.\n")
	} else {
		builder.WriteString("- Clarification has reached its maximum rounds for this node.\n")
		builder.WriteString("- You must return {\"kind\":\"result\",\"result\":<payload matching the node result schema>,\"clarification\":null}.\n")
	}
	builder.WriteString("- Do not return bare result fields or bare questions at the top level.\n")
	return builder.String()
}

func ResumeTargetSessionID(req Request) string {
	if strings.TrimSpace(req.NodeRun.SessionID) == "" {
		return ""
	}
	if len(req.NodeRun.Clarifications) == 0 {
		return ""
	}
	latest := req.NodeRun.Clarifications[len(req.NodeRun.Clarifications)-1]
	if latest.Response == nil {
		return ""
	}
	return req.NodeRun.SessionID
}

func ParseOutputEnvelope(req Request, outputBytes []byte) (Result, error) {
	output, err := taskconfig.NormalizeJSONMap(outputBytes)
	if err != nil {
		return Result{}, err
	}

	kind, _ := output["kind"].(string)
	switch kind {
	case string(ResultKindClarification):
		if !ClarificationRemaining(req) {
			return Result{}, errors.New("output kind=clarification is not allowed for this node")
		}
		clarificationPayload, ok := output["clarification"].(map[string]interface{})
		if !ok {
			return Result{}, errors.New("output kind=clarification must contain clarification object")
		}
		reqBody, err := ParseClarificationRequest(clarificationPayload)
		if err != nil {
			return Result{}, err
		}
		return Result{
			Kind:          ResultKindClarification,
			Clarification: reqBody,
		}, nil
	case string(ResultKindResult):
		resultPayload, ok := output["result"].(map[string]interface{})
		if !ok {
			return Result{}, errors.New("output kind=result must contain result object")
		}
		if err := taskconfig.ValidateValue(&req.ResultSchema, resultPayload); err != nil {
			return Result{}, err
		}
		return Result{
			Kind:   ResultKindResult,
			Result: resultPayload,
		}, nil
	default:
		return Result{}, errors.New("output.json must contain kind=result or kind=clarification")
	}
}

func ParseClarificationRequest(payload map[string]interface{}) (*taskdomain.ClarificationRequest, error) {
	questionsValue, ok := payload["questions"].([]interface{})
	if !ok {
		return nil, errors.New("clarification output must contain questions array")
	}
	request := &taskdomain.ClarificationRequest{
		Questions: make([]taskdomain.ClarificationQuestion, 0, len(questionsValue)),
	}
	for _, rawQuestion := range questionsValue {
		questionMap, ok := rawQuestion.(map[string]interface{})
		if !ok {
			return nil, errors.New("clarification question must be an object")
		}
		question := taskdomain.ClarificationQuestion{
			Question:     asString(questionMap["question"]),
			WhyItMatters: asString(questionMap["why_it_matters"]),
			MultiSelect:  asBool(questionMap["multi_select"]),
		}
		options, ok := questionMap["options"].([]interface{})
		if !ok {
			return nil, errors.New("clarification question options must be an array")
		}
		for _, rawOption := range options {
			optionMap, ok := rawOption.(map[string]interface{})
			if !ok {
				return nil, errors.New("clarification option must be an object")
			}
			question.Options = append(question.Options, taskdomain.ClarificationOption{
				Label:       asString(optionMap["label"]),
				Description: asString(optionMap["description"]),
			})
		}
		request.Questions = append(request.Questions, question)
	}
	return request, nil
}

func buildClarificationEnvelopeSchema(cfg taskconfig.ClarificationConfig) map[string]interface{} {
	return map[string]interface{}{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []string{"questions"},
		"properties": map[string]interface{}{
			"questions": map[string]interface{}{
				"type":     "array",
				"maxItems": cfg.MaxQuestions,
				"items": map[string]interface{}{
					"type":                 "object",
					"additionalProperties": false,
					"required":             []string{"question", "why_it_matters", "options", "multi_select"},
					"properties": map[string]interface{}{
						"question":       map[string]interface{}{"type": "string"},
						"why_it_matters": map[string]interface{}{"type": "string"},
						"options": map[string]interface{}{
							"type":     "array",
							"minItems": cfg.MinOptionsPerQuestion,
							"maxItems": cfg.MaxOptionsPerQuestion,
							"items": map[string]interface{}{
								"type":                 "object",
								"additionalProperties": false,
								"required":             []string{"label", "description"},
								"properties": map[string]interface{}{
									"label":       map[string]interface{}{"type": "string"},
									"description": map[string]interface{}{"type": "string"},
								},
							},
						},
						"multi_select": map[string]interface{}{"type": "boolean"},
					},
				},
			},
		},
	}
}

func asString(value interface{}) string {
	text, _ := value.(string)
	return text
}

func asBool(value interface{}) bool {
	flag, _ := value.(bool)
	return flag
}

func schemaMap(schema taskconfig.JSONSchema) map[string]interface{} {
	data, err := json.Marshal(schema)
	if err != nil {
		panic(fmt.Sprintf("marshal schema: %v", err))
	}
	var out map[string]interface{}
	if err := json.Unmarshal(data, &out); err != nil {
		panic(fmt.Sprintf("unmarshal schema map: %v", err))
	}
	return out
}

func nullableObjectSchema(schema map[string]interface{}) map[string]interface{} {
	cloned := cloneMap(schema)
	cloned["type"] = []string{"object", "null"}
	return cloned
}

func cloneMap(value map[string]interface{}) map[string]interface{} {
	data, err := json.Marshal(value)
	if err != nil {
		panic(fmt.Sprintf("clone map marshal: %v", err))
	}
	var out map[string]interface{}
	if err := json.Unmarshal(data, &out); err != nil {
		panic(fmt.Sprintf("clone map unmarshal: %v", err))
	}
	return out
}

package codexexec

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/LaLanMo/muxagent-cli/internal/taskconfig"
	"github.com/LaLanMo/muxagent-cli/internal/taskdomain"
	"github.com/LaLanMo/muxagent-cli/internal/taskexecutor"
)

type Executor struct {
	BinaryPath string
}

func New(binaryPath string) *Executor {
	if strings.TrimSpace(binaryPath) == "" {
		binaryPath = "codex"
	}
	return &Executor{BinaryPath: binaryPath}
}

func (e *Executor) Execute(ctx context.Context, req taskexecutor.Request, progress func(taskexecutor.Progress)) (taskexecutor.Result, error) {
	if err := os.MkdirAll(req.ArtifactDir, 0o755); err != nil {
		return taskexecutor.Result{}, err
	}
	if strings.TrimSpace(req.SchemaPath) == "" {
		return taskexecutor.Result{}, errors.New("schema path is required")
	}
	if err := os.MkdirAll(filepath.Dir(req.SchemaPath), 0o755); err != nil {
		return taskexecutor.Result{}, err
	}
	outputPath := filepath.Join(req.ArtifactDir, "output.json")
	outputSchema := buildOutputSchema(req)
	if err := writeSchema(req.SchemaPath, outputSchema); err != nil {
		return taskexecutor.Result{}, err
	}

	prompt := appendOutputContract(req)
	args := buildExecArgs(req, outputPath, prompt)

	cmd := exec.CommandContext(ctx, e.BinaryPath, args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return taskexecutor.Result{}, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return taskexecutor.Result{}, err
	}

	if err := cmd.Start(); err != nil {
		return taskexecutor.Result{}, err
	}

	var sessionID string
	resumeSessionID := resumeTargetSessionID(req)
	stderrLines := make(chan string, 8)
	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			stderrLines <- scanner.Text()
		}
		close(stderrLines)
	}()

	scanner := bufio.NewScanner(stdout)
	var structuredError string
	for scanner.Scan() {
		line := scanner.Bytes()
		progressMessage, foundSessionID, errorMessage, err := parseJSONLLine(line)
		if err != nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			return taskexecutor.Result{}, err
		}
		if foundSessionID != "" && sessionID == "" {
			sessionID = foundSessionID
		}
		if resumeSessionID != "" && foundSessionID != "" && foundSessionID != resumeSessionID {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			return taskexecutor.Result{}, fmt.Errorf("codex resume switched threads: expected %q, got %q", resumeSessionID, foundSessionID)
		}
		if errorMessage != "" {
			structuredError = errorMessage
		}
		if progress != nil && (progressMessage != "" || foundSessionID != "") {
			progress(taskexecutor.Progress{
				Message:   progressMessage,
				SessionID: foundSessionID,
			})
		}
	}
	if err := scanner.Err(); err != nil {
		return taskexecutor.Result{}, err
	}
	if err := cmd.Wait(); err != nil {
		var stderrText []string
		for line := range stderrLines {
			stderrText = append(stderrText, line)
		}
		if structuredError != "" {
			return taskexecutor.Result{}, fmt.Errorf("%w: %s", err, structuredError)
		}
		if len(stderrText) > 0 {
			return taskexecutor.Result{}, fmt.Errorf("%w: %s", err, strings.Join(stderrText, "\n"))
		}
		return taskexecutor.Result{}, err
	}

	outputBytes, err := os.ReadFile(outputPath)
	if err != nil {
		return taskexecutor.Result{}, err
	}
	output, err := taskconfig.NormalizeJSONMap(outputBytes)
	if err != nil {
		return taskexecutor.Result{}, err
	}

	kind, _ := output["kind"].(string)
	switch kind {
	case string(taskexecutor.ResultKindClarification):
		if !clarificationRemaining(req) {
			return taskexecutor.Result{}, errors.New("output kind=clarification is not allowed for this node")
		}
		clarificationPayload, ok := output["clarification"].(map[string]interface{})
		if !ok {
			return taskexecutor.Result{}, errors.New("output kind=clarification must contain clarification object")
		}
		reqBody, err := parseClarificationRequest(clarificationPayload)
		if err != nil {
			return taskexecutor.Result{}, err
		}
		return taskexecutor.Result{
			SessionID:     coalesceSessionID(sessionID, resumeSessionID),
			Kind:          taskexecutor.ResultKindClarification,
			Clarification: reqBody,
		}, nil
	case string(taskexecutor.ResultKindResult):
		resultPayload, ok := output["result"].(map[string]interface{})
		if !ok {
			return taskexecutor.Result{}, errors.New("output kind=result must contain result object")
		}
		if err := taskconfig.ValidateValue(&req.ResultSchema, resultPayload); err != nil {
			return taskexecutor.Result{}, err
		}
		return taskexecutor.Result{
			SessionID: coalesceSessionID(sessionID, resumeSessionID),
			Kind:      taskexecutor.ResultKindResult,
			Result:    resultPayload,
		}, nil
	default:
		return taskexecutor.Result{}, errors.New("output.json must contain kind=result or kind=clarification")
	}
}

func buildExecArgs(req taskexecutor.Request, outputPath, prompt string) []string {
	args := []string{
		"exec",
		"-s", "danger-full-access",
		"--json",
		"--output-schema", req.SchemaPath,
		"-o", outputPath,
		"-C", req.WorkDir,
		"--skip-git-repo-check",
	}
	if sessionID := resumeTargetSessionID(req); sessionID != "" {
		args = append(args, "resume", sessionID, prompt)
		return args
	}
	args = append(args, prompt)
	return args
}

func resumeTargetSessionID(req taskexecutor.Request) string {
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

func coalesceSessionID(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func buildOutputSchema(req taskexecutor.Request) map[string]interface{} {
	resultSchema := schemaMap(req.ResultSchema)
	if !clarificationConfigured(req) {
		return map[string]interface{}{
			"type":                 "object",
			"additionalProperties": false,
			"required":             []string{"kind", "result"},
			"properties": map[string]interface{}{
				"kind": map[string]interface{}{
					"type": "string",
					"enum": []string{string(taskexecutor.ResultKindResult)},
				},
				"result": resultSchema,
			},
		}
	}
	if !clarificationRemaining(req) {
		return map[string]interface{}{
			"type":                 "object",
			"additionalProperties": false,
			"required":             []string{"kind", "result", "clarification"},
			"properties": map[string]interface{}{
				"kind": map[string]interface{}{
					"type": "string",
					"enum": []string{string(taskexecutor.ResultKindResult)},
				},
				"result": resultSchema,
				"clarification": map[string]interface{}{
					"type": "null",
				},
			},
		}
	}
	properties := map[string]interface{}{
		"kind": map[string]interface{}{
			"type": "string",
			"enum": []string{
				string(taskexecutor.ResultKindResult),
				string(taskexecutor.ResultKindClarification),
			},
		},
		"result":        nullableObjectSchema(resultSchema),
		"clarification": nullableObjectSchema(buildClarificationEnvelopeSchema(req.ClarificationConfig)),
	}
	return map[string]interface{}{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []string{"kind", "result", "clarification"},
		"properties":           properties,
	}
}

func writeSchema(path string, schema interface{}) error {
	data, err := json.MarshalIndent(schema, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func clarificationConfigured(req taskexecutor.Request) bool {
	return req.NodeDefinition.MaxClarificationRounds > 0
}

func clarificationRemaining(req taskexecutor.Request) bool {
	return clarificationConfigured(req) && len(req.NodeRun.Clarifications) < req.NodeDefinition.MaxClarificationRounds
}

func appendOutputContract(req taskexecutor.Request) string {
	var builder strings.Builder
	builder.WriteString(req.Prompt)
	builder.WriteString("\n\nOutput contract:\n")
	builder.WriteString("- Return exactly one JSON object matching the provided schema.\n")
	if !clarificationConfigured(req) {
		builder.WriteString("- Clarification is disabled for this node. You must return {\"kind\":\"result\",\"result\":<payload matching the node result schema>}.\n")
		builder.WriteString("- Do not return any clarification payload.\n")
		builder.WriteString("- Do not return bare result fields or bare questions at the top level.\n")
		return builder.String()
	}
	if clarificationRemaining(req) {
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

func parseJSONLLine(line []byte) (message string, sessionID string, errorMessage string, err error) {
	rawLine := strings.TrimSpace(string(line))
	var payload map[string]interface{}
	if err := json.Unmarshal(line, &payload); err != nil {
		return "", "", "", fmt.Errorf("invalid codex jsonl line: %w", err)
	}
	if kind, _ := payload["type"].(string); kind != "" {
		switch {
		case strings.Contains(kind, "thread.started"):
			if id, _ := payload["thread_id"].(string); id != "" {
				sessionID = id
			}
		case kind == "error":
			errorMessage = asString(payload["message"])
		case strings.Contains(kind, "turn.failed"):
			if errorMap, ok := payload["error"].(map[string]interface{}); ok {
				errorMessage = asString(errorMap["message"])
			}
		default:
			message = rawLine
		}
	} else {
		message = rawLine
	}
	if sessionID == "" {
		if id, _ := payload["session_id"].(string); id != "" {
			sessionID = id
		}
	}
	return message, sessionID, errorMessage, nil
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

func parseClarificationRequest(payload map[string]interface{}) (*taskdomain.ClarificationRequest, error) {
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

func asString(value interface{}) string {
	text, _ := value.(string)
	return text
}

func asBool(value interface{}) bool {
	flag, _ := value.(bool)
	return flag
}

func boolPtr(value bool) *bool {
	return &value
}

func intPtr(value int) *int {
	return &value
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

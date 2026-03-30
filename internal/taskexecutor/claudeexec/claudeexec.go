package claudeexec

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/LaLanMo/muxagent-cli/internal/taskexecutor"
)

type Executor struct {
	BinaryPath string
}

func New(binaryPath string) *Executor {
	if strings.TrimSpace(binaryPath) == "" {
		binaryPath = "claude"
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
	if err := taskexecutor.WriteSchema(req.SchemaPath, taskexecutor.BuildOutputSchema(req)); err != nil {
		return taskexecutor.Result{}, err
	}
	schemaBytes, err := os.ReadFile(req.SchemaPath)
	if err != nil {
		return taskexecutor.Result{}, err
	}

	prompt := taskexecutor.AppendOutputContract(req)
	expectedSessionID, args := buildExecArgs(req, strings.TrimSpace(string(schemaBytes)), prompt)

	cmd := exec.CommandContext(ctx, e.BinaryPath, args...)
	cmd.Dir = req.WorkDir
	cmd.Env = buildChildEnv(os.Environ())

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return taskexecutor.Result{}, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return taskexecutor.Result{}, err
	}

	if err := cmd.Start(); err != nil {
		return taskexecutor.Result{}, fmt.Errorf("start claude: %w", err)
	}
	if progress != nil && expectedSessionID != "" {
		progress(taskexecutor.Progress{SessionID: expectedSessionID})
	}

	var (
		stderrBuf bytes.Buffer
		stderrWG  sync.WaitGroup
	)
	stderrWG.Add(1)
	go func() {
		defer stderrWG.Done()
		_, _ = io.Copy(&stderrBuf, stderr)
	}()

	decoder := json.NewDecoder(stdout)
	var (
		sawFinalResult            bool
		finalResult               *taskexecutor.Result
		finalOutput               []byte
		finalErr                  error
		lastStructuredOutputInput json.RawMessage
	)
	for {
		var raw json.RawMessage
		if err := decoder.Decode(&raw); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			stopCommand(cmd)
			stderrWG.Wait()
			return taskexecutor.Result{}, fmt.Errorf("invalid claude stream json: %w", err)
		}

		message, err := parseStreamMessage(raw)
		if err != nil {
			stopCommand(cmd)
			stderrWG.Wait()
			return taskexecutor.Result{}, err
		}
		if captured := extractStructuredOutputInput(raw); captured != nil {
			lastStructuredOutputInput = captured
		}
		if message.SessionID != "" && message.SessionID != expectedSessionID {
			stopCommand(cmd)
			stderrWG.Wait()
			return taskexecutor.Result{}, fmt.Errorf("claude session drift: expected %q, got %q", expectedSessionID, message.SessionID)
		}
		if message.Type != "result" {
			if progress != nil {
				update := buildProgressUpdate(raw, message)
				if update.Message != "" || update.SessionID != "" || len(update.Events) > 0 {
					progress(update)
				}
			}
			continue
		}

		sawFinalResult = true
		switch {
		case message.Subtype == "success":
			so := message.StructuredOutput
			if (len(bytes.TrimSpace(so)) == 0 || bytes.Equal(bytes.TrimSpace(so), []byte("null"))) && lastStructuredOutputInput != nil {
				so = lastStructuredOutputInput
			}
			if len(bytes.TrimSpace(so)) == 0 || bytes.Equal(bytes.TrimSpace(so), []byte("null")) {
				finalErr = errors.New("claude success result is missing structured_output")
				continue
			}
			finalOutput, err = canonicalJSON(so)
			if err != nil {
				finalErr = fmt.Errorf("invalid structured_output: %w", err)
				continue
			}
			result, err := taskexecutor.ParseOutputEnvelope(req, finalOutput)
			if err != nil {
				finalErr = fmt.Errorf("invalid envelope payload: %w", err)
				continue
			}
			finalResult = &result
		case strings.HasPrefix(message.Subtype, "error_"):
			finalErr = fmt.Errorf("claude %s: %s", message.Subtype, strings.Join(message.Errors, "; "))
		default:
			finalErr = fmt.Errorf("unsupported claude result subtype %q", message.Subtype)
		}
	}

	waitErr := cmd.Wait()
	stderrWG.Wait()
	stderrText := strings.TrimSpace(stderrBuf.String())
	if waitErr != nil {
		if finalErr != nil && stderrText != "" {
			return taskexecutor.Result{}, fmt.Errorf("%w: %s", finalErr, stderrText)
		}
		if finalErr != nil {
			return taskexecutor.Result{}, finalErr
		}
		if stderrText != "" {
			return taskexecutor.Result{}, fmt.Errorf("%w: %s", waitErr, stderrText)
		}
		return taskexecutor.Result{}, waitErr
	}
	if finalErr != nil {
		if stderrText != "" {
			return taskexecutor.Result{}, fmt.Errorf("%w: %s", finalErr, stderrText)
		}
		return taskexecutor.Result{}, finalErr
	}
	if !sawFinalResult {
		if stderrText != "" {
			return taskexecutor.Result{}, fmt.Errorf("claude stream ended without final result message: %s", stderrText)
		}
		return taskexecutor.Result{}, errors.New("claude stream ended without final result message")
	}
	if finalResult == nil {
		return taskexecutor.Result{}, errors.New("claude stream produced no valid structured output")
	}
	if err := os.WriteFile(outputPath, finalOutput, 0o644); err != nil {
		return taskexecutor.Result{}, err
	}
	finalResult.SessionID = expectedSessionID
	return *finalResult, nil
}

func buildProgressUpdate(raw json.RawMessage, message streamMessage) taskexecutor.Progress {
	events := claudeProgressEvents(raw, message.SessionID)
	if len(events) == 0 && (message.Type == "system" || message.Type == "rate_limit_event") {
		return taskexecutor.Progress{}
	}
	return taskexecutor.Progress{
		Message:   summarizeEvents(events, strings.TrimSpace(string(raw))),
		SessionID: message.SessionID,
		Events:    events,
	}
}

func buildExecArgs(req taskexecutor.Request, schemaJSON, prompt string) (string, []string) {
	expectedSessionID := taskexecutor.ResumeTargetSessionID(req)
	args := []string{
		"--print",
		"--output-format", "stream-json",
		"--verbose",
		"--json-schema", schemaJSON,
		"--setting-sources", "user,project,local",
		"--dangerously-skip-permissions",
	}
	if expectedSessionID != "" {
		args = append(args, "--resume", expectedSessionID, prompt)
		return expectedSessionID, args
	}
	expectedSessionID = req.NodeRun.ID
	args = append(args, "--session-id", expectedSessionID, prompt)
	return expectedSessionID, args
}

type streamMessage struct {
	Type             string          `json:"type"`
	Subtype          string          `json:"subtype"`
	SessionID        string          `json:"session_id"`
	StructuredOutput json.RawMessage `json:"structured_output"`
	Errors           []string        `json:"-"`
}

func parseStreamMessage(raw json.RawMessage) (streamMessage, error) {
	var payload map[string]interface{}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return streamMessage{}, fmt.Errorf("invalid claude stream json: %w", err)
	}
	message := streamMessage{
		Type:      asString(payload["type"]),
		Subtype:   asString(payload["subtype"]),
		SessionID: asString(payload["session_id"]),
	}
	if structuredOutput, ok := payload["structured_output"]; ok {
		data, err := json.Marshal(structuredOutput)
		if err != nil {
			return streamMessage{}, err
		}
		message.StructuredOutput = data
	}
	if rawErrors, ok := payload["errors"].([]interface{}); ok {
		message.Errors = make([]string, 0, len(rawErrors))
		for _, rawErr := range rawErrors {
			switch item := rawErr.(type) {
			case string:
				message.Errors = append(message.Errors, item)
			case map[string]interface{}:
				text := asString(item["message"])
				if text == "" {
					textBytes, err := json.Marshal(item)
					if err != nil {
						return streamMessage{}, err
					}
					text = string(textBytes)
				}
				message.Errors = append(message.Errors, text)
			}
		}
	}
	return message, nil
}

func buildChildEnv(base []string) []string {
	filtered := make([]string, 0, len(base))
	for _, entry := range base {
		key, _, _ := strings.Cut(entry, "=")
		if key == "CLAUDECODE" {
			continue
		}
		filtered = append(filtered, entry)
	}
	return filtered
}

func canonicalJSON(raw json.RawMessage) ([]byte, error) {
	var value interface{}
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, err
	}
	return json.Marshal(value)
}

func stopCommand(cmd *exec.Cmd) {
	if cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
	_ = cmd.Wait()
}

func asString(value interface{}) string {
	text, _ := value.(string)
	return text
}

func asMap(value interface{}) map[string]interface{} {
	item, _ := value.(map[string]interface{})
	return item
}

func asSlice(value interface{}) []interface{} {
	items, _ := value.([]interface{})
	return items
}

func asBool(value interface{}) bool {
	flag, _ := value.(bool)
	return flag
}

func asInt64(value interface{}) (int64, bool) {
	switch item := value.(type) {
	case float64:
		return int64(item), true
	case int64:
		return item, true
	case int:
		return int64(item), true
	default:
		return 0, false
	}
}

func summarizeEvents(events []taskexecutor.StreamEvent, fallback string) string {
	summaries := make([]string, 0, len(events))
	for _, event := range events {
		if summary := event.Summary(); summary != "" {
			summaries = append(summaries, summary)
		}
	}
	if len(summaries) == 0 {
		return fallback
	}
	return strings.Join(summaries, "\n")
}

func claudeProgressEvents(raw json.RawMessage, sessionID string) []taskexecutor.StreamEvent {
	var payload map[string]interface{}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return []taskexecutor.StreamEvent{{
			Kind:      taskexecutor.StreamEventKindRaw,
			SessionID: sessionID,
			Raw:       strings.TrimSpace(string(raw)),
		}}
	}
	rawLine := strings.TrimSpace(string(raw))
	switch asString(payload["type"]) {
	case "assistant":
		return claudeAssistantEvents(rawLine, sessionID, payload)
	case "user":
		return claudeUserEvents(rawLine, sessionID, payload)
	case "system", "rate_limit_event":
		return nil
	default:
		return []taskexecutor.StreamEvent{{
			Kind:      taskexecutor.StreamEventKindRaw,
			SessionID: sessionID,
			Raw:       rawLine,
		}}
	}
}

func claudeAssistantEvents(rawLine, sessionID string, payload map[string]interface{}) []taskexecutor.StreamEvent {
	messageValue := payload["message"]
	if text, ok := messageValue.(string); ok {
		text = strings.TrimSpace(text)
		if text == "" {
			return nil
		}
		return []taskexecutor.StreamEvent{{
			Kind:      taskexecutor.StreamEventKindMessage,
			SessionID: sessionID,
			Raw:       rawLine,
			Message: &taskexecutor.MessagePart{
				Role: taskexecutor.MessageRoleAssistant,
				Type: taskexecutor.MessagePartTypeText,
				Text: text,
			},
		}}
	}
	message := asMap(messageValue)
	if len(message) == 0 {
		return nil
	}
	messageID := asString(message["id"])
	var events []taskexecutor.StreamEvent
	for _, rawBlock := range asSlice(message["content"]) {
		block := asMap(rawBlock)
		switch asString(block["type"]) {
		case "thinking":
			text := strings.TrimSpace(asString(block["thinking"]))
			if text == "" {
				continue
			}
			events = append(events, taskexecutor.StreamEvent{
				Kind:      taskexecutor.StreamEventKindMessage,
				SessionID: sessionID,
				Raw:       rawLine,
				Message: &taskexecutor.MessagePart{
					MessageID: messageID,
					PartID:    asString(block["id"]),
					Role:      taskexecutor.MessageRoleAssistant,
					Type:      taskexecutor.MessagePartTypeReasoning,
					Text:      text,
				},
			})
		case "text":
			text := strings.TrimSpace(asString(block["text"]))
			if text == "" {
				continue
			}
			events = append(events, taskexecutor.StreamEvent{
				Kind:      taskexecutor.StreamEventKindMessage,
				SessionID: sessionID,
				Raw:       rawLine,
				Message: &taskexecutor.MessagePart{
					MessageID: messageID,
					PartID:    asString(block["id"]),
					Role:      taskexecutor.MessageRoleAssistant,
					Type:      taskexecutor.MessagePartTypeText,
					Text:      text,
				},
			})
		case "tool_use":
			name := asString(block["name"])
			if name == "StructuredOutput" {
				continue
			}
			input := asMap(block["input"])
			events = append(events, taskexecutor.StreamEvent{
				Kind:      taskexecutor.StreamEventKindTool,
				SessionID: sessionID,
				Raw:       rawLine,
				Tool: &taskexecutor.ToolCall{
					CallID:       asString(block["id"]),
					Name:         name,
					Kind:         claudeToolKindForName(name),
					Title:        name,
					Status:       taskexecutor.ToolStatusInProgress,
					InputSummary: claudeToolInputSummary(name, input),
					Paths:        claudeToolPaths(input),
					RawInputJSON: rawJSON(input),
				},
			})
		}
	}
	return events
}

func claudeUserEvents(rawLine, sessionID string, payload map[string]interface{}) []taskexecutor.StreamEvent {
	message := asMap(payload["message"])
	if len(message) == 0 {
		return nil
	}
	resultPayload := asMap(payload["tool_use_result"])
	var events []taskexecutor.StreamEvent
	for _, rawBlock := range asSlice(message["content"]) {
		block := asMap(rawBlock)
		if asString(block["type"]) != "tool_result" {
			continue
		}
		content := strings.TrimSpace(claudeToolResultContent(block["content"]))
		callID := asString(block["tool_use_id"])
		tool := claudeToolResultDetails(callID, content, asBool(block["is_error"]), resultPayload)
		if tool == nil {
			continue
		}
		events = append(events, taskexecutor.StreamEvent{
			Kind:      taskexecutor.StreamEventKindTool,
			SessionID: sessionID,
			Raw:       rawLine,
			Tool:      tool,
		})
	}
	return events
}

func claudeToolResultContent(value interface{}) string {
	switch item := value.(type) {
	case string:
		return item
	case []interface{}:
		parts := make([]string, 0, len(item))
		for _, entry := range item {
			if text := strings.TrimSpace(asString(entry)); text != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, "\n")
	default:
		return asString(value)
	}
}

func claudeToolKindForName(name string) taskexecutor.ToolKind {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "bash":
		return taskexecutor.ToolKindShell
	case "grep", "glob", "ls":
		return taskexecutor.ToolKindSearch
	case "read", "notebookread":
		return taskexecutor.ToolKindRead
	case "edit", "notebookedit":
		return taskexecutor.ToolKindEdit
	case "write":
		return taskexecutor.ToolKindWrite
	case "webfetch", "websearch":
		return taskexecutor.ToolKindFetch
	case "structuredoutput":
		return taskexecutor.ToolKindStructuredOutput
	default:
		return taskexecutor.ToolKindOther
	}
}

func claudeToolInputSummary(name string, input map[string]interface{}) string {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "bash":
		return firstNonEmptyString(asString(input["command"]), asString(input["description"]))
	case "grep":
		return joinNonEmpty(" ", asString(input["pattern"]), claudeToolPath(input))
	case "glob":
		return joinNonEmpty(" ", asString(input["pattern"]), claudeToolPath(input))
	case "ls":
		return claudeToolPath(input)
	case "read", "edit", "write", "notebookread", "notebookedit":
		return claudeToolPath(input)
	case "webfetch":
		return firstNonEmptyString(asString(input["url"]), asString(input["query"]))
	case "websearch":
		return firstNonEmptyString(asString(input["query"]), asString(input["url"]))
	}
	switch claudeToolKindForName(name) {
	case taskexecutor.ToolKindShell:
		return firstNonEmptyString(asString(input["command"]), asString(input["description"]))
	case taskexecutor.ToolKindRead, taskexecutor.ToolKindEdit, taskexecutor.ToolKindWrite:
		return claudeToolPath(input)
	case taskexecutor.ToolKindFetch:
		return firstNonEmptyString(asString(input["url"]), asString(input["query"]))
	case taskexecutor.ToolKindSearch:
		return firstNonEmptyString(joinNonEmpty(" ", asString(input["pattern"]), claudeToolPath(input)), claudeToolPath(input))
	default:
		if path := claudeToolPath(input); path != "" {
			return path
		}
		if url := asString(input["url"]); url != "" {
			return url
		}
		if query := asString(input["query"]); query != "" {
			return query
		}
		if pattern := asString(input["pattern"]); pattern != "" {
			return pattern
		}
		if command := asString(input["command"]); command != "" {
			return command
		}
		return strings.TrimSpace(rawJSON(input))
	}
}

func claudeToolPaths(input map[string]interface{}) []string {
	if path := claudeToolPath(input); path != "" {
		return []string{path}
	}
	return nil
}

func claudeToolResultDetails(callID, content string, isError bool, payload map[string]interface{}) *taskexecutor.ToolCall {
	if callID == "" {
		return nil
	}
	if content == "Structured output provided successfully" && len(payload) == 0 {
		return nil
	}
	tool := &taskexecutor.ToolCall{
		CallID:     callID,
		Status:     taskexecutor.ToolStatusCompleted,
		OutputText: content,
	}
	if isError {
		tool.Status = taskexecutor.ToolStatusFailed
		tool.ErrorText = content
		tool.OutputText = ""
	}
	if len(payload) == 0 {
		tool.Kind = taskexecutor.ToolKindOther
		tool.Name = "tool_result"
		return tool
	}
	tool.RawOutputJSON = rawJSON(payload)
	switch {
	case payload["stdout"] != nil || payload["stderr"] != nil || payload["interrupted"] != nil:
		tool.Name = "Bash"
		tool.Kind = taskexecutor.ToolKindShell
		if output := strings.TrimSpace(asString(payload["stdout"])); output != "" {
			tool.OutputText = output
		}
		if stderr := strings.TrimSpace(asString(payload["stderr"])); stderr != "" && (isError || tool.OutputText == "") {
			tool.ErrorText = stderr
		}
	case payload["oldString"] != nil || payload["newString"] != nil || payload["replaceAll"] != nil:
		tool.Name = "Edit"
		tool.Kind = taskexecutor.ToolKindEdit
		if path := claudePayloadPath(payload); path != "" {
			tool.InputSummary = path
			tool.Paths = []string{path}
		}
		if oldString := asString(payload["oldString"]); oldString != "" {
			old := oldString
			tool.Diffs = append(tool.Diffs, taskexecutor.ToolDiff{
				Path:    claudePayloadPath(payload),
				OldText: &old,
				NewText: asString(payload["newString"]),
			})
		}
	case strings.EqualFold(asString(payload["type"]), "create"):
		tool.Name = "Write"
		tool.Kind = taskexecutor.ToolKindWrite
		if path := claudePayloadPath(payload); path != "" {
			tool.InputSummary = path
			tool.Paths = []string{path}
		}
	case claudePayloadPath(payload) != "":
		tool.Name = "Read"
		tool.Kind = taskexecutor.ToolKindRead
		if path := claudePayloadPath(payload); path != "" {
			tool.InputSummary = path
			tool.Paths = []string{path}
		}
	default:
		tool.Name = "tool_result"
		tool.Kind = taskexecutor.ToolKindOther
	}
	if tool.ErrorText != "" {
		tool.Status = taskexecutor.ToolStatusFailed
	}
	return tool
}

func claudeToolPath(input map[string]interface{}) string {
	return firstNonEmptyString(
		asString(input["file_path"]),
		asString(input["filePath"]),
		asString(input["path"]),
	)
}

func claudePayloadPath(payload map[string]interface{}) string {
	return firstNonEmptyString(
		asString(payload["filePath"]),
		asString(payload["file_path"]),
		asString(payload["path"]),
	)
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

func joinNonEmpty(sep string, values ...string) string {
	parts := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		parts = append(parts, value)
	}
	return strings.Join(parts, sep)
}

func rawJSON(value interface{}) string {
	if value == nil {
		return ""
	}
	data, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	return string(data)
}

// extractStructuredOutputInput recovers the StructuredOutput tool input from
// an assistant-type stream message. Claude Code stream-json emits assistant
// turns as:
//
//	{"type":"assistant","message":{"content":[{"type":"tool_use","name":"StructuredOutput","input":{...}}]}}
//
// Returns the input payload if found, nil otherwise. Safely returns nil for
// any non-matching message format.
func extractStructuredOutputInput(raw json.RawMessage) json.RawMessage {
	var envelope struct {
		Message struct {
			Content []struct {
				Type  string          `json:"type"`
				Name  string          `json:"name"`
				Input json.RawMessage `json:"input"`
			} `json:"content"`
		} `json:"message"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil
	}
	for _, block := range envelope.Message.Content {
		if block.Type == "tool_use" && block.Name == "StructuredOutput" && len(block.Input) > 0 {
			return block.Input
		}
	}
	return nil
}

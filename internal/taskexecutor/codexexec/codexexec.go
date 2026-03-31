package codexexec

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
	outputSchema := taskexecutor.BuildOutputSchema(req)
	if err := taskexecutor.WriteSchema(req.SchemaPath, outputSchema); err != nil {
		return taskexecutor.Result{}, err
	}

	prompt := taskexecutor.AppendOutputContract(req)
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
	resumeSessionID := taskexecutor.ResumeTargetSessionID(req)
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
	var structuredError string
	for {
		var raw json.RawMessage
		if err := decoder.Decode(&raw); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			stopCommand(cmd)
			stderrWG.Wait()
			return taskexecutor.Result{}, fmt.Errorf("invalid codex jsonl line: %w", err)
		}
		line, err := parseJSONLLine(raw)
		if err != nil {
			stopCommand(cmd)
			stderrWG.Wait()
			return taskexecutor.Result{}, err
		}
		if line.SessionID != "" && sessionID == "" {
			sessionID = line.SessionID
		}
		if resumeSessionID != "" && line.SessionID != "" && line.SessionID != resumeSessionID {
			stopCommand(cmd)
			stderrWG.Wait()
			return taskexecutor.Result{}, fmt.Errorf("codex resume switched threads: expected %q, got %q", resumeSessionID, line.SessionID)
		}
		if line.ErrorMessage != "" {
			structuredError = line.ErrorMessage
		}
		if progress != nil && (line.Message != "" || line.SessionID != "" || len(line.Events) > 0) {
			progress(taskexecutor.Progress{
				Message:   line.Message,
				SessionID: line.SessionID,
				Events:    append([]taskexecutor.StreamEvent(nil), line.Events...),
			})
		}
	}
	if err := cmd.Wait(); err != nil {
		stderrWG.Wait()
		stderrText := strings.TrimSpace(stderrBuf.String())
		if structuredError != "" {
			return taskexecutor.Result{}, fmt.Errorf("%w: %s", err, structuredError)
		}
		if stderrText != "" {
			return taskexecutor.Result{}, fmt.Errorf("%w: %s", err, stderrText)
		}
		return taskexecutor.Result{}, err
	}
	stderrWG.Wait()

	outputBytes, err := os.ReadFile(outputPath)
	if err != nil {
		return taskexecutor.Result{}, err
	}
	result, err := taskexecutor.ParseOutputEnvelope(req, outputBytes)
	if err != nil {
		return taskexecutor.Result{}, err
	}
	result.SessionID = coalesceSessionID(sessionID, resumeSessionID)
	return result, nil
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
	if sessionID := taskexecutor.ResumeTargetSessionID(req); sessionID != "" {
		args = append(args, "resume", sessionID, prompt)
		return args
	}
	args = append(args, prompt)
	return args
}

func coalesceSessionID(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func stopCommand(cmd *exec.Cmd) {
	if cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
	_ = cmd.Wait()
}

type parsedJSONLLine struct {
	Message      string
	SessionID    string
	ErrorMessage string
	Events       []taskexecutor.StreamEvent
}

func parseJSONLLine(line []byte) (parsedJSONLLine, error) {
	rawLine := strings.TrimSpace(string(line))
	var payload map[string]interface{}
	if err := json.Unmarshal(line, &payload); err != nil {
		return parsedJSONLLine{}, fmt.Errorf("invalid codex jsonl line: %w", err)
	}
	parsed := parsedJSONLLine{}
	handled := false
	if kind, _ := payload["type"].(string); kind != "" {
		handled = true
		switch {
		case strings.Contains(kind, "thread.started"):
			if id, _ := payload["thread_id"].(string); id != "" {
				parsed.SessionID = id
			}
		case kind == "error":
			parsed.ErrorMessage = asString(payload["message"])
		case strings.Contains(kind, "turn.failed"):
			if errorMap, ok := payload["error"].(map[string]interface{}); ok {
				parsed.ErrorMessage = asString(errorMap["message"])
			}
		case kind == "turn.completed":
			if usage := codexUsageEvent(rawLine, payload); usage.Kind != "" {
				parsed.Events = append(parsed.Events, usage)
			}
		default:
			if event := codexStreamEvent(rawLine, kind, payload); event.Kind != "" {
				parsed.Events = append(parsed.Events, event)
			} else {
				handled = false
			}
		}
	}
	if parsed.SessionID == "" {
		if id, _ := payload["session_id"].(string); id != "" {
			parsed.SessionID = id
		}
	}
	if len(parsed.Events) == 0 && parsed.ErrorMessage == "" && rawLine != "" && !handled {
		parsed.Events = append(parsed.Events, taskexecutor.StreamEvent{
			Kind:      taskexecutor.StreamEventKindRaw,
			SessionID: parsed.SessionID,
			Raw:       rawLine,
		})
	}
	for i := range parsed.Events {
		if parsed.Events[i].SessionID == "" {
			parsed.Events[i].SessionID = parsed.SessionID
		}
	}
	parsed.Message = summarizeEvents(parsed.Events)
	return parsed, nil
}

func asString(value interface{}) string {
	text, _ := value.(string)
	return text
}

func codexStreamEvent(rawLine, kind string, payload map[string]interface{}) taskexecutor.StreamEvent {
	item := asMap(payload["item"])
	if len(item) == 0 {
		return taskexecutor.StreamEvent{}
	}
	itemType := asString(item["type"])
	status := codexStatus(kind, item)
	switch itemType {
	case "todo_list":
		steps := make([]taskexecutor.PlanStep, 0, len(asSlice(item["items"])))
		for _, rawStep := range asSlice(item["items"]) {
			stepMap := asMap(rawStep)
			if len(stepMap) == 0 {
				continue
			}
			stepStatus := "pending"
			if asBool(stepMap["completed"]) {
				stepStatus = "completed"
			}
			steps = append(steps, taskexecutor.PlanStep{
				Text:   asString(stepMap["text"]),
				Status: stepStatus,
			})
		}
		if len(steps) == 0 {
			return taskexecutor.StreamEvent{}
		}
		return taskexecutor.StreamEvent{
			Kind: taskexecutor.StreamEventKindPlan,
			Raw:  rawLine,
			Plan: &taskexecutor.PlanSnapshot{
				PlanID: asString(item["id"]),
				Steps:  steps,
			},
		}
	case "command_execution":
		command := asString(item["command"])
		event := taskexecutor.StreamEvent{
			Kind: taskexecutor.StreamEventKindTool,
			Raw:  rawLine,
			Tool: &taskexecutor.ToolCall{
				CallID:       asString(item["id"]),
				Name:         "command_execution",
				Kind:         taskexecutor.ToolKindShell,
				Title:        "Run command",
				Status:       status,
				InputSummary: command,
				OutputText:   asString(item["aggregated_output"]),
			},
		}
		if exitCode, ok := asInt64(item["exit_code"]); ok && exitCode != 0 {
			event.Tool.Status = taskexecutor.ToolStatusFailed
			event.Tool.ErrorText = fmt.Sprintf("exit code %d", exitCode)
		}
		return event
	case "file_change":
		changes := asSlice(item["changes"])
		paths := make([]string, 0, len(changes))
		summaries := make([]string, 0, len(changes))
		for _, rawChange := range changes {
			change := asMap(rawChange)
			path := asString(change["path"])
			if path == "" {
				continue
			}
			paths = append(paths, path)
			summaries = append(summaries, summarizeChangedPath(change))
		}
		return taskexecutor.StreamEvent{
			Kind: taskexecutor.StreamEventKindTool,
			Raw:  rawLine,
			Tool: &taskexecutor.ToolCall{
				CallID:       asString(item["id"]),
				Name:         "file_change",
				Kind:         taskexecutor.ToolKindFileChange,
				Title:        "Changed files",
				Status:       status,
				InputSummary: strings.Join(summaries, ", "),
				Paths:        paths,
			},
		}
	case "web_search":
		return taskexecutor.StreamEvent{
			Kind: taskexecutor.StreamEventKindTool,
			Raw:  rawLine,
			Tool: &taskexecutor.ToolCall{
				CallID:       asString(item["id"]),
				Name:         "web_search",
				Kind:         taskexecutor.ToolKindFetch,
				Title:        "web search",
				Status:       status,
				InputSummary: codexWebSearchSummary(item),
			},
		}
	case "agent_message":
		text := asString(item["text"])
		if summary, ok := codexAgentEnvelopeSummary(text); ok {
			if summary == "" {
				return taskexecutor.StreamEvent{}
			}
			text = summary
		}
		text = strings.TrimSpace(text)
		if text == "" {
			return taskexecutor.StreamEvent{}
		}
		return taskexecutor.StreamEvent{
			Kind: taskexecutor.StreamEventKindMessage,
			Raw:  rawLine,
			Message: &taskexecutor.MessagePart{
				MessageID: asString(item["id"]),
				Role:      taskexecutor.MessageRoleAssistant,
				Type:      taskexecutor.MessagePartTypeText,
				Text:      text,
			},
		}
	default:
		return taskexecutor.StreamEvent{}
	}
}

func codexUsageEvent(rawLine string, payload map[string]interface{}) taskexecutor.StreamEvent {
	usage := asMap(payload["usage"])
	if len(usage) == 0 {
		return taskexecutor.StreamEvent{}
	}
	inputTokens, _ := asInt64(usage["input_tokens"])
	cachedInputTokens, _ := asInt64(usage["cached_input_tokens"])
	outputTokens, _ := asInt64(usage["output_tokens"])
	return taskexecutor.StreamEvent{
		Kind: taskexecutor.StreamEventKindUsage,
		Raw:  rawLine,
		Usage: &taskexecutor.UsageSnapshot{
			InputTokens:       inputTokens,
			CachedInputTokens: cachedInputTokens,
			OutputTokens:      outputTokens,
			TotalTokens:       inputTokens + outputTokens,
		},
	}
}

func codexStatus(kind string, item map[string]interface{}) taskexecutor.ToolStatus {
	if status := asString(item["status"]); status != "" {
		switch status {
		case "pending":
			return taskexecutor.ToolStatusPending
		case "in_progress":
			return taskexecutor.ToolStatusInProgress
		case "completed":
			return taskexecutor.ToolStatusCompleted
		case "failed":
			return taskexecutor.ToolStatusFailed
		}
	}
	switch kind {
	case "item.started":
		return taskexecutor.ToolStatusInProgress
	case "item.completed":
		return taskexecutor.ToolStatusCompleted
	default:
		return taskexecutor.ToolStatusPending
	}
}

func codexAgentEnvelopeSummary(text string) (string, bool) {
	text = strings.TrimSpace(text)
	if text == "" || !strings.HasPrefix(text, "{") {
		return "", false
	}
	var payload map[string]interface{}
	if err := json.Unmarshal([]byte(text), &payload); err != nil {
		return "", false
	}
	kind := asString(payload["kind"])
	if kind == "" {
		return "", false
	}
	if result := asMap(payload["result"]); len(result) > 0 {
		if summary := asString(result["summary"]); summary != "" {
			return summary, true
		}
	}
	if clarification := asMap(payload["clarification"]); len(clarification) > 0 {
		if questions := asSlice(clarification["questions"]); len(questions) > 0 {
			first := asMap(questions[0])
			if question := asString(first["question"]); question != "" {
				return question, true
			}
		}
	}
	return "", true
}

func summarizeEvents(events []taskexecutor.StreamEvent) string {
	summaries := make([]string, 0, len(events))
	for _, event := range events {
		if summary := event.Summary(); summary != "" {
			summaries = append(summaries, summary)
		}
	}
	return strings.Join(summaries, "\n")
}

func summarizeChangedPath(change map[string]interface{}) string {
	path := asString(change["path"])
	if path == "" {
		return ""
	}
	label := filepath.Base(path)
	switch strings.ToLower(strings.TrimSpace(asString(change["kind"]))) {
	case "add", "create":
		return "A " + label
	case "delete", "remove":
		return "D " + label
	case "rename", "move":
		return "R " + label
	default:
		return "M " + label
	}
}

func codexWebSearchSummary(item map[string]interface{}) string {
	if query := strings.TrimSpace(asString(item["query"])); query != "" {
		return query
	}
	action := asMap(item["action"])
	if query := strings.TrimSpace(asString(action["query"])); query != "" {
		return query
	}
	for _, rawQuery := range asSlice(action["queries"]) {
		if query := strings.TrimSpace(asString(rawQuery)); query != "" {
			return query
		}
	}
	return ""
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
	case int:
		return int64(item), true
	case int64:
		return item, true
	default:
		return 0, false
	}
}

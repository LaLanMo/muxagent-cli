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
				progress(taskexecutor.Progress{Message: strings.TrimSpace(string(raw))})
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

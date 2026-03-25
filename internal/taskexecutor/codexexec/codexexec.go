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
		progressMessage, foundSessionID, errorMessage, err := parseJSONLLine(raw)
		if err != nil {
			stopCommand(cmd)
			stderrWG.Wait()
			return taskexecutor.Result{}, err
		}
		if foundSessionID != "" && sessionID == "" {
			sessionID = foundSessionID
		}
		if resumeSessionID != "" && foundSessionID != "" && foundSessionID != resumeSessionID {
			stopCommand(cmd)
			stderrWG.Wait()
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

func asString(value interface{}) string {
	text, _ := value.(string)
	return text
}

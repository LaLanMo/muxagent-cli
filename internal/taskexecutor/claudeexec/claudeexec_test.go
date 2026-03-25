package claudeexec

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LaLanMo/muxagent-cli/internal/taskconfig"
	"github.com/LaLanMo/muxagent-cli/internal/taskdomain"
	"github.com/LaLanMo/muxagent-cli/internal/taskexecutor"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExecutorParsesFreshSuccessAndWritesOutput(t *testing.T) {
	binaryPath := writeFakeClaude(t)
	t.Setenv("FAKE_CLAUDE_MODE", "result")

	executor := New(binaryPath)
	req := requestFixture(t.TempDir())
	var progress []taskexecutor.Progress

	result, err := executor.Execute(context.Background(), req, func(item taskexecutor.Progress) {
		progress = append(progress, item)
	})
	require.NoError(t, err)
	assert.Equal(t, req.NodeRun.ID, result.SessionID)
	assert.Equal(t, taskexecutor.ResultKindResult, result.Kind)
	require.Len(t, progress, 3)
	assert.Equal(t, req.NodeRun.ID, progress[0].SessionID)
	assert.Empty(t, progress[0].Message)
	assert.Contains(t, progress[1].Message, `"type":"assistant"`)
	assert.Contains(t, progress[2].Message, `"type":"tool_use"`)

	outputBytes, err := os.ReadFile(filepath.Join(req.ArtifactDir, "output.json"))
	require.NoError(t, err)
	var output map[string]interface{}
	require.NoError(t, json.Unmarshal(outputBytes, &output))
	assert.Equal(t, "result", output["kind"])
}

func TestExecutorParsesClarificationEnvelope(t *testing.T) {
	binaryPath := writeFakeClaude(t)
	t.Setenv("FAKE_CLAUDE_MODE", "clarification")

	executor := New(binaryPath)
	result, err := executor.Execute(context.Background(), requestFixture(t.TempDir()), nil)
	require.NoError(t, err)
	assert.Equal(t, taskexecutor.ResultKindClarification, result.Kind)
	require.NotNil(t, result.Clarification)
	assert.Len(t, result.Clarification.Questions, 1)
}

func TestBuildExecArgsUsesResumeForAnsweredClarification(t *testing.T) {
	req := requestFixture(t.TempDir())
	req.NodeRun.SessionID = "session-123"
	req.NodeRun.Clarifications = []taskdomain.ClarificationExchange{
		{
			Request: taskdomain.ClarificationRequest{
				Questions: []taskdomain.ClarificationQuestion{{Question: "Need input"}},
			},
			Response: &taskdomain.ClarificationResponse{
				Answers: []taskdomain.ClarificationAnswer{{Selected: "A"}},
			},
		},
	}

	expectedSessionID, args := buildExecArgs(req, `{"type":"object"}`, "resume prompt")
	assert.Equal(t, "session-123", expectedSessionID)
	assert.Contains(t, strings.Join(args, "\n"), "--resume\nsession-123\nresume prompt")
	assert.NotContains(t, strings.Join(args, "\n"), "--session-id")
}

func TestBuildExecArgsUsesDeterministicSessionIDForFreshRun(t *testing.T) {
	req := requestFixture(t.TempDir())

	expectedSessionID, args := buildExecArgs(req, `{"type":"object"}`, "fresh prompt")
	assert.Equal(t, req.NodeRun.ID, expectedSessionID)
	assert.Contains(t, strings.Join(args, "\n"), "--session-id\n"+req.NodeRun.ID+"\nfresh prompt")
}

func TestExecutorFailsWithoutStructuredOutput(t *testing.T) {
	binaryPath := writeFakeClaude(t)
	t.Setenv("FAKE_CLAUDE_MODE", "missing-structured-output")

	executor := New(binaryPath)
	_, err := executor.Execute(context.Background(), requestFixture(t.TempDir()), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing structured_output")
}

func TestExecutorFailsOnErrorSubtype(t *testing.T) {
	binaryPath := writeFakeClaude(t)
	t.Setenv("FAKE_CLAUDE_MODE", "error-subtype")

	executor := New(binaryPath)
	_, err := executor.Execute(context.Background(), requestFixture(t.TempDir()), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "error_max_turns")
	assert.Contains(t, err.Error(), "out of turns")
}

func TestExecutorFailsOnMalformedStreamJSON(t *testing.T) {
	binaryPath := writeFakeClaude(t)
	t.Setenv("FAKE_CLAUDE_MODE", "badjson")

	executor := New(binaryPath)
	_, err := executor.Execute(context.Background(), requestFixture(t.TempDir()), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid claude stream json")
}

func TestExecutorFailsOnSessionMismatch(t *testing.T) {
	binaryPath := writeFakeClaude(t)
	t.Setenv("FAKE_CLAUDE_MODE", "session-mismatch")

	executor := New(binaryPath)
	_, err := executor.Execute(context.Background(), requestFixture(t.TempDir()), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "claude session drift")
}

func TestExecutorFailsWhenFinalResultIsMissing(t *testing.T) {
	binaryPath := writeFakeClaude(t)
	t.Setenv("FAKE_CLAUDE_MODE", "no-final-result")

	executor := New(binaryPath)
	_, err := executor.Execute(context.Background(), requestFixture(t.TempDir()), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "without final result message")
}

func TestExecutorStripsInheritedCLAUDECODE(t *testing.T) {
	binaryPath := writeFakeClaude(t)
	envFile := filepath.Join(t.TempDir(), "claude-env.txt")
	t.Setenv("FAKE_CLAUDE_MODE", "result")
	t.Setenv("FAKE_CLAUDE_ENV_FILE", envFile)
	t.Setenv("CLAUDECODE", "nested")

	executor := New(binaryPath)
	_, err := executor.Execute(context.Background(), requestFixture(t.TempDir()), nil)
	require.NoError(t, err)

	data, err := os.ReadFile(envFile)
	require.NoError(t, err)
	assert.Equal(t, "", strings.TrimSpace(string(data)))
}

func requestFixture(artifactDir string) taskexecutor.Request {
	allow := false
	return taskexecutor.Request{
		Task: taskdomain.Task{
			ID:          "task-1",
			Description: "Implement feature",
			WorkDir:     artifactDir,
		},
		NodeRun: taskdomain.NodeRun{
			ID:       "run-1",
			TaskID:   "task-1",
			NodeName: "implement",
		},
		NodeDefinition: taskconfig.NodeDefinition{
			SystemPrompt:           "./prompt.md",
			MaxClarificationRounds: 2,
			ResultSchema: taskconfig.JSONSchema{
				Type:                 "object",
				AdditionalProperties: &allow,
				Required:             []string{"file_paths"},
				Properties: map[string]*taskconfig.JSONSchema{
					"file_paths": {
						Type:  "array",
						Items: &taskconfig.JSONSchema{Type: "string"},
					},
				},
			},
		},
		ClarificationConfig: taskconfig.ClarificationConfig{
			MaxQuestions:          4,
			MaxOptionsPerQuestion: 4,
			MinOptionsPerQuestion: 2,
		},
		ConfigPath:  filepath.Join(artifactDir, "config.yaml"),
		SchemaPath:  filepath.Join(artifactDir, "schemas", "implement.json"),
		WorkDir:     artifactDir,
		ArtifactDir: artifactDir,
		Prompt:      "do it",
		ResultSchema: taskconfig.JSONSchema{
			Type:                 "object",
			AdditionalProperties: &allow,
			Required:             []string{"file_paths"},
			Properties: map[string]*taskconfig.JSONSchema{
				"file_paths": {
					Type:  "array",
					Items: &taskconfig.JSONSchema{Type: "string"},
				},
			},
		},
	}
}

func writeFakeClaude(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fake-claude.sh")
	script := `#!/bin/sh
set -eu
mode="${FAKE_CLAUDE_MODE:-result}"
args_file="${FAKE_CLAUDE_ARGS_FILE:-}"
env_file="${FAKE_CLAUDE_ENV_FILE:-}"
session_id=""
resume_id=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    --session-id)
      session_id="$2"
      shift 2
      ;;
    --resume)
      resume_id="$2"
      shift 2
      ;;
    *)
      shift
      ;;
  esac
done

if [ -n "$resume_id" ]; then
  expected_session="$resume_id"
else
  expected_session="$session_id"
fi

if [ -n "$args_file" ]; then
  : > "$args_file"
  for arg in "$@"; do
    printf '%s\n' "$arg" >> "$args_file"
  done
fi

if [ -n "$env_file" ]; then
  printf '%s' "${CLAUDECODE:-}" > "$env_file"
fi

case "$mode" in
  result)
    printf '{"type":"assistant","message":"planning","session_id":"%s"}\n' "$expected_session"
    printf '{"type":"tool_use","tool":"edit","session_id":"%s"}\n' "$expected_session"
    printf '{"type":"result","subtype":"success","session_id":"%s","structured_output":{"kind":"result","result":{"file_paths":["/tmp/artifact.md"]},"clarification":null}}\n' "$expected_session"
    ;;
  clarification)
    printf '{"type":"assistant","message":"need input","session_id":"%s"}\n' "$expected_session"
    printf '{"type":"result","subtype":"success","session_id":"%s","structured_output":{"kind":"clarification","result":null,"clarification":{"questions":[{"question":"What should we do?","why_it_matters":"Need direction","options":[{"label":"A","description":"Option A"},{"label":"B","description":"Option B"}],"multi_select":false}]}}}\n' "$expected_session"
    ;;
  missing-structured-output)
    printf '{"type":"result","subtype":"success","session_id":"%s"}\n' "$expected_session"
    ;;
  error-subtype)
    printf '{"type":"result","subtype":"error_max_turns","session_id":"%s","errors":[{"message":"out of turns"}]}\n' "$expected_session"
    exit 1
    ;;
  badjson)
    echo '{bad json'
    ;;
  session-mismatch)
    printf '{"type":"assistant","message":"planning","session_id":"wrong-session"}\n'
    ;;
  no-final-result)
    printf '{"type":"assistant","message":"planning","session_id":"%s"}\n' "$expected_session"
    ;;
esac
`
	require.NoError(t, os.WriteFile(path, []byte(script), 0o755))
	return path
}

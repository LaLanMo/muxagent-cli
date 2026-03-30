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
	require.Len(t, progress, 4)
	assert.Equal(t, req.NodeRun.ID, progress[0].SessionID)
	assert.Empty(t, progress[0].Message)
	assert.Equal(t, "thinking: inspect the workspace first", progress[1].Message)
	assert.Equal(t, "shell running: pwd", progress[2].Message)
	assert.Equal(t, "shell", progress[3].Message)
	require.Len(t, progress[2].Events, 1)
	require.NotNil(t, progress[2].Events[0].Tool)
	assert.Equal(t, taskexecutor.ToolKindShell, progress[2].Events[0].Tool.Kind)

	outputBytes, err := os.ReadFile(filepath.Join(req.ArtifactDir, "output.json"))
	require.NoError(t, err)
	var output map[string]interface{}
	require.NoError(t, json.Unmarshal(outputBytes, &output))
	assert.Equal(t, "result", output["kind"])
}

func TestBuildProgressUpdateParsesClaudeEditToolLifecycle(t *testing.T) {
	startRaw := json.RawMessage(`{"type":"assistant","message":{"id":"msg-edit","role":"assistant","content":[{"type":"tool_use","id":"toolu-edit","name":"Edit","input":{"replace_all":false,"file_path":"/tmp/project/sample.txt","old_string":"alpha\nbeta\n","new_string":"alpha\nbeta\ngamma\n"},"caller":{"type":"direct"}}]},"session_id":"run-1"}`)
	resultRaw := json.RawMessage(`{"type":"user","message":{"role":"user","content":[{"tool_use_id":"toolu-edit","type":"tool_result","content":"The file /tmp/project/sample.txt has been updated.","is_error":false}]},"tool_use_result":{"filePath":"/tmp/project/sample.txt","oldString":"alpha\nbeta\n","newString":"alpha\nbeta\ngamma\n","structuredPatch":[{"oldStart":1,"oldLines":2,"newStart":1,"newLines":3,"lines":[" alpha"," beta","+gamma"]}],"userModified":false,"replaceAll":false},"session_id":"run-1"}`)

	start := buildProgressUpdate(startRaw, streamMessage{Type: "assistant", SessionID: "run-1"})
	require.Len(t, start.Events, 1)
	require.NotNil(t, start.Events[0].Tool)
	assert.Equal(t, taskexecutor.ToolKindEdit, start.Events[0].Tool.Kind)
	assert.Equal(t, "edit running: /tmp/project/sample.txt", start.Message)

	done := buildProgressUpdate(resultRaw, streamMessage{Type: "user", SessionID: "run-1"})
	require.Len(t, done.Events, 1)
	require.NotNil(t, done.Events[0].Tool)
	assert.Equal(t, taskexecutor.ToolKindEdit, done.Events[0].Tool.Kind)
	assert.Equal(t, "edit: /tmp/project/sample.txt", done.Message)
	require.Len(t, done.Events[0].Tool.Diffs, 1)
	assert.Equal(t, "/tmp/project/sample.txt", done.Events[0].Tool.Diffs[0].Path)
	assert.Equal(t, "alpha\nbeta\ngamma\n", done.Events[0].Tool.Diffs[0].NewText)
}

func TestBuildProgressUpdateClassifiesClaudeNativeTools(t *testing.T) {
	tests := []struct {
		name      string
		toolName  string
		inputJSON string
		wantKind  taskexecutor.ToolKind
		wantText  string
	}{
		{
			name:      "grep maps to search",
			toolName:  "Grep",
			inputJSON: `{"pattern":"theme","path":"/tmp/project"}`,
			wantKind:  taskexecutor.ToolKindSearch,
			wantText:  "theme /tmp/project",
		},
		{
			name:      "glob maps to search",
			toolName:  "Glob",
			inputJSON: `{"pattern":"**/*.go","path":"/tmp/project"}`,
			wantKind:  taskexecutor.ToolKindSearch,
			wantText:  "**/*.go /tmp/project",
		},
		{
			name:      "ls maps to search",
			toolName:  "LS",
			inputJSON: `{"path":"/tmp/project"}`,
			wantKind:  taskexecutor.ToolKindSearch,
			wantText:  "/tmp/project",
		},
		{
			name:      "notebook read maps to read",
			toolName:  "NotebookRead",
			inputJSON: `{"file_path":"/tmp/project/demo.ipynb"}`,
			wantKind:  taskexecutor.ToolKindRead,
			wantText:  "/tmp/project/demo.ipynb",
		},
		{
			name:      "notebook edit maps to edit",
			toolName:  "NotebookEdit",
			inputJSON: `{"file_path":"/tmp/project/demo.ipynb"}`,
			wantKind:  taskexecutor.ToolKindEdit,
			wantText:  "/tmp/project/demo.ipynb",
		},
		{
			name:      "web fetch maps to fetch",
			toolName:  "WebFetch",
			inputJSON: `{"url":"https://example.com"}`,
			wantKind:  taskexecutor.ToolKindFetch,
			wantText:  "https://example.com",
		},
		{
			name:      "todo write stays native fallback",
			toolName:  "TodoWrite",
			inputJSON: `{"todos":[{"content":"Inspect repo","activeForm":"Inspecting repo","status":"pending"}]}`,
			wantKind:  taskexecutor.ToolKindOther,
			wantText:  `{"todos":[{"content":"Inspect repo","activeForm":"Inspecting repo","status":"pending"}]}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw := json.RawMessage(`{"type":"assistant","message":{"id":"msg-1","role":"assistant","content":[{"type":"tool_use","id":"toolu-1","name":"` + tt.toolName + `","input":` + tt.inputJSON + `,"caller":{"type":"direct"}}]},"session_id":"run-1"}`)

			update := buildProgressUpdate(raw, streamMessage{Type: "assistant", SessionID: "run-1"})
			require.Len(t, update.Events, 1)
			require.NotNil(t, update.Events[0].Tool)
			assert.Equal(t, tt.wantKind, update.Events[0].Tool.Kind)
			assert.Equal(t, tt.toolName, update.Events[0].Tool.Name)
			if strings.HasPrefix(tt.wantText, "{") {
				assert.JSONEq(t, tt.wantText, update.Events[0].Tool.InputSummary)
				return
			}
			assert.Equal(t, tt.wantText, update.Events[0].Tool.InputSummary)
		})
	}
}

func TestClaudeSparseToolResultDoesNotDowngradeExistingLabel(t *testing.T) {
	startRaw := json.RawMessage(`{"type":"assistant","message":{"id":"msg-search","role":"assistant","content":[{"type":"tool_use","id":"toolu-search","name":"Grep","input":{"pattern":"theme","path":"/tmp/project"},"caller":{"type":"direct"}}]},"session_id":"run-1"}`)
	resultRaw := json.RawMessage(`{"type":"user","message":{"role":"user","content":[{"tool_use_id":"toolu-search","type":"tool_result","content":"done","is_error":false}]},"session_id":"run-1"}`)

	start := buildProgressUpdate(startRaw, streamMessage{Type: "assistant", SessionID: "run-1"})
	done := buildProgressUpdate(resultRaw, streamMessage{Type: "user", SessionID: "run-1"})
	require.Len(t, start.Events, 1)
	require.Len(t, done.Events, 1)

	merged := taskexecutor.MergeStreamEvent(start.Events[0], done.Events[0])
	require.NotNil(t, merged.Tool)
	assert.Equal(t, taskexecutor.ToolKindSearch, merged.Tool.Kind)
	assert.Equal(t, "Grep", merged.Tool.Name)
	assert.Equal(t, taskexecutor.ToolStatusCompleted, merged.Tool.Status)
	assert.Equal(t, "search", merged.Tool.DisplayLabel())
}

func TestClaudeUnknownToolResultShapeDoesNotDowngradeExistingLabel(t *testing.T) {
	startRaw := json.RawMessage(`{"type":"assistant","message":{"id":"msg-fetch","role":"assistant","content":[{"type":"tool_use","id":"toolu-fetch","name":"WebFetch","input":{"url":"https://example.com"},"caller":{"type":"direct"}}]},"session_id":"run-1"}`)
	resultRaw := json.RawMessage(`{"type":"user","message":{"role":"user","content":[{"tool_use_id":"toolu-fetch","type":"tool_result","content":"fetched","is_error":false}]},"tool_use_result":{"unexpected":"shape"},"session_id":"run-1"}`)

	start := buildProgressUpdate(startRaw, streamMessage{Type: "assistant", SessionID: "run-1"})
	done := buildProgressUpdate(resultRaw, streamMessage{Type: "user", SessionID: "run-1"})
	require.Len(t, start.Events, 1)
	require.Len(t, done.Events, 1)

	merged := taskexecutor.MergeStreamEvent(start.Events[0], done.Events[0])
	require.NotNil(t, merged.Tool)
	assert.Equal(t, taskexecutor.ToolKindFetch, merged.Tool.Kind)
	assert.Equal(t, "WebFetch", merged.Tool.Name)
	assert.Equal(t, taskexecutor.ToolStatusCompleted, merged.Tool.Status)
	assert.Equal(t, "fetch", merged.Tool.DisplayLabel())
}

func TestClaudeToolResultRecognizesResultOnlyPayloads(t *testing.T) {
	raw := json.RawMessage(`{"type":"user","message":{"role":"user","content":[{"tool_use_id":"toolu-read","type":"tool_result","content":"File read","is_error":false}]},"tool_use_result":{"filePath":"/tmp/project/sample.txt"},"session_id":"run-1"}`)

	update := buildProgressUpdate(raw, streamMessage{Type: "user", SessionID: "run-1"})
	require.Len(t, update.Events, 1)
	require.NotNil(t, update.Events[0].Tool)
	assert.Equal(t, taskexecutor.ToolKindRead, update.Events[0].Tool.Kind)
	assert.Equal(t, "Read", update.Events[0].Tool.Name)
	assert.Equal(t, "/tmp/project/sample.txt", update.Events[0].Tool.InputSummary)
}

func TestClaudeToolResultMarksShellFailureFromStderrOnlyPayload(t *testing.T) {
	raw := json.RawMessage(`{"type":"user","message":{"role":"user","content":[{"tool_use_id":"toolu-bash","type":"tool_result","content":"permission denied","is_error":true}]},"tool_use_result":{"stdout":"","stderr":"permission denied","interrupted":false},"session_id":"run-1"}`)

	update := buildProgressUpdate(raw, streamMessage{Type: "user", SessionID: "run-1"})
	require.Len(t, update.Events, 1)
	require.NotNil(t, update.Events[0].Tool)
	assert.Equal(t, taskexecutor.ToolKindShell, update.Events[0].Tool.Kind)
	assert.Equal(t, taskexecutor.ToolStatusFailed, update.Events[0].Tool.Status)
	assert.Equal(t, "permission denied", update.Events[0].Tool.ErrorText)
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

func TestExecutorRecoversStructuredOutputFromStream(t *testing.T) {
	binaryPath := writeFakeClaude(t)
	t.Setenv("FAKE_CLAUDE_MODE", "recovered-structured-output")

	executor := New(binaryPath)
	req := requestFixture(t.TempDir())
	result, err := executor.Execute(context.Background(), req, nil)
	require.NoError(t, err)
	assert.Equal(t, taskexecutor.ResultKindResult, result.Kind)

	outputBytes, err := os.ReadFile(filepath.Join(req.ArtifactDir, "output.json"))
	require.NoError(t, err)
	var output map[string]interface{}
	require.NoError(t, json.Unmarshal(outputBytes, &output))
	assert.Equal(t, "result", output["kind"])
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
    printf '{"type":"system","subtype":"init","session_id":"%s","cwd":"/tmp/project"}\n' "$expected_session"
    printf '{"type":"assistant","message":{"id":"msg-1","role":"assistant","content":[{"type":"thinking","thinking":"inspect the workspace first"}]},"session_id":"%s"}\n' "$expected_session"
    printf '{"type":"assistant","message":{"id":"msg-2","role":"assistant","content":[{"type":"tool_use","id":"toolu-1","name":"Bash","input":{"command":"pwd","description":"Print working directory"},"caller":{"type":"direct"}}]},"session_id":"%s"}\n' "$expected_session"
    printf '%s\n' '{"type":"user","message":{"role":"user","content":[{"tool_use_id":"toolu-1","type":"tool_result","content":"/tmp/project\n","is_error":false}]},"tool_use_result":{"stdout":"/tmp/project\n","stderr":"","interrupted":false},"session_id":"'"$expected_session"'"}'
    printf '{"type":"result","subtype":"success","session_id":"%s","structured_output":{"kind":"result","result":{"file_paths":["/tmp/artifact.md"]},"clarification":null}}\n' "$expected_session"
    ;;
  clarification)
    printf '{"type":"assistant","message":{"id":"msg-1","role":"assistant","content":[{"type":"text","text":"need input"}]},"session_id":"%s"}\n' "$expected_session"
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
    printf '{"type":"assistant","message":{"id":"msg-1","role":"assistant","content":[{"type":"text","text":"planning"}]},"session_id":"wrong-session"}\n'
    ;;
  no-final-result)
    printf '{"type":"assistant","message":{"id":"msg-1","role":"assistant","content":[{"type":"text","text":"planning"}]},"session_id":"%s"}\n' "$expected_session"
    ;;
  recovered-structured-output)
    printf '{"type":"assistant","message":{"id":"msg-1","role":"assistant","content":[{"type":"tool_use","name":"StructuredOutput","id":"tu-1","input":{"kind":"result","result":{"file_paths":["/tmp/artifact.md"]},"clarification":null}}]},"session_id":"%s"}\n' "$expected_session"
    printf '{"type":"assistant","message":{"id":"msg-2","role":"assistant","content":[{"type":"text","text":"background task completed"}]},"session_id":"%s"}\n' "$expected_session"
    printf '{"type":"result","subtype":"success","session_id":"%s","structured_output":null}\n' "$expected_session"
    ;;
esac
`
	require.NoError(t, os.WriteFile(path, []byte(script), 0o755))
	return path
}

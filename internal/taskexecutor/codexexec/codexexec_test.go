package codexexec

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/LaLanMo/muxagent-cli/internal/taskconfig"
	"github.com/LaLanMo/muxagent-cli/internal/taskdomain"
	"github.com/LaLanMo/muxagent-cli/internal/taskexecutor"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExecutorParsesResultAndProgress(t *testing.T) {
	binaryPath := writeFakeCodex(t)
	t.Setenv("FAKE_CODEX_MODE", "result")

	executor := New(binaryPath)
	artifactDir := t.TempDir()
	req := requestFixture(artifactDir)
	var progress []taskexecutor.Progress

	result, err := executor.Execute(context.Background(), req, func(item taskexecutor.Progress) {
		progress = append(progress, item)
	})
	require.NoError(t, err)
	assert.Equal(t, "thread-123", result.SessionID)
	assert.Equal(t, taskexecutor.ResultKindResult, result.Kind)
	assert.NotEmpty(t, result.Result["file_paths"])
	require.Len(t, progress, 6)
	assert.Equal(t, "thread-123", progress[0].SessionID)
	assert.Empty(t, progress[0].Message)
	assert.Equal(t, []string{
		"plan: 0/3 complete, next planning changes",
		"plan: 1/3 complete, next editing files",
		"shell: /bin/zsh -lc 'go test ./...'",
		"files: A artifact.md",
		"assistant: wrapping up",
	}, []string{
		progress[1].Message,
		progress[2].Message,
		progress[3].Message,
		progress[4].Message,
		progress[5].Message,
	})
	schema := readGeneratedSchema(t, artifactDir)
	assert.Equal(t, []interface{}{"kind", "result", "clarification"}, schema["required"])
	properties := schema["properties"].(map[string]interface{})
	assert.Equal(t, []interface{}{"object", "null"}, properties["result"].(map[string]interface{})["type"])
	assert.Equal(t, []interface{}{"object", "null"}, properties["clarification"].(map[string]interface{})["type"])
}

func TestExecutorParsesClarification(t *testing.T) {
	binaryPath := writeFakeCodex(t)
	t.Setenv("FAKE_CODEX_MODE", "clarification")

	executor := New(binaryPath)
	result, err := executor.Execute(context.Background(), requestFixture(t.TempDir()), nil)
	require.NoError(t, err)
	assert.Equal(t, taskexecutor.ResultKindClarification, result.Kind)
	require.NotNil(t, result.Clarification)
	assert.Len(t, result.Clarification.Questions, 1)
	assert.Equal(t, "Option A", result.Clarification.Questions[0].Options[0].Description)
}

func TestExecutorFailsOnMalformedJSONL(t *testing.T) {
	binaryPath := writeFakeCodex(t)
	t.Setenv("FAKE_CODEX_MODE", "badjson")

	executor := New(binaryPath)
	_, err := executor.Execute(context.Background(), requestFixture(t.TempDir()), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid codex jsonl line")
}

func TestExecutorFailsOnSchemaMismatch(t *testing.T) {
	binaryPath := writeFakeCodex(t)
	t.Setenv("FAKE_CODEX_MODE", "invalid-output")

	executor := New(binaryPath)
	_, err := executor.Execute(context.Background(), requestFixture(t.TempDir()), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "file_paths")
}

func TestExecutorPropagatesSubprocessFailure(t *testing.T) {
	binaryPath := writeFakeCodex(t)
	t.Setenv("FAKE_CODEX_MODE", "fail")

	executor := New(binaryPath)
	_, err := executor.Execute(context.Background(), requestFixture(t.TempDir()), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "stderr boom")
}

func TestExecutorIncludesStructuredJSONLError(t *testing.T) {
	binaryPath := writeFakeCodex(t)
	t.Setenv("FAKE_CODEX_MODE", "jsonl-error")

	executor := New(binaryPath)
	_, err := executor.Execute(context.Background(), requestFixture(t.TempDir()), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid_json_schema")
}

func TestExecutorHandlesVeryLargeJSONLMessages(t *testing.T) {
	binaryPath := writeFakeCodex(t)
	t.Setenv("FAKE_CODEX_MODE", "large-jsonl")

	executor := New(binaryPath)
	var progress []taskexecutor.Progress

	result, err := executor.Execute(context.Background(), requestFixture(t.TempDir()), func(item taskexecutor.Progress) {
		progress = append(progress, item)
	})
	require.NoError(t, err)
	assert.Equal(t, taskexecutor.ResultKindResult, result.Kind)
	require.Len(t, progress, 3)
	assert.Equal(t, "thread-123", progress[0].SessionID)
	assert.Greater(t, len(progress[1].Message), 1024*1024)
	assert.Contains(t, progress[1].Message, `"type":"response_item"`)
	assert.Equal(t, "assistant: after huge event", progress[2].Message)
}

func TestExecutorWithoutClarificationGeneratesResultOnlySchema(t *testing.T) {
	binaryPath := writeFakeCodex(t)
	t.Setenv("FAKE_CODEX_MODE", "result")

	executor := New(binaryPath)
	req := requestFixture(t.TempDir())
	req.NodeDefinition.MaxClarificationRounds = 0
	result, err := executor.Execute(context.Background(), req, nil)
	require.NoError(t, err)
	assert.Equal(t, taskexecutor.ResultKindResult, result.Kind)
	schema := readGeneratedSchema(t, req.ArtifactDir)
	assert.Equal(t, []interface{}{"kind", "result"}, schema["required"])
	properties := schema["properties"].(map[string]interface{})
	_, hasClarification := properties["clarification"]
	assert.False(t, hasClarification)
}

func TestExecutorExhaustedClarificationRoundsForcesClarificationNull(t *testing.T) {
	binaryPath := writeFakeCodex(t)
	t.Setenv("FAKE_CODEX_MODE", "result")

	executor := New(binaryPath)
	req := requestFixture(t.TempDir())
	req.NodeDefinition.MaxClarificationRounds = 1
	req.NodeRun.SessionID = "thread-123"
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

	_, err := executor.Execute(context.Background(), req, nil)
	require.NoError(t, err)
	schema := readGeneratedSchema(t, req.ArtifactDir)
	assert.Equal(t, []interface{}{"kind", "result", "clarification"}, schema["required"])
	properties := schema["properties"].(map[string]interface{})
	assert.Equal(t, []interface{}{"result"}, properties["kind"].(map[string]interface{})["enum"])
	assert.Equal(t, "null", properties["clarification"].(map[string]interface{})["type"])
	assert.Equal(t, "object", properties["result"].(map[string]interface{})["type"])
}

func TestBuildExecArgsUsesResumeForAnsweredClarification(t *testing.T) {
	req := requestFixture(t.TempDir())
	req.NodeRun.SessionID = "thread-123"
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

	args := buildExecArgs(req, filepath.Join(req.ArtifactDir, "output.json"), "resume prompt")
	assert.Equal(t, []string{
		"exec",
		"-s", "danger-full-access",
		"--json",
		"--output-schema", req.SchemaPath,
		"-o", filepath.Join(req.ArtifactDir, "output.json"),
		"-C", req.WorkDir,
		"--skip-git-repo-check",
		"resume", "thread-123", "resume prompt",
	}, args)
}

func TestBuildExecArgsStartsFreshWithoutAnsweredClarification(t *testing.T) {
	req := requestFixture(t.TempDir())
	req.NodeRun.SessionID = "thread-123"
	req.NodeRun.Clarifications = []taskdomain.ClarificationExchange{
		{
			Request: taskdomain.ClarificationRequest{
				Questions: []taskdomain.ClarificationQuestion{{Question: "Need input"}},
			},
		},
	}

	args := buildExecArgs(req, filepath.Join(req.ArtifactDir, "output.json"), "fresh prompt")
	assert.Equal(t, "fresh prompt", args[len(args)-1])
	assert.NotContains(t, args, "resume")
}

func TestExecutorInvokesCodexResumeForAnsweredClarification(t *testing.T) {
	binaryPath := writeFakeCodex(t)
	argsFile := filepath.Join(t.TempDir(), "args.txt")
	t.Setenv("FAKE_CODEX_MODE", "result")
	t.Setenv("FAKE_CODEX_ARGS_FILE", argsFile)

	executor := New(binaryPath)
	req := requestFixture(t.TempDir())
	req.NodeRun.SessionID = "thread-123"
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

	_, err := executor.Execute(context.Background(), req, nil)
	require.NoError(t, err)

	data, err := os.ReadFile(argsFile)
	require.NoError(t, err)
	assert.Contains(t, string(data), "resume\nthread-123\n")
}

func TestExecutorFailsWhenResumeSwitchesToDifferentThread(t *testing.T) {
	binaryPath := writeFakeCodex(t)
	t.Setenv("FAKE_CODEX_MODE", "resume-different-thread")

	executor := New(binaryPath)
	req := requestFixture(t.TempDir())
	req.NodeRun.SessionID = "thread-123"
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

	_, err := executor.Execute(context.Background(), req, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "switched threads")
}

func TestParseJSONLLineUsesRawStreamingJSONLLines(t *testing.T) {
	line, err := parseJSONLLine([]byte(`{"type":"item.completed","item":{"id":"item_0","type":"file_change","changes":[{"path":"/tmp/project/hello.txt","kind":"add"}],"status":"completed"}}`))
	require.NoError(t, err)
	assert.Equal(t, "files: A hello.txt", line.Message)
	assert.Empty(t, line.SessionID)
	assert.Empty(t, line.ErrorMessage)
	require.Len(t, line.Events, 1)
	require.NotNil(t, line.Events[0].Tool)
	assert.Equal(t, taskexecutor.ToolKindFileChange, line.Events[0].Tool.Kind)

	line, err = parseJSONLLine([]byte(`{"type":"item.completed","item":{"id":"item_0","type":"file_change","changes":[{"path":"/tmp/project/hello.txt","kind":"add"},{"path":"/tmp/project/footer.go","kind":"update"},{"path":"/tmp/project/old.txt","kind":"delete"}],"status":"completed"}}`))
	require.NoError(t, err)
	assert.Equal(t, "files: A hello.txt, M footer.go, D old.txt", line.Message)
	require.Len(t, line.Events, 1)
	require.NotNil(t, line.Events[0].Tool)
	assert.Equal(t, "A hello.txt, M footer.go, D old.txt", line.Events[0].Tool.InputSummary)

	line, err = parseJSONLLine([]byte(`{"type":"item.started","item":{"id":"item_1","type":"command_execution","command":"/bin/zsh -lc 'pwd && ls'","status":"in_progress"}}`))
	require.NoError(t, err)
	assert.Equal(t, "shell running: /bin/zsh -lc 'pwd && ls'", line.Message)
	assert.Empty(t, line.SessionID)
	assert.Empty(t, line.ErrorMessage)
	require.Len(t, line.Events, 1)
	require.NotNil(t, line.Events[0].Tool)
	assert.Equal(t, taskexecutor.ToolKindShell, line.Events[0].Tool.Kind)

	line, err = parseJSONLLine([]byte(`{"type":"item.started","item":{"id":"ws_123","type":"web_search","query":"","action":{"type":"other"}}}`))
	require.NoError(t, err)
	assert.Equal(t, "fetch running: web search", line.Message)
	assert.Empty(t, line.SessionID)
	assert.Empty(t, line.ErrorMessage)
	require.Len(t, line.Events, 1)
	require.NotNil(t, line.Events[0].Tool)
	assert.Equal(t, taskexecutor.ToolKindFetch, line.Events[0].Tool.Kind)
	assert.Equal(t, "ws_123", line.Events[0].Tool.CallID)
	assert.Equal(t, "web_search", line.Events[0].Tool.Name)
	assert.Equal(t, "web search", line.Events[0].Tool.DisplaySubject())

	line, err = parseJSONLLine([]byte(`{"type":"item.completed","item":{"id":"ws_123","type":"web_search","query":"latest github release announcement","action":{"type":"search","query":"latest github release announcement","queries":["latest github release announcement"]}}}`))
	require.NoError(t, err)
	assert.Equal(t, "fetch: latest github release announcement", line.Message)
	assert.Empty(t, line.SessionID)
	assert.Empty(t, line.ErrorMessage)
	require.Len(t, line.Events, 1)
	require.NotNil(t, line.Events[0].Tool)
	assert.Equal(t, taskexecutor.ToolKindFetch, line.Events[0].Tool.Kind)
	assert.Equal(t, "latest github release announcement", line.Events[0].Tool.InputSummary)

	line, err = parseJSONLLine([]byte(`{"type":"item.completed","item":{"id":"item_2","type":"agent_message","text":"{\"kind\":\"result\",\"result\":{\"summary\":\"all done\",\"file_paths\":[\"/tmp/hello.txt\"]},\"clarification\":null}"}}`))
	require.NoError(t, err)
	assert.Equal(t, "assistant: all done", line.Message)
	assert.Empty(t, line.SessionID)
	assert.Empty(t, line.ErrorMessage)
	require.Len(t, line.Events, 1)
	require.NotNil(t, line.Events[0].Message)
	assert.Equal(t, "all done", line.Events[0].Message.Text)
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

func writeFakeCodex(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fake-codex.sh")
	script := `#!/bin/sh
set -eu
output=""
args_file="${FAKE_CODEX_ARGS_FILE:-}"
if [ -n "$args_file" ]; then
  : > "$args_file"
  for arg in "$@"; do
    printf '%s\n' "$arg" >> "$args_file"
  done
fi
while [ "$#" -gt 0 ]; do
  case "$1" in
    -o)
      output="$2"
      shift 2
      ;;
    *)
      shift
      ;;
  esac
done

	case "${FAKE_CODEX_MODE:-result}" in
	  result)
	    printf '%s\n' '{"type":"thread.started","thread_id":"thread-123"}'
	    printf '%s\n' '{"type":"item.started","item":{"id":"item_0","type":"todo_list","items":[{"text":"planning changes","completed":false},{"text":"editing files","completed":false},{"text":"running tests","completed":false}],"status":"in_progress"}}'
	    printf '%s\n' '{"type":"item.updated","item":{"id":"item_0","type":"todo_list","items":[{"text":"planning changes","completed":true},{"text":"editing files","completed":false},{"text":"running tests","completed":false}],"status":"in_progress"}}'
	    printf '%s\n' '{"type":"item.completed","item":{"id":"item_1","type":"command_execution","command":"/bin/zsh -lc '\''go test ./...'\''","aggregated_output":"ok\n","exit_code":0,"status":"completed"}}'
	    printf '%s\n' '{"type":"item.completed","item":{"id":"item_2","type":"file_change","changes":[{"path":"/tmp/artifact.md","kind":"add"}],"status":"completed"}}'
	    printf '%s\n' '{"type":"item.completed","item":{"id":"item_3","type":"agent_message","text":"wrapping up"}}'
	    printf '%s\n' '{"kind":"result","result":{"file_paths":["/tmp/artifact.md"]},"clarification":null}' > "$output"
	    ;;
	  clarification)
	    echo '{"type":"thread.started","thread_id":"thread-123"}'
	    printf '%s\n' '{"kind":"clarification","result":null,"clarification":{"questions":[{"question":"What should we do?","why_it_matters":"Need direction","options":[{"label":"A","description":"Option A"},{"label":"B","description":"Option B"}],"multi_select":false}]}}' > "$output"
	    ;;
	  resume-different-thread)
	    echo '{"type":"thread.started","thread_id":"thread-999"}'
	    printf '%s\n' '{"kind":"result","result":{"file_paths":["/tmp/artifact.md"]},"clarification":null}' > "$output"
	    ;;
	  badjson)
	    echo '{bad json'
	    printf '%s\n' '{"kind":"result","result":{"file_paths":["/tmp/artifact.md"]},"clarification":null}' > "$output"
	    ;;
	  invalid-output)
	    echo '{"type":"thread.started","thread_id":"thread-123"}'
	    printf '%s\n' '{"kind":"result","result":{"wrong":true},"clarification":null}' > "$output"
	    ;;
	jsonl-error)
    echo '{"type":"error","message":"invalid_json_schema: boom"}'
    exit 1
    ;;
  large-jsonl)
    echo '{"type":"thread.started","thread_id":"thread-123"}'
    python3 - <<'PY'
import json
blob = "x" * (1024 * 1024 + 32768)
print(json.dumps({"type":"response_item","payload":{"type":"reasoning","encrypted_content":blob}}, separators=(',', ':')))
print(json.dumps({"type":"item.completed","item":{"id":"item_9","type":"agent_message","text":"after huge event"}}, separators=(',', ':')))
PY
    printf '%s\n' '{"kind":"result","result":{"file_paths":["/tmp/artifact.md"]},"clarification":null}' > "$output"
    ;;
  fail)
    echo 'stderr boom' >&2
    exit 2
    ;;
esac
`
	require.NoError(t, os.WriteFile(path, []byte(script), 0o755))
	return path
}

func readGeneratedSchema(t *testing.T, artifactDir string) map[string]interface{} {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(artifactDir, "schemas", "implement.json"))
	require.NoError(t, err)
	var schema map[string]interface{}
	require.NoError(t, json.Unmarshal(data, &schema))
	return schema
}

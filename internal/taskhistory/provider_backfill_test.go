package taskhistory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	appconfig "github.com/LaLanMo/muxagent-cli/internal/config"
	"github.com/LaLanMo/muxagent-cli/internal/taskconfig"
	"github.com/LaLanMo/muxagent-cli/internal/taskdomain"
	"github.com/LaLanMo/muxagent-cli/internal/taskexecutor"
)

func TestLoadFallsBackToClaudeTranscript(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", root)

	task := taskdomain.Task{
		ID:           "task-claude",
		WorkDir:      t.TempDir(),
		ExecutionDir: "/Users/by/Projects/cmdr/muxagent-cli",
	}
	cfg := &taskconfig.Config{Runtime: appconfig.RuntimeClaudeCode}
	run := taskdomain.NodeRun{
		ID:        "run-claude",
		TaskID:    task.ID,
		NodeName:  "upsert_plan",
		Status:    taskdomain.NodeRunRunning,
		SessionID: "session-claude",
	}

	transcriptPath := filepath.Join(root, "projects", "-Users-by-Projects-cmdr-muxagent-cli", "session-claude.jsonl")
	if err := os.MkdirAll(filepath.Dir(transcriptPath), 0o755); err != nil {
		t.Fatalf("mkdir transcript dir: %v", err)
	}
	content := "" +
		"{\"type\":\"assistant\",\"uuid\":\"claude-1\",\"timestamp\":\"2026-04-08T10:00:00Z\",\"sessionId\":\"session-claude\",\"message\":{\"id\":\"msg-1\",\"role\":\"assistant\",\"content\":[{\"type\":\"text\",\"id\":\"part-1\",\"text\":\"planning\"},{\"type\":\"tool_use\",\"id\":\"toolu-1\",\"name\":\"Read\",\"input\":{\"file_path\":\"/tmp/plan.md\"}}]}}\n" +
		"{\"type\":\"user\",\"uuid\":\"claude-2\",\"timestamp\":\"2026-04-08T10:00:01Z\",\"sessionId\":\"session-claude\",\"message\":{\"role\":\"user\",\"content\":[{\"type\":\"tool_result\",\"tool_use_id\":\"toolu-1\",\"content\":\"Structured output provided successfully\",\"is_error\":false}]}}\n"
	if err := os.WriteFile(transcriptPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write transcript: %v", err)
	}

	result, err := Load(task, cfg, run)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := result.Provenance; got != "provider_backfilled" {
		t.Fatalf("provenance = %q, want provider_backfilled", got)
	}
	if got := result.Completeness; got != "open" {
		t.Fatalf("completeness = %q, want open", got)
	}
	if got := len(result.Events); got != 2 {
		t.Fatalf("event count = %d, want 2", got)
	}
	if got := result.Events[0].Message.Text; got != "planning" {
		t.Fatalf("message text = %q, want planning", got)
	}
	if got := result.Events[1].Tool.Name; got != "Read" {
		t.Fatalf("tool name = %q, want Read", got)
	}
}

func TestLoadBackfillsClaudeProgressFramesAndCamelCaseToolResult(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", root)

	task := taskdomain.Task{
		ID:           "task-claude-progress",
		WorkDir:      t.TempDir(),
		ExecutionDir: "/Users/by/Projects/cmdr/muxagent-cli",
	}
	cfg := &taskconfig.Config{Runtime: appconfig.RuntimeClaudeCode}
	run := taskdomain.NodeRun{
		ID:        "run-claude-progress",
		TaskID:    task.ID,
		NodeName:  "upsert_plan",
		Status:    taskdomain.NodeRunDone,
		SessionID: "session-claude-progress",
	}

	transcriptPath := filepath.Join(root, "projects", "-Users-by-Projects-cmdr-muxagent-cli", "session-claude-progress.jsonl")
	if err := os.MkdirAll(filepath.Dir(transcriptPath), 0o755); err != nil {
		t.Fatalf("mkdir transcript dir: %v", err)
	}
	content := "" +
		"{\"type\":\"progress\",\"uuid\":\"outer-1\",\"timestamp\":\"2026-04-08T10:00:00Z\",\"sessionId\":\"session-claude-progress\",\"data\":{\"message\":{\"type\":\"assistant\",\"timestamp\":\"2026-04-08T10:00:00Z\",\"message\":{\"id\":\"msg-1\",\"role\":\"assistant\",\"content\":[{\"type\":\"tool_use\",\"id\":\"toolu-read\",\"name\":\"Read\",\"input\":{\"file_path\":\"/tmp/plan.md\"}}]}}}}\n" +
		"{\"type\":\"progress\",\"uuid\":\"outer-2\",\"timestamp\":\"2026-04-08T10:00:01Z\",\"sessionId\":\"session-claude-progress\",\"data\":{\"message\":{\"type\":\"user\",\"message\":{\"role\":\"user\",\"content\":[{\"tool_use_id\":\"toolu-read\",\"type\":\"tool_result\",\"content\":\"File read\",\"is_error\":false}]},\"toolUseResult\":{\"filePath\":\"/tmp/plan.md\"}}}}\n"
	if err := os.WriteFile(transcriptPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write transcript: %v", err)
	}

	result, err := Load(task, cfg, run)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := result.Provenance; got != "provider_backfilled" {
		t.Fatalf("provenance = %q, want provider_backfilled", got)
	}
	if got := result.Completeness; got != "complete" {
		t.Fatalf("completeness = %q, want complete", got)
	}
	if got := len(result.Events); got != 2 {
		t.Fatalf("event count = %d, want 2", got)
	}
	if got := result.Events[0].Tool.Kind; got != string(taskexecutor.ToolKindRead) {
		t.Fatalf("tool kind = %q, want read", got)
	}
	if got := result.Events[1].Tool.InputSummary; got != "/tmp/plan.md" {
		t.Fatalf("tool input summary = %q, want /tmp/plan.md", got)
	}
}

func TestLoadFallsBackToCodexTranscript(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CODEX_HOME", root)

	task := taskdomain.Task{
		ID:           "task-codex",
		WorkDir:      t.TempDir(),
		ExecutionDir: "/Users/by/Projects/cmdr/muxagent-cli",
	}
	cfg := &taskconfig.Config{Runtime: appconfig.RuntimeCodex}
	run := taskdomain.NodeRun{
		ID:        "run-codex",
		TaskID:    task.ID,
		NodeName:  "handle_request",
		Status:    taskdomain.NodeRunRunning,
		SessionID: "019d-test-session",
	}

	transcriptPath := filepath.Join(root, "sessions", "2026", "04", "08", "rollout-2026-04-08T10-00-00-019d-test-session.jsonl")
	if err := os.MkdirAll(filepath.Dir(transcriptPath), 0o755); err != nil {
		t.Fatalf("mkdir transcript dir: %v", err)
	}
	content := "" +
		"{\"timestamp\":\"2026-04-08T10:00:00Z\",\"type\":\"session_meta\",\"payload\":{\"id\":\"019d-test-session\",\"cwd\":\"/Users/by/Projects/cmdr/muxagent-cli\"}}\n" +
		"{\"timestamp\":\"2026-04-08T10:00:01Z\",\"type\":\"response_item\",\"payload\":{\"type\":\"function_call\",\"call_id\":\"call-1\",\"name\":\"shell_command\",\"arguments\":\"{\\\"command\\\":[\\\"bash\\\",\\\"-lc\\\",\\\"pwd\\\"],\\\"workdir\\\":\\\"/tmp/project\\\"}\"}}\n" +
		"{\"timestamp\":\"2026-04-08T10:00:02Z\",\"type\":\"response_item\",\"payload\":{\"type\":\"function_call_output\",\"call_id\":\"call-1\",\"output\":\"/tmp/project\"}}\n" +
		"{\"timestamp\":\"2026-04-08T10:00:03Z\",\"type\":\"response_item\",\"payload\":{\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"done\"}]}}\n" +
		"{\"timestamp\":\"2026-04-08T10:00:04Z\",\"type\":\"event_msg\",\"payload\":{\"type\":\"task_complete\"}}\n"
	if err := os.WriteFile(transcriptPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write transcript: %v", err)
	}

	result, err := Load(task, cfg, run)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := result.Provenance; got != "provider_backfilled" {
		t.Fatalf("provenance = %q, want provider_backfilled", got)
	}
	if got := result.Completeness; got != "complete" {
		t.Fatalf("completeness = %q, want complete", got)
	}
	if got := len(result.Events); got != 3 {
		t.Fatalf("event count = %d, want 3", got)
	}
	if got := result.Events[0].Tool.InputSummary; got != "pwd" {
		t.Fatalf("tool input summary = %q, want pwd", got)
	}
	if got := result.Events[1].Tool.OutputText; got != "/tmp/project" {
		t.Fatalf("tool output = %q, want /tmp/project", got)
	}
	if got := result.Events[2].Message.Text; got != "done" {
		t.Fatalf("assistant text = %q, want done", got)
	}
}

func TestLoadBackfillsCodexShellFailures(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CODEX_HOME", root)

	task := taskdomain.Task{
		ID:           "task-codex-shell-failure",
		WorkDir:      t.TempDir(),
		ExecutionDir: "/Users/by/Projects/cmdr/muxagent-cli",
	}
	cfg := &taskconfig.Config{Runtime: appconfig.RuntimeCodex}
	run := taskdomain.NodeRun{
		ID:        "run-codex-shell-failure",
		TaskID:    task.ID,
		NodeName:  "handle_request",
		Status:    taskdomain.NodeRunFailed,
		SessionID: "019d-shell-failure",
	}

	transcriptPath := filepath.Join(root, "sessions", "2026", "04", "08", "rollout-2026-04-08T10-00-00-019d-shell-failure.jsonl")
	if err := os.MkdirAll(filepath.Dir(transcriptPath), 0o755); err != nil {
		t.Fatalf("mkdir transcript dir: %v", err)
	}
	content := "" +
		"{\"timestamp\":\"2026-04-08T10:00:00Z\",\"type\":\"session_meta\",\"payload\":{\"id\":\"019d-shell-failure\"}}\n" +
		"{\"timestamp\":\"2026-04-08T10:00:01Z\",\"type\":\"response_item\",\"payload\":{\"type\":\"function_call\",\"call_id\":\"call-1\",\"name\":\"shell_command\",\"arguments\":\"{\\\"command\\\":[\\\"bash\\\",\\\"-lc\\\",\\\"exit 2\\\"],\\\"workdir\\\":\\\"/tmp/project\\\"}\"}}\n" +
		"{\"timestamp\":\"2026-04-08T10:00:02Z\",\"type\":\"response_item\",\"payload\":{\"type\":\"function_call_output\",\"call_id\":\"call-1\",\"output\":\"Exit code: 2\\nWall time: 0.1 seconds\\nOutput:\\npermission denied\\n\"}}\n"
	if err := os.WriteFile(transcriptPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write transcript: %v", err)
	}

	result, err := Load(task, cfg, run)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := result.Completeness; got != "complete" {
		t.Fatalf("completeness = %q, want complete", got)
	}
	if got := len(result.Events); got != 2 {
		t.Fatalf("event count = %d, want 2", got)
	}
	if got := result.Events[1].Tool.Status; got != string(taskexecutor.ToolStatusFailed) {
		t.Fatalf("tool status = %q, want failed", got)
	}
	if got := result.Events[1].Tool.Kind; got != string(taskexecutor.ToolKindShell) {
		t.Fatalf("tool kind = %q, want shell", got)
	}
}

func TestLoadMergesPersistedHistoryWithProviderTail(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CODEX_HOME", root)

	workDir := t.TempDir()
	task := taskdomain.Task{
		ID:           "task-merged",
		WorkDir:      workDir,
		ExecutionDir: "/Users/by/Projects/cmdr/muxagent-cli",
	}
	cfg := &taskconfig.Config{Runtime: appconfig.RuntimeCodex}
	run := taskdomain.NodeRun{
		ID:        "run-merged",
		TaskID:    task.ID,
		NodeName:  "handle_request",
		Status:    taskdomain.NodeRunDone,
		SessionID: "019d-merged-session",
	}

	if err := Append(workDir, task.ID, run.ID, taskexecutor.Progress{
		SessionID: run.SessionID,
		Events: []taskexecutor.StreamEvent{{
			EventID:    "evt-local-1",
			Seq:        1,
			EmittedAt:  time.Date(2026, 4, 8, 10, 0, 1, 0, time.UTC),
			SessionID:  run.SessionID,
			Kind:       taskexecutor.StreamEventKindTool,
			Provenance: taskexecutor.StreamEventProvenanceExecutorPersisted,
			Tool: &taskexecutor.ToolCall{
				CallID:       "call-1",
				Name:         "shell_command",
				Kind:         taskexecutor.ToolKindShell,
				Status:       taskexecutor.ToolStatusInProgress,
				InputSummary: "pwd",
			},
		}},
	}, time.Date(2026, 4, 8, 10, 0, 1, 0, time.UTC)); err != nil {
		t.Fatalf("append history: %v", err)
	}

	transcriptPath := filepath.Join(root, "sessions", "2026", "04", "08", "rollout-2026-04-08T10-00-00-019d-merged-session.jsonl")
	if err := os.MkdirAll(filepath.Dir(transcriptPath), 0o755); err != nil {
		t.Fatalf("mkdir transcript dir: %v", err)
	}
	content := "" +
		"{\"timestamp\":\"2026-04-08T10:00:00Z\",\"type\":\"session_meta\",\"payload\":{\"id\":\"019d-merged-session\"}}\n" +
		"{\"timestamp\":\"2026-04-08T10:00:01Z\",\"type\":\"response_item\",\"payload\":{\"type\":\"function_call\",\"call_id\":\"call-1\",\"name\":\"shell_command\",\"arguments\":\"{\\\"command\\\":[\\\"bash\\\",\\\"-lc\\\",\\\"pwd\\\"],\\\"workdir\\\":\\\"/tmp/project\\\"}\"}}\n" +
		"{\"timestamp\":\"2026-04-08T10:00:02Z\",\"type\":\"response_item\",\"payload\":{\"type\":\"function_call_output\",\"call_id\":\"call-1\",\"output\":\"Exit code: 0\\nWall time: 0.1 seconds\\nOutput:\\n/tmp/project\\n\"}}\n" +
		"{\"timestamp\":\"2026-04-08T10:00:03Z\",\"type\":\"response_item\",\"payload\":{\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"done\"}]}}\n" +
		"{\"timestamp\":\"2026-04-08T10:00:04Z\",\"type\":\"event_msg\",\"payload\":{\"type\":\"task_complete\"}}\n"
	if err := os.WriteFile(transcriptPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write transcript: %v", err)
	}

	result, err := Load(task, cfg, run)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := result.Provenance; got != "mixed_recovered" {
		t.Fatalf("provenance = %q, want mixed_recovered", got)
	}
	if got := result.Completeness; got != "complete" {
		t.Fatalf("completeness = %q, want complete", got)
	}
	if got := len(result.Events); got != 3 {
		t.Fatalf("event count = %d, want 3", got)
	}
	if got := result.Events[0].Tool.CallID; got != "call-1" {
		t.Fatalf("first tool call_id = %q, want call-1", got)
	}
	if got := result.Events[1].Tool.OutputText; !strings.Contains(got, "/tmp/project") {
		t.Fatalf("merged tool output = %q, want /tmp/project", got)
	}
	if got := result.Events[2].Message.Text; got != "done" {
		t.Fatalf("tail assistant text = %q, want done", got)
	}
}

func TestLoadToleratesTornProviderTranscriptTail(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CODEX_HOME", root)

	task := taskdomain.Task{
		ID:           "task-codex-torn",
		WorkDir:      t.TempDir(),
		ExecutionDir: "/Users/by/Projects/cmdr/muxagent-cli",
	}
	cfg := &taskconfig.Config{Runtime: appconfig.RuntimeCodex}
	run := taskdomain.NodeRun{
		ID:        "run-codex-torn",
		TaskID:    task.ID,
		NodeName:  "handle_request",
		Status:    taskdomain.NodeRunRunning,
		SessionID: "019d-torn-session",
	}

	transcriptPath := filepath.Join(root, "sessions", "2026", "04", "08", "rollout-2026-04-08T10-00-00-019d-torn-session.jsonl")
	if err := os.MkdirAll(filepath.Dir(transcriptPath), 0o755); err != nil {
		t.Fatalf("mkdir transcript dir: %v", err)
	}
	content := "" +
		"{\"timestamp\":\"2026-04-08T10:00:00Z\",\"type\":\"session_meta\",\"payload\":{\"id\":\"019d-torn-session\"}}\n" +
		"{\"timestamp\":\"2026-04-08T10:00:01Z\",\"type\":\"response_item\",\"payload\":{\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"partial ok\"}]}}\n" +
		"{\"timestamp\":\"2026-04-08T10:00:02Z\",\"type\":\"response_item\",\"payload\":{\"type\":\"message\""
	if err := os.WriteFile(transcriptPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write transcript: %v", err)
	}

	result, err := Load(task, cfg, run)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := len(result.Events); got != 1 {
		t.Fatalf("event count = %d, want 1", got)
	}
	if got := result.Events[0].Message.Text; got != "partial ok" {
		t.Fatalf("assistant text = %q, want partial ok", got)
	}
}

func TestLoadPrefersPersistedHistoryWithoutProviderFallback(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CODEX_HOME", root)

	workDir := t.TempDir()
	task := taskdomain.Task{
		ID:           "task-persisted",
		WorkDir:      workDir,
		ExecutionDir: "/Users/by/Projects/cmdr/muxagent-cli",
	}
	cfg := &taskconfig.Config{Runtime: appconfig.RuntimeCodex}
	run := taskdomain.NodeRun{
		ID:        "run-persisted",
		TaskID:    task.ID,
		NodeName:  "handle_request",
		Status:    taskdomain.NodeRunDone,
		SessionID: "session-persisted",
	}

	if err := Append(workDir, task.ID, run.ID, taskexecutorProgress("persisted"), time.Date(2026, 4, 8, 10, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("append history: %v", err)
	}

	result, err := Load(task, cfg, run)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := result.Provenance; got != "executor_persisted" {
		t.Fatalf("provenance = %q, want executor_persisted", got)
	}
	if got := len(result.Events); got != 1 {
		t.Fatalf("event count = %d, want 1", got)
	}
	if got := result.Events[0].Message.Text; got != "persisted" {
		t.Fatalf("message text = %q, want persisted", got)
	}
}

func taskexecutorProgress(text string) taskexecutor.Progress {
	return taskexecutor.Progress{
		SessionID: "session-persisted",
		Events: []taskexecutor.StreamEvent{{
			EventID:    "evt-1",
			Seq:        1,
			EmittedAt:  time.Date(2026, 4, 8, 10, 0, 0, 0, time.UTC),
			SessionID:  "session-persisted",
			Kind:       taskexecutor.StreamEventKindMessage,
			Provenance: taskexecutor.StreamEventProvenanceExecutorPersisted,
			Message: &taskexecutor.MessagePart{
				MessageID: "msg-1",
				PartID:    "part-1",
				Role:      taskexecutor.MessageRoleAssistant,
				Type:      taskexecutor.MessagePartTypeText,
				Text:      text,
			},
		}},
	}
}

package appserver

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	appconfig "github.com/LaLanMo/muxagent-cli/internal/config"
	"github.com/LaLanMo/muxagent-cli/internal/taskconfig"
	"github.com/LaLanMo/muxagent-cli/internal/taskdomain"
	"github.com/LaLanMo/muxagent-cli/internal/taskexecutor"
	"github.com/LaLanMo/muxagent-cli/internal/taskhistory"
	"github.com/LaLanMo/muxagent-cli/internal/taskruntime"
	"github.com/LaLanMo/muxagent-cli/internal/taskstore"
)

func TestServerRejectsCallsBeforeInitialize(t *testing.T) {
	server := newTestServer(t)

	var in bytes.Buffer
	var out bytes.Buffer
	writeRequestFrame(t, &in, 1, methodServiceStatus, map[string]any{})

	if err := server.Serve(context.Background(), &in, &out); err != nil {
		t.Fatalf("serve: %v", err)
	}

	messages := readFramesAsMaps(t, out.Bytes())
	if len(messages) != 1 {
		t.Fatalf("message count = %d, want 1", len(messages))
	}
	if got := nestedFloat(messages[0], "error", "code"); int(got) != int(errorCodeNotInitialized) {
		t.Fatalf("error code = %v, want %d", got, errorCodeNotInitialized)
	}
}

func TestServerWorkspaceLifecycle(t *testing.T) {
	stateDir := filepath.Join(t.TempDir(), "appserver")
	workspacePath := t.TempDir()

	server := newTestServerAtPath(t, stateDir)
	var firstIn bytes.Buffer
	var firstOut bytes.Buffer
	writeRequestFrame(t, &firstIn, 1, methodInitialize, map[string]any{"protocol_version": protocolVersion})
	writeRequestFrame(t, &firstIn, 2, methodWorkspaceAdd, map[string]any{
		"path":         workspacePath,
		"display_name": "cmdr",
	})
	writeRequestFrame(t, &firstIn, 3, methodServiceShutdown, map[string]any{})

	if err := server.Serve(context.Background(), &firstIn, &firstOut); err != nil {
		t.Fatalf("serve add flow: %v", err)
	}

	firstMessages := readFramesAsMaps(t, firstOut.Bytes())
	addResponse := responseByID(t, firstMessages, 2)
	workspaceID, ok := nestedString(addResponse, "result", "workspace", "workspace_id")
	if !ok || workspaceID == "" {
		t.Fatalf("workspace_id missing in add response: %#v", addResponse)
	}

	server = newTestServerAtPath(t, stateDir)
	var secondIn bytes.Buffer
	var secondOut bytes.Buffer
	writeRequestFrame(t, &secondIn, 1, methodInitialize, map[string]any{"protocol_version": protocolVersion})
	writeRequestFrame(t, &secondIn, 2, methodServiceStatus, map[string]any{})
	writeRequestFrame(t, &secondIn, 3, methodWorkspaceUpdate, map[string]any{
		"workspace_id": workspaceID,
		"display_name": "cmdr core",
	})
	writeRequestFrame(t, &secondIn, 4, methodWorkspaceGet, map[string]any{
		"workspace_id": workspaceID,
	})
	writeRequestFrame(t, &secondIn, 5, methodWorkspaceList, map[string]any{})
	writeRequestFrame(t, &secondIn, 6, methodWorkspaceRemove, map[string]any{
		"workspace_id": workspaceID,
	})
	writeRequestFrame(t, &secondIn, 7, methodServiceShutdown, map[string]any{})

	if err := server.Serve(context.Background(), &secondIn, &secondOut); err != nil {
		t.Fatalf("serve lifecycle: %v", err)
	}

	secondMessages := readFramesAsMaps(t, secondOut.Bytes())
	statusResponse := responseByID(t, secondMessages, 2)
	updateResponse := responseByID(t, secondMessages, 3)
	getResponse := responseByID(t, secondMessages, 4)
	listResponse := responseByID(t, secondMessages, 5)
	removeResponse := responseByID(t, secondMessages, 6)

	if got := nestedFloat(statusResponse, "result", "workspace_count"); int(got) != 1 {
		t.Fatalf("workspace_count = %v, want 1", got)
	}
	if got := nestedStringMust(t, updateResponse, "result", "workspace", "display_name"); got != "cmdr core" {
		t.Fatalf("updated display_name = %q, want %q", got, "cmdr core")
	}
	if got := nestedStringMust(t, getResponse, "result", "workspace", "display_name"); got != "cmdr core" {
		t.Fatalf("get display_name = %q, want %q", got, "cmdr core")
	}
	if got := len(nestedSlice(t, listResponse, "result", "workspaces")); got != 1 {
		t.Fatalf("workspace list count = %d, want 1", got)
	}
	if removed, _ := nestedBool(removeResponse, "result", "removed"); !removed {
		t.Fatalf("removed = false, want true")
	}

	var sawAddNotification bool
	var sawUpdateNotification bool
	var sawRemoveNotification bool
	for _, message := range append(firstMessages, secondMessages...) {
		if method, _ := nestedString(message, "method"); method != methodNotification {
			continue
		}
		kind, _ := nestedString(message, "params", "kind")
		switch kind {
		case notificationWorkspaceAdded:
			sawAddNotification = true
		case notificationWorkspaceUpdated:
			sawUpdateNotification = true
		case notificationWorkspaceRemoved:
			sawRemoveNotification = true
		}
	}
	if !sawAddNotification || !sawUpdateNotification || !sawRemoveNotification {
		t.Fatalf("notifications missing: add=%v update=%v remove=%v", sawAddNotification, sawUpdateNotification, sawRemoveNotification)
	}
}

func TestServerTaskReadFlows(t *testing.T) {
	stateDir := filepath.Join(t.TempDir(), "appserver")
	workspacePath := t.TempDir()
	server := newTestServerWithOptions(t, stateDir, testServerOptions{
		loadCatalog: func() (*taskconfig.Catalog, error) {
			cfg, err := taskconfig.LoadDefault()
			if err != nil {
				return nil, err
			}
			return &taskconfig.Catalog{
				DefaultAlias: "default",
				Entries: []taskconfig.CatalogEntry{{
					Alias:     "default",
					Path:      "/tmp/default/config.yaml",
					Config:    cfg,
					Builtin:   true,
					BuiltinID: "default",
				}},
			}, nil
		},
		loadRegistry: func() (taskconfig.Registry, error) {
			return taskconfig.Registry{
				DefaultAlias: "default",
				Configs: []taskconfig.RegistryEntry{{
					Alias: "default",
					Path:  "default",
				}},
			}, nil
		},
		loadTaskLaunchPreferences: func() appconfig.TaskLaunchPreferences {
			return appconfig.TaskLaunchPreferences{UseWorktree: true}
		},
	})
	workspace, _, err := server.registry.Add(workspacePath, "cmdr")
	if err != nil {
		t.Fatalf("add workspace: %v", err)
	}
	taskID, awaitingRunID := seedAwaitingTask(t, workspacePath)
	server.markInitialized()

	listResultAny, _, _, rpcErr := server.handleRequest(context.Background(), request{
		Method: methodTaskList,
		Params: mustRawParams(t, taskListParams{WorkspaceID: workspace.WorkspaceID}),
	})
	if rpcErr != nil {
		t.Fatalf("task.list rpc error: %+v", rpcErr)
	}
	listResult := listResultAny.(taskListResult)
	if got := len(listResult.Tasks); got != 1 {
		t.Fatalf("task.list count = %d, want 1", got)
	}
	if got := listResult.Tasks[0].Status; got != string(taskdomain.TaskStatusAwaitingUser) {
		t.Fatalf("task.list status = %q, want %q", got, taskdomain.TaskStatusAwaitingUser)
	}

	getResultAny, _, _, rpcErr := server.handleRequest(context.Background(), request{
		Method: methodTaskGet,
		Params: mustRawParams(t, taskGetParams{WorkspaceID: workspace.WorkspaceID, TaskID: taskID}),
	})
	if rpcErr != nil {
		t.Fatalf("task.get rpc error: %+v", rpcErr)
	}
	getResult := getResultAny.(taskGetResult)
	if getResult.InputRequest == nil {
		t.Fatal("task.get input_request = nil, want value")
	}
	if got := getResult.InputRequest.NodeRunID; got != awaitingRunID {
		t.Fatalf("task.get input_request.node_run_id = %q, want %q", got, awaitingRunID)
	}

	server.handleRuntimeEvent(workspace.WorkspaceID, taskruntime.RunEvent{
		Type:      taskruntime.EventNodeStarted,
		TaskID:    taskID,
		NodeRunID: awaitingRunID,
		NodeName:  "approve_plan",
	})
	server.handleRuntimeEvent(workspace.WorkspaceID, taskruntime.RunEvent{
		Type:      taskruntime.EventNodeProgress,
		TaskID:    taskID,
		NodeRunID: awaitingRunID,
		NodeName:  "approve_plan",
		Progress: &taskruntime.ProgressInfo{
			Message: "approval pending",
			Events: []taskexecutor.StreamEvent{
				{
					Kind: taskexecutor.StreamEventKindTool,
					Tool: &taskexecutor.ToolCall{
						Name:         "Read",
						Kind:         taskexecutor.ToolKindRead,
						Status:       taskexecutor.ToolStatusCompleted,
						InputSummary: "/tmp/plan.md",
					},
				},
			},
		},
	})

	getResultAny, _, _, rpcErr = server.handleRequest(context.Background(), request{
		Method: methodTaskGet,
		Params: mustRawParams(t, taskGetParams{WorkspaceID: workspace.WorkspaceID, TaskID: taskID}),
	})
	if rpcErr != nil {
		t.Fatalf("task.get live output rpc error: %+v", rpcErr)
	}
	getResult = getResultAny.(taskGetResult)
	if got := getResult.LiveOutputRunID; got != awaitingRunID {
		t.Fatalf("task.get live_output_run_id = %q, want %q", got, awaitingRunID)
	}
	if got := len(getResult.LiveEvents); got != 1 {
		t.Fatalf("task.get live_events length = %d, want 1", got)
	}
	if got := getResult.LiveEvents[0].Kind; got != string(taskexecutor.StreamEventKindTool) {
		t.Fatalf("task.get live_events[0].kind = %q, want %q", got, taskexecutor.StreamEventKindTool)
	}
	if got := getResult.LiveEvents[0].InputSummary; got != "/tmp/plan.md" {
		t.Fatalf("task.get live_events[0].input_summary = %q, want %q", got, "/tmp/plan.md")
	}

	err = taskhistory.Append(workspacePath, taskID, awaitingRunID, taskexecutor.Progress{
		SessionID: "session-123",
		Events: []taskexecutor.StreamEvent{
			{
				EventID:    "evt-read",
				Seq:        1,
				EmittedAt:  time.Date(2026, 4, 3, 12, 1, 0, 0, time.UTC),
				SessionID:  "session-123",
				Kind:       taskexecutor.StreamEventKindTool,
				Provenance: taskexecutor.StreamEventProvenanceExecutorPersisted,
				Tool: &taskexecutor.ToolCall{
					Name:         "Read",
					Kind:         taskexecutor.ToolKindRead,
					Status:       taskexecutor.ToolStatusCompleted,
					InputSummary: "plan.md",
				},
			},
		},
	}, time.Date(2026, 4, 3, 12, 1, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("append history: %v", err)
	}
	err = taskhistory.Append(workspacePath, taskID, awaitingRunID, taskexecutor.Progress{
		SessionID: "session-123",
		Events: []taskexecutor.StreamEvent{
			{
				EventID:    "evt-approval",
				Seq:        2,
				EmittedAt:  time.Date(2026, 4, 3, 12, 1, 15, 0, time.UTC),
				SessionID:  "session-123",
				Kind:       taskexecutor.StreamEventKindMessage,
				Provenance: taskexecutor.StreamEventProvenanceExecutorPersisted,
				Message: &taskexecutor.MessagePart{
					MessageID: "msg-approval",
					PartID:    "part-approval",
					Role:      taskexecutor.MessageRoleAssistant,
					Type:      taskexecutor.MessagePartTypeText,
					Text:      "approval pending",
				},
			},
		},
	}, time.Date(2026, 4, 3, 12, 1, 15, 0, time.UTC))
	if err != nil {
		t.Fatalf("append message history: %v", err)
	}

	historyResultAny, _, _, rpcErr := server.handleRequest(context.Background(), request{
		Method: methodTaskRunHistory,
		Params: mustRawParams(t, taskRunHistoryParams{
			WorkspaceID: workspace.WorkspaceID,
			TaskID:      taskID,
			NodeRunID:   awaitingRunID,
		}),
	})
	if rpcErr != nil {
		t.Fatalf("task.run_history rpc error: %+v", rpcErr)
	}
	historyResult := historyResultAny.(taskRunHistoryResult)
	if got := historyResult.Provenance; got != "executor_persisted" {
		t.Fatalf("task.run_history provenance = %q, want executor_persisted", got)
	}
	if got := historyResult.Completeness; got != "complete" {
		t.Fatalf("task.run_history completeness = %q, want complete", got)
	}
	if got := historyResult.SessionID; got != "session-123" {
		t.Fatalf("task.run_history session_id = %q, want session-123", got)
	}
	if got := len(historyResult.Events); got != 2 {
		t.Fatalf("task.run_history event count = %d, want 2", got)
	}
	if got := historyResult.Events[0].Name; got != "Read" {
		t.Fatalf("task.run_history first tool name = %q, want Read", got)
	}
	if got := historyResult.Events[1].Text; got != "approval pending" {
		t.Fatalf("task.run_history second message = %q, want approval pending", got)
	}

	inputResultAny, _, _, rpcErr := server.handleRequest(context.Background(), request{
		Method: methodTaskInputRequest,
		Params: mustRawParams(t, taskInputRequestParams{
			WorkspaceID: workspace.WorkspaceID,
			TaskID:      taskID,
			NodeRunID:   awaitingRunID,
		}),
	})
	if rpcErr != nil {
		t.Fatalf("task.input_request rpc error: %+v", rpcErr)
	}
	inputResult := inputResultAny.(taskInputRequestResult)
	if inputResult.InputRequest == nil || inputResult.InputRequest.Kind != string(taskruntime.InputKindHumanNode) {
		t.Fatalf("task.input_request kind = %#v, want human_node", inputResult.InputRequest)
	}

	artifactResultAny, _, _, rpcErr := server.handleRequest(context.Background(), request{
		Method: methodArtifactList,
		Params: mustRawParams(t, artifactListParams{WorkspaceID: workspace.WorkspaceID, TaskID: taskID}),
	})
	if rpcErr != nil {
		t.Fatalf("artifact.list rpc error: %+v", rpcErr)
	}
	artifactResult := artifactResultAny.(artifactListResult)
	if got := len(artifactResult.Artifacts); got != 1 {
		t.Fatalf("artifact.list count = %d, want 1", got)
	}
	if got := artifactResult.Artifacts[0].PreviewName; got != "plan.md" {
		t.Fatalf("artifact preview_name = %q, want plan.md", got)
	}

	catalogResultAny, _, _, rpcErr := server.handleRequest(context.Background(), request{
		Method: methodConfigCatalog,
		Params: mustRawParams(t, map[string]any{}),
	})
	if rpcErr != nil {
		t.Fatalf("config.catalog rpc error: %+v", rpcErr)
	}
	catalogResult := catalogResultAny.(configCatalogResult)
	if catalogResult.DefaultAlias != "default" {
		t.Fatalf("default_alias = %q, want default", catalogResult.DefaultAlias)
	}
	if !catalogResult.DefaultUseWorktree {
		t.Fatal("default_use_worktree = false, want true")
	}
}

func TestServerTaskRunHistoryFallsBackToProviderTranscript(t *testing.T) {
	workspacePath := t.TempDir()
	codexHome := t.TempDir()
	t.Setenv("CODEX_HOME", codexHome)

	server := newTestServer(t)
	workspace, _, err := server.registry.Add(workspacePath, "cmdr")
	if err != nil {
		t.Fatalf("add workspace: %v", err)
	}
	server.markInitialized()

	store, err := taskstore.Open(workspacePath)
	if err != nil {
		t.Fatalf("open task store: %v", err)
	}
	defer func() { _ = store.Close() }()

	taskID := "task-provider-history"
	configPath, err := taskconfig.DefaultConfigPath()
	if err != nil {
		t.Fatalf("default config path: %v", err)
	}
	materialized, err := taskconfig.Materialize(workspacePath, taskID, configPath)
	if err != nil {
		t.Fatalf("materialize config: %v", err)
	}
	configBytes, err := os.ReadFile(materialized.ConfigPath)
	if err != nil {
		t.Fatalf("read materialized config: %v", err)
	}
	configText := strings.Replace(string(configBytes), "runtime: claude-code", "runtime: codex", 1)
	configText = strings.Replace(configText, "runtime: default", "runtime: codex", 1)
	if err := os.WriteFile(materialized.ConfigPath, []byte(configText), 0o644); err != nil {
		t.Fatalf("force codex runtime: %v", err)
	}

	now := time.Date(2026, 4, 8, 10, 0, 0, 0, time.UTC)
	task := taskdomain.Task{
		ID:           taskID,
		Description:  "provider history",
		ConfigAlias:  "default",
		ConfigPath:   materialized.ConfigPath,
		WorkDir:      taskstore.NormalizeWorkDir(workspacePath),
		ExecutionDir: taskstore.NormalizeWorkDir(workspacePath),
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := store.CreateTask(context.Background(), task); err != nil {
		t.Fatalf("create task: %v", err)
	}
	run := taskdomain.NodeRun{
		ID:        "run-provider-history",
		TaskID:    taskID,
		NodeName:  "draft_plan",
		Status:    taskdomain.NodeRunRunning,
		SessionID: "019d-provider-history",
		StartedAt: now,
	}
	if err := store.SaveNodeRun(context.Background(), run); err != nil {
		t.Fatalf("save run: %v", err)
	}

	transcriptPath := filepath.Join(codexHome, "sessions", "2026", "04", "08", "rollout-2026-04-08T10-00-00-019d-provider-history.jsonl")
	if err := os.MkdirAll(filepath.Dir(transcriptPath), 0o755); err != nil {
		t.Fatalf("mkdir transcript dir: %v", err)
	}
	content := "" +
		"{\"timestamp\":\"2026-04-08T10:00:00Z\",\"type\":\"session_meta\",\"payload\":{\"id\":\"019d-provider-history\",\"cwd\":\"" + task.ExecutionDir + "\"}}\n" +
		"{\"timestamp\":\"2026-04-08T10:00:01Z\",\"type\":\"response_item\",\"payload\":{\"type\":\"function_call\",\"call_id\":\"call-1\",\"name\":\"exec_command\",\"arguments\":\"{\\\"cmd\\\":\\\"pwd\\\"}\"}}\n" +
		"{\"timestamp\":\"2026-04-08T10:00:02Z\",\"type\":\"response_item\",\"payload\":{\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"done\"}]}}\n" +
		"{\"timestamp\":\"2026-04-08T10:00:03Z\",\"type\":\"event_msg\",\"payload\":{\"type\":\"task_complete\"}}\n"
	if err := os.WriteFile(transcriptPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write transcript: %v", err)
	}

	historyResultAny, _, _, rpcErr := server.handleRequest(context.Background(), request{
		Method: methodTaskRunHistory,
		Params: mustRawParams(t, taskRunHistoryParams{
			WorkspaceID: workspace.WorkspaceID,
			TaskID:      taskID,
			NodeRunID:   run.ID,
		}),
	})
	if rpcErr != nil {
		t.Fatalf("task.run_history rpc error: %+v", rpcErr)
	}
	historyResult := historyResultAny.(taskRunHistoryResult)
	if got := historyResult.Provenance; got != "provider_backfilled" {
		t.Fatalf("task.run_history provenance = %q, want provider_backfilled", got)
	}
	if got := historyResult.Completeness; got != "complete" {
		t.Fatalf("task.run_history completeness = %q, want complete", got)
	}
	if got := len(historyResult.Events); got != 2 {
		t.Fatalf("task.run_history event count = %d, want 2", got)
	}
	if got := historyResult.Events[0].Name; got != "exec_command" {
		t.Fatalf("task.run_history first tool name = %q, want exec_command", got)
	}
	if got := historyResult.Events[1].Text; got != "done" {
		t.Fatalf("task.run_history assistant text = %q, want done", got)
	}
}

func TestServerTaskRunHistoryIgnoresPartialTrailingChunk(t *testing.T) {
	stateDir := filepath.Join(t.TempDir(), "appserver")
	workspacePath := t.TempDir()
	server := newTestServerAtPath(t, stateDir)
	workspace, _, err := server.registry.Add(workspacePath, "cmdr")
	if err != nil {
		t.Fatalf("add workspace: %v", err)
	}
	taskID, nodeRunID := seedAwaitingTask(t, workspacePath)
	server.markInitialized()

	recordedAt := time.Date(2026, 4, 8, 10, 0, 0, 0, time.UTC)
	if err := taskhistory.Append(workspacePath, taskID, nodeRunID, taskexecutor.Progress{
		SessionID: "session-123",
		Events: []taskexecutor.StreamEvent{
			{
				EventID:    "evt-first",
				Seq:        1,
				EmittedAt:  recordedAt,
				SessionID:  "session-123",
				Kind:       taskexecutor.StreamEventKindMessage,
				Provenance: taskexecutor.StreamEventProvenanceExecutorPersisted,
				Message: &taskexecutor.MessagePart{
					MessageID: "msg-first",
					PartID:    "part-first",
					Role:      taskexecutor.MessageRoleAssistant,
					Type:      taskexecutor.MessagePartTypeText,
					Text:      "first chunk",
				},
			},
		},
	}, recordedAt); err != nil {
		t.Fatalf("append first event: %v", err)
	}
	path := taskstore.RunHistoryPath(workspacePath, taskID, nodeRunID)
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		t.Fatalf("open history file: %v", err)
	}
	if _, err := file.WriteString(`{"event_id":"evt-partial","seq":2,"kind":"message","message":{"text":"partial"}`); err != nil {
		_ = file.Close()
		t.Fatalf("append partial trailing record: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close history file: %v", err)
	}

	resultAny, _, _, rpcErr := server.handleRequest(context.Background(), request{
		Method: methodTaskRunHistory,
		Params: mustRawParams(t, taskRunHistoryParams{
			WorkspaceID: workspace.WorkspaceID,
			TaskID:      taskID,
			NodeRunID:   nodeRunID,
		}),
	})
	if rpcErr != nil {
		t.Fatalf("task.run_history rpc error: %+v", rpcErr)
	}
	result := resultAny.(taskRunHistoryResult)
	if got := len(result.Events); got != 1 {
		t.Fatalf("history event count = %d, want 1", got)
	}
	if got := result.Events[0].Text; got != "first chunk" {
		t.Fatalf("first history message = %q, want first chunk", got)
	}
}

func TestServerTaskRunHistoryReadsPersistedHistoryWithoutConfig(t *testing.T) {
	stateDir := filepath.Join(t.TempDir(), "appserver")
	workspacePath := t.TempDir()
	server := newTestServerAtPath(t, stateDir)
	workspace, _, err := server.registry.Add(workspacePath, "cmdr")
	if err != nil {
		t.Fatalf("add workspace: %v", err)
	}
	taskID, nodeRunID := seedAwaitingTask(t, workspacePath)
	server.markInitialized()

	recordedAt := time.Date(2026, 4, 8, 10, 0, 0, 0, time.UTC)
	if err := taskhistory.Append(workspacePath, taskID, nodeRunID, taskexecutor.Progress{
		SessionID: "session-123",
		Events: []taskexecutor.StreamEvent{{
			EventID:    "evt-first",
			Seq:        1,
			EmittedAt:  recordedAt,
			SessionID:  "session-123",
			Kind:       taskexecutor.StreamEventKindMessage,
			Provenance: taskexecutor.StreamEventProvenanceExecutorPersisted,
			Message: &taskexecutor.MessagePart{
				MessageID: "msg-first",
				PartID:    "part-first",
				Role:      taskexecutor.MessageRoleAssistant,
				Type:      taskexecutor.MessagePartTypeText,
				Text:      "persisted survives",
			},
		}},
	}, recordedAt); err != nil {
		t.Fatalf("append history: %v", err)
	}
	if err := os.Remove(taskstore.ConfigPath(workspacePath, taskID)); err != nil {
		t.Fatalf("remove config: %v", err)
	}

	resultAny, _, _, rpcErr := server.handleRequest(context.Background(), request{
		Method: methodTaskRunHistory,
		Params: mustRawParams(t, taskRunHistoryParams{
			WorkspaceID: workspace.WorkspaceID,
			TaskID:      taskID,
			NodeRunID:   nodeRunID,
		}),
	})
	if rpcErr != nil {
		t.Fatalf("task.run_history rpc error: %+v", rpcErr)
	}
	result := resultAny.(taskRunHistoryResult)
	if got := result.Provenance; got != "executor_persisted" {
		t.Fatalf("history provenance = %q, want executor_persisted", got)
	}
	if got := len(result.Events); got != 1 {
		t.Fatalf("history event count = %d, want 1", got)
	}
	if got := result.Events[0].Text; got != "persisted survives" {
		t.Fatalf("history message = %q, want persisted survives", got)
	}
}

func TestServerTaskGetClearsLiveOutputAfterTerminalEvent(t *testing.T) {
	stateDir := filepath.Join(t.TempDir(), "appserver")
	workspacePath := t.TempDir()
	server := newTestServerAtPath(t, stateDir)
	workspace, _, err := server.registry.Add(workspacePath, "cmdr")
	if err != nil {
		t.Fatalf("add workspace: %v", err)
	}
	taskID, nodeRunID := seedAwaitingTask(t, workspacePath)
	server.markInitialized()

	server.handleRuntimeEvent(workspace.WorkspaceID, taskruntime.RunEvent{
		Type:      taskruntime.EventNodeStarted,
		TaskID:    taskID,
		NodeRunID: nodeRunID,
		NodeName:  "approve_plan",
	})
	server.handleRuntimeEvent(workspace.WorkspaceID, taskruntime.RunEvent{
		Type:      taskruntime.EventNodeProgress,
		TaskID:    taskID,
		NodeRunID: nodeRunID,
		NodeName:  "approve_plan",
		Progress: &taskruntime.ProgressInfo{
			Events: []taskexecutor.StreamEvent{{
				Kind: taskexecutor.StreamEventKindTool,
				Tool: &taskexecutor.ToolCall{
					Name:         "Read",
					Kind:         taskexecutor.ToolKindRead,
					Status:       taskexecutor.ToolStatusCompleted,
					InputSummary: "plan.md",
				},
			}},
		},
	})

	getResultAny, _, _, rpcErr := server.handleRequest(context.Background(), request{
		Method: methodTaskGet,
		Params: mustRawParams(t, taskGetParams{WorkspaceID: workspace.WorkspaceID, TaskID: taskID}),
	})
	if rpcErr != nil {
		t.Fatalf("task.get before terminal rpc error: %+v", rpcErr)
	}
	getResult := getResultAny.(taskGetResult)
	if len(getResult.LiveEvents) == 0 {
		t.Fatal("task.get live_events = empty before terminal event, want events")
	}

	server.handleRuntimeEvent(workspace.WorkspaceID, taskruntime.RunEvent{
		Type:      taskruntime.EventNodeCompleted,
		TaskID:    taskID,
		NodeRunID: nodeRunID,
		NodeName:  "approve_plan",
	})

	getResultAny, _, _, rpcErr = server.handleRequest(context.Background(), request{
		Method: methodTaskGet,
		Params: mustRawParams(t, taskGetParams{WorkspaceID: workspace.WorkspaceID, TaskID: taskID}),
	})
	if rpcErr != nil {
		t.Fatalf("task.get after terminal rpc error: %+v", rpcErr)
	}
	getResult = getResultAny.(taskGetResult)
	if got := len(getResult.LiveEvents); got != 0 {
		t.Fatalf("task.get live_events length after terminal = %d, want 0", got)
	}
	if getResult.LiveOutputRunID != "" {
		t.Fatalf("task.get live_output_run_id after terminal = %q, want empty", getResult.LiveOutputRunID)
	}
}

func TestServerTaskMutationsRouteByWorkspaceAndCorrelateNotifications(t *testing.T) {
	stateDir := filepath.Join(t.TempDir(), "appserver")
	workspacePath := t.TempDir()
	fakeService := newFakeRuntimeService()
	server := newTestServerWithOptions(t, stateDir, testServerOptions{
		runtimeFactory: func(workDir string) (runtimeService, error) {
			if workDir != taskstore.NormalizeWorkDir(workspacePath) {
				t.Fatalf("runtime factory workDir = %q, want %q", workDir, taskstore.NormalizeWorkDir(workspacePath))
			}
			return fakeService, nil
		},
	})
	defer func() { _ = server.runtimes.closeAll() }()

	workspace, _, err := server.registry.Add(workspacePath, "cmdr")
	if err != nil {
		t.Fatalf("add workspace: %v", err)
	}
	server.markInitialized()

	var notifications []notification
	server.setNotificationSink(func(n notification) {
		notifications = append(notifications, n)
	})
	defer server.setNotificationSink(nil)

	startResultAny, _, _, rpcErr := server.handleRequest(context.Background(), request{
		Method: methodTaskStart,
		Params: mustRawParams(t, taskStartParams{
			WorkspaceID:     workspace.WorkspaceID,
			ClientCommandID: "cmd-1",
			Description:     "Ship it",
			ConfigAlias:     "default",
			ConfigPath:      "/tmp/default/config.yaml",
			UseWorktree:     true,
		}),
	})
	if rpcErr != nil {
		t.Fatalf("task.start rpc error: %+v", rpcErr)
	}
	startResult := startResultAny.(commandAcceptedResult)
	if !startResult.Accepted || startResult.ClientCommandID != "cmd-1" {
		t.Fatalf("task.start accepted result = %#v", startResult)
	}

	dispatches := fakeService.Dispatched()
	if len(dispatches) != 1 {
		t.Fatalf("dispatch count = %d, want 1", len(dispatches))
	}
	if got := dispatches[0].WorkDir; got != taskstore.NormalizeWorkDir(workspacePath) {
		t.Fatalf("dispatch work_dir = %q, want %q", got, taskstore.NormalizeWorkDir(workspacePath))
	}

	server.handleRuntimeEvent(workspace.WorkspaceID, taskruntime.RunEvent{
		Type:   taskruntime.EventTaskCreated,
		TaskID: "task-123",
	})
	if len(notifications) != 1 {
		t.Fatalf("notification count = %d, want 1", len(notifications))
	}
	params := notifications[0].Params.(notificationParams)
	if params.Kind != string(taskruntime.EventTaskCreated) {
		t.Fatalf("notification kind = %q, want %q", params.Kind, taskruntime.EventTaskCreated)
	}
	if params.WorkspaceID != workspace.WorkspaceID {
		t.Fatalf("notification workspace_id = %q, want %q", params.WorkspaceID, workspace.WorkspaceID)
	}
	payload := params.Payload.(taskNotificationPayload)
	if payload.ClientCommandID != "cmd-1" {
		t.Fatalf("notification client_command_id = %q, want cmd-1", payload.ClientCommandID)
	}

	submitResultAny, _, _, rpcErr := server.handleRequest(context.Background(), request{
		Method: methodTaskSubmitInput,
		Params: mustRawParams(t, taskSubmitInputParams{
			WorkspaceID:     workspace.WorkspaceID,
			ClientCommandID: "cmd-2",
			TaskID:          "task-123",
			NodeRunID:       "run-1",
			Payload:         map[string]interface{}{"approved": true},
		}),
	})
	if rpcErr != nil {
		t.Fatalf("task.submit_input rpc error: %+v", rpcErr)
	}
	submitResult := submitResultAny.(commandAcceptedResult)
	if !submitResult.Accepted {
		t.Fatalf("task.submit_input accepted = false")
	}
	dispatches = fakeService.Dispatched()
	if got := len(dispatches); got != 2 {
		t.Fatalf("dispatch count after submit = %d, want 2", got)
	}
	if dispatches[1].Type != taskruntime.CommandSubmitInput {
		t.Fatalf("second dispatch type = %q, want %q", dispatches[1].Type, taskruntime.CommandSubmitInput)
	}
}

func TestServeConnEOFDoesNotCloseRuntime(t *testing.T) {
	stateDir := filepath.Join(t.TempDir(), "appserver")
	workspacePath := t.TempDir()
	fakeService := newFakeRuntimeService()
	server := newTestServerWithOptions(t, stateDir, testServerOptions{
		runtimeFactory: func(workDir string) (runtimeService, error) {
			return fakeService, nil
		},
	})
	workspace, _, err := server.registry.Add(workspacePath, "cmdr")
	if err != nil {
		t.Fatalf("add workspace: %v", err)
	}

	err = server.runtimes.dispatch(workspace, taskruntime.RunCommand{Type: taskruntime.CommandStartTask})
	if err != nil {
		t.Fatalf("dispatch runtime command: %v", err)
	}

	var in bytes.Buffer
	var out bytes.Buffer
	writeRequestFrame(t, &in, 1, methodInitialize, map[string]any{"protocol_version": protocolVersion})

	if err := server.ServeConn(context.Background(), &in, &out, ConnectionOptions{}); err != nil {
		t.Fatalf("serve conn: %v", err)
	}

	if got := fakeService.CloseCalls(); got != 0 {
		t.Fatalf("close calls = %d, want 0", got)
	}
	if got := fakeService.PrepareShutdownCalls(); got != 0 {
		t.Fatalf("prepare shutdown calls = %d, want 0", got)
	}
}

func TestLateNotificationSinkAfterSessionCloseDoesNotPanic(t *testing.T) {
	server := newTestServer(t)
	sessionCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	session := &connectionSession{
		id:       "session-a",
		outgoing: make(chan any, 8),
		ctx:      sessionCtx,
		cancel:   cancel,
	}

	_, _, _, rpcErr := server.handleSessionRequest(context.Background(), session, request{
		Method: methodInitialize,
		Params: mustRawParams(t, initializeParams{ProtocolVersion: protocolVersion}),
	})
	if rpcErr != nil {
		t.Fatalf("initialize rpc error: %+v", rpcErr)
	}

	server.mu.Lock()
	sink := server.notificationSink
	server.mu.Unlock()
	if sink == nil {
		t.Fatal("notification sink = nil, want attached sink")
	}

	server.detachClientSession(session.id)
	session.closeOutgoing()

	sink(notification{
		JSONRPC: jsonRPCVersion,
		Method:  methodNotification,
		Params:  notificationParams{Kind: string(taskruntime.EventNodeStarted)},
	})
}

func TestInitializeRejectsSecondInteractiveClientButAllowsPassiveProbe(t *testing.T) {
	server := newTestServer(t)
	sessionA := &connectionSession{id: "session-a", outgoing: make(chan any, 8)}
	sessionB := &connectionSession{id: "session-b", outgoing: make(chan any, 8)}
	probe := &connectionSession{id: "probe", outgoing: make(chan any, 8)}

	_, _, _, rpcErr := server.handleSessionRequest(context.Background(), sessionA, request{
		Method: methodInitialize,
		Params: mustRawParams(t, initializeParams{ProtocolVersion: protocolVersion}),
	})
	if rpcErr != nil {
		t.Fatalf("first initialize rpc error: %+v", rpcErr)
	}
	defer server.detachClientSession(sessionA.id)

	_, _, _, rpcErr = server.handleSessionRequest(context.Background(), sessionB, request{
		Method: methodInitialize,
		Params: mustRawParams(t, initializeParams{ProtocolVersion: protocolVersion}),
	})
	if rpcErr == nil || rpcErr.Code != errorCodeBusy {
		t.Fatalf("second initialize rpc error = %+v, want busy", rpcErr)
	}

	_, _, _, rpcErr = server.handleSessionRequest(context.Background(), probe, request{
		Method: methodInitialize,
		Params: mustRawParams(t, initializeParams{ProtocolVersion: protocolVersion, Passive: true}),
	})
	if rpcErr != nil {
		t.Fatalf("passive initialize rpc error: %+v", rpcErr)
	}
	if probe.attached {
		t.Fatal("passive probe attached interactive session")
	}
}

func TestReconnectReplaysBackloggedNotificationWithClientCommandID(t *testing.T) {
	stateDir := filepath.Join(t.TempDir(), "appserver")
	workspacePath := t.TempDir()
	fakeService := newFakeRuntimeService()
	server := newTestServerWithOptions(t, stateDir, testServerOptions{
		runtimeFactory: func(workDir string) (runtimeService, error) {
			return fakeService, nil
		},
	})
	workspace, _, err := server.registry.Add(workspacePath, "cmdr")
	if err != nil {
		t.Fatalf("add workspace: %v", err)
	}

	sessionA := &connectionSession{id: "session-a", outgoing: make(chan any, 8)}
	_, _, _, rpcErr := server.handleSessionRequest(context.Background(), sessionA, request{
		Method: methodInitialize,
		Params: mustRawParams(t, initializeParams{ProtocolVersion: protocolVersion}),
	})
	if rpcErr != nil {
		t.Fatalf("initialize sessionA rpc error: %+v", rpcErr)
	}

	retryResultAny, _, _, rpcErr := server.handleRequest(context.Background(), request{
		Method: methodTaskRetryNode,
		Params: mustRawParams(t, taskRetryNodeParams{
			WorkspaceID:     workspace.WorkspaceID,
			ClientCommandID: "cmd-retry",
			TaskID:          "task-123",
			NodeRunID:       "run-old",
		}),
	})
	if rpcErr != nil {
		t.Fatalf("task.retry_node rpc error: %+v", rpcErr)
	}
	retryResult := retryResultAny.(commandAcceptedResult)
	if !retryResult.Accepted {
		t.Fatal("task.retry_node accepted = false")
	}

	server.detachClientSession(sessionA.id)
	sessionA.closeOutgoing()

	server.handleRuntimeEvent(workspace.WorkspaceID, taskruntime.RunEvent{
		Type:      taskruntime.EventNodeStarted,
		TaskID:    "task-123",
		NodeRunID: "run-new",
		NodeName:  "upsert_plan",
	})

	sessionB := &connectionSession{id: "session-b", outgoing: make(chan any, 8)}
	_, notifications, _, rpcErr := server.handleSessionRequest(context.Background(), sessionB, request{
		Method: methodInitialize,
		Params: mustRawParams(t, initializeParams{ProtocolVersion: protocolVersion}),
	})
	if rpcErr != nil {
		t.Fatalf("initialize sessionB rpc error: %+v", rpcErr)
	}
	if len(notifications) != 1 {
		t.Fatalf("backlog notification count = %d, want 1", len(notifications))
	}
	payload := notifications[0].Params.(notificationParams).Payload.(taskNotificationPayload)
	if payload.ClientCommandID != "cmd-retry" {
		t.Fatalf("backlog client_command_id = %q, want cmd-retry", payload.ClientCommandID)
	}
}

func TestServiceShutdownReturnsAckAndRequestsGracefulShutdown(t *testing.T) {
	server := newTestServer(t)

	var in bytes.Buffer
	out := &shutdownAwareBuffer{t: t, server: server}
	writeRequestFrame(t, &in, 1, methodInitialize, map[string]any{"protocol_version": protocolVersion})
	writeRequestFrame(t, &in, 2, methodServiceShutdown, map[string]any{})

	if err := server.ServeConn(context.Background(), &in, out, ConnectionOptions{}); err != nil {
		t.Fatalf("serve conn: %v", err)
	}
	if !server.GracefulShutdownRequested() {
		t.Fatal("graceful shutdown = false, want true")
	}

	messages := readFramesAsMaps(t, out.Bytes())
	shutdownResponse := responseByID(t, messages, 2)
	if accepted, _ := nestedBool(shutdownResponse, "result", "accepted"); !accepted {
		t.Fatalf("service.shutdown accepted = %#v, want true", shutdownResponse)
	}
}

func TestServerConfigRuntimeFlows(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	stateDir := filepath.Join(t.TempDir(), "appserver")
	server := newTestServerWithOptions(t, stateDir, testServerOptions{
		loadConfig: func() (appconfig.Config, error) {
			return appconfig.Config{
				Runtimes: map[appconfig.RuntimeID]appconfig.RuntimeSettings{
					appconfig.RuntimeCodex: {},
				},
			}, nil
		},
	})
	server.markInitialized()

	catalogResultAny, _, _, rpcErr := server.handleRequest(context.Background(), request{
		Method: methodConfigCatalog,
		Params: mustRawParams(t, map[string]any{}),
	})
	if rpcErr != nil {
		t.Fatalf("config.catalog rpc error: %+v", rpcErr)
	}
	catalogResult := catalogResultAny.(configCatalogResult)
	if len(catalogResult.Entries) == 0 {
		t.Fatal("config.catalog entries = 0, want builtins")
	}
	defaultAlias := catalogResult.DefaultAlias

	getResultAny, _, _, rpcErr := server.handleRequest(context.Background(), request{
		Method: methodConfigGet,
		Params: mustRawParams(t, configGetParams{Alias: defaultAlias}),
	})
	if rpcErr != nil {
		t.Fatalf("config.get rpc error: %+v", rpcErr)
	}
	getResult := getResultAny.(configGetResult)
	if !getResult.Entry.Builtin {
		t.Fatal("config.get builtin = false, want true")
	}
	if getResult.Entry.Revision == "" {
		t.Fatal("config.get revision = empty, want value")
	}
	if getResult.Entry.Config == nil {
		t.Fatal("config.get config = nil, want value")
	}

	cloneResultAny, _, _, rpcErr := server.handleRequest(context.Background(), request{
		Method: methodConfigClone,
		Params: mustRawParams(t, configCloneParams{
			SourceAlias: defaultAlias,
			NewAlias:    "custom-plan",
		}),
	})
	if rpcErr != nil {
		t.Fatalf("config.clone rpc error: %+v", rpcErr)
	}
	cloneResult := cloneResultAny.(configCloneResult)
	if cloneResult.Entry.Builtin {
		t.Fatal("cloned config builtin = true, want false")
	}
	if cloneResult.Entry.Config == nil {
		t.Fatal("cloned config payload = nil")
	}
	initialRevision := cloneResult.Entry.Revision

	validDraft := *cloneResult.Entry.Config
	validDraft.Description = "Custom plan"
	validateResultAny, _, _, rpcErr := server.handleRequest(context.Background(), request{
		Method: methodConfigValidate,
		Params: mustRawParams(t, configValidateParams{Config: &validDraft}),
	})
	if rpcErr != nil {
		t.Fatalf("config.validate rpc error: %+v", rpcErr)
	}
	validateResult := validateResultAny.(configValidateResult)
	if !validateResult.Valid {
		t.Fatalf("config.validate valid = false, want true (error=%q)", validateResult.Error)
	}
	if validateResult.RuntimeID != appconfig.RuntimeCodex {
		t.Fatalf("validated runtime = %q, want %q", validateResult.RuntimeID, appconfig.RuntimeCodex)
	}
	if !validateResult.RuntimeConfigured {
		t.Fatal("validated runtime_configured = false, want true")
	}

	invalidDraft := validDraft
	invalidDraft.Runtime = appconfig.RuntimeClaudeCode
	invalidResultAny, _, _, rpcErr := server.handleRequest(context.Background(), request{
		Method: methodConfigValidate,
		Params: mustRawParams(t, configValidateParams{Config: &invalidDraft}),
	})
	if rpcErr != nil {
		t.Fatalf("invalid config.validate rpc error: %+v", rpcErr)
	}
	invalidResult := invalidResultAny.(configValidateResult)
	if invalidResult.Valid {
		t.Fatal("invalid config.validate valid = true, want false")
	}
	if !strings.Contains(invalidResult.Error, `runtime "claude-code" is not configured`) {
		t.Fatalf("invalid config.validate error = %q, want runtime-not-configured", invalidResult.Error)
	}

	saveResultAny, _, _, rpcErr := server.handleRequest(context.Background(), request{
		Method: methodConfigSave,
		Params: mustRawParams(t, configSaveParams{
			Alias:            cloneResult.Entry.Alias,
			ExpectedRevision: initialRevision,
			Config:           &validDraft,
		}),
	})
	if rpcErr != nil {
		t.Fatalf("config.save rpc error: %+v", rpcErr)
	}
	saveResult := saveResultAny.(configSaveResult)
	if saveResult.Entry.Description != "Custom plan" {
		t.Fatalf("saved description = %q, want %q", saveResult.Entry.Description, "Custom plan")
	}
	if saveResult.Entry.Revision == "" || saveResult.Entry.Revision == initialRevision {
		t.Fatalf("saved revision = %q, want new revision after save", saveResult.Entry.Revision)
	}

	_, _, _, rpcErr = server.handleRequest(context.Background(), request{
		Method: methodConfigSave,
		Params: mustRawParams(t, configSaveParams{
			Alias:            cloneResult.Entry.Alias,
			ExpectedRevision: initialRevision,
			Config:           &validDraft,
		}),
	})
	if rpcErr == nil || rpcErr.Code != errorCodeConfigConflict {
		t.Fatalf("stale config.save rpc error = %+v, want config conflict", rpcErr)
	}

	builtinDraft := *getResult.Entry.Config
	builtinDraft.Description = "Builtin default edited"
	builtinSaveAny, _, _, rpcErr := server.handleRequest(context.Background(), request{
		Method: methodConfigSave,
		Params: mustRawParams(t, configSaveParams{
			Alias:            defaultAlias,
			ExpectedRevision: getResult.Entry.Revision,
			Config:           &builtinDraft,
		}),
	})
	if rpcErr != nil {
		t.Fatalf("builtin config.save rpc error: %+v", rpcErr)
	}
	builtinSave := builtinSaveAny.(configSaveResult)
	if builtinSave.Entry.Description != "Builtin default edited" {
		t.Fatalf("builtin config.save description = %q, want updated value", builtinSave.Entry.Description)
	}

	resetAny, _, _, rpcErr := server.handleRequest(context.Background(), request{
		Method: methodConfigReset,
		Params: mustRawParams(t, configResetParams{Alias: defaultAlias}),
	})
	if rpcErr != nil {
		t.Fatalf("config.reset rpc error: %+v", rpcErr)
	}
	resetResult := resetAny.(configResetResult)
	if resetResult.Entry.Description != getResult.Entry.Description {
		t.Fatalf("config.reset description = %q, want %q", resetResult.Entry.Description, getResult.Entry.Description)
	}

	renameResultAny, _, _, rpcErr := server.handleRequest(context.Background(), request{
		Method: methodConfigRename,
		Params: mustRawParams(t, configRenameParams{
			Alias:    cloneResult.Entry.Alias,
			NewAlias: "custom-plan-2",
		}),
	})
	if rpcErr != nil {
		t.Fatalf("config.rename rpc error: %+v", rpcErr)
	}
	renameResult := renameResultAny.(configRenameResult)
	if renameResult.Entry.Alias != "custom-plan-2" {
		t.Fatalf("renamed alias = %q, want %q", renameResult.Entry.Alias, "custom-plan-2")
	}

	setDefaultResultAny, _, _, rpcErr := server.handleRequest(context.Background(), request{
		Method: methodConfigSetDefault,
		Params: mustRawParams(t, configSetDefaultParams{Alias: renameResult.Entry.Alias}),
	})
	if rpcErr != nil {
		t.Fatalf("config.set_default rpc error: %+v", rpcErr)
	}
	setDefaultResult := setDefaultResultAny.(configSetDefaultResult)
	if !setDefaultResult.Entry.IsDefault {
		t.Fatal("config.set_default is_default = false, want true")
	}

	runtimeListAny, _, _, rpcErr := server.handleRequest(context.Background(), request{
		Method: methodRuntimeList,
		Params: mustRawParams(t, map[string]any{}),
	})
	if rpcErr != nil {
		t.Fatalf("runtime.list rpc error: %+v", rpcErr)
	}
	runtimeList := runtimeListAny.(runtimeListResult)
	if got := len(runtimeList.Runtimes); got != 1 {
		t.Fatalf("runtime.list count = %d, want 1", got)
	}
	if runtimeList.Runtimes[0].RuntimeID != appconfig.RuntimeCodex {
		t.Fatalf("runtime.list[0] = %q, want %q", runtimeList.Runtimes[0].RuntimeID, appconfig.RuntimeCodex)
	}

	deleteResultAny, _, _, rpcErr := server.handleRequest(context.Background(), request{
		Method: methodConfigDelete,
		Params: mustRawParams(t, configDeleteParams{Alias: renameResult.Entry.Alias}),
	})
	if rpcErr != nil {
		t.Fatalf("config.delete rpc error: %+v", rpcErr)
	}
	deleteResult := deleteResultAny.(configDeleteResult)
	if !deleteResult.Removed {
		t.Fatal("config.delete removed = false, want true")
	}

	catalogAfterDeleteAny, _, _, rpcErr := server.handleRequest(context.Background(), request{
		Method: methodConfigCatalog,
		Params: mustRawParams(t, map[string]any{}),
	})
	if rpcErr != nil {
		t.Fatalf("config.catalog after delete rpc error: %+v", rpcErr)
	}
	catalogAfterDelete := catalogAfterDeleteAny.(configCatalogResult)
	if catalogAfterDelete.DefaultAlias != taskconfig.DefaultAlias {
		t.Fatalf("default alias after delete = %q, want %q", catalogAfterDelete.DefaultAlias, taskconfig.DefaultAlias)
	}
}

func TestServerConfigPromptFlows(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	stateDir := filepath.Join(t.TempDir(), "appserver")
	server := newTestServerWithOptions(t, stateDir, testServerOptions{
		loadConfig: func() (appconfig.Config, error) {
			return appconfig.Config{
				Runtimes: map[appconfig.RuntimeID]appconfig.RuntimeSettings{
					appconfig.RuntimeCodex: {},
				},
			}, nil
		},
	})
	server.markInitialized()

	catalog, err := taskconfig.LoadCatalog()
	if err != nil {
		t.Fatalf("load catalog: %v", err)
	}
	defaultAlias := catalog.DefaultAlias

	getBuiltinAny, _, _, rpcErr := server.handleRequest(context.Background(), request{
		Method: methodConfigPromptGet,
		Params: mustRawParams(t, configPromptGetParams{
			Alias:    defaultAlias,
			NodeName: "draft_plan",
		}),
	})
	if rpcErr != nil {
		t.Fatalf("builtin config.prompt.get rpc error: %+v", rpcErr)
	}
	getBuiltin := getBuiltinAny.(configPromptGetResult)
	if !getBuiltin.Prompt.Builtin || getBuiltin.Prompt.ReadOnly {
		t.Fatalf("builtin prompt flags = builtin:%v readonly:%v, want true/false", getBuiltin.Prompt.Builtin, getBuiltin.Prompt.ReadOnly)
	}
	if getBuiltin.Prompt.Path == "" || getBuiltin.Prompt.Content == "" || getBuiltin.Prompt.Revision == "" {
		t.Fatalf("builtin prompt payload incomplete: %#v", getBuiltin.Prompt)
	}

	builtinUpdatedContent := getBuiltin.Prompt.Content + "\n\nTighten verification."
	builtinSaveAny, _, _, rpcErr := server.handleRequest(context.Background(), request{
		Method: methodConfigPromptSave,
		Params: mustRawParams(t, configPromptSaveParams{
			Alias:            defaultAlias,
			NodeName:         "draft_plan",
			ExpectedRevision: getBuiltin.Prompt.Revision,
			Content:          builtinUpdatedContent,
		}),
	})
	if rpcErr != nil {
		t.Fatalf("builtin config.prompt.save rpc error: %+v", rpcErr)
	}
	builtinSave := builtinSaveAny.(configPromptSaveResult)
	if builtinSave.Prompt.Content != builtinUpdatedContent {
		t.Fatalf("builtin prompt save content mismatch")
	}

	_, _, _, rpcErr = server.handleRequest(context.Background(), request{
		Method: methodConfigReset,
		Params: mustRawParams(t, configResetParams{Alias: defaultAlias}),
	})
	if rpcErr != nil {
		t.Fatalf("builtin prompt config.reset rpc error: %+v", rpcErr)
	}
	resetPromptAny, _, _, rpcErr := server.handleRequest(context.Background(), request{
		Method: methodConfigPromptGet,
		Params: mustRawParams(t, configPromptGetParams{
			Alias:    defaultAlias,
			NodeName: "draft_plan",
		}),
	})
	if rpcErr != nil {
		t.Fatalf("builtin prompt reload after reset rpc error: %+v", rpcErr)
	}
	resetPrompt := resetPromptAny.(configPromptGetResult)
	if resetPrompt.Prompt.Content != getBuiltin.Prompt.Content {
		t.Fatalf("builtin prompt after reset = %q, want original content", resetPrompt.Prompt.Content)
	}

	cloneAny, _, _, rpcErr := server.handleRequest(context.Background(), request{
		Method: methodConfigClone,
		Params: mustRawParams(t, configCloneParams{
			SourceAlias: defaultAlias,
			NewAlias:    "prompt-copy",
		}),
	})
	if rpcErr != nil {
		t.Fatalf("config.clone rpc error: %+v", rpcErr)
	}
	cloneResult := cloneAny.(configCloneResult)

	getCustomAny, _, _, rpcErr := server.handleRequest(context.Background(), request{
		Method: methodConfigPromptGet,
		Params: mustRawParams(t, configPromptGetParams{
			Alias:    cloneResult.Entry.Alias,
			NodeName: "draft_plan",
		}),
	})
	if rpcErr != nil {
		t.Fatalf("custom config.prompt.get rpc error: %+v", rpcErr)
	}
	getCustom := getCustomAny.(configPromptGetResult)
	if getCustom.Prompt.ReadOnly {
		t.Fatal("custom prompt readonly = true, want false")
	}

	updatedContent := getCustom.Prompt.Content + "\n\nAdd stronger implementation guardrails."
	savePromptAny, _, _, rpcErr := server.handleRequest(context.Background(), request{
		Method: methodConfigPromptSave,
		Params: mustRawParams(t, configPromptSaveParams{
			Alias:            cloneResult.Entry.Alias,
			NodeName:         "draft_plan",
			ExpectedRevision: getCustom.Prompt.Revision,
			Content:          updatedContent,
		}),
	})
	if rpcErr != nil {
		t.Fatalf("custom config.prompt.save rpc error: %+v", rpcErr)
	}
	savePrompt := savePromptAny.(configPromptSaveResult)
	if savePrompt.Prompt.Content != updatedContent {
		t.Fatalf("saved prompt content mismatch")
	}
	if savePrompt.Prompt.Revision == "" || savePrompt.Prompt.Revision == getCustom.Prompt.Revision {
		t.Fatalf("saved prompt revision = %q, want new revision", savePrompt.Prompt.Revision)
	}

	_, _, _, rpcErr = server.handleRequest(context.Background(), request{
		Method: methodConfigPromptSave,
		Params: mustRawParams(t, configPromptSaveParams{
			Alias:            cloneResult.Entry.Alias,
			NodeName:         "draft_plan",
			ExpectedRevision: getCustom.Prompt.Revision,
			Content:          updatedContent,
		}),
	})
	if rpcErr == nil || rpcErr.Code != errorCodeConfigConflict {
		t.Fatalf("stale config.prompt.save rpc error = %+v, want config conflict", rpcErr)
	}

	_, _, _, rpcErr = server.handleRequest(context.Background(), request{
		Method: methodConfigPromptGet,
		Params: mustRawParams(t, configPromptGetParams{
			Alias:    cloneResult.Entry.Alias,
			NodeName: "approve_plan",
		}),
	})
	if rpcErr == nil || rpcErr.Code != errorCodeInvalidParams {
		t.Fatalf("human node config.prompt.get rpc error = %+v, want invalid params", rpcErr)
	}

	_, _, _, rpcErr = server.handleRequest(context.Background(), request{
		Method: methodConfigPromptGet,
		Params: mustRawParams(t, configPromptGetParams{
			Alias:    cloneResult.Entry.Alias,
			NodeName: "done",
		}),
	})
	if rpcErr == nil || rpcErr.Code != errorCodeInvalidParams {
		t.Fatalf("terminal node config.prompt.get rpc error = %+v, want invalid params", rpcErr)
	}
}

func newTestServer(t *testing.T) *Server {
	t.Helper()
	return newTestServerAtPath(t, filepath.Join(t.TempDir(), "appserver"))
}

func newTestServerAtPath(t *testing.T, stateDir string) *Server {
	t.Helper()
	return newTestServerWithOptions(t, stateDir, testServerOptions{})
}

type testServerOptions struct {
	runtimeFactory            runtimeServiceFactory
	loadConfig                func() (appconfig.Config, error)
	loadCatalog               func() (*taskconfig.Catalog, error)
	loadRegistry              func() (taskconfig.Registry, error)
	loadTaskLaunchPreferences func() appconfig.TaskLaunchPreferences
}

func newTestServerWithOptions(t *testing.T, stateDir string, opts testServerOptions) *Server {
	t.Helper()
	server, err := New(Options{
		StateDir:                  stateDir,
		ServerVersion:             "test",
		LoadConfig:                coalesceLoadConfig(opts.loadConfig),
		LoadCatalog:               opts.loadCatalog,
		LoadRegistry:              opts.loadRegistry,
		LoadTaskLaunchPreferences: opts.loadTaskLaunchPreferences,
		RuntimeFactory:            opts.runtimeFactory,
		WorktreeAvailable:         func(string) bool { return true },
	})
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	return server
}

func coalesceLoadConfig(fn func() (appconfig.Config, error)) func() (appconfig.Config, error) {
	if fn != nil {
		return fn
	}
	return func() (appconfig.Config, error) {
		return appconfig.Default(), nil
	}
}

func writeRequestFrame(t *testing.T, dst *bytes.Buffer, id int, method string, params map[string]any) {
	t.Helper()
	writer := newFrameWriter(dst)
	if err := writer.writeJSON(map[string]any{
		"jsonrpc": jsonRPCVersion,
		"id":      id,
		"method":  method,
		"params":  params,
	}); err != nil {
		t.Fatalf("write frame: %v", err)
	}
}

func readFramesAsMaps(t *testing.T, payload []byte) []map[string]any {
	t.Helper()
	reader := newFrameReader(bytes.NewReader(payload))
	var messages []map[string]any
	for {
		frame, err := reader.readFrame()
		if err != nil {
			if err == io.EOF {
				break
			}
			t.Fatalf("read frame: %v", err)
		}
		var msg map[string]any
		if err := json.Unmarshal(frame, &msg); err != nil {
			t.Fatalf("unmarshal frame: %v", err)
		}
		messages = append(messages, msg)
	}
	return messages
}

func responseByID(t *testing.T, messages []map[string]any, id int) map[string]any {
	t.Helper()
	for _, message := range messages {
		rawID, ok := message["id"]
		if !ok {
			continue
		}
		number, ok := rawID.(float64)
		if !ok {
			continue
		}
		if int(number) == id {
			return message
		}
	}
	t.Fatalf("missing response id %d", id)
	return nil
}

func nestedString(m map[string]any, path ...string) (string, bool) {
	value, ok := nestedValue(m, path...)
	if !ok {
		return "", false
	}
	str, ok := value.(string)
	return str, ok
}

func nestedStringMust(t *testing.T, m map[string]any, path ...string) string {
	t.Helper()
	value, ok := nestedString(m, path...)
	if !ok {
		t.Fatalf("missing string at path %v in %#v", path, m)
	}
	return value
}

func nestedBool(m map[string]any, path ...string) (bool, bool) {
	value, ok := nestedValue(m, path...)
	if !ok {
		return false, false
	}
	boolean, ok := value.(bool)
	return boolean, ok
}

func nestedFloat(m map[string]any, path ...string) float64 {
	value, ok := nestedValue(m, path...)
	if !ok {
		return 0
	}
	number, _ := value.(float64)
	return number
}

func nestedSlice(t *testing.T, m map[string]any, path ...string) []any {
	t.Helper()
	value, ok := nestedValue(m, path...)
	if !ok {
		t.Fatalf("missing slice at path %v in %#v", path, m)
	}
	items, ok := value.([]any)
	if !ok {
		t.Fatalf("value at path %v is %T, want []any", path, value)
	}
	return items
}

func nestedValue(m map[string]any, path ...string) (any, bool) {
	var current any = m
	for _, segment := range path {
		nextMap, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		value, ok := nextMap[segment]
		if !ok {
			return nil, false
		}
		current = value
	}
	return current, true
}

func mustRawParams(t *testing.T, params any) json.RawMessage {
	t.Helper()
	payload, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}
	return payload
}

func seedAwaitingTask(t *testing.T, workDir string) (taskID string, awaitingRunID string) {
	t.Helper()
	store, err := taskstore.Open(workDir)
	if err != nil {
		t.Fatalf("open task store: %v", err)
	}
	defer func() { _ = store.Close() }()

	taskID = "task-awaiting"
	configPath, err := taskconfig.DefaultConfigPath()
	if err != nil {
		t.Fatalf("default config path: %v", err)
	}
	materialized, err := taskconfig.Materialize(workDir, taskID, configPath)
	if err != nil {
		t.Fatalf("materialize config: %v", err)
	}

	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
	completedAt := now.Add(30 * time.Second)
	task := taskdomain.Task{
		ID:           taskID,
		Description:  "Review desktop task startup",
		ConfigAlias:  "default",
		ConfigPath:   materialized.ConfigPath,
		WorkDir:      taskstore.NormalizeWorkDir(workDir),
		ExecutionDir: taskstore.NormalizeWorkDir(workDir),
		CreatedAt:    now,
		UpdatedAt:    now.Add(2 * time.Minute),
	}
	entryRun := taskdomain.NodeRun{
		ID:          "run-draft",
		TaskID:      taskID,
		NodeName:    "draft_plan",
		Status:      taskdomain.NodeRunDone,
		Result:      map[string]interface{}{"file_paths": []string{"plan.md"}},
		StartedAt:   now,
		CompletedAt: &completedAt,
	}
	if err := store.CreateTaskWithEntryRun(context.Background(), task, entryRun); err != nil {
		t.Fatalf("create task with entry run: %v", err)
	}

	awaitingRunID = "run-approve"
	awaitingRun := taskdomain.NodeRun{
		ID:        awaitingRunID,
		TaskID:    taskID,
		NodeName:  "approve_plan",
		Status:    taskdomain.NodeRunAwaitingUser,
		StartedAt: now.Add(time.Minute),
	}
	if err := store.SaveNodeRun(context.Background(), awaitingRun); err != nil {
		t.Fatalf("save awaiting node run: %v", err)
	}

	artifactPath := filepath.Join(taskstore.RunDir(workDir, taskID, entryRun.ID), "plan.md")
	if err := os.MkdirAll(filepath.Dir(artifactPath), 0o755); err != nil {
		t.Fatalf("mkdir artifact dir: %v", err)
	}
	if err := os.WriteFile(artifactPath, []byte("# plan\n"), 0o644); err != nil {
		t.Fatalf("write artifact: %v", err)
	}
	return taskID, awaitingRunID
}

type fakeRuntimeService struct {
	mu           sync.Mutex
	events       chan taskruntime.RunEvent
	dispatches   []taskruntime.RunCommand
	closeCalls   int
	prepareCalls int
}

func newFakeRuntimeService() *fakeRuntimeService {
	return &fakeRuntimeService{
		events: make(chan taskruntime.RunEvent, 16),
	}
}

func (f *fakeRuntimeService) Run(ctx context.Context) error {
	<-ctx.Done()
	return ctx.Err()
}

func (f *fakeRuntimeService) Events() <-chan taskruntime.RunEvent {
	return f.events
}

func (f *fakeRuntimeService) Dispatch(cmd taskruntime.RunCommand) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.dispatches = append(f.dispatches, cmd)
}

func (f *fakeRuntimeService) PrepareShutdown(ctx context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.prepareCalls++
	return nil
}

func (f *fakeRuntimeService) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closeCalls++
	return nil
}

func (f *fakeRuntimeService) Dispatched() []taskruntime.RunCommand {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]taskruntime.RunCommand, len(f.dispatches))
	copy(out, f.dispatches)
	return out
}

func (f *fakeRuntimeService) CloseCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.closeCalls
}

func (f *fakeRuntimeService) PrepareShutdownCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.prepareCalls
}

type shutdownAwareBuffer struct {
	t      *testing.T
	server *Server
	bytes.Buffer
}

func (b *shutdownAwareBuffer) Write(p []byte) (int, error) {
	b.t.Helper()
	if b.server != nil && b.server.GracefulShutdownRequested() {
		b.t.Fatal("graceful shutdown requested before shutdown response finished writing")
	}
	return b.Buffer.Write(p)
}

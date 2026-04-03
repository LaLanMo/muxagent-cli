package appserver

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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
	"github.com/LaLanMo/muxagent-cli/internal/taskruntime"
	"github.com/LaLanMo/muxagent-cli/internal/taskstore"
	"gopkg.in/yaml.v3"
)

type integrationExecutor struct {
	mu     sync.Mutex
	steps  map[string][]taskexecutor.Result
	errors map[string][]error
}

type blockingIntegrationExecutor struct{}

func (e *integrationExecutor) Execute(ctx context.Context, req taskexecutor.Request, progress func(taskexecutor.Progress)) (taskexecutor.Result, error) {
	e.mu.Lock()
	if sequence := e.errors[req.NodeRun.NodeName]; len(sequence) > 0 {
		execErr := sequence[0]
		e.errors[req.NodeRun.NodeName] = sequence[1:]
		e.mu.Unlock()
		if progress != nil {
			progress(taskexecutor.Progress{Message: "running " + req.NodeRun.NodeName})
		}
		return taskexecutor.Result{}, execErr
	}
	sequence := e.steps[req.NodeRun.NodeName]
	if len(sequence) == 0 {
		e.mu.Unlock()
		return taskexecutor.Result{}, fmt.Errorf("unexpected node %s", req.NodeRun.NodeName)
	}
	result := sequence[0]
	e.steps[req.NodeRun.NodeName] = sequence[1:]
	e.mu.Unlock()
	if progress != nil {
		progress(taskexecutor.Progress{Message: "running " + req.NodeRun.NodeName})
	}
	return materializeResultArtifacts(req, result)
}

func (blockingIntegrationExecutor) Execute(ctx context.Context, req taskexecutor.Request, progress func(taskexecutor.Progress)) (taskexecutor.Result, error) {
	if progress != nil {
		progress(taskexecutor.Progress{Message: "running " + req.NodeRun.NodeName})
	}
	<-ctx.Done()
	return taskexecutor.Result{}, ctx.Err()
}

func TestServerWithRealTaskRuntime_StartAndSubmitInput(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	_, err := taskconfig.EnsureManagedDefaultAssets()
	if err != nil {
		t.Fatalf("EnsureManagedDefaultAssets: %v", err)
	}
	configPath, err := taskconfig.DefaultConfigPath()
	if err != nil {
		t.Fatalf("DefaultConfigPath: %v", err)
	}

	executor := &integrationExecutor{
		steps: map[string][]taskexecutor.Result{
			"draft_plan":  {{Kind: taskexecutor.ResultKindResult, Result: artifactResult("plan.md")}},
			"review_plan": {{Kind: taskexecutor.ResultKindResult, Result: map[string]interface{}{"passed": true, "file_paths": []interface{}{"review.md"}}}},
			"implement":   {{Kind: taskexecutor.ResultKindResult, Result: artifactResult("impl.md")}},
			"verify":      {{Kind: taskexecutor.ResultKindResult, Result: map[string]interface{}{"passed": true, "file_paths": []interface{}{"verify.md"}}}},
		},
	}

	workDir := t.TempDir()
	service, err := taskruntime.NewService(workDir, executor)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	server, err := New(Options{
		Service:       service,
		WorkDir:       workDir,
		ServerVersion: "test",
		LoadTaskLaunchPreferences: func() appconfig.TaskLaunchPreferences {
			return appconfig.TaskLaunchPreferences{}
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	inputReader, inputWriter := io.Pipe()
	outputReader, outputWriter := io.Pipe()
	inputFrames := newFrameWriter(inputWriter)
	outputFrames := newFrameReader(outputReader)

	var (
		serveErr error
		wg       sync.WaitGroup
	)
	wg.Add(1)
	go func() {
		defer wg.Done()
		serveErr = server.Serve(context.Background(), inputReader, outputWriter)
	}()

	writeRequest := func(id int, method string, params map[string]any) {
		t.Helper()
		payload := rpcRequestPayload(t, id, method, params)
		if err := inputFrames.writeFrame(payload); err != nil {
			t.Fatalf("writeFrame(%s): %v", method, err)
		}
	}
	readMessage := func() map[string]any {
		t.Helper()
		type frameResult struct {
			frame []byte
			err   error
		}
		resultCh := make(chan frameResult, 1)
		go func() {
			frame, err := outputFrames.readFrame()
			resultCh <- frameResult{frame: frame, err: err}
		}()
		select {
		case result := <-resultCh:
			if result.err != nil {
				t.Fatalf("readFrame: %v", result.err)
			}
			return decodeFrameMap(t, result.frame)
		case <-time.After(5 * time.Second):
			t.Fatal("timed out waiting for app-server frame")
			return nil
		}
	}

	writeRequest(1, methodInitialize, map[string]any{"protocol_version": protocolVersion})
	if got := nestedString(readMessage(), "result", "server_name"); got != "muxagent app-server" {
		t.Fatalf("server_name = %q", got)
	}

	writeRequest(2, methodTaskStart, map[string]any{
		"client_command_id": "cmd-start",
		"description":       "Implement app-server",
		"config_alias":      taskconfig.DefaultAlias,
		"config_path":       configPath,
	})
	startResp := readMessage()
	if got := nestedBool(startResp, "result", "accepted"); !got {
		t.Fatalf("start accepted = false")
	}
	if got := nestedString(startResp, "result", "client_command_id"); got != "cmd-start" {
		t.Fatalf("start client_command_id = %q", got)
	}

	var (
		taskID    string
		nodeRunID string
	)
	for taskID == "" || nodeRunID == "" {
		msg := readMessage()
		method := stringValue(msg["method"])
		if method != string(taskruntime.EventTaskCreated) && method != string(taskruntime.EventInputRequested) {
			continue
		}
		if method == string(taskruntime.EventTaskCreated) {
			taskID = nestedString(msg, "params", "event", "task_id")
			continue
		}
		taskID = nestedString(msg, "params", "event", "task_id")
		nodeRunID = nestedString(msg, "params", "event", "node_run_id")
	}

	writeRequest(3, methodTaskInputRequest, map[string]any{
		"task_id":     taskID,
		"node_run_id": nodeRunID,
	})
	inputRequestResp := readMessage()
	if got := nestedString(inputRequestResp, "result", "input_request", "node_run_id"); got != nodeRunID {
		t.Fatalf("input_request.node_run_id = %q, want %q", got, nodeRunID)
	}
	if got := nestedString(inputRequestResp, "result", "input_request", "kind"); got != string(taskruntime.InputKindHumanNode) {
		t.Fatalf("input_request.kind = %q, want %q", got, taskruntime.InputKindHumanNode)
	}

	writeRequest(4, methodTaskSubmitInput, map[string]any{
		"client_command_id": "cmd-input",
		"task_id":           taskID,
		"node_run_id":       nodeRunID,
		"payload":           map[string]any{"approved": true},
	})
	submitResp := readMessage()
	if got := nestedBool(submitResp, "result", "accepted"); !got {
		t.Fatalf("submit accepted = false")
	}
	if got := nestedString(submitResp, "result", "client_command_id"); got != "cmd-input" {
		t.Fatalf("submit client_command_id = %q", got)
	}

	for {
		msg := readMessage()
		if stringValue(msg["method"]) != string(taskruntime.EventTaskCompleted) {
			continue
		}
		if got := nestedString(msg, "params", "event", "task_id"); got != taskID {
			continue
		}
		break
	}

	writeRequest(5, methodTaskGet, map[string]any{"task_id": taskID})
	taskGet := readMessage()
	if got := nestedString(taskGet, "result", "task", "status"); got != string(taskdomain.TaskStatusDone) {
		t.Fatalf("task status = %q, want %q", got, taskdomain.TaskStatusDone)
	}

	writeRequest(6, methodArtifactList, map[string]any{"task_id": taskID})
	artifactList := readMessage()
	artifactPath := nestedString(artifactList, "result", "artifacts", "0", "resolved_path")
	if !filepath.IsAbs(artifactPath) {
		t.Fatalf("artifact resolved_path = %q, want absolute path", artifactPath)
	}
	if _, err := os.ReadFile(artifactPath); err != nil {
		t.Fatalf("ReadFile(%q): %v", artifactPath, err)
	}

	_ = inputWriter.Close()
	_ = outputReader.Close()
	wg.Wait()
	if serveErr != nil {
		t.Fatalf("Serve: %v", serveErr)
	}
}

func TestServerWithRealTaskRuntime_ShutdownDrainsQueuedStart(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	_, err := taskconfig.EnsureManagedDefaultAssets()
	if err != nil {
		t.Fatalf("EnsureManagedDefaultAssets: %v", err)
	}
	configPath, err := taskconfig.DefaultConfigPath()
	if err != nil {
		t.Fatalf("DefaultConfigPath: %v", err)
	}

	workDir := t.TempDir()
	service, err := taskruntime.NewService(workDir, blockingIntegrationExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	server, err := New(Options{
		Service:       service,
		WorkDir:       workDir,
		ServerVersion: "test",
		LoadTaskLaunchPreferences: func() appconfig.TaskLaunchPreferences {
			return appconfig.TaskLaunchPreferences{}
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	input := framesFromJSON(
		t,
		rpcRequestPayload(t, 1, methodInitialize, map[string]any{"protocol_version": protocolVersion}),
		rpcRequestPayload(t, 2, methodTaskStart, map[string]any{
			"description":  "Queued start before shutdown",
			"config_alias": taskconfig.DefaultAlias,
			"config_path":  configPath,
		}),
		rpcRequestPayload(t, 3, methodServiceShutdown, map[string]any{}),
	)
	var output bytes.Buffer
	if err := server.Serve(context.Background(), bytes.NewReader(input), &output); err != nil {
		t.Fatalf("Serve: %v", err)
	}
	messages := decodeOutputFrames(t, output.Bytes())

	store, err := taskstore.Open(workDir)
	if err != nil {
		t.Fatalf("Open store: %v", err)
	}
	defer store.Close()

	var createdTaskID string
	for _, message := range messages {
		if stringValue(message["method"]) != string(taskruntime.EventTaskCreated) {
			continue
		}
		createdTaskID = nestedString(message, "params", "event", "task_id")
		break
	}
	if createdTaskID == "" {
		t.Fatalf("missing task.created notification (messages=%v)", messages)
	}

	task, err := store.GetTask(context.Background(), createdTaskID)
	if err != nil {
		t.Fatalf("GetTask(%q): %v (messages=%v)", createdTaskID, err, messages)
	}
	if task.ID != createdTaskID {
		t.Fatalf("task id = %q, want %q", task.ID, createdTaskID)
	}
	foundShutdownResponse := false
	for _, message := range messages {
		if stringValue(message["method"]) != "" {
			continue
		}
		if id, _ := message["id"].(float64); id != 3 {
			continue
		}
		foundShutdownResponse = true
		if got := nestedBool(message, "result", "accepted"); !got {
			t.Fatalf("shutdown accepted = false")
		}
	}
	if !foundShutdownResponse {
		t.Fatal("did not observe shutdown response")
	}
}

func TestServerWithRealTaskRuntime_StatusAndCatalog(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if _, err := taskconfig.EnsureManagedDefaultAssets(); err != nil {
		t.Fatalf("EnsureManagedDefaultAssets: %v", err)
	}
	if _, err := appconfig.SaveTaskLaunchPreferences(appconfig.TaskLaunchPreferences{UseWorktree: true}); err != nil {
		t.Fatalf("SaveTaskLaunchPreferences: %v", err)
	}

	workDir := t.TempDir()
	service, err := taskruntime.NewService(workDir, &integrationExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	server, err := New(Options{
		Service:           service,
		WorkDir:           workDir,
		ServerVersion:     "test",
		WorktreeAvailable: func(string) bool { return true },
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	input := framesFromJSON(
		t,
		rpcRequestPayload(t, 1, methodInitialize, map[string]any{"protocol_version": protocolVersion}),
		rpcRequestPayload(t, 2, methodConfigCatalog, map[string]any{}),
		rpcRequestPayload(t, 3, methodServiceStatus, map[string]any{}),
	)
	var output bytes.Buffer
	if err := server.Serve(context.Background(), bytes.NewReader(input), &output); err != nil {
		t.Fatalf("Serve: %v", err)
	}

	messages := decodeOutputFrames(t, output.Bytes())
	if got := nestedString(messages[1], "result", "default_alias"); got == "" {
		t.Fatal("config.catalog default_alias is empty")
	}
	entries, ok := nestedValue(messages[1], "result", "entries").([]any)
	if !ok || len(entries) == 0 {
		t.Fatalf("config.catalog entries = %T len=%d, want non-empty", nestedValue(messages[1], "result", "entries"), len(entries))
	}
	if got := nestedString(messages[2], "result", "work_dir"); got != taskstore.NormalizeWorkDir(workDir) {
		t.Fatalf("service.status work_dir = %q, want %q", got, taskstore.NormalizeWorkDir(workDir))
	}
	if got := nestedBool(messages[2], "result", "worktree_available"); !got {
		t.Fatal("service.status worktree_available = false, want true")
	}
	if got := nestedBool(messages[2], "result", "default_use_worktree"); !got {
		t.Fatal("service.status default_use_worktree = false, want true")
	}
	if got, _ := nestedValue(messages[2], "result", "protocol_version").(float64); int(got) != protocolVersion {
		t.Fatalf("service.status protocol_version = %v, want %d", nestedValue(messages[2], "result", "protocol_version"), protocolVersion)
	}
}

func TestServerWithRealTaskRuntime_StartFollowUpAndListTasks(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	configPath := writeIntegrationConfig(t, singleAgentIntegrationFixture())

	executor := &integrationExecutor{
		steps: map[string][]taskexecutor.Result{
			"implement": {
				{Kind: taskexecutor.ResultKindResult, Result: artifactResult("parent-impl.md")},
				{Kind: taskexecutor.ResultKindResult, Result: artifactResult("child-impl.md")},
			},
		},
	}
	workDir := t.TempDir()
	service, err := taskruntime.NewService(workDir, executor)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server, err := New(Options{
		Service:       service,
		WorkDir:       workDir,
		ServerVersion: "test",
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	session := newIntegrationSession(t, server)
	defer session.Close()

	session.Request(1, methodInitialize, map[string]any{"protocol_version": protocolVersion})
	if got := nestedString(session.WaitForResponse(1), "result", "server_name"); got != "muxagent app-server" {
		t.Fatalf("server_name = %q", got)
	}

	session.Request(2, methodTaskStart, map[string]any{
		"client_command_id": "cmd-parent",
		"description":       "Parent task",
		"config_alias":      "single-agent",
		"config_path":       configPath,
	})
	if got := nestedBool(session.WaitForResponse(2), "result", "accepted"); !got {
		t.Fatal("task.start accepted = false")
	}
	parentCreated := session.WaitForNotification(string(taskruntime.EventTaskCreated), func(msg map[string]any) bool {
		return nestedString(msg, "params", "client_command_id") == "cmd-parent"
	})
	parentTaskID := nestedString(parentCreated, "params", "event", "task_id")
	if parentTaskID == "" {
		t.Fatal("missing parent task id")
	}
	session.WaitForNotification(string(taskruntime.EventTaskCompleted), func(msg map[string]any) bool {
		return nestedString(msg, "params", "event", "task_id") == parentTaskID
	})

	session.Request(3, methodTaskStartFollowUp, map[string]any{
		"client_command_id": "cmd-follow",
		"parent_task_id":    parentTaskID,
		"description":       "Child task",
	})
	if got := nestedBool(session.WaitForResponse(3), "result", "accepted"); !got {
		t.Fatal("task.start_follow_up accepted = false")
	}
	childCreated := session.WaitForNotification(string(taskruntime.EventTaskCreated), func(msg map[string]any) bool {
		return nestedString(msg, "params", "client_command_id") == "cmd-follow"
	})
	childTaskID := nestedString(childCreated, "params", "event", "task_id")
	if childTaskID == "" || childTaskID == parentTaskID {
		t.Fatalf("child task id = %q, want distinct non-empty task id", childTaskID)
	}
	session.WaitForNotification(string(taskruntime.EventTaskCompleted), func(msg map[string]any) bool {
		return nestedString(msg, "params", "event", "task_id") == childTaskID
	})

	session.Request(4, methodTaskList, map[string]any{})
	taskList := session.WaitForResponse(4)
	if got := nestedString(taskList, "result", "tasks", "0", "task", "id"); got != childTaskID {
		t.Fatalf("task.list first task id = %q, want %q", got, childTaskID)
	}
	if got := nestedString(taskList, "result", "tasks", "1", "task", "id"); got != parentTaskID {
		t.Fatalf("task.list second task id = %q, want %q", got, parentTaskID)
	}

	session.Request(5, methodTaskGet, map[string]any{"task_id": childTaskID})
	childTask := session.WaitForResponse(5)
	if got := nestedString(childTask, "result", "task", "task", "config_alias"); got != "single-agent" {
		t.Fatalf("child config_alias = %q, want %q", got, "single-agent")
	}
	if got := nestedString(childTask, "result", "task", "task", "config_path"); got != configPath {
		t.Fatalf("child config_path = %q, want %q", got, configPath)
	}

	store, err := taskstore.Open(workDir)
	if err != nil {
		t.Fatalf("Open store: %v", err)
	}
	defer store.Close()
	parentID, err := store.GetFollowUpParentTaskID(context.Background(), childTaskID)
	if err != nil {
		t.Fatalf("GetFollowUpParentTaskID(%q): %v", childTaskID, err)
	}
	if parentID != parentTaskID {
		t.Fatalf("follow-up parent id = %q, want %q", parentID, parentTaskID)
	}
}

func TestServerWithRealTaskRuntime_RetryNode(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cfg := singleAgentIntegrationFixture()
	cfg.Topology.MaxIterations = 2
	cfg.Topology.Nodes[0].MaxIterations = 2
	configPath := writeIntegrationConfig(t, cfg)

	executor := &integrationExecutor{
		errors: map[string][]error{
			"implement": {fmt.Errorf("runtime unavailable")},
		},
		steps: map[string][]taskexecutor.Result{
			"implement": {
				{Kind: taskexecutor.ResultKindResult, Result: artifactResult("impl.md")},
			},
		},
	}
	workDir := t.TempDir()
	service, err := taskruntime.NewService(workDir, executor)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server, err := New(Options{
		Service:       service,
		WorkDir:       workDir,
		ServerVersion: "test",
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	session := newIntegrationSession(t, server)
	defer session.Close()

	session.Request(1, methodInitialize, map[string]any{"protocol_version": protocolVersion})
	session.WaitForResponse(1)

	session.Request(2, methodTaskStart, map[string]any{
		"description":  "Retry task",
		"config_alias": "retry-flow",
		"config_path":  configPath,
	})
	if got := nestedBool(session.WaitForResponse(2), "result", "accepted"); !got {
		t.Fatal("task.start accepted = false")
	}
	created := session.WaitForNotification(string(taskruntime.EventTaskCreated), nil)
	taskID := nestedString(created, "params", "event", "task_id")
	failed := session.WaitForNotification(string(taskruntime.EventTaskFailed), func(msg map[string]any) bool {
		return nestedString(msg, "params", "event", "task_id") == taskID
	})
	nodeRunID := nestedString(failed, "params", "event", "node_run_id")
	if nodeRunID == "" {
		t.Fatal("missing failed node_run_id")
	}

	session.Request(3, methodTaskRetryNode, map[string]any{
		"client_command_id": "cmd-retry",
		"task_id":           taskID,
		"node_run_id":       nodeRunID,
	})
	if got := nestedBool(session.WaitForResponse(3), "result", "accepted"); !got {
		t.Fatal("task.retry_node accepted = false")
	}
	retried := session.WaitForNotification(string(taskruntime.EventNodeStarted), func(msg map[string]any) bool {
		return nestedString(msg, "params", "client_command_id") == "cmd-retry"
	})
	if got := nestedString(retried, "params", "event", "task_id"); got != taskID {
		t.Fatalf("retry node.started task_id = %q, want %q", got, taskID)
	}
	session.WaitForNotification(string(taskruntime.EventTaskCompleted), func(msg map[string]any) bool {
		return nestedString(msg, "params", "event", "task_id") == taskID
	})

	session.Request(4, methodTaskGet, map[string]any{"task_id": taskID})
	taskGet := session.WaitForResponse(4)
	if got := nestedString(taskGet, "result", "task", "status"); got != string(taskdomain.TaskStatusDone) {
		t.Fatalf("task status = %q, want %q", got, taskdomain.TaskStatusDone)
	}
	if got := nestedString(taskGet, "result", "task", "node_runs", "1", "triggered_by", "reason"); got != taskdomain.TriggerReasonManualRetry {
		t.Fatalf("retry trigger reason = %q, want %q", got, taskdomain.TriggerReasonManualRetry)
	}
}

func TestServerWithRealTaskRuntime_ContinueBlockedTask(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	configPath := writeIntegrationConfig(t, reviewLoopLimitIntegrationFixture())

	executor := &integrationExecutor{
		steps: map[string][]taskexecutor.Result{
			"draft_plan": {
				{Kind: taskexecutor.ResultKindResult, Result: artifactResult("plan-1.md")},
				{Kind: taskexecutor.ResultKindResult, Result: artifactResult("plan-2.md")},
			},
			"review_plan": {
				{Kind: taskexecutor.ResultKindResult, Result: map[string]interface{}{"passed": false, "file_paths": []interface{}{"review-1.md"}}},
				{Kind: taskexecutor.ResultKindResult, Result: map[string]interface{}{"passed": true, "file_paths": []interface{}{"review-2.md"}}},
			},
		},
	}
	workDir := t.TempDir()
	service, err := taskruntime.NewService(workDir, executor)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server, err := New(Options{
		Service:       service,
		WorkDir:       workDir,
		ServerVersion: "test",
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	session := newIntegrationSession(t, server)
	defer session.Close()

	session.Request(1, methodInitialize, map[string]any{"protocol_version": protocolVersion})
	session.WaitForResponse(1)

	session.Request(2, methodTaskStart, map[string]any{
		"description":  "Continue blocked task",
		"config_alias": "review-loop",
		"config_path":  configPath,
	})
	if got := nestedBool(session.WaitForResponse(2), "result", "accepted"); !got {
		t.Fatal("task.start accepted = false")
	}
	created := session.WaitForNotification(string(taskruntime.EventTaskCreated), nil)
	taskID := nestedString(created, "params", "event", "task_id")
	session.WaitForNotification(string(taskruntime.EventTaskFailed), func(msg map[string]any) bool {
		return nestedString(msg, "params", "event", "task_id") == taskID
	})

	session.Request(3, methodTaskGet, map[string]any{"task_id": taskID})
	blocked := session.WaitForResponse(3)
	if got := nestedString(blocked, "result", "task", "current_issue", "kind"); got != string(taskdomain.TaskIssueBlockedStep) {
		t.Fatalf("current_issue.kind = %q, want %q", got, taskdomain.TaskIssueBlockedStep)
	}
	if got := nestedString(blocked, "result", "task", "blocked_steps", "0", "node_name"); got != "draft_plan" {
		t.Fatalf("blocked_steps[0].node_name = %q, want %q", got, "draft_plan")
	}

	session.Request(4, methodTaskContinueBlocked, map[string]any{
		"client_command_id": "cmd-continue",
		"task_id":           taskID,
	})
	if got := nestedBool(session.WaitForResponse(4), "result", "accepted"); !got {
		t.Fatal("task.continue_blocked accepted = false")
	}
	restarted := session.WaitForNotification(string(taskruntime.EventNodeStarted), func(msg map[string]any) bool {
		return nestedString(msg, "params", "client_command_id") == "cmd-continue"
	})
	if got := nestedString(restarted, "params", "event", "task_id"); got != taskID {
		t.Fatalf("continue node.started task_id = %q, want %q", got, taskID)
	}
	session.WaitForNotification(string(taskruntime.EventTaskCompleted), func(msg map[string]any) bool {
		return nestedString(msg, "params", "event", "task_id") == taskID
	})

	session.Request(5, methodTaskGet, map[string]any{"task_id": taskID})
	completed := session.WaitForResponse(5)
	if got := nestedString(completed, "result", "task", "status"); got != string(taskdomain.TaskStatusDone) {
		t.Fatalf("task status = %q, want %q", got, taskdomain.TaskStatusDone)
	}
	if steps, ok := nestedValue(completed, "result", "task", "blocked_steps").([]any); ok && len(steps) > 0 {
		t.Fatalf("blocked_steps = %v, want empty", steps)
	}
}

type integrationSession struct {
	t       *testing.T
	reader  *frameReader
	writer  *frameWriter
	cleanup func()
	pending []map[string]any
}

func newIntegrationSession(t *testing.T, server *Server) *integrationSession {
	t.Helper()
	reader, inputWriter, cleanup := startLiveServer(t, server)
	return &integrationSession{
		t:       t,
		reader:  reader,
		writer:  newFrameWriter(inputWriter),
		cleanup: cleanup,
	}
}

func (s *integrationSession) Close() {
	s.cleanup()
}

func (s *integrationSession) Request(id int, method string, params map[string]any) {
	s.t.Helper()
	if params == nil {
		params = map[string]any{}
	}
	if err := s.writer.writeFrame(rpcRequestPayload(s.t, id, method, params)); err != nil {
		s.t.Fatalf("writeFrame(%s): %v", method, err)
	}
}

func (s *integrationSession) WaitForResponse(id int) map[string]any {
	s.t.Helper()
	return s.waitFor(fmt.Sprintf("response %d", id), func(msg map[string]any) bool {
		if stringValue(msg["method"]) != "" {
			return false
		}
		gotID, _ := msg["id"].(float64)
		return int(gotID) == id
	})
}

func (s *integrationSession) WaitForNotification(method string, match func(map[string]any) bool) map[string]any {
	s.t.Helper()
	return s.waitFor("notification "+method, func(msg map[string]any) bool {
		if stringValue(msg["method"]) != method {
			return false
		}
		if match == nil {
			return true
		}
		return match(msg)
	})
}

func (s *integrationSession) waitFor(label string, match func(map[string]any) bool) map[string]any {
	s.t.Helper()
	for i, msg := range s.pending {
		if !match(msg) {
			continue
		}
		s.pending = append(s.pending[:i], s.pending[i+1:]...)
		return msg
	}

	deadline := time.After(5 * time.Second)
	for {
		select {
		case <-deadline:
			s.t.Fatalf("timed out waiting for %s", label)
		default:
		}
		msg := s.readMessage()
		if match(msg) {
			return msg
		}
		s.pending = append(s.pending, msg)
	}
}

func (s *integrationSession) readMessage() map[string]any {
	s.t.Helper()
	type frameResult struct {
		frame []byte
		err   error
	}
	resultCh := make(chan frameResult, 1)
	go func() {
		frame, err := s.reader.readFrame()
		resultCh <- frameResult{frame: frame, err: err}
	}()

	select {
	case result := <-resultCh:
		if result.err != nil {
			s.t.Fatalf("readFrame: %v", result.err)
		}
		return decodeFrameMap(s.t, result.frame)
	case <-time.After(5 * time.Second):
		s.t.Fatal("timed out waiting for app-server frame")
		return nil
	}
}

func artifactResult(name string) map[string]interface{} {
	return map[string]interface{}{
		"file_paths": []interface{}{name},
	}
}

func materializeResultArtifacts(req taskexecutor.Request, result taskexecutor.Result) (taskexecutor.Result, error) {
	cloned := taskexecutor.Result{
		SessionID:     result.SessionID,
		Kind:          result.Kind,
		Result:        cloneResultMap(result.Result),
		Clarification: result.Clarification,
	}
	filePaths, ok := cloned.Result["file_paths"].([]interface{})
	if !ok {
		return cloned, nil
	}
	for idx, value := range filePaths {
		name, ok := value.(string)
		if !ok {
			continue
		}
		path := req.ArtifactDir + "/" + name
		if err := osWriteFile(path, []byte("artifact:"+name)); err != nil {
			return taskexecutor.Result{}, err
		}
		filePaths[idx] = path
	}
	cloned.Result["file_paths"] = filePaths
	return cloned, nil
}

func cloneResultMap(input map[string]interface{}) map[string]interface{} {
	if input == nil {
		return nil
	}
	payload, err := json.Marshal(input)
	if err != nil {
		panic(err)
	}
	var cloned map[string]interface{}
	if err := json.Unmarshal(payload, &cloned); err != nil {
		panic(err)
	}
	return cloned
}

func osWriteFile(path string, content []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, content, 0o644)
}

func writeIntegrationConfig(t *testing.T, cfg *taskconfig.Config) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	configDir := filepath.Dir(path)
	for name, def := range cfg.NodeDefinitions {
		if def.Type == taskconfig.NodeTypeHuman || def.Type == taskconfig.NodeTypeTerminal {
			continue
		}
		if def.SystemPrompt == "" {
			def.SystemPrompt = "./prompts/" + name + ".md"
			cfg.NodeDefinitions[name] = def
		}
		promptPath := filepath.Join(configDir, strings.TrimPrefix(def.SystemPrompt, "./"))
		if err := os.MkdirAll(filepath.Dir(promptPath), 0o755); err != nil {
			t.Fatalf("MkdirAll(%q): %v", filepath.Dir(promptPath), err)
		}
		if err := os.WriteFile(promptPath, []byte("# "+name), 0o644); err != nil {
			t.Fatalf("WriteFile(%q): %v", promptPath, err)
		}
	}
	payload, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatalf("Marshal config: %v", err)
	}
	if err := os.WriteFile(path, payload, 0o644); err != nil {
		t.Fatalf("WriteFile(%q): %v", path, err)
	}
	return path
}

func singleAgentIntegrationFixture() *taskconfig.Config {
	return &taskconfig.Config{
		Version: 1,
		Clarification: taskconfig.ClarificationConfig{
			MaxQuestions:          4,
			MaxOptionsPerQuestion: 4,
			MinOptionsPerQuestion: 2,
		},
		Topology: taskconfig.Topology{
			MaxIterations: 1,
			Entry:         "implement",
			Nodes: []taskconfig.NodeRef{
				{Name: "implement"},
				{Name: "done"},
			},
			Edges: []taskconfig.Edge{
				{From: "implement", To: "done"},
			},
		},
		NodeDefinitions: map[string]taskconfig.NodeDefinition{
			"implement": integrationArtifactAgentNode("./prompts/node.md"),
			"done":      {Type: taskconfig.NodeTypeTerminal},
		},
	}
}

func reviewLoopLimitIntegrationFixture() *taskconfig.Config {
	deny := false
	return &taskconfig.Config{
		Version: 1,
		Clarification: taskconfig.ClarificationConfig{
			MaxQuestions:          4,
			MaxOptionsPerQuestion: 4,
			MinOptionsPerQuestion: 2,
		},
		Topology: taskconfig.Topology{
			MaxIterations: 3,
			Entry:         "draft_plan",
			Nodes: []taskconfig.NodeRef{
				{Name: "draft_plan", MaxIterations: 1},
				{Name: "review_plan"},
				{Name: "done"},
			},
			Edges: []taskconfig.Edge{
				{From: "draft_plan", To: "review_plan"},
				{From: "review_plan", To: "draft_plan", When: taskconfig.EdgeCondition{Kind: taskconfig.ConditionWhen, Field: "passed", Equals: false}},
				{From: "review_plan", To: "done", When: taskconfig.EdgeCondition{Kind: taskconfig.ConditionWhen, Field: "passed", Equals: true}},
			},
		},
		NodeDefinitions: map[string]taskconfig.NodeDefinition{
			"draft_plan": integrationArtifactAgentNode("./prompts/draft_plan.md"),
			"review_plan": {
				Type:         taskconfig.NodeTypeAgent,
				SystemPrompt: "./prompts/review_plan.md",
				ResultSchema: taskconfig.JSONSchema{
					Type:                 "object",
					AdditionalProperties: &deny,
					Required:             []string{"passed", "file_paths"},
					Properties: map[string]*taskconfig.JSONSchema{
						"passed":     {Type: "boolean"},
						"file_paths": {Type: "array", MinItems: intPtr(1), Items: &taskconfig.JSONSchema{Type: "string"}},
					},
				},
			},
			"done": {Type: taskconfig.NodeTypeTerminal},
		},
	}
}

func integrationArtifactAgentNode(prompt string) taskconfig.NodeDefinition {
	deny := false
	return taskconfig.NodeDefinition{
		Type:         taskconfig.NodeTypeAgent,
		SystemPrompt: prompt,
		ResultSchema: taskconfig.JSONSchema{
			Type:                 "object",
			AdditionalProperties: &deny,
			Required:             []string{"file_paths"},
			Properties: map[string]*taskconfig.JSONSchema{
				"file_paths": {
					Type:     "array",
					MinItems: intPtr(1),
					Items:    &taskconfig.JSONSchema{Type: "string"},
				},
			},
		},
	}
}

func intPtr(v int) *int {
	return &v
}

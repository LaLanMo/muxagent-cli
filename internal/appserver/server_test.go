package appserver

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	appconfig "github.com/LaLanMo/muxagent-cli/internal/config"
	"github.com/LaLanMo/muxagent-cli/internal/taskconfig"
	"github.com/LaLanMo/muxagent-cli/internal/taskdomain"
	"github.com/LaLanMo/muxagent-cli/internal/taskruntime"
	"github.com/LaLanMo/muxagent-cli/internal/taskstore"
	"github.com/stretchr/testify/require"
)

type fakeService struct {
	events chan taskruntime.RunEvent
	stopCh chan struct{}

	listTasksResult taskListFixture
	loadTaskResult  taskGetFixture
	inputResult     *taskruntime.InputRequest
	inputErr        error
	dispatched      []taskruntime.RunCommand

	prepareShutdownCalls int
	closeCalls           int
}

type taskListFixture struct {
	views []taskdomain.TaskView
	err   error
}

type taskGetFixture struct {
	view   taskdomain.TaskView
	config *taskconfig.Config
	err    error
}

func newFakeService() *fakeService {
	return &fakeService{
		events: make(chan taskruntime.RunEvent, 8),
		stopCh: make(chan struct{}, 1),
	}
}

func (f *fakeService) Run(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-f.stopCh:
		return nil
	}
}

func (f *fakeService) Events() <-chan taskruntime.RunEvent {
	return f.events
}

func (f *fakeService) Dispatch(cmd taskruntime.RunCommand) {
	f.dispatched = append(f.dispatched, cmd)
	if cmd.Type == taskruntime.CommandShutdown {
		f.prepareShutdownCalls++
		select {
		case f.stopCh <- struct{}{}:
		default:
		}
	}
}

func (f *fakeService) ListTaskViews(ctx context.Context, workDir string) ([]taskdomain.TaskView, error) {
	return f.listTasksResult.views, f.listTasksResult.err
}

func (f *fakeService) LoadTaskView(ctx context.Context, taskID string) (taskdomain.TaskView, *taskconfig.Config, error) {
	return f.loadTaskResult.view, f.loadTaskResult.config, f.loadTaskResult.err
}

func (f *fakeService) BuildInputRequest(ctx context.Context, taskID, nodeRunID string) (*taskruntime.InputRequest, error) {
	return f.inputResult, f.inputErr
}

func (f *fakeService) PrepareShutdown(ctx context.Context) error {
	f.prepareShutdownCalls++
	return nil
}

func (f *fakeService) Close() error {
	f.closeCalls++
	return nil
}

func TestServerInitializeAndListTasks(t *testing.T) {
	service := newFakeService()
	service.listTasksResult.views = []taskdomain.TaskView{{
		Task: taskdomain.Task{
			ID:          "task-1",
			Description: "Implement app-server",
			ConfigAlias: "default",
			ConfigPath:  "/tmp/config.yaml",
			WorkDir:     "/tmp/project",
			CreatedAt:   time.Unix(10, 0).UTC(),
			UpdatedAt:   time.Unix(20, 0).UTC(),
		},
		Status: taskdomain.TaskStatusRunning,
	}}

	server := mustNewTestServer(t, Options{
		Service:       service,
		WorkDir:       "/tmp/project",
		ServerVersion: "test",
		LoadCatalog: func() (*taskconfig.Catalog, error) {
			return &taskconfig.Catalog{DefaultAlias: "default"}, nil
		},
		LoadRegistry: func() (taskconfig.Registry, error) {
			return taskconfig.Registry{}, nil
		},
		LoadTaskLaunchPreferences: func() appconfig.TaskLaunchPreferences {
			return appconfig.TaskLaunchPreferences{}
		},
	})

	input := framesFromJSON(
		t,
		rpcRequestPayload(t, 1, methodInitialize, map[string]any{"protocol_version": protocolVersion}),
		rpcRequestPayload(t, 2, methodTaskList, map[string]any{}),
	)
	var output bytes.Buffer
	if err := server.Serve(context.Background(), bytes.NewReader(input), &output); err != nil {
		t.Fatalf("Serve: %v", err)
	}

	messages := decodeOutputFrames(t, output.Bytes())
	if len(messages) != 2 {
		t.Fatalf("frame count = %d, want 2", len(messages))
	}
	if got := nestedString(messages[0], "result", "server_name"); got != "muxagent app-server" {
		t.Fatalf("server_name = %q", got)
	}
	if got := nestedString(messages[1], "result", "tasks", "0", "task", "id"); got != "task-1" {
		t.Fatalf("task id = %q", got)
	}
}

func TestServerReturnsConfigCatalogAndStatus(t *testing.T) {
	service := newFakeService()
	catalog := &taskconfig.Catalog{
		DefaultAlias: "default",
		Entries: []taskconfig.CatalogEntry{{
			Alias: "default",
			Path:  "/configs/default/config.yaml",
			Config: &taskconfig.Config{
				Version:     1,
				Description: "Default flow",
				Runtime:     appconfig.RuntimeCodex,
				Topology: taskconfig.Topology{
					Nodes: []taskconfig.NodeRef{{Name: "plan"}, {Name: "review"}},
				},
			},
			Builtin: true,
		}},
	}

	server := mustNewTestServer(t, Options{
		Service:       service,
		WorkDir:       "/tmp/project",
		ServerVersion: "test",
		LoadCatalog: func() (*taskconfig.Catalog, error) {
			return catalog, nil
		},
		LoadRegistry: func() (taskconfig.Registry, error) {
			return taskconfig.Registry{
				Configs: []taskconfig.RegistryEntry{{Alias: "default", Path: "default"}},
			}, nil
		},
		LoadTaskLaunchPreferences: func() appconfig.TaskLaunchPreferences {
			return appconfig.TaskLaunchPreferences{UseWorktree: true}
		},
		WorktreeAvailable: func(string) bool { return true },
	})

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
	if got := nestedString(messages[1], "result", "entries", "0", "runtime_name"); got != "Codex" {
		t.Fatalf("runtime_name = %q", got)
	}
	if got := nestedBool(messages[2], "result", "default_use_worktree"); !got {
		t.Fatalf("default_use_worktree = false, want true")
	}
	methods := nestedValue(messages[0], "result", "capabilities", "methods")
	list, ok := methods.([]any)
	if !ok {
		t.Fatalf("capabilities.methods = %T, want []any", methods)
	}
	if !containsString(list, methodTaskStart) || !containsString(list, methodTaskContinueBlocked) {
		t.Fatalf("capabilities.methods missing mutating methods: %v", list)
	}
}

func TestServerTaskGetIncludesInputRequest(t *testing.T) {
	service := newFakeService()
	service.loadTaskResult = taskGetFixture{
		view: taskdomain.TaskView{
			Task: taskdomain.Task{
				ID:          "task-1",
				ConfigAlias: "default",
				ConfigPath:  "/tmp/config.yaml",
			},
			Status: taskdomain.TaskStatusAwaitingUser,
			NodeRuns: []taskdomain.NodeRunView{{
				NodeRun: taskdomain.NodeRun{
					ID:     "run-1",
					Status: taskdomain.NodeRunAwaitingUser,
				},
			}},
		},
		config: &taskconfig.Config{Version: 1, Runtime: appconfig.RuntimeCodex},
	}
	service.inputResult = &taskruntime.InputRequest{
		Kind:      taskruntime.InputKindClarification,
		TaskID:    "task-1",
		NodeRunID: "run-1",
		NodeName:  "review",
	}

	server := mustNewTestServer(t, Options{
		Service:       service,
		WorkDir:       "/tmp/project",
		ServerVersion: "test",
		LoadCatalog: func() (*taskconfig.Catalog, error) {
			return &taskconfig.Catalog{DefaultAlias: "default"}, nil
		},
		LoadRegistry: func() (taskconfig.Registry, error) {
			return taskconfig.Registry{}, nil
		},
		LoadTaskLaunchPreferences: func() appconfig.TaskLaunchPreferences {
			return appconfig.TaskLaunchPreferences{}
		},
	})

	input := framesFromJSON(
		t,
		rpcRequestPayload(t, 1, methodInitialize, map[string]any{"protocol_version": protocolVersion}),
		rpcRequestPayload(t, 2, methodTaskGet, map[string]any{"task_id": "task-1"}),
	)
	var output bytes.Buffer
	if err := server.Serve(context.Background(), bytes.NewReader(input), &output); err != nil {
		t.Fatalf("Serve: %v", err)
	}

	messages := decodeOutputFrames(t, output.Bytes())
	if got := nestedString(messages[1], "result", "input_request", "node_run_id"); got != "run-1" {
		t.Fatalf("input_request.node_run_id = %q", got)
	}
}

func TestServerDispatchesMutatingCommands(t *testing.T) {
	tests := []struct {
		name         string
		method       string
		params       map[string]any
		wantCommand  taskruntime.RunCommand
		wantClientID string
	}{
		{
			name:   "task.start",
			method: methodTaskStart,
			params: map[string]any{
				"client_command_id": "cmd-start",
				"description":       "Implement desktop shell",
				"config_alias":      "default",
				"config_path":       "/tmp/default.yaml",
				"use_worktree":      true,
			},
			wantCommand: taskruntime.RunCommand{
				Type:        taskruntime.CommandStartTask,
				Description: "Implement desktop shell",
				ConfigAlias: "default",
				ConfigPath:  "/tmp/default.yaml",
				WorkDir:     "/tmp/project",
				UseWorktree: true,
			},
			wantClientID: "cmd-start",
		},
		{
			name:   "task.start_follow_up",
			method: methodTaskStartFollowUp,
			params: map[string]any{
				"client_command_id": "cmd-follow",
				"parent_task_id":    "task-1",
				"description":       "Tighten review copy",
				"config_alias":      "default",
				"config_path":       "/tmp/default.yaml",
			},
			wantCommand: taskruntime.RunCommand{
				Type:         taskruntime.CommandStartFollowUp,
				ParentTaskID: "task-1",
				Description:  "Tighten review copy",
				ConfigAlias:  "default",
				ConfigPath:   "/tmp/default.yaml",
			},
			wantClientID: "cmd-follow",
		},
		{
			name:   "task.submit_input",
			method: methodTaskSubmitInput,
			params: map[string]any{
				"client_command_id": "cmd-input",
				"task_id":           "task-1",
				"node_run_id":       "run-1",
				"payload":           map[string]any{"approved": true},
			},
			wantCommand: taskruntime.RunCommand{
				Type:      taskruntime.CommandSubmitInput,
				TaskID:    "task-1",
				NodeRunID: "run-1",
				Payload:   map[string]interface{}{"approved": true},
			},
			wantClientID: "cmd-input",
		},
		{
			name:   "task.retry_node",
			method: methodTaskRetryNode,
			params: map[string]any{
				"client_command_id": "cmd-retry",
				"task_id":           "task-1",
				"node_run_id":       "run-2",
				"force":             true,
			},
			wantCommand: taskruntime.RunCommand{
				Type:      taskruntime.CommandRetryNode,
				TaskID:    "task-1",
				NodeRunID: "run-2",
				Force:     true,
			},
			wantClientID: "cmd-retry",
		},
		{
			name:   "task.continue_blocked",
			method: methodTaskContinueBlocked,
			params: map[string]any{
				"client_command_id": "cmd-continue",
				"task_id":           "task-1",
			},
			wantCommand: taskruntime.RunCommand{
				Type:   taskruntime.CommandContinueBlocked,
				TaskID: "task-1",
			},
			wantClientID: "cmd-continue",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := newFakeService()
			server := mustNewTestServer(t, Options{
				Service:       service,
				WorkDir:       "/tmp/project",
				ServerVersion: "test",
				LoadCatalog: func() (*taskconfig.Catalog, error) {
					return &taskconfig.Catalog{DefaultAlias: "default"}, nil
				},
				LoadRegistry: func() (taskconfig.Registry, error) {
					return taskconfig.Registry{}, nil
				},
				LoadTaskLaunchPreferences: func() appconfig.TaskLaunchPreferences {
					return appconfig.TaskLaunchPreferences{}
				},
			})

			input := framesFromJSON(
				t,
				rpcRequestPayload(t, 1, methodInitialize, map[string]any{"protocol_version": protocolVersion}),
				rpcRequestPayload(t, 2, tt.method, tt.params),
			)
			var output bytes.Buffer
			if err := server.Serve(context.Background(), bytes.NewReader(input), &output); err != nil {
				t.Fatalf("Serve: %v", err)
			}

			if len(service.dispatched) != 1 {
				t.Fatalf("dispatched = %d, want 1", len(service.dispatched))
			}
			if !reflect.DeepEqual(service.dispatched[0], tt.wantCommand) {
				t.Fatalf("dispatched = %#v, want %#v", service.dispatched[0], tt.wantCommand)
			}

			messages := decodeOutputFrames(t, output.Bytes())
			if got := nestedBool(messages[1], "result", "accepted"); !got {
				t.Fatalf("accepted = false, want true")
			}
			if got := nestedString(messages[1], "result", "client_command_id"); got != tt.wantClientID {
				t.Fatalf("client_command_id = %q, want %q", got, tt.wantClientID)
			}
		})
	}
}

func TestServerRejectsInvalidMutatingParams(t *testing.T) {
	service := newFakeService()
	server := mustNewTestServer(t, Options{
		Service:       service,
		WorkDir:       "/tmp/project",
		ServerVersion: "test",
		LoadCatalog: func() (*taskconfig.Catalog, error) {
			return &taskconfig.Catalog{DefaultAlias: "default"}, nil
		},
		LoadRegistry: func() (taskconfig.Registry, error) {
			return taskconfig.Registry{}, nil
		},
		LoadTaskLaunchPreferences: func() appconfig.TaskLaunchPreferences {
			return appconfig.TaskLaunchPreferences{}
		},
	})

	input := framesFromJSON(
		t,
		rpcRequestPayload(t, 1, methodInitialize, map[string]any{"protocol_version": protocolVersion}),
		rpcRequestPayload(t, 2, methodTaskStartFollowUp, map[string]any{
			"parent_task_id": "task-1",
			"config_alias":   "default",
		}),
	)
	var output bytes.Buffer
	if err := server.Serve(context.Background(), bytes.NewReader(input), &output); err != nil {
		t.Fatalf("Serve: %v", err)
	}

	if len(service.dispatched) != 0 {
		t.Fatalf("dispatched = %d, want 0", len(service.dispatched))
	}
	messages := decodeOutputFrames(t, output.Bytes())
	if got := nestedString(messages[1], "error", "message"); got != "config_alias and config_path must be provided together" {
		t.Fatalf("error.message = %q", got)
	}
}

func TestServerArtifactListResolvesRelativeRunArtifacts(t *testing.T) {
	workDir := t.TempDir()
	taskID := "1234567890abcdef"
	artifactPath := filepath.Join(taskstore.ArtifactRunDir(workDir, taskID, 1, "draft_plan"), "plan.md")
	if err := os.MkdirAll(filepath.Dir(artifactPath), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(artifactPath, []byte("# plan\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	service := newFakeService()
	service.loadTaskResult = taskGetFixture{
		view: taskdomain.TaskView{
			Task: taskdomain.Task{
				ID:      taskID,
				WorkDir: workDir,
			},
			Status: taskdomain.TaskStatusDone,
			NodeRuns: []taskdomain.NodeRunView{{
				NodeRun: taskdomain.NodeRun{
					ID:        "run-1",
					TaskID:    taskID,
					NodeName:  "draft_plan",
					Status:    taskdomain.NodeRunDone,
					StartedAt: time.Unix(10, 0).UTC(),
				},
				ArtifactPaths: []string{"plan.md"},
			}},
			ArtifactPaths: []string{"plan.md"},
		},
	}

	server := mustNewTestServer(t, Options{
		Service:       service,
		WorkDir:       workDir,
		ServerVersion: "test",
		LoadCatalog: func() (*taskconfig.Catalog, error) {
			return &taskconfig.Catalog{DefaultAlias: "default"}, nil
		},
		LoadRegistry: func() (taskconfig.Registry, error) {
			return taskconfig.Registry{}, nil
		},
		LoadTaskLaunchPreferences: func() appconfig.TaskLaunchPreferences {
			return appconfig.TaskLaunchPreferences{}
		},
	})

	input := framesFromJSON(
		t,
		rpcRequestPayload(t, 1, methodInitialize, map[string]any{"protocol_version": protocolVersion}),
		rpcRequestPayload(t, 2, methodArtifactList, map[string]any{"task_id": taskID}),
	)
	var output bytes.Buffer
	if err := server.Serve(context.Background(), bytes.NewReader(input), &output); err != nil {
		t.Fatalf("Serve: %v", err)
	}

	messages := decodeOutputFrames(t, output.Bytes())
	if got := nestedString(messages[1], "result", "artifacts", "0", "resolved_path"); got != artifactPath {
		t.Fatalf("resolved_path = %q, want %q", got, artifactPath)
	}
	if got := nestedString(messages[1], "result", "artifacts", "0", "source_label"); got != "draft_plan (#1)" {
		t.Fatalf("source_label = %q", got)
	}
	if got := nestedString(messages[1], "result", "artifacts", "0", "display_path"); got != ".muxagent/tasks/12345678/artifacts/01-draft_plan/plan.md" {
		t.Fatalf("display_path = %q", got)
	}
}

func TestServerArtifactListRejectsPathsOutsideWorkspace(t *testing.T) {
	workDir := t.TempDir()
	outsidePath := filepath.Join(t.TempDir(), "secrets.txt")
	require.NoError(t, os.WriteFile(outsidePath, []byte("secret"), 0o644))

	service := newFakeService()
	service.loadTaskResult = taskGetFixture{
		view: taskdomain.TaskView{
			Task: taskdomain.Task{
				ID:      "task-1",
				WorkDir: workDir,
			},
			Status: taskdomain.TaskStatusDone,
			NodeRuns: []taskdomain.NodeRunView{{
				NodeRun: taskdomain.NodeRun{
					ID:        "run-1",
					TaskID:    "task-1",
					NodeName:  "draft_plan",
					Status:    taskdomain.NodeRunDone,
					StartedAt: time.Unix(10, 0).UTC(),
				},
				ArtifactPaths: []string{outsidePath, "../../../../../../escape.md"},
			}},
		},
	}

	server := mustNewTestServer(t, Options{
		Service:       service,
		WorkDir:       workDir,
		ServerVersion: "test",
	})

	input := framesFromJSON(
		t,
		rpcRequestPayload(t, 1, methodInitialize, map[string]any{"protocol_version": protocolVersion}),
		rpcRequestPayload(t, 2, methodArtifactList, map[string]any{"task_id": "task-1"}),
	)
	var output bytes.Buffer
	if err := server.Serve(context.Background(), bytes.NewReader(input), &output); err != nil {
		t.Fatalf("Serve: %v", err)
	}

	messages := decodeOutputFrames(t, output.Bytes())
	artifacts, _ := nestedValue(messages[1], "result", "artifacts").([]any)
	if len(artifacts) != 0 {
		t.Fatalf("artifact count = %d, want 0", len(artifacts))
	}
}

func TestServerArtifactListIncludesInheritedWorkspaceArtifacts(t *testing.T) {
	workDir := t.TempDir()
	parentArtifact := filepath.Join(workDir, ".muxagent", "tasks", "parent-task", "artifacts", "01-draft_plan", "plan.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(parentArtifact), 0o755))
	require.NoError(t, os.WriteFile(parentArtifact, []byte("# inherited\n"), 0o644))

	service := newFakeService()
	service.loadTaskResult = taskGetFixture{
		view: taskdomain.TaskView{
			Task: taskdomain.Task{
				ID:      "child-task",
				WorkDir: workDir,
			},
			Status: taskdomain.TaskStatusAwaitingUser,
			NodeRuns: []taskdomain.NodeRunView{{
				NodeRun: taskdomain.NodeRun{
					ID:        "await-run",
					TaskID:    "child-task",
					NodeName:  "approval",
					Status:    taskdomain.NodeRunAwaitingUser,
					StartedAt: time.Unix(10, 0).UTC(),
				},
			}},
		},
	}
	service.inputResult = &taskruntime.InputRequest{
		TaskID:        "child-task",
		NodeRunID:     "await-run",
		NodeName:      "approval",
		ArtifactPaths: []string{parentArtifact},
	}

	server := mustNewTestServer(t, Options{
		Service:       service,
		WorkDir:       workDir,
		ServerVersion: "test",
	})

	input := framesFromJSON(
		t,
		rpcRequestPayload(t, 1, methodInitialize, map[string]any{"protocol_version": protocolVersion}),
		rpcRequestPayload(t, 2, methodArtifactList, map[string]any{"task_id": "child-task"}),
	)
	var output bytes.Buffer
	if err := server.Serve(context.Background(), bytes.NewReader(input), &output); err != nil {
		t.Fatalf("Serve: %v", err)
	}

	messages := decodeOutputFrames(t, output.Bytes())
	if got := nestedString(messages[1], "result", "artifacts", "0", "resolved_path"); got != parentArtifact {
		t.Fatalf("resolved_path = %q, want %q", got, parentArtifact)
	}
}

func TestServerServiceShutdownQueuesRuntimeCommand(t *testing.T) {
	service := newFakeService()
	server := mustNewTestServer(t, Options{
		Service:       service,
		WorkDir:       "/tmp/project",
		ServerVersion: "test",
		LoadCatalog: func() (*taskconfig.Catalog, error) {
			return &taskconfig.Catalog{DefaultAlias: "default"}, nil
		},
		LoadRegistry: func() (taskconfig.Registry, error) {
			return taskconfig.Registry{}, nil
		},
		LoadTaskLaunchPreferences: func() appconfig.TaskLaunchPreferences {
			return appconfig.TaskLaunchPreferences{}
		},
	})

	input := framesFromJSON(
		t,
		rpcRequestPayload(t, 1, methodInitialize, map[string]any{"protocol_version": protocolVersion}),
		rpcRequestPayload(t, 2, methodServiceShutdown, map[string]any{}),
	)
	var output bytes.Buffer
	if err := server.Serve(context.Background(), bytes.NewReader(input), &output); err != nil {
		t.Fatalf("Serve: %v", err)
	}
	if service.prepareShutdownCalls != 1 {
		t.Fatalf("prepareShutdownCalls = %d, want 1", service.prepareShutdownCalls)
	}
	if len(service.dispatched) != 1 || service.dispatched[0].Type != taskruntime.CommandShutdown {
		t.Fatalf("dispatched = %#v, want queued task.shutdown command", service.dispatched)
	}
}

func TestServerTaskInputRequestMapsLookupFailuresToInvalidParams(t *testing.T) {
	service := newFakeService()
	service.inputErr = fmt.Errorf("%w: node run %q is not awaiting user input", taskruntime.ErrNodeRunNotAwaitingUser, "run-1")
	server := mustNewTestServer(t, Options{
		Service:       service,
		WorkDir:       "/tmp/project",
		ServerVersion: "test",
	})

	input := framesFromJSON(
		t,
		rpcRequestPayload(t, 1, methodInitialize, map[string]any{"protocol_version": protocolVersion}),
		rpcRequestPayload(t, 2, methodTaskInputRequest, map[string]any{"task_id": "task-1", "node_run_id": "run-1"}),
	)
	var output bytes.Buffer
	if err := server.Serve(context.Background(), bytes.NewReader(input), &output); err != nil {
		t.Fatalf("Serve: %v", err)
	}

	messages := decodeOutputFrames(t, output.Bytes())
	got, _ := nestedValue(messages[1], "error", "code").(float64)
	if got != float64(errorCodeInvalidParams) {
		t.Fatalf("error.code = %v, want %d", got, errorCodeInvalidParams)
	}
}

func TestRuntimeLookupRPCErrorMapsSQLNoRowsToInvalidParams(t *testing.T) {
	rpcErr := runtimeLookupRPCError(sql.ErrNoRows)
	if rpcErr.Code != errorCodeInvalidParams {
		t.Fatalf("rpcErr.Code = %d, want %d", rpcErr.Code, errorCodeInvalidParams)
	}
}

func TestServerCorrelatesClientCommandIDOnCausalNotification(t *testing.T) {
	tests := []struct {
		name           string
		method         string
		params         map[string]any
		event          taskruntime.RunEvent
		wantClientID   string
		wantEventType  string
		wantEventTask  string
		wantEventRunID string
	}{
		{
			name:   "task.start uses task.created",
			method: methodTaskStart,
			params: map[string]any{
				"client_command_id": "cmd-start",
				"description":       "Implement desktop shell",
				"config_alias":      "default",
				"config_path":       "/tmp/default.yaml",
			},
			event: taskruntime.RunEvent{
				Type:   taskruntime.EventTaskCreated,
				TaskID: "task-1",
			},
			wantClientID:  "cmd-start",
			wantEventType: string(taskruntime.EventTaskCreated),
			wantEventTask: "task-1",
		},
		{
			name:   "task.submit_input uses node scoped event",
			method: methodTaskSubmitInput,
			params: map[string]any{
				"client_command_id": "cmd-input",
				"task_id":           "task-1",
				"node_run_id":       "run-1",
				"payload":           map[string]any{"approved": true},
			},
			event: taskruntime.RunEvent{
				Type:      taskruntime.EventNodeCompleted,
				TaskID:    "task-1",
				NodeRunID: "run-1",
			},
			wantClientID:   "cmd-input",
			wantEventType:  string(taskruntime.EventNodeCompleted),
			wantEventTask:  "task-1",
			wantEventRunID: "run-1",
		},
		{
			name:   "task.continue_blocked accepts human restart",
			method: methodTaskContinueBlocked,
			params: map[string]any{
				"client_command_id": "cmd-continue",
				"task_id":           "task-1",
			},
			event: taskruntime.RunEvent{
				Type:      taskruntime.EventInputRequested,
				TaskID:    "task-1",
				NodeRunID: "run-2",
			},
			wantClientID:   "cmd-continue",
			wantEventType:  string(taskruntime.EventInputRequested),
			wantEventTask:  "task-1",
			wantEventRunID: "run-2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := newFakeService()
			server := mustNewTestServer(t, Options{
				Service:       service,
				WorkDir:       "/tmp/project",
				ServerVersion: "test",
				LoadCatalog: func() (*taskconfig.Catalog, error) {
					return &taskconfig.Catalog{DefaultAlias: "default"}, nil
				},
				LoadRegistry: func() (taskconfig.Registry, error) {
					return taskconfig.Registry{}, nil
				},
				LoadTaskLaunchPreferences: func() appconfig.TaskLaunchPreferences {
					return appconfig.TaskLaunchPreferences{}
				},
			})

			outputFrames, inputWriter, cleanup := startLiveServer(t, server)
			defer cleanup()

			if _, err := inputWriter.Write(framesFromJSON(
				t,
				rpcRequestPayload(t, 1, methodInitialize, map[string]any{"protocol_version": protocolVersion}),
				rpcRequestPayload(t, 2, tt.method, tt.params),
			)); err != nil {
				t.Fatalf("write requests: %v", err)
			}

			readFrameMapFromReader(t, outputFrames)
			ackMsg := readFrameMapFromReader(t, outputFrames)
			if got := nestedString(ackMsg, "result", "client_command_id"); got != tt.wantClientID {
				t.Fatalf("ack client_command_id = %q, want %q", got, tt.wantClientID)
			}

			service.events <- tt.event
			eventMsg := readFrameMapFromReader(t, outputFrames)
			if got := stringValue(eventMsg["method"]); got != tt.wantEventType {
				t.Fatalf("method = %q, want %q", got, tt.wantEventType)
			}
			if got := nestedString(eventMsg, "params", "client_command_id"); got != tt.wantClientID {
				t.Fatalf("notification client_command_id = %q, want %q", got, tt.wantClientID)
			}
			if got := nestedString(eventMsg, "params", "event", "task_id"); got != tt.wantEventTask {
				t.Fatalf("event task_id = %q, want %q", got, tt.wantEventTask)
			}
			if tt.wantEventRunID != "" {
				if got := nestedString(eventMsg, "params", "event", "node_run_id"); got != tt.wantEventRunID {
					t.Fatalf("event node_run_id = %q, want %q", got, tt.wantEventRunID)
				}
			}
		})
	}
}

func TestServerCorrelatesClientCommandIDOnDispatchCommandError(t *testing.T) {
	service := newFakeService()
	server := mustNewTestServer(t, Options{
		Service:       service,
		WorkDir:       "/tmp/project",
		ServerVersion: "test",
		LoadCatalog: func() (*taskconfig.Catalog, error) {
			return &taskconfig.Catalog{DefaultAlias: "default"}, nil
		},
		LoadRegistry: func() (taskconfig.Registry, error) {
			return taskconfig.Registry{}, nil
		},
		LoadTaskLaunchPreferences: func() appconfig.TaskLaunchPreferences {
			return appconfig.TaskLaunchPreferences{}
		},
	})

	outputFrames, inputWriter, cleanup := startLiveServer(t, server)
	defer cleanup()

	if _, err := inputWriter.Write(framesFromJSON(
		t,
		rpcRequestPayload(t, 1, methodInitialize, map[string]any{"protocol_version": protocolVersion}),
		rpcRequestPayload(t, 2, methodTaskRetryNode, map[string]any{
			"client_command_id": "cmd-retry",
			"task_id":           "task-1",
			"node_run_id":       "run-1",
		}),
	)); err != nil {
		t.Fatalf("write requests: %v", err)
	}

	readFrameMapFromReader(t, outputFrames)
	readFrameMapFromReader(t, outputFrames)

	service.events <- taskruntime.RunEvent{
		Type:   taskruntime.EventCommandError,
		TaskID: "task-1",
		Error:  &taskruntime.RunError{Message: "retry unavailable"},
	}

	eventMsg := readFrameMapFromReader(t, outputFrames)
	if got := stringValue(eventMsg["method"]); got != string(taskruntime.EventCommandError) {
		t.Fatalf("method = %q", got)
	}
	if got := nestedString(eventMsg, "params", "client_command_id"); got != "cmd-retry" {
		t.Fatalf("notification client_command_id = %q, want %q", got, "cmd-retry")
	}
}

func TestServerIgnoresNodeExecutionCommandErrorForCorrelation(t *testing.T) {
	service := newFakeService()
	server := mustNewTestServer(t, Options{
		Service:       service,
		WorkDir:       "/tmp/project",
		ServerVersion: "test",
		LoadCatalog: func() (*taskconfig.Catalog, error) {
			return &taskconfig.Catalog{DefaultAlias: "default"}, nil
		},
		LoadRegistry: func() (taskconfig.Registry, error) {
			return taskconfig.Registry{}, nil
		},
		LoadTaskLaunchPreferences: func() appconfig.TaskLaunchPreferences {
			return appconfig.TaskLaunchPreferences{}
		},
	})

	outputFrames, inputWriter, cleanup := startLiveServer(t, server)
	defer cleanup()

	if _, err := inputWriter.Write(framesFromJSON(
		t,
		rpcRequestPayload(t, 1, methodInitialize, map[string]any{"protocol_version": protocolVersion}),
		rpcRequestPayload(t, 2, methodTaskStart, map[string]any{
			"client_command_id": "cmd-start",
			"description":       "Implement desktop shell",
			"config_alias":      "default",
			"config_path":       "/tmp/default.yaml",
		}),
	)); err != nil {
		t.Fatalf("write requests: %v", err)
	}

	readFrameMapFromReader(t, outputFrames)
	readFrameMapFromReader(t, outputFrames)

	service.events <- taskruntime.RunEvent{
		Type:      taskruntime.EventCommandError,
		TaskID:    "task-9",
		NodeRunID: "run-9",
		NodeName:  "implement",
		Error:     &taskruntime.RunError{Message: "executor failed"},
	}
	firstEvent := readFrameMapFromReader(t, outputFrames)
	if got := nestedString(firstEvent, "params", "client_command_id"); got != "" {
		t.Fatalf("runtime command error client_command_id = %q, want empty", got)
	}

	service.events <- taskruntime.RunEvent{
		Type:   taskruntime.EventTaskCreated,
		TaskID: "task-1",
	}
	secondEvent := readFrameMapFromReader(t, outputFrames)
	if got := nestedString(secondEvent, "params", "client_command_id"); got != "cmd-start" {
		t.Fatalf("task.created client_command_id = %q, want %q", got, "cmd-start")
	}
}

func TestServerForwardsRunEventNotifications(t *testing.T) {
	service := newFakeService()
	server := mustNewTestServer(t, Options{
		Service:       service,
		WorkDir:       "/tmp/project",
		ServerVersion: "test",
		LoadCatalog: func() (*taskconfig.Catalog, error) {
			return &taskconfig.Catalog{DefaultAlias: "default"}, nil
		},
		LoadRegistry: func() (taskconfig.Registry, error) {
			return taskconfig.Registry{}, nil
		},
		LoadTaskLaunchPreferences: func() appconfig.TaskLaunchPreferences {
			return appconfig.TaskLaunchPreferences{}
		},
	})

	inputReader, inputWriter := io.Pipe()
	outputReader, outputWriter := io.Pipe()

	var (
		serveErr error
		wg       sync.WaitGroup
	)
	wg.Add(1)
	go func() {
		defer wg.Done()
		serveErr = server.Serve(context.Background(), inputReader, outputWriter)
	}()

	if _, err := inputWriter.Write(framesFromJSON(t, rpcRequestPayload(t, 1, methodInitialize, map[string]any{"protocol_version": protocolVersion}))); err != nil {
		t.Fatalf("write initialize: %v", err)
	}

	outputFrames := newFrameReader(outputReader)
	initFrame, err := outputFrames.readFrame()
	if err != nil {
		t.Fatalf("read initialize response: %v", err)
	}
	initMsg := decodeFrameMap(t, initFrame)
	if got := nestedString(initMsg, "result", "server_name"); got != "muxagent app-server" {
		t.Fatalf("server_name = %q", got)
	}

	service.events <- taskruntime.RunEvent{
		Type:     taskruntime.EventTaskCreated,
		TaskID:   "task-1",
		NodeName: "plan",
	}
	eventFrame, err := outputFrames.readFrame()
	if err != nil {
		t.Fatalf("read event notification: %v", err)
	}
	eventMsg := decodeFrameMap(t, eventFrame)
	if got := stringValue(eventMsg["method"]); got != string(taskruntime.EventTaskCreated) {
		t.Fatalf("method = %q", got)
	}
	if got := nestedString(eventMsg, "params", "event", "task_id"); got != "task-1" {
		t.Fatalf("task_id = %q", got)
	}
	if got := nestedString(eventMsg, "params", "client_command_id"); got != "" {
		t.Fatalf("client_command_id = %q, want empty", got)
	}

	_ = inputWriter.Close()
	_ = outputReader.Close()
	wg.Wait()
	if serveErr != nil {
		t.Fatalf("Serve: %v", serveErr)
	}
}

func TestServerAttachesClientCommandIDToFirstCausalNotification(t *testing.T) {
	service := newFakeService()
	server := mustNewTestServer(t, Options{
		Service:       service,
		WorkDir:       "/tmp/project",
		ServerVersion: "test",
		LoadCatalog: func() (*taskconfig.Catalog, error) {
			return &taskconfig.Catalog{DefaultAlias: "default"}, nil
		},
		LoadRegistry: func() (taskconfig.Registry, error) {
			return taskconfig.Registry{}, nil
		},
		LoadTaskLaunchPreferences: func() appconfig.TaskLaunchPreferences {
			return appconfig.TaskLaunchPreferences{}
		},
	})

	inputReader, inputWriter := io.Pipe()
	outputReader, outputWriter := io.Pipe()

	var (
		serveErr error
		wg       sync.WaitGroup
	)
	wg.Add(1)
	go func() {
		defer wg.Done()
		serveErr = server.Serve(context.Background(), inputReader, outputWriter)
	}()

	if _, err := inputWriter.Write(framesFromJSON(
		t,
		rpcRequestPayload(t, 1, methodInitialize, map[string]any{"protocol_version": protocolVersion}),
		rpcRequestPayload(t, 2, methodTaskStart, map[string]any{
			"client_command_id": "cmd-start",
			"description":       "Start task",
			"config_alias":      "default",
			"config_path":       "/tmp/default.yaml",
		}),
	)); err != nil {
		t.Fatalf("write requests: %v", err)
	}

	outputFrames := newFrameReader(outputReader)
	if _, err := outputFrames.readFrame(); err != nil {
		t.Fatalf("read initialize response: %v", err)
	}
	startFrame, err := outputFrames.readFrame()
	if err != nil {
		t.Fatalf("read start response: %v", err)
	}
	startMsg := decodeFrameMap(t, startFrame)
	if got := nestedString(startMsg, "result", "client_command_id"); got != "cmd-start" {
		t.Fatalf("start client_command_id = %q", got)
	}

	service.events <- taskruntime.RunEvent{
		Type:   taskruntime.EventTaskCreated,
		TaskID: "task-1",
	}
	eventFrame, err := outputFrames.readFrame()
	if err != nil {
		t.Fatalf("read event notification: %v", err)
	}
	eventMsg := decodeFrameMap(t, eventFrame)
	if got := nestedString(eventMsg, "params", "client_command_id"); got != "cmd-start" {
		t.Fatalf("params.client_command_id = %q, want %q", got, "cmd-start")
	}

	_ = inputWriter.Close()
	_ = outputReader.Close()
	wg.Wait()
	if serveErr != nil {
		t.Fatalf("Serve: %v", serveErr)
	}
}

func TestServerAttachesClientCommandIDToCommandErrorNotification(t *testing.T) {
	service := newFakeService()
	server := mustNewTestServer(t, Options{
		Service:       service,
		WorkDir:       "/tmp/project",
		ServerVersion: "test",
		LoadCatalog: func() (*taskconfig.Catalog, error) {
			return &taskconfig.Catalog{DefaultAlias: "default"}, nil
		},
		LoadRegistry: func() (taskconfig.Registry, error) {
			return taskconfig.Registry{}, nil
		},
		LoadTaskLaunchPreferences: func() appconfig.TaskLaunchPreferences {
			return appconfig.TaskLaunchPreferences{}
		},
	})

	inputReader, inputWriter := io.Pipe()
	outputReader, outputWriter := io.Pipe()

	var (
		serveErr error
		wg       sync.WaitGroup
	)
	wg.Add(1)
	go func() {
		defer wg.Done()
		serveErr = server.Serve(context.Background(), inputReader, outputWriter)
	}()

	if _, err := inputWriter.Write(framesFromJSON(
		t,
		rpcRequestPayload(t, 1, methodInitialize, map[string]any{"protocol_version": protocolVersion}),
		rpcRequestPayload(t, 2, methodTaskSubmitInput, map[string]any{
			"client_command_id": "cmd-input",
			"task_id":           "task-1",
			"node_run_id":       "run-1",
			"payload":           map[string]any{"approved": true},
		}),
	)); err != nil {
		t.Fatalf("write requests: %v", err)
	}

	outputFrames := newFrameReader(outputReader)
	if _, err := outputFrames.readFrame(); err != nil {
		t.Fatalf("read initialize response: %v", err)
	}
	if _, err := outputFrames.readFrame(); err != nil {
		t.Fatalf("read submit response: %v", err)
	}

	service.events <- taskruntime.RunEvent{
		Type:   taskruntime.EventCommandError,
		TaskID: "task-1",
		Error:  &taskruntime.RunError{Message: "boom"},
	}
	eventFrame, err := outputFrames.readFrame()
	if err != nil {
		t.Fatalf("read event notification: %v", err)
	}
	eventMsg := decodeFrameMap(t, eventFrame)
	if got := nestedString(eventMsg, "params", "client_command_id"); got != "cmd-input" {
		t.Fatalf("params.client_command_id = %q, want %q", got, "cmd-input")
	}

	_ = inputWriter.Close()
	_ = outputReader.Close()
	wg.Wait()
	if serveErr != nil {
		t.Fatalf("Serve: %v", serveErr)
	}
}

func mustNewTestServer(t *testing.T, opts Options) *Server {
	t.Helper()
	server, err := New(opts)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return server
}

func rpcRequestPayload(t *testing.T, id int, method string, params map[string]any) []byte {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"jsonrpc": jsonRPCVersion,
		"id":      id,
		"method":  method,
		"params":  params,
	})
	if err != nil {
		t.Fatalf("Marshal request: %v", err)
	}
	return payload
}

func framesFromJSON(t *testing.T, payloads ...[]byte) []byte {
	t.Helper()
	var out bytes.Buffer
	writer := newFrameWriter(&out)
	for _, payload := range payloads {
		if err := writer.writeFrame(payload); err != nil {
			t.Fatalf("writeFrame: %v", err)
		}
	}
	return out.Bytes()
}

func decodeOutputFrames(t *testing.T, payload []byte) []map[string]any {
	t.Helper()
	reader := newFrameReader(bytes.NewReader(payload))
	var messages []map[string]any
	for {
		frame, err := reader.readFrame()
		if err == io.EOF {
			return messages
		}
		if err != nil {
			t.Fatalf("readFrame: %v", err)
		}
		messages = append(messages, decodeFrameMap(t, frame))
	}
}

func decodeFrameMap(t *testing.T, frame []byte) map[string]any {
	t.Helper()
	var decoded map[string]any
	if err := json.Unmarshal(frame, &decoded); err != nil {
		t.Fatalf("Unmarshal frame: %v", err)
	}
	return decoded
}

func readFrameMapFromReader(t *testing.T, reader *frameReader) map[string]any {
	t.Helper()
	frame, err := reader.readFrame()
	if err != nil {
		t.Fatalf("readFrame: %v", err)
	}
	return decodeFrameMap(t, frame)
}

func startLiveServer(t *testing.T, server *Server) (*frameReader, *io.PipeWriter, func()) {
	t.Helper()

	inputReader, inputWriter := io.Pipe()
	outputReader, outputWriter := io.Pipe()

	var (
		serveErr error
		wg       sync.WaitGroup
	)
	wg.Add(1)
	go func() {
		defer wg.Done()
		serveErr = server.Serve(context.Background(), inputReader, outputWriter)
	}()

	cleanup := func() {
		_ = inputWriter.Close()
		_ = outputReader.Close()
		wg.Wait()
		if serveErr != nil {
			t.Fatalf("Serve: %v", serveErr)
		}
	}

	return newFrameReader(outputReader), inputWriter, cleanup
}

func nestedString(root map[string]any, path ...string) string {
	value := nestedValue(root, path...)
	return stringValue(value)
}

func nestedBool(root map[string]any, path ...string) bool {
	value := nestedValue(root, path...)
	flag, _ := value.(bool)
	return flag
}

func nestedValue(root map[string]any, path ...string) any {
	current := any(root)
	for _, part := range path {
		switch typed := current.(type) {
		case map[string]any:
			current = typed[part]
		case []any:
			index := 0
			fmtSscanf(part, &index)
			if index < 0 || index >= len(typed) {
				return nil
			}
			current = typed[index]
		default:
			return nil
		}
	}
	return current
}

func fmtSscanf(input string, target *int) {
	var parsed int
	_, _ = fmt.Sscanf(input, "%d", &parsed)
	*target = parsed
}

func stringValue(value any) string {
	text, _ := value.(string)
	return text
}

func containsString(values []any, want string) bool {
	for _, value := range values {
		if stringValue(value) == want {
			return true
		}
	}
	return false
}

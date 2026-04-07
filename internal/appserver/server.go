package appserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	appconfig "github.com/LaLanMo/muxagent-cli/internal/config"
	"github.com/LaLanMo/muxagent-cli/internal/filelock"
	"github.com/LaLanMo/muxagent-cli/internal/taskconfig"
	"github.com/LaLanMo/muxagent-cli/internal/taskdomain"
	"github.com/LaLanMo/muxagent-cli/internal/taskruntime"
	"github.com/LaLanMo/muxagent-cli/internal/worktree"
	"github.com/google/uuid"
	"gopkg.in/yaml.v3"
)

type Options struct {
	StateDir                  string
	ServerVersion             string
	LoadConfig                func() (appconfig.Config, error)
	LoadCatalog               func() (*taskconfig.Catalog, error)
	LoadRegistry              func() (taskconfig.Registry, error)
	LoadTaskLaunchPreferences func() appconfig.TaskLaunchPreferences
	WorktreeAvailable         func(string) bool
	RuntimeFactory            runtimeServiceFactory
	Now                       func() time.Time
}

type Server struct {
	stateDir                  string
	serverVersion             string
	runtimeCount              int
	loadConfig                func() (appconfig.Config, error)
	loadCatalog               func() (*taskconfig.Catalog, error)
	loadRegistry              func() (taskconfig.Registry, error)
	loadTaskLaunchPreferences func() appconfig.TaskLaunchPreferences
	worktreeAvailable         func(string) bool
	now                       func() time.Time
	lockPath                  string
	registry                  *workspaceRegistry
	runtimes                  *runtimeManager

	mu               sync.Mutex
	initialized      bool
	connectedClients int
	pendingCommands  []pendingClientCommand
	notificationSink func(notification)
}

type stopMode int

const (
	stopModeContinue stopMode = iota
	stopModeDrainAndExit
)

func New(opts Options) (*Server, error) {
	if opts.LoadConfig == nil {
		opts.LoadConfig = appconfig.LoadEffective
	}
	if opts.LoadCatalog == nil {
		opts.LoadCatalog = taskconfig.LoadCatalog
	}
	if opts.LoadRegistry == nil {
		opts.LoadRegistry = taskconfig.LoadRegistry
	}
	if opts.LoadTaskLaunchPreferences == nil {
		opts.LoadTaskLaunchPreferences = appconfig.LoadTaskLaunchPreferences
	}
	if opts.WorktreeAvailable == nil {
		opts.WorktreeAvailable = func(path string) bool {
			_, err := worktree.FindRepoRoot(path)
			return err == nil
		}
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}

	stateDir, err := resolveStateDir(opts.StateDir)
	if err != nil {
		return nil, err
	}
	cfg, err := opts.LoadConfig()
	if err != nil {
		return nil, fmt.Errorf("load effective config: %w", err)
	}
	registry := newWorkspaceRegistry(workspacesFilePath(stateDir), opts.Now)
	if err := registry.Load(); err != nil {
		return nil, fmt.Errorf("load workspace registry: %w", err)
	}

	s := &Server{
		stateDir:                  stateDir,
		serverVersion:             opts.ServerVersion,
		runtimeCount:              len(cfg.ConfiguredRuntimeIDs()),
		loadConfig:                opts.LoadConfig,
		loadCatalog:               opts.LoadCatalog,
		loadRegistry:              opts.LoadRegistry,
		loadTaskLaunchPreferences: opts.LoadTaskLaunchPreferences,
		worktreeAvailable:         opts.WorktreeAvailable,
		now:                       opts.Now,
		lockPath:                  singletonLockPath(stateDir),
		registry:                  registry,
	}
	s.runtimes = newRuntimeManager(opts.RuntimeFactory, s.handleRuntimeEvent)
	return s, nil
}

func (s *Server) StateDir() string {
	return s.stateDir
}

func (s *Server) Serve(ctx context.Context, stdin io.Reader, stdout io.Writer) (err error) {
	lock, err := filelock.Acquire(s.lockPath, "muxagent app-server is already running")
	if err != nil {
		return err
	}
	defer func() {
		_ = lock.Release()
	}()

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	s.setConnectedClients(1)
	defer s.setConnectedClients(0)

	reader := newFrameReader(stdin)
	writer := newFrameWriter(stdout)
	outgoing := make(chan any, 256)
	writeErrCh := make(chan error, 1)
	writerDone := make(chan struct{})

	go func() {
		defer close(writerDone)
		for payload := range outgoing {
			if err := writer.writeJSON(payload); err != nil {
				select {
				case writeErrCh <- err:
				default:
				}
				return
			}
		}
	}()

	s.setNotificationSink(func(n notification) {
		select {
		case outgoing <- n:
		case <-ctx.Done():
		}
	})
	defer s.setNotificationSink(nil)
	defer func() {
		_ = s.runtimes.closeAll()
		close(outgoing)
		<-writerDone
		select {
		case writeErr := <-writeErrCh:
			if err == nil && writeErr != nil {
				err = writeErr
			}
		default:
		}
	}()

	for {
		select {
		case writeErr := <-writeErrCh:
			if writeErr != nil {
				return writeErr
			}
		default:
		}

		frame, readErr := reader.readFrame()
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return nil
			}
			return readErr
		}

		stopAfter, handleErr := s.handleFrame(ctx, frame, outgoing)
		if handleErr != nil {
			return handleErr
		}
		if stopAfter == stopModeDrainAndExit {
			return nil
		}
	}
}

func (s *Server) handleFrame(ctx context.Context, frame []byte, outgoing chan<- any) (stopMode, error) {
	var msg incomingMessage
	if err := json.Unmarshal(frame, &msg); err != nil {
		return stopModeContinue, enqueueJSON(outgoing, response{
			JSONRPC: jsonRPCVersion,
			ID:      json.RawMessage("null"),
			Error:   &rpcError{Code: errorCodeParseError, Message: "parse error"},
		})
	}
	if msg.JSONRPC != jsonRPCVersion {
		return stopModeContinue, enqueueJSON(outgoing, response{
			JSONRPC: jsonRPCVersion,
			ID:      requestIDOrNull(msg.ID),
			Error:   &rpcError{Code: errorCodeInvalidRequest, Message: "jsonrpc must be 2.0"},
		})
	}
	if msg.isNotification() {
		return stopModeContinue, nil
	}
	if !msg.isRequest() {
		return stopModeContinue, enqueueJSON(outgoing, response{
			JSONRPC: jsonRPCVersion,
			ID:      requestIDOrNull(msg.ID),
			Error:   &rpcError{Code: errorCodeInvalidRequest, Message: "request must include method and id"},
		})
	}
	if !s.isInitialized() && msg.Method != methodInitialize {
		return stopModeContinue, enqueueJSON(outgoing, response{
			JSONRPC: jsonRPCVersion,
			ID:      requestIDOrNull(msg.ID),
			Error:   &rpcError{Code: errorCodeNotInitialized, Message: "server not initialized"},
		})
	}

	result, notifications, stopAfter, rpcErr := s.handleRequest(ctx, request{
		JSONRPC: msg.JSONRPC,
		ID:      msg.ID,
		Method:  msg.Method,
		Params:  msg.Params,
	})
	if err := enqueueJSON(outgoing, response{
		JSONRPC: jsonRPCVersion,
		ID:      msg.ID,
		Result:  result,
		Error:   rpcErr,
	}); err != nil {
		return stopModeContinue, err
	}
	for _, outgoingNotification := range notifications {
		if err := enqueueJSON(outgoing, outgoingNotification); err != nil {
			return stopModeContinue, err
		}
	}
	return stopAfter, nil
}

func (s *Server) handleRequest(ctx context.Context, req request) (any, []notification, stopMode, *rpcError) {
	switch req.Method {
	case methodInitialize:
		params, err := decodeParams[initializeParams](req.Params)
		if err != nil {
			return nil, nil, stopModeContinue, &rpcError{Code: errorCodeInvalidParams, Message: err.Error()}
		}
		if params.ProtocolVersion != protocolVersion {
			return nil, nil, stopModeContinue, &rpcError{Code: errorCodeInvalidParams, Message: fmt.Sprintf("unsupported protocol version %d", params.ProtocolVersion)}
		}
		s.markInitialized()
		return initializeResult{
			ProtocolVersion: protocolVersion,
			ServerName:      "muxagent app-server",
			ServerVersion:   s.serverVersion,
			Capabilities: serverCapabilitiesDTO{
				Methods: []string{
					methodInitialize,
					methodServiceStatus,
					methodServiceShutdown,
					methodWorkspaceList,
					methodWorkspaceAdd,
					methodWorkspaceRemove,
					methodWorkspaceUpdate,
					methodWorkspaceGet,
					methodTaskList,
					methodTaskGet,
					methodTaskInputRequest,
					methodTaskStart,
					methodTaskStartFollowUp,
					methodTaskSubmitInput,
					methodTaskRetryNode,
					methodTaskContinueBlocked,
					methodArtifactList,
					methodConfigCatalog,
					methodConfigGet,
					methodConfigClone,
					methodConfigRename,
					methodConfigDelete,
					methodConfigReset,
					methodConfigSetDefault,
					methodConfigValidate,
					methodConfigSave,
					methodConfigPromptGet,
					methodConfigPromptSave,
					methodRuntimeList,
				},
				Notifications: []string{methodNotification},
			},
		}, nil, stopModeContinue, nil

	case methodServiceStatus:
		return serviceStatusResult{
			StateDir:         s.stateDir,
			ServerVersion:    s.serverVersion,
			ProtocolVersion:  protocolVersion,
			WorkspaceCount:   s.registry.Count(),
			RuntimeCount:     s.runtimeCount,
			ConnectedClients: s.connectedClientCount(),
		}, nil, stopModeContinue, nil

	case methodServiceShutdown:
		return serviceShutdownResult{Accepted: true}, nil, stopModeDrainAndExit, nil

	case methodWorkspaceList:
		return workspaceListResult{Workspaces: s.workspaceDTOs(s.registry.List())}, nil, stopModeContinue, nil

	case methodWorkspaceAdd:
		params, err := decodeParams[workspaceAddParams](req.Params)
		if err != nil {
			return nil, nil, stopModeContinue, &rpcError{Code: errorCodeInvalidParams, Message: err.Error()}
		}
		record, created, err := s.registry.Add(params.Path, params.DisplayName)
		if err != nil {
			return nil, nil, stopModeContinue, mapWorkspaceError(err)
		}
		dto := s.workspaceDTO(record)
		kind := notificationWorkspaceUpdated
		if created {
			kind = notificationWorkspaceAdded
		}
		return workspaceAddResult{Workspace: dto}, []notification{
			s.newNotification(kind, dto.WorkspaceID, workspaceNotificationPayload{Workspace: dto}),
		}, stopModeContinue, nil

	case methodWorkspaceUpdate:
		params, err := decodeParams[workspaceUpdateParams](req.Params)
		if err != nil {
			return nil, nil, stopModeContinue, &rpcError{Code: errorCodeInvalidParams, Message: err.Error()}
		}
		record, err := s.registry.Update(strings.TrimSpace(params.WorkspaceID), params.DisplayName)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil, nil, stopModeContinue, workspaceMissingRPCError(params.WorkspaceID)
			}
			return nil, nil, stopModeContinue, mapWorkspaceError(err)
		}
		dto := s.workspaceDTO(record)
		return workspaceUpdateResult{Workspace: dto}, []notification{
			s.newNotification(notificationWorkspaceUpdated, dto.WorkspaceID, workspaceNotificationPayload{Workspace: dto}),
		}, stopModeContinue, nil

	case methodWorkspaceRemove:
		params, err := decodeParams[workspaceRemoveParams](req.Params)
		if err != nil {
			return nil, nil, stopModeContinue, &rpcError{Code: errorCodeInvalidParams, Message: err.Error()}
		}
		workspaceID := strings.TrimSpace(params.WorkspaceID)
		removed, err := s.registry.Remove(workspaceID)
		if err != nil {
			return nil, nil, stopModeContinue, mapWorkspaceError(err)
		}
		if !removed {
			return nil, nil, stopModeContinue, workspaceMissingRPCError(params.WorkspaceID)
		}
		if err := s.runtimes.remove(workspaceID); err != nil {
			return nil, nil, stopModeContinue, &rpcError{Code: errorCodeInternalError, Message: err.Error()}
		}
		return workspaceRemoveResult{Removed: true}, []notification{
			s.newNotification(notificationWorkspaceRemoved, workspaceID, workspaceRemovedPayload{Removed: true}),
		}, stopModeContinue, nil

	case methodWorkspaceGet:
		params, err := decodeParams[workspaceGetParams](req.Params)
		if err != nil {
			return nil, nil, stopModeContinue, &rpcError{Code: errorCodeInvalidParams, Message: err.Error()}
		}
		record, ok := s.registry.Get(strings.TrimSpace(params.WorkspaceID))
		if !ok {
			return nil, nil, stopModeContinue, workspaceMissingRPCError(params.WorkspaceID)
		}
		return workspaceGetResult{Workspace: s.workspaceDTO(record)}, nil, stopModeContinue, nil

	case methodTaskList:
		params, err := decodeParams[taskListParams](req.Params)
		if err != nil {
			return nil, nil, stopModeContinue, &rpcError{Code: errorCodeInvalidParams, Message: err.Error()}
		}
		workspace, rpcErr := s.requireWorkspace(params.WorkspaceID)
		if rpcErr != nil {
			return nil, nil, stopModeContinue, rpcErr
		}
		model, rpcErr := s.openWorkspaceReadModel(workspace)
		if rpcErr != nil {
			return nil, nil, stopModeContinue, rpcErr
		}
		defer func() { _ = model.Close() }()
		views, err := model.ListTaskViews(ctx)
		if err != nil {
			return nil, nil, stopModeContinue, runtimeLookupRPCError(err)
		}
		tasks := make([]taskViewDTO, 0, len(views))
		for _, view := range views {
			tasks = append(tasks, taskViewToDTO(view))
		}
		return taskListResult{Tasks: tasks}, nil, stopModeContinue, nil

	case methodTaskGet:
		params, err := decodeParams[taskGetParams](req.Params)
		if err != nil {
			return nil, nil, stopModeContinue, &rpcError{Code: errorCodeInvalidParams, Message: err.Error()}
		}
		workspace, rpcErr := s.requireWorkspace(params.WorkspaceID)
		if rpcErr != nil {
			return nil, nil, stopModeContinue, rpcErr
		}
		model, rpcErr := s.openWorkspaceReadModel(workspace)
		if rpcErr != nil {
			return nil, nil, stopModeContinue, rpcErr
		}
		defer func() { _ = model.Close() }()
		view, cfg, err := model.LoadTaskView(ctx, strings.TrimSpace(params.TaskID))
		if err != nil {
			return nil, nil, stopModeContinue, runtimeLookupRPCError(err)
		}
		var input *taskruntime.InputRequest
		if view.Status == taskdomain.TaskStatusAwaitingUser {
			if nodeRunID := latestAwaitingRunID(view); nodeRunID != "" {
				input, err = model.BuildInputRequest(ctx, params.TaskID, nodeRunID)
				if err != nil {
					return nil, nil, stopModeContinue, runtimeLookupRPCError(err)
				}
			}
		}
		result := taskGetResult{
			Task:         taskViewToDTO(view),
			InputRequest: inputRequestToDTO(input),
		}
		if cfg != nil {
			result.Config = &configViewDTO{
				Path:   view.Task.ConfigPath,
				Config: cfg,
			}
		}
		return result, nil, stopModeContinue, nil

	case methodTaskInputRequest:
		params, err := decodeParams[taskInputRequestParams](req.Params)
		if err != nil {
			return nil, nil, stopModeContinue, &rpcError{Code: errorCodeInvalidParams, Message: err.Error()}
		}
		workspace, rpcErr := s.requireWorkspace(params.WorkspaceID)
		if rpcErr != nil {
			return nil, nil, stopModeContinue, rpcErr
		}
		model, rpcErr := s.openWorkspaceReadModel(workspace)
		if rpcErr != nil {
			return nil, nil, stopModeContinue, rpcErr
		}
		defer func() { _ = model.Close() }()
		input, err := model.BuildInputRequest(ctx, strings.TrimSpace(params.TaskID), strings.TrimSpace(params.NodeRunID))
		if err != nil {
			return nil, nil, stopModeContinue, runtimeLookupRPCError(err)
		}
		return taskInputRequestResult{InputRequest: inputRequestToDTO(input)}, nil, stopModeContinue, nil

	case methodTaskStart:
		params, err := decodeParams[taskStartParams](req.Params)
		if err != nil {
			return nil, nil, stopModeContinue, &rpcError{Code: errorCodeInvalidParams, Message: err.Error()}
		}
		workspace, rpcErr := s.requireWorkspace(params.WorkspaceID)
		if rpcErr != nil {
			return nil, nil, stopModeContinue, rpcErr
		}
		if strings.TrimSpace(params.ConfigAlias) == "" {
			return nil, nil, stopModeContinue, &rpcError{Code: errorCodeInvalidParams, Message: "config_alias is required"}
		}
		if strings.TrimSpace(params.ConfigPath) == "" {
			return nil, nil, stopModeContinue, &rpcError{Code: errorCodeInvalidParams, Message: "config_path is required"}
		}
		cmd := taskruntime.RunCommand{
			Type:        taskruntime.CommandStartTask,
			Description: params.Description,
			ConfigAlias: params.ConfigAlias,
			ConfigPath:  params.ConfigPath,
			WorkDir:     workspace.Path,
			UseWorktree: params.UseWorktree,
		}
		s.enqueuePendingClientCommand(workspace.WorkspaceID, methodTaskStart, params.ClientCommandID, cmd)
		if err := s.runtimes.dispatch(workspace, cmd); err != nil {
			return nil, nil, stopModeContinue, &rpcError{Code: errorCodeInternalError, Message: err.Error()}
		}
		return commandAcceptedResult{Accepted: true, ClientCommandID: params.ClientCommandID}, nil, stopModeContinue, nil

	case methodTaskStartFollowUp:
		params, err := decodeParams[taskStartFollowUpParams](req.Params)
		if err != nil {
			return nil, nil, stopModeContinue, &rpcError{Code: errorCodeInvalidParams, Message: err.Error()}
		}
		workspace, rpcErr := s.requireWorkspace(params.WorkspaceID)
		if rpcErr != nil {
			return nil, nil, stopModeContinue, rpcErr
		}
		if strings.TrimSpace(params.ParentTaskID) == "" {
			return nil, nil, stopModeContinue, &rpcError{Code: errorCodeInvalidParams, Message: "parent_task_id is required"}
		}
		if xorBlank(params.ConfigAlias, params.ConfigPath) {
			return nil, nil, stopModeContinue, &rpcError{Code: errorCodeInvalidParams, Message: "config_alias and config_path must be provided together"}
		}
		cmd := taskruntime.RunCommand{
			Type:         taskruntime.CommandStartFollowUp,
			ParentTaskID: params.ParentTaskID,
			Description:  params.Description,
			ConfigAlias:  params.ConfigAlias,
			ConfigPath:   params.ConfigPath,
		}
		s.enqueuePendingClientCommand(workspace.WorkspaceID, methodTaskStartFollowUp, params.ClientCommandID, cmd)
		if err := s.runtimes.dispatch(workspace, cmd); err != nil {
			return nil, nil, stopModeContinue, &rpcError{Code: errorCodeInternalError, Message: err.Error()}
		}
		return commandAcceptedResult{Accepted: true, ClientCommandID: params.ClientCommandID}, nil, stopModeContinue, nil

	case methodTaskSubmitInput:
		params, err := decodeParams[taskSubmitInputParams](req.Params)
		if err != nil {
			return nil, nil, stopModeContinue, &rpcError{Code: errorCodeInvalidParams, Message: err.Error()}
		}
		workspace, rpcErr := s.requireWorkspace(params.WorkspaceID)
		if rpcErr != nil {
			return nil, nil, stopModeContinue, rpcErr
		}
		if strings.TrimSpace(params.TaskID) == "" {
			return nil, nil, stopModeContinue, &rpcError{Code: errorCodeInvalidParams, Message: "task_id is required"}
		}
		if strings.TrimSpace(params.NodeRunID) == "" {
			return nil, nil, stopModeContinue, &rpcError{Code: errorCodeInvalidParams, Message: "node_run_id is required"}
		}
		payload := params.Payload
		if payload == nil {
			payload = map[string]interface{}{}
		}
		cmd := taskruntime.RunCommand{
			Type:      taskruntime.CommandSubmitInput,
			TaskID:    params.TaskID,
			NodeRunID: params.NodeRunID,
			Payload:   payload,
		}
		s.enqueuePendingClientCommand(workspace.WorkspaceID, methodTaskSubmitInput, params.ClientCommandID, cmd)
		if err := s.runtimes.dispatch(workspace, cmd); err != nil {
			return nil, nil, stopModeContinue, &rpcError{Code: errorCodeInternalError, Message: err.Error()}
		}
		return commandAcceptedResult{Accepted: true, ClientCommandID: params.ClientCommandID}, nil, stopModeContinue, nil

	case methodTaskRetryNode:
		params, err := decodeParams[taskRetryNodeParams](req.Params)
		if err != nil {
			return nil, nil, stopModeContinue, &rpcError{Code: errorCodeInvalidParams, Message: err.Error()}
		}
		workspace, rpcErr := s.requireWorkspace(params.WorkspaceID)
		if rpcErr != nil {
			return nil, nil, stopModeContinue, rpcErr
		}
		if strings.TrimSpace(params.TaskID) == "" {
			return nil, nil, stopModeContinue, &rpcError{Code: errorCodeInvalidParams, Message: "task_id is required"}
		}
		if strings.TrimSpace(params.NodeRunID) == "" {
			return nil, nil, stopModeContinue, &rpcError{Code: errorCodeInvalidParams, Message: "node_run_id is required"}
		}
		cmd := taskruntime.RunCommand{
			Type:      taskruntime.CommandRetryNode,
			TaskID:    params.TaskID,
			NodeRunID: params.NodeRunID,
			Force:     params.Force,
		}
		s.enqueuePendingClientCommand(workspace.WorkspaceID, methodTaskRetryNode, params.ClientCommandID, cmd)
		if err := s.runtimes.dispatch(workspace, cmd); err != nil {
			return nil, nil, stopModeContinue, &rpcError{Code: errorCodeInternalError, Message: err.Error()}
		}
		return commandAcceptedResult{Accepted: true, ClientCommandID: params.ClientCommandID}, nil, stopModeContinue, nil

	case methodTaskContinueBlocked:
		params, err := decodeParams[taskContinueBlockedParams](req.Params)
		if err != nil {
			return nil, nil, stopModeContinue, &rpcError{Code: errorCodeInvalidParams, Message: err.Error()}
		}
		workspace, rpcErr := s.requireWorkspace(params.WorkspaceID)
		if rpcErr != nil {
			return nil, nil, stopModeContinue, rpcErr
		}
		if strings.TrimSpace(params.TaskID) == "" {
			return nil, nil, stopModeContinue, &rpcError{Code: errorCodeInvalidParams, Message: "task_id is required"}
		}
		cmd := taskruntime.RunCommand{
			Type:   taskruntime.CommandContinueBlocked,
			TaskID: params.TaskID,
		}
		s.enqueuePendingClientCommand(workspace.WorkspaceID, methodTaskContinueBlocked, params.ClientCommandID, cmd)
		if err := s.runtimes.dispatch(workspace, cmd); err != nil {
			return nil, nil, stopModeContinue, &rpcError{Code: errorCodeInternalError, Message: err.Error()}
		}
		return commandAcceptedResult{Accepted: true, ClientCommandID: params.ClientCommandID}, nil, stopModeContinue, nil

	case methodArtifactList:
		params, err := decodeParams[artifactListParams](req.Params)
		if err != nil {
			return nil, nil, stopModeContinue, &rpcError{Code: errorCodeInvalidParams, Message: err.Error()}
		}
		workspace, rpcErr := s.requireWorkspace(params.WorkspaceID)
		if rpcErr != nil {
			return nil, nil, stopModeContinue, rpcErr
		}
		if strings.TrimSpace(params.TaskID) == "" {
			return nil, nil, stopModeContinue, &rpcError{Code: errorCodeInvalidParams, Message: "task_id is required"}
		}
		model, rpcErr := s.openWorkspaceReadModel(workspace)
		if rpcErr != nil {
			return nil, nil, stopModeContinue, rpcErr
		}
		defer func() { _ = model.Close() }()
		artifacts, err := loadTaskArtifactRefs(ctx, model, strings.TrimSpace(params.TaskID))
		if err != nil {
			return nil, nil, stopModeContinue, runtimeLookupRPCError(err)
		}
		return artifactListResult{Artifacts: artifacts}, nil, stopModeContinue, nil

	case methodConfigCatalog:
		catalog, err := s.loadCatalog()
		if err != nil {
			return nil, nil, stopModeContinue, &rpcError{Code: errorCodeInternalError, Message: err.Error()}
		}
		reg, err := s.loadRegistry()
		if err != nil {
			return nil, nil, stopModeContinue, &rpcError{Code: errorCodeInternalError, Message: err.Error()}
		}
		runtimeCfg, err := s.loadRuntimeConfig()
		if err != nil {
			return nil, nil, stopModeContinue, &rpcError{Code: errorCodeInternalError, Message: err.Error()}
		}
		prefs := s.loadTaskLaunchPreferences()
		return buildConfigCatalogResult(catalog, reg, runtimeCfg, prefs.UseWorktree), nil, stopModeContinue, nil

	case methodConfigGet:
		params, err := decodeParams[configGetParams](req.Params)
		if err != nil {
			return nil, nil, stopModeContinue, &rpcError{Code: errorCodeInvalidParams, Message: err.Error()}
		}
		entry, rpcErr := s.loadConfigDetail(params.Alias)
		if rpcErr != nil {
			return nil, nil, stopModeContinue, rpcErr
		}
		return configGetResult{Entry: entry}, nil, stopModeContinue, nil

	case methodConfigClone:
		params, err := decodeParams[configCloneParams](req.Params)
		if err != nil {
			return nil, nil, stopModeContinue, &rpcError{Code: errorCodeInvalidParams, Message: err.Error()}
		}
		if strings.TrimSpace(params.NewAlias) == "" {
			return nil, nil, stopModeContinue, &rpcError{Code: errorCodeInvalidParams, Message: "new_alias is required"}
		}
		lookup, rpcErr := s.configLookup(params.SourceAlias)
		if rpcErr != nil {
			return nil, nil, stopModeContinue, rpcErr
		}
		if _, err := taskconfig.CloneConfig(params.NewAlias, lookup.entry.Path); err != nil {
			return nil, nil, stopModeContinue, &rpcError{Code: errorCodeInvalidParams, Message: err.Error()}
		}
		entry, rpcErr := s.loadConfigDetail(params.NewAlias)
		if rpcErr != nil {
			return nil, nil, stopModeContinue, rpcErr
		}
		return configCloneResult{Entry: entry}, nil, stopModeContinue, nil

	case methodConfigRename:
		params, err := decodeParams[configRenameParams](req.Params)
		if err != nil {
			return nil, nil, stopModeContinue, &rpcError{Code: errorCodeInvalidParams, Message: err.Error()}
		}
		if _, err := taskconfig.RenameConfigAlias(params.Alias, params.NewAlias); err != nil {
			return nil, nil, stopModeContinue, &rpcError{Code: errorCodeInvalidParams, Message: err.Error()}
		}
		entry, rpcErr := s.loadConfigDetail(params.NewAlias)
		if rpcErr != nil {
			return nil, nil, stopModeContinue, rpcErr
		}
		return configRenameResult{Entry: entry}, nil, stopModeContinue, nil

	case methodConfigDelete:
		params, err := decodeParams[configDeleteParams](req.Params)
		if err != nil {
			return nil, nil, stopModeContinue, &rpcError{Code: errorCodeInvalidParams, Message: err.Error()}
		}
		if _, err := taskconfig.DeleteConfig(params.Alias); err != nil {
			return nil, nil, stopModeContinue, &rpcError{Code: errorCodeInvalidParams, Message: err.Error()}
		}
		return configDeleteResult{Removed: true}, nil, stopModeContinue, nil

	case methodConfigReset:
		params, err := decodeParams[configResetParams](req.Params)
		if err != nil {
			return nil, nil, stopModeContinue, &rpcError{Code: errorCodeInvalidParams, Message: err.Error()}
		}
		lookup, rpcErr := s.configLookup(params.Alias)
		if rpcErr != nil {
			return nil, nil, stopModeContinue, rpcErr
		}
		if !lookup.entry.Builtin {
			return nil, nil, stopModeContinue, &rpcError{Code: errorCodeInvalidParams, Message: fmt.Sprintf("task config alias %q is not builtin", lookup.entry.Alias)}
		}
		if _, err := taskconfig.ResetBuiltinConfig(lookup.entry.Alias); err != nil {
			return nil, nil, stopModeContinue, &rpcError{Code: errorCodeInternalError, Message: err.Error()}
		}
		entry, rpcErr := s.loadConfigDetail(lookup.entry.Alias)
		if rpcErr != nil {
			return nil, nil, stopModeContinue, rpcErr
		}
		return configResetResult{Entry: entry}, nil, stopModeContinue, nil

	case methodConfigSetDefault:
		params, err := decodeParams[configSetDefaultParams](req.Params)
		if err != nil {
			return nil, nil, stopModeContinue, &rpcError{Code: errorCodeInvalidParams, Message: err.Error()}
		}
		if _, err := taskconfig.SetDefaultConfig(params.Alias); err != nil {
			return nil, nil, stopModeContinue, &rpcError{Code: errorCodeInvalidParams, Message: err.Error()}
		}
		entry, rpcErr := s.loadConfigDetail(params.Alias)
		if rpcErr != nil {
			return nil, nil, stopModeContinue, rpcErr
		}
		return configSetDefaultResult{Entry: entry}, nil, stopModeContinue, nil

	case methodConfigValidate:
		params, err := decodeParams[configValidateParams](req.Params)
		if err != nil {
			return nil, nil, stopModeContinue, &rpcError{Code: errorCodeInvalidParams, Message: err.Error()}
		}
		runtimeCfg, err := s.loadRuntimeConfig()
		if err != nil {
			return nil, nil, stopModeContinue, &rpcError{Code: errorCodeInternalError, Message: err.Error()}
		}
		normalized, validationErr := validateConfigDraft(runtimeCfg, params.Config)
		result := configValidateResult{}
		if normalized != nil {
			result.Config = normalized
			result.RuntimeID = normalized.Runtime
			result.RuntimeName = runtimeDisplayName(normalized.Runtime)
			result.RuntimeConfigured = runtimeConfigured(runtimeCfg, normalized.Runtime)
		}
		if validationErr != nil {
			result.Valid = false
			result.Error = validationErr.Error()
			return result, nil, stopModeContinue, nil
		}
		result.Valid = true
		return result, nil, stopModeContinue, nil

	case methodConfigSave:
		params, err := decodeParams[configSaveParams](req.Params)
		if err != nil {
			return nil, nil, stopModeContinue, &rpcError{Code: errorCodeInvalidParams, Message: err.Error()}
		}
		if strings.TrimSpace(params.ExpectedRevision) == "" {
			return nil, nil, stopModeContinue, &rpcError{Code: errorCodeInvalidParams, Message: "expected_revision is required"}
		}
		lookup, rpcErr := s.configLookup(params.Alias)
		if rpcErr != nil {
			return nil, nil, stopModeContinue, rpcErr
		}
		currentRevision, err := configRevision(lookup.entry.Path)
		if err != nil {
			return nil, nil, stopModeContinue, &rpcError{Code: errorCodeInternalError, Message: err.Error()}
		}
		if currentRevision != strings.TrimSpace(params.ExpectedRevision) {
			return nil, nil, stopModeContinue, configConflictRPCError(lookup.entry.Alias, currentRevision)
		}
		runtimeCfg, err := s.loadRuntimeConfig()
		if err != nil {
			return nil, nil, stopModeContinue, &rpcError{Code: errorCodeInternalError, Message: err.Error()}
		}
		_, err = validateConfigDraft(runtimeCfg, params.Config)
		if err != nil {
			return nil, nil, stopModeContinue, &rpcError{Code: errorCodeInvalidParams, Message: err.Error()}
		}
		payload, err := yaml.Marshal(params.Config)
		if err != nil {
			return nil, nil, stopModeContinue, &rpcError{Code: errorCodeInternalError, Message: err.Error()}
		}
		if err := os.WriteFile(lookup.entry.Path, payload, 0o644); err != nil {
			return nil, nil, stopModeContinue, &rpcError{Code: errorCodeInternalError, Message: err.Error()}
		}
		entry, rpcErr := s.loadConfigDetail(lookup.entry.Alias)
		if rpcErr != nil {
			return nil, nil, stopModeContinue, rpcErr
		}
		return configSaveResult{Entry: entry}, nil, stopModeContinue, nil

	case methodConfigPromptGet:
		params, err := decodeParams[configPromptGetParams](req.Params)
		if err != nil {
			return nil, nil, stopModeContinue, &rpcError{Code: errorCodeInvalidParams, Message: err.Error()}
		}
		prompt, rpcErr := s.loadConfigPrompt(params.Alias, params.NodeName)
		if rpcErr != nil {
			return nil, nil, stopModeContinue, rpcErr
		}
		return configPromptGetResult{Prompt: prompt}, nil, stopModeContinue, nil

	case methodConfigPromptSave:
		params, err := decodeParams[configPromptSaveParams](req.Params)
		if err != nil {
			return nil, nil, stopModeContinue, &rpcError{Code: errorCodeInvalidParams, Message: err.Error()}
		}
		lookup, rpcErr := s.configPromptLookup(params.Alias, params.NodeName)
		if rpcErr != nil {
			return nil, nil, stopModeContinue, rpcErr
		}
		expectedRevision := strings.TrimSpace(params.ExpectedRevision)
		currentRevision, err := promptRevision(lookup.resolvedPath)
		switch {
		case err == nil:
			if currentRevision != expectedRevision {
				return nil, nil, stopModeContinue, configPromptConflictRPCError(lookup.lookup.entry.Alias, lookup.nodeName, currentRevision)
			}
		case errors.Is(err, os.ErrNotExist):
			if expectedRevision != "" {
				return nil, nil, stopModeContinue, configPromptConflictRPCError(lookup.lookup.entry.Alias, lookup.nodeName, "")
			}
		default:
			return nil, nil, stopModeContinue, &rpcError{Code: errorCodeInternalError, Message: err.Error()}
		}
		if strings.TrimSpace(params.Content) == "" {
			return nil, nil, stopModeContinue, &rpcError{Code: errorCodeInvalidParams, Message: "content is required"}
		}
		if err := os.MkdirAll(filepath.Dir(lookup.resolvedPath), 0o755); err != nil {
			return nil, nil, stopModeContinue, &rpcError{Code: errorCodeInternalError, Message: err.Error()}
		}
		if err := os.WriteFile(lookup.resolvedPath, []byte(params.Content), 0o644); err != nil {
			return nil, nil, stopModeContinue, &rpcError{Code: errorCodeInternalError, Message: err.Error()}
		}
		prompt, rpcErr := s.loadConfigPrompt(lookup.lookup.entry.Alias, lookup.nodeName)
		if rpcErr != nil {
			return nil, nil, stopModeContinue, rpcErr
		}
		return configPromptSaveResult{Prompt: prompt}, nil, stopModeContinue, nil

	case methodRuntimeList:
		runtimeCfg, err := s.loadRuntimeConfig()
		if err != nil {
			return nil, nil, stopModeContinue, &rpcError{Code: errorCodeInternalError, Message: err.Error()}
		}
		return runtimeListResult{Runtimes: runtimeListDTOs(runtimeCfg)}, nil, stopModeContinue, nil

	default:
		return nil, nil, stopModeContinue, &rpcError{Code: errorCodeMethodNotFound, Message: fmt.Sprintf("method %q not found", req.Method)}
	}
}

func (s *Server) workspaceDTOs(records []workspaceRecord) []workspaceSummaryDTO {
	items := make([]workspaceSummaryDTO, 0, len(records))
	for _, record := range records {
		items = append(items, s.workspaceDTO(record))
	}
	return items
}

func (s *Server) workspaceDTO(record workspaceRecord) workspaceSummaryDTO {
	reachable := false
	if info, err := os.Stat(record.Path); err == nil && info.IsDir() {
		reachable = true
	}
	worktreeAvailable := false
	if reachable && s.worktreeAvailable(record.Path) {
		worktreeAvailable = true
	}
	dto := workspaceSummaryDTO{
		WorkspaceID:       record.WorkspaceID,
		Path:              record.Path,
		DisplayName:       normalizedDisplayName(record.DisplayName, record.Path),
		Source:            record.Source,
		Reachable:         reachable,
		WorktreeAvailable: worktreeAvailable,
		AddedAt:           record.AddedAt.UTC(),
		TaskCounts:        taskCountsDTO{},
		Actor:             s.runtimes.snapshot(record.WorkspaceID),
	}
	if !record.LastOpenedAt.IsZero() {
		at := record.LastOpenedAt.UTC()
		dto.LastOpenedAt = &at
	}
	return dto
}

func (s *Server) openWorkspaceReadModel(workspace workspaceRecord) (*taskReadModel, *rpcError) {
	model, err := openTaskReadModel(workspace.Path)
	if err != nil {
		return nil, mapWorkspaceError(err)
	}
	return model, nil
}

func (s *Server) requireWorkspace(workspaceID string) (workspaceRecord, *rpcError) {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return workspaceRecord{}, &rpcError{Code: errorCodeInvalidParams, Message: "workspace_id is required"}
	}
	record, ok := s.registry.Get(workspaceID)
	if !ok {
		return workspaceRecord{}, workspaceMissingRPCError(workspaceID)
	}
	return record, nil
}

func (s *Server) handleRuntimeEvent(workspaceID string, event taskruntime.RunEvent) {
	payload := taskNotificationPayload{
		ClientCommandID: s.claimPendingClientCommandID(workspaceID, event),
		Event:           runEventToDTO(event),
	}
	s.emitNotification(s.newNotification(string(event.Type), workspaceID, payload))
}

func (s *Server) newNotification(kind, workspaceID string, payload any) notification {
	return notification{
		JSONRPC: jsonRPCVersion,
		Method:  methodNotification,
		Params: notificationParams{
			EventID:     "evt_" + uuid.NewString(),
			At:          s.now().UTC(),
			Kind:        kind,
			WorkspaceID: workspaceID,
			Payload:     payload,
		},
	}
}

func (s *Server) emitNotification(n notification) {
	s.mu.Lock()
	sink := s.notificationSink
	s.mu.Unlock()
	if sink != nil {
		sink(n)
	}
}

func (s *Server) setNotificationSink(sink func(notification)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.notificationSink = sink
}

func (s *Server) markInitialized() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.initialized = true
}

func (s *Server) isInitialized() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.initialized
}

func (s *Server) setConnectedClients(count int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.connectedClients = count
}

func (s *Server) connectedClientCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.connectedClients
}

func enqueueJSON(outgoing chan<- any, payload any) error {
	outgoing <- payload
	return nil
}

func workspaceMissingRPCError(workspaceID string) *rpcError {
	return &rpcError{
		Code:    errorCodeWorkspaceMissing,
		Message: "workspace not found",
		Data: map[string]any{
			"workspace_id": workspaceID,
		},
	}
}

func mapWorkspaceError(err error) *rpcError {
	if err == nil {
		return nil
	}
	msg := err.Error()
	if strings.Contains(msg, "workspace unavailable") || strings.Contains(msg, "path unavailable") {
		return &rpcError{Code: errorCodeWorkspaceUnreachable, Message: msg}
	}
	return &rpcError{Code: errorCodeInvalidParams, Message: msg}
}

func decodeParams[T any](raw json.RawMessage) (T, error) {
	var params T
	if len(raw) == 0 {
		raw = []byte("{}")
	}
	if err := json.Unmarshal(raw, &params); err != nil {
		return params, err
	}
	return params, nil
}

func requestIDOrNull(id json.RawMessage) json.RawMessage {
	if len(id) == 0 {
		return json.RawMessage("null")
	}
	return id
}

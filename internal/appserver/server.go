package appserver

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"sync"

	appconfig "github.com/LaLanMo/muxagent-cli/internal/config"
	"github.com/LaLanMo/muxagent-cli/internal/taskconfig"
	"github.com/LaLanMo/muxagent-cli/internal/taskdomain"
	"github.com/LaLanMo/muxagent-cli/internal/taskruntime"
	"github.com/LaLanMo/muxagent-cli/internal/taskstore"
)

type RuntimeService interface {
	Run(ctx context.Context) error
	Events() <-chan taskruntime.RunEvent
	Dispatch(cmd taskruntime.RunCommand)
	ListTaskViews(ctx context.Context, workDir string) ([]taskdomain.TaskView, error)
	LoadTaskView(ctx context.Context, taskID string) (taskdomain.TaskView, *taskconfig.Config, error)
	BuildInputRequest(ctx context.Context, taskID, nodeRunID string) (*taskruntime.InputRequest, error)
	PrepareShutdown(ctx context.Context) error
	Close() error
}

type Options struct {
	Service                   RuntimeService
	WorkDir                   string
	ServerVersion             string
	LoadCatalog               func() (*taskconfig.Catalog, error)
	LoadRegistry              func() (taskconfig.Registry, error)
	LoadTaskLaunchPreferences func() appconfig.TaskLaunchPreferences
	WorktreeAvailable         func(string) bool
}

type Server struct {
	service       RuntimeService
	workDir       string
	serverVersion string

	loadCatalog               func() (*taskconfig.Catalog, error)
	loadRegistry              func() (taskconfig.Registry, error)
	loadTaskLaunchPreferences func() appconfig.TaskLaunchPreferences
	worktreeAvailable         func(string) bool

	mu              sync.Mutex
	initialized     bool
	pendingCommands []pendingClientCommand
}

type pendingClientCommand struct {
	method          string
	clientCommandID string
	taskID          string
	nodeRunID       string
}

type stopMode int

const (
	stopModeContinue stopMode = iota
	stopModeDrainAndExit
)

func New(opts Options) (*Server, error) {
	if opts.Service == nil {
		return nil, errors.New("app-server requires a runtime service")
	}
	workDir := strings.TrimSpace(opts.WorkDir)
	if workDir == "" {
		return nil, errors.New("app-server requires a workdir")
	}
	if !filepath.IsAbs(workDir) {
		return nil, errors.New("app-server requires an absolute workdir")
	}
	workDir = taskstore.NormalizeWorkDir(workDir)
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
		opts.WorktreeAvailable = func(string) bool { return false }
	}
	return &Server{
		service:                   opts.Service,
		workDir:                   workDir,
		serverVersion:             opts.ServerVersion,
		loadCatalog:               opts.LoadCatalog,
		loadRegistry:              opts.LoadRegistry,
		loadTaskLaunchPreferences: opts.LoadTaskLaunchPreferences,
		worktreeAvailable:         opts.WorktreeAvailable,
	}, nil
}

func (s *Server) Serve(ctx context.Context, stdin io.Reader, stdout io.Writer) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	defer s.service.Close()

	writer := newFrameWriter(stdout)
	reader := newFrameReader(stdin)

	runErrCh := make(chan error, 1)
	go func() {
		runErrCh <- s.service.Run(ctx)
	}()

	eventErrCh := make(chan error, 1)
	go s.forwardEvents(ctx, writer, eventErrCh)

	for {
		frame, err := reader.readFrame()
		if err != nil {
			cancel()
			if errors.Is(err, io.EOF) {
				return s.waitForRun(ctx, runErrCh, nil)
			}
			return s.waitForRun(ctx, runErrCh, err)
		}

		select {
		case err := <-eventErrCh:
			cancel()
			return s.waitForRun(ctx, runErrCh, err)
		default:
		}

		stopAfter, err := s.handleFrame(ctx, frame, writer)
		if err != nil {
			cancel()
			return s.waitForRun(ctx, runErrCh, err)
		}
		if stopAfter == stopModeDrainAndExit {
			return s.waitForShutdown(cancel, runErrCh, eventErrCh)
		}
	}
}

func (s *Server) forwardEvents(ctx context.Context, writer *frameWriter, errCh chan<- error) {
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-s.service.Events():
			if !ok {
				return
			}
			params := eventNotificationParams{Event: runEventToDTO(event)}
			if clientCommandID := s.claimPendingClientCommandID(event); clientCommandID != "" {
				params.ClientCommandID = clientCommandID
			}
			if err := writer.writeJSON(notification{
				JSONRPC: jsonRPCVersion,
				Method:  string(event.Type),
				Params:  params,
			}); err != nil {
				select {
				case errCh <- err:
				default:
				}
				return
			}
		}
	}
}

func (s *Server) handleFrame(ctx context.Context, frame []byte, writer *frameWriter) (stopMode, error) {
	var msg incomingMessage
	if err := json.Unmarshal(frame, &msg); err != nil {
		return stopModeContinue, writer.writeJSON(response{
			JSONRPC: jsonRPCVersion,
			ID:      json.RawMessage("null"),
			Error:   &rpcError{Code: errorCodeParseError, Message: "parse error"},
		})
	}
	if msg.JSONRPC != jsonRPCVersion {
		return stopModeContinue, writer.writeJSON(response{
			JSONRPC: jsonRPCVersion,
			ID:      requestIDOrNull(msg.ID),
			Error:   &rpcError{Code: errorCodeInvalidRequest, Message: "jsonrpc must be 2.0"},
		})
	}
	if msg.isNotification() {
		return stopModeContinue, nil
	}
	if !msg.isRequest() {
		return stopModeContinue, writer.writeJSON(response{
			JSONRPC: jsonRPCVersion,
			ID:      requestIDOrNull(msg.ID),
			Error:   &rpcError{Code: errorCodeInvalidRequest, Message: "request must include method and id"},
		})
	}
	if !s.isInitialized() && msg.Method != methodInitialize {
		return stopModeContinue, writer.writeJSON(response{
			JSONRPC: jsonRPCVersion,
			ID:      requestIDOrNull(msg.ID),
			Error:   &rpcError{Code: errorCodeNotInitialized, Message: "server not initialized"},
		})
	}

	result, stopAfter, rpcErr := s.handleRequest(ctx, request{
		JSONRPC: msg.JSONRPC,
		ID:      msg.ID,
		Method:  msg.Method,
		Params:  msg.Params,
	})
	resp := response{
		JSONRPC: jsonRPCVersion,
		ID:      msg.ID,
		Result:  result,
		Error:   rpcErr,
	}
	if err := writer.writeJSON(resp); err != nil {
		return stopModeContinue, err
	}
	return stopAfter, nil
}

func (s *Server) handleRequest(ctx context.Context, req request) (any, stopMode, *rpcError) {
	switch req.Method {
	case methodInitialize:
		params, err := decodeParams[initializeParams](req.Params)
		if err != nil {
			return nil, stopModeContinue, &rpcError{Code: errorCodeInvalidParams, Message: err.Error()}
		}
		if params.ProtocolVersion != protocolVersion {
			return nil, stopModeContinue, &rpcError{Code: errorCodeInvalidParams, Message: fmt.Sprintf("unsupported protocol version %d", params.ProtocolVersion)}
		}
		s.markInitialized()
		return initializeResult{
			ProtocolVersion: protocolVersion,
			ServerName:      "muxagent app-server",
			ServerVersion:   s.serverVersion,
			WorkDir:         s.workDir,
			Capabilities: serverCapabilitiesDto{
				Methods: []string{
					methodInitialize,
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
					methodServiceStatus,
					methodServiceShutdown,
				},
				Notifications: []string{
					string(taskruntime.EventTaskCreated),
					string(taskruntime.EventNodeStarted),
					string(taskruntime.EventNodeProgress),
					string(taskruntime.EventNodeCompleted),
					string(taskruntime.EventNodeFailed),
					string(taskruntime.EventInputRequested),
					string(taskruntime.EventTaskCompleted),
					string(taskruntime.EventTaskFailed),
					string(taskruntime.EventCommandError),
				},
			},
		}, stopModeContinue, nil
	case methodTaskList:
		views, err := s.service.ListTaskViews(ctx, s.workDir)
		if err != nil {
			return nil, stopModeContinue, &rpcError{Code: errorCodeInternalError, Message: err.Error()}
		}
		tasks := make([]taskViewDto, 0, len(views))
		for _, view := range views {
			tasks = append(tasks, taskViewToDTO(view))
		}
		return taskListResult{Tasks: tasks}, stopModeContinue, nil
	case methodTaskGet:
		params, err := decodeParams[taskGetParams](req.Params)
		if err != nil {
			return nil, stopModeContinue, &rpcError{Code: errorCodeInvalidParams, Message: err.Error()}
		}
		view, cfg, err := s.service.LoadTaskView(ctx, params.TaskID)
		if err != nil {
			return nil, stopModeContinue, runtimeLookupRPCError(err)
		}
		var input *taskruntime.InputRequest
		if view.Status == taskdomain.TaskStatusAwaitingUser {
			if nodeRunID := latestAwaitingRunID(view); nodeRunID != "" {
				input, err = s.service.BuildInputRequest(ctx, params.TaskID, nodeRunID)
				if err != nil {
					return nil, stopModeContinue, runtimeLookupRPCError(err)
				}
			}
		}
		result := taskGetResult{
			Task:         taskViewToDTO(view),
			InputRequest: inputRequestToDTO(input),
		}
		if cfg != nil {
			result.Config = &configViewDto{
				Path:   view.Task.ConfigPath,
				Config: cfg,
			}
		}
		return result, stopModeContinue, nil
	case methodTaskInputRequest:
		params, err := decodeParams[taskInputRequestParams](req.Params)
		if err != nil {
			return nil, stopModeContinue, &rpcError{Code: errorCodeInvalidParams, Message: err.Error()}
		}
		input, err := s.service.BuildInputRequest(ctx, params.TaskID, params.NodeRunID)
		if err != nil {
			return nil, stopModeContinue, runtimeLookupRPCError(err)
		}
		return taskInputRequestResult{InputRequest: inputRequestToDTO(input)}, stopModeContinue, nil
	case methodTaskStart:
		params, err := decodeParams[taskStartParams](req.Params)
		if err != nil {
			return nil, stopModeContinue, &rpcError{Code: errorCodeInvalidParams, Message: err.Error()}
		}
		if strings.TrimSpace(params.ConfigAlias) == "" {
			return nil, stopModeContinue, &rpcError{Code: errorCodeInvalidParams, Message: "config_alias is required"}
		}
		if strings.TrimSpace(params.ConfigPath) == "" {
			return nil, stopModeContinue, &rpcError{Code: errorCodeInvalidParams, Message: "config_path is required"}
		}
		cmd := taskruntime.RunCommand{
			Type:        taskruntime.CommandStartTask,
			Description: params.Description,
			ConfigAlias: params.ConfigAlias,
			ConfigPath:  params.ConfigPath,
			WorkDir:     s.workDir,
			UseWorktree: params.UseWorktree,
		}
		s.enqueuePendingClientCommand(methodTaskStart, params.ClientCommandID, cmd)
		s.service.Dispatch(cmd)
		return commandAcceptedResult{Accepted: true, ClientCommandID: params.ClientCommandID}, stopModeContinue, nil
	case methodTaskStartFollowUp:
		params, err := decodeParams[taskStartFollowUpParams](req.Params)
		if err != nil {
			return nil, stopModeContinue, &rpcError{Code: errorCodeInvalidParams, Message: err.Error()}
		}
		if strings.TrimSpace(params.ParentTaskID) == "" {
			return nil, stopModeContinue, &rpcError{Code: errorCodeInvalidParams, Message: "parent_task_id is required"}
		}
		if xorBlank(params.ConfigAlias, params.ConfigPath) {
			return nil, stopModeContinue, &rpcError{Code: errorCodeInvalidParams, Message: "config_alias and config_path must be provided together"}
		}
		cmd := taskruntime.RunCommand{
			Type:         taskruntime.CommandStartFollowUp,
			ParentTaskID: params.ParentTaskID,
			Description:  params.Description,
			ConfigAlias:  params.ConfigAlias,
			ConfigPath:   params.ConfigPath,
		}
		s.enqueuePendingClientCommand(methodTaskStartFollowUp, params.ClientCommandID, cmd)
		s.service.Dispatch(cmd)
		return commandAcceptedResult{Accepted: true, ClientCommandID: params.ClientCommandID}, stopModeContinue, nil
	case methodTaskSubmitInput:
		params, err := decodeParams[taskSubmitInputParams](req.Params)
		if err != nil {
			return nil, stopModeContinue, &rpcError{Code: errorCodeInvalidParams, Message: err.Error()}
		}
		if strings.TrimSpace(params.TaskID) == "" {
			return nil, stopModeContinue, &rpcError{Code: errorCodeInvalidParams, Message: "task_id is required"}
		}
		if strings.TrimSpace(params.NodeRunID) == "" {
			return nil, stopModeContinue, &rpcError{Code: errorCodeInvalidParams, Message: "node_run_id is required"}
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
		s.enqueuePendingClientCommand(methodTaskSubmitInput, params.ClientCommandID, cmd)
		s.service.Dispatch(cmd)
		return commandAcceptedResult{Accepted: true, ClientCommandID: params.ClientCommandID}, stopModeContinue, nil
	case methodTaskRetryNode:
		params, err := decodeParams[taskRetryNodeParams](req.Params)
		if err != nil {
			return nil, stopModeContinue, &rpcError{Code: errorCodeInvalidParams, Message: err.Error()}
		}
		if strings.TrimSpace(params.TaskID) == "" {
			return nil, stopModeContinue, &rpcError{Code: errorCodeInvalidParams, Message: "task_id is required"}
		}
		if strings.TrimSpace(params.NodeRunID) == "" {
			return nil, stopModeContinue, &rpcError{Code: errorCodeInvalidParams, Message: "node_run_id is required"}
		}
		cmd := taskruntime.RunCommand{
			Type:      taskruntime.CommandRetryNode,
			TaskID:    params.TaskID,
			NodeRunID: params.NodeRunID,
			Force:     params.Force,
		}
		s.enqueuePendingClientCommand(methodTaskRetryNode, params.ClientCommandID, cmd)
		s.service.Dispatch(cmd)
		return commandAcceptedResult{Accepted: true, ClientCommandID: params.ClientCommandID}, stopModeContinue, nil
	case methodTaskContinueBlocked:
		params, err := decodeParams[taskContinueBlockedParams](req.Params)
		if err != nil {
			return nil, stopModeContinue, &rpcError{Code: errorCodeInvalidParams, Message: err.Error()}
		}
		if strings.TrimSpace(params.TaskID) == "" {
			return nil, stopModeContinue, &rpcError{Code: errorCodeInvalidParams, Message: "task_id is required"}
		}
		cmd := taskruntime.RunCommand{
			Type:   taskruntime.CommandContinueBlocked,
			TaskID: params.TaskID,
		}
		s.enqueuePendingClientCommand(methodTaskContinueBlocked, params.ClientCommandID, cmd)
		s.service.Dispatch(cmd)
		return commandAcceptedResult{Accepted: true, ClientCommandID: params.ClientCommandID}, stopModeContinue, nil
	case methodArtifactList:
		params, err := decodeParams[artifactListParams](req.Params)
		if err != nil {
			return nil, stopModeContinue, &rpcError{Code: errorCodeInvalidParams, Message: err.Error()}
		}
		if strings.TrimSpace(params.TaskID) == "" {
			return nil, stopModeContinue, &rpcError{Code: errorCodeInvalidParams, Message: "task_id is required"}
		}
		artifacts, err := s.loadTaskArtifactRefs(ctx, params.TaskID)
		if err != nil {
			return nil, stopModeContinue, runtimeLookupRPCError(err)
		}
		return artifactListResult{Artifacts: artifacts}, stopModeContinue, nil
	case methodConfigCatalog:
		catalog, err := s.loadCatalog()
		if err != nil {
			return nil, stopModeContinue, &rpcError{Code: errorCodeInternalError, Message: err.Error()}
		}
		reg, err := s.loadRegistry()
		if err != nil {
			return nil, stopModeContinue, &rpcError{Code: errorCodeInternalError, Message: err.Error()}
		}
		return buildConfigCatalogResult(catalog, reg), stopModeContinue, nil
	case methodServiceStatus:
		prefs := s.loadTaskLaunchPreferences()
		worktreeAvailable := s.worktreeAvailable(s.workDir)
		return serviceStatusResult{
			WorkDir:            s.workDir,
			ServerVersion:      s.serverVersion,
			ProtocolVersion:    protocolVersion,
			WorktreeAvailable:  worktreeAvailable,
			DefaultUseWorktree: worktreeAvailable && prefs.UseWorktree,
		}, stopModeContinue, nil
	case methodServiceShutdown:
		s.service.Dispatch(taskruntime.RunCommand{Type: taskruntime.CommandShutdown})
		return serviceShutdownResult{Accepted: true}, stopModeDrainAndExit, nil
	default:
		return nil, stopModeContinue, &rpcError{Code: errorCodeMethodNotFound, Message: fmt.Sprintf("method %q not found", req.Method)}
	}
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

func (s *Server) enqueuePendingClientCommand(method, clientCommandID string, cmd taskruntime.RunCommand) {
	clientCommandID = strings.TrimSpace(clientCommandID)
	if clientCommandID == "" {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.pendingCommands = append(s.pendingCommands, pendingClientCommand{
		method:          method,
		clientCommandID: clientCommandID,
		taskID:          strings.TrimSpace(cmd.TaskID),
		nodeRunID:       strings.TrimSpace(cmd.NodeRunID),
	})
}

func (s *Server) claimPendingClientCommandID(event taskruntime.RunEvent) string {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i, pending := range s.pendingCommands {
		if !pending.matches(event) {
			continue
		}
		clientCommandID := pending.clientCommandID
		s.pendingCommands = append(s.pendingCommands[:i], s.pendingCommands[i+1:]...)
		return clientCommandID
	}
	return ""
}

func (p pendingClientCommand) matches(event taskruntime.RunEvent) bool {
	if p.matchesDispatchCommandError(event) {
		return true
	}

	switch p.method {
	case methodTaskStart, methodTaskStartFollowUp:
		return event.Type == taskruntime.EventTaskCreated
	case methodTaskSubmitInput:
		if event.TaskID != p.taskID || event.NodeRunID != p.nodeRunID {
			return false
		}
		switch event.Type {
		case taskruntime.EventNodeStarted, taskruntime.EventNodeCompleted, taskruntime.EventInputRequested:
			return true
		default:
			return false
		}
	case methodTaskRetryNode:
		return event.TaskID == p.taskID && event.Type == taskruntime.EventNodeStarted
	case methodTaskContinueBlocked:
		if event.TaskID != p.taskID {
			return false
		}
		switch event.Type {
		case taskruntime.EventNodeStarted, taskruntime.EventNodeCompleted, taskruntime.EventInputRequested:
			return true
		default:
			return false
		}
	default:
		return false
	}
}

func (p pendingClientCommand) matchesDispatchCommandError(event taskruntime.RunEvent) bool {
	if !isDispatchCommandError(event) {
		return false
	}
	switch p.method {
	case methodTaskStart, methodTaskStartFollowUp:
		return strings.TrimSpace(event.TaskID) == ""
	default:
		return event.TaskID == p.taskID
	}
}

func isDispatchCommandError(event taskruntime.RunEvent) bool {
	return event.Type == taskruntime.EventCommandError &&
		strings.TrimSpace(event.NodeRunID) == "" &&
		strings.TrimSpace(event.NodeName) == "" &&
		event.TaskView == nil &&
		event.Progress == nil &&
		event.InputRequest == nil
}

func runtimeLookupRPCError(err error) *rpcError {
	if errors.Is(err, sql.ErrNoRows) ||
		errors.Is(err, taskruntime.ErrNodeRunTaskMismatch) ||
		errors.Is(err, taskruntime.ErrNodeRunNotAwaitingUser) {
		return &rpcError{Code: errorCodeInvalidParams, Message: err.Error()}
	}
	return &rpcError{Code: errorCodeInternalError, Message: err.Error()}
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

func latestAwaitingRunID(view taskdomain.TaskView) string {
	for i := len(view.NodeRuns) - 1; i >= 0; i-- {
		if view.NodeRuns[i].Status == taskdomain.NodeRunAwaitingUser {
			return view.NodeRuns[i].ID
		}
	}
	return ""
}

func xorBlank(values ...string) bool {
	blankCount := 0
	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			blankCount++
		}
	}
	return blankCount > 0 && blankCount < len(values)
}

func (s *Server) waitForRun(ctx context.Context, runErrCh <-chan error, fallback error) error {
	select {
	case err := <-runErrCh:
		if err != nil && !errors.Is(err, context.Canceled) {
			return err
		}
	case <-ctx.Done():
	}
	return fallback
}

func (s *Server) waitForShutdown(cancel context.CancelFunc, runErrCh <-chan error, eventErrCh <-chan error) error {
	select {
	case err := <-runErrCh:
		cancel()
		if err != nil && !errors.Is(err, context.Canceled) {
			return err
		}
		return nil
	case err := <-eventErrCh:
		cancel()
		return s.waitForRun(context.Background(), runErrCh, err)
	}
}

package taskruntime

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	appconfig "github.com/LaLanMo/muxagent-cli/internal/config"
	"github.com/LaLanMo/muxagent-cli/internal/taskconfig"
	"github.com/LaLanMo/muxagent-cli/internal/taskdomain"
	"github.com/LaLanMo/muxagent-cli/internal/taskengine"
	"github.com/LaLanMo/muxagent-cli/internal/taskexecutor"
	"github.com/LaLanMo/muxagent-cli/internal/taskruntime/instancelock"
	"github.com/LaLanMo/muxagent-cli/internal/taskstore"
	"github.com/google/uuid"
)

type Service struct {
	workDir        string
	configOverride string
	lock           *instancelock.Lock
	store          *taskstore.Store
	engine         *taskengine.Engine
	executor       taskexecutor.Executor
	bus            *LocalBus
	nodeWG         sync.WaitGroup

	mu             sync.Mutex
	rootCtx        context.Context
	taskCancels    map[string]context.CancelFunc
	taskCtxs       map[string]context.Context
	shutdownReason string
}

func NewService(workDir, configOverride string, executor taskexecutor.Executor) (*Service, error) {
	workDir = taskstore.NormalizeWorkDir(workDir)
	lock, err := instancelock.Acquire(workDir)
	if err != nil {
		return nil, err
	}
	store, err := taskstore.Open(workDir)
	if err != nil {
		_ = lock.Release()
		return nil, err
	}
	service := &Service{
		workDir:        workDir,
		configOverride: configOverride,
		lock:           lock,
		store:          store,
		engine:         taskengine.New(),
		executor:       executor,
		bus:            NewLocalBus(16, 64),
		taskCancels:    map[string]context.CancelFunc{},
		taskCtxs:       map[string]context.Context{},
	}
	if err := service.reconcileStaleRunning(context.Background()); err != nil {
		_ = store.Close()
		_ = lock.Release()
		return nil, err
	}
	return service, nil
}

func (s *Service) Close() error {
	s.mu.Lock()
	for _, cancel := range s.taskCancels {
		cancel()
	}
	s.taskCancels = map[string]context.CancelFunc{}
	s.taskCtxs = map[string]context.Context{}
	s.mu.Unlock()
	s.nodeWG.Wait()
	storeErr := s.store.Close()
	lockErr := s.lock.Release()
	if storeErr != nil {
		return storeErr
	}
	return lockErr
}

func (s *Service) PrepareShutdown(ctx context.Context) error {
	s.setShutdownReason(taskdomain.FailureReasonInterruptedByUser)
	s.cancelTasks()
	return s.failActiveRunningNodeRuns(ctx, taskdomain.FailureReasonInterruptedByUser)
}

func (s *Service) Events() <-chan RunEvent {
	return s.bus.Events
}

func (s *Service) Dispatch(cmd RunCommand) {
	s.bus.Commands <- cmd
}

func (s *Service) Run(ctx context.Context) error {
	s.rootCtx = ctx
	for {
		select {
		case <-ctx.Done():
			s.cancelTasks()
			return ctx.Err()
		case cmd := <-s.bus.Commands:
			if err := s.handleCommand(ctx, cmd); err != nil {
				if !shouldPublishCommandError(err) {
					continue
				}
				s.publish(RunEvent{
					Type:   EventCommandError,
					TaskID: cmd.TaskID,
					Error:  &RunError{Message: err.Error()},
				})
			}
			if cmd.Type == CommandShutdown {
				return nil
			}
		}
	}
}

func (s *Service) handleCommand(ctx context.Context, cmd RunCommand) error {
	switch cmd.Type {
	case CommandStartTask:
		return s.startTask(ctx, cmd.Description, firstNonEmpty(cmd.WorkDir, s.workDir), firstNonEmpty(cmd.ConfigPath, s.configOverride), cmd.Runtime)
	case CommandSubmitInput:
		return s.submitInput(ctx, cmd.TaskID, cmd.NodeRunID, cmd.Payload)
	case CommandRetryNode:
		return s.retryNode(ctx, cmd.TaskID, cmd.NodeRunID, cmd.Force)
	case CommandContinueBlocked:
		return s.continueBlockedStep(ctx, cmd.TaskID)
	case CommandShutdown:
		return s.PrepareShutdown(ctx)
	default:
		return fmt.Errorf("unsupported command %q", cmd.Type)
	}
}

func (s *Service) startTask(ctx context.Context, description, workDir, configOverride string, runtimeOverride appconfig.RuntimeID) error {
	workDir = taskstore.NormalizeWorkDir(workDir)
	taskID := uuid.NewString()
	now := time.Now().UTC()
	task := taskdomain.Task{
		ID:          taskID,
		Description: description,
		WorkDir:     workDir,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	materialized, err := taskconfig.MaterializeWithRuntime(workDir, taskID, configOverride, runtimeOverride)
	if err != nil {
		return err
	}
	if err := s.store.CreateTask(ctx, task); err != nil {
		return err
	}
	view := taskdomain.DeriveTaskView(task, materialized.Config, nil, nil)
	s.publish(RunEvent{Type: EventTaskCreated, TaskID: taskID, TaskView: &view})

	entry := materialized.Config.Topology.Entry
	nodeRun := taskdomain.NodeRun{
		ID:        uuid.NewString(),
		TaskID:    taskID,
		NodeName:  entry,
		Status:    initialStatus(materialized.Config.NodeDefinitions[entry]),
		StartedAt: now,
	}
	if err := s.store.SaveNodeRun(ctx, nodeRun); err != nil {
		return err
	}
	s.engine.RegisterEntryRun(taskID, nodeRun)

	taskCtx, cancel := context.WithCancel(s.rootCtx)
	s.mu.Lock()
	s.taskCancels[taskID] = cancel
	s.taskCtxs[taskID] = taskCtx
	s.mu.Unlock()

	return s.startNode(taskCtx, task, materialized.Config, nodeRun)
}

func (s *Service) submitInput(ctx context.Context, taskID, nodeRunID string, payload map[string]interface{}) error {
	task, err := s.store.GetTask(ctx, taskID)
	if err != nil {
		return err
	}
	cfg, err := taskconfig.Load(taskstore.ConfigPath(task.WorkDir, task.ID))
	if err != nil {
		return err
	}
	run, err := s.store.GetNodeRun(ctx, nodeRunID)
	if err != nil {
		return err
	}
	if run.TaskID != taskID {
		return fmt.Errorf("node run %q does not belong to task %q", nodeRunID, taskID)
	}
	if run.Status != taskdomain.NodeRunAwaitingUser {
		return fmt.Errorf("node run %q is not awaiting user input", nodeRunID)
	}

	def := cfg.NodeDefinitions[run.NodeName]
	now := time.Now().UTC()
	if def.Type == taskconfig.NodeTypeHuman {
		if err := taskconfig.ValidateValue(&def.ResultSchema, payload); err != nil {
			return err
		}
		runs, err := s.store.ListNodeRunsByTask(ctx, taskID)
		if err != nil {
			return err
		}
		result, err := materializeHumanNodeArtifact(task, run, runs, payload, now)
		if err != nil {
			return err
		}
		run.Result = result
		run.Status = taskdomain.NodeRunDone
		run.FailureReason = ""
		run.CompletedAt = &now
		if err := s.store.SaveNodeRun(ctx, run); err != nil {
			return err
		}
		return s.afterNodeCompleted(ctx, task, cfg, run)
	}

	if len(run.Clarifications) == 0 {
		return errors.New("clarification input submitted for a node without clarification request")
	}
	response, err := parseClarificationResponse(run.Clarifications[len(run.Clarifications)-1].Request, payload)
	if err != nil {
		return err
	}
	run.Clarifications[len(run.Clarifications)-1].Response = response
	run.Clarifications[len(run.Clarifications)-1].AnsweredAt = &now
	run.Status = taskdomain.NodeRunRunning
	run.FailureReason = ""
	if err := s.store.SaveNodeRun(ctx, run); err != nil {
		return err
	}
	view, err := s.refreshTaskView(context.Background(), task.ID)
	if err != nil {
		return err
	}
	s.publish(RunEvent{
		Type:      EventNodeStarted,
		TaskID:    task.ID,
		NodeRunID: run.ID,
		NodeName:  run.NodeName,
		TaskView:  &view,
	})

	taskCtx := s.lookupTaskContext(taskID)
	if taskCtx == nil {
		taskCtx = ctx
	}
	s.runAgentNodeAsync(taskCtx, task, cfg, run)
	return nil
}

func (s *Service) rebuildEngineState(taskID string, runs []taskdomain.NodeRun) {
	sort.Slice(runs, func(i, j int) bool {
		if runs[i].StartedAt.Equal(runs[j].StartedAt) {
			return runs[i].ID < runs[j].ID
		}
		return runs[i].StartedAt.Before(runs[j].StartedAt)
	})
	for _, run := range runs {
		if run.TriggeredBy == nil {
			s.engine.RegisterEntryRun(taskID, run)
		} else {
			s.engine.RegisterTriggeredRun(taskID, run, run.TriggeredBy.NodeRunID)
		}
	}
}

func (s *Service) publish(event RunEvent) {
	select {
	case s.bus.Events <- event:
	default:
		s.bus.Events <- event
	}
}

func (s *Service) cancelTasks() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, cancel := range s.taskCancels {
		cancel()
	}
}

func (s *Service) lookupTaskContext(taskID string) context.Context {
	s.mu.Lock()
	defer s.mu.Unlock()
	if taskCtx, ok := s.taskCtxs[taskID]; ok {
		return taskCtx
	}
	if s.rootCtx == nil {
		return context.Background()
	}
	return s.rootCtx
}

func (s *Service) setShutdownReason(reason string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.shutdownReason == "" {
		s.shutdownReason = reason
	}
}

func (s *Service) currentShutdownReason() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.shutdownReason
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

type reportedTaskFailureError struct {
	cause error
}

func (e reportedTaskFailureError) Error() string {
	return e.cause.Error()
}

func (e reportedTaskFailureError) Unwrap() error {
	return e.cause
}

func markTaskFailureReported(err error) error {
	if err == nil {
		return nil
	}
	return reportedTaskFailureError{cause: err}
}

func shouldPublishCommandError(err error) bool {
	if err == nil {
		return false
	}
	var reported reportedTaskFailureError
	return !errors.As(err, &reported)
}

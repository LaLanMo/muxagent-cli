package taskruntime

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/LaLanMo/muxagent-cli/internal/taskconfig"
	"github.com/LaLanMo/muxagent-cli/internal/taskdomain"
	"github.com/LaLanMo/muxagent-cli/internal/taskengine"
	"github.com/LaLanMo/muxagent-cli/internal/taskexecutor"
	"github.com/LaLanMo/muxagent-cli/internal/taskstore"
	"github.com/google/uuid"
)

type Service struct {
	workDir        string
	configOverride string
	store          *taskstore.Store
	engine         *taskengine.Engine
	executor       taskexecutor.Executor
	bus            *LocalBus

	mu          sync.Mutex
	rootCtx     context.Context
	taskCancels map[string]context.CancelFunc
	taskCtxs    map[string]context.Context
}

func NewService(workDir, configOverride string, executor taskexecutor.Executor) (*Service, error) {
	workDir = taskstore.NormalizeWorkDir(workDir)
	store, err := taskstore.Open(workDir)
	if err != nil {
		return nil, err
	}
	return &Service{
		workDir:        workDir,
		configOverride: configOverride,
		store:          store,
		engine:         taskengine.New(),
		executor:       executor,
		bus:            NewLocalBus(16, 64),
		taskCancels:    map[string]context.CancelFunc{},
		taskCtxs:       map[string]context.Context{},
	}, nil
}

func (s *Service) Close() error {
	s.mu.Lock()
	for _, cancel := range s.taskCancels {
		cancel()
	}
	s.taskCancels = map[string]context.CancelFunc{}
	s.taskCtxs = map[string]context.Context{}
	s.mu.Unlock()
	return s.store.Close()
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
			return ctx.Err()
		case cmd := <-s.bus.Commands:
			if err := s.handleCommand(ctx, cmd); err != nil {
				s.publish(RunEvent{
					Type:   EventTaskFailed,
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
		return s.startTask(ctx, cmd.Description, firstNonEmpty(cmd.WorkDir, s.workDir), firstNonEmpty(cmd.ConfigPath, s.configOverride))
	case CommandSubmitInput:
		return s.submitInput(ctx, cmd.TaskID, cmd.NodeRunID, cmd.Payload)
	case CommandShutdown:
		return s.Close()
	default:
		return fmt.Errorf("unsupported command %q", cmd.Type)
	}
}

func (s *Service) startTask(ctx context.Context, description, workDir, configOverride string) error {
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
	materialized, err := taskconfig.Materialize(workDir, taskID, configOverride)
	if err != nil {
		return err
	}
	if err := s.store.CreateTask(ctx, task); err != nil {
		return err
	}
	view := taskdomain.DeriveTaskView(task, materialized.Config, nil)
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
	if err := s.store.SaveNodeRun(ctx, run); err != nil {
		return err
	}

	taskCtx := s.lookupTaskContext(taskID)
	if taskCtx == nil {
		taskCtx = ctx
	}
	return s.executeAgentNode(taskCtx, task, cfg, run)
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

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

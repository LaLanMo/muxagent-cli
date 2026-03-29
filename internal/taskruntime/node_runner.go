package taskruntime

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/LaLanMo/muxagent-cli/internal/taskconfig"
	"github.com/LaLanMo/muxagent-cli/internal/taskdomain"
	"github.com/LaLanMo/muxagent-cli/internal/taskexecutor"
	"github.com/LaLanMo/muxagent-cli/internal/taskstore"
	"github.com/LaLanMo/muxagent-cli/internal/worktree"
	"github.com/google/uuid"
)

func (s *Service) runAgentNodeAsync(ctx context.Context, task taskdomain.Task, cfg *taskconfig.Config, run taskdomain.NodeRun) {
	if ctx == nil {
		ctx = context.Background()
	}
	s.nodeWG.Add(1)
	go func() {
		defer s.nodeWG.Done()
		if err := s.executeAgentNode(ctx, task, cfg, run); err != nil && shouldPublishCommandError(err) {
			s.publish(RunEvent{
				Type:      EventCommandError,
				TaskID:    task.ID,
				NodeRunID: run.ID,
				NodeName:  run.NodeName,
				Error:     &RunError{Message: err.Error()},
			})
		}
	}()
}

func (s *Service) startNode(ctx context.Context, task taskdomain.Task, cfg *taskconfig.Config, run taskdomain.NodeRun) error {
	view, err := s.refreshTaskView(context.Background(), task.ID)
	if err != nil {
		return err
	}
	switch cfg.NodeDefinitions[run.NodeName].Type {
	case taskconfig.NodeTypeTerminal:
		now := time.Now().UTC()
		run.Status = taskdomain.NodeRunDone
		run.Result = map[string]interface{}{}
		run.CompletedAt = &now
		if err := s.store.SaveNodeRun(context.Background(), run); err != nil {
			return err
		}
		return s.afterNodeCompleted(context.Background(), task, cfg, run)
	case taskconfig.NodeTypeHuman:
		s.publish(RunEvent{
			Type:         EventInputRequested,
			TaskID:       task.ID,
			NodeRunID:    run.ID,
			NodeName:     run.NodeName,
			TaskView:     &view,
			InputRequest: s.buildInputRequest(task, cfg, viewNodeRuns(view), run),
		})
		return nil
	default:
		s.publish(RunEvent{
			Type:      EventNodeStarted,
			TaskID:    task.ID,
			NodeRunID: run.ID,
			NodeName:  run.NodeName,
			TaskView:  &view,
		})
		s.runAgentNodeAsync(ctx, task, cfg, run)
		return nil
	}
}

func (s *Service) executeAgentNode(ctx context.Context, task taskdomain.Task, cfg *taskconfig.Config, run taskdomain.NodeRun) error {
	runs, err := s.store.ListNodeRunsByTask(context.Background(), task.ID)
	if err != nil {
		return err
	}
	artifactDir, err := runArtifactDir(task, runs, run)
	if err != nil {
		return err
	}
	prompt, err := buildPrompt(task, cfg, taskstore.ConfigPath(task.WorkDir, task.ID), runs, run, artifactDir)
	if err != nil {
		return err
	}
	executionDir := task.ExecutionWorkDir()
	if task.ExecutionDir != "" && task.ExecutionDir != task.WorkDir {
		executionDir, err = worktree.ResolveWorktreeCWD(task.ExecutionDir, ".")
		if err != nil {
			return err
		}
	}
	req := taskexecutor.Request{
		Task:                task,
		NodeRun:             run,
		NodeDefinition:      cfg.NodeDefinitions[run.NodeName],
		ClarificationConfig: cfg.Clarification,
		ConfigPath:          taskstore.ConfigPath(task.WorkDir, task.ID),
		SchemaPath:          taskstore.SchemaPath(task.WorkDir, task.ID, run.NodeName),
		WorkDir:             executionDir,
		ArtifactDir:         artifactDir,
		Runtime:             cfg.Runtime,
		Prompt:              prompt,
		ResultSchema:        cfg.NodeDefinitions[run.NodeName].ResultSchema,
	}
	inputPath, err := ensureAgentInputArtifact(task, run, runs, taskexecutor.AppendOutputContract(req))
	if err != nil {
		return err
	}
	var progressErr error
	result, err := s.executor.Execute(ctx, req, func(item taskexecutor.Progress) {
		if progressErr != nil {
			return
		}
		progressErr = s.handleNodeProgress(context.Background(), task, &run, item)
	})
	if progressErr != nil {
		return progressErr
	}
	if err != nil {
		return s.failRun(context.Background(), task, cfg, run, err)
	}
	if result.SessionID != "" {
		run.SessionID = result.SessionID
	}
	switch result.Kind {
	case taskexecutor.ResultKindClarification:
		def := cfg.NodeDefinitions[run.NodeName]
		if len(run.Clarifications) >= def.MaxClarificationRounds && def.MaxClarificationRounds > 0 {
			return s.failRun(context.Background(), task, cfg, run, fmt.Errorf("node %q exceeded max_clarification_rounds", run.NodeName))
		}
		now := time.Now().UTC()
		run.Status = taskdomain.NodeRunAwaitingUser
		run.FailureReason = ""
		run.Clarifications = append(run.Clarifications, taskdomain.ClarificationExchange{
			Request:     *result.Clarification,
			RequestedAt: now,
		})
		if _, err := writeClarificationInputArtifact(task, run, runs); err != nil {
			return err
		}
		if err := s.store.SaveNodeRun(context.Background(), run); err != nil {
			return err
		}
		view, err := s.refreshTaskView(context.Background(), task.ID)
		if err != nil {
			return err
		}
		s.publish(RunEvent{
			Type:         EventInputRequested,
			TaskID:       task.ID,
			NodeRunID:    run.ID,
			NodeName:     run.NodeName,
			TaskView:     &view,
			InputRequest: s.buildInputRequest(task, cfg, viewNodeRuns(view), run),
		})
		return nil
	case taskexecutor.ResultKindResult:
		resultSchema := cfg.NodeDefinitions[run.NodeName].ResultSchema
		if err := taskconfig.ValidateValue(&resultSchema, result.Result); err != nil {
			return s.failRun(context.Background(), task, cfg, run, err)
		}
		storedResult := cloneMap(result.Result)
		if len(run.Clarifications) > 0 {
			inputPath, err = writeClarificationInputArtifact(task, run, runs)
			if err != nil {
				return err
			}
		}
		storedResult = appendArtifactPaths(storedResult, inputPath)
		now := time.Now().UTC()
		run.Result = storedResult
		run.Status = taskdomain.NodeRunDone
		run.FailureReason = ""
		run.CompletedAt = &now
		if err := s.store.SaveNodeRun(context.Background(), run); err != nil {
			return err
		}
		return s.afterNodeCompleted(context.Background(), task, cfg, run)
	default:
		return s.failRun(context.Background(), task, cfg, run, fmt.Errorf("unsupported execution result kind %q", result.Kind))
	}
}

func (s *Service) handleNodeProgress(ctx context.Context, task taskdomain.Task, run *taskdomain.NodeRun, item taskexecutor.Progress) error {
	if item.SessionID != "" && item.SessionID != run.SessionID {
		run.SessionID = item.SessionID
		if err := s.store.SaveNodeRun(ctx, *run); err != nil {
			return err
		}
	}
	if item.Message == "" && item.SessionID == "" {
		return nil
	}
	s.publish(RunEvent{
		Type:      EventNodeProgress,
		TaskID:    task.ID,
		NodeRunID: run.ID,
		NodeName:  run.NodeName,
		Progress: &ProgressInfo{
			Message:   item.Message,
			SessionID: item.SessionID,
		},
	})
	return nil
}

func (s *Service) afterNodeCompleted(ctx context.Context, task taskdomain.Task, cfg *taskconfig.Config, run taskdomain.NodeRun) error {
	view, err := s.refreshTaskView(ctx, task.ID)
	if err != nil {
		return err
	}
	s.publish(RunEvent{
		Type:      EventNodeCompleted,
		TaskID:    task.ID,
		NodeRunID: run.ID,
		NodeName:  run.NodeName,
		TaskView:  &view,
	})

	runs, err := s.store.ListNodeRunsByTask(ctx, task.ID)
	if err != nil {
		return err
	}
	s.rebuildEngineState(task.ID, runs)
	resolution, err := s.engine.ResolveCompletion(cfg, task.ID, runs, run)
	if err != nil {
		return s.failRun(ctx, task, cfg, run, err)
	}

	sort.Slice(resolution.Transitions, func(i, j int) bool {
		if resolution.Transitions[i].To == resolution.Transitions[j].To {
			return resolution.Transitions[i].Reason < resolution.Transitions[j].Reason
		}
		return resolution.Transitions[i].To < resolution.Transitions[j].To
	})
	sort.Slice(resolution.Blocked, func(i, j int) bool {
		if resolution.Blocked[i].To == resolution.Blocked[j].To {
			return resolution.Blocked[i].Reason < resolution.Blocked[j].Reason
		}
		return resolution.Blocked[i].To < resolution.Blocked[j].To
	})

	nextRuns := make([]taskdomain.NodeRun, 0, len(resolution.Transitions))
	for _, next := range resolution.Transitions {
		now := time.Now().UTC()
		nextRun := taskdomain.NodeRun{
			ID:          uuid.NewString(),
			TaskID:      task.ID,
			NodeName:    next.To,
			Status:      initialStatus(cfg.NodeDefinitions[next.To]),
			TriggeredBy: &next.Trigger,
			StartedAt:   now,
		}
		if err := s.store.SaveNodeRun(ctx, nextRun); err != nil {
			return err
		}
		s.engine.RegisterTriggeredRun(task.ID, nextRun, next.Trigger.NodeRunID)
		nextRuns = append(nextRuns, nextRun)
	}
	if len(resolution.Blocked) > 0 {
		blockedView, err := s.refreshTaskView(ctx, task.ID)
		if err != nil {
			return err
		}
		if blockedView.Status == taskdomain.TaskStatusFailed && blockedView.CurrentIssue != nil && blockedView.CurrentIssue.Kind == taskdomain.TaskIssueBlockedStep {
			s.publish(RunEvent{
				Type:     EventTaskFailed,
				TaskID:   task.ID,
				NodeName: blockedView.CurrentIssue.NodeName,
				TaskView: &blockedView,
				Error:    &RunError{Message: blockedView.CurrentIssue.Reason},
			})
		}
	}
	for _, nextRun := range nextRuns {
		if err := s.startNode(s.lookupTaskContext(task.ID), task, cfg, nextRun); err != nil {
			return err
		}
	}
	if resolution.TaskDone {
		view, err := s.refreshTaskView(ctx, task.ID)
		if err != nil {
			return err
		}
		if view.Status == taskdomain.TaskStatusDone {
			s.publish(RunEvent{
				Type:      EventTaskCompleted,
				TaskID:    task.ID,
				NodeRunID: run.ID,
				NodeName:  run.NodeName,
				TaskView:  &view,
			})
		}
	}
	return nil
}

func (s *Service) failRun(ctx context.Context, task taskdomain.Task, cfg *taskconfig.Config, run taskdomain.NodeRun, runErr error) error {
	now := time.Now().UTC()
	run.Status = taskdomain.NodeRunFailed
	run.FailureReason = s.failureReasonForError(runErr)
	run.CompletedAt = &now
	if err := s.store.SaveNodeRun(ctx, run); err != nil {
		return err
	}
	view, err := s.refreshTaskView(ctx, task.ID)
	if err != nil {
		return err
	}
	eventType := EventNodeFailed
	if view.Status == taskdomain.TaskStatusFailed {
		eventType = EventTaskFailed
	}
	s.publish(RunEvent{
		Type:      eventType,
		TaskID:    task.ID,
		NodeRunID: run.ID,
		NodeName:  run.NodeName,
		TaskView:  &view,
		Error:     &RunError{Message: runErr.Error()},
	})
	return markTaskFailureReported(runErr)
}

func (s *Service) failureReasonForError(runErr error) string {
	if runErr == nil {
		return ""
	}
	if shutdownReason := s.currentShutdownReason(); shutdownReason != "" {
		if errors.Is(runErr, context.Canceled) {
			return shutdownReason
		}
		if strings.Contains(strings.ToLower(runErr.Error()), "context canceled") {
			return shutdownReason
		}
	}
	return runErr.Error()
}

func initialStatus(def taskconfig.NodeDefinition) taskdomain.NodeRunStatus {
	switch def.Type {
	case taskconfig.NodeTypeHuman:
		return taskdomain.NodeRunAwaitingUser
	case taskconfig.NodeTypeTerminal:
		return taskdomain.NodeRunDone
	default:
		return taskdomain.NodeRunRunning
	}
}

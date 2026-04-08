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
	"github.com/LaLanMo/muxagent-cli/internal/taskhistory"
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
	return s.startNodeInternal(ctx, task, cfg, run, true)
}

func (s *Service) startNodeInternal(ctx context.Context, task taskdomain.Task, cfg *taskconfig.Config, run taskdomain.NodeRun, emitEvents bool) error {
	view, err := s.refreshTaskView(context.Background(), task.ID)
	if err != nil {
		return err
	}
	runs := viewNodeRuns(view)
	if err := persistRunManifest(task, runs, run); err != nil {
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
		if err := persistRunManifest(task, runs, run); err != nil {
			return err
		}
		return s.afterNodeCompletedInternal(context.Background(), task, cfg, run, emitEvents)
	case taskconfig.NodeTypeHuman:
		if !emitEvents {
			return nil
		}
		inputRequest, err := s.buildInputRequest(context.Background(), task, cfg, viewNodeRuns(view), run)
		if err != nil {
			return err
		}
		s.publish(RunEvent{
			Type:         EventInputRequested,
			TaskID:       task.ID,
			NodeRunID:    run.ID,
			NodeName:     run.NodeName,
			TaskView:     &view,
			InputRequest: inputRequest,
		})
		return nil
	default:
		if emitEvents {
			s.publish(RunEvent{
				Type:      EventNodeStarted,
				TaskID:    task.ID,
				NodeRunID: run.ID,
				NodeName:  run.NodeName,
				TaskView:  &view,
			})
		}
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
	inherited, err := s.loadInheritedContext(context.Background(), task)
	if err != nil {
		return err
	}
	prompt, err := buildPromptWithInheritedContext(task, cfg, taskstore.ConfigPath(task.WorkDir, task.ID), runs, run, artifactDir, inherited)
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
	if _, err := ensureAgentInputArtifact(task, run, runs, taskexecutor.AppendOutputContract(req)); err != nil {
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
		if err := persistRunManifest(task, runs, run); err != nil {
			return err
		}
		view, err := s.refreshTaskView(context.Background(), task.ID)
		if err != nil {
			return err
		}
		inputRequest, err := s.buildInputRequest(context.Background(), task, cfg, viewNodeRuns(view), run)
		if err != nil {
			return err
		}
		s.publish(RunEvent{
			Type:         EventInputRequested,
			TaskID:       task.ID,
			NodeRunID:    run.ID,
			NodeName:     run.NodeName,
			TaskView:     &view,
			InputRequest: inputRequest,
		})
		return nil
	case taskexecutor.ResultKindResult:
		resultSchema := cfg.NodeDefinitions[run.NodeName].ResultSchema
		if err := taskconfig.ValidateValue(&resultSchema, result.Result); err != nil {
			return s.failRun(context.Background(), task, cfg, run, err)
		}
		storedResult := cloneMap(result.Result)
		if len(run.Clarifications) > 0 {
			if _, err := writeClarificationInputArtifact(task, run, runs); err != nil {
				return err
			}
		}
		now := time.Now().UTC()
		run.Result = storedResult
		run.Status = taskdomain.NodeRunDone
		run.FailureReason = ""
		run.CompletedAt = &now
		if err := s.store.SaveNodeRun(context.Background(), run); err != nil {
			return err
		}
		if err := persistRunManifest(task, runs, run); err != nil {
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
	if item.Message == "" && item.SessionID == "" && len(item.Events) == 0 {
		return nil
	}
	annotated, err := s.annotateProgressEvents(task, *run, item)
	if err != nil {
		return err
	}
	if err := taskhistory.Append(task.WorkDir, task.ID, run.ID, annotated, time.Now().UTC()); err != nil {
		return err
	}
	s.publish(RunEvent{
		Type:      EventNodeProgress,
		TaskID:    task.ID,
		NodeRunID: run.ID,
		NodeName:  run.NodeName,
		Progress: &ProgressInfo{
			Message:   annotated.Message,
			SessionID: annotated.SessionID,
			Events:    append([]taskexecutor.StreamEvent(nil), annotated.Events...),
		},
	})
	return nil
}

func (s *Service) annotateProgressEvents(task taskdomain.Task, run taskdomain.NodeRun, item taskexecutor.Progress) (taskexecutor.Progress, error) {
	if len(item.Events) == 0 {
		return item, nil
	}
	baseSeq, err := s.currentRunEventSeq(task, run.ID)
	if err != nil {
		return taskexecutor.Progress{}, err
	}
	now := time.Now().UTC()
	annotated := taskexecutor.Progress{
		Message:   item.Message,
		SessionID: firstNonEmpty(item.SessionID, run.SessionID),
		Events:    make([]taskexecutor.StreamEvent, 0, len(item.Events)),
	}
	for _, event := range item.Events {
		next := event
		if next.EventID == "" {
			next.EventID = "evt_" + uuid.NewString()
		}
		baseSeq++
		next.Seq = baseSeq
		if next.EmittedAt.IsZero() {
			next.EmittedAt = now
		}
		if next.SessionID == "" {
			next.SessionID = annotated.SessionID
		}
		if next.Provenance == "" {
			next.Provenance = taskexecutor.StreamEventProvenanceExecutorPersisted
		}
		annotated.Events = append(annotated.Events, next)
	}
	s.mu.Lock()
	s.runEventSeqs[run.ID] = baseSeq
	s.mu.Unlock()
	return annotated, nil
}

func (s *Service) currentRunEventSeq(task taskdomain.Task, nodeRunID string) (uint64, error) {
	s.mu.Lock()
	current, ok := s.runEventSeqs[nodeRunID]
	s.mu.Unlock()
	if ok {
		return current, nil
	}
	lastSeq, err := taskhistory.LastSeq(task.WorkDir, task.ID, nodeRunID)
	if err != nil {
		if taskhistory.IsMissing(err) {
			lastSeq = 0
		} else {
			return 0, err
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if current, ok := s.runEventSeqs[nodeRunID]; ok {
		return current, nil
	}
	s.runEventSeqs[nodeRunID] = lastSeq
	return lastSeq, nil
}

func (s *Service) afterNodeCompleted(ctx context.Context, task taskdomain.Task, cfg *taskconfig.Config, run taskdomain.NodeRun) error {
	return s.afterNodeCompletedInternal(ctx, task, cfg, run, true)
}

func (s *Service) afterNodeCompletedInternal(ctx context.Context, task taskdomain.Task, cfg *taskconfig.Config, run taskdomain.NodeRun, emitEvents bool) error {
	view, err := s.refreshTaskView(ctx, task.ID)
	if err != nil {
		return err
	}
	if emitEvents {
		s.publish(RunEvent{
			Type:      EventNodeCompleted,
			TaskID:    task.ID,
			NodeRunID: run.ID,
			NodeName:  run.NodeName,
			TaskView:  &view,
		})
	}

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
		if emitEvents && blockedView.Status == taskdomain.TaskStatusFailed && blockedView.CurrentIssue != nil && blockedView.CurrentIssue.Kind == taskdomain.TaskIssueBlockedStep {
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
		if err := s.startNodeInternal(s.lookupTaskContext(task.ID), task, cfg, nextRun, emitEvents); err != nil {
			return err
		}
	}
	if resolution.TaskDone {
		view, err := s.refreshTaskView(ctx, task.ID)
		if err != nil {
			return err
		}
		if emitEvents && view.Status == taskdomain.TaskStatusDone {
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
	runs, err := s.store.ListNodeRunsByTask(ctx, task.ID)
	if err != nil {
		return err
	}
	if err := persistRunManifest(task, runs, run); err != nil {
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

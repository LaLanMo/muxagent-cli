package taskruntime

import (
	"context"
	"fmt"
	"time"

	"github.com/LaLanMo/muxagent-cli/internal/taskconfig"
	"github.com/LaLanMo/muxagent-cli/internal/taskdomain"
	"github.com/LaLanMo/muxagent-cli/internal/taskengine"
	"github.com/LaLanMo/muxagent-cli/internal/taskstore"
	"github.com/google/uuid"
)

func (s *Service) retryNode(ctx context.Context, taskID, failedRunID string, force bool) error {
	task, err := s.store.GetTask(ctx, taskID)
	if err != nil {
		return err
	}
	cfg, err := taskconfig.Load(taskstore.ConfigPath(task.WorkDir, task.ID))
	if err != nil {
		return err
	}
	runs, err := s.store.ListNodeRunsByTask(ctx, taskID)
	if err != nil {
		return err
	}
	blockedSteps, err := taskengine.DeriveBlockedSteps(cfg, runs)
	if err != nil {
		return err
	}
	target := taskdomain.RecoveryTargetForTask(cfg, runs, blockedSteps)
	if target == nil {
		return fmt.Errorf("task %q has no retryable failed or blocked step", taskID)
	}
	return s.retryFailedRun(ctx, task, cfg, runs, target, failedRunID, force)
}

func (s *Service) retryFailedRun(ctx context.Context, task taskdomain.Task, cfg *taskconfig.Config, runs []taskdomain.NodeRun, target *taskdomain.RecoveryTarget, failedRunID string, force bool) error {
	if failedRunID == "" {
		return fmt.Errorf("missing failed node run id")
	}
	if target.Kind != taskdomain.RecoveryTargetFailedRun || target.Run == nil || target.Run.ID != failedRunID {
		return fmt.Errorf("node run %q is not the latest open failed agent node", failedRunID)
	}
	failedRun := *target.Run
	if cfg.NodeDefinitions[failedRun.NodeName].Type != taskconfig.NodeTypeAgent {
		return fmt.Errorf("node %q is not retryable", failedRun.NodeName)
	}
	if !target.RetryAllowed && !force {
		return fmt.Errorf("retry unavailable: %s (%d/%d)", target.Reason, target.NextIteration-1, target.MaxIterations)
	}

	now := time.Now().UTC()
	reason := taskdomain.TriggerReasonManualRetry
	if force {
		reason = taskdomain.TriggerReasonManualRetryForce
	}
	retryRun := taskdomain.NodeRun{
		ID:        uuid.NewString(),
		TaskID:    task.ID,
		NodeName:  failedRun.NodeName,
		Status:    initialStatus(cfg.NodeDefinitions[failedRun.NodeName]),
		StartedAt: now,
		TriggeredBy: &taskdomain.TriggeredBy{
			NodeRunID: failedRun.ID,
			Reason:    reason,
		},
	}
	if err := s.store.SaveNodeRun(ctx, retryRun); err != nil {
		return err
	}
	if !s.engine.HasRun(failedRun.ID) {
		s.rebuildEngineState(task.ID, runs)
	}
	s.engine.RegisterTriggeredRun(task.ID, retryRun, failedRun.ID)
	return s.startNode(s.lookupTaskContext(task.ID), task, cfg, retryRun)
}

func (s *Service) continueBlockedStep(ctx context.Context, taskID string) error {
	task, err := s.store.GetTask(ctx, taskID)
	if err != nil {
		return err
	}
	cfg, err := taskconfig.Load(taskstore.ConfigPath(task.WorkDir, task.ID))
	if err != nil {
		return err
	}
	runs, err := s.store.ListNodeRunsByTask(ctx, taskID)
	if err != nil {
		return err
	}
	blockedSteps, err := taskengine.DeriveBlockedSteps(cfg, runs)
	if err != nil {
		return err
	}
	target := taskdomain.RecoveryTargetForTask(cfg, runs, blockedSteps)
	if target == nil || target.Kind != taskdomain.RecoveryTargetBlockedStep || target.BlockedStep == nil {
		return fmt.Errorf("task %q has no blocked step to continue", taskID)
	}
	blockedStep := *target.BlockedStep
	if blockedStep.TriggeredBy == nil {
		return fmt.Errorf("blocked step %q is missing trigger metadata", blockedStep.NodeName)
	}

	now := time.Now().UTC()
	nextRun := taskdomain.NodeRun{
		ID:        uuid.NewString(),
		TaskID:    task.ID,
		NodeName:  blockedStep.NodeName,
		Status:    initialStatus(cfg.NodeDefinitions[blockedStep.NodeName]),
		StartedAt: now,
		TriggeredBy: &taskdomain.TriggeredBy{
			NodeRunID: blockedStep.TriggeredBy.NodeRunID,
			Reason:    taskdomain.TriggerReasonManualContinueForce,
		},
	}
	if err := s.store.SaveNodeRun(ctx, nextRun); err != nil {
		return err
	}
	if !s.engine.HasRun(blockedStep.TriggeredBy.NodeRunID) {
		s.rebuildEngineState(task.ID, runs)
	}
	s.engine.RegisterTriggeredRun(task.ID, nextRun, blockedStep.TriggeredBy.NodeRunID)
	return s.startNode(s.lookupTaskContext(task.ID), task, cfg, nextRun)
}

package taskruntime

import (
	"context"
	"fmt"
	"time"

	"github.com/LaLanMo/muxagent-cli/internal/taskconfig"
	"github.com/LaLanMo/muxagent-cli/internal/taskdomain"
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

	var failedRun *taskdomain.NodeRun
	for i := range runs {
		if runs[i].ID == failedRunID {
			failedRun = &runs[i]
			break
		}
	}
	if failedRun == nil {
		return fmt.Errorf("node run %q not found for task %q", failedRunID, taskID)
	}
	if failedRun.Status != taskdomain.NodeRunFailed {
		return fmt.Errorf("node run %q is not failed", failedRunID)
	}
	if cfg.NodeDefinitions[failedRun.NodeName].Type != taskconfig.NodeTypeAgent {
		return fmt.Errorf("node %q is not retryable", failedRun.NodeName)
	}

	retryability := taskdomain.RetryabilityForTask(cfg, runs)
	if retryability == nil || retryability.Run.ID != failedRunID {
		return fmt.Errorf("node run %q is not the latest open failed agent node", failedRunID)
	}
	if !retryability.RetryAllowed && !force {
		return fmt.Errorf("retry unavailable: %s (%d/%d)", retryability.Reason, retryability.NextIteration-1, retryability.MaxIterations)
	}

	now := time.Now().UTC()
	reason := taskdomain.TriggerReasonManualRetry
	if force {
		reason = taskdomain.TriggerReasonManualRetryForce
	}
	retryRun := taskdomain.NodeRun{
		ID:        uuid.NewString(),
		TaskID:    taskID,
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
		s.rebuildEngineState(taskID, runs)
	}
	s.engine.RegisterTriggeredRun(taskID, retryRun, failedRun.ID)
	return s.startNode(s.lookupTaskContext(taskID), task, cfg, retryRun)
}

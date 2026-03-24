package taskruntime

import (
	"context"
	"time"

	"github.com/LaLanMo/muxagent-cli/internal/taskdomain"
)

func (s *Service) reconcileStaleRunning(ctx context.Context) error {
	return s.failActiveRunningNodeRuns(ctx, taskdomain.FailureReasonOrphanedAfterRestart)
}

func (s *Service) failActiveRunningNodeRuns(ctx context.Context, reason string) error {
	runs, err := s.store.ListNodeRunsByStatus(ctx, taskdomain.NodeRunRunning)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	for _, run := range runs {
		run.Status = taskdomain.NodeRunFailed
		run.FailureReason = reason
		run.CompletedAt = &now
		if err := s.store.SaveNodeRun(ctx, run); err != nil {
			return err
		}
	}
	return nil
}

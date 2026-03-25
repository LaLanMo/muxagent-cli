package taskdomain

import "github.com/LaLanMo/muxagent-cli/internal/taskconfig"

const (
	TriggerReasonManualRetry         = "manual_retry"
	TriggerReasonManualRetryForce    = "manual_retry_force"
	TriggerReasonManualContinue      = "manual_continue"
	TriggerReasonManualContinueForce = "manual_continue_force"

	FailureReasonInterruptedByUser    = "interrupted_by_user"
	FailureReasonOrphanedAfterRestart = "orphaned_after_restart"
)

type RecoveryTargetKind string

const (
	RecoveryTargetFailedRun   RecoveryTargetKind = "failed_run"
	RecoveryTargetBlockedStep RecoveryTargetKind = "blocked_step"
)

type RecoveryTarget struct {
	Kind              RecoveryTargetKind
	Run               *NodeRun
	BlockedStep       *BlockedStep
	NodeName          string
	NextIteration     int
	MaxIterations     int
	RetryAllowed      bool
	ForceRetryAllowed bool
	Reason            string
}

func IsManualRetryReason(reason string) bool {
	return reason == TriggerReasonManualRetry || reason == TriggerReasonManualRetryForce
}

func IsManualContinueReason(reason string) bool {
	return reason == TriggerReasonManualContinue || reason == TriggerReasonManualContinueForce
}

func LatestBlockedStep(steps []BlockedStep) *BlockedStep {
	for i := len(steps) - 1; i >= 0; i-- {
		step := steps[i]
		return &step
	}
	return nil
}

func IterationCount(runs []NodeRun, nodeName string) int {
	count := 0
	for _, run := range runs {
		if run.NodeName == nodeName {
			count++
		}
	}
	return count
}

func MaxIterationsForNode(cfg *taskconfig.Config, nodeName string) int {
	limit := cfg.Topology.MaxIterations
	for _, node := range cfg.Topology.Nodes {
		if node.Name == nodeName && node.MaxIterations > 0 {
			return node.MaxIterations
		}
	}
	return limit
}

func IsOpenFailedRun(runs []NodeRun, run NodeRun) bool {
	if run.Status != NodeRunFailed {
		return false
	}
	for _, candidate := range runs {
		if candidate.TriggeredBy == nil {
			continue
		}
		if candidate.TriggeredBy.NodeRunID != run.ID {
			continue
		}
		if IsManualRetryReason(candidate.TriggeredBy.Reason) {
			return false
		}
	}
	return true
}

func HasOpenFailedRuns(runs []NodeRun) bool {
	for _, run := range runs {
		if IsOpenFailedRun(runs, run) {
			return true
		}
	}
	return false
}

func LatestOpenFailedRun(runs []NodeRun) *NodeRun {
	for i := len(runs) - 1; i >= 0; i-- {
		if !IsOpenFailedRun(runs, runs[i]) {
			continue
		}
		run := runs[i]
		return &run
	}
	return nil
}

func RecoveryTargetForTask(cfg *taskconfig.Config, runs []NodeRun, blockedSteps []BlockedStep) *RecoveryTarget {
	var latestFailed *NodeRun
	for i := len(runs) - 1; i >= 0; i-- {
		run := runs[i]
		if !IsOpenFailedRun(runs, run) {
			continue
		}
		if cfg.NodeDefinitions[run.NodeName].Type != taskconfig.NodeTypeAgent {
			break
		}
		candidate := run
		latestFailed = &candidate
		break
	}
	latestBlocked := LatestBlockedStep(blockedSteps)

	switch {
	case latestFailed == nil && latestBlocked == nil:
		return nil
	case latestFailed == nil:
		limit := MaxIterationsForNode(cfg, latestBlocked.NodeName)
		return &RecoveryTarget{
			Kind:              RecoveryTargetBlockedStep,
			BlockedStep:       latestBlocked,
			NodeName:          latestBlocked.NodeName,
			NextIteration:     latestBlocked.Iteration,
			MaxIterations:     limit,
			RetryAllowed:      false,
			ForceRetryAllowed: true,
			Reason:            latestBlocked.Reason,
		}
	case latestBlocked == nil:
		return failedRunRecoveryTarget(cfg, runs, *latestFailed)
	default:
		failedAt := latestFailed.StartedAt
		if latestFailed.CompletedAt != nil {
			failedAt = latestFailed.CompletedAt.UTC()
		}
		if !latestBlocked.CreatedAt.After(failedAt) {
			return failedRunRecoveryTarget(cfg, runs, *latestFailed)
		}
		limit := MaxIterationsForNode(cfg, latestBlocked.NodeName)
		return &RecoveryTarget{
			Kind:              RecoveryTargetBlockedStep,
			BlockedStep:       latestBlocked,
			NodeName:          latestBlocked.NodeName,
			NextIteration:     latestBlocked.Iteration,
			MaxIterations:     limit,
			RetryAllowed:      false,
			ForceRetryAllowed: true,
			Reason:            latestBlocked.Reason,
		}
	}
}

func CurrentIssueForTask(cfg *taskconfig.Config, runs []NodeRun, blockedSteps []BlockedStep) *TaskIssue {
	target := RecoveryTargetForTask(cfg, runs, blockedSteps)
	if target == nil {
		return nil
	}
	switch target.Kind {
	case RecoveryTargetBlockedStep:
		if target.BlockedStep == nil {
			return nil
		}
		return &TaskIssue{
			Kind:       TaskIssueBlockedStep,
			NodeName:   target.NodeName,
			Iteration:  target.NextIteration,
			Reason:     target.Reason,
			OccurredAt: target.BlockedStep.CreatedAt,
		}
	case RecoveryTargetFailedRun:
		if target.Run == nil {
			return nil
		}
		occurredAt := target.Run.StartedAt
		if target.Run.CompletedAt != nil {
			occurredAt = target.Run.CompletedAt.UTC()
		}
		return &TaskIssue{
			Kind:       TaskIssueFailedRun,
			NodeName:   target.NodeName,
			Iteration:  target.NextIteration - 1,
			Reason:     DisplayFailureReason(target.Run.FailureReason),
			OccurredAt: occurredAt,
		}
	default:
		return nil
	}
}

func failedRunRecoveryTarget(cfg *taskconfig.Config, runs []NodeRun, run NodeRun) *RecoveryTarget {
	limit := MaxIterationsForNode(cfg, run.NodeName)
	nextIteration := IterationCount(runs, run.NodeName) + 1
	info := &RecoveryTarget{
		Kind:              RecoveryTargetFailedRun,
		Run:               &run,
		NodeName:          run.NodeName,
		NextIteration:     nextIteration,
		MaxIterations:     limit,
		RetryAllowed:      nextIteration <= limit,
		ForceRetryAllowed: true,
	}
	if !info.RetryAllowed {
		info.Reason = "max_iterations reached"
	}
	return info
}

func DisplayFailureReason(reason string) string {
	switch reason {
	case "":
		return ""
	case FailureReasonInterruptedByUser:
		return "Interrupted by user."
	case FailureReasonOrphanedAfterRestart:
		return "Muxagent restarted while this step was running."
	default:
		return reason
	}
}

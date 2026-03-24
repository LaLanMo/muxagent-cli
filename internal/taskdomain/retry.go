package taskdomain

import "github.com/LaLanMo/muxagent-cli/internal/taskconfig"

const (
	TriggerReasonManualRetry      = "manual_retry"
	TriggerReasonManualRetryForce = "manual_retry_force"

	FailureReasonInterruptedByUser    = "interrupted_by_user"
	FailureReasonOrphanedAfterRestart = "orphaned_after_restart"
)

type Retryability struct {
	Run               NodeRun
	NextIteration     int
	MaxIterations     int
	RetryAllowed      bool
	ForceRetryAllowed bool
	Reason            string
}

func IsManualRetryReason(reason string) bool {
	return reason == TriggerReasonManualRetry || reason == TriggerReasonManualRetryForce
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

func RetryabilityForTask(cfg *taskconfig.Config, runs []NodeRun) *Retryability {
	for i := len(runs) - 1; i >= 0; i-- {
		run := runs[i]
		if !IsOpenFailedRun(runs, run) {
			continue
		}
		if cfg.NodeDefinitions[run.NodeName].Type != taskconfig.NodeTypeAgent {
			return nil
		}
		limit := MaxIterationsForNode(cfg, run.NodeName)
		nextIteration := IterationCount(runs, run.NodeName) + 1
		info := &Retryability{
			Run:               run,
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
	return nil
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

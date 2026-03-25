package taskdomain

import (
	"testing"
	"time"

	"github.com/LaLanMo/muxagent-cli/internal/taskconfig"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDeriveTaskStatusIgnoresRetriedHistoricalFailure(t *testing.T) {
	cfg := retryTestConfig(2)
	now := time.Now().UTC()
	runs := []NodeRun{
		{ID: "run-1", TaskID: "task-1", NodeName: "implement", Status: NodeRunFailed, StartedAt: now, CompletedAt: timePtr(now)},
		{
			ID:        "run-2",
			TaskID:    "task-1",
			NodeName:  "implement",
			Status:    NodeRunRunning,
			StartedAt: now.Add(time.Second),
			TriggeredBy: &TriggeredBy{
				NodeRunID: "run-1",
				Reason:    TriggerReasonManualRetry,
			},
		},
	}

	view := DeriveTaskView(Task{ID: "task-1"}, cfg, runs, nil)
	assert.Equal(t, TaskStatusRunning, view.Status)
	assert.Equal(t, "implement", view.CurrentNodeName)
}

func TestDeriveTaskStatusReturnsDoneAfterSuccessfulRetry(t *testing.T) {
	cfg := retryTerminalConfig(2)
	now := time.Now().UTC()
	runs := []NodeRun{
		{ID: "run-1", TaskID: "task-1", NodeName: "implement", Status: NodeRunFailed, StartedAt: now, CompletedAt: timePtr(now)},
		{
			ID:          "run-2",
			TaskID:      "task-1",
			NodeName:    "implement",
			Status:      NodeRunDone,
			StartedAt:   now.Add(time.Second),
			CompletedAt: timePtr(now.Add(2 * time.Second)),
			TriggeredBy: &TriggeredBy{
				NodeRunID: "run-1",
				Reason:    TriggerReasonManualRetryForce,
			},
		},
		{
			ID:          "run-3",
			TaskID:      "task-1",
			NodeName:    "done",
			Status:      NodeRunDone,
			StartedAt:   now.Add(3 * time.Second),
			CompletedAt: timePtr(now.Add(4 * time.Second)),
			TriggeredBy: &TriggeredBy{
				NodeRunID: "run-2",
				Reason:    "edge: implement -> done",
			},
		},
	}

	view := DeriveTaskView(Task{ID: "task-1"}, cfg, runs, nil)
	assert.Equal(t, TaskStatusDone, view.Status)
}

func TestRecoveryTargetForFailedRun(t *testing.T) {
	cfg := retryTestConfig(2)
	now := time.Now().UTC()
	runs := []NodeRun{
		{ID: "run-1", TaskID: "task-1", NodeName: "implement", Status: NodeRunFailed, StartedAt: now, CompletedAt: timePtr(now)},
	}

	info := RecoveryTargetForTask(cfg, runs, nil)
	require.NotNil(t, info)
	require.NotNil(t, info.Run)
	assert.Equal(t, RecoveryTargetFailedRun, info.Kind)
	assert.Equal(t, "run-1", info.Run.ID)
	assert.Equal(t, 2, info.NextIteration)
	assert.Equal(t, 2, info.MaxIterations)
	assert.True(t, info.RetryAllowed)
}

func TestRecoveryTargetForFailedRunForceOnlyAfterMaxIterations(t *testing.T) {
	cfg := retryTestConfig(1)
	now := time.Now().UTC()
	runs := []NodeRun{
		{ID: "run-1", TaskID: "task-1", NodeName: "implement", Status: NodeRunFailed, StartedAt: now, CompletedAt: timePtr(now)},
	}

	info := RecoveryTargetForTask(cfg, runs, nil)
	require.NotNil(t, info)
	assert.False(t, info.RetryAllowed)
	assert.True(t, info.ForceRetryAllowed)
	assert.Equal(t, "max_iterations reached", info.Reason)
}

func TestRecoveryTargetPrefersOpenBlockedStep(t *testing.T) {
	cfg := retryTestConfig(1)
	now := time.Now().UTC()
	runs := []NodeRun{
		{ID: "run-1", TaskID: "task-1", NodeName: "implement", Status: NodeRunDone, StartedAt: now, CompletedAt: timePtr(now)},
	}
	blocked := []BlockedStep{
		{
			NodeName:  "implement",
			Iteration: 2,
			Reason:    "node \"implement\" exceeded max_iterations",
			TriggeredBy: &TriggeredBy{
				NodeRunID: "run-1",
				Reason:    "edge: implement -> implement",
			},
			CreatedAt: now.Add(time.Second),
		},
	}

	info := RecoveryTargetForTask(cfg, runs, blocked)
	require.NotNil(t, info)
	assert.Equal(t, RecoveryTargetBlockedStep, info.Kind)
	require.NotNil(t, info.BlockedStep)
	assert.Equal(t, "implement", info.BlockedStep.NodeName)
	assert.False(t, info.RetryAllowed)
	assert.True(t, info.ForceRetryAllowed)
	assert.Equal(t, 2, info.NextIteration)
}

func TestDeriveTaskViewUsesOpenBlockedStepAsCurrentNode(t *testing.T) {
	cfg := retryTestConfig(1)
	now := time.Now().UTC()
	runs := []NodeRun{
		{ID: "run-1", TaskID: "task-1", NodeName: "implement", Status: NodeRunDone, StartedAt: now, CompletedAt: timePtr(now)},
	}
	blocked := []BlockedStep{
		{
			NodeName:  "implement",
			Iteration: 2,
			Reason:    "node \"implement\" exceeded max_iterations",
			TriggeredBy: &TriggeredBy{
				NodeRunID: "run-1",
				Reason:    "edge: implement -> implement",
			},
			CreatedAt: now.Add(time.Second),
		},
	}

	view := DeriveTaskView(Task{ID: "task-1"}, cfg, runs, blocked)
	assert.Equal(t, TaskStatusFailed, view.Status)
	assert.Equal(t, "implement", view.CurrentNodeName)
	require.NotNil(t, view.CurrentIssue)
	assert.Equal(t, TaskIssueBlockedStep, view.CurrentIssue.Kind)
	require.Len(t, view.BlockedSteps, 1)
	assert.Equal(t, "implement", view.BlockedSteps[0].NodeName)
}

func retryTestConfig(maxIterations int) *taskconfig.Config {
	deny := false
	return &taskconfig.Config{
		Version: 1,
		Topology: taskconfig.Topology{
			MaxIterations: maxIterations,
			Entry:         "implement",
			Nodes: []taskconfig.NodeRef{
				{Name: "implement", MaxIterations: maxIterations},
			},
		},
		NodeDefinitions: map[string]taskconfig.NodeDefinition{
			"implement": {
				Type: taskconfig.NodeTypeAgent,
				ResultSchema: taskconfig.JSONSchema{
					Type:                 "object",
					AdditionalProperties: &deny,
					Required:             []string{"file_paths"},
					Properties: map[string]*taskconfig.JSONSchema{
						"file_paths": {Type: "array", Items: &taskconfig.JSONSchema{Type: "string"}},
					},
				},
			},
		},
	}
}

func retryTerminalConfig(maxIterations int) *taskconfig.Config {
	cfg := retryTestConfig(maxIterations)
	cfg.Topology.Nodes = append(cfg.Topology.Nodes, taskconfig.NodeRef{Name: "done"})
	cfg.Topology.Edges = []taskconfig.Edge{{From: "implement", To: "done"}}
	cfg.NodeDefinitions["done"] = taskconfig.NodeDefinition{Type: taskconfig.NodeTypeTerminal}
	return cfg
}

func timePtr(ts time.Time) *time.Time {
	return &ts
}

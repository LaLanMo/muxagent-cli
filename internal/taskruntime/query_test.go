package taskruntime

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/LaLanMo/muxagent-cli/internal/taskdomain"
	"github.com/LaLanMo/muxagent-cli/internal/taskexecutor"
	"github.com/stretchr/testify/require"
)

func TestBuildInputRequestRejectsCompletedRun(t *testing.T) {
	service := newTestServiceWithConfig(t, singleAgentTerminalFixture(), &fakeExecutor{
		steps: map[string][]taskexecutor.Result{
			"implement": {{
				Kind:   taskexecutor.ResultKindResult,
				Result: resultWithArtifact("impl.md"),
			}},
		},
	})
	defer service.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = service.Run(ctx) }()

	service.Dispatch(startTaskCommand(t, service, "completed task"))
	completed := waitForEvent(t, service.Events(), EventTaskCompleted)
	view, _, err := service.LoadTaskView(context.Background(), completed.TaskID)
	require.NoError(t, err)
	var targetRunID string
	for _, run := range view.NodeRuns {
		if run.NodeName == "implement" {
			targetRunID = run.ID
			break
		}
	}
	require.NotEmpty(t, targetRunID)

	_, err = service.BuildInputRequest(context.Background(), completed.TaskID, targetRunID)
	require.ErrorIs(t, err, ErrNodeRunNotAwaitingUser)
}

func TestLoadInheritedInputArtifactsResolvesRelativeRunArtifacts(t *testing.T) {
	service := newTestService(t, &fakeExecutor{})
	defer service.Close()

	ctx := context.Background()
	now := time.Now().UTC()
	parentTask := taskdomain.Task{
		ID:          "parent-relative",
		Description: "parent task",
		WorkDir:     service.workDir,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	childTask := taskdomain.Task{
		ID:          "child-relative",
		Description: "child task",
		WorkDir:     service.workDir,
		CreatedAt:   now.Add(time.Second),
		UpdatedAt:   now.Add(time.Second),
	}
	require.NoError(t, service.store.CreateTask(ctx, parentTask))
	require.NoError(t, service.store.CreateTask(ctx, childTask))
	require.NoError(t, service.store.AttachFollowUpParent(ctx, parentTask.ID, childTask.ID, now.Add(2*time.Second)))

	completedAt := now.Add(3 * time.Second)
	parentRun := taskdomain.NodeRun{
		ID:          "parent-run",
		TaskID:      parentTask.ID,
		NodeName:    "draft_plan",
		Status:      taskdomain.NodeRunDone,
		Result:      resultWithArtifact("plan.md"),
		StartedAt:   now.Add(time.Minute),
		CompletedAt: &completedAt,
	}
	require.NoError(t, service.store.SaveNodeRun(ctx, parentRun))

	artifactPath := mustRunArtifactPathForRun(t, parentTask, []taskdomain.NodeRun{parentRun}, parentRun, "plan.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(artifactPath), 0o755))
	require.NoError(t, os.WriteFile(artifactPath, []byte("plan"), 0o644))

	artifacts, err := service.loadInheritedInputArtifacts(ctx, childTask)
	require.NoError(t, err)
	require.Equal(t, []string{artifactPath}, artifacts)
}

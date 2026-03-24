package taskruntime

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LaLanMo/muxagent-cli/internal/taskconfig"
	"github.com/LaLanMo/muxagent-cli/internal/taskdomain"
	"github.com/LaLanMo/muxagent-cli/internal/taskexecutor"
	"github.com/LaLanMo/muxagent-cli/internal/taskstore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestServiceHappyPathCompletesDefaultFlow(t *testing.T) {
	service := newTestService(t, &fakeExecutor{
		steps: map[string][]taskexecutor.Result{
			"upsert_plan": {{Kind: taskexecutor.ResultKindResult, Result: resultWithArtifact("plan.md")}},
			"review_plan": {{Kind: taskexecutor.ResultKindResult, Result: map[string]interface{}{"passed": true, "file_paths": []interface{}{"/tmp/review.md"}}}},
			"implement":   {{Kind: taskexecutor.ResultKindResult, Result: resultWithArtifact("impl.md")}},
			"verify":      {{Kind: taskexecutor.ResultKindResult, Result: map[string]interface{}{"passed": true, "file_paths": []interface{}{"/tmp/verify.md"}}}},
		},
	})
	defer service.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = service.Run(ctx) }()

	service.Dispatch(RunCommand{Type: CommandStartTask, Description: "Implement login", WorkDir: service.workDir})
	inputEvent := waitForEvent(t, service.Events(), EventInputRequested)
	require.NotNil(t, inputEvent.InputRequest)
	assert.Equal(t, InputKindHumanNode, inputEvent.InputRequest.Kind)

	service.Dispatch(RunCommand{
		Type:      CommandSubmitInput,
		TaskID:    inputEvent.TaskID,
		NodeRunID: inputEvent.NodeRunID,
		Payload:   map[string]interface{}{"approved": true},
	})
	completed := waitForEvent(t, service.Events(), EventTaskCompleted)
	require.NotNil(t, completed.TaskView)
	assert.Equal(t, taskdomain.TaskStatusDone, completed.TaskView.Status)

	views, err := service.ListTaskViews(context.Background(), service.workDir)
	require.NoError(t, err)
	require.Len(t, views, 1)
	assert.Equal(t, taskdomain.TaskStatusDone, views[0].Status)
}

func TestServiceClarificationUsesSameNodeRun(t *testing.T) {
	executor := &fakeExecutor{
		steps: map[string][]taskexecutor.Result{
			"upsert_plan": {
				{
					Kind: taskexecutor.ResultKindClarification,
					Clarification: &taskdomain.ClarificationRequest{
						Questions: []taskdomain.ClarificationQuestion{
							{
								Question:     "Need a choice",
								WhyItMatters: "Impacts plan",
								Options: []taskdomain.ClarificationOption{
									{Label: "A", Description: "Option A"},
									{Label: "B", Description: "Option B"},
								},
							},
						},
					},
				},
				{Kind: taskexecutor.ResultKindResult, Result: resultWithArtifact("plan.md")},
			},
			"review_plan": {{Kind: taskexecutor.ResultKindResult, Result: map[string]interface{}{"passed": true, "file_paths": []interface{}{"/tmp/review.md"}}}},
		},
	}
	service := newTestService(t, executor)
	defer service.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = service.Run(ctx) }()

	service.Dispatch(RunCommand{Type: CommandStartTask, Description: "Implement login", WorkDir: service.workDir})
	requested := waitForEvent(t, service.Events(), EventInputRequested)
	require.Equal(t, InputKindClarification, requested.InputRequest.Kind)

	beforeRuns, err := service.store.ListNodeRunsByTask(context.Background(), requested.TaskID)
	require.NoError(t, err)
	assert.Len(t, beforeRuns, 1)

	service.Dispatch(RunCommand{
		Type:      CommandSubmitInput,
		TaskID:    requested.TaskID,
		NodeRunID: requested.NodeRunID,
		Payload: map[string]interface{}{
			"answers": []interface{}{
				map[string]interface{}{"selected": "A"},
			},
		},
	})
	waitForEvent(t, service.Events(), EventInputRequested)

	afterRuns, err := service.store.ListNodeRunsByTask(context.Background(), requested.TaskID)
	require.NoError(t, err)
	count := 0
	for _, run := range afterRuns {
		if run.NodeName == "upsert_plan" {
			count++
			assert.Len(t, run.Clarifications, 1)
		}
	}
	assert.Equal(t, 1, count)

	upsertRequests := executor.requestsForNode("upsert_plan")
	require.Len(t, upsertRequests, 2)
	assert.Empty(t, upsertRequests[0].NodeRun.SessionID)
	assert.Equal(t, upsertRequests[0].NodeRun.ID+"-session", upsertRequests[1].NodeRun.SessionID)
	require.Len(t, upsertRequests[1].NodeRun.Clarifications, 1)
	require.NotNil(t, upsertRequests[1].NodeRun.Clarifications[0].Response)
	assert.Contains(t, upsertRequests[1].Prompt, "You asked the user for clarification in this same thread.")
	assert.Contains(t, upsertRequests[1].Prompt, "Options offered:")
	assert.Contains(t, upsertRequests[1].Prompt, "User selected:")
	assert.Contains(t, upsertRequests[1].Prompt, "Stay in the existing thread context")
}

func TestServiceReviewRejectLoopsBackToUpsertPlan(t *testing.T) {
	service := newTestService(t, &fakeExecutor{
		steps: map[string][]taskexecutor.Result{
			"upsert_plan": {
				{Kind: taskexecutor.ResultKindResult, Result: resultWithArtifact("plan-1.md")},
				{Kind: taskexecutor.ResultKindResult, Result: resultWithArtifact("plan-2.md")},
			},
			"review_plan": {
				{Kind: taskexecutor.ResultKindResult, Result: map[string]interface{}{"passed": false, "file_paths": []interface{}{"/tmp/review-1.md"}}},
				{Kind: taskexecutor.ResultKindResult, Result: map[string]interface{}{"passed": true, "file_paths": []interface{}{"/tmp/review-2.md"}}},
			},
		},
	})
	defer service.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = service.Run(ctx) }()

	service.Dispatch(RunCommand{Type: CommandStartTask, Description: "Implement login", WorkDir: service.workDir})
	inputRequested := waitForEvent(t, service.Events(), EventInputRequested)
	assert.Equal(t, "approve_plan", inputRequested.NodeName)

	runs, err := service.store.ListNodeRunsByTask(context.Background(), inputRequested.TaskID)
	require.NoError(t, err)
	upsertCount := 0
	reviewCount := 0
	for _, run := range runs {
		switch run.NodeName {
		case "upsert_plan":
			upsertCount++
		case "review_plan":
			reviewCount++
		}
	}
	assert.Equal(t, 2, upsertCount)
	assert.Equal(t, 2, reviewCount)
}

func TestServiceHumanNodeSubmissionCreatesAuditArtifactAndFeedsNextPrompt(t *testing.T) {
	executor := &fakeExecutor{
		steps: map[string][]taskexecutor.Result{
			"upsert_plan": {
				{Kind: taskexecutor.ResultKindResult, Result: resultWithArtifact("plan-1.md")},
				{Kind: taskexecutor.ResultKindResult, Result: resultWithArtifact("plan-2.md")},
			},
			"review_plan": {
				{Kind: taskexecutor.ResultKindResult, Result: map[string]interface{}{"passed": true, "file_paths": []interface{}{"/tmp/review-1.md"}}},
				{Kind: taskexecutor.ResultKindResult, Result: map[string]interface{}{"passed": true, "file_paths": []interface{}{"/tmp/review-2.md"}}},
			},
		},
	}
	service := newTestService(t, executor)
	defer service.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = service.Run(ctx) }()

	service.Dispatch(RunCommand{Type: CommandStartTask, Description: "reject once", WorkDir: service.workDir})
	firstApproval := waitForEvent(t, service.Events(), EventInputRequested)
	require.Equal(t, InputKindHumanNode, firstApproval.InputRequest.Kind)

	service.Dispatch(RunCommand{
		Type:      CommandSubmitInput,
		TaskID:    firstApproval.TaskID,
		NodeRunID: firstApproval.NodeRunID,
		Payload: map[string]interface{}{
			"approved": false,
			"feedback": "Need more detail",
		},
	})
	secondApproval := waitForEvent(t, service.Events(), EventInputRequested)
	require.Equal(t, "approve_plan", secondApproval.NodeName)

	runs, err := service.store.ListNodeRunsByTask(context.Background(), firstApproval.TaskID)
	require.NoError(t, err)

	var approvalRun taskdomain.NodeRun
	for _, run := range runs {
		if run.ID == firstApproval.NodeRunID {
			approvalRun = run
			break
		}
	}
	require.Equal(t, taskdomain.NodeRunDone, approvalRun.Status)
	artifactPaths := taskdomain.ArtifactPaths(approvalRun.Result)
	require.Len(t, artifactPaths, 1)
	assert.FileExists(t, artifactPaths[0])
	assert.Equal(t, false, approvalRun.Result["approved"])
	assert.Equal(t, "Need more detail", approvalRun.Result["feedback"])

	data, err := os.ReadFile(artifactPaths[0])
	require.NoError(t, err)
	var envelope map[string]interface{}
	require.NoError(t, json.Unmarshal(data, &envelope))
	assert.Equal(t, "human_node_result", envelope["kind"])
	assert.Equal(t, "approve_plan", envelope["node_name"])
	result, ok := envelope["result"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, false, result["approved"])
	assert.Equal(t, "Need more detail", result["feedback"])

	upsertPrompts := executor.requestsForNode("upsert_plan")
	require.Len(t, upsertPrompts, 2)
	assert.Contains(t, upsertPrompts[1].Prompt, artifactPaths[0])

	view, _, err := service.LoadTaskView(context.Background(), firstApproval.TaskID)
	require.NoError(t, err)
	assert.Contains(t, view.ArtifactPaths, artifactPaths[0])
}

func TestServicePublishesProgressAndPersistsSessionIDBeforeCompletion(t *testing.T) {
	blockRelease := make(chan struct{})
	blockStarted := make(chan struct{}, 1)
	service := newTestServiceWithConfig(t, singleAgentTerminalFixture(), &blockingExecutor{
		steps: map[string][]taskexecutor.Result{
			"implement": {{Kind: taskexecutor.ResultKindResult, Result: resultWithArtifact("impl.md")}},
		},
		progressByNode: map[string][]taskexecutor.Progress{
			"implement": {
				{SessionID: "thread-123"},
				{Message: "planning changes"},
				{Message: "editing files"},
				{Message: "running tests"},
				{Message: "writing artifact"},
				{Message: "wrapping up"},
			},
		},
		blockNode:    "implement",
		blockRelease: blockRelease,
		blockStarted: blockStarted,
	})
	defer service.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = service.Run(ctx) }()

	service.Dispatch(RunCommand{Type: CommandStartTask, Description: "stream progress", WorkDir: service.workDir})
	<-blockStarted

	progressEvent := waitForEventWhere(t, service.Events(), 5*time.Second, func(event RunEvent) bool {
		return event.Type == EventNodeProgress &&
			event.NodeName == "implement" &&
			event.Progress != nil &&
			event.Progress.SessionID == "thread-123"
	})
	runs, err := service.store.ListNodeRunsByTask(context.Background(), progressEvent.TaskID)
	require.NoError(t, err)
	require.Len(t, runs, 1)
	assert.Equal(t, taskdomain.NodeRunRunning, runs[0].Status)
	assert.Equal(t, "thread-123", runs[0].SessionID)

	close(blockRelease)
	completed := waitForEvent(t, service.Events(), EventTaskCompleted)
	require.NotNil(t, completed.TaskView)
	assert.Equal(t, taskdomain.TaskStatusDone, completed.TaskView.Status)
}

func TestServiceRejectsCrossTaskNodeRunInput(t *testing.T) {
	service := newTestService(t, &fakeExecutor{
		steps: map[string][]taskexecutor.Result{
			"upsert_plan": {
				{Kind: taskexecutor.ResultKindResult, Result: resultWithArtifact("plan-1.md")},
				{Kind: taskexecutor.ResultKindResult, Result: resultWithArtifact("plan-2.md")},
			},
			"review_plan": {
				{Kind: taskexecutor.ResultKindResult, Result: map[string]interface{}{"passed": true, "file_paths": []interface{}{"/tmp/review-1.md"}}},
				{Kind: taskexecutor.ResultKindResult, Result: map[string]interface{}{"passed": true, "file_paths": []interface{}{"/tmp/review-2.md"}}},
			},
			"implement": {
				{Kind: taskexecutor.ResultKindResult, Result: resultWithArtifact("impl-1.md")},
				{Kind: taskexecutor.ResultKindResult, Result: resultWithArtifact("impl-2.md")},
			},
			"verify": {
				{Kind: taskexecutor.ResultKindResult, Result: map[string]interface{}{"passed": true, "file_paths": []interface{}{"/tmp/verify-1.md"}}},
				{Kind: taskexecutor.ResultKindResult, Result: map[string]interface{}{"passed": true, "file_paths": []interface{}{"/tmp/verify-2.md"}}},
			},
		},
	})
	defer service.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = service.Run(ctx) }()

	service.Dispatch(RunCommand{Type: CommandStartTask, Description: "task one", WorkDir: service.workDir})
	first := waitForEvent(t, service.Events(), EventInputRequested)
	service.Dispatch(RunCommand{Type: CommandStartTask, Description: "task two", WorkDir: service.workDir})
	second := waitForEvent(t, service.Events(), EventInputRequested)

	_, err := service.BuildInputRequest(context.Background(), first.TaskID, second.NodeRunID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not belong")

	err = service.submitInput(context.Background(), first.TaskID, second.NodeRunID, map[string]interface{}{"approved": true})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not belong")

	run, err := service.store.GetNodeRun(context.Background(), second.NodeRunID)
	require.NoError(t, err)
	assert.Equal(t, taskdomain.NodeRunAwaitingUser, run.Status)
}

func TestServiceRejectsInvalidClarificationPayload(t *testing.T) {
	cases := []struct {
		name    string
		payload map[string]interface{}
		wantErr string
	}{
		{
			name:    "missing answers",
			payload: map[string]interface{}{},
			wantErr: "answers array",
		},
		{
			name: "single select receives array",
			payload: map[string]interface{}{
				"answers": []interface{}{
					map[string]interface{}{"selected": []interface{}{"A"}},
				},
			},
			wantErr: "single string value",
		},
		{
			name: "missing selected",
			payload: map[string]interface{}{
				"answers": []interface{}{
					map[string]interface{}{},
				},
			},
			wantErr: "must contain selected",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			service := newTestService(t, &fakeExecutor{
				steps: map[string][]taskexecutor.Result{
					"upsert_plan": {
						{
							Kind: taskexecutor.ResultKindClarification,
							Clarification: &taskdomain.ClarificationRequest{
								Questions: []taskdomain.ClarificationQuestion{
									{
										Question:     "Need a choice",
										WhyItMatters: "Impacts plan",
										Options: []taskdomain.ClarificationOption{
											{Label: "A", Description: "Option A"},
											{Label: "B", Description: "Option B"},
										},
									},
								},
							},
						},
					},
				},
			})
			defer service.Close()

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			go func() { _ = service.Run(ctx) }()

			service.Dispatch(RunCommand{Type: CommandStartTask, Description: "clarify", WorkDir: service.workDir})
			event := waitForEvent(t, service.Events(), EventInputRequested)
			require.Equal(t, InputKindClarification, event.InputRequest.Kind)

			err := service.submitInput(context.Background(), event.TaskID, event.NodeRunID, tc.payload)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)

			run, err := service.store.GetNodeRun(context.Background(), event.NodeRunID)
			require.NoError(t, err)
			assert.Equal(t, taskdomain.NodeRunAwaitingUser, run.Status)
			require.Len(t, run.Clarifications, 1)
			assert.Nil(t, run.Clarifications[0].Response)
		})
	}
}

func TestServiceJoinAllWaitsForAllBranchesBeforeJoining(t *testing.T) {
	service := newTestServiceWithConfig(t, joinAllRuntimeFixture(), &fakeExecutor{
		steps: map[string][]taskexecutor.Result{
			"start": {{Kind: taskexecutor.ResultKindResult, Result: resultWithArtifact("start.md")}},
			"left":  {{Kind: taskexecutor.ResultKindResult, Result: resultWithArtifact("left.md")}},
			"right": {{Kind: taskexecutor.ResultKindResult, Result: resultWithArtifact("right.md")}},
			"join":  {{Kind: taskexecutor.ResultKindResult, Result: resultWithArtifact("join.md")}},
		},
	})
	defer service.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = service.Run(ctx) }()

	service.Dispatch(RunCommand{Type: CommandStartTask, Description: "join", WorkDir: service.workDir})
	completed := waitForEvent(t, service.Events(), EventTaskCompleted)
	require.NotNil(t, completed.TaskView)
	assert.Equal(t, taskdomain.TaskStatusDone, completed.TaskView.Status)

	runs, err := service.store.ListNodeRunsByTask(context.Background(), completed.TaskID)
	require.NoError(t, err)
	assertNodeRunCounts(t, runs, map[string]int{
		"start": 1,
		"left":  1,
		"right": 1,
		"join":  1,
		"end":   1,
	})
}

func TestServiceDoesNotCompleteUntilAllActiveTerminalRunsFinish(t *testing.T) {
	blockRelease := make(chan struct{})
	blockStarted := make(chan struct{}, 1)
	service := newTestServiceWithConfig(t, parallelTerminalFixture(), &blockingExecutor{
		steps: map[string][]taskexecutor.Result{
			"start": {{Kind: taskexecutor.ResultKindResult, Result: resultWithArtifact("start.md")}},
			"left":  {{Kind: taskexecutor.ResultKindResult, Result: resultWithArtifact("left.md")}},
			"right": {{Kind: taskexecutor.ResultKindResult, Result: resultWithArtifact("right.md")}},
		},
		blockNode:    "right",
		blockRelease: blockRelease,
		blockStarted: blockStarted,
	})
	defer service.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = service.Run(ctx) }()

	service.Dispatch(RunCommand{Type: CommandStartTask, Description: "parallel terminals", WorkDir: service.workDir})
	<-blockStarted
	waitForEventWhere(t, service.Events(), 5*time.Second, func(event RunEvent) bool {
		return event.Type == EventNodeCompleted && event.NodeName == "left"
	})
	assertNoEventTypeWithin(t, service.Events(), EventTaskCompleted, 300*time.Millisecond)

	close(blockRelease)
	completed := waitForEvent(t, service.Events(), EventTaskCompleted)
	require.NotNil(t, completed.TaskView)
	assert.Equal(t, taskdomain.TaskStatusDone, completed.TaskView.Status)
}

func TestServiceRetryNodeCreatesNewRunAndRecoversFailedTask(t *testing.T) {
	cfg := singleAgentTerminalFixture()
	cfg.Topology.MaxIterations = 2
	cfg.Topology.Nodes[0].MaxIterations = 2
	service := newTestServiceWithConfig(t, cfg, &fakeExecutor{
		errors: map[string][]error{
			"implement": {fmt.Errorf("runtime unavailable")},
		},
		steps: map[string][]taskexecutor.Result{
			"implement": {{Kind: taskexecutor.ResultKindResult, Result: resultWithArtifact("impl.md")}},
		},
	})
	defer service.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = service.Run(ctx) }()

	service.Dispatch(RunCommand{Type: CommandStartTask, Description: "retry after failure", WorkDir: service.workDir})
	failed := waitForEvent(t, service.Events(), EventTaskFailed)
	require.NotNil(t, failed.TaskView)
	assert.Equal(t, taskdomain.TaskStatusFailed, failed.TaskView.Status)

	service.Dispatch(RunCommand{
		Type:      CommandRetryNode,
		TaskID:    failed.TaskID,
		NodeRunID: failed.NodeRunID,
	})
	completed := waitForEvent(t, service.Events(), EventTaskCompleted)
	require.NotNil(t, completed.TaskView)
	assert.Equal(t, taskdomain.TaskStatusDone, completed.TaskView.Status)

	runs, err := service.store.ListNodeRunsByTask(context.Background(), failed.TaskID)
	require.NoError(t, err)
	assertNodeRunCounts(t, runs, map[string]int{
		"implement": 2,
		"done":      1,
	})
	require.Equal(t, taskdomain.TriggerReasonManualRetry, runs[1].TriggeredBy.Reason)
	assert.Equal(t, failed.NodeRunID, runs[1].TriggeredBy.NodeRunID)
}

func TestServiceRetryNodeRequiresForceAfterMaxIterations(t *testing.T) {
	cfg := singleAgentTerminalFixture()
	cfg.Topology.MaxIterations = 1
	cfg.Topology.Nodes[0].MaxIterations = 1
	service := newTestServiceWithConfig(t, cfg, &fakeExecutor{
		errors: map[string][]error{
			"implement": {fmt.Errorf("bad environment")},
		},
		steps: map[string][]taskexecutor.Result{
			"implement": {{Kind: taskexecutor.ResultKindResult, Result: resultWithArtifact("impl.md")}},
		},
	})
	defer service.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = service.Run(ctx) }()

	service.Dispatch(RunCommand{Type: CommandStartTask, Description: "force retry", WorkDir: service.workDir})
	failed := waitForEvent(t, service.Events(), EventTaskFailed)

	err := service.retryNode(context.Background(), failed.TaskID, failed.NodeRunID, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "retry unavailable")

	err = service.retryNode(context.Background(), failed.TaskID, failed.NodeRunID, true)
	require.NoError(t, err)

	completed := waitForEvent(t, service.Events(), EventTaskCompleted)
	require.NotNil(t, completed.TaskView)
	assert.Equal(t, taskdomain.TaskStatusDone, completed.TaskView.Status)

	runs, err := service.store.ListNodeRunsByTask(context.Background(), failed.TaskID)
	require.NoError(t, err)
	require.Len(t, runs, 3)
	require.NotNil(t, runs[1].TriggeredBy)
	assert.Equal(t, taskdomain.TriggerReasonManualRetryForce, runs[1].TriggeredBy.Reason)
}

func TestNewServiceReconcilesStaleRunningRunsOnStartup(t *testing.T) {
	workDir := t.TempDir()
	store, err := taskstore.Open(workDir)
	require.NoError(t, err)

	now := time.Now().UTC()
	task := taskdomain.Task{
		ID:          "task-stale",
		Description: "stale",
		WorkDir:     workDir,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	require.NoError(t, store.CreateTask(context.Background(), task))
	_, err = taskconfig.Materialize(workDir, task.ID, "")
	require.NoError(t, err)
	require.NoError(t, store.SaveNodeRun(context.Background(), taskdomain.NodeRun{
		ID:        "run-stale",
		TaskID:    task.ID,
		NodeName:  "upsert_plan",
		Status:    taskdomain.NodeRunRunning,
		StartedAt: now,
	}))
	require.NoError(t, store.Close())

	service, err := NewService(workDir, "", &fakeExecutor{steps: map[string][]taskexecutor.Result{}})
	require.NoError(t, err)
	defer service.Close()

	runs, err := service.store.ListNodeRunsByTask(context.Background(), task.ID)
	require.NoError(t, err)
	require.Len(t, runs, 1)
	assert.Equal(t, taskdomain.NodeRunFailed, runs[0].Status)
	assert.Equal(t, taskdomain.FailureReasonOrphanedAfterRestart, runs[0].FailureReason)
	require.NotNil(t, runs[0].CompletedAt)
}

func TestPrepareShutdownMarksRunningRunsInterrupted(t *testing.T) {
	blockRelease := make(chan struct{})
	blockStarted := make(chan struct{}, 1)
	service := newTestServiceWithConfig(t, singleAgentTerminalFixture(), &blockingExecutor{
		steps: map[string][]taskexecutor.Result{
			"implement": {{Kind: taskexecutor.ResultKindResult, Result: resultWithArtifact("impl.md")}},
		},
		blockNode:    "implement",
		blockRelease: blockRelease,
		blockStarted: blockStarted,
	})
	defer service.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = service.Run(ctx) }()

	service.Dispatch(RunCommand{Type: CommandStartTask, Description: "shutdown", WorkDir: service.workDir})
	<-blockStarted
	require.NoError(t, service.PrepareShutdown(context.Background()))
	cancel()
	close(blockRelease)

	runs, err := service.store.ListNodeRunsByStatus(context.Background(), taskdomain.NodeRunFailed)
	require.NoError(t, err)
	require.NotEmpty(t, runs)
	assert.Equal(t, taskdomain.FailureReasonInterruptedByUser, runs[0].FailureReason)
}

type fakeExecutor struct {
	mu       sync.Mutex
	steps    map[string][]taskexecutor.Result
	progress map[string][]taskexecutor.Progress
	errors   map[string][]error
	requests []taskexecutor.Request
}

func (f *fakeExecutor) Execute(ctx context.Context, req taskexecutor.Request, progress func(taskexecutor.Progress)) (taskexecutor.Result, error) {
	f.mu.Lock()
	f.requests = append(f.requests, req)
	progressItems := append([]taskexecutor.Progress(nil), f.progress[req.NodeRun.NodeName]...)
	errSequence := f.errors[req.NodeRun.NodeName]
	if len(errSequence) > 0 {
		execErr := errSequence[0]
		f.errors[req.NodeRun.NodeName] = errSequence[1:]
		f.mu.Unlock()
		if progress != nil {
			for _, item := range progressItems {
				progress(item)
			}
		}
		return taskexecutor.Result{}, execErr
	}
	sequence := f.steps[req.NodeRun.NodeName]
	if len(sequence) == 0 {
		f.mu.Unlock()
		return taskexecutor.Result{}, fmt.Errorf("unexpected node %s", req.NodeRun.NodeName)
	}
	result := sequence[0]
	f.steps[req.NodeRun.NodeName] = sequence[1:]
	f.mu.Unlock()
	if progress != nil {
		if len(progressItems) == 0 {
			progressItems = []taskexecutor.Progress{{Message: fmt.Sprintf("running %s", req.NodeRun.NodeName)}}
		}
		for _, item := range progressItems {
			progress(item)
		}
	}
	return materializeExecutorArtifacts(req, result)
}

func (f *fakeExecutor) requestsForNode(nodeName string) []taskexecutor.Request {
	f.mu.Lock()
	defer f.mu.Unlock()
	var requests []taskexecutor.Request
	for _, req := range f.requests {
		if req.NodeRun.NodeName == nodeName {
			requests = append(requests, req)
		}
	}
	return requests
}

func newTestService(t *testing.T, executor taskexecutor.Executor) *Service {
	t.Helper()
	workDir := t.TempDir()
	service, err := NewService(workDir, "", executor)
	require.NoError(t, err)
	return service
}

func newTestServiceWithConfig(t *testing.T, cfg *taskconfig.Config, executor taskexecutor.Executor) *Service {
	t.Helper()
	workDir := t.TempDir()
	configPath := writeOverrideConfig(t, cfg)
	service, err := NewService(workDir, configPath, executor)
	require.NoError(t, err)
	return service
}

func waitForEvent(t *testing.T, events <-chan RunEvent, want EventType) RunEvent {
	t.Helper()
	timeout := time.After(5 * time.Second)
	for {
		select {
		case <-timeout:
			t.Fatalf("timed out waiting for %s", want)
		case event := <-events:
			if event.Type == want {
				return event
			}
		}
	}
}

func waitForEventWhere(t *testing.T, events <-chan RunEvent, timeout time.Duration, match func(RunEvent) bool) RunEvent {
	t.Helper()
	deadline := time.After(timeout)
	for {
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for matching event")
		case event := <-events:
			if match(event) {
				return event
			}
		}
	}
}

func assertNoEventTypeWithin(t *testing.T, events <-chan RunEvent, want EventType, duration time.Duration) {
	t.Helper()
	deadline := time.After(duration)
	for {
		select {
		case <-deadline:
			return
		case event := <-events:
			if event.Type == want {
				t.Fatalf("unexpected %s event", want)
			}
		}
	}
}

func resultWithArtifact(name string) map[string]interface{} {
	return map[string]interface{}{
		"file_paths": []interface{}{name},
	}
}

func assertNodeRunCounts(t *testing.T, runs []taskdomain.NodeRun, want map[string]int) {
	t.Helper()
	got := map[string]int{}
	for _, run := range runs {
		got[run.NodeName]++
	}
	assert.Equal(t, want, got)
}

type blockingExecutor struct {
	mu             sync.Mutex
	steps          map[string][]taskexecutor.Result
	progressByNode map[string][]taskexecutor.Progress
	blockNode      string
	blockRelease   <-chan struct{}
	blockStarted   chan<- struct{}
}

func (b *blockingExecutor) Execute(ctx context.Context, req taskexecutor.Request, progress func(taskexecutor.Progress)) (taskexecutor.Result, error) {
	b.mu.Lock()
	progressItems := append([]taskexecutor.Progress(nil), b.progressByNode[req.NodeRun.NodeName]...)
	sequence := b.steps[req.NodeRun.NodeName]
	if len(sequence) == 0 {
		b.mu.Unlock()
		return taskexecutor.Result{}, fmt.Errorf("unexpected node %s", req.NodeRun.NodeName)
	}
	result := sequence[0]
	b.steps[req.NodeRun.NodeName] = sequence[1:]
	b.mu.Unlock()
	if progress != nil {
		for _, item := range progressItems {
			progress(item)
		}
	}
	if req.NodeRun.NodeName == b.blockNode {
		select {
		case b.blockStarted <- struct{}{}:
		default:
		}
		select {
		case <-ctx.Done():
			return taskexecutor.Result{}, ctx.Err()
		case <-b.blockRelease:
		}
	}
	return materializeExecutorArtifacts(req, result)
}

func materializeExecutorArtifacts(req taskexecutor.Request, result taskexecutor.Result) (taskexecutor.Result, error) {
	if result.Kind == taskexecutor.ResultKindResult {
		for _, rawPath := range taskdomain.ArtifactPaths(result.Result) {
			path := rawPath
			if !filepath.IsAbs(path) {
				path = filepath.Join(req.ArtifactDir, path)
			}
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				return taskexecutor.Result{}, err
			}
			if err := os.WriteFile(path, []byte("artifact"), 0o644); err != nil {
				return taskexecutor.Result{}, err
			}
		}
	}
	if result.SessionID == "" {
		result.SessionID = req.NodeRun.ID + "-session"
	}
	return result, nil
}

func writeOverrideConfig(t *testing.T, cfg *taskconfig.Config) string {
	t.Helper()
	configDir := t.TempDir()
	promptsDir := filepath.Join(configDir, "prompts")
	require.NoError(t, os.MkdirAll(promptsDir, 0o755))

	for name, def := range cfg.NodeDefinitions {
		if def.Type == taskconfig.NodeTypeHuman || def.Type == taskconfig.NodeTypeTerminal {
			continue
		}
		if def.SystemPrompt == "" {
			def.SystemPrompt = "./prompts/" + name + ".md"
			cfg.NodeDefinitions[name] = def
		}
		path := filepath.Join(configDir, strings.TrimPrefix(def.SystemPrompt, "./"))
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte("# "+name), 0o644))
	}

	data, err := yaml.Marshal(cfg)
	require.NoError(t, err)
	configPath := filepath.Join(configDir, "taskflow.yaml")
	require.NoError(t, os.WriteFile(configPath, data, 0o644))
	return configPath
}

func joinAllRuntimeFixture() *taskconfig.Config {
	return &taskconfig.Config{
		Version: 1,
		Clarification: taskconfig.ClarificationConfig{
			MaxQuestions:          4,
			MaxOptionsPerQuestion: 4,
			MinOptionsPerQuestion: 2,
		},
		Topology: taskconfig.Topology{
			MaxIterations: 3,
			Entry:         "start",
			Nodes: []taskconfig.NodeRef{
				{Name: "start"},
				{Name: "left"},
				{Name: "right"},
				{Name: "join", Join: taskconfig.JoinAll},
				{Name: "end"},
			},
			Edges: []taskconfig.Edge{
				{From: "start", To: "left"},
				{From: "start", To: "right"},
				{From: "left", To: "join"},
				{From: "right", To: "join"},
				{From: "join", To: "end"},
			},
		},
		NodeDefinitions: map[string]taskconfig.NodeDefinition{
			"start": artifactAgentNode(),
			"left":  artifactAgentNode(),
			"right": artifactAgentNode(),
			"join":  artifactAgentNode(),
			"end":   {Type: taskconfig.NodeTypeTerminal},
		},
	}
}

func parallelTerminalFixture() *taskconfig.Config {
	return &taskconfig.Config{
		Version: 1,
		Clarification: taskconfig.ClarificationConfig{
			MaxQuestions:          4,
			MaxOptionsPerQuestion: 4,
			MinOptionsPerQuestion: 2,
		},
		Topology: taskconfig.Topology{
			MaxIterations: 3,
			Entry:         "start",
			Nodes: []taskconfig.NodeRef{
				{Name: "start"},
				{Name: "left"},
				{Name: "right"},
				{Name: "end_left"},
				{Name: "end_right"},
			},
			Edges: []taskconfig.Edge{
				{From: "start", To: "left"},
				{From: "start", To: "right"},
				{From: "left", To: "end_left"},
				{From: "right", To: "end_right"},
			},
		},
		NodeDefinitions: map[string]taskconfig.NodeDefinition{
			"start":     artifactAgentNode(),
			"left":      artifactAgentNode(),
			"right":     artifactAgentNode(),
			"end_left":  {Type: taskconfig.NodeTypeTerminal},
			"end_right": {Type: taskconfig.NodeTypeTerminal},
		},
	}
}

func singleAgentTerminalFixture() *taskconfig.Config {
	return &taskconfig.Config{
		Version: 1,
		Clarification: taskconfig.ClarificationConfig{
			MaxQuestions:          4,
			MaxOptionsPerQuestion: 4,
			MinOptionsPerQuestion: 2,
		},
		Topology: taskconfig.Topology{
			MaxIterations: 1,
			Entry:         "implement",
			Nodes: []taskconfig.NodeRef{
				{Name: "implement"},
				{Name: "done"},
			},
			Edges: []taskconfig.Edge{
				{From: "implement", To: "done"},
			},
		},
		NodeDefinitions: map[string]taskconfig.NodeDefinition{
			"implement": artifactAgentNode(),
			"done":      {Type: taskconfig.NodeTypeTerminal},
		},
	}
}

func artifactAgentNode() taskconfig.NodeDefinition {
	deny := false
	return taskconfig.NodeDefinition{
		Type:         taskconfig.NodeTypeAgent,
		SystemPrompt: "./prompts/node.md",
		ResultSchema: taskconfig.JSONSchema{
			Type:                 "object",
			AdditionalProperties: &deny,
			Required:             []string{"file_paths"},
			Properties: map[string]*taskconfig.JSONSchema{
				"file_paths": {
					Type:     "array",
					MinItems: intPtr(1),
					Items:    &taskconfig.JSONSchema{Type: "string"},
				},
			},
		},
	}
}

func intPtr(value int) *int {
	return &value
}

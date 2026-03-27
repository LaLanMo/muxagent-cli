package tasktui

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/LaLanMo/muxagent-cli/internal/taskdomain"
	"github.com/LaLanMo/muxagent-cli/internal/taskruntime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFailureFooterShowsRetryActionAndDispatchesRetry(t *testing.T) {
	service := &fakeService{events: make(chan taskruntime.RunEvent, 8)}
	model := NewModel(service, "/tmp/project", "", nil, "v0.1.0")
	next, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 32})
	model = next.(Model)
	model.currentConfig = retryTUIConfig(2)
	model.current = &taskdomain.TaskView{
		Task:            taskdomain.Task{ID: "task-1", Description: "Broken task"},
		Status:          taskdomain.TaskStatusFailed,
		CurrentNodeName: "implement",
		NodeRuns: []taskdomain.NodeRunView{
			{NodeRun: taskdomain.NodeRun{ID: "run-1", TaskID: "task-1", NodeName: "implement", Status: taskdomain.NodeRunFailed, StartedAt: time.Now().UTC(), CompletedAt: timePtr(time.Now().UTC())}},
		},
	}
	model.activeTaskID = "task-1"
	model.screen = ScreenFailed
	model.errorText = "executor failed"
	model.syncComponents()

	view := strippedView(model.View().Content)
	assert.Contains(t, view, "Retry step")
	assert.Contains(t, view, "Ctrl+C quit")
	assert.Equal(t, FocusRegionActionPanel, model.focusRegion)

	next, cmd := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = next.(Model)
	require.NotNil(t, cmd)
	require.Nil(t, cmd())
	require.Len(t, service.dispatched, 1)
	assert.Equal(t, taskruntime.CommandRetryNode, service.dispatched[0].Type)
	assert.Equal(t, "run-1", service.dispatched[0].NodeRunID)
	assert.False(t, service.dispatched[0].Force)
	assert.Equal(t, ScreenRunning, model.screen)
}

func TestFailureFooterShowsForceRetryWhenIterationLimitReached(t *testing.T) {
	service := &fakeService{events: make(chan taskruntime.RunEvent, 8)}
	model := NewModel(service, "/tmp/project", "", nil, "v0.1.0")
	next, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 32})
	model = next.(Model)
	model.currentConfig = retryTUIConfig(1)
	model.current = &taskdomain.TaskView{
		Task:            taskdomain.Task{ID: "task-1", Description: "Broken task"},
		Status:          taskdomain.TaskStatusFailed,
		CurrentNodeName: "implement",
		NodeRuns: []taskdomain.NodeRunView{
			{NodeRun: taskdomain.NodeRun{ID: "run-1", TaskID: "task-1", NodeName: "implement", Status: taskdomain.NodeRunFailed, StartedAt: time.Now().UTC(), CompletedAt: timePtr(time.Now().UTC())}},
		},
	}
	model.activeTaskID = "task-1"
	model.screen = ScreenFailed
	model.errorText = "executor failed"
	model.syncComponents()

	view := strippedView(model.View().Content)
	assert.Contains(t, view, "Force retry")
	assert.Contains(t, view, "Retry limit reached")
	assert.Contains(t, view, "Ctrl+C quit")

	next, cmd := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = next.(Model)
	require.NotNil(t, cmd)
	require.Nil(t, cmd())
	require.Len(t, service.dispatched, 1)
	assert.Equal(t, taskruntime.CommandRetryNode, service.dispatched[0].Type)
	assert.True(t, service.dispatched[0].Force)
}

func TestFailureFooterShowsForceContinueForBlockedStep(t *testing.T) {
	service := &fakeService{events: make(chan taskruntime.RunEvent, 8)}
	model := NewModel(service, "/tmp/project", "", nil, "v0.1.0")
	next, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 32})
	model = next.(Model)
	model.currentConfig = retryTUIConfig(1)
	model.current = &taskdomain.TaskView{
		Task:            taskdomain.Task{ID: "task-1", Description: "Blocked task"},
		Status:          taskdomain.TaskStatusFailed,
		CurrentNodeName: "implement",
		NodeRuns: []taskdomain.NodeRunView{
			{NodeRun: taskdomain.NodeRun{ID: "run-1", TaskID: "task-1", NodeName: "implement", Status: taskdomain.NodeRunDone, StartedAt: time.Now().UTC(), CompletedAt: timePtr(time.Now().UTC())}},
		},
		BlockedSteps: []taskdomain.BlockedStep{
			{
				NodeName:  "implement",
				Iteration: 2,
				Reason:    "node \"implement\" exceeded max_iterations",
				TriggeredBy: &taskdomain.TriggeredBy{
					NodeRunID: "run-1",
					Reason:    "edge: implement -> implement",
				},
				CreatedAt: time.Now().UTC().Add(time.Second),
			},
		},
	}
	model.activeTaskID = "task-1"
	model.screen = ScreenFailed
	model.syncComponents()

	view := strippedView(model.View().Content)
	assert.Contains(t, view, "Task blocked")
	assert.Contains(t, view, "Force continue")
	assert.Contains(t, view, "Ctrl+C quit")

	next, cmd := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = next.(Model)
	require.NotNil(t, cmd)
	require.Nil(t, cmd())
	require.Len(t, service.dispatched, 1)
	assert.Equal(t, taskruntime.CommandContinueBlocked, service.dispatched[0].Type)
	assert.Equal(t, ScreenRunning, model.screen)
}

func TestRetryCommandErrorRestoresFailedScreen(t *testing.T) {
	service := &fakeService{events: make(chan taskruntime.RunEvent, 8)}
	model := NewModel(service, "/tmp/project", "", nil, "v0.1.0")
	next, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 32})
	model = next.(Model)
	model.currentConfig = retryTUIConfig(2)
	model.current = &taskdomain.TaskView{
		Task:            taskdomain.Task{ID: "task-1", Description: "Broken task"},
		Status:          taskdomain.TaskStatusFailed,
		CurrentNodeName: "implement",
		NodeRuns: []taskdomain.NodeRunView{
			{NodeRun: taskdomain.NodeRun{ID: "run-1", TaskID: "task-1", NodeName: "implement", Status: taskdomain.NodeRunFailed, StartedAt: time.Now().UTC(), CompletedAt: timePtr(time.Now().UTC())}},
		},
	}
	model.activeTaskID = "task-1"
	model.screen = ScreenFailed
	model.errorText = "executor failed"
	model.syncComponents()

	next, cmd := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = next.(Model)
	require.NotNil(t, cmd)
	require.Nil(t, cmd())
	require.NotNil(t, model.pendingRuntimeCmd)
	assert.Equal(t, pendingRuntimeCommandRetry, model.pendingRuntimeCmd.kind)
	assert.Equal(t, ScreenRunning, model.screen)

	next, _ = model.Update(taskruntime.RunEvent{
		Type:   taskruntime.EventCommandError,
		TaskID: "task-1",
		Error:  &taskruntime.RunError{Message: "cannot retry node"},
	})
	model = next.(Model)

	assert.Equal(t, ScreenFailed, model.screen)
	assert.Equal(t, "cannot retry node", model.errorText)
	assert.Equal(t, failureActionRetry, model.failure.action)
	assert.Nil(t, model.pendingRuntimeCmd)
}

func TestForceContinueCommandErrorRestoresFailedScreen(t *testing.T) {
	service := &fakeService{events: make(chan taskruntime.RunEvent, 8)}
	model := NewModel(service, "/tmp/project", "", nil, "v0.1.0")
	next, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 32})
	model = next.(Model)
	model.currentConfig = retryTUIConfig(1)
	model.current = &taskdomain.TaskView{
		Task:            taskdomain.Task{ID: "task-1", Description: "Blocked task"},
		Status:          taskdomain.TaskStatusFailed,
		CurrentNodeName: "implement",
		NodeRuns: []taskdomain.NodeRunView{
			{NodeRun: taskdomain.NodeRun{ID: "run-1", TaskID: "task-1", NodeName: "implement", Status: taskdomain.NodeRunDone, StartedAt: time.Now().UTC(), CompletedAt: timePtr(time.Now().UTC())}},
		},
		BlockedSteps: []taskdomain.BlockedStep{
			{
				NodeName:  "implement",
				Iteration: 2,
				Reason:    "node \"implement\" exceeded max_iterations",
				TriggeredBy: &taskdomain.TriggeredBy{
					NodeRunID: "run-1",
					Reason:    "edge: implement -> implement",
				},
				CreatedAt: time.Now().UTC().Add(time.Second),
			},
		},
	}
	model.activeTaskID = "task-1"
	model.screen = ScreenFailed
	model.syncComponents()

	next, cmd := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = next.(Model)
	require.NotNil(t, cmd)
	require.Nil(t, cmd())
	require.NotNil(t, model.pendingRuntimeCmd)
	assert.Equal(t, pendingRuntimeCommandContinueBlocked, model.pendingRuntimeCmd.kind)
	assert.Equal(t, ScreenRunning, model.screen)

	next, _ = model.Update(taskruntime.RunEvent{
		Type:   taskruntime.EventCommandError,
		TaskID: "task-1",
		Error:  &taskruntime.RunError{Message: "cannot continue blocked step"},
	})
	model = next.(Model)

	assert.Equal(t, ScreenFailed, model.screen)
	assert.Equal(t, "cannot continue blocked step", model.errorText)
	assert.Equal(t, failureActionForceContinue, model.failure.action)
	assert.Nil(t, model.pendingRuntimeCmd)
}

func TestFailedScreenShiftTabTogglesArtifactsRoundTrip(t *testing.T) {
	tempDir := t.TempDir()
	artifactPath := filepath.Join(tempDir, "failure.log")
	require.NoError(t, os.WriteFile(artifactPath, []byte("executor failed\n"), 0o644))

	service := &fakeService{events: make(chan taskruntime.RunEvent, 8)}
	model := NewModel(service, tempDir, "", nil, "v0.1.0")
	next, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 32})
	model = next.(Model)
	model.currentConfig = retryTUIConfig(2)
	model.current = &taskdomain.TaskView{
		Task:            taskdomain.Task{ID: "task-1", Description: "Broken task", WorkDir: tempDir},
		Status:          taskdomain.TaskStatusFailed,
		CurrentNodeName: "implement",
		ArtifactPaths:   []string{artifactPath},
		NodeRuns: []taskdomain.NodeRunView{
			{NodeRun: taskdomain.NodeRun{ID: "run-1", TaskID: "task-1", NodeName: "implement", Status: taskdomain.NodeRunFailed, StartedAt: time.Now().UTC(), CompletedAt: timePtr(time.Now().UTC())}, ArtifactPaths: []string{artifactPath}},
		},
	}
	model.activeTaskID = "task-1"
	model.screen = ScreenFailed
	model.errorText = "executor failed"
	model.syncComponents()

	assert.Equal(t, FocusRegionActionPanel, model.focusRegion)
	assert.Equal(t, DetailTabTimeline, model.activeDetailTab)

	next, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	model = next.(Model)

	assert.Equal(t, DetailTabArtifacts, model.activeDetailTab)
	assert.Equal(t, FocusRegionArtifactFiles, model.focusRegion)

	next, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	model = next.(Model)

	assert.Equal(t, DetailTabTimeline, model.activeDetailTab)
	assert.Equal(t, FocusRegionDetail, model.focusRegion)
}

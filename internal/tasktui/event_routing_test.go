package tasktui

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/LaLanMo/muxagent-cli/internal/taskconfig"
	"github.com/LaLanMo/muxagent-cli/internal/taskdomain"
	"github.com/LaLanMo/muxagent-cli/internal/taskruntime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBackgroundEventsDoNotLeaveTaskList(t *testing.T) {
	baseTask := taskdomain.Task{ID: "task-1", Description: "Implement login"}
	draftView := taskdomain.TaskView{Task: baseTask, Status: taskdomain.TaskStatusDraft}
	runningView := taskdomain.TaskView{
		Task:            baseTask,
		Status:          taskdomain.TaskStatusRunning,
		CurrentNodeName: "implement",
		NodeRuns: []taskdomain.NodeRunView{
			{NodeRun: taskdomain.NodeRun{ID: "run-1", TaskID: "task-1", NodeName: "implement", Status: taskdomain.NodeRunRunning, StartedAt: time.Now().UTC()}},
		},
	}
	awaitingApprovalView := taskdomain.TaskView{
		Task:            baseTask,
		Status:          taskdomain.TaskStatusAwaitingUser,
		CurrentNodeType: taskconfig.NodeTypeHuman,
		CurrentNodeName: "approve_plan",
		NodeRuns: []taskdomain.NodeRunView{
			{NodeRun: taskdomain.NodeRun{ID: "run-1", TaskID: "task-1", NodeName: "approve_plan", Status: taskdomain.NodeRunAwaitingUser, StartedAt: time.Now().UTC()}},
		},
	}
	awaitingInputView := taskdomain.TaskView{
		Task:            baseTask,
		Status:          taskdomain.TaskStatusAwaitingUser,
		CurrentNodeType: taskconfig.NodeTypeAgent,
		CurrentNodeName: "draft_plan",
		NodeRuns: []taskdomain.NodeRunView{
			{NodeRun: taskdomain.NodeRun{ID: "run-2", TaskID: "task-1", NodeName: "draft_plan", Status: taskdomain.NodeRunAwaitingUser, StartedAt: time.Now().UTC()}},
		},
	}
	doneView := taskdomain.TaskView{
		Task:            baseTask,
		Status:          taskdomain.TaskStatusDone,
		CurrentNodeName: "done",
		NodeRuns: []taskdomain.NodeRunView{
			{NodeRun: taskdomain.NodeRun{ID: "run-1", TaskID: "task-1", NodeName: "done", Status: taskdomain.NodeRunDone, StartedAt: time.Now().UTC(), CompletedAt: timePtr(time.Now().UTC())}},
		},
	}
	failedView := taskdomain.TaskView{
		Task:            baseTask,
		Status:          taskdomain.TaskStatusFailed,
		CurrentNodeName: "implement",
		NodeRuns: []taskdomain.NodeRunView{
			{NodeRun: taskdomain.NodeRun{ID: "run-1", TaskID: "task-1", NodeName: "implement", Status: taskdomain.NodeRunFailed, StartedAt: time.Now().UTC(), CompletedAt: timePtr(time.Now().UTC())}},
		},
	}

	tests := []struct {
		name      string
		event     func() taskruntime.RunEvent
		wantSnips []string
	}{
		{
			name: "node started",
			event: func() taskruntime.RunEvent {
				view := runningView
				return taskruntime.RunEvent{
					Type:      taskruntime.EventNodeStarted,
					TaskID:    "task-1",
					NodeRunID: "run-1",
					NodeName:  "implement",
					TaskView:  &view,
				}
			},
			wantSnips: []string{"running Implement login"},
		},
		{
			name: "node progress",
			event: func() taskruntime.RunEvent {
				return taskruntime.RunEvent{
					Type:      taskruntime.EventNodeProgress,
					TaskID:    "task-1",
					NodeRunID: "run-1",
					NodeName:  "implement",
					Progress:  &taskruntime.ProgressInfo{Message: "stream update"},
				}
			},
			wantSnips: []string{"running Implement login"},
		},
		{
			name: "node completed",
			event: func() taskruntime.RunEvent {
				view := runningView
				return taskruntime.RunEvent{
					Type:      taskruntime.EventNodeCompleted,
					TaskID:    "task-1",
					NodeRunID: "run-1",
					NodeName:  "implement",
					TaskView:  &view,
				}
			},
			wantSnips: []string{"running Implement login"},
		},
		{
			name: "human input requested",
			event: func() taskruntime.RunEvent {
				view := awaitingApprovalView
				return taskruntime.RunEvent{
					Type:      taskruntime.EventInputRequested,
					TaskID:    "task-1",
					NodeRunID: "run-1",
					NodeName:  "approve_plan",
					TaskView:  &view,
					InputRequest: &taskruntime.InputRequest{
						Kind:      taskruntime.InputKindHumanNode,
						TaskID:    "task-1",
						NodeRunID: "run-1",
						NodeName:  "approve_plan",
					},
				}
			},
			wantSnips: []string{"Implement login", "awaiting approval", "approve_plan"},
		},
		{
			name: "clarification requested",
			event: func() taskruntime.RunEvent {
				view := awaitingInputView
				return taskruntime.RunEvent{
					Type:      taskruntime.EventInputRequested,
					TaskID:    "task-1",
					NodeRunID: "run-2",
					NodeName:  "draft_plan",
					TaskView:  &view,
					InputRequest: &taskruntime.InputRequest{
						Kind:      taskruntime.InputKindClarification,
						TaskID:    "task-1",
						NodeRunID: "run-2",
						NodeName:  "draft_plan",
					},
				}
			},
			wantSnips: []string{"Implement login", "awaiting clarification", "draft_plan"},
		},
		{
			name: "task completed",
			event: func() taskruntime.RunEvent {
				view := doneView
				return taskruntime.RunEvent{
					Type:     taskruntime.EventTaskCompleted,
					TaskID:   "task-1",
					NodeName: "done",
					TaskView: &view,
				}
			},
			wantSnips: []string{"done Implement login"},
		},
		{
			name: "task failed",
			event: func() taskruntime.RunEvent {
				view := failedView
				return taskruntime.RunEvent{
					Type:     taskruntime.EventTaskFailed,
					TaskID:   "task-1",
					NodeName: "implement",
					TaskView: &view,
					Error:    &taskruntime.RunError{Message: "executor failed"},
				}
			},
			wantSnips: []string{"failed Implement login"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model := NewModel(&fakeService{events: make(chan taskruntime.RunEvent, 8)}, "/tmp/project", "", nil, "v0.1.0")
			next, _ := model.Update(tea.WindowSizeMsg{Width: 100, Height: 28})
			model = next.(Model)
			model.tasks = []taskdomain.TaskView{draftView}
			model.syncComponents()

			next, _ = model.Update(tt.event())
			model = next.(Model)

			assert.Equal(t, ScreenTaskList, model.screen)
			view := strippedView(model.View().Content)
			assert.Contains(t, view, "new task")
			assert.NotContains(t, view, "Approve this plan?")
			assert.NotContains(t, view, "Artifacts (")
			for _, snippet := range tt.wantSnips {
				assert.Contains(t, view, snippet)
			}
		})
	}
}

func TestBackgroundNodeProgressDoesNotLeaveTaskList(t *testing.T) {
	model := NewModel(&fakeService{events: make(chan taskruntime.RunEvent, 8)}, "/tmp/project", "", nil, "v0.1.0")
	next, _ := model.Update(tea.WindowSizeMsg{Width: 100, Height: 28})
	model = next.(Model)
	model.tasks = []taskdomain.TaskView{
		{
			Task:   taskdomain.Task{ID: "task-1", Description: "Implement login"},
			Status: taskdomain.TaskStatusRunning,
		},
	}
	model.syncComponents()

	next, _ = model.Update(taskruntime.RunEvent{
		Type:      taskruntime.EventNodeProgress,
		TaskID:    "task-1",
		NodeRunID: "run-1",
		NodeName:  "implement",
		Progress:  &taskruntime.ProgressInfo{Message: "stream update"},
	})
	model = next.(Model)

	assert.Equal(t, ScreenTaskList, model.screen)
	assert.Equal(t, []string{"stream update"}, model.progressByRun["run-1"])
	view := strippedView(model.View().Content)
	assert.Contains(t, view, "new task")
	assert.NotContains(t, view, "Artifacts (")
	assert.NotContains(t, view, "stream update")
}

func TestActiveTaskEventsStillDriveDetailTransitions(t *testing.T) {
	baseView := taskdomain.TaskView{
		Task:            taskdomain.Task{ID: "task-1", Description: "Implement login"},
		Status:          taskdomain.TaskStatusRunning,
		CurrentNodeName: "implement",
		NodeRuns: []taskdomain.NodeRunView{
			{NodeRun: taskdomain.NodeRun{ID: "run-1", TaskID: "task-1", NodeName: "implement", Status: taskdomain.NodeRunRunning, StartedAt: time.Now().UTC()}},
		},
	}

	tests := []struct {
		name       string
		event      func() taskruntime.RunEvent
		wantScreen Screen
		wantText   string
	}{
		{
			name: "progress keeps running screen",
			event: func() taskruntime.RunEvent {
				return taskruntime.RunEvent{
					Type:      taskruntime.EventNodeProgress,
					TaskID:    "task-1",
					NodeRunID: "run-1",
					NodeName:  "implement",
					Progress:  &taskruntime.ProgressInfo{Message: "stream update"},
				}
			},
			wantScreen: ScreenRunning,
			wantText:   "stream update",
		},
		{
			name: "clarification request opens clarification screen",
			event: func() taskruntime.RunEvent {
				view := taskdomain.TaskView{
					Task:            baseView.Task,
					Status:          taskdomain.TaskStatusAwaitingUser,
					CurrentNodeName: "implement",
					NodeRuns: []taskdomain.NodeRunView{
						{NodeRun: taskdomain.NodeRun{ID: "run-1", TaskID: "task-1", NodeName: "implement", Status: taskdomain.NodeRunAwaitingUser, StartedAt: time.Now().UTC()}},
					},
				}
				return taskruntime.RunEvent{
					Type:      taskruntime.EventInputRequested,
					TaskID:    "task-1",
					NodeRunID: "run-1",
					NodeName:  "implement",
					TaskView:  &view,
					InputRequest: &taskruntime.InputRequest{
						Kind:      taskruntime.InputKindClarification,
						TaskID:    "task-1",
						NodeRunID: "run-1",
						NodeName:  "implement",
						Questions: []taskdomain.ClarificationQuestion{{Question: "What should we do?"}},
					},
				}
			},
			wantScreen: ScreenClarification,
			wantText:   "What should we do?",
		},
		{
			name: "approval request opens approval screen",
			event: func() taskruntime.RunEvent {
				view := taskdomain.TaskView{
					Task:            baseView.Task,
					Status:          taskdomain.TaskStatusAwaitingUser,
					CurrentNodeName: "approve_plan",
					NodeRuns: []taskdomain.NodeRunView{
						{NodeRun: taskdomain.NodeRun{ID: "run-1", TaskID: "task-1", NodeName: "approve_plan", Status: taskdomain.NodeRunAwaitingUser, StartedAt: time.Now().UTC()}},
					},
				}
				return taskruntime.RunEvent{
					Type:      taskruntime.EventInputRequested,
					TaskID:    "task-1",
					NodeRunID: "run-1",
					NodeName:  "approve_plan",
					TaskView:  &view,
					InputRequest: &taskruntime.InputRequest{
						Kind:      taskruntime.InputKindHumanNode,
						TaskID:    "task-1",
						NodeRunID: "run-1",
						NodeName:  "approve_plan",
					},
				}
			},
			wantScreen: ScreenApproval,
			wantText:   "Approve this plan?",
		},
		{
			name: "task completed opens complete screen",
			event: func() taskruntime.RunEvent {
				view := taskdomain.TaskView{
					Task:            baseView.Task,
					Status:          taskdomain.TaskStatusDone,
					CurrentNodeName: "done",
					NodeRuns: []taskdomain.NodeRunView{
						{NodeRun: taskdomain.NodeRun{ID: "run-1", TaskID: "task-1", NodeName: "done", Status: taskdomain.NodeRunDone, StartedAt: time.Now().UTC(), CompletedAt: timePtr(time.Now().UTC())}},
					},
				}
				return taskruntime.RunEvent{
					Type:      taskruntime.EventTaskCompleted,
					TaskID:    "task-1",
					NodeRunID: "run-1",
					NodeName:  "done",
					TaskView:  &view,
				}
			},
			wantScreen: ScreenComplete,
			wantText:   "Task completed successfully",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model := NewModel(&fakeService{events: make(chan taskruntime.RunEvent, 8)}, "/tmp/project", "", nil, "v0.1.0")
			next, _ := model.Update(tea.WindowSizeMsg{Width: 100, Height: 28})
			model = next.(Model)
			model.current = &baseView
			model.activeTaskID = baseView.Task.ID
			model.screen = ScreenRunning
			model.syncComponents()

			next, _ = model.Update(tt.event())
			model = next.(Model)

			assert.Equal(t, tt.wantScreen, model.screen)
			assert.Equal(t, "task-1", model.activeTaskID)
			assert.Contains(t, strippedView(model.View().Content), tt.wantText)
		})
	}
}

func TestTaskCompletedEventShowsArtifactsPaneImmediately(t *testing.T) {
	tempDir := t.TempDir()
	artifactPath := filepath.Join(tempDir, "summary.md")
	require.NoError(t, os.WriteFile(artifactPath, []byte("# Summary\n\n- Ship it\n"), 0o644))

	service := &fakeService{events: make(chan taskruntime.RunEvent, 8)}
	model := NewModel(service, tempDir, "", nil, "v0.1.0")
	next, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 32})
	model = next.(Model)
	model.current = &taskdomain.TaskView{
		Task:          taskdomain.Task{ID: "task-1", Description: "Implement login", WorkDir: tempDir},
		Status:        taskdomain.TaskStatusRunning,
		ArtifactPaths: []string{artifactPath},
		NodeRuns: []taskdomain.NodeRunView{
			{NodeRun: taskdomain.NodeRun{ID: "run-1", TaskID: "task-1", NodeName: "verify", Status: taskdomain.NodeRunRunning, StartedAt: time.Now().UTC()}},
		},
	}
	model.activeTaskID = "task-1"
	model.screen = ScreenRunning
	model.syncComponents()

	completedView := taskdomain.TaskView{
		Task:          taskdomain.Task{ID: "task-1", Description: "Implement login", WorkDir: tempDir},
		Status:        taskdomain.TaskStatusDone,
		ArtifactPaths: []string{artifactPath},
		NodeRuns: []taskdomain.NodeRunView{
			{NodeRun: taskdomain.NodeRun{ID: "run-1", TaskID: "task-1", NodeName: "verify", Status: taskdomain.NodeRunDone, StartedAt: time.Now().UTC(), CompletedAt: timePtr(time.Now().UTC())}},
		},
	}

	next, _ = model.Update(taskruntime.RunEvent{
		Type:      taskruntime.EventTaskCompleted,
		TaskID:    "task-1",
		NodeRunID: "run-1",
		NodeName:  "verify",
		TaskView:  &completedView,
	})
	model = next.(Model)

	assert.Equal(t, ScreenComplete, model.screen)
	assert.Equal(t, FocusRegionDetail, model.focusRegion)

	view := strippedView(model.View().Content)
	assert.Contains(t, view, "Shift+Tab artifacts")
	assert.Contains(t, view, "Esc back")
	assert.Contains(t, view, "Ctrl+C quit")
	assert.NotContains(t, view, "Enter open")
}

func TestBackgroundTaskDoesNotStealOpenedDetail(t *testing.T) {
	model := NewModel(&fakeService{events: make(chan taskruntime.RunEvent, 8)}, "/tmp/project", "", nil, "v0.1.0")
	next, _ := model.Update(tea.WindowSizeMsg{Width: 100, Height: 28})
	model = next.(Model)
	model.current = &taskdomain.TaskView{
		Task:            taskdomain.Task{ID: "task-a", Description: "Task A"},
		Status:          taskdomain.TaskStatusRunning,
		CurrentNodeName: "implement",
	}
	model.activeTaskID = "task-a"
	model.tasks = []taskdomain.TaskView{
		{Task: taskdomain.Task{ID: "task-a", Description: "Task A"}, Status: taskdomain.TaskStatusRunning},
		{Task: taskdomain.Task{ID: "task-b", Description: "Task B"}, Status: taskdomain.TaskStatusRunning},
	}
	model.screen = ScreenRunning
	model.syncComponents()

	backgroundView := taskdomain.TaskView{
		Task:            taskdomain.Task{ID: "task-b", Description: "Task B"},
		Status:          taskdomain.TaskStatusFailed,
		CurrentNodeName: "verify",
	}
	next, _ = model.Update(taskruntime.RunEvent{
		Type:     taskruntime.EventTaskFailed,
		TaskID:   "task-b",
		NodeName: "verify",
		TaskView: &backgroundView,
		Error:    &taskruntime.RunError{Message: "verify failed"},
	})
	model = next.(Model)

	assert.Equal(t, ScreenRunning, model.screen)
	require.NotNil(t, model.current)
	assert.Equal(t, "task-a", model.current.Task.ID)
	assert.Equal(t, taskdomain.TaskStatusFailed, taskStatusForID(model.tasks, "task-b"))
}

func TestBackToTaskListStopsFollowingTask(t *testing.T) {
	service := &fakeService{events: make(chan taskruntime.RunEvent, 8)}
	model := NewModel(service, "/tmp/project", "", nil, "v0.1.0")
	next, _ := model.Update(tea.WindowSizeMsg{Width: 100, Height: 28})
	model = next.(Model)
	activeView := taskdomain.TaskView{
		Task:            taskdomain.Task{ID: "task-1", Description: "Implement login"},
		Status:          taskdomain.TaskStatusRunning,
		CurrentNodeName: "implement",
		NodeRuns: []taskdomain.NodeRunView{
			{NodeRun: taskdomain.NodeRun{ID: "run-1", TaskID: "task-1", NodeName: "implement", Status: taskdomain.NodeRunRunning, StartedAt: time.Now().UTC()}},
		},
	}
	model.current = &activeView
	model.activeTaskID = "task-1"
	model.tasks = []taskdomain.TaskView{activeView}
	model.screen = ScreenRunning
	model.syncComponents()
	service.tasks = []taskdomain.TaskView{activeView}

	next, cmd := model.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	model = next.(Model)
	require.NotNil(t, cmd)
	msg := cmd()
	if msg != nil {
		next, _ := model.Update(msg)
		model = next.(Model)
	}
	assert.Equal(t, ScreenTaskList, model.screen)
	assert.Empty(t, model.activeTaskID)
	selected, ok := selectedTaskListItem(model.taskList)
	require.True(t, ok)
	assert.Equal(t, taskListActionNone, selected.action)
	assert.Equal(t, "task-1", selected.view.Task.ID)

	completedView := taskdomain.TaskView{
		Task:            taskdomain.Task{ID: "task-1", Description: "Implement login"},
		Status:          taskdomain.TaskStatusDone,
		CurrentNodeName: "done",
		NodeRuns: []taskdomain.NodeRunView{
			{NodeRun: taskdomain.NodeRun{ID: "run-1", TaskID: "task-1", NodeName: "done", Status: taskdomain.NodeRunDone, StartedAt: time.Now().UTC(), CompletedAt: timePtr(time.Now().UTC())}},
		},
	}
	next, _ = model.Update(taskruntime.RunEvent{
		Type:      taskruntime.EventTaskCompleted,
		TaskID:    "task-1",
		NodeRunID: "run-1",
		NodeName:  "done",
		TaskView:  &completedView,
	})
	model = next.(Model)

	assert.Equal(t, ScreenTaskList, model.screen)
	view := strippedView(model.View().Content)
	assert.Contains(t, view, "new task")
	assert.NotContains(t, view, "Task completed successfully")
}

func TestEscReloadDoesNotDiscardTaskCreatedEvent(t *testing.T) {
	service := &fakeService{events: make(chan taskruntime.RunEvent, 8)}
	model := NewModel(service, "/tmp/project", "", nil, "v0.1.0")
	next, _ := model.Update(tea.WindowSizeMsg{Width: 100, Height: 28})
	model = next.(Model)
	model.editor.SetValue("Implement login")

	cmd := model.submitNewTask()
	require.NotNil(t, cmd)
	require.Nil(t, cmd())
	assert.Equal(t, ScreenRunning, model.screen)

	next, reloadCmd := model.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	model = next.(Model)
	require.NotNil(t, reloadCmd)

	staleReloadMsg := reloadCmd()
	require.IsType(t, tasksLoadedMsg{}, staleReloadMsg)
	assert.Equal(t, ScreenTaskList, model.screen)

	createdAt := time.Now().UTC()
	createdView := taskdomain.TaskView{
		Task: taskdomain.Task{
			ID:          "task-1",
			Description: "Implement login",
			WorkDir:     "/tmp/project",
			CreatedAt:   createdAt,
			UpdatedAt:   createdAt,
		},
		Status:          taskdomain.TaskStatusRunning,
		CurrentNodeName: "implement",
	}
	next, _ = model.Update(taskruntime.RunEvent{
		Type:     taskruntime.EventTaskCreated,
		TaskID:   "task-1",
		TaskView: &createdView,
	})
	model = next.(Model)
	require.Len(t, model.tasks, 1)
	assert.Equal(t, "task-1", model.tasks[0].Task.ID)

	next, _ = model.Update(staleReloadMsg)
	model = next.(Model)

	require.Len(t, model.tasks, 1)
	assert.Equal(t, "task-1", model.tasks[0].Task.ID)
	view := strippedView(model.View().Content)
	assert.Contains(t, view, "Implement login")
}

func TestNodeFailedEventKeepsRunningScreenWhenTaskStillRunning(t *testing.T) {
	service := &fakeService{events: make(chan taskruntime.RunEvent, 8)}
	model := NewModel(service, "/tmp/project", "", nil, "v0.1.0")
	next, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 32})
	model = next.(Model)
	model.currentConfig = retryTUIConfig(2)
	model.current = &taskdomain.TaskView{
		Task:            taskdomain.Task{ID: "task-1", Description: "Parallel task"},
		Status:          taskdomain.TaskStatusRunning,
		CurrentNodeName: "implement",
		NodeRuns: []taskdomain.NodeRunView{
			{NodeRun: taskdomain.NodeRun{ID: "run-1", TaskID: "task-1", NodeName: "implement", Status: taskdomain.NodeRunFailed, StartedAt: time.Now().UTC(), CompletedAt: timePtr(time.Now().UTC())}},
			{NodeRun: taskdomain.NodeRun{ID: "run-2", TaskID: "task-1", NodeName: "implement", Status: taskdomain.NodeRunRunning, StartedAt: time.Now().UTC()}},
		},
	}
	model.activeTaskID = "task-1"
	model.screen = ScreenRunning
	model.syncComponents()

	next, _ = model.Update(taskruntime.RunEvent{
		Type:      taskruntime.EventNodeFailed,
		TaskID:    "task-1",
		NodeRunID: "run-1",
		NodeName:  "implement",
		TaskView:  model.current,
		Error:     &taskruntime.RunError{Message: "executor failed"},
	})
	model = next.(Model)

	assert.Equal(t, ScreenRunning, model.screen)
	assert.Equal(t, "executor failed", model.errorText)
}

func TestCommandErrorDoesNotForceFailedScreen(t *testing.T) {
	service := &fakeService{events: make(chan taskruntime.RunEvent, 8)}
	model := NewModel(service, "/tmp/project", "", nil, "v0.1.0")
	next, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 32})
	model = next.(Model)
	model.current = &taskdomain.TaskView{Task: taskdomain.Task{ID: "task-1", Description: "Running task"}}
	model.activeTaskID = "task-1"
	model.screen = ScreenRunning

	next, _ = model.Update(taskruntime.RunEvent{
		Type:   taskruntime.EventCommandError,
		TaskID: "task-1",
		Error:  &taskruntime.RunError{Message: "cannot continue blocked step"},
	})
	model = next.(Model)

	assert.Equal(t, ScreenRunning, model.screen)
	assert.Equal(t, "cannot continue blocked step", model.errorText)
}

func TestStartTaskCommandErrorStillFollowsAfterTaskCreated(t *testing.T) {
	service := &fakeService{events: make(chan taskruntime.RunEvent, 8)}
	model := NewModel(service, "/tmp/project", "", nil, "v0.1.0")
	next, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 32})
	model = next.(Model)
	model.editor.SetValue("Implement login")

	cmd := model.submitNewTask()
	require.NotNil(t, cmd)
	require.Nil(t, cmd())
	require.NotNil(t, model.pendingRuntimeCmd)
	assert.Equal(t, pendingRuntimeCommandStartTask, model.pendingRuntimeCmd.kind)
	assert.Equal(t, ScreenRunning, model.screen)

	createdView := taskdomain.TaskView{
		Task:            taskdomain.Task{ID: "task-1", Description: "Implement login", WorkDir: "/tmp/project"},
		Status:          taskdomain.TaskStatusRunning,
		CurrentNodeName: "implement",
	}
	next, _ = model.Update(taskruntime.RunEvent{
		Type:     taskruntime.EventTaskCreated,
		TaskID:   "task-1",
		TaskView: &createdView,
	})
	model = next.(Model)

	require.NotNil(t, model.pendingRuntimeCmd)
	assert.Equal(t, "task-1", model.pendingRuntimeCmd.taskID)
	assert.Equal(t, "task-1", model.activeTaskID)

	next, _ = model.Update(taskruntime.RunEvent{
		Type:  taskruntime.EventCommandError,
		Error: &taskruntime.RunError{Message: "executor bootstrap failed"},
	})
	model = next.(Model)

	assert.Equal(t, ScreenRunning, model.screen)
	assert.Equal(t, "executor bootstrap failed", model.errorText)
	assert.Nil(t, model.pendingRuntimeCmd)
}

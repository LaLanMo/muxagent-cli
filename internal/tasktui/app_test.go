package tasktui

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	appconfig "github.com/LaLanMo/muxagent-cli/internal/config"
	"github.com/LaLanMo/muxagent-cli/internal/taskconfig"
	"github.com/LaLanMo/muxagent-cli/internal/taskdomain"
	"github.com/LaLanMo/muxagent-cli/internal/taskruntime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestModelRendersTaskListAndNewTaskModal(t *testing.T) {
	service := &fakeService{
		events: make(chan taskruntime.RunEvent, 8),
	}
	model := NewModel(service, "/tmp/project", "", nil, "v0.1.0")
	next, _ := model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	model = next.(Model)

	next, _ = model.Update(tasksLoadedMsg{
		tasks: []taskdomain.TaskView{
			{Task: taskdomain.Task{ID: "task-1", Description: "Implement login"}, Status: taskdomain.TaskStatusRunning},
		},
	})
	model = next.(Model)
	view := model.View()
	assert.True(t, view.AltScreen)
	assert.Contains(t, strippedView(view.Content), "muxagent")
	assert.Contains(t, strippedView(view.Content), "Ctrl+N new task")

	next, _ = model.Update(tea.KeyPressMsg{Code: 'n', Mod: tea.ModCtrl})
	model = next.(Model)
	view = model.View()
	assert.Contains(t, strippedView(view.Content), "New Task")
	assert.Contains(t, strippedView(view.Content), "runtime codex")
	assert.Contains(t, strippedView(view.Content), "Enter submit")
}

func TestModelSubmitsNewTaskCommand(t *testing.T) {
	service := &fakeService{
		events: make(chan taskruntime.RunEvent, 8),
	}
	model := NewModel(service, "/tmp/project", "./taskflow.yaml", &taskconfig.Config{Runtime: appconfig.RuntimeClaudeCode}, "v0.1.0")
	next, _ := model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	model = next.(Model)
	next, _ = model.Update(tea.KeyPressMsg{Code: 'n', Mod: tea.ModCtrl})
	model = next.(Model)

	model = typeText(t, model, "Implement login")
	require.Contains(t, strippedView(model.View().Content), "Implement login")

	next, cmd := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = next.(Model)
	require.NotNil(t, cmd)
	require.Nil(t, cmd())
	require.Len(t, service.dispatched, 1)
	assert.Equal(t, taskruntime.CommandStartTask, service.dispatched[0].Type)
	assert.Equal(t, "./taskflow.yaml", service.dispatched[0].ConfigPath)
	assert.Equal(t, appconfig.RuntimeClaudeCode, service.dispatched[0].Runtime)
	assert.Equal(t, ScreenRunning, model.screen)
}

func TestNewTaskTextAreaGrowsOnCtrlJAndPreservesFirstLine(t *testing.T) {
	service := &fakeService{events: make(chan taskruntime.RunEvent, 8)}
	model := NewModel(service, "/tmp/project", "", nil, "v0.1.0")
	next, _ := model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	model = next.(Model)

	// Open new task modal — must execute focus cmd
	next, cmd := model.Update(tea.KeyPressMsg{Code: 'n', Mod: tea.ModCtrl})
	model = next.(Model)
	assert.Equal(t, ScreenNewTask, model.screen)
	if cmd != nil {
		msg := cmd()
		if msg != nil {
			next, _ = model.Update(msg)
			model = next.(Model)
		}
	}

	// Verify textarea is focused
	require.True(t, model.newTaskInput.Focused(), "textarea must be focused")

	// Type first line
	model = typeText(t, model, "first line")
	assert.Equal(t, "first line", model.newTaskInput.Value())

	// Press Ctrl+J to insert newline
	next, _ = model.Update(tea.KeyPressMsg{Code: 'j', Mod: tea.ModCtrl})
	model = next.(Model)
	assert.Equal(t, 2, model.newTaskInput.LineCount(), "should have 2 lines after ctrl+j")

	// Type second line
	model = typeText(t, model, "second line")

	// Both lines must be visible — first line should NOT disappear
	view := strippedView(model.View().Content)
	assert.Contains(t, view, "first line", "first line must remain visible after Ctrl+J")
	assert.Contains(t, view, "second line", "second line must be visible")

	// Textarea height should have grown
	assert.GreaterOrEqual(t, model.newTaskInput.Height(), 2)

	// Submit should include both lines
	next, cmd = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = next.(Model)
	require.NotNil(t, cmd)
	cmd()
	require.Len(t, service.dispatched, 1)
	assert.Contains(t, service.dispatched[0].Description, "first line")
	assert.Contains(t, service.dispatched[0].Description, "second line")
}

func TestNewTaskTextAreaShrinksOnLineDeletion(t *testing.T) {
	service := &fakeService{events: make(chan taskruntime.RunEvent, 8)}
	model := NewModel(service, "/tmp/project", "", nil, "v0.1.0")
	next, _ := model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	model = next.(Model)

	next, _ = model.Update(tea.KeyPressMsg{Code: 'n', Mod: tea.ModCtrl})
	model = next.(Model)

	model = typeText(t, model, "line one")
	next, _ = model.Update(tea.KeyPressMsg{Code: 'j', Mod: tea.ModCtrl})
	model = next.(Model)
	model = typeText(t, model, "line two")
	assert.GreaterOrEqual(t, model.newTaskInput.Height(), 2)

	// Delete "line two" by pressing backspace for each char + the newline
	for range len("line two") + 1 {
		next, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
		model = next.(Model)
	}

	// Should shrink back to 1 line
	assert.Equal(t, 1, model.newTaskInput.Height())
}

func TestNewTaskTextAreaAllowsUnlimitedLinesWithMaxVisual10(t *testing.T) {
	service := &fakeService{events: make(chan taskruntime.RunEvent, 8)}
	model := NewModel(service, "/tmp/project", "", nil, "v0.1.0")
	next, _ := model.Update(tea.WindowSizeMsg{Width: 80, Height: 40})
	model = next.(Model)

	// Open modal and focus
	next, cmd := model.Update(tea.KeyPressMsg{Code: 'n', Mod: tea.ModCtrl})
	model = next.(Model)
	if cmd != nil {
		if msg := cmd(); msg != nil {
			next, _ = model.Update(msg)
			model = next.(Model)
		}
	}
	require.True(t, model.newTaskInput.Focused())

	// Type 15 lines (more than the 10-line visual cap)
	for i := range 15 {
		if i > 0 {
			next, _ = model.Update(tea.KeyPressMsg{Code: 'j', Mod: tea.ModCtrl})
			model = next.(Model)
		}
		model = typeText(t, model, "line")
	}

	assert.Equal(t, 15, model.newTaskInput.LineCount(), "should allow >10 lines")
	assert.Equal(t, 10, model.newTaskInput.Height(), "visual height capped at 10")

	// Delete lines back to 10 — scroll offset should reset to 0
	for model.newTaskInput.LineCount() > 10 {
		next, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
		model = next.(Model)
	}
	assert.Equal(t, 10, model.newTaskInput.LineCount())
	assert.Equal(t, 10, model.newTaskInput.Height())
	assert.Equal(t, 0, model.newTaskInput.ScrollYOffset(),
		"scroll offset must reset when lines <= height")

	// Delete down to 5 lines — height should shrink
	for model.newTaskInput.LineCount() > 5 {
		next, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
		model = next.(Model)
	}
	assert.Equal(t, 5, model.newTaskInput.LineCount())
	assert.Equal(t, 5, model.newTaskInput.Height())
}

func TestModelOpensAwaitingTaskIntoApprovalScreen(t *testing.T) {
	view := taskdomain.TaskView{
		Task:            taskdomain.Task{ID: "task-1", Description: "Implement login"},
		Status:          taskdomain.TaskStatusAwaitingUser,
		CurrentNodeName: "approve_plan",
		NodeRuns: []taskdomain.NodeRunView{
			{NodeRun: taskdomain.NodeRun{ID: "run-1", TaskID: "task-1", NodeName: "approve_plan", Status: taskdomain.NodeRunAwaitingUser}},
		},
	}
	service := &fakeService{
		events: make(chan taskruntime.RunEvent, 8),
		tasks:  []taskdomain.TaskView{view},
		openViews: map[string]taskdomain.TaskView{
			"task-1": view,
		},
		inputs: map[string]*taskruntime.InputRequest{
			"run-1": {
				Kind:          taskruntime.InputKindHumanNode,
				TaskID:        "task-1",
				NodeRunID:     "run-1",
				NodeName:      "approve_plan",
				ArtifactPaths: []string{"/tmp/plan.md"},
			},
		},
	}
	model := NewModel(service, "/tmp/project", "", nil, "v0.1.0")
	model.tasks = service.tasks
	next, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 32})
	model = next.(Model)

	next, cmd := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = next.(Model)
	msg := cmd()
	next, _ = model.Update(msg)
	model = next.(Model)

	assert.Equal(t, ScreenApproval, model.screen)
	assert.Contains(t, strippedView(model.View().Content), "Approve this plan?")
	assert.Contains(t, strippedView(model.View().Content), "plan.md")
}

func TestModelHandlesClarificationEvent(t *testing.T) {
	service := &fakeService{
		events: make(chan taskruntime.RunEvent, 8),
	}
	model := NewModel(service, "/tmp/project", "", nil, "v0.1.0")
	next, _ := model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	model = next.(Model)
	model.activeTaskID = "task-1"
	model.current = &taskdomain.TaskView{
		Task:   taskdomain.Task{ID: "task-1", Description: "Implement login"},
		Status: taskdomain.TaskStatusRunning,
	}
	model.screen = ScreenRunning
	event := taskruntime.RunEvent{
		Type:      taskruntime.EventInputRequested,
		TaskID:    "task-1",
		NodeRunID: "run-1",
		NodeName:  "upsert_plan",
		TaskView: &taskdomain.TaskView{
			Task:   taskdomain.Task{ID: "task-1", Description: "Implement login"},
			Status: taskdomain.TaskStatusAwaitingUser,
		},
		InputRequest: &taskruntime.InputRequest{
			Kind:      taskruntime.InputKindClarification,
			TaskID:    "task-1",
			NodeRunID: "run-1",
			NodeName:  "upsert_plan",
			Questions: []taskdomain.ClarificationQuestion{
				{
					Question:     "What should we do?",
					WhyItMatters: "Need direction",
					Options: []taskdomain.ClarificationOption{
						{Label: "A", Description: "Option A"},
						{Label: "B", Description: "Option B"},
					},
				},
			},
		},
	}

	next, _ = model.Update(event)
	model = next.(Model)
	assert.Equal(t, ScreenClarification, model.screen)
	assert.Contains(t, strippedView(model.View().Content), "Other")
	assert.Contains(t, strippedView(model.View().Content), "Need direction")
}

func TestClarificationMultiSelectSubmitsArrayAnswers(t *testing.T) {
	service := &fakeService{events: make(chan taskruntime.RunEvent, 8)}
	model := NewModel(service, "/tmp/project", "", nil, "v0.1.0")
	next, _ := model.Update(tea.WindowSizeMsg{Width: 100, Height: 28})
	model = next.(Model)
	model.current = &taskdomain.TaskView{
		Task:   taskdomain.Task{ID: "task-1", Description: "Implement login"},
		Status: taskdomain.TaskStatusAwaitingUser,
		NodeRuns: []taskdomain.NodeRunView{
			{NodeRun: taskdomain.NodeRun{ID: "run-1", TaskID: "task-1", NodeName: "upsert_plan", Status: taskdomain.NodeRunAwaitingUser}},
		},
	}
	model.activeTaskID = "task-1"
	model.currentInput = &taskruntime.InputRequest{
		Kind:      taskruntime.InputKindClarification,
		TaskID:    "task-1",
		NodeRunID: "run-1",
		NodeName:  "upsert_plan",
		Questions: []taskdomain.ClarificationQuestion{
			{
				Question:     "Which outputs should we include?",
				WhyItMatters: "The final plan depends on the selected outputs.",
				MultiSelect:  true,
				Options: []taskdomain.ClarificationOption{
					{Label: "API", Description: "Add an API spec"},
					{Label: "UI", Description: "Add a UI mock"},
				},
			},
		},
	}
	model.screen = ScreenClarification
	model.syncComponents()

	next, cmd := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = next.(Model)
	assert.Nil(t, cmd, "toggling a multi-select option should not submit immediately")
	assert.Empty(t, service.dispatched)
	assert.Contains(t, strippedView(model.View().Content), "[x] API")

	for range 2 {
		next, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyDown})
		model = next.(Model)
	}
	next, cmd = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = next.(Model)
	require.NotNil(t, cmd)
	require.Nil(t, cmd())
	require.Len(t, service.dispatched, 1)
	assert.True(t, model.submittingInput)
	assert.Equal(t, ScreenClarification, model.screen)
	assert.Equal(t, taskruntime.CommandSubmitInput, service.dispatched[0].Type)
	assert.Equal(t, "run-1", service.dispatched[0].NodeRunID)
	assert.Equal(t, map[string]interface{}{
		"answers": []interface{}{
			map[string]interface{}{"selected": []string{"API"}},
		},
	}, service.dispatched[0].Payload)
}

func TestClarificationSelectingOtherImmediatelyShowsInput(t *testing.T) {
	tests := []struct {
		name        string
		question    taskdomain.ClarificationQuestion
		downPresses int
	}{
		{
			name: "single select",
			question: taskdomain.ClarificationQuestion{
				Question:     "What should we do?",
				WhyItMatters: "Need direction",
				Options: []taskdomain.ClarificationOption{
					{Label: "A", Description: "Option A"},
					{Label: "B", Description: "Option B"},
				},
			},
			downPresses: 2,
		},
		{
			name: "multi select",
			question: taskdomain.ClarificationQuestion{
				Question:     "Which outputs should we include?",
				WhyItMatters: "Need direction",
				MultiSelect:  true,
				Options: []taskdomain.ClarificationOption{
					{Label: "API", Description: "Option A"},
					{Label: "UI", Description: "Option B"},
				},
			},
			downPresses: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := &fakeService{events: make(chan taskruntime.RunEvent, 8)}
			model := NewModel(service, "/tmp/project", "", nil, "v0.1.0")
			next, _ := model.Update(tea.WindowSizeMsg{Width: 100, Height: 28})
			model = next.(Model)
			model.current = &taskdomain.TaskView{
				Task:   taskdomain.Task{ID: "task-1", Description: "Implement login"},
				Status: taskdomain.TaskStatusAwaitingUser,
				NodeRuns: []taskdomain.NodeRunView{
					{NodeRun: taskdomain.NodeRun{ID: "run-1", TaskID: "task-1", NodeName: "upsert_plan", Status: taskdomain.NodeRunAwaitingUser}},
				},
			}
			model.activeTaskID = "task-1"
			model.currentInput = &taskruntime.InputRequest{
				Kind:      taskruntime.InputKindClarification,
				TaskID:    "task-1",
				NodeRunID: "run-1",
				NodeName:  "upsert_plan",
				Questions: []taskdomain.ClarificationQuestion{tt.question},
			}
			model.screen = ScreenClarification
			model.syncComponents()

			for range tt.downPresses {
				next, cmd := model.Update(tea.KeyPressMsg{Code: tea.KeyDown})
				model = next.(Model)
				if cmd != nil {
					if msg := cmd(); msg != nil {
						next, _ = model.Update(msg)
						model = next.(Model)
					}
				}
			}

			assert.True(t, model.clarificationOther)
			assert.Empty(t, service.dispatched)
			assert.Equal(t, "Write your own answer…", model.detailInput.Placeholder)
			view := strippedView(model.View().Content)
			assert.Contains(t, view, "Other")
			assert.Contains(t, view, "Write your own answer")
		})
	}
}

func TestClarificationCommandErrorKeepsInputVisibleAndShowsError(t *testing.T) {
	service := &fakeService{events: make(chan taskruntime.RunEvent, 8)}
	model := NewModel(service, "/tmp/project", "", nil, "v0.1.0")
	next, _ := model.Update(tea.WindowSizeMsg{Width: 100, Height: 28})
	model = next.(Model)
	model.current = &taskdomain.TaskView{
		Task:   taskdomain.Task{ID: "task-1", Description: "Implement login"},
		Status: taskdomain.TaskStatusAwaitingUser,
		NodeRuns: []taskdomain.NodeRunView{
			{NodeRun: taskdomain.NodeRun{ID: "run-1", TaskID: "task-1", NodeName: "upsert_plan", Status: taskdomain.NodeRunAwaitingUser}},
		},
	}
	model.activeTaskID = "task-1"
	model.currentInput = &taskruntime.InputRequest{
		Kind:      taskruntime.InputKindClarification,
		TaskID:    "task-1",
		NodeRunID: "run-1",
		NodeName:  "upsert_plan",
		Questions: []taskdomain.ClarificationQuestion{
			{
				Question:     "What should we do?",
				WhyItMatters: "Need direction",
				Options: []taskdomain.ClarificationOption{
					{Label: "A", Description: "Option A"},
					{Label: "B", Description: "Option B"},
				},
			},
		},
	}
	model.screen = ScreenClarification
	model.syncComponents()

	next, cmd := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = next.(Model)
	require.NotNil(t, cmd)
	require.Nil(t, cmd())
	assert.True(t, model.submittingInput)
	assert.Equal(t, ScreenClarification, model.screen)
	require.NotNil(t, model.currentInput)

	next, _ = model.Update(taskruntime.RunEvent{
		Type:   taskruntime.EventCommandError,
		TaskID: "task-1",
		Error:  &taskruntime.RunError{Message: "clarification answer 0: must be an array for multi-select questions"},
	})
	model = next.(Model)

	assert.False(t, model.submittingInput)
	assert.Equal(t, ScreenClarification, model.screen)
	require.NotNil(t, model.currentInput)
	view := strippedView(model.View().Content)
	assert.Contains(t, view, "What should we do?")
	assert.Contains(t, view, "clarification answer 0: must be an array for multi-select questions")
}

func TestSubmittingClarificationIgnoresUnrelatedNodeProgress(t *testing.T) {
	model := NewModel(&fakeService{events: make(chan taskruntime.RunEvent, 8)}, "/tmp/project", "", nil, "v0.1.0")
	next, _ := model.Update(tea.WindowSizeMsg{Width: 100, Height: 28})
	model = next.(Model)
	model.current = &taskdomain.TaskView{
		Task:   taskdomain.Task{ID: "task-1", Description: "Implement login"},
		Status: taskdomain.TaskStatusAwaitingUser,
		NodeRuns: []taskdomain.NodeRunView{
			{NodeRun: taskdomain.NodeRun{ID: "run-1", TaskID: "task-1", NodeName: "upsert_plan", Status: taskdomain.NodeRunAwaitingUser}},
			{NodeRun: taskdomain.NodeRun{ID: "run-2", TaskID: "task-1", NodeName: "verify", Status: taskdomain.NodeRunRunning}},
		},
	}
	model.activeTaskID = "task-1"
	model.currentInput = &taskruntime.InputRequest{
		Kind:      taskruntime.InputKindClarification,
		TaskID:    "task-1",
		NodeRunID: "run-1",
		NodeName:  "upsert_plan",
		Questions: []taskdomain.ClarificationQuestion{{Question: "What should we do?"}},
	}
	model.screen = ScreenClarification
	model.submittingInput = true
	model.syncComponents()

	next, _ = model.Update(taskruntime.RunEvent{
		Type:      taskruntime.EventNodeProgress,
		TaskID:    "task-1",
		NodeRunID: "run-2",
		NodeName:  "verify",
		Progress:  &taskruntime.ProgressInfo{Message: "still running"},
	})
	model = next.(Model)

	assert.True(t, model.submittingInput)
	require.NotNil(t, model.currentInput)
	assert.Equal(t, "run-1", model.currentInput.NodeRunID)
	assert.Equal(t, ScreenClarification, model.screen)
}

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
	awaitingView := taskdomain.TaskView{
		Task:            baseTask,
		Status:          taskdomain.TaskStatusAwaitingUser,
		CurrentNodeName: "approve_plan",
		NodeRuns: []taskdomain.NodeRunView{
			{NodeRun: taskdomain.NodeRun{ID: "run-1", TaskID: "task-1", NodeName: "approve_plan", Status: taskdomain.NodeRunAwaitingUser, StartedAt: time.Now().UTC()}},
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
				view := awaitingView
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
			wantSnips: []string{"awaiting Implement login"},
		},
		{
			name: "clarification requested",
			event: func() taskruntime.RunEvent {
				view := awaitingView
				return taskruntime.RunEvent{
					Type:      taskruntime.EventInputRequested,
					TaskID:    "task-1",
					NodeRunID: "run-1",
					NodeName:  "upsert_plan",
					TaskView:  &view,
					InputRequest: &taskruntime.InputRequest{
						Kind:      taskruntime.InputKindClarification,
						TaskID:    "task-1",
						NodeRunID: "run-1",
						NodeName:  "upsert_plan",
					},
				}
			},
			wantSnips: []string{"awaiting Implement login"},
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
			assert.Contains(t, view, "Ctrl+N new task")
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
	assert.Contains(t, view, "Ctrl+N new task")
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
		next, _ = model.Update(msg)
		model = next.(Model)
	}
	assert.Equal(t, ScreenTaskList, model.screen)
	assert.Empty(t, model.activeTaskID)

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
	assert.Contains(t, view, "Ctrl+N new task")
	assert.NotContains(t, view, "Task completed successfully")
}

func TestDetailScreenShowsLatestFourRunningStreamMessagesAndThreadID(t *testing.T) {
	model := NewModel(&fakeService{events: make(chan taskruntime.RunEvent, 8)}, "/tmp/project", "", nil, "v0.1.0")
	next, _ := model.Update(tea.WindowSizeMsg{Width: 100, Height: 28})
	model = next.(Model)
	now := time.Now().UTC()
	model.current = &taskdomain.TaskView{
		Task:   taskdomain.Task{ID: "task-1", Description: "Implement login"},
		Status: taskdomain.TaskStatusRunning,
		NodeRuns: []taskdomain.NodeRunView{
			{NodeRun: taskdomain.NodeRun{ID: "run-1", TaskID: "task-1", NodeName: "implement", Status: taskdomain.NodeRunRunning, StartedAt: now}},
		},
	}
	model.activeTaskID = "task-1"
	model.screen = ScreenRunning
	model.handleEvent(taskruntime.RunEvent{
		Type:      taskruntime.EventNodeProgress,
		TaskID:    "task-1",
		NodeRunID: "run-1",
		NodeName:  "implement",
		Progress:  &taskruntime.ProgressInfo{SessionID: "thread-123"},
	})
	for _, message := range []string{"stream-one", "stream-two", "stream-three", "stream-four", "stream-five"} {
		model.handleEvent(taskruntime.RunEvent{
			Type:      taskruntime.EventNodeProgress,
			TaskID:    "task-1",
			NodeRunID: "run-1",
			NodeName:  "implement",
			Progress:  &taskruntime.ProgressInfo{Message: message},
		})
	}
	model.syncComponents()

	view := strippedView(model.View().Content)
	assert.Contains(t, view, "▌ ● implement")
	assert.Contains(t, view, "thread: thread-123")
	assert.NotContains(t, view, "stream-one")
	assert.Contains(t, view, "stream-two")
	assert.Contains(t, view, "stream-three")
	assert.Contains(t, view, "stream-four")
	assert.Contains(t, view, "stream-five")
}

func TestNodeProgressDoesNotReopenCollapsedArtifacts(t *testing.T) {
	tempDir := t.TempDir()
	artifactPath := filepath.Join(tempDir, "summary.md")
	require.NoError(t, os.WriteFile(artifactPath, []byte("# Summary\n"), 0o644))

	model := NewModel(&fakeService{events: make(chan taskruntime.RunEvent, 8)}, tempDir, "", nil, "v0.1.0")
	next, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 32})
	model = next.(Model)
	now := time.Now().UTC()
	model.current = &taskdomain.TaskView{
		Task:          taskdomain.Task{ID: "task-1", Description: "Implement login", WorkDir: tempDir},
		Status:        taskdomain.TaskStatusRunning,
		ArtifactPaths: []string{artifactPath},
		NodeRuns: []taskdomain.NodeRunView{
			{NodeRun: taskdomain.NodeRun{ID: "run-1", TaskID: "task-1", NodeName: "implement", Status: taskdomain.NodeRunRunning, StartedAt: now}},
		},
	}
	model.activeTaskID = "task-1"
	model.screen = ScreenRunning
	model.artifactCollapsed = true
	model.syncComponents()

	next, _ = model.Update(taskruntime.RunEvent{
		Type:      taskruntime.EventNodeProgress,
		TaskID:    "task-1",
		NodeRunID: "run-1",
		NodeName:  "implement",
		Progress:  &taskruntime.ProgressInfo{Message: "stream update"},
	})
	model = next.(Model)

	assert.True(t, model.artifactCollapsed)
	view := strippedView(model.View().Content)
	assert.Contains(t, view, "Tab expand artifacts")
	assert.NotContains(t, view, "Files")
}

func TestProgressLinesTruncateLongMessagesInsteadOfWrapping(t *testing.T) {
	lines := progressLines([]string{
		`{"type":"item.updated","message":"` + strings.Repeat("artifact stream ", 12) + `"}`,
	}, 18)

	require.Len(t, lines, 1)
	stripped := ansi.Strip(lines[0])
	assert.NotContains(t, stripped, "\n")
	assert.Contains(t, stripped, "…")
}

func TestCompletedDetailShowsThreadIDWithoutOldStreamMessages(t *testing.T) {
	model := NewModel(&fakeService{events: make(chan taskruntime.RunEvent, 8)}, "/tmp/project", "", nil, "v0.1.0")
	next, _ := model.Update(tea.WindowSizeMsg{Width: 100, Height: 28})
	model = next.(Model)
	now := time.Now().UTC()
	model.current = &taskdomain.TaskView{
		Task:   taskdomain.Task{ID: "task-1", Description: "Implement login"},
		Status: taskdomain.TaskStatusDone,
		NodeRuns: []taskdomain.NodeRunView{
			{NodeRun: taskdomain.NodeRun{ID: "run-1", TaskID: "task-1", NodeName: "implement", Status: taskdomain.NodeRunDone, SessionID: "thread-123", StartedAt: now, CompletedAt: timePtr(now.Add(time.Minute))}},
		},
	}
	model.screen = ScreenComplete
	model.progressByRun["run-1"] = []string{"old-stream"}
	model.syncComponents()

	view := strippedView(model.View().Content)
	assert.Contains(t, view, "thread: thread-123")
	assert.NotContains(t, view, "old-stream")
}

func TestDetailScreenShowsIterationLabelsForRepeatedNodeRuns(t *testing.T) {
	model := NewModel(&fakeService{events: make(chan taskruntime.RunEvent, 8)}, "/tmp/project", "", nil, "v0.1.0")
	next, _ := model.Update(tea.WindowSizeMsg{Width: 100, Height: 28})
	model = next.(Model)
	now := time.Now().UTC()
	model.current = &taskdomain.TaskView{
		Task:   taskdomain.Task{ID: "task-1", Description: "Implement login"},
		Status: taskdomain.TaskStatusDone,
		NodeRuns: []taskdomain.NodeRunView{
			{NodeRun: taskdomain.NodeRun{ID: "run-1", TaskID: "task-1", NodeName: "upsert_plan", Status: taskdomain.NodeRunDone, StartedAt: now}},
			{NodeRun: taskdomain.NodeRun{ID: "run-2", TaskID: "task-1", NodeName: "review_plan", Status: taskdomain.NodeRunDone, StartedAt: now.Add(time.Minute)}},
			{NodeRun: taskdomain.NodeRun{ID: "run-3", TaskID: "task-1", NodeName: "upsert_plan", Status: taskdomain.NodeRunDone, StartedAt: now.Add(2 * time.Minute)}},
		},
	}
	model.screen = ScreenComplete
	model.syncComponents()

	view := strippedView(model.View().Content)
	assert.Contains(t, view, "✓ upsert_plan")
	assert.Contains(t, view, "✓ upsert_plan (#2)")
	assert.Contains(t, view, "✓ review_plan")
	assert.NotContains(t, view, "review_plan (#")
}

func TestTaskListMetaUsesHashIterationSuffixForRepeatedCurrentNode(t *testing.T) {
	view := taskdomain.TaskView{
		Task:            taskdomain.Task{ID: "task-1", Description: "Implement login"},
		Status:          taskdomain.TaskStatusAwaitingUser,
		CurrentNodeName: "approve_plan",
		NodeRuns: []taskdomain.NodeRunView{
			{NodeRun: taskdomain.NodeRun{ID: "run-1", NodeName: "upsert_plan"}},
			{NodeRun: taskdomain.NodeRun{ID: "run-2", NodeName: "review_plan"}},
			{NodeRun: taskdomain.NodeRun{ID: "run-3", NodeName: "approve_plan"}},
			{NodeRun: taskdomain.NodeRun{ID: "run-4", NodeName: "upsert_plan"}},
			{NodeRun: taskdomain.NodeRun{ID: "run-5", NodeName: "review_plan"}},
			{NodeRun: taskdomain.NodeRun{ID: "run-6", NodeName: "approve_plan"}},
		},
	}

	meta := taskListMeta(view)
	assert.Contains(t, meta, "approve_plan (#2)")
}

func TestTaskListDelegateRendersSelectedRowAsFullWidthBlock(t *testing.T) {
	delegate := taskListDelegate{}
	model := newTaskListModel()
	model.SetSize(48, 8)
	model.SetItems([]list.Item{
		taskListItem{
			view: taskdomain.TaskView{
				Task:   taskdomain.Task{ID: "task-1", Description: "create a hello.txt file in this dir"},
				Status: taskdomain.TaskStatusDone,
			},
		},
	})
	model.Select(0)

	var buf bytes.Buffer
	delegate.Render(&buf, model, 0, model.Items()[0])
	lines := strings.Split(strings.TrimSuffix(ansi.Strip(buf.String()), "\n"), "\n")
	require.Len(t, lines, 2)
	assert.Contains(t, lines[0], "❯ done")
	assert.Equal(t, model.Width(), ansi.StringWidth(lines[0]))
	assert.Equal(t, model.Width(), ansi.StringWidth(lines[1]))
}

func TestTaskListDelegateUsesAwaitingBackgroundOnlyForAwaitingRows(t *testing.T) {
	delegate := taskListDelegate{}

	render := func(view taskdomain.TaskView) string {
		model := newTaskListModel()
		model.SetSize(48, 8)
		model.SetItems([]list.Item{taskListItem{view: view}})
		model.Select(0)
		var buf bytes.Buffer
		delegate.Render(&buf, model, 0, model.Items()[0])
		return buf.String()
	}

	doneRaw := render(taskdomain.TaskView{
		Task:   taskdomain.Task{ID: "task-1", Description: "done task"},
		Status: taskdomain.TaskStatusDone,
	})
	awaitingRaw := render(taskdomain.TaskView{
		Task:   taskdomain.Task{ID: "task-2", Description: "awaiting task"},
		Status: taskdomain.TaskStatusAwaitingUser,
	})

	assert.NotContains(t, doneRaw, "48;2;42;32;0")
	assert.Contains(t, awaitingRaw, "48;2;42;32;0")
	assert.Contains(t, ansi.Strip(doneRaw), "❯ done")
	assert.Contains(t, ansi.Strip(awaitingRaw), "❯ awaiting")
}

func TestTaskListDelegateUsesRunningAccentForRunningTitle(t *testing.T) {
	delegate := taskListDelegate{}
	model := newTaskListModel()
	model.SetSize(64, 8)
	model.SetItems([]list.Item{
		taskListItem{
			view: taskdomain.TaskView{
				Task:   taskdomain.Task{ID: "task-1", Description: "running task"},
				Status: taskdomain.TaskStatusRunning,
			},
		},
	})
	model.Select(0)

	var buf bytes.Buffer
	delegate.Render(&buf, model, 0, model.Items()[0])
	raw := buf.String()

	assert.Contains(t, ansi.Strip(raw), "❯ running running task")
	assert.Contains(t, raw, tuiTheme.runningText.Render("running task"))
}

func TestApprovalScreenShowsExpandedArtifactsPaneAndPreview(t *testing.T) {
	tempDir := t.TempDir()
	planPath := filepath.Join(tempDir, "plan.md")
	apiPath := filepath.Join(tempDir, "api.md")
	require.NoError(t, os.WriteFile(planPath, []byte("# Plan\n\n1. Do the thing\n"), 0o644))
	require.NoError(t, os.WriteFile(apiPath, []byte("# API\n\n- endpoint\n"), 0o644))

	model := NewModel(&fakeService{events: make(chan taskruntime.RunEvent, 8)}, tempDir, "", nil, "v0.1.0")
	next, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 32})
	model = next.(Model)
	model.current = &taskdomain.TaskView{
		Task:   taskdomain.Task{ID: "task-1", Description: "Implement login", WorkDir: tempDir},
		Status: taskdomain.TaskStatusAwaitingUser,
		NodeRuns: []taskdomain.NodeRunView{
			{NodeRun: taskdomain.NodeRun{ID: "run-1", TaskID: "task-1", NodeName: "approve_plan", Status: taskdomain.NodeRunAwaitingUser}},
		},
	}
	model.currentInput = &taskruntime.InputRequest{
		Kind:          taskruntime.InputKindHumanNode,
		TaskID:        "task-1",
		NodeRunID:     "run-1",
		NodeName:      "approve_plan",
		ArtifactPaths: []string{planPath, apiPath},
	}
	model.screen = ScreenApproval
	model.artifactCollapsed = false
	model.syncComponents()

	view := strippedView(model.View().Content)
	assert.Contains(t, view, "Artifacts (2)")
	assert.Contains(t, view, "Files")
	assert.Contains(t, view, "Preview · plan.md")
	assert.Contains(t, view, "Plan")
	assert.NotContains(t, view, "# Plan")
	assert.Contains(t, view, "Review artifacts in the pane")
}

func TestRunningDetailLetsUserSwitchArtifactSelection(t *testing.T) {
	tempDir := t.TempDir()
	oldPath := filepath.Join(tempDir, "old.md")
	newPath := filepath.Join(tempDir, "new.md")
	require.NoError(t, os.WriteFile(oldPath, []byte("# Old\n"), 0o644))
	require.NoError(t, os.WriteFile(newPath, []byte("# New\n"), 0o644))

	model := NewModel(&fakeService{events: make(chan taskruntime.RunEvent, 8)}, tempDir, "", nil, "v0.1.0")
	next, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 32})
	model = next.(Model)
	model.current = &taskdomain.TaskView{
		Task:          taskdomain.Task{ID: "task-1", Description: "Implement login", WorkDir: tempDir},
		Status:        taskdomain.TaskStatusRunning,
		ArtifactPaths: []string{oldPath, newPath},
		NodeRuns: []taskdomain.NodeRunView{
			{NodeRun: taskdomain.NodeRun{ID: "run-1", TaskID: "task-1", NodeName: "implement", Status: taskdomain.NodeRunRunning, StartedAt: time.Now().UTC()}},
		},
	}
	model.screen = ScreenRunning
	model.artifactCollapsed = false
	model.syncComponents()

	view := strippedView(model.View().Content)
	assert.Contains(t, view, "Preview · new.md")
	assert.Contains(t, view, "New")
	assert.NotContains(t, view, "# New")

	next, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyLeft})
	model = next.(Model)
	view = strippedView(model.View().Content)
	assert.Contains(t, view, "Preview · old.md")
	assert.Contains(t, view, "Old")
	assert.NotContains(t, view, "# Old")
}

func TestCompleteScreenDefaultsToCollapsedArtifactRail(t *testing.T) {
	tempDir := t.TempDir()
	artifactPath := filepath.Join(tempDir, "summary.md")
	require.NoError(t, os.WriteFile(artifactPath, []byte("# Summary\n"), 0o644))

	model := NewModel(&fakeService{events: make(chan taskruntime.RunEvent, 8)}, tempDir, "", nil, "v0.1.0")
	next, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 32})
	model = next.(Model)
	model.current = &taskdomain.TaskView{
		Task:          taskdomain.Task{ID: "task-1", Description: "Implement login", WorkDir: tempDir},
		Status:        taskdomain.TaskStatusDone,
		ArtifactPaths: []string{artifactPath},
		NodeRuns: []taskdomain.NodeRunView{
			{NodeRun: taskdomain.NodeRun{ID: "run-1", TaskID: "task-1", NodeName: "verify", Status: taskdomain.NodeRunDone, StartedAt: time.Now().UTC()}},
		},
	}
	model.screen = ScreenComplete
	model.artifactCollapsed = true
	model.syncComponents()

	view := strippedView(model.View().Content)
	assert.Contains(t, view, "Tab expand artifacts")
	assert.NotContains(t, view, "Files")
	assert.NotContains(t, view, "Preview ·")
}

func TestExpandedArtifactPaneUsesMacPreviewHint(t *testing.T) {
	tempDir := t.TempDir()
	artifactPath := filepath.Join(tempDir, "summary.md")
	require.NoError(t, os.WriteFile(artifactPath, []byte("# Summary\n"), 0o644))

	model := NewModel(&fakeService{events: make(chan taskruntime.RunEvent, 8)}, tempDir, "", nil, "v0.1.0")
	next, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 32})
	model = next.(Model)
	model.current = &taskdomain.TaskView{
		Task:          taskdomain.Task{ID: "task-1", Description: "Implement login", WorkDir: tempDir},
		Status:        taskdomain.TaskStatusRunning,
		ArtifactPaths: []string{artifactPath},
		NodeRuns: []taskdomain.NodeRunView{
			{NodeRun: taskdomain.NodeRun{ID: "run-1", TaskID: "task-1", NodeName: "implement", Status: taskdomain.NodeRunRunning, StartedAt: time.Now().UTC()}},
		},
	}
	model.screen = ScreenRunning
	model.artifactCollapsed = false
	model.syncComponents()

	view := strippedView(model.View().Content)
	assert.Contains(t, view, "Ctrl+U/Ctrl+D preview")
	assert.NotContains(t, view, "PgUp")
	assert.NotContains(t, view, "PgDn")
}

func TestArtifactPaneShowsCompactFileWindow(t *testing.T) {
	model := NewModel(&fakeService{events: make(chan taskruntime.RunEvent, 8)}, "/tmp/project", "", nil, "v0.1.0")
	model.artifactItems = []artifactItem{
		{Label: "one"}, {Label: "two"}, {Label: "three"}, {Label: "four"}, {Label: "five"}, {Label: "six"}, {Label: "seven"},
	}
	model.artifactIndex = 3

	lines := model.renderArtifactFileLines(80, artifactVisibleCapacity(len(model.artifactItems)))
	assert.Equal(t, 3, artifactVisibleCapacity(len(model.artifactItems)))
	assert.Len(t, lines, 5)
	assert.Contains(t, ansi.Strip(lines[0]), "earlier file")
	assert.Contains(t, ansi.Strip(lines[len(lines)-1]), "more file")
}

func TestArtifactPaneLabelsFilesWithSourceNodeAndIteration(t *testing.T) {
	tempDir := t.TempDir()
	firstPlan := filepath.Join(tempDir, ".muxagent", "tasks", "task-1", "todo-first.md")
	secondPlan := filepath.Join(tempDir, ".muxagent", "tasks", "task-1", "todo-second.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(firstPlan), 0o755))
	require.NoError(t, os.WriteFile(firstPlan, []byte("# First\n"), 0o644))
	require.NoError(t, os.WriteFile(secondPlan, []byte("# Second\n"), 0o644))

	model := NewModel(&fakeService{events: make(chan taskruntime.RunEvent, 8)}, tempDir, "", nil, "v0.1.0")
	next, _ := model.Update(tea.WindowSizeMsg{Width: 132, Height: 34})
	model = next.(Model)
	model.current = &taskdomain.TaskView{
		Task:          taskdomain.Task{ID: "task-1", Description: "Implement login", WorkDir: tempDir},
		Status:        taskdomain.TaskStatusDone,
		ArtifactPaths: []string{firstPlan, secondPlan},
		NodeRuns: []taskdomain.NodeRunView{
			{NodeRun: taskdomain.NodeRun{ID: "run-1", TaskID: "task-1", NodeName: "upsert_plan", Status: taskdomain.NodeRunDone}, ArtifactPaths: []string{firstPlan}},
			{NodeRun: taskdomain.NodeRun{ID: "run-2", TaskID: "task-1", NodeName: "review_plan", Status: taskdomain.NodeRunDone}},
			{NodeRun: taskdomain.NodeRun{ID: "run-3", TaskID: "task-1", NodeName: "upsert_plan", Status: taskdomain.NodeRunDone}, ArtifactPaths: []string{secondPlan}},
		},
	}
	model.screen = ScreenRunning
	model.artifactCollapsed = false
	model.syncComponents()

	view := strippedView(model.View().Content)
	assert.Contains(t, view, "upsert_plan (#1) · .muxagent/tasks/task-1/todo-first.md")
	assert.Contains(t, view, "upsert_plan (#2) · .muxagent/tasks/task-1/todo-second.md")
	assert.Contains(t, view, "Preview · upsert_plan (#2) · todo-second.md")
}

func TestMarkdownArtifactPreviewRendersMarkdownInsteadOfRawSource(t *testing.T) {
	tempDir := t.TempDir()
	artifactPath := filepath.Join(tempDir, "summary.md")
	require.NoError(t, os.WriteFile(artifactPath, []byte("# Summary\n\n- Ship it\n"), 0o644))

	model := NewModel(&fakeService{events: make(chan taskruntime.RunEvent, 8)}, tempDir, "", nil, "v0.1.0")
	next, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 32})
	model = next.(Model)
	model.current = &taskdomain.TaskView{
		Task:          taskdomain.Task{ID: "task-1", Description: "Implement login", WorkDir: tempDir},
		Status:        taskdomain.TaskStatusRunning,
		ArtifactPaths: []string{artifactPath},
		NodeRuns: []taskdomain.NodeRunView{
			{NodeRun: taskdomain.NodeRun{ID: "run-1", TaskID: "task-1", NodeName: "implement", Status: taskdomain.NodeRunRunning, StartedAt: time.Now().UTC()}},
		},
	}
	model.screen = ScreenRunning
	model.artifactCollapsed = false
	model.syncComponents()

	view := strippedView(model.View().Content)
	assert.Contains(t, view, "Summary")
	assert.Contains(t, view, "Ship it")
	assert.NotContains(t, view, "# Summary")
}

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
	assert.Contains(t, view, "r retry step")

	next, cmd := model.Update(tea.KeyPressMsg{Text: "r", Code: 'r'})
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
	assert.Contains(t, view, "R force retry")
	assert.Contains(t, view, "Retry limit reached")

	next, cmd := model.Update(tea.KeyPressMsg{Text: "R", Code: 'R'})
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
	assert.Contains(t, view, "R force continue")

	next, cmd := model.Update(tea.KeyPressMsg{Text: "R", Code: 'R'})
	model = next.(Model)
	require.NotNil(t, cmd)
	require.Nil(t, cmd())
	require.Len(t, service.dispatched, 1)
	assert.Equal(t, taskruntime.CommandContinueBlocked, service.dispatched[0].Type)
	assert.Equal(t, ScreenRunning, model.screen)
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

func retryTUIConfig(maxIterations int) *taskconfig.Config {
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

func typeText(t *testing.T, model Model, value string) Model {
	t.Helper()
	for _, r := range value {
		next, _ := model.Update(tea.KeyPressMsg{Text: string(r), Code: r})
		model = next.(Model)
	}
	return model
}

func timePtr(ts time.Time) *time.Time {
	return &ts
}

func strippedView(view string) string {
	return ansi.Strip(view)
}

func taskStatusForID(tasks []taskdomain.TaskView, taskID string) taskdomain.TaskStatus {
	for _, task := range tasks {
		if task.Task.ID == taskID {
			return task.Status
		}
	}
	return ""
}

type fakeService struct {
	events     chan taskruntime.RunEvent
	dispatched []taskruntime.RunCommand
	tasks      []taskdomain.TaskView
	openViews  map[string]taskdomain.TaskView
	inputs     map[string]*taskruntime.InputRequest
}

func (f *fakeService) Run(ctx context.Context) error       { return nil }
func (f *fakeService) Events() <-chan taskruntime.RunEvent { return f.events }
func (f *fakeService) Dispatch(cmd taskruntime.RunCommand) { f.dispatched = append(f.dispatched, cmd) }
func (f *fakeService) ListTaskViews(ctx context.Context, workDir string) ([]taskdomain.TaskView, error) {
	return append([]taskdomain.TaskView(nil), f.tasks...), nil
}
func (f *fakeService) LoadTaskView(ctx context.Context, taskID string) (taskdomain.TaskView, *taskconfig.Config, error) {
	return f.openViews[taskID], nil, nil
}
func (f *fakeService) BuildInputRequest(ctx context.Context, taskID, nodeRunID string) (*taskruntime.InputRequest, error) {
	return f.inputs[nodeRunID], nil
}
func (f *fakeService) PrepareShutdown(ctx context.Context) error { return nil }
func (f *fakeService) Close() error                              { return nil }

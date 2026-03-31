package tasktui

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/LaLanMo/muxagent-cli/internal/taskdomain"
	"github.com/LaLanMo/muxagent-cli/internal/taskruntime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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

	model = openFirstTaskFromList(t, model)

	assert.Equal(t, ScreenApproval, model.screen)
	assert.Contains(t, strippedView(model.View().Content), "Approve this plan?")
	assert.Contains(t, strippedView(model.View().Content), "Shift+Tab artifacts")
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
		NodeName:  "draft_plan",
		TaskView: &taskdomain.TaskView{
			Task:   taskdomain.Task{ID: "task-1", Description: "Implement login"},
			Status: taskdomain.TaskStatusAwaitingUser,
		},
		InputRequest: &taskruntime.InputRequest{
			Kind:      taskruntime.InputKindClarification,
			TaskID:    "task-1",
			NodeRunID: "run-1",
			NodeName:  "draft_plan",
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

func TestModelOpensAwaitingTaskIntoClarificationScreen(t *testing.T) {
	view := taskdomain.TaskView{
		Task:            taskdomain.Task{ID: "task-1", Description: "Implement login"},
		Status:          taskdomain.TaskStatusAwaitingUser,
		CurrentNodeName: "draft_plan",
		NodeRuns: []taskdomain.NodeRunView{
			{NodeRun: taskdomain.NodeRun{ID: "run-1", TaskID: "task-1", NodeName: "draft_plan", Status: taskdomain.NodeRunAwaitingUser}},
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
				Kind:      taskruntime.InputKindClarification,
				TaskID:    "task-1",
				NodeRunID: "run-1",
				NodeName:  "draft_plan",
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
		},
	}
	model := NewModel(service, "/tmp/project", "", nil, "v0.1.0")
	model.tasks = service.tasks
	next, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 32})
	model = next.(Model)

	model = openFirstTaskFromList(t, model)

	assert.Equal(t, ScreenClarification, model.screen)
	assert.Equal(t, FocusRegionChoices, model.focusRegion)
	assert.Contains(t, strippedView(model.View().Content), "Need direction")
	assert.Contains(t, strippedView(model.View().Content), "Other")
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
			{NodeRun: taskdomain.NodeRun{ID: "run-1", TaskID: "task-1", NodeName: "draft_plan", Status: taskdomain.NodeRunAwaitingUser}},
		},
	}
	model.activeTaskID = "task-1"
	model.currentInput = &taskruntime.InputRequest{
		Kind:      taskruntime.InputKindClarification,
		TaskID:    "task-1",
		NodeRunID: "run-1",
		NodeName:  "draft_plan",
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

	for range 3 {
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

func TestClarificationMultiSelectOtherSubmitsCustomAnswer(t *testing.T) {
	service := &fakeService{events: make(chan taskruntime.RunEvent, 8)}
	model := NewModel(service, "/tmp/project", "", nil, "v0.1.0")
	next, _ := model.Update(tea.WindowSizeMsg{Width: 100, Height: 28})
	model = next.(Model)
	model.current = &taskdomain.TaskView{
		Task:   taskdomain.Task{ID: "task-1", Description: "Implement login"},
		Status: taskdomain.TaskStatusAwaitingUser,
		NodeRuns: []taskdomain.NodeRunView{
			{NodeRun: taskdomain.NodeRun{ID: "run-1", TaskID: "task-1", NodeName: "draft_plan", Status: taskdomain.NodeRunAwaitingUser}},
		},
	}
	model.activeTaskID = "task-1"
	question := taskdomain.ClarificationQuestion{
		Question:     "Which outputs should we include?",
		WhyItMatters: "The final plan depends on the selected outputs.",
		MultiSelect:  true,
		Options: []taskdomain.ClarificationOption{
			{Label: "API", Description: "Add an API spec"},
			{Label: "UI", Description: "Add a UI mock"},
		},
	}
	model.currentInput = &taskruntime.InputRequest{
		Kind:      taskruntime.InputKindClarification,
		TaskID:    "task-1",
		NodeRunID: "run-1",
		NodeName:  "draft_plan",
		Questions: []taskdomain.ClarificationQuestion{question},
	}
	model.screen = ScreenClarification
	model.syncComponents()

	for range 2 {
		next, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyDown})
		model = next.(Model)
	}
	assert.Equal(t, clarificationOtherRowIndex(question), model.clarification.option)
	model = typeText(t, model, "Docs only")

	next, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	model = next.(Model)
	assert.Equal(t, clarificationContinueRowIndex(question), model.clarification.option)

	next, cmd := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = next.(Model)
	require.NotNil(t, cmd)
	require.Nil(t, cmd())
	require.Len(t, service.dispatched, 1)
	assert.Equal(t, taskruntime.CommandSubmitInput, service.dispatched[0].Type)
	assert.Equal(t, map[string]interface{}{
		"answers": []interface{}{
			map[string]interface{}{"selected": []string{"Docs only"}},
		},
	}, service.dispatched[0].Payload)
}

func TestClarificationMultiQuestionHeaderNavigationAndSubmitGuard(t *testing.T) {
	service := &fakeService{events: make(chan taskruntime.RunEvent, 8)}
	model := NewModel(service, "/tmp/project", "", nil, "v0.1.0")
	next, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 32})
	model = next.(Model)
	model.current = &taskdomain.TaskView{
		Task:   taskdomain.Task{ID: "task-1", Description: "Implement login"},
		Status: taskdomain.TaskStatusAwaitingUser,
		NodeRuns: []taskdomain.NodeRunView{
			{NodeRun: taskdomain.NodeRun{ID: "run-1", TaskID: "task-1", NodeName: "draft_plan", Status: taskdomain.NodeRunAwaitingUser}},
		},
	}
	model.activeTaskID = "task-1"
	model.currentInput = &taskruntime.InputRequest{
		Kind:      taskruntime.InputKindClarification,
		TaskID:    "task-1",
		NodeRunID: "run-1",
		NodeName:  "draft_plan",
		Questions: []taskdomain.ClarificationQuestion{
			{
				Question:     "Which path should we take?",
				WhyItMatters: "The plan changes based on this choice.",
				Options: []taskdomain.ClarificationOption{
					{Label: "A", Description: "Option A"},
					{Label: "B", Description: "Option B"},
				},
			},
			{
				Question:     "Which reviewer should we use?",
				WhyItMatters: "The review style changes the prompt.",
				Options: []taskdomain.ClarificationOption{
					{Label: "Sidekick", Description: "Ship quickly"},
					{Label: "Strict", Description: "Push harder"},
				},
			},
		},
	}
	model.screen = ScreenClarification
	model.focusRegion = FocusRegionChoices
	model.syncComponents()

	view := strippedView(model.View().Content)
	assert.Contains(t, view, "□ Submit")

	next, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	model = next.(Model)
	assert.Equal(t, 1, model.clarification.question)
	assert.Equal(t, 1, model.clarification.headerSelection)

	next, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = next.(Model)
	assert.Equal(t, 1, model.clarification.question)
	assert.Equal(t, "Sidekick", clarificationAnswerAt(model.clarification.answers, 1).Selected)

	next, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	model = next.(Model)
	assert.True(t, model.clarificationSubmitSelected())

	next, cmd := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = next.(Model)
	assert.Nil(t, cmd)
	assert.Empty(t, service.dispatched)
	assert.Equal(t, 0, model.clarification.question)
	assert.Equal(t, "Answer every question before submitting.", model.errorText)

	next, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = next.(Model)
	assert.Equal(t, 1, model.clarification.question, "single-select answers should still auto-advance to the next question")

	view = strippedView(model.View().Content)
	assert.Contains(t, view, "✓ Submit")

	next, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	model = next.(Model)
	assert.True(t, model.clarificationSubmitSelected())

	next, cmd = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = next.(Model)
	require.NotNil(t, cmd)
	require.Nil(t, cmd())
	require.Len(t, service.dispatched, 1)
	assert.Equal(t, map[string]interface{}{
		"answers": []interface{}{
			map[string]interface{}{"selected": "A"},
			map[string]interface{}{"selected": "Sidekick"},
		},
	}, service.dispatched[0].Payload)
}

func TestClarificationOtherRowUsesCtrlPNForQuestionNavigation(t *testing.T) {
	service := &fakeService{events: make(chan taskruntime.RunEvent, 8)}
	model := NewModel(service, "/tmp/project", "", nil, "v0.1.0")
	next, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 32})
	model = next.(Model)
	model.current = &taskdomain.TaskView{
		Task:   taskdomain.Task{ID: "task-1", Description: "Implement login"},
		Status: taskdomain.TaskStatusAwaitingUser,
		NodeRuns: []taskdomain.NodeRunView{
			{NodeRun: taskdomain.NodeRun{ID: "run-1", TaskID: "task-1", NodeName: "draft_plan", Status: taskdomain.NodeRunAwaitingUser}},
		},
	}
	model.activeTaskID = "task-1"
	model.currentInput = &taskruntime.InputRequest{
		Kind:      taskruntime.InputKindClarification,
		TaskID:    "task-1",
		NodeRunID: "run-1",
		NodeName:  "draft_plan",
		Questions: []taskdomain.ClarificationQuestion{
			{
				Question:     "Which path should we take?",
				WhyItMatters: "The plan changes based on this choice.",
				Options: []taskdomain.ClarificationOption{
					{Label: "A", Description: "Option A"},
					{Label: "B", Description: "Option B"},
				},
			},
			{
				Question:     "What should we tell the reviewer?",
				WhyItMatters: "This changes the review guidance.",
				Options: []taskdomain.ClarificationOption{
					{Label: "Strict", Description: "Push harder"},
				},
			},
		},
	}
	model.screen = ScreenClarification
	model.focusRegion = FocusRegionChoices
	model.syncComponents()

	next, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = next.(Model)
	assert.Equal(t, 1, model.clarification.question)

	next, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	model = next.(Model)
	assert.Equal(t, clarificationOtherRowIndex(model.currentInput.Questions[1]), model.clarification.option)

	model = typeText(t, model, "Need docs")
	view := strippedView(model.View().Content)
	assert.Contains(t, view, "Ctrl+P/N questions")

	next, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyLeft})
	model = next.(Model)
	assert.Equal(t, 1, model.clarification.question)
	assert.Equal(t, "Need docs", model.editor.Value())

	next, _ = model.Update(tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl})
	model = next.(Model)
	assert.Equal(t, 0, model.clarification.question)

	next, _ = model.Update(tea.KeyPressMsg{Code: 'n', Mod: tea.ModCtrl})
	model = next.(Model)
	assert.Equal(t, 1, model.clarification.question)
	assert.Equal(t, clarificationOtherRowIndex(model.currentInput.Questions[1]), model.clarification.option)
	assert.Equal(t, "Need docs", model.editor.Value())
}

func TestClarificationOtherInputIsAlwaysVisible(t *testing.T) {
	tests := []struct {
		name     string
		question taskdomain.ClarificationQuestion
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
					{NodeRun: taskdomain.NodeRun{ID: "run-1", TaskID: "task-1", NodeName: "draft_plan", Status: taskdomain.NodeRunAwaitingUser}},
				},
			}
			model.activeTaskID = "task-1"
			model.currentInput = &taskruntime.InputRequest{
				Kind:      taskruntime.InputKindClarification,
				TaskID:    "task-1",
				NodeRunID: "run-1",
				NodeName:  "draft_plan",
				Questions: []taskdomain.ClarificationQuestion{tt.question},
			}
			model.screen = ScreenClarification
			model.syncComponents()

			view := strippedView(model.View().Content)
			assert.Contains(t, view, "Other")
			assert.NotContains(t, view, "Write your own answer")
			assert.Equal(t, FocusRegionChoices, model.focusRegion)

			for range 2 {
				next, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyDown})
				model = next.(Model)
			}
			assert.Equal(t, clarificationOtherRowIndex(tt.question), model.clarification.option)
			view = strippedView(model.View().Content)
			assert.Contains(t, view, "Write your own answer")
			assert.Empty(t, service.dispatched)
			assert.Equal(t, "Write your own answer…", model.editor.Placeholder())
		})
	}
}

func TestClarificationFooterRemainsVisibleWithManyOptionsAndOtherInput(t *testing.T) {
	service := &fakeService{events: make(chan taskruntime.RunEvent, 8)}
	model := NewModel(service, "/tmp/project", "", nil, "v0.1.0")
	next, _ := model.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	model = next.(Model)
	options := make([]taskdomain.ClarificationOption, 0, 10)
	for i := range 10 {
		options = append(options, taskdomain.ClarificationOption{
			Label:       fmt.Sprintf("Option %d", i+1),
			Description: "Needs review",
		})
	}
	model.current = &taskdomain.TaskView{
		Task:   taskdomain.Task{ID: "task-1", Description: "Implement login"},
		Status: taskdomain.TaskStatusAwaitingUser,
		NodeRuns: []taskdomain.NodeRunView{
			{NodeRun: taskdomain.NodeRun{ID: "run-1", TaskID: "task-1", NodeName: "draft_plan", Status: taskdomain.NodeRunAwaitingUser}},
		},
	}
	model.activeTaskID = "task-1"
	model.currentInput = &taskruntime.InputRequest{
		Kind:      taskruntime.InputKindClarification,
		TaskID:    "task-1",
		NodeRunID: "run-1",
		NodeName:  "draft_plan",
		Questions: []taskdomain.ClarificationQuestion{{
			Question:     "What should we do?",
			WhyItMatters: "Need direction",
			Options:      options,
		}},
	}
	model.screen = ScreenClarification
	model.syncComponents()

	view := strippedView(model.View().Content)
	assert.Contains(t, view, "Ctrl+C quit")
	assert.Contains(t, view, "↑↓ select")
	assert.NotContains(t, view, "Write your own answer")

	for range len(options) {
		next, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyDown})
		model = next.(Model)
	}

	view = strippedView(model.View().Content)
	assert.Contains(t, view, "Ctrl+C quit")
	assert.Contains(t, view, "Enter newline")
	assert.Contains(t, view, "Write your own answer")
}

func TestClarificationFooterDoesNotContainQuestionPanel(t *testing.T) {
	service := &fakeService{events: make(chan taskruntime.RunEvent, 8)}
	model := NewModel(service, "/tmp/project", "", nil, "v0.1.0")
	next, _ := model.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	model = next.(Model)
	model.current = &taskdomain.TaskView{
		Task:   taskdomain.Task{ID: "task-1", Description: "Implement login"},
		Status: taskdomain.TaskStatusAwaitingUser,
		NodeRuns: []taskdomain.NodeRunView{
			{NodeRun: taskdomain.NodeRun{ID: "run-1", TaskID: "task-1", NodeName: "draft_plan", Status: taskdomain.NodeRunAwaitingUser}},
		},
	}
	model.activeTaskID = "task-1"
	model.currentInput = &taskruntime.InputRequest{
		Kind:      taskruntime.InputKindClarification,
		TaskID:    "task-1",
		NodeRunID: "run-1",
		NodeName:  "draft_plan",
		Questions: []taskdomain.ClarificationQuestion{{
			Question:     "Which path should we take?",
			WhyItMatters: "The plan changes based on this choice.",
			Options: []taskdomain.ClarificationOption{
				{Label: "A", Description: "Option A"},
				{Label: "B", Description: "Option B"},
			},
		}},
	}
	model.screen = ScreenClarification
	model.syncComponents()

	footer := strippedView(model.renderDetailFooter(surfaceRect{Width: detailContentWidth(96, model.activeDetailTab)}))
	assert.Contains(t, footer, "Ctrl+C quit")
	assert.Contains(t, footer, "↑↓ select")
	assert.NotContains(t, footer, "Question 1/1")
	assert.NotContains(t, footer, "Which path should we take?")

	snapshot := model.computeDetailLayoutSnapshot()
	panel := strippedView(model.renderDetailPanel(model.computeDetailPanelSurface(snapshot.Frame)))
	assert.Contains(t, panel, "Question 1/1")
	assert.Contains(t, panel, "Which path should we take?")
}

func TestClarificationWithoutArtifactsDoesNotRenderArtifactLauncher(t *testing.T) {
	service := &fakeService{events: make(chan taskruntime.RunEvent, 8)}
	model := NewModel(service, "/tmp/project", "", nil, "v0.1.0")
	next, _ := model.Update(tea.WindowSizeMsg{Width: 149, Height: 39})
	model = next.(Model)
	model.current = &taskdomain.TaskView{
		Task:   taskdomain.Task{ID: "task-1", Description: "Need clarification"},
		Status: taskdomain.TaskStatusAwaitingUser,
		NodeRuns: []taskdomain.NodeRunView{
			{NodeRun: taskdomain.NodeRun{ID: "run-1", TaskID: "task-1", NodeName: "draft_plan", Status: taskdomain.NodeRunAwaitingUser}},
		},
	}
	model.activeTaskID = "task-1"
	model.currentInput = &taskruntime.InputRequest{
		Kind:      taskruntime.InputKindClarification,
		TaskID:    "task-1",
		NodeRunID: "run-1",
		NodeName:  "draft_plan",
		Questions: []taskdomain.ClarificationQuestion{{
			Question:     "Which path should we take?",
			WhyItMatters: "The plan changes based on this choice.",
			Options: []taskdomain.ClarificationOption{
				{Label: "A", Description: "Option A"},
				{Label: "B", Description: "Option B"},
			},
		}},
	}
	model.screen = ScreenClarification
	model.syncComponents()

	view := strippedView(model.View().Content)
	assert.Contains(t, view, "Question 1/1")
	assert.Contains(t, view, "Ctrl+C quit")
	assert.NotContains(t, view, "Artifacts (0)")
	assert.Contains(t, view, "Other")

	next, cmd := model.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	model = next.(Model)
	if cmd != nil {
		if msg := cmd(); msg != nil {
			next, _ = model.Update(msg)
			model = next.(Model)
		}
	}

	view = strippedView(model.View().Content)
	assert.Contains(t, view, "Other")
	assert.Contains(t, view, "Ctrl+C quit")
	assert.NotContains(t, view, "Artifacts (0)")
}

func TestClarificationTabReachesVisibleArtifactPane(t *testing.T) {
	tempDir := t.TempDir()
	artifactPath := filepath.Join(tempDir, "plan.md")
	require.NoError(t, os.WriteFile(artifactPath, []byte("# Plan\nhello\n"), 0o644))

	service := &fakeService{events: make(chan taskruntime.RunEvent, 8)}
	model := NewModel(service, tempDir, "", nil, "v0.1.0")
	next, _ := model.Update(tea.WindowSizeMsg{Width: 149, Height: 39})
	model = next.(Model)
	model.current = &taskdomain.TaskView{
		Task:          taskdomain.Task{ID: "task-1", Description: "Need clarification", WorkDir: tempDir},
		Status:        taskdomain.TaskStatusAwaitingUser,
		ArtifactPaths: []string{artifactPath},
		NodeRuns: []taskdomain.NodeRunView{
			{NodeRun: taskdomain.NodeRun{ID: "run-1", TaskID: "task-1", NodeName: "draft_plan", Status: taskdomain.NodeRunDone}, ArtifactPaths: []string{artifactPath}},
			{NodeRun: taskdomain.NodeRun{ID: "run-2", TaskID: "task-1", NodeName: "review_plan", Status: taskdomain.NodeRunAwaitingUser}},
		},
	}
	model.activeTaskID = "task-1"
	model.currentInput = &taskruntime.InputRequest{
		Kind:          taskruntime.InputKindClarification,
		TaskID:        "task-1",
		NodeRunID:     "run-2",
		NodeName:      "review_plan",
		ArtifactPaths: []string{artifactPath},
		Questions: []taskdomain.ClarificationQuestion{{
			Question:     "进入 chat_screen 时，屏幕上显示的具体状态是什么？请在下面选择最接近的一项，如果都不准确就在 Other 里补充。",
			WhyItMatters: "长文本会挤压 clarification 面板，所以这里需要证明 footer 和 artifacts 仍然同时可见。",
			Options: []taskdomain.ClarificationOption{
				{Label: "A", Description: "显示 This session cannot be restored on this device yet."},
				{Label: "B", Description: "显示 Send a message to get started 或者空白。"},
			},
		}},
	}
	model.screen = ScreenClarification
	model.syncComponents()

	view := strippedView(model.View().Content)
	assert.Contains(t, view, "Shift+Tab artifacts")
	assert.Contains(t, view, "Question 1/1")
	assert.Contains(t, view, "Ctrl+C quit")

	// Switch to artifacts tab via Shift+Tab
	next, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	model = next.(Model)

	assert.Equal(t, DetailTabArtifacts, model.activeDetailTab)
	assert.Equal(t, FocusRegionArtifactFiles, model.focusRegion)
	view = strippedView(model.View().Content)
	assert.Contains(t, view, "Shift+Tab timeline")
	assert.Contains(t, view, "Files")
	assert.Contains(t, view, "Ctrl+C quit")
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
			{NodeRun: taskdomain.NodeRun{ID: "run-1", TaskID: "task-1", NodeName: "draft_plan", Status: taskdomain.NodeRunAwaitingUser}},
		},
	}
	model.activeTaskID = "task-1"
	model.currentInput = &taskruntime.InputRequest{
		Kind:      taskruntime.InputKindClarification,
		TaskID:    "task-1",
		NodeRunID: "run-1",
		NodeName:  "draft_plan",
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

func TestClarificationNodeStartedClearsSubmittedInputAndShowsRunningState(t *testing.T) {
	model := NewModel(&fakeService{events: make(chan taskruntime.RunEvent, 8)}, "/tmp/project", "", nil, "v0.1.0")
	next, _ := model.Update(tea.WindowSizeMsg{Width: 100, Height: 28})
	model = next.(Model)
	model.current = &taskdomain.TaskView{
		Task:            taskdomain.Task{ID: "task-1", Description: "Implement login"},
		Status:          taskdomain.TaskStatusAwaitingUser,
		CurrentNodeName: "draft_plan",
		NodeRuns: []taskdomain.NodeRunView{
			{NodeRun: taskdomain.NodeRun{ID: "run-1", TaskID: "task-1", NodeName: "draft_plan", Status: taskdomain.NodeRunAwaitingUser}},
		},
	}
	model.activeTaskID = "task-1"
	model.currentInput = &taskruntime.InputRequest{
		Kind:      taskruntime.InputKindClarification,
		TaskID:    "task-1",
		NodeRunID: "run-1",
		NodeName:  "draft_plan",
		Questions: []taskdomain.ClarificationQuestion{{Question: "What should we do?"}},
	}
	model.screen = ScreenClarification
	model.submittingInput = true
	model.syncComponents()

	runningView := taskdomain.TaskView{
		Task:            taskdomain.Task{ID: "task-1", Description: "Implement login"},
		Status:          taskdomain.TaskStatusRunning,
		CurrentNodeName: "draft_plan",
		NodeRuns: []taskdomain.NodeRunView{
			{NodeRun: taskdomain.NodeRun{ID: "run-1", TaskID: "task-1", NodeName: "draft_plan", Status: taskdomain.NodeRunRunning}},
		},
	}
	next, _ = model.Update(taskruntime.RunEvent{
		Type:      taskruntime.EventNodeStarted,
		TaskID:    "task-1",
		NodeRunID: "run-1",
		NodeName:  "draft_plan",
		TaskView:  &runningView,
	})
	model = next.(Model)

	assert.False(t, model.submittingInput)
	assert.Nil(t, model.currentInput)
	assert.Equal(t, ScreenRunning, model.screen)
	require.NotNil(t, model.current)
	assert.Equal(t, taskdomain.TaskStatusRunning, model.current.Status)
}

func TestSubmittingClarificationIgnoresUnrelatedNodeProgress(t *testing.T) {
	model := NewModel(&fakeService{events: make(chan taskruntime.RunEvent, 8)}, "/tmp/project", "", nil, "v0.1.0")
	next, _ := model.Update(tea.WindowSizeMsg{Width: 100, Height: 28})
	model = next.(Model)
	model.current = &taskdomain.TaskView{
		Task:   taskdomain.Task{ID: "task-1", Description: "Implement login"},
		Status: taskdomain.TaskStatusAwaitingUser,
		NodeRuns: []taskdomain.NodeRunView{
			{NodeRun: taskdomain.NodeRun{ID: "run-1", TaskID: "task-1", NodeName: "draft_plan", Status: taskdomain.NodeRunAwaitingUser}},
			{NodeRun: taskdomain.NodeRun{ID: "run-2", TaskID: "task-1", NodeName: "verify", Status: taskdomain.NodeRunRunning}},
		},
	}
	model.activeTaskID = "task-1"
	model.currentInput = &taskruntime.InputRequest{
		Kind:      taskruntime.InputKindClarification,
		TaskID:    "task-1",
		NodeRunID: "run-1",
		NodeName:  "draft_plan",
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

func TestFocusedFeedbackRowTakesPriorityOverArtifactPaneKeys(t *testing.T) {
	tempDir := t.TempDir()
	artifactPath := filepath.Join(tempDir, "plan.md")
	require.NoError(t, os.WriteFile(artifactPath, []byte("# Plan\n"), 0o644))

	service := &fakeService{events: make(chan taskruntime.RunEvent, 8)}
	model := NewModel(service, tempDir, "", nil, "v0.1.0")
	next, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 32})
	model = next.(Model)

	model.screen = ScreenApproval
	model.approval.choice = approvalRowFeedback
	model.current = &taskdomain.TaskView{
		Task:   taskdomain.Task{ID: "task-1", Description: "Implement login", WorkDir: tempDir},
		Status: taskdomain.TaskStatusAwaitingUser,
	}
	model.currentInput = &taskruntime.InputRequest{
		Kind:          taskruntime.InputKindHumanNode,
		TaskID:        "task-1",
		NodeRunID:     "run-1",
		NodeName:      "approve_plan",
		ArtifactPaths: []string{artifactPath},
	}
	model.focusRegion = FocusRegionActionPanel
	model.activeTaskID = "task-1"
	model.syncComponents()
	_ = model.syncInputFocus()
	model.editor.SetValue("Need more detail")

	next, _ = model.Update(tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl})
	model = next.(Model)

	assert.Equal(t, "", model.editor.Value(), "ctrl+u should reach the focused feedback row instead of the artifact pane")
	assert.NotContains(t, strippedView(model.View().Content), "Ctrl+U/Ctrl+D preview")
}

func TestFocusedResponseEditorShiftTabTogglesDetailTabs(t *testing.T) {
	tempDir := t.TempDir()
	artifactPath := filepath.Join(tempDir, "plan.md")
	require.NoError(t, os.WriteFile(artifactPath, []byte("# Plan\n"), 0o644))

	tests := []struct {
		name           string
		nodeName       string
		screen         Screen
		setup          func(*Model)
		wantEditorText string
	}{
		{
			name:     "approval feedback row",
			nodeName: "approve_plan",
			screen:   ScreenApproval,
			setup: func(model *Model) {
				model.approval.choice = approvalRowFeedback
				model.currentInput = &taskruntime.InputRequest{
					Kind:          taskruntime.InputKindHumanNode,
					TaskID:        "task-1",
					NodeRunID:     "run-1",
					NodeName:      "approve_plan",
					ArtifactPaths: []string{artifactPath},
				}
			},
			wantEditorText: "Need more detail",
		},
		{
			name:     "clarification other row",
			nodeName: "implement",
			screen:   ScreenClarification,
			setup: func(model *Model) {
				model.currentInput = &taskruntime.InputRequest{
					Kind:          taskruntime.InputKindClarification,
					TaskID:        "task-1",
					NodeRunID:     "run-1",
					NodeName:      "implement",
					ArtifactPaths: []string{artifactPath},
					Questions: []taskdomain.ClarificationQuestion{{
						Question:     "Which path should we take?",
						WhyItMatters: "Need the answer before implementing.",
						Options: []taskdomain.ClarificationOption{
							{Label: "A", Description: "First path"},
							{Label: "B", Description: "Second path"},
						},
					}},
				}
				model.clarification.option = 2
				model.clarification.other = map[int]bool{0: true}
			},
			wantEditorText: "Custom answer",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := &fakeService{events: make(chan taskruntime.RunEvent, 8)}
			model := NewModel(service, tempDir, "", nil, "v0.1.0")
			next, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 32})
			model = next.(Model)
			model.current = &taskdomain.TaskView{
				Task:          taskdomain.Task{ID: "task-1", Description: "Implement login", WorkDir: tempDir},
				Status:        taskdomain.TaskStatusAwaitingUser,
				ArtifactPaths: []string{artifactPath},
				NodeRuns: []taskdomain.NodeRunView{
					{NodeRun: taskdomain.NodeRun{ID: "run-1", TaskID: "task-1", NodeName: tt.nodeName, Status: taskdomain.NodeRunAwaitingUser}, ArtifactPaths: []string{artifactPath}},
				},
			}
			model.screen = tt.screen
			switch tt.screen {
			case ScreenApproval:
				model.focusRegion = FocusRegionActionPanel
			case ScreenClarification:
				model.focusRegion = FocusRegionChoices
			}
			model.activeTaskID = "task-1"
			tt.setup(&model)
			model.syncComponents()
			_ = model.syncInputFocus()
			model.editor.SetValue(tt.wantEditorText)

			next, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
			model = next.(Model)

			assert.Equal(t, DetailTabArtifacts, model.activeDetailTab)
			assert.Equal(t, tt.wantEditorText, model.editor.Value())
		})
	}
}

func TestApprovalFeedbackRowEnterInsertsNewlineAndActionRowSubmits(t *testing.T) {
	service := &fakeService{events: make(chan taskruntime.RunEvent, 8)}
	model := NewModel(service, "/tmp/project", "", nil, "v0.1.0")
	next, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 32})
	model = next.(Model)
	model.current = &taskdomain.TaskView{
		Task:   taskdomain.Task{ID: "task-1", Description: "Implement login"},
		Status: taskdomain.TaskStatusAwaitingUser,
		NodeRuns: []taskdomain.NodeRunView{
			{NodeRun: taskdomain.NodeRun{ID: "run-1", TaskID: "task-1", NodeName: "approve_plan", Status: taskdomain.NodeRunAwaitingUser}},
		},
	}
	model.activeTaskID = "task-1"
	model.currentInput = &taskruntime.InputRequest{
		Kind:      taskruntime.InputKindHumanNode,
		TaskID:    "task-1",
		NodeRunID: "run-1",
		NodeName:  "approve_plan",
	}
	model.screen = ScreenApproval
	model.approval.choice = approvalRowFeedback
	model.focusRegion = FocusRegionActionPanel
	model.syncComponents()
	_ = model.syncInputFocus()

	view := strippedView(model.View().Content)
	assert.Contains(t, view, "Feedback")

	model = typeText(t, model, "Need more")
	next, cmd := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = next.(Model)
	if cmd != nil {
		_ = cmd()
	}
	model = typeText(t, model, "detail")
	assert.Equal(t, "Need more\ndetail", model.editor.Value())
	assert.Empty(t, service.dispatched)

	next, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	model = next.(Model)
	assert.Equal(t, approvalRowReject, model.approval.choice)

	next, cmd = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = next.(Model)
	require.NotNil(t, cmd)
	require.Nil(t, cmd())
	require.Len(t, service.dispatched, 1)
	assert.Equal(t, taskruntime.CommandSubmitInput, service.dispatched[0].Type)
	assert.Equal(t, map[string]interface{}{
		"approved": false,
		"feedback": "Need more\ndetail",
	}, service.dispatched[0].Payload)
}

func TestApprovalWithFeedbackUpdatesLabelsAndCanApprove(t *testing.T) {
	service := &fakeService{events: make(chan taskruntime.RunEvent, 8)}
	model := NewModel(service, "/tmp/project", "", nil, "v0.1.0")
	next, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 32})
	model = next.(Model)
	model.current = &taskdomain.TaskView{
		Task:   taskdomain.Task{ID: "task-1", Description: "Implement login"},
		Status: taskdomain.TaskStatusAwaitingUser,
		NodeRuns: []taskdomain.NodeRunView{
			{NodeRun: taskdomain.NodeRun{ID: "run-1", TaskID: "task-1", NodeName: "approve_plan", Status: taskdomain.NodeRunAwaitingUser}},
		},
	}
	model.activeTaskID = "task-1"
	model.currentInput = &taskruntime.InputRequest{
		Kind:      taskruntime.InputKindHumanNode,
		TaskID:    "task-1",
		NodeRunID: "run-1",
		NodeName:  "approve_plan",
	}
	model.screen = ScreenApproval
	model.approval.choice = approvalRowFeedback
	model.focusRegion = FocusRegionActionPanel
	model.syncComponents()
	_ = model.syncInputFocus()

	model = typeText(t, model, "Looks good")
	view := strippedView(model.View().Content)
	assert.Contains(t, view, "Approve with feedback")
	assert.Contains(t, view, "Reject with feedback")

	for range 2 {
		next, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyUp})
		model = next.(Model)
	}
	assert.Equal(t, approvalRowApprove, model.approval.choice)

	next, cmd := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = next.(Model)
	require.NotNil(t, cmd)
	require.Nil(t, cmd())
	require.Len(t, service.dispatched, 1)
	assert.Equal(t, map[string]interface{}{
		"approved": true,
		"feedback": "Looks good",
	}, service.dispatched[0].Payload)
}

func TestApprovalTypingOnlyMutatesFeedbackRow(t *testing.T) {
	service := &fakeService{events: make(chan taskruntime.RunEvent, 8)}
	model := NewModel(service, "/tmp/project", "", nil, "v0.1.0")
	next, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 32})
	model = next.(Model)
	model.current = &taskdomain.TaskView{
		Task:   taskdomain.Task{ID: "task-1", Description: "Implement login"},
		Status: taskdomain.TaskStatusAwaitingUser,
		NodeRuns: []taskdomain.NodeRunView{
			{NodeRun: taskdomain.NodeRun{ID: "run-1", TaskID: "task-1", NodeName: "approve_plan", Status: taskdomain.NodeRunAwaitingUser}},
		},
	}
	model.activeTaskID = "task-1"
	model.currentInput = &taskruntime.InputRequest{
		Kind:      taskruntime.InputKindHumanNode,
		TaskID:    "task-1",
		NodeRunID: "run-1",
		NodeName:  "approve_plan",
	}
	model.screen = ScreenApproval
	model.approval.choice = approvalRowApprove
	model.focusRegion = FocusRegionActionPanel
	model.syncComponents()
	model.editor.SetValue("Need more detail")

	next, _ = model.Update(tea.KeyPressMsg{Text: "x", Code: 'x'})
	model = next.(Model)
	assert.Equal(t, "Need more detail", model.editor.Value())

	for range 2 {
		next, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyDown})
		model = next.(Model)
	}
	assert.Equal(t, approvalRowFeedback, model.approval.choice)

	next, _ = model.Update(tea.KeyPressMsg{Text: "x", Code: 'x'})
	model = next.(Model)
	assert.Equal(t, "Need more detailx", model.editor.Value())
}

func TestClarificationOtherRowEnterInsertsNewlineAndContinueSubmits(t *testing.T) {
	service := &fakeService{events: make(chan taskruntime.RunEvent, 8)}
	model := NewModel(service, "/tmp/project", "", nil, "v0.1.0")
	next, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 32})
	model = next.(Model)
	model.current = &taskdomain.TaskView{
		Task:   taskdomain.Task{ID: "task-1", Description: "Implement login"},
		Status: taskdomain.TaskStatusAwaitingUser,
		NodeRuns: []taskdomain.NodeRunView{
			{NodeRun: taskdomain.NodeRun{ID: "run-1", TaskID: "task-1", NodeName: "draft_plan", Status: taskdomain.NodeRunAwaitingUser}},
		},
	}
	model.activeTaskID = "task-1"
	question := taskdomain.ClarificationQuestion{
		Question:     "What should we do?",
		WhyItMatters: "Need direction",
		Options: []taskdomain.ClarificationOption{
			{Label: "A", Description: "Option A"},
			{Label: "B", Description: "Option B"},
		},
	}
	model.currentInput = &taskruntime.InputRequest{
		Kind:      taskruntime.InputKindClarification,
		TaskID:    "task-1",
		NodeRunID: "run-1",
		NodeName:  "draft_plan",
		Questions: []taskdomain.ClarificationQuestion{question},
	}
	model.screen = ScreenClarification
	model.focusRegion = FocusRegionChoices
	model.clarification.option = clarificationOtherRowIndex(question)
	model.clarification.other = map[int]bool{0: true}
	model.syncComponents()
	_ = model.syncInputFocus()

	model = typeText(t, model, "Custom")
	next, cmd := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = next.(Model)
	if cmd != nil {
		_ = cmd()
	}
	model = typeText(t, model, "answer")
	assert.Equal(t, "Custom\nanswer", model.editor.Value())
	assert.Empty(t, service.dispatched)

	next, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	model = next.(Model)
	assert.Equal(t, clarificationContinueRowIndex(question), model.clarification.option)

	next, cmd = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = next.(Model)
	require.NotNil(t, cmd)
	require.Nil(t, cmd())
	require.Len(t, service.dispatched, 1)
	assert.Equal(t, taskruntime.CommandSubmitInput, service.dispatched[0].Type)
	assert.Equal(t, map[string]interface{}{
		"answers": []interface{}{
			map[string]interface{}{"selected": "Custom\nanswer"},
		},
	}, service.dispatched[0].Payload)
}

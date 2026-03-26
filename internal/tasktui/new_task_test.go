package tasktui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

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
	assert.Contains(t, strippedView(view.Content), renderBlockGlyphRows("MUX")[0])
	assert.NotContains(t, strippedView(view.Content), "muxagent CLI")
	assert.Contains(t, strippedView(view.Content), "new task")
	assert.Contains(t, strippedView(view.Content), "Enter select")

	model = openNewTaskModal(t, model)
	view = model.View()
	assert.Contains(t, strippedView(view.Content), "New Task")
	assert.Contains(t, strippedView(view.Content), "runtime codex")
	assert.Contains(t, strippedView(view.Content), "Start task")
	assert.Contains(t, strippedView(view.Content), "Enter newline")
	assert.Contains(t, strippedView(view.Content), "Tab start")
	assert.NotContains(t, strippedView(view.Content), "Enter select")
}

func TestTaskListHeaderShowsVersionRevisionCwdAndRuntime(t *testing.T) {
	service := &fakeService{events: make(chan taskruntime.RunEvent, 8)}
	model := NewModel(
		service,
		"/tmp/project",
		"",
		&taskconfig.Config{Runtime: appconfig.RuntimeClaudeCode},
		"muxagent version dev (1234567890ab)",
	)
	next, _ := model.Update(tea.WindowSizeMsg{Width: 100, Height: 28})
	model = next.(Model)

	view := strippedView(model.View().Content)
	assert.Contains(t, view, renderBlockGlyphRows("MUX")[0])
	assert.Contains(t, view, renderBlockGlyphRows("AGENT")[0])
	assert.NotContains(t, view, "muxagent CLI")
	assert.Contains(t, view, "dev")
	assert.Contains(t, view, "1234567890ab")
	assert.Contains(t, view, "/tmp/project")
	assert.Contains(t, view, "runtime Claude Code")
}

func TestModelSubmitsNewTaskCommand(t *testing.T) {
	service := &fakeService{
		events: make(chan taskruntime.RunEvent, 8),
	}
	model := NewModel(service, "/tmp/project", "./taskflow.yaml", &taskconfig.Config{Runtime: appconfig.RuntimeClaudeCode}, "v0.1.0")
	next, _ := model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	model = next.(Model)
	model = openNewTaskModal(t, model)

	model = typeText(t, model, "Implement login")
	require.Contains(t, strippedView(model.View().Content), "Implement login")

	model, cmd := submitNewTaskModal(t, model)
	require.NotNil(t, cmd)
	require.Nil(t, cmd())
	require.Len(t, service.dispatched, 1)
	assert.Equal(t, taskruntime.CommandStartTask, service.dispatched[0].Type)
	assert.Equal(t, "./taskflow.yaml", service.dispatched[0].ConfigPath)
	assert.Equal(t, appconfig.RuntimeClaudeCode, service.dispatched[0].Runtime)
	assert.Equal(t, ScreenRunning, model.screen)
}

func TestNewTaskTextAreaGrowsOnEnterAndPreservesFirstLine(t *testing.T) {
	service := &fakeService{events: make(chan taskruntime.RunEvent, 8)}
	model := NewModel(service, "/tmp/project", "", nil, "v0.1.0")
	next, _ := model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	model = next.(Model)

	model = openNewTaskModal(t, model)
	require.True(t, model.newTaskInput.Focused(), "textarea must be focused")

	model = typeText(t, model, "first line")
	assert.Equal(t, "first line", model.newTaskInput.Value())

	next, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = next.(Model)
	assert.Equal(t, 2, model.newTaskInput.LineCount(), "should have 2 lines after enter")

	model = typeText(t, model, "second line")

	view := strippedView(model.View().Content)
	assert.Contains(t, view, "first line", "first line must remain visible after enter")
	assert.Contains(t, view, "second line", "second line must be visible")
	assert.GreaterOrEqual(t, model.newTaskInput.Height(), 2)

	model, cmd := submitNewTaskModal(t, model)
	require.NotNil(t, cmd)
	cmd()
	require.Len(t, service.dispatched, 1)
	assert.Contains(t, service.dispatched[0].Description, "first line")
	assert.Contains(t, service.dispatched[0].Description, "second line")
}

func TestNewTaskEscFromComposerCancelsAndResetsInput(t *testing.T) {
	service := &fakeService{events: make(chan taskruntime.RunEvent, 8)}
	model := NewModel(service, "/tmp/project", "", nil, "v0.1.0")
	next, _ := model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	model = next.(Model)

	model = openNewTaskModal(t, model)
	model = typeText(t, model, "Implement login")

	next, cmd := model.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	model = next.(Model)
	if cmd != nil {
		_ = cmd()
	}

	assert.Equal(t, ScreenTaskList, model.screen)
	assert.Equal(t, "", model.newTaskInput.Value())
}

func TestNewTaskEscFromActionPanelCancelsAndResetsInput(t *testing.T) {
	service := &fakeService{events: make(chan taskruntime.RunEvent, 8)}
	model := NewModel(service, "/tmp/project", "", nil, "v0.1.0")
	next, _ := model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	model = next.(Model)

	model = openNewTaskModal(t, model)
	model = typeText(t, model, "Implement login")

	next, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	model = next.(Model)
	assert.Equal(t, FocusRegionActionPanel, model.focusRegion)

	next, cmd := model.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	model = next.(Model)
	if cmd != nil {
		_ = cmd()
	}

	assert.Equal(t, ScreenTaskList, model.screen)
	assert.Equal(t, "", model.newTaskInput.Value())
}

func TestNewTaskTextAreaShrinksOnLineDeletion(t *testing.T) {
	service := &fakeService{events: make(chan taskruntime.RunEvent, 8)}
	model := NewModel(service, "/tmp/project", "", nil, "v0.1.0")
	next, _ := model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	model = next.(Model)

	model = openNewTaskModal(t, model)

	model = typeText(t, model, "line one")
	next, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = next.(Model)
	model = typeText(t, model, "line two")
	assert.GreaterOrEqual(t, model.newTaskInput.Height(), 2)

	for range len("line two") + 1 {
		next, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
		model = next.(Model)
	}

	assert.Equal(t, 1, model.newTaskInput.Height())
}

func TestNewTaskTextAreaAllowsUnlimitedLinesWithMaxVisual10(t *testing.T) {
	service := &fakeService{events: make(chan taskruntime.RunEvent, 8)}
	model := NewModel(service, "/tmp/project", "", nil, "v0.1.0")
	next, _ := model.Update(tea.WindowSizeMsg{Width: 80, Height: 40})
	model = next.(Model)

	model = openNewTaskModal(t, model)
	require.True(t, model.newTaskInput.Focused())

	for i := range 15 {
		if i > 0 {
			next, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
			model = next.(Model)
		}
		model = typeText(t, model, "line")
	}

	assert.Equal(t, 15, model.newTaskInput.LineCount(), "should allow >10 lines")
	assert.Equal(t, 10, model.newTaskInput.Height(), "visual height capped at 10")

	for model.newTaskInput.LineCount() > 10 {
		next, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
		model = next.(Model)
	}
	assert.Equal(t, 10, model.newTaskInput.LineCount())
	assert.Equal(t, 10, model.newTaskInput.Height())
	assert.Equal(t, 0, model.newTaskInput.ScrollYOffset(), "scroll offset must reset when lines <= height")

	for model.newTaskInput.LineCount() > 5 {
		next, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
		model = next.(Model)
	}
	assert.Equal(t, 5, model.newTaskInput.LineCount())
	assert.Equal(t, 5, model.newTaskInput.Height())
}

func TestNewTaskTextAreaGrowsForSoftWrappedSingleLine(t *testing.T) {
	service := &fakeService{events: make(chan taskruntime.RunEvent, 8)}
	model := NewModel(service, "/tmp/project", "", nil, "v0.1.0")
	next, _ := model.Update(tea.WindowSizeMsg{Width: 44, Height: 24})
	model = next.(Model)

	model = openNewTaskModal(t, model)
	model = typeText(t, model, strings.Repeat("x", 80))

	assert.Equal(t, 1, model.newTaskInput.LineCount(), "logical line count should stay at one")
	assert.GreaterOrEqual(t, model.newTaskInput.Height(), 2, "soft-wrapped single line should grow visually")
}

func TestNewTaskTextAreaSoftWrappedSingleLineShrinksAndResetsScroll(t *testing.T) {
	service := &fakeService{events: make(chan taskruntime.RunEvent, 8)}
	model := NewModel(service, "/tmp/project", "", nil, "v0.1.0")
	next, _ := model.Update(tea.WindowSizeMsg{Width: 44, Height: 24})
	model = next.(Model)

	model = openNewTaskModal(t, model)
	model = typeText(t, model, strings.Repeat("x", 400))

	assert.Equal(t, 1, model.newTaskInput.LineCount(), "logical line count should stay at one")
	assert.Equal(t, 10, model.newTaskInput.Height(), "visual height should still cap at 10")
	assert.Greater(t, model.newTaskInput.ScrollYOffset(), 0, "wrapped overflow should scroll once height cap is reached")

	for range 395 {
		next, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
		model = next.(Model)
	}

	assert.Equal(t, 1, model.newTaskInput.LineCount())
	assert.Equal(t, 1, model.newTaskInput.Height(), "shrinking wrapped content should shrink the composer")
	assert.Equal(t, 0, model.newTaskInput.ScrollYOffset(), "scroll offset should reset once wrapped content fits again")
}

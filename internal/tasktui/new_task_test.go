package tasktui

import (
	"regexp"
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
	assert.Contains(t, strippedView(view.Content), "Task description")
	assert.Contains(t, strippedView(view.Content), "runtime codex")
	assert.Contains(t, strippedView(view.Content), "Enter newline")
	assert.Contains(t, strippedView(view.Content), "Tab start")
	assert.Equal(t, 1, strings.Count(strippedView(view.Content), "Ctrl+P prev config"))
	assert.Equal(t, 1, strings.Count(strippedView(view.Content), "Ctrl+N next config"))
	assert.Equal(t, 1, strings.Count(strippedView(view.Content), "Enter newline"))
	assert.Equal(t, 1, strings.Count(strippedView(view.Content), "Tab start"))
	assert.NotContains(t, strippedView(view.Content), "Enter select")
}

func TestTaskListHeaderShowsVersionRevisionCwdAndConfig(t *testing.T) {
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

	metrics := model.computeScreenMetrics()
	header := strippedView(model.renderTaskListHeader(metrics.innerWidth))
	assert.Contains(t, header, renderBlockGlyphRows("MUX")[0])
	assert.Contains(t, header, renderBlockGlyphRows("AGENT")[0])
	assert.NotContains(t, header, "muxagent CLI")
	assert.Contains(t, header, "dev")
	assert.Contains(t, header, "1234567890ab")
	assert.Contains(t, header, "/tmp/project")
	assert.Contains(t, header, "config default")
	assert.NotContains(t, header, "runtime")
}

func TestTaskListCompactHeaderShowsCwdAndConfigWithoutRuntime(t *testing.T) {
	service := &fakeService{events: make(chan taskruntime.RunEvent, 8)}
	model := NewModel(service, "/tmp", "", nil, "v0.1.0")
	next, _ := model.Update(tea.WindowSizeMsg{Width: 60, Height: 24})
	model = next.(Model)

	metrics := model.computeScreenMetrics()
	header := strippedView(model.renderTaskListHeader(metrics.innerWidth))
	assert.Contains(t, header, "cwd /tmp")
	assert.Contains(t, header, "config default")
	assert.NotContains(t, header, "runtime")
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
	assert.Equal(t, taskconfig.DefaultAlias, service.dispatched[0].ConfigAlias)
	assert.Equal(t, "./taskflow.yaml", service.dispatched[0].ConfigPath)
	assert.Equal(t, appconfig.RuntimeClaudeCode, service.dispatched[0].Runtime)
	assert.Equal(t, ScreenRunning, model.screen)
}

func TestNewTaskCyclesToNextTaskConfigViaHotkey(t *testing.T) {
	service := &fakeService{events: make(chan taskruntime.RunEvent, 8)}
	model := NewModelWithCatalog(service, "/tmp/project", &taskconfig.Catalog{
		DefaultAlias: taskconfig.DefaultAlias,
		Entries: []taskconfig.CatalogEntry{
			{Alias: taskconfig.DefaultAlias, Config: &taskconfig.Config{Runtime: appconfig.RuntimeCodex}},
			{Alias: "reviewer", Path: "/tmp/reviewer.yaml", Config: &taskconfig.Config{Runtime: appconfig.RuntimeClaudeCode}},
		},
	}, "", "v0.1.0")
	next, _ := model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	model = next.(Model)
	model = openNewTaskModal(t, model)

	next, _ = model.Update(tea.KeyPressMsg{Code: 'n', Mod: tea.ModCtrl})
	model = next.(Model)

	view := strippedView(model.View().Content)
	assert.Contains(t, view, "config reviewer")
	assert.Contains(t, view, "runtime claude-code")
}

func TestNewTaskModalShowsWorktreeModeWhenAvailable(t *testing.T) {
	service := &fakeService{events: make(chan taskruntime.RunEvent, 8)}
	model := NewModel(service, "/tmp/project", "", nil, "v0.1.0")
	model.worktreeLaunchAvailable = true
	model.rememberedUseWorktree = false
	next, _ := model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	model = next.(Model)

	model = openNewTaskModal(t, model)

	view := strippedView(model.View().Content)
	assert.Contains(t, view, "worktree off")
	assert.Contains(t, view, "Ctrl+T worktree off")
}

func TestNewTaskToggleWorktreeDispatchesAndRemembersSelection(t *testing.T) {
	service := &fakeService{events: make(chan taskruntime.RunEvent, 8)}
	model := NewModel(service, "/tmp/project", "./taskflow.yaml", &taskconfig.Config{Runtime: appconfig.RuntimeCodex}, "v0.1.0")
	model.worktreeLaunchAvailable = true
	var saved []bool
	model.saveTaskLaunchPreference = func(useWorktree bool) error {
		saved = append(saved, useWorktree)
		return nil
	}
	next, _ := model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	model = next.(Model)

	model = openNewTaskModal(t, model)
	next, _ = model.Update(tea.KeyPressMsg{Code: 't', Mod: tea.ModCtrl})
	model = next.(Model)
	model = typeText(t, model, "Implement login")

	model, cmd := submitNewTaskModal(t, model)
	require.NotNil(t, cmd)
	require.Nil(t, cmd())
	require.Len(t, service.dispatched, 1)
	assert.True(t, service.dispatched[0].UseWorktree)
	assert.Equal(t, []bool{true}, saved)
	assert.True(t, model.rememberedUseWorktree)
}

func TestNewTaskReopensWithRememberedWorktreePreference(t *testing.T) {
	service := &fakeService{events: make(chan taskruntime.RunEvent, 8)}
	model := NewModel(service, "/tmp/project", "", nil, "v0.1.0")
	model.worktreeLaunchAvailable = true
	model.rememberedUseWorktree = true
	next, _ := model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	model = next.(Model)

	model = openNewTaskModal(t, model)
	assert.True(t, model.newTask.useWorktree)
	assert.Contains(t, strippedView(model.View().Content), "worktree on")

	next, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	model = next.(Model)
	model = openNewTaskModal(t, model)
	assert.True(t, model.newTask.useWorktree)
}

func TestModelSubmitsSelectedTaskConfigAlias(t *testing.T) {
	service := &fakeService{events: make(chan taskruntime.RunEvent, 8)}
	model := NewModelWithCatalog(service, "/tmp/project", &taskconfig.Catalog{
		DefaultAlias: taskconfig.DefaultAlias,
		Entries: []taskconfig.CatalogEntry{
			{Alias: taskconfig.DefaultAlias, Config: &taskconfig.Config{Runtime: appconfig.RuntimeCodex}},
			{Alias: "reviewer", Path: "/tmp/reviewer.yaml", Config: &taskconfig.Config{Runtime: appconfig.RuntimeClaudeCode}},
		},
	}, "", "v0.1.0")
	next, _ := model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	model = next.(Model)
	model = openNewTaskModal(t, model)

	next, _ = model.Update(tea.KeyPressMsg{Code: 'n', Mod: tea.ModCtrl})
	model = next.(Model)
	model = typeText(t, model, "Review docs")

	model, cmd := submitNewTaskModal(t, model)
	require.NotNil(t, cmd)
	require.Nil(t, cmd())
	require.Len(t, service.dispatched, 1)
	assert.Equal(t, "reviewer", service.dispatched[0].ConfigAlias)
	assert.Equal(t, "/tmp/reviewer.yaml", service.dispatched[0].ConfigPath)
	assert.Equal(t, appconfig.RuntimeClaudeCode, service.dispatched[0].Runtime)
}

func TestStartTaskCommandErrorReturnsToComposerAndPreservesInput(t *testing.T) {
	service := &fakeService{events: make(chan taskruntime.RunEvent, 8)}
	model := NewModel(service, "/tmp/project", "./taskflow.yaml", &taskconfig.Config{Runtime: appconfig.RuntimeClaudeCode}, "v0.1.0")
	next, _ := model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	model = next.(Model)
	model = openNewTaskModal(t, model)
	longDescription := strings.TrimSpace(strings.Repeat(
		"Broken config command-error regression coverage should preserve every character across the running-to-composer bounce. ",
		6,
	))
	require.Greater(t, len(longDescription), 512)
	model = typeText(t, model, longDescription)

	model, cmd := submitNewTaskModal(t, model)
	require.NotNil(t, cmd)
	require.Nil(t, cmd())
	require.Equal(t, ScreenRunning, model.screen)
	require.Len(t, service.dispatched, 1)
	assert.Equal(t, longDescription, service.dispatched[0].Description)

	next, _ = model.Update(taskruntime.RunEvent{
		Type:  taskruntime.EventCommandError,
		Error: &taskruntime.RunError{Message: "invalid task config"},
	})
	model = next.(Model)

	assert.Equal(t, ScreenNewTask, model.screen)
	assert.Equal(t, longDescription, model.editor.Value())
	assert.Equal(t, "invalid task config", model.errorText)
	assert.Nil(t, model.current)
	assert.Empty(t, model.activeTaskID)
}

func TestNewTaskTextAreaGrowsOnEnterAndPreservesFirstLine(t *testing.T) {
	service := &fakeService{events: make(chan taskruntime.RunEvent, 8)}
	model := NewModel(service, "/tmp/project", "", nil, "v0.1.0")
	next, _ := model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	model = next.(Model)

	model = openNewTaskModal(t, model)
	require.True(t, model.editor.Focused(), "textarea must be focused")
	initialHeight := model.editor.Height()

	model = typeText(t, model, "first line")
	assert.Equal(t, "first line", model.editor.Value())

	next, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = next.(Model)
	assert.Equal(t, 2, model.editor.LineCount(), "should have 2 lines after enter")

	model = typeText(t, model, "second line")

	view := strippedView(model.View().Content)
	assert.Contains(t, view, "first line", "first line must remain visible after enter")
	assert.Contains(t, view, "second line", "second line must be visible")
	assert.Equal(t, initialHeight, model.editor.Height(), "editor height should stay fixed")

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
	assert.Equal(t, "", model.editor.Value())
}

func TestNewTaskTabStartsTask(t *testing.T) {
	service := &fakeService{events: make(chan taskruntime.RunEvent, 8)}
	model := NewModel(service, "/tmp/project", "", nil, "v0.1.0")
	next, _ := model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	model = next.(Model)

	model = openNewTaskModal(t, model)
	model = typeText(t, model, "Implement login")

	next, cmd := model.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	model = next.(Model)
	require.NotNil(t, cmd)
	require.Nil(t, cmd())
	assert.Equal(t, ScreenRunning, model.screen)
	assert.Len(t, service.dispatched, 1)
	assert.Equal(t, taskruntime.CommandStartTask, service.dispatched[0].Type)
}

func TestNewTaskTabDoesNothingWhenDescriptionEmpty(t *testing.T) {
	service := &fakeService{events: make(chan taskruntime.RunEvent, 8)}
	model := NewModel(service, "/tmp/project", "", nil, "v0.1.0")
	next, _ := model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	model = next.(Model)

	model = openNewTaskModal(t, model)
	next, cmd := model.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	model = next.(Model)
	if cmd != nil {
		_ = cmd()
	}
	assert.Equal(t, ScreenNewTask, model.screen)
	assert.Equal(t, FocusRegionComposer, model.focusRegion)
	assert.Empty(t, service.dispatched)
}

func TestNewTaskEditorKeepsFixedHeightWhileDeleting(t *testing.T) {
	service := &fakeService{events: make(chan taskruntime.RunEvent, 8)}
	model := NewModel(service, "/tmp/project", "", nil, "v0.1.0")
	next, _ := model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	model = next.(Model)

	model = openNewTaskModal(t, model)
	initialHeight := model.editor.Height()

	model = typeText(t, model, "line one")
	next, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = next.(Model)
	model = typeText(t, model, "line two")
	assert.Equal(t, initialHeight, model.editor.Height())

	for range len("line two") + 1 {
		next, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
		model = next.(Model)
	}

	assert.Equal(t, initialHeight, model.editor.Height())
}

func TestNewTaskEditorScrollsInternallyForLongContent(t *testing.T) {
	service := &fakeService{events: make(chan taskruntime.RunEvent, 8)}
	model := NewModel(service, "/tmp/project", "", nil, "v0.1.0")
	next, _ := model.Update(tea.WindowSizeMsg{Width: 80, Height: 40})
	model = next.(Model)

	model = openNewTaskModal(t, model)
	require.True(t, model.editor.Focused())
	initialHeight := model.editor.Height()

	for i := range 15 {
		if i > 0 {
			next, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
			model = next.(Model)
		}
		model = typeText(t, model, "line")
	}

	assert.Equal(t, 15, model.editor.LineCount(), "should allow >10 lines")
	assert.Equal(t, initialHeight, model.editor.Height(), "editor height should stay fixed")
	assert.Greater(t, model.editor.ScrollYOffset(), 0, "long drafts should scroll inside the fixed-height editor")
}

func TestNewTaskEditorKeepsFixedHeightForSoftWrappedSingleLine(t *testing.T) {
	service := &fakeService{events: make(chan taskruntime.RunEvent, 8)}
	model := NewModel(service, "/tmp/project", "", nil, "v0.1.0")
	next, _ := model.Update(tea.WindowSizeMsg{Width: 44, Height: 24})
	model = next.(Model)

	model = openNewTaskModal(t, model)
	initialHeight := model.editor.Height()
	model = typeText(t, model, strings.Repeat("x", 80))

	assert.Equal(t, 1, model.editor.LineCount(), "logical line count should stay at one")
	assert.Equal(t, initialHeight, model.editor.Height(), "soft-wrapped input should scroll, not resize")
}

func TestNewTaskEditorWidthMatchesFieldInnerWidth(t *testing.T) {
	service := &fakeService{events: make(chan taskruntime.RunEvent, 8)}
	model := NewModel(service, "/tmp/project", "", nil, "v0.1.0")
	next, _ := model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	model = next.(Model)

	model = openNewTaskModal(t, model)

	metrics := model.computeScreenMetrics()
	header := model.renderAppHeader(metrics.innerWidth)
	footer := renderFooterHintBar(metrics.innerWidth, model.newTaskModalHint())
	layout := model.computeNewTaskScreenLayout(header, footer)
	fieldWidth := max(18, layout.modalInnerWidth)

	assert.Equal(t, editorFieldInnerWidth(fieldWidth), model.editor.Width(), "textarea width must match the field shell's inner width")
}

func TestNewTaskFocusedEditorUsesRealCursor(t *testing.T) {
	service := &fakeService{events: make(chan taskruntime.RunEvent, 8)}
	model := NewModel(service, "/tmp/project", "", nil, "v0.1.0")
	next, _ := model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	model = next.(Model)

	model = openNewTaskModal(t, model)
	model = typeText(t, model, "你好")

	view := model.View()
	require.NotNil(t, view.Cursor, "focused shared editor should expose a real terminal cursor")
	lines := strings.Split(strippedView(view.Content), "\n")
	require.Less(t, view.Cursor.Position.Y, len(lines))
	assert.Contains(t, lines[view.Cursor.Position.Y], "│", "cursor should land on an editor content row, not on labels or chrome")
	assert.NotContains(t, lines[view.Cursor.Position.Y], "Task description")
}

func TestNewTaskLongSingleLineWrapsWithoutTinyContinuationRows(t *testing.T) {
	service := &fakeService{events: make(chan taskruntime.RunEvent, 8)}
	model := NewModel(service, "/tmp/project", "", nil, "v0.1.0")
	next, _ := model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	model = next.(Model)

	model = openNewTaskModal(t, model)
	innerWidth := model.editor.Width()
	long := strings.Repeat("x", innerWidth*2+12)
	model = typeText(t, model, long)

	re := regexp.MustCompile(`x+`)
	field := strippedView(model.renderEditorField(innerWidth+editorFieldFrameWidth(), "Task description", ""))
	lines := strings.Split(field, "\n")
	chunkLengths := make([]int, 0, 4)
	for _, line := range lines {
		if !strings.Contains(line, "│") {
			continue
		}
		if match := re.FindString(line); match != "" {
			chunkLengths = append(chunkLengths, len(match))
		}
	}

	assert.Equal(t, []int{innerWidth, innerWidth, 12}, chunkLengths, "single-line soft wrap should consume the field width evenly instead of producing tiny orphan rows")
}

func TestNewTaskEditorSoftWrappedSingleLineScrollsAndResets(t *testing.T) {
	service := &fakeService{events: make(chan taskruntime.RunEvent, 8)}
	model := NewModel(service, "/tmp/project", "", nil, "v0.1.0")
	next, _ := model.Update(tea.WindowSizeMsg{Width: 44, Height: 24})
	model = next.(Model)

	model = openNewTaskModal(t, model)
	initialHeight := model.editor.Height()
	model = typeText(t, model, strings.Repeat("x", 400))

	assert.Equal(t, 1, model.editor.LineCount(), "logical line count should stay at one")
	assert.Equal(t, initialHeight, model.editor.Height(), "visual height should stay fixed")
	assert.Greater(t, model.editor.ScrollYOffset(), 0, "wrapped overflow should scroll once height cap is reached")

	for range 395 {
		next, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
		model = next.(Model)
	}

	assert.Equal(t, 1, model.editor.LineCount())
	assert.Equal(t, initialHeight, model.editor.Height(), "shrinking wrapped content should keep the fixed editor height")
	assert.Equal(t, 0, model.editor.ScrollYOffset(), "scroll offset should reset once wrapped content fits again")
}

package tasktui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/LaLanMo/muxagent-cli/internal/taskdomain"
	"github.com/LaLanMo/muxagent-cli/internal/taskruntime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCtrlCOpensQuitDialogAndBlocksUnderlyingInput(t *testing.T) {
	service := &fakeService{events: make(chan taskruntime.RunEvent, 8)}
	model := NewModel(service, "/tmp/project", "", nil, "v0.1.0")
	next, _ := model.Update(tea.WindowSizeMsg{Width: 100, Height: 28})
	model = next.(Model)
	model.tasks = []taskdomain.TaskView{
		{Task: taskdomain.Task{ID: "task-1", Description: "Implement login"}, Status: taskdomain.TaskStatusRunning},
	}
	model.syncComponents()

	model = openNewTaskModal(t, model)
	model = typeText(t, model, "stay")
	require.Equal(t, "stay", model.editor.Value())

	next, cmd := model.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	model = next.(Model)
	require.NotNil(t, model.dialog)
	assert.Equal(t, ScreenNewTask, model.screen)
	if cmd != nil {
		assert.Nil(t, cmd())
	}
	view := strippedView(model.View().Content)
	assert.Contains(t, view, "Quit muxagent?")
	assert.Contains(t, view, "Cancel")
	assert.Contains(t, view, "Quit")
	assert.Contains(t, view, "Enter newline", "dialog should preserve the underlying screen outside the card")

	next, _ = model.Update(tea.KeyPressMsg{Text: "x", Code: 'x'})
	model = next.(Model)
	assert.Equal(t, "stay", model.editor.Value(), "dialog must swallow underlying form-editor input")

	next, cmd = model.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	model = next.(Model)
	assert.Nil(t, model.dialog)
	if cmd != nil {
		_ = cmd()
	}

	next, _ = model.Update(tea.KeyPressMsg{Text: "!", Code: '!'})
	model = next.(Model)
	assert.Equal(t, "stay!", model.editor.Value(), "form-editor focus should be restored after dialog closes")
}

func TestQuitDialogConfirmQuits(t *testing.T) {
	service := &fakeService{events: make(chan taskruntime.RunEvent, 8)}
	model := NewModel(service, "/tmp/project", "", nil, "v0.1.0")
	next, _ := model.Update(tea.WindowSizeMsg{Width: 100, Height: 28})
	model = next.(Model)

	next, cmd := model.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	model = next.(Model)
	require.NotNil(t, model.dialog)
	if cmd != nil {
		assert.Nil(t, cmd())
	}

	next, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	model = next.(Model)

	next, cmd = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = next.(Model)
	require.NotNil(t, cmd)
	_, ok := cmd().(tea.QuitMsg)
	assert.True(t, ok)
}

func TestQuitDialogRendersButtonsOnOneRow(t *testing.T) {
	dialog := newQuitDialog()
	view := strippedView(dialog.View(surfaceRect{Width: 100, Height: 28}))
	lines := strings.Split(view, "\n")
	buttonLines := 0
	for _, line := range lines {
		if strings.Contains(line, "Cancel") && strings.Contains(line, "Quit") {
			buttonLines++
		}
	}
	assert.Equal(t, 1, buttonLines, "dialog actions should render as a single horizontal row")
}

func TestQuitDialogViewHasUniformLineWidths(t *testing.T) {
	dialog := newQuitDialog()
	view := dialog.View(surfaceRect{Width: 100, Height: 28})
	lines := strings.Split(view, "\n")
	require.NotEmpty(t, lines)
	width := ansi.StringWidth(lines[0])
	for _, line := range lines {
		assert.Equal(t, width, ansi.StringWidth(line), "dialog lines should all render to the same width")
	}
}

func TestQuitDialogViewHasOpaqueMoat(t *testing.T) {
	dialog := newQuitDialog()
	view := strippedView(dialog.View(surfaceRect{Width: 100, Height: 28}))
	lines := strings.Split(view, "\n")
	require.NotEmpty(t, lines)
	for _, line := range lines {
		require.NotEmpty(t, line)
		assert.Equal(t, " ", string(line[0]), "dialog should keep a left moat outside the border")
		assert.Equal(t, " ", string(line[len(line)-1]), "dialog should keep a right moat outside the border")
	}
}

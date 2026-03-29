package tasktui

import (
	"os"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/LaLanMo/muxagent-cli/internal/taskconfig"
	"github.com/LaLanMo/muxagent-cli/internal/taskruntime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTaskConfigScreenCrudViaKeyFlow(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	model := NewModel(&fakeService{events: make(chan taskruntime.RunEvent, 8)}, t.TempDir(), "", nil, "v0.1.0")
	next, _ := model.Update(tea.WindowSizeMsg{Width: 100, Height: 28})
	model = next.(Model)
	model = openTaskConfigScreenFromList(t, model)
	assert.Contains(t, strippedView(model.View().Content), "default")

	model, _ = pressTaskConfigKey(t, model, tea.KeyPressMsg{Text: "n", Code: 'n'})
	model = typeText(t, model, "reviewer")
	model, _ = pressTaskConfigKey(t, model, tea.KeyPressMsg{Code: tea.KeyEnter})

	assert.Equal(t, "reviewer", model.taskConfigs.selectedAlias)
	assert.Equal(t, "reviewer", model.selectedConfigAlias)
	require.True(t, hasTaskConfigEntry(model.taskConfigs.entries, "reviewer"))

	model, _ = pressTaskConfigKey(t, model, tea.KeyPressMsg{Text: "r", Code: 'r'})
	model = clearTaskConfigEditorText(t, model)
	model = typeText(t, model, "deep-review")
	model, _ = pressTaskConfigKey(t, model, tea.KeyPressMsg{Code: tea.KeyEnter})

	assert.Equal(t, "deep-review", model.taskConfigs.selectedAlias)
	assert.Equal(t, "deep-review", model.selectedConfigAlias)
	require.True(t, hasTaskConfigEntry(model.taskConfigs.entries, "deep-review"))
	require.False(t, hasTaskConfigEntry(model.taskConfigs.entries, "reviewer"))

	model, _ = pressTaskConfigKey(t, model, tea.KeyPressMsg{Code: tea.KeyEnter})
	assert.Equal(t, "deep-review", model.configCatalog.DefaultAlias)
	assert.Equal(t, "deep-review", model.selectedConfigAlias)

	model, _ = pressTaskConfigKey(t, model, tea.KeyPressMsg{Text: "x", Code: 'x'})
	model, _ = pressTaskConfigKey(t, model, tea.KeyPressMsg{Code: tea.KeyEnter})

	assert.Equal(t, taskconfig.DefaultAlias, model.configCatalog.DefaultAlias)
	assert.Equal(t, taskconfig.DefaultAlias, model.selectedConfigAlias)
	assert.False(t, hasTaskConfigEntry(model.taskConfigs.entries, "deep-review"))
}

func TestTaskConfigScreenEnterOnBuiltinDefaultShowsStatusAndContextualHint(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	model := NewModel(&fakeService{events: make(chan taskruntime.RunEvent, 8)}, t.TempDir(), "", nil, "v0.1.0")
	next, _ := model.Update(tea.WindowSizeMsg{Width: 100, Height: 28})
	model = next.(Model)
	model = openTaskConfigScreenFromList(t, model)

	require.Equal(t, taskconfig.DefaultAlias, model.configCatalog.DefaultAlias)
	assert.Contains(t, strippedView(model.View().Content), "default selected")
	assert.NotContains(t, strippedView(model.View().Content), "r rename")
	assert.NotContains(t, strippedView(model.View().Content), "x delete")

	model, cmd := pressTaskConfigKey(t, model, tea.KeyPressMsg{Code: tea.KeyEnter})
	require.Nil(t, cmd)
	assert.Equal(t, taskconfig.DefaultAlias, model.configCatalog.DefaultAlias)
	assert.Equal(t, `config "default" is already the default`, model.taskConfigs.statusText)
}

func TestTaskConfigScreenRejectsInvalidConfigAsDefaultViaEnter(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	taskConfigDir, err := taskconfig.TaskConfigDir()
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Join(taskConfigDir, "broken"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(taskConfigDir, "broken", "config.yaml"), []byte("version: ["), 0o644))
	_, err = taskconfig.SaveRegistry(taskconfig.Registry{
		DefaultAlias: taskconfig.DefaultAlias,
		Configs: []taskconfig.RegistryEntry{
			{Alias: "broken", Path: "broken"},
		},
	})
	require.NoError(t, err)

	model := NewModel(&fakeService{events: make(chan taskruntime.RunEvent, 8)}, t.TempDir(), "", nil, "v0.1.0")
	next, _ := model.Update(tea.WindowSizeMsg{Width: 100, Height: 28})
	model = next.(Model)
	model = openTaskConfigScreenFromList(t, model)

	for i := 0; i < 3; i++ {
		model, _ = pressTaskConfigKey(t, model, tea.KeyPressMsg{Code: tea.KeyDown})
	}
	selected, ok := selectedTaskConfigListItem(model.configList)
	require.True(t, ok)
	require.Equal(t, "broken", selected.summary.Alias)

	model, cmd := pressTaskConfigKey(t, model, tea.KeyPressMsg{Code: tea.KeyEnter})
	require.Nil(t, cmd)
	assert.Equal(t, taskconfig.DefaultAlias, model.configCatalog.DefaultAlias)
	assert.Equal(t, taskconfig.DefaultAlias, model.selectedConfigAlias)
	assert.Contains(t, model.taskConfigs.errorText, `config "broken" is invalid`)
}

func TestTaskConfigScreenBuiltinRowsCannotBeRenamedOrDeleted(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	model := NewModel(&fakeService{events: make(chan taskruntime.RunEvent, 8)}, t.TempDir(), "", nil, "v0.1.0")
	next, _ := model.Update(tea.WindowSizeMsg{Width: 100, Height: 28})
	model = next.(Model)
	model = openTaskConfigScreenFromList(t, model)

	model, cmd := pressTaskConfigKey(t, model, tea.KeyPressMsg{Text: "r", Code: 'r'})
	require.Nil(t, cmd)
	assert.Contains(t, model.taskConfigs.errorText, `config "default" cannot be renamed`)

	model, cmd = pressTaskConfigKey(t, model, tea.KeyPressMsg{Text: "x", Code: 'x'})
	require.Nil(t, cmd)
	assert.Contains(t, model.taskConfigs.errorText, `config "default" cannot be deleted`)
}

func TestTaskConfigFormShowsEditorCursor(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	model := NewModel(&fakeService{events: make(chan taskruntime.RunEvent, 8)}, t.TempDir(), "", nil, "v0.1.0")
	next, _ := model.Update(tea.WindowSizeMsg{Width: 100, Height: 28})
	model = next.(Model)
	model = openTaskConfigScreenFromList(t, model)

	model, _ = pressTaskConfigKey(t, model, tea.KeyPressMsg{Text: "n", Code: 'n'})
	model = typeText(t, model, "reviewer")

	view := model.View()
	require.NotNil(t, view.Cursor)
}

func TestNewTaskSkipsInvalidConfigsWhenCyclingAndRejectsBrokenSelection(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	taskConfigDir, err := taskconfig.TaskConfigDir()
	require.NoError(t, err)
	reviewerDir := filepath.Join(taskConfigDir, "reviewer")
	require.NoError(t, os.MkdirAll(reviewerDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(reviewerDir, "config.yaml"), []byte(`version: 1
runtime: codex
clarification:
  max_questions: 4
  max_options_per_question: 4
  min_options_per_question: 2
topology:
  max_iterations: 1
  entry: start
  nodes:
    - name: start
    - name: done
  edges:
    - from: start
      to: done
node_definitions:
  start:
    system_prompt: ./prompt.md
    result_schema:
      type: object
      additionalProperties: false
      properties: {}
  done:
    type: terminal
`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(reviewerDir, "prompt.md"), []byte("# prompt"), 0o644))

	brokenDir := filepath.Join(taskConfigDir, "broken")
	require.NoError(t, os.MkdirAll(brokenDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(brokenDir, "config.yaml"), []byte("version: ["), 0o644))

	catalog, err := taskconfig.LoadCatalog()
	require.NoError(t, err)
	catalog.Entries = append(catalog.Entries,
		taskconfig.CatalogEntry{Alias: "broken", Path: filepath.Join(brokenDir, "config.yaml")},
		taskconfig.CatalogEntry{Alias: "reviewer", Path: filepath.Join(reviewerDir, "config.yaml")},
	)

	model := NewModelWithCatalog(&fakeService{events: make(chan taskruntime.RunEvent, 8)}, t.TempDir(), catalog, "v0.1.0")
	next, _ := model.Update(tea.WindowSizeMsg{Width: 100, Height: 28})
	model = next.(Model)
	model.selectedConfigAlias = "broken"

	model = openNewTaskModal(t, model)
	next, _ = model.Update(tea.KeyPressMsg{Code: 'n', Mod: tea.ModCtrl})
	model = next.(Model)
	assert.Equal(t, "reviewer", model.selectedConfigAlias)

	model.selectedConfigAlias = "broken"
	model = typeText(t, model, "Review docs")
	model, cmd := submitNewTaskModal(t, model)
	require.Nil(t, cmd)
	assert.Equal(t, ScreenNewTask, model.screen)
	assert.Contains(t, model.errorText, `task config "broken" is invalid`)
}

func pressTaskConfigKey(t *testing.T, model Model, key tea.KeyPressMsg) (Model, tea.Cmd) {
	t.Helper()
	next, cmd := model.Update(key)
	model = next.(Model)
	if cmd != nil {
		msg := cmd()
		if msg != nil {
			next, _ := model.Update(msg)
			model = next.(Model)
		}
	}
	return model, cmd
}

func clearTaskConfigEditorText(t *testing.T, model Model) Model {
	t.Helper()
	for range len(model.editor.Value()) {
		next, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
		model = next.(Model)
	}
	return model
}

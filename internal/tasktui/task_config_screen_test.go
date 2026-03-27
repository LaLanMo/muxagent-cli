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

func TestTaskConfigScreenLoadsCatalogAndHealth(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	taskConfigDir, err := taskconfig.TaskConfigDir()
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Join(taskConfigDir, "broken"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(taskConfigDir, "broken", "config.yaml"), []byte("version: ["), 0o644))
	_, err = taskconfig.SaveRegistry(taskconfig.Registry{
		DefaultAlias: taskconfig.DefaultAlias,
		Configs: []taskconfig.RegistryEntry{
			{Alias: taskconfig.DefaultAlias, Path: taskconfig.DefaultAlias},
			{Alias: "broken", Path: "broken"},
		},
	})
	require.NoError(t, err)

	model := NewModel(&fakeService{events: make(chan taskruntime.RunEvent, 8)}, t.TempDir(), "", nil, "v0.1.0")
	model = openTaskConfigScreen(t, model)

	require.Equal(t, ScreenTaskConfigs, model.screen)
	require.Len(t, model.taskConfigs.entries, 2)

	var broken taskConfigSummary
	for _, entry := range model.taskConfigs.entries {
		if entry.Alias == "broken" {
			broken = entry
		}
	}
	assert.Equal(t, "broken", broken.Alias)
	assert.NotEmpty(t, broken.LoadErr)
}

func TestTaskConfigScreenCrudLifecycle(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	model := NewModel(&fakeService{events: make(chan taskruntime.RunEvent, 8)}, t.TempDir(), "", nil, "v0.1.0")
	model = openTaskConfigScreen(t, model)

	cmd := model.openCloneTaskConfigForm()
	model = runCmdUpdateModel(t, model, cmd)
	model = typeText(t, model, "reviewer")
	cmd = model.submitTaskConfigForm()
	require.NotNil(t, cmd)
	model = runCmdUpdateModel(t, model, cmd)

	assert.Equal(t, "reviewer", model.taskConfigs.selectedAlias)
	assert.Equal(t, "reviewer", model.selectedConfigAlias)
	require.True(t, hasTaskConfigEntry(model.taskConfigs.entries, "reviewer"))

	cmd = model.openRenameTaskConfigForm()
	model = runCmdUpdateModel(t, model, cmd)
	model.editor.SetValue("deep-review")
	cmd = model.submitTaskConfigForm()
	require.NotNil(t, cmd)
	model = runCmdUpdateModel(t, model, cmd)

	assert.Equal(t, "deep-review", model.taskConfigs.selectedAlias)
	assert.Equal(t, "deep-review", model.selectedConfigAlias)
	require.True(t, hasTaskConfigEntry(model.taskConfigs.entries, "deep-review"))
	require.False(t, hasTaskConfigEntry(model.taskConfigs.entries, "reviewer"))

	cmd = model.submitSetDefaultTaskConfig()
	require.NotNil(t, cmd)
	model = runCmdUpdateModel(t, model, cmd)
	assert.Equal(t, "deep-review", model.configCatalog.DefaultAlias)

	cmd = model.openDeleteTaskConfigConfirm()
	model = runCmdUpdateModel(t, model, cmd)
	cmd = model.submitDeleteTaskConfig()
	require.NotNil(t, cmd)
	model = runCmdUpdateModel(t, model, cmd)

	assert.Equal(t, taskconfig.DefaultAlias, model.configCatalog.DefaultAlias)
	assert.Equal(t, taskconfig.DefaultAlias, model.selectedConfigAlias)
	assert.False(t, hasTaskConfigEntry(model.taskConfigs.entries, "deep-review"))

	taskConfigDir, err := taskconfig.TaskConfigDir()
	require.NoError(t, err)
	assert.NoDirExists(t, filepath.Join(taskConfigDir, "reviewer"))
}

func openTaskConfigScreen(t *testing.T, model Model) Model {
	t.Helper()
	cmd := model.openTaskConfigs()
	require.NotNil(t, cmd)
	return runCmdUpdateModel(t, model, cmd)
}

func runCmdUpdateModel(t *testing.T, model Model, cmd tea.Cmd) Model {
	t.Helper()
	if cmd == nil {
		return model
	}
	msg := cmd()
	if msg == nil {
		return model
	}
	next, _ := model.Update(msg)
	return next.(Model)
}

func hasTaskConfigEntry(entries []taskConfigSummary, alias string) bool {
	for _, entry := range entries {
		if entry.Alias == alias {
			return true
		}
	}
	return false
}

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

func TestTaskConfigScreenLoadsBuiltinsAndBrokenConfigHealth(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	setTaskTUIRuntimePath(t, "codex")

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
	model = openTaskConfigScreen(t, model)

	require.Equal(t, ScreenTaskConfigs, model.screen)
	require.Len(t, model.taskConfigs.entries, 6)

	assert.Equal(t, "default", model.taskConfigs.entries[0].Alias)
	assert.Equal(t, "plan-only", model.taskConfigs.entries[1].Alias)
	assert.Equal(t, "single-run", model.taskConfigs.entries[2].Alias)
	assert.Equal(t, "autonomous", model.taskConfigs.entries[3].Alias)
	assert.Equal(t, "yolo", model.taskConfigs.entries[4].Alias)

	broken := model.taskConfigs.entries[5]
	assert.Equal(t, "broken", broken.Alias)
	assert.False(t, broken.Builtin)
	assert.NotEmpty(t, broken.LoadErr)
}

func TestTaskConfigScreenRuntimeTogglePersistsSelectionAndUpdatesSummary(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	setTaskTUIRuntimePath(t, "codex", "claude")

	model := NewModel(&fakeService{events: make(chan taskruntime.RunEvent, 8)}, t.TempDir(), "", nil, "v0.1.0")
	model = openTaskConfigScreen(t, model)

	selected, ok := model.selectedManagedTaskConfig()
	require.True(t, ok)
	assert.Equal(t, "default", selected.Alias)
	assert.Equal(t, "Codex", selected.Runtime)

	cmd := model.toggleSelectedTaskConfigRuntime()
	require.NotNil(t, cmd)
	model = runCmdUpdateModel(t, model, cmd)

	assert.Equal(t, "default", model.taskConfigs.selectedAlias)
	assert.Equal(t, taskconfig.DefaultAlias, model.selectedConfigAlias)
	assert.Equal(t, `config "default" runtime is now Claude Code`, model.taskConfigs.statusText)

	selected, ok = model.selectedManagedTaskConfig()
	require.True(t, ok)
	assert.Equal(t, "Claude Code", selected.Runtime)
	assert.Contains(t, strippedView(model.View().Content), "Shift+Tab runtime OpenCode")

	entry, ok := model.configCatalog.Entry(taskconfig.DefaultAlias)
	require.True(t, ok)
	cfg, err := entry.LoadConfig()
	require.NoError(t, err)
	assert.Equal(t, "claude-code", string(cfg.Runtime))

	cmd = model.toggleSelectedTaskConfigRuntime()
	require.NotNil(t, cmd)
	model = runCmdUpdateModel(t, model, cmd)

	selected, ok = model.selectedManagedTaskConfig()
	require.True(t, ok)
	assert.Equal(t, "OpenCode", selected.Runtime)
	assert.Equal(t, `config "default" runtime is now OpenCode`, model.taskConfigs.statusText)
	assert.Contains(t, strippedView(model.View().Content), "Shift+Tab runtime Codex")

	entry, ok = model.configCatalog.Entry(taskconfig.DefaultAlias)
	require.True(t, ok)
	cfg, err = entry.LoadConfig()
	require.NoError(t, err)
	assert.Equal(t, "opencode", string(cfg.Runtime))
}

func TestTaskConfigScreenUserConfigLifecycleWithoutCloneFlow(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	setTaskTUIRuntimePath(t, "codex")

	catalog, err := taskconfig.LoadCatalog()
	require.NoError(t, err)
	source, ok := catalog.Entry(taskconfig.DefaultAlias)
	require.True(t, ok)
	_, err = taskconfig.CloneConfig("reviewer", source.Path)
	require.NoError(t, err)

	model := NewModel(&fakeService{events: make(chan taskruntime.RunEvent, 8)}, t.TempDir(), "", nil, "v0.1.0")
	model = openTaskConfigScreen(t, model)

	for i := 0; i < len(model.taskConfigs.entries); i++ {
		selected, ok := model.selectedManagedTaskConfig()
		require.True(t, ok)
		if selected.Alias == "reviewer" {
			break
		}
		next, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyDown})
		model = next.(Model)
	}

	cmd := model.openRenameTaskConfigForm()
	model = runCmdUpdateModel(t, model, cmd)
	model.editor.SetValue("deep-review")
	cmd = model.submitTaskConfigForm()
	require.NotNil(t, cmd)
	model = runCmdUpdateModel(t, model, cmd)

	assert.Equal(t, "deep-review", model.taskConfigs.selectedAlias)
	assert.Equal(t, taskconfig.DefaultAlias, model.selectedConfigAlias)
	require.True(t, hasTaskConfigEntry(model.taskConfigs.entries, "deep-review"))
	assert.False(t, hasTaskConfigEntry(model.taskConfigs.entries, "reviewer"))

	cmd = model.submitSetDefaultTaskConfig()
	require.NotNil(t, cmd)
	model = runCmdUpdateModel(t, model, cmd)
	assert.Equal(t, "deep-review", model.configCatalog.DefaultAlias)
	assert.Equal(t, "deep-review", model.selectedConfigAlias)

	cmd = model.openDeleteTaskConfigConfirm()
	model = runCmdUpdateModel(t, model, cmd)
	cmd = model.submitDeleteTaskConfig()
	require.NotNil(t, cmd)
	model = runCmdUpdateModel(t, model, cmd)

	assert.Equal(t, taskconfig.DefaultAlias, model.configCatalog.DefaultAlias)
	assert.Equal(t, taskconfig.DefaultAlias, model.selectedConfigAlias)
	assert.False(t, hasTaskConfigEntry(model.taskConfigs.entries, "deep-review"))
}

func TestTaskConfigHeaderPrioritizesModeIntent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	setTaskTUIRuntimePath(t, "codex")

	model := NewModel(&fakeService{events: make(chan taskruntime.RunEvent, 8)}, t.TempDir(), "", nil, "v0.1.0")
	next, _ := model.Update(tea.WindowSizeMsg{Width: 100, Height: 28})
	model = next.(Model)
	model = openTaskConfigScreen(t, model)

	selected, ok := model.selectedManagedTaskConfig()
	require.True(t, ok)

	headerMeta := strippedView(model.renderSelectedTaskConfigMeta(selected))
	screen := strippedView(model.View().Content)

	assert.Contains(t, headerMeta, "selected default")
	assert.Contains(t, headerMeta, "owner builtin")
	assert.Contains(t, headerMeta, "mode Human-gated workflow")
	assert.Contains(t, screen, "plan-only")
	assert.Contains(t, screen, "builtin")
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

package taskconfig

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	appconfig "github.com/LaLanMo/muxagent-cli/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadCatalogSeedsBuiltinModesAndRegistryMetadata(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	catalog, err := LoadCatalog()
	require.NoError(t, err)
	require.Len(t, catalog.Entries, 4)
	assert.Equal(t, DefaultAlias, catalog.DefaultAlias)

	assert.Equal(t, DefaultAlias, catalog.Entries[0].Alias)
	assert.Equal(t, BuiltinIDDefault, catalog.Entries[0].BuiltinID)
	assert.True(t, catalog.Entries[0].Builtin)
	assert.Equal(t, BuiltinIDPlanOnly, catalog.Entries[1].BuiltinID)
	assert.Equal(t, BuiltinIDAutonomous, catalog.Entries[2].BuiltinID)
	assert.Equal(t, BuiltinIDYolo, catalog.Entries[3].BuiltinID)
	for _, entry := range catalog.Entries {
		cfg, err := entry.LoadConfig()
		require.NoError(t, err)
		assert.NotEmpty(t, cfg.Description)
		assert.FileExists(t, entry.Path)
	}

	reg, err := LoadRegistry()
	require.NoError(t, err)
	require.Len(t, reg.Configs, 4)
	assert.Equal(t, DefaultAlias, reg.DefaultAlias)

	defaultEntry, ok := registryEntryByBuiltinID(reg.Configs, BuiltinIDDefault)
	require.True(t, ok)
	assert.Equal(t, DefaultAlias, defaultEntry.Alias)
	assert.Equal(t, managedDefaultBundleDir, defaultEntry.Path)

	planOnlyEntry, ok := registryEntryByBuiltinID(reg.Configs, BuiltinIDPlanOnly)
	require.True(t, ok)
	assert.Equal(t, "builtin/plan-only", planOnlyEntry.Path)

	autonomousEntry, ok := registryEntryByBuiltinID(reg.Configs, BuiltinIDAutonomous)
	require.True(t, ok)
	assert.Equal(t, "builtin/autonomous", autonomousEntry.Path)

	yoloEntry, ok := registryEntryByBuiltinID(reg.Configs, BuiltinIDYolo)
	require.True(t, ok)
	assert.Equal(t, "builtin/yolo", yoloEntry.Path)
}

func TestLoadCatalogKeepsBuiltinsFirstAndAllowsBrokenUserBundles(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	taskConfigDir, err := TaskConfigDir()
	require.NoError(t, err)
	brokenDir := filepath.Join(taskConfigDir, "broken")
	require.NoError(t, os.MkdirAll(brokenDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(brokenDir, managedConfigFile), []byte("version: ["), 0o644))
	_, err = SaveRegistry(Registry{
		DefaultAlias: DefaultAlias,
		Configs: []RegistryEntry{
			{Alias: "broken", Path: "broken"},
		},
	})
	require.NoError(t, err)

	catalog, err := LoadCatalog()
	require.NoError(t, err)
	require.Len(t, catalog.Entries, 5)

	assert.Equal(t, BuiltinIDDefault, catalog.Entries[0].BuiltinID)
	assert.Equal(t, BuiltinIDPlanOnly, catalog.Entries[1].BuiltinID)
	assert.Equal(t, BuiltinIDAutonomous, catalog.Entries[2].BuiltinID)
	assert.Equal(t, BuiltinIDYolo, catalog.Entries[3].BuiltinID)
	assert.Equal(t, "broken", catalog.Entries[4].Alias)
	assert.False(t, catalog.Entries[4].Builtin)
	assert.Equal(t, filepath.Join(brokenDir, managedConfigFile), catalog.Entries[4].Path)
}

func TestLoadCatalogClassifiesMissingLegacyDefaultRowAsBuiltin(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	_, err := SaveRegistry(Registry{
		DefaultAlias: DefaultAlias,
		Configs: []RegistryEntry{
			{Alias: DefaultAlias, Path: managedDefaultBundleDir},
		},
	})
	require.NoError(t, err)

	catalog, err := LoadCatalog()
	require.NoError(t, err)
	defaultEntry, ok := catalog.Entry(DefaultAlias)
	require.True(t, ok)
	assert.True(t, defaultEntry.Builtin)
	assert.Equal(t, BuiltinIDDefault, defaultEntry.BuiltinID)

	reg, err := LoadRegistry()
	require.NoError(t, err)
	entry, ok := registryEntryByAlias(reg.Configs, DefaultAlias)
	require.True(t, ok)
	assert.Equal(t, BuiltinIDDefault, entry.BuiltinID)
}

func TestLoadCatalogStampsLegacyDefaultAsBuiltinAndPreservesCustomFiles(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	configPath := writeSimpleUserBundle(t, managedDefaultBundleDir, "custom-default")
	promptPath := filepath.Join(filepath.Dir(configPath), "prompt.md")
	require.NoError(t, os.WriteFile(promptPath, []byte("# custom default prompt"), 0o644))
	_, err := SaveRegistry(Registry{
		DefaultAlias: DefaultAlias,
		Configs: []RegistryEntry{
			{Alias: DefaultAlias, Path: managedDefaultBundleDir},
		},
	})
	require.NoError(t, err)

	catalog, err := LoadCatalog()
	require.NoError(t, err)
	require.Len(t, catalog.Entries, 4)

	defaultEntry, ok := catalog.Entry(DefaultAlias)
	require.True(t, ok)
	assert.True(t, defaultEntry.Builtin)
	data, err := os.ReadFile(promptPath)
	require.NoError(t, err)
	assert.Equal(t, "# custom default prompt", string(data))

	reg, err := LoadRegistry()
	require.NoError(t, err)
	entry, ok := registryEntryByAlias(reg.Configs, DefaultAlias)
	require.True(t, ok)
	assert.Equal(t, BuiltinIDDefault, entry.BuiltinID)
}

func TestLoadCatalogSeedsMissingBuiltinConfigAndFollowsPATH(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	setTaskConfigRuntimePath(t, "claude")

	taskConfigDir, err := TaskConfigDir()
	require.NoError(t, err)
	defaultDir := filepath.Join(taskConfigDir, managedDefaultBundleDir)
	require.NoError(t, os.MkdirAll(defaultDir, 0o755))

	catalog, err := LoadCatalog()
	require.NoError(t, err)
	defaultEntry, ok := catalog.Entry(DefaultAlias)
	require.True(t, ok)

	cfg, err := defaultEntry.LoadConfig()
	require.NoError(t, err)
	assert.Equal(t, appconfig.RuntimeClaudeCode, cfg.Runtime)

	currentBytes, err := builtinConfigBytes(BuiltinIDDefault)
	require.NoError(t, err)
	configBytes, err := os.ReadFile(defaultEntry.Path)
	require.NoError(t, err)
	assert.Equal(t, string(currentBytes), string(configBytes))
	assert.NotContains(t, string(configBytes), "runtime:")

	setTaskConfigRuntimePath(t, "codex")
	reloaded, err := LoadCatalog()
	require.NoError(t, err)
	reloadedEntry, ok := reloaded.Entry(DefaultAlias)
	require.True(t, ok)
	reloadedCfg, err := reloadedEntry.LoadConfig()
	require.NoError(t, err)
	assert.Equal(t, appconfig.RuntimeCodex, reloadedCfg.Runtime)
}

func TestLoadCatalogFallsBackWhenBuiltinAliasIsAlreadyUserOwned(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	writeSimpleUserBundle(t, "plan-only-user", "plan-only")
	_, err := SaveRegistry(Registry{
		DefaultAlias: DefaultAlias,
		Configs: []RegistryEntry{
			{Alias: "plan-only", Path: "plan-only-user"},
		},
	})
	require.NoError(t, err)

	catalog, err := LoadCatalog()
	require.NoError(t, err)

	builtinEntry, ok := catalog.Entry("builtin-plan-only")
	require.True(t, ok)
	assert.True(t, builtinEntry.Builtin)
	assert.Equal(t, BuiltinIDPlanOnly, builtinEntry.BuiltinID)

	userEntry, ok := catalog.Entry("plan-only")
	require.True(t, ok)
	assert.False(t, userEntry.Builtin)

	reg, err := LoadRegistry()
	require.NoError(t, err)
	builtinRow, ok := registryEntryByBuiltinID(reg.Configs, BuiltinIDPlanOnly)
	require.True(t, ok)
	assert.Equal(t, "builtin-plan-only", builtinRow.Alias)
	assert.Equal(t, "builtin/plan-only", builtinRow.Path)
}

func TestLoadCatalogFallsBackWhenBuiltinYoloAliasIsAlreadyUserOwned(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	writeSimpleUserBundle(t, "yolo-user", "yolo")
	_, err := SaveRegistry(Registry{
		DefaultAlias: DefaultAlias,
		Configs: []RegistryEntry{
			{Alias: "yolo", Path: "yolo-user"},
		},
	})
	require.NoError(t, err)

	catalog, err := LoadCatalog()
	require.NoError(t, err)

	builtinEntry, ok := catalog.Entry("builtin-yolo")
	require.True(t, ok)
	assert.True(t, builtinEntry.Builtin)
	assert.Equal(t, BuiltinIDYolo, builtinEntry.BuiltinID)

	userEntry, ok := catalog.Entry("yolo")
	require.True(t, ok)
	assert.False(t, userEntry.Builtin)

	reg, err := LoadRegistry()
	require.NoError(t, err)
	builtinRow, ok := registryEntryByBuiltinID(reg.Configs, BuiltinIDYolo)
	require.True(t, ok)
	assert.Equal(t, "builtin-yolo", builtinRow.Alias)
	assert.Equal(t, "builtin/yolo", builtinRow.Path)
}

func TestCloneConfigCreatesUserOwnedCopyFromBuiltin(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	catalog, err := LoadCatalog()
	require.NoError(t, err)
	source, ok := catalog.Entry(DefaultAlias)
	require.True(t, ok)

	cloned, err := CloneConfig("Review Copy", source.Path)
	require.NoError(t, err)
	entry, ok := cloned.Entry("Review Copy")
	require.True(t, ok)
	assert.False(t, entry.Builtin)
	assert.Equal(t, "review-copy", mustBundlePathForConfigPath(t, entry.Path))

	reg, err := LoadRegistry()
	require.NoError(t, err)
	row, ok := registryEntryByAlias(reg.Configs, "Review Copy")
	require.True(t, ok)
	assert.Empty(t, row.BuiltinID)
}

func TestCloneConfigCleansUpPartialBundleOnCopyFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission-based copy failure is platform-specific")
	}

	home := t.TempDir()
	t.Setenv("HOME", home)

	catalog, err := LoadCatalog()
	require.NoError(t, err)
	source, ok := catalog.Entry(DefaultAlias)
	require.True(t, ok)
	sourceDir := filepath.Dir(source.Path)
	unreadable := filepath.Join(sourceDir, "prompts", "blocked.md")
	require.NoError(t, os.WriteFile(unreadable, []byte("blocked"), 0o000))
	t.Cleanup(func() {
		_ = os.Chmod(unreadable, 0o644)
	})

	_, err = CloneConfig("review-copy", source.Path)
	require.Error(t, err)

	taskConfigDir, err := TaskConfigDir()
	require.NoError(t, err)
	assert.NoDirExists(t, filepath.Join(taskConfigDir, "review-copy"))

	reg, err := LoadRegistry()
	require.NoError(t, err)
	_, ok = registryEntryByAlias(reg.Configs, "review-copy")
	assert.False(t, ok)
}

func TestRenameConfigAliasMovesCanonicalUserDefaultAndReleasesBuiltinDefault(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	writeSimpleUserBundle(t, managedDefaultBundleDir, "custom-default")
	_, err := SaveRegistry(Registry{
		DefaultAlias: DefaultAlias,
		Configs: []RegistryEntry{
			{Alias: DefaultAlias, Path: managedDefaultBundleDir},
		},
	})
	require.NoError(t, err)

	catalog, err := RenameConfigAlias(DefaultAlias, "deep-review")
	require.NoError(t, err)
	assert.Equal(t, "deep-review", catalog.DefaultAlias)

	renamedEntry, ok := catalog.Entry("deep-review")
	require.True(t, ok)
	assert.False(t, renamedEntry.Builtin)
	assert.Equal(t, "deep-review", mustBundlePathForConfigPath(t, renamedEntry.Path))
	assert.FileExists(t, renamedEntry.Path)

	defaultEntry, ok := catalog.Entry(DefaultAlias)
	require.True(t, ok)
	assert.True(t, defaultEntry.Builtin)
	assert.FileExists(t, defaultEntry.Path)

	reg, err := LoadRegistry()
	require.NoError(t, err)
	assert.Equal(t, "deep-review", reg.DefaultAlias)
}

func TestRenameBuiltinConfigRejected(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	_, err := LoadCatalog()
	require.NoError(t, err)

	_, err = RenameConfigAlias(DefaultAlias, "renamed-default")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `task config alias "default" cannot be renamed`)
}

func TestSetDefaultConfigUpdatesRegistry(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	writeSimpleUserBundle(t, "reviewer", "reviewer")
	_, err := SaveRegistry(Registry{
		DefaultAlias: DefaultAlias,
		Configs: []RegistryEntry{
			{Alias: "reviewer", Path: "reviewer"},
		},
	})
	require.NoError(t, err)

	catalog, err := SetDefaultConfig("reviewer")
	require.NoError(t, err)
	assert.Equal(t, "reviewer", catalog.DefaultAlias)

	reg, err := LoadRegistry()
	require.NoError(t, err)
	assert.Equal(t, "reviewer", reg.DefaultAlias)
}

func TestSetConfigRuntimePinsBuiltinAndCustomConfigs(t *testing.T) {
	tests := []struct {
		name          string
		setup         func(t *testing.T)
		alias         string
		wantConfigYML string
	}{
		{
			name:          "builtin config",
			setup:         func(t *testing.T) {},
			alias:         DefaultAlias,
			wantConfigYML: "runtime: claude-code",
		},
		{
			name: "custom config",
			setup: func(t *testing.T) {
				writeSimpleUserBundle(t, "reviewer", "reviewer")
				_, err := SaveRegistry(Registry{
					DefaultAlias: DefaultAlias,
					Configs: []RegistryEntry{
						{Alias: "reviewer", Path: "reviewer"},
					},
				})
				require.NoError(t, err)
			},
			alias:         "reviewer",
			wantConfigYML: "runtime: claude-code",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			setTaskConfigRuntimePath(t, "codex")

			_, err := LoadCatalog()
			require.NoError(t, err)
			tt.setup(t)

			catalog, err := SetConfigRuntime(tt.alias, appconfig.RuntimeClaudeCode)
			require.NoError(t, err)
			entry, ok := catalog.Entry(tt.alias)
			require.True(t, ok)
			cfg, err := entry.LoadConfig()
			require.NoError(t, err)
			assert.Equal(t, appconfig.RuntimeClaudeCode, cfg.Runtime)

			data, err := os.ReadFile(entry.Path)
			require.NoError(t, err)
			assert.Contains(t, string(data), tt.wantConfigYML)

			setTaskConfigRuntimePath(t, "codex")
			reloaded, err := LoadCatalog()
			require.NoError(t, err)
			reloadedEntry, ok := reloaded.Entry(tt.alias)
			require.True(t, ok)
			reloadedCfg, err := reloadedEntry.LoadConfig()
			require.NoError(t, err)
			assert.Equal(t, appconfig.RuntimeClaudeCode, reloadedCfg.Runtime)
		})
	}
}

func TestSetConfigRuntimeRejectsInvalidConfigWithoutMutatingFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	setTaskConfigRuntimePath(t, "codex")

	taskConfigDir, err := TaskConfigDir()
	require.NoError(t, err)
	brokenDir := filepath.Join(taskConfigDir, "broken")
	require.NoError(t, os.MkdirAll(brokenDir, 0o755))
	configPath := filepath.Join(brokenDir, managedConfigFile)
	require.NoError(t, os.WriteFile(configPath, []byte("version: ["), 0o644))
	_, err = SaveRegistry(Registry{
		DefaultAlias: DefaultAlias,
		Configs: []RegistryEntry{
			{Alias: "broken", Path: "broken"},
		},
	})
	require.NoError(t, err)

	before, err := os.ReadFile(configPath)
	require.NoError(t, err)

	_, err = SetConfigRuntime("broken", appconfig.RuntimeClaudeCode)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `load task config "broken"`)

	after, err := os.ReadFile(configPath)
	require.NoError(t, err)
	assert.Equal(t, string(before), string(after))
}

func TestDeleteConfigReleasesCustomDefaultAndRestoresBuiltinDefault(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	writeSimpleUserBundle(t, managedDefaultBundleDir, "custom-default")
	_, err := SaveRegistry(Registry{
		DefaultAlias: DefaultAlias,
		Configs: []RegistryEntry{
			{Alias: DefaultAlias, Path: managedDefaultBundleDir},
		},
	})
	require.NoError(t, err)

	catalog, err := DeleteConfig(DefaultAlias)
	require.NoError(t, err)
	assert.Equal(t, DefaultAlias, catalog.DefaultAlias)

	defaultEntry, ok := catalog.Entry(DefaultAlias)
	require.True(t, ok)
	assert.True(t, defaultEntry.Builtin)
	assert.FileExists(t, defaultEntry.Path)

	reg, err := LoadRegistry()
	require.NoError(t, err)
	entry, ok := registryEntryByBuiltinID(reg.Configs, BuiltinIDDefault)
	require.True(t, ok)
	assert.Equal(t, DefaultAlias, entry.Alias)
}

func TestDeleteBuiltinConfigRejected(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	_, err := LoadCatalog()
	require.NoError(t, err)

	_, err = DeleteConfig(BuiltinIDAutonomous)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `task config alias "autonomous" cannot be deleted`)
}

func TestLoadRegistryRejectsInvalidPaths(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	regPath, err := RegistryPath()
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Dir(regPath), 0o755))
	require.NoError(t, os.WriteFile(regPath, []byte(`{
  "configs": [
    { "alias": "oops", "path": "../outside" }
  ]
}`), 0o600))

	_, err = LoadRegistry()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must stay within taskconfigs directory")
}

func TestLoadRegistryRejectsDuplicateBundlePaths(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	regPath, err := RegistryPath()
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Dir(regPath), 0o755))
	require.NoError(t, os.WriteFile(regPath, []byte(`{
  "configs": [
    { "alias": "alpha", "path": "shared" },
    { "alias": "beta", "path": "shared" }
  ]
}`), 0o600))

	_, err = LoadRegistry()
	require.Error(t, err)
	assert.Contains(t, err.Error(), `task config path "shared" is duplicated`)
}

func TestBundlePathForConfigPathReturnsRelativeBundlePath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	configPath := writeSimpleUserBundle(t, "reviewer", "reviewer")
	bundlePath, err := BundlePathForConfigPath(configPath)
	require.NoError(t, err)
	assert.Equal(t, "reviewer", bundlePath)
}

func writeSimpleUserBundle(t *testing.T, bundlePath, alias string) string {
	t.Helper()
	taskConfigDir, err := TaskConfigDir()
	require.NoError(t, err)
	destDir := filepath.Join(taskConfigDir, filepath.FromSlash(bundlePath))
	require.NoError(t, os.MkdirAll(destDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(destDir, managedConfigFile), []byte(simpleUserConfigYAML(alias)), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(destDir, "prompt.md"), []byte("# prompt"), 0o644))
	return filepath.Join(destDir, managedConfigFile)
}

func simpleUserConfigYAML(alias string) string {
	return `version: 1
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
      required: []
      properties: {}
  done:
    type: terminal
`
}

func mustBundlePathForConfigPath(t *testing.T, configPath string) string {
	t.Helper()
	bundlePath, err := BundlePathForConfigPath(configPath)
	require.NoError(t, err)
	return bundlePath
}

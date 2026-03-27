package taskconfig

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadCatalogWithoutRegistryReturnsBuiltinDefault(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	catalog, err := LoadCatalog()
	require.NoError(t, err)
	require.Len(t, catalog.Entries, 1)
	assert.Equal(t, builtinDefaultAlias, catalog.DefaultAlias)
	assert.Equal(t, builtinDefaultAlias, catalog.Entries[0].Alias)
	defaultPath, err := DefaultConfigPath()
	require.NoError(t, err)
	assert.Equal(t, defaultPath, catalog.Entries[0].Path)
	assert.FileExists(t, catalog.Entries[0].Path)
	assert.FileExists(t, filepath.Join(filepath.Dir(defaultPath), "prompts", "upsert_plan.md"))

	reg, err := LoadRegistry()
	require.NoError(t, err)
	require.Len(t, reg.Configs, 1)
	assert.Equal(t, builtinDefaultAlias, reg.DefaultAlias)
	assert.Equal(t, builtinDefaultAlias, reg.Configs[0].Alias)
	assert.Equal(t, managedDefaultBundleDir, reg.Configs[0].Path)
}

func TestLoadCatalogIncludesRegistryEntries(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	regPath, err := RegistryPath()
	require.NoError(t, err)
	taskConfigDir, err := TaskConfigDir()
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(taskConfigDir, 0o755))

	bundleDir := filepath.Join(taskConfigDir, "bugfix")
	require.NoError(t, os.MkdirAll(bundleDir, 0o755))
	configPath := filepath.Join(bundleDir, managedConfigFile)
	require.NoError(t, os.WriteFile(configPath, []byte(`
version: 1
runtime: claude-code
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
    result_schema:
      type: object
      additionalProperties: false
      required: []
      properties: {}
`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(bundleDir, "prompt.md"), []byte("# prompt"), 0o644))
	require.NoError(t, os.WriteFile(regPath, []byte(`{
  "default_alias": "bugfix",
  "configs": [
    { "alias": "default", "path": "default" },
    { "alias": "bugfix", "path": "bugfix" }
  ]
}`), 0o600))

	catalog, err := LoadCatalog()
	require.NoError(t, err)
	require.Len(t, catalog.Entries, 2)
	assert.Equal(t, "bugfix", catalog.DefaultAlias)

	entry, ok := catalog.Entry("bugfix")
	require.True(t, ok)
	assert.Equal(t, configPath, entry.Path)
}

func TestLoadCatalogAllowsBrokenUserConfigFiles(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	regPath, err := RegistryPath()
	require.NoError(t, err)
	taskConfigDir, err := TaskConfigDir()
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(taskConfigDir, 0o755))
	bundleDir := filepath.Join(taskConfigDir, "broken")
	require.NoError(t, os.MkdirAll(bundleDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(bundleDir, managedConfigFile), []byte("version: ["), 0o644))
	require.NoError(t, os.WriteFile(regPath, []byte(`{
  "default_alias": "broken",
  "configs": [
    { "alias": "default", "path": "default" },
    { "alias": "broken", "path": "broken" }
  ]
}`), 0o600))

	catalog, err := LoadCatalog()
	require.NoError(t, err)
	entry, ok := catalog.Entry("broken")
	require.True(t, ok)
	assert.Equal(t, filepath.Join(bundleDir, managedConfigFile), entry.Path)
	assert.Nil(t, entry.Config)
}

func TestLoadCatalogFallsBackToDefaultWhenRegistryDefaultAliasIsMissing(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	regPath, err := RegistryPath()
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Dir(regPath), 0o755))
	require.NoError(t, os.WriteFile(regPath, []byte(`{
  "default_alias": "missing",
  "configs": [
    { "alias": "default", "path": "default" },
    { "alias": "reviewer", "path": "reviewer" }
  ]
}`), 0o600))

	catalog, err := LoadCatalog()
	require.NoError(t, err)
	assert.Equal(t, builtinDefaultAlias, catalog.DefaultAlias)
}

func TestLoadCatalogRepairsManagedDefaultRegistryPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	regPath, err := RegistryPath()
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Dir(regPath), 0o755))
	require.NoError(t, os.WriteFile(regPath, []byte(`{
  "default_alias": "default",
  "configs": [
    { "alias": "default", "path": "custom-default" },
    { "alias": "reviewer", "path": "reviewer" }
  ]
}`), 0o600))

	catalog, err := LoadCatalog()
	require.NoError(t, err)
	entry, ok := catalog.Entry(DefaultAlias)
	require.True(t, ok)

	defaultPath, err := DefaultConfigPath()
	require.NoError(t, err)
	assert.Equal(t, defaultPath, entry.Path)

	reg, err := LoadRegistry()
	require.NoError(t, err)
	defEntry, ok := registryEntryByAlias(reg.Configs, DefaultAlias)
	require.True(t, ok)
	assert.Equal(t, managedDefaultBundleDir, defEntry.Path)
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

func TestLoadRegistryRejectsYAMLFilePaths(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	regPath, err := RegistryPath()
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Dir(regPath), 0o755))
	require.NoError(t, os.WriteFile(regPath, []byte(`{
  "configs": [
    { "alias": "oops", "path": "oops/config.yaml" }
  ]
}`), 0o600))

	_, err = LoadRegistry()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "path must point to a bundle directory")
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

func TestLoadRegistryRejectsCaseInsensitiveDuplicateBundlePaths(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	regPath, err := RegistryPath()
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Dir(regPath), 0o755))
	require.NoError(t, os.WriteFile(regPath, []byte(`{
  "configs": [
    { "alias": "alpha", "path": "Reviewer" },
    { "alias": "beta", "path": "reviewer" }
  ]
}`), 0o600))

	_, err = LoadRegistry()
	require.Error(t, err)
	assert.Contains(t, err.Error(), `task config path "reviewer" is duplicated`)
}

func TestSaveRegistryNormalizesOrdering(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	path, err := SaveRegistry(Registry{
		DefaultAlias: "beta",
		Configs: []RegistryEntry{
			{Alias: builtinDefaultAlias, Path: managedDefaultBundleDir},
			{Alias: "beta", Path: "b"},
			{Alias: "alpha", Path: "a"},
		},
	})
	require.NoError(t, err)
	assert.FileExists(t, path)

	reg, err := LoadRegistry()
	require.NoError(t, err)
	require.Len(t, reg.Configs, 3)
	assert.Equal(t, "alpha", reg.Configs[0].Alias)
	assert.Equal(t, "beta", reg.Configs[1].Alias)
	assert.Equal(t, builtinDefaultAlias, reg.Configs[2].Alias)
	assert.Equal(t, "beta", reg.DefaultAlias)
}

func TestCloneConfigCreatesUniqueBundleAndRegistryEntry(t *testing.T) {
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
	assert.Equal(t, "review-copy", mustBundlePathForConfigPath(t, entry.Path))
	assert.FileExists(t, entry.Path)
	assert.FileExists(t, filepath.Join(filepath.Dir(entry.Path), "prompts", "upsert_plan.md"))

	_, err = CloneConfig("Review Copy!", source.Path)
	require.NoError(t, err)

	reg, err := LoadRegistry()
	require.NoError(t, err)
	require.Len(t, reg.Configs, 3)
	first, ok := registryEntryByAlias(reg.Configs, "Review Copy")
	require.True(t, ok)
	assert.Equal(t, "review-copy", first.Path)
	second, ok := registryEntryByAlias(reg.Configs, "Review Copy!")
	require.True(t, ok)
	assert.Equal(t, "review-copy-2", second.Path)
}

func TestCloneConfigRejectsNonManagedSourcePaths(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	randomPath := filepath.Join(t.TempDir(), "random.yaml")
	require.NoError(t, os.WriteFile(randomPath, []byte("version: 1"), 0o644))

	_, err := CloneConfig("review-copy", randomPath)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must point to config.yaml")
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

func TestRenameConfigAliasUpdatesDefaultAliasWithoutMovingBundle(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	configPath := writeManagedBundle(t, "reviewer")
	_, err := SaveRegistry(Registry{
		DefaultAlias: "reviewer",
		Configs: []RegistryEntry{
			{Alias: DefaultAlias, Path: managedDefaultBundleDir},
			{Alias: "reviewer", Path: "reviewer"},
		},
	})
	require.NoError(t, err)

	catalog, err := RenameConfigAlias("reviewer", "deep-review")
	require.NoError(t, err)
	assert.Equal(t, "deep-review", catalog.DefaultAlias)

	entry, ok := catalog.Entry("deep-review")
	require.True(t, ok)
	assert.Equal(t, configPath, entry.Path)
	assert.Equal(t, "reviewer", mustBundlePathForConfigPath(t, entry.Path))

	reg, err := LoadRegistry()
	require.NoError(t, err)
	assert.Equal(t, "deep-review", reg.DefaultAlias)
	_, ok = registryEntryByAlias(reg.Configs, "deep-review")
	assert.True(t, ok)
}

func TestRenameConfigAliasRejectsManagedDefault(t *testing.T) {
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

	writeManagedBundle(t, "reviewer")
	_, err := SaveRegistry(Registry{
		DefaultAlias: DefaultAlias,
		Configs: []RegistryEntry{
			{Alias: DefaultAlias, Path: managedDefaultBundleDir},
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

func TestDeleteConfigRemovesRegistryEntryAndBundle(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	configPath := writeManagedBundle(t, "reviewer")
	_, err := SaveRegistry(Registry{
		DefaultAlias: "reviewer",
		Configs: []RegistryEntry{
			{Alias: DefaultAlias, Path: managedDefaultBundleDir},
			{Alias: "reviewer", Path: "reviewer"},
		},
	})
	require.NoError(t, err)

	catalog, err := DeleteConfig("reviewer")
	require.NoError(t, err)
	assert.Equal(t, DefaultAlias, catalog.DefaultAlias)
	_, ok := catalog.Entry("reviewer")
	assert.False(t, ok)
	assert.NoFileExists(t, filepath.Dir(configPath))

	reg, err := LoadRegistry()
	require.NoError(t, err)
	assert.Equal(t, DefaultAlias, reg.DefaultAlias)
	require.Len(t, reg.Configs, 1)
}

func TestDeleteConfigRejectsManagedDefault(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	_, err := LoadCatalog()
	require.NoError(t, err)

	_, err = DeleteConfig(DefaultAlias)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `task config alias "default" cannot be deleted`)
}

func TestBundlePathForConfigPathReturnsRelativeBundlePath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	configPath := writeManagedBundle(t, "reviewer")
	bundlePath, err := BundlePathForConfigPath(configPath)
	require.NoError(t, err)
	assert.Equal(t, "reviewer", bundlePath)
}

func writeManagedBundle(t *testing.T, bundlePath string) string {
	t.Helper()
	defaultConfigPath, err := EnsureManagedDefaultAssets()
	require.NoError(t, err)
	taskConfigDir, err := TaskConfigDir()
	require.NoError(t, err)

	destDir := filepath.Join(taskConfigDir, filepath.FromSlash(bundlePath))
	require.NoError(t, copyDir(filepath.Dir(defaultConfigPath), destDir))
	return filepath.Join(destDir, managedConfigFile)
}

func mustBundlePathForConfigPath(t *testing.T, configPath string) string {
	t.Helper()
	bundlePath, err := BundlePathForConfigPath(configPath)
	require.NoError(t, err)
	return bundlePath
}

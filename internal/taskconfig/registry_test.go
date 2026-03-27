package taskconfig

import (
	"os"
	"path/filepath"
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

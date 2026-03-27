package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	appconfig "github.com/LaLanMo/muxagent-cli/internal/config"
	"github.com/LaLanMo/muxagent-cli/internal/taskconfig"
	cliversion "github.com/LaLanMo/muxagent-cli/internal/version"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRootVersionFlag(t *testing.T) {
	cmd := NewRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"--version"})

	err := cmd.Execute()
	require.NoError(t, err)
	assert.Equal(t, cliversion.CLIString()+"\n", out.String())
}

func TestRootVersionShorthandFlag(t *testing.T) {
	cmd := NewRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"-v"})

	err := cmd.Execute()
	require.NoError(t, err)
	assert.Equal(t, cliversion.CLIString()+"\n", out.String())
}

func TestVersionCommand(t *testing.T) {
	cmd := NewRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"version"})

	err := cmd.Execute()
	require.NoError(t, err)
	assert.Equal(t, cliversion.CLIString()+"\n", out.String())
}

func TestVersionCommandRejectsExtraArgs(t *testing.T) {
	cmd := NewRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"version", "extra"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown command \"extra\" for \"muxagent version\"")
}

func TestRootHelpIncludesVersionEntryPoints(t *testing.T) {
	cmd := NewRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"--help"})

	err := cmd.Execute()
	require.NoError(t, err)
	assert.Contains(t, out.String(), "version     Show muxagent version")
	assert.Contains(t, out.String(), "-v, --version")
}

func TestRootHelpOmitsCompletionCommand(t *testing.T) {
	cmd := NewRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"--help"})

	err := cmd.Execute()
	require.NoError(t, err)
	assert.NotContains(t, out.String(), "completion")
	assert.Contains(t, out.String(), "auth")
	assert.Contains(t, out.String(), "daemon")
	assert.Contains(t, out.String(), "health")
}

func TestCompletionCommandUnavailable(t *testing.T) {
	cmd := NewRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"completion"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown command \"completion\" for \"muxagent\"")
}

func TestRootLaunchesTaskTUIOnBareInvocation(t *testing.T) {
	var (
		called     bool
		gotWorkDir string
	)
	cmd := newRootCmd(rootOptions{
		launchTUI: func(ctx context.Context, workDir string, launch taskTUILaunchOptions) error {
			called = true
			gotWorkDir = workDir
			return nil
		},
	})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(nil)

	err := cmd.Execute()
	require.NoError(t, err)
	require.True(t, called)
	assert.NotEmpty(t, gotWorkDir)
}

func TestRootRejectsRemovedConfigFlag(t *testing.T) {
	cmd := NewRootCmd()
	cmd.SetArgs([]string{"-c", "./my-config.yaml"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown shorthand flag: 'c'")
}

func TestRootPassesRuntimeOverrideToTaskTUI(t *testing.T) {
	var gotRuntime appconfig.RuntimeID
	cmd := newRootCmd(rootOptions{
		launchTUI: func(ctx context.Context, workDir string, launch taskTUILaunchOptions) error {
			gotRuntime = launch.Runtime
			return nil
		},
	})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"--runtime", "claude-code"})

	err := cmd.Execute()
	require.NoError(t, err)
	assert.Equal(t, appconfig.RuntimeClaudeCode, gotRuntime)
}

func TestRootRejectsInvalidRuntime(t *testing.T) {
	cmd := newRootCmd(rootOptions{
		launchTUI: func(ctx context.Context, workDir string, launch taskTUILaunchOptions) error {
			return nil
		},
	})
	cmd.SetArgs([]string{"--runtime", "bogus"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), `runtime "bogus" is not supported`)
}

func TestRootPropagatesTaskTUILaunchError(t *testing.T) {
	cmd := newRootCmd(rootOptions{
		launchTUI: func(ctx context.Context, workDir string, launch taskTUILaunchOptions) error {
			return errors.New("boom")
		},
	})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(nil)

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "boom")
}

func TestRootLaunchesTaskTUIWithWorktreeAvailabilityInGitRepo(t *testing.T) {
	repo := initRootTestGitRepo(t)
	t.Setenv("HOME", t.TempDir())
	prevWD, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(repo))
	t.Cleanup(func() {
		require.NoError(t, os.Chdir(prevWD))
	})

	var got taskTUILaunchOptions
	cmd := newRootCmd(rootOptions{
		launchTUI: func(ctx context.Context, workDir string, launch taskTUILaunchOptions) error {
			got = launch
			return nil
		},
	})

	err = cmd.Execute()
	require.NoError(t, err)
	assert.True(t, got.WorktreeAvailable)
	assert.False(t, got.DefaultUseWorktree)
}

func TestRootLaunchesTaskTUIWithoutWorktreeOutsideGitRepo(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	prevWD, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() {
		require.NoError(t, os.Chdir(prevWD))
	})

	_, err = appconfig.SaveTaskLaunchPreferences(appconfig.TaskLaunchPreferences{UseWorktree: true})
	require.NoError(t, err)

	var got taskTUILaunchOptions
	cmd := newRootCmd(rootOptions{
		launchTUI: func(ctx context.Context, workDir string, launch taskTUILaunchOptions) error {
			got = launch
			return nil
		},
	})

	err = cmd.Execute()
	require.NoError(t, err)
	assert.False(t, got.WorktreeAvailable)
	assert.False(t, got.DefaultUseWorktree)
}

func TestRootIgnoresMalformedTaskLaunchPreferences(t *testing.T) {
	repo := initRootTestGitRepo(t)
	home := t.TempDir()
	t.Setenv("HOME", home)

	prefsPath, err := appconfig.TaskLaunchPreferencesPath()
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Dir(prefsPath), 0o755))
	require.NoError(t, os.WriteFile(prefsPath, []byte("{"), 0o600))

	prevWD, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(repo))
	t.Cleanup(func() {
		require.NoError(t, os.Chdir(prevWD))
	})

	called := false
	cmd := newRootCmd(rootOptions{
		launchTUI: func(ctx context.Context, workDir string, launch taskTUILaunchOptions) error {
			called = true
			assert.True(t, launch.WorktreeAvailable)
			assert.False(t, launch.DefaultUseWorktree)
			return nil
		},
	})

	err = cmd.Execute()
	require.NoError(t, err)
	assert.True(t, called)
}

func TestLoadTaskConfigCatalogUsesRegistryDefault(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	registryPath, err := taskconfig.RegistryPath()
	require.NoError(t, err)
	taskConfigDir, err := taskconfig.TaskConfigDir()
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(taskConfigDir, 0o755))

	bundleDir := filepath.Join(taskConfigDir, "bugfix")
	require.NoError(t, os.MkdirAll(bundleDir, 0o755))
	configPath := filepath.Join(bundleDir, "config.yaml")
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
	require.NoError(t, os.WriteFile(registryPath, []byte(`{
  "default_alias": "bugfix",
  "configs": [
    { "alias": "default", "path": "default" },
    { "alias": "bugfix", "path": "bugfix" }
  ]
}`), 0o600))

	catalog, err := loadTaskConfigCatalog()
	require.NoError(t, err)
	assert.Equal(t, "bugfix", catalog.DefaultAlias)
	entry, ok := catalog.Entry("bugfix")
	require.True(t, ok)
	assert.Equal(t, configPath, entry.Path)
	assert.Nil(t, entry.Config)
}

func TestLoadTaskConfigCatalogAllowsBrokenRegistryConfigAtStartup(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	registryPath, err := taskconfig.RegistryPath()
	require.NoError(t, err)
	taskConfigDir, err := taskconfig.TaskConfigDir()
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(taskConfigDir, 0o755))
	bundleDir := filepath.Join(taskConfigDir, "broken")
	require.NoError(t, os.MkdirAll(bundleDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(bundleDir, "config.yaml"), []byte("version: ["), 0o644))
	require.NoError(t, os.WriteFile(registryPath, []byte(`{
  "default_alias": "broken",
  "configs": [
    { "alias": "default", "path": "default" },
    { "alias": "broken", "path": "broken" }
  ]
}`), 0o600))

	catalog, err := loadTaskConfigCatalog()
	require.NoError(t, err)
	assert.Equal(t, "broken", catalog.DefaultAlias)
	entry, ok := catalog.Entry("broken")
	require.True(t, ok)
	assert.Equal(t, filepath.Join(bundleDir, "config.yaml"), entry.Path)
}

func initRootTestGitRepo(t *testing.T) string {
	t.Helper()

	repo := t.TempDir()
	runRootGit(t, repo, "git", "init")
	runRootGit(t, repo, "git", "config", "user.email", "test@test.com")
	runRootGit(t, repo, "git", "config", "user.name", "Test")
	return repo
}

func runRootGit(t *testing.T, dir string, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "command %s %v failed: %s", name, args, string(out))
}

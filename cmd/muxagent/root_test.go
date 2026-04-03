package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/LaLanMo/muxagent-cli/cmd/muxagent/update"
	appconfig "github.com/LaLanMo/muxagent-cli/internal/config"
	"github.com/LaLanMo/muxagent-cli/internal/taskconfig"
	cliversion "github.com/LaLanMo/muxagent-cli/internal/version"
	"github.com/spf13/cobra"
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
	assert.NotContains(t, out.String(), "acp-test")
	assert.Contains(t, out.String(), "app-server")
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

func TestRootSkipsStartupUpdateWhenNotInteractive(t *testing.T) {
	originalVersion := cliversion.Version
	cliversion.Version = "v1.0.0"
	t.Cleanup(func() {
		cliversion.Version = originalVersion
	})

	checkCalled := false
	launchCalled := false
	cmd := newRootCmd(rootOptions{
		isInteractive: func(cmd *cobra.Command) bool { return false },
		checkForStartupUpdate: func(ctx context.Context) (update.StartupCheckResult, error) {
			checkCalled = true
			return update.StartupCheckResult{}, nil
		},
		launchTUI: func(ctx context.Context, workDir string, launch taskTUILaunchOptions) error {
			launchCalled = true
			return nil
		},
	})

	err := cmd.Execute()
	require.NoError(t, err)
	assert.False(t, checkCalled)
	assert.True(t, launchCalled)
}

func TestRootSkipsStartupUpdateForDevBuild(t *testing.T) {
	checkCalled := false
	cmd := newRootCmd(rootOptions{
		isInteractive: func(cmd *cobra.Command) bool { return true },
		checkForStartupUpdate: func(ctx context.Context) (update.StartupCheckResult, error) {
			checkCalled = true
			return update.StartupCheckResult{}, nil
		},
		launchTUI: func(ctx context.Context, workDir string, launch taskTUILaunchOptions) error {
			return nil
		},
	})

	err := cmd.Execute()
	require.NoError(t, err)
	assert.False(t, checkCalled)
}

func TestRootSkipsStartupUpdateWithinCadence(t *testing.T) {
	originalVersion := cliversion.Version
	cliversion.Version = "v1.0.0"
	t.Cleanup(func() {
		cliversion.Version = originalVersion
	})

	now := time.Date(2026, time.March, 31, 12, 0, 0, 0, time.UTC)
	checkCalled := false
	cmd := newRootCmd(rootOptions{
		now:           func() time.Time { return now },
		isInteractive: func(cmd *cobra.Command) bool { return true },
		loadStartupUpdateState: func() appconfig.StartupUpdateState {
			return appconfig.StartupUpdateState{LastCheckedAt: now.Add(-time.Hour)}
		},
		checkForStartupUpdate: func(ctx context.Context) (update.StartupCheckResult, error) {
			checkCalled = true
			return update.StartupCheckResult{}, nil
		},
		launchTUI: func(ctx context.Context, workDir string, launch taskTUILaunchOptions) error {
			return nil
		},
	})

	err := cmd.Execute()
	require.NoError(t, err)
	assert.False(t, checkCalled)
}

func TestRootSkipsStartupUpdateWithinFailureBackoff(t *testing.T) {
	originalVersion := cliversion.Version
	cliversion.Version = "v1.0.0"
	t.Cleanup(func() {
		cliversion.Version = originalVersion
	})

	now := time.Date(2026, time.March, 31, 12, 0, 0, 0, time.UTC)
	checkCalled := false
	cmd := newRootCmd(rootOptions{
		now:           func() time.Time { return now },
		isInteractive: func(cmd *cobra.Command) bool { return true },
		loadStartupUpdateState: func() appconfig.StartupUpdateState {
			return appconfig.StartupUpdateState{LastFailedAt: now.Add(-time.Hour)}
		},
		checkForStartupUpdate: func(ctx context.Context) (update.StartupCheckResult, error) {
			checkCalled = true
			return update.StartupCheckResult{}, nil
		},
		launchTUI: func(ctx context.Context, workDir string, launch taskTUILaunchOptions) error {
			return nil
		},
	})

	err := cmd.Execute()
	require.NoError(t, err)
	assert.False(t, checkCalled)
}

func TestRootWarnsOnStartupUpdateCheckFailureButStillLaunches(t *testing.T) {
	originalVersion := cliversion.Version
	cliversion.Version = "v1.0.0"
	t.Cleanup(func() {
		cliversion.Version = originalVersion
	})

	now := time.Date(2026, time.March, 31, 12, 0, 0, 0, time.UTC)
	var saved appconfig.StartupUpdateState
	launchCalled := false
	cmd := newRootCmd(rootOptions{
		now:           func() time.Time { return now },
		isInteractive: func(cmd *cobra.Command) bool { return true },
		checkForStartupUpdate: func(ctx context.Context) (update.StartupCheckResult, error) {
			return update.StartupCheckResult{}, errors.New("network down")
		},
		saveStartupUpdateState: func(state appconfig.StartupUpdateState) (string, error) {
			saved = state
			return "", nil
		},
		launchTUI: func(ctx context.Context, workDir string, launch taskTUILaunchOptions) error {
			launchCalled = true
			return nil
		},
	})
	var errOut bytes.Buffer
	cmd.SetErr(&errOut)

	err := cmd.Execute()
	require.NoError(t, err)
	assert.True(t, launchCalled)
	assert.True(t, saved.LastFailedAt.Equal(now))
	assert.Contains(t, errOut.String(), "Warning: Failed to check for updates")
}

func TestRootLaterPersistsStartupUpdateState(t *testing.T) {
	originalVersion := cliversion.Version
	cliversion.Version = "v1.0.0"
	t.Cleanup(func() {
		cliversion.Version = originalVersion
	})

	now := time.Date(2026, time.March, 31, 12, 0, 0, 0, time.UTC)
	var saved appconfig.StartupUpdateState
	cmd := newRootCmd(rootOptions{
		now:           func() time.Time { return now },
		isInteractive: func(cmd *cobra.Command) bool { return true },
		checkForStartupUpdate: func(ctx context.Context) (update.StartupCheckResult, error) {
			return update.StartupCheckResult{CurrentVersion: "v1.0.0", LatestVersion: "v1.2.0", UpdateAvailable: true}, nil
		},
		promptForStartupUpdate: func(cmd *cobra.Command, result update.StartupCheckResult) startupUpdateChoice {
			return startupUpdateChoiceLater
		},
		saveStartupUpdateState: func(state appconfig.StartupUpdateState) (string, error) {
			saved = state
			return "", nil
		},
		launchTUI: func(ctx context.Context, workDir string, launch taskTUILaunchOptions) error {
			return nil
		},
	})

	err := cmd.Execute()
	require.NoError(t, err)
	assert.True(t, saved.LastCheckedAt.Equal(now))
	assert.True(t, saved.LastFailedAt.IsZero())
	assert.Empty(t, saved.SkippedVersion)
}

func TestRootSkipVersionPersistsStartupUpdateState(t *testing.T) {
	originalVersion := cliversion.Version
	cliversion.Version = "v1.0.0"
	t.Cleanup(func() {
		cliversion.Version = originalVersion
	})

	now := time.Date(2026, time.March, 31, 12, 0, 0, 0, time.UTC)
	var saved appconfig.StartupUpdateState
	cmd := newRootCmd(rootOptions{
		now:           func() time.Time { return now },
		isInteractive: func(cmd *cobra.Command) bool { return true },
		checkForStartupUpdate: func(ctx context.Context) (update.StartupCheckResult, error) {
			return update.StartupCheckResult{CurrentVersion: "v1.0.0", LatestVersion: "v1.2.0", UpdateAvailable: true}, nil
		},
		promptForStartupUpdate: func(cmd *cobra.Command, result update.StartupCheckResult) startupUpdateChoice {
			return startupUpdateChoiceSkipVersion
		},
		saveStartupUpdateState: func(state appconfig.StartupUpdateState) (string, error) {
			saved = state
			return "", nil
		},
		launchTUI: func(ctx context.Context, workDir string, launch taskTUILaunchOptions) error {
			return nil
		},
	})

	err := cmd.Execute()
	require.NoError(t, err)
	assert.True(t, saved.LastCheckedAt.Equal(now))
	assert.Equal(t, "v1.2.0", saved.SkippedVersion)
}

func TestRootWarnsWhenStartupUpdateInstallFails(t *testing.T) {
	originalVersion := cliversion.Version
	cliversion.Version = "v1.0.0"
	t.Cleanup(func() {
		cliversion.Version = originalVersion
	})

	now := time.Date(2026, time.March, 31, 12, 0, 0, 0, time.UTC)
	var saved appconfig.StartupUpdateState
	cmd := newRootCmd(rootOptions{
		now:           func() time.Time { return now },
		isInteractive: func(cmd *cobra.Command) bool { return true },
		checkForStartupUpdate: func(ctx context.Context) (update.StartupCheckResult, error) {
			return update.StartupCheckResult{CurrentVersion: "v1.0.0", LatestVersion: "v1.2.0", UpdateAvailable: true}, nil
		},
		promptForStartupUpdate: func(cmd *cobra.Command, result update.StartupCheckResult) startupUpdateChoice {
			return startupUpdateChoiceUpdateNow
		},
		installStartupUpdate: func(ctx context.Context, latest string) error {
			return errors.New("install boom")
		},
		saveStartupUpdateState: func(state appconfig.StartupUpdateState) (string, error) {
			saved = state
			return "", nil
		},
		launchTUI: func(ctx context.Context, workDir string, launch taskTUILaunchOptions) error {
			return nil
		},
	})
	var errOut bytes.Buffer
	cmd.SetErr(&errOut)

	err := cmd.Execute()
	require.NoError(t, err)
	assert.True(t, saved.LastFailedAt.Equal(now))
	assert.Contains(t, errOut.String(), "Warning: Automatic update failed")
}

func TestRootStartupResumeSuccessPersistsCheckedState(t *testing.T) {
	originalVersion := cliversion.Version
	cliversion.Version = "v1.0.0"
	t.Cleanup(func() {
		cliversion.Version = originalVersion
	})

	now := time.Date(2026, time.March, 31, 12, 0, 0, 0, time.UTC)
	var saved appconfig.StartupUpdateState
	checkCalled := false
	cmd := newRootCmd(rootOptions{
		now:           func() time.Time { return now },
		isInteractive: func(cmd *cobra.Command) bool { return true },
		consumeStartupResumeOutcome: func() update.StartupResumeOutcome {
			return update.StartupResumeOutcome{Resumed: true, Version: "v1.2.0"}
		},
		checkForStartupUpdate: func(ctx context.Context) (update.StartupCheckResult, error) {
			checkCalled = true
			return update.StartupCheckResult{}, nil
		},
		saveStartupUpdateState: func(state appconfig.StartupUpdateState) (string, error) {
			saved = state
			return "", nil
		},
		launchTUI: func(ctx context.Context, workDir string, launch taskTUILaunchOptions) error {
			return nil
		},
	})

	err := cmd.Execute()
	require.NoError(t, err)
	assert.False(t, checkCalled)
	assert.True(t, saved.LastCheckedAt.Equal(now))
	assert.Empty(t, saved.SkippedVersion)
}

func TestRootWarnsWhenStartupUpdateStateSaveFails(t *testing.T) {
	originalVersion := cliversion.Version
	cliversion.Version = "v1.0.0"
	t.Cleanup(func() {
		cliversion.Version = originalVersion
	})

	now := time.Date(2026, time.March, 31, 12, 0, 0, 0, time.UTC)
	cmd := newRootCmd(rootOptions{
		now:           func() time.Time { return now },
		isInteractive: func(cmd *cobra.Command) bool { return true },
		checkForStartupUpdate: func(ctx context.Context) (update.StartupCheckResult, error) {
			return update.StartupCheckResult{CurrentVersion: "v1.0.0", LatestVersion: "v1.2.0", UpdateAvailable: true}, nil
		},
		promptForStartupUpdate: func(cmd *cobra.Command, result update.StartupCheckResult) startupUpdateChoice {
			return startupUpdateChoiceLater
		},
		saveStartupUpdateState: func(state appconfig.StartupUpdateState) (string, error) {
			return "", errors.New("disk full")
		},
		launchTUI: func(ctx context.Context, workDir string, launch taskTUILaunchOptions) error {
			return nil
		},
	})
	var errOut bytes.Buffer
	cmd.SetErr(&errOut)

	err := cmd.Execute()
	require.NoError(t, err)
	assert.Contains(t, errOut.String(), "Warning: Failed to save update state")
}

func TestDefaultPromptForStartupUpdateEmptyInputDefaultsToLater(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.SetIn(bytes.NewBufferString("\n"))
	var out bytes.Buffer
	var errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)

	choice := defaultPromptForStartupUpdate(cmd, update.StartupCheckResult{CurrentVersion: "v1.0.0", LatestVersion: "v1.2.0", UpdateAvailable: true})

	assert.Equal(t, startupUpdateChoiceLater, choice)
	assert.Contains(t, out.String(), "Update available: v1.0.0 -> v1.2.0")
	assert.Empty(t, errOut.String())
}

func TestDefaultPromptForStartupUpdateRetriesInvalidInput(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.SetIn(bytes.NewBufferString("laterer\n3\n"))
	var out bytes.Buffer
	var errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)

	choice := defaultPromptForStartupUpdate(cmd, update.StartupCheckResult{CurrentVersion: "v1.0.0", LatestVersion: "v1.2.0", UpdateAvailable: true})

	assert.Equal(t, startupUpdateChoiceSkipVersion, choice)
	assert.Contains(t, errOut.String(), "Please enter 1, 2, or 3.")
}

func TestDefaultPromptForStartupUpdateAcceptsUpdateNow(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.SetIn(bytes.NewBufferString("1\n"))

	choice := defaultPromptForStartupUpdate(cmd, update.StartupCheckResult{CurrentVersion: "v1.0.0", LatestVersion: "v1.2.0", UpdateAvailable: true})

	assert.Equal(t, startupUpdateChoiceUpdateNow, choice)
}

func TestDefaultPromptForStartupUpdateWarnsOnReadError(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.SetIn(errReader{err: errors.New("boom")})
	var errOut bytes.Buffer
	cmd.SetErr(&errOut)

	choice := defaultPromptForStartupUpdate(cmd, update.StartupCheckResult{CurrentVersion: "v1.0.0", LatestVersion: "v1.2.0", UpdateAvailable: true})

	assert.Equal(t, startupUpdateChoiceLater, choice)
	assert.Contains(t, errOut.String(), "Warning: Failed to read update choice")
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

type errReader struct {
	err error
}

func (r errReader) Read(p []byte) (int, error) {
	return 0, r.err
}

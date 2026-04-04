package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/LaLanMo/muxagent-cli/cmd/muxagent/appserver"
	"github.com/LaLanMo/muxagent-cli/cmd/muxagent/auth"
	"github.com/LaLanMo/muxagent-cli/cmd/muxagent/config"
	"github.com/LaLanMo/muxagent-cli/cmd/muxagent/daemon"
	"github.com/LaLanMo/muxagent-cli/cmd/muxagent/health"
	"github.com/LaLanMo/muxagent-cli/cmd/muxagent/update"
	appconfig "github.com/LaLanMo/muxagent-cli/internal/config"
	"github.com/LaLanMo/muxagent-cli/internal/taskconfig"
	"github.com/LaLanMo/muxagent-cli/internal/taskexecutor"
	"github.com/LaLanMo/muxagent-cli/internal/taskexecutor/claudeexec"
	codextaskexecutor "github.com/LaLanMo/muxagent-cli/internal/taskexecutor/codex"
	"github.com/LaLanMo/muxagent-cli/internal/taskruntime"
	"github.com/LaLanMo/muxagent-cli/internal/taskstore"
	"github.com/LaLanMo/muxagent-cli/internal/tasktui"
	cliversion "github.com/LaLanMo/muxagent-cli/internal/version"
	"github.com/LaLanMo/muxagent-cli/internal/worktree"
	"github.com/spf13/cobra"
)

type taskTUILaunchOptions struct {
	WorktreeAvailable  bool
	DefaultUseWorktree bool
}

type launchFuncWithOptions func(ctx context.Context, workDir string, launch taskTUILaunchOptions) error

type startupUpdateChoice string

const (
	startupUpdateCheckCadence   = 24 * time.Hour
	startupUpdateFailureBackoff = 6 * time.Hour

	startupUpdateChoiceUpdateNow   startupUpdateChoice = "update-now"
	startupUpdateChoiceLater       startupUpdateChoice = "later"
	startupUpdateChoiceSkipVersion startupUpdateChoice = "skip-version"
)

type rootOptions struct {
	launchTUI                   launchFuncWithOptions
	now                         func() time.Time
	isInteractive               func(cmd *cobra.Command) bool
	loadStartupUpdateState      func() appconfig.StartupUpdateState
	saveStartupUpdateState      func(state appconfig.StartupUpdateState) (string, error)
	consumeStartupResumeOutcome func() update.StartupResumeOutcome
	checkForStartupUpdate       func(ctx context.Context) (update.StartupCheckResult, error)
	installStartupUpdate        func(ctx context.Context, latest string) error
	promptForStartupUpdate      func(cmd *cobra.Command, result update.StartupCheckResult) startupUpdateChoice
}

func NewRootCmd() *cobra.Command {
	return newRootCmd(rootOptions{
		launchTUI: func(ctx context.Context, workDir string, launch taskTUILaunchOptions) error {
			catalog, err := loadTaskConfigCatalog()
			if err != nil {
				return err
			}
			service, err := taskruntime.NewService(
				workDir,
				taskexecutor.NewRouter(codextaskexecutor.New(""), claudeexec.New("")),
			)
			if err != nil {
				return err
			}
			defer service.Close()
			return tasktui.App{
				Service:                 service,
				WorkDir:                 workDir,
				ConfigCatalog:           catalog,
				WorktreeLaunchAvailable: launch.WorktreeAvailable,
				DefaultUseWorktree:      launch.DefaultUseWorktree,
				SaveTaskLaunchPreference: func(useWorktree bool) error {
					_, err := appconfig.SaveTaskLaunchPreferences(appconfig.TaskLaunchPreferences{UseWorktree: useWorktree})
					return err
				},
				Version: cliversion.CLIString(),
			}.Run(ctx)
		},
		now:                         time.Now,
		isInteractive:               defaultIsInteractive,
		loadStartupUpdateState:      appconfig.LoadStartupUpdateState,
		saveStartupUpdateState:      appconfig.SaveStartupUpdateState,
		consumeStartupResumeOutcome: update.ConsumeStartupResumeOutcome,
		checkForStartupUpdate:       update.CheckForStartupUpdate,
		installStartupUpdate:        update.InstallStartupUpdate,
		promptForStartupUpdate:      defaultPromptForStartupUpdate,
	})
}

func newRootCmd(opts rootOptions) *cobra.Command {
	if opts.now == nil {
		opts.now = time.Now
	}
	if opts.isInteractive == nil {
		opts.isInteractive = defaultIsInteractive
	}
	if opts.loadStartupUpdateState == nil {
		opts.loadStartupUpdateState = appconfig.LoadStartupUpdateState
	}
	if opts.saveStartupUpdateState == nil {
		opts.saveStartupUpdateState = appconfig.SaveStartupUpdateState
	}
	if opts.consumeStartupResumeOutcome == nil {
		opts.consumeStartupResumeOutcome = update.ConsumeStartupResumeOutcome
	}
	if opts.checkForStartupUpdate == nil {
		opts.checkForStartupUpdate = update.CheckForStartupUpdate
	}
	if opts.installStartupUpdate == nil {
		opts.installStartupUpdate = update.InstallStartupUpdate
	}
	if opts.promptForStartupUpdate == nil {
		opts.promptForStartupUpdate = defaultPromptForStartupUpdate
	}

	rootCmd := &cobra.Command{
		Use:     "muxagent",
		Short:   "MuxAgent CLI",
		Version: cliversion.CLIString(),
		RunE: func(cmd *cobra.Command, args []string) error {
			workDir, err := os.Getwd()
			if err != nil {
				return err
			}
			workDir = taskstore.NormalizeWorkDir(workDir)
			worktreeAvailable := false
			if _, err := worktree.FindRepoRoot(workDir); err == nil {
				worktreeAvailable = true
			}
			prefs := appconfig.LoadTaskLaunchPreferences()
			maybeHandleStartupUpdate(cmd, opts)
			return opts.launchTUI(cmd.Context(), workDir, taskTUILaunchOptions{
				WorktreeAvailable:  worktreeAvailable,
				DefaultUseWorktree: worktreeAvailable && prefs.UseWorktree,
			})
		},
	}
	rootCmd.SetVersionTemplate("{{.Version}}\n")
	rootCmd.CompletionOptions.DisableDefaultCmd = true

	rootCmd.AddCommand(
		appserver.NewCmd(),
		auth.NewCmd(),
		config.NewCmd(),
		daemon.NewCmd(),
		health.NewCmd(),
		update.NewCmd(),
		newVersionCmd(),
	)

	return rootCmd
}

func Execute() {
	if err := NewRootCmd().Execute(); err != nil {
		os.Exit(1)
	}
}

func loadTaskConfigCatalog() (*taskconfig.Catalog, error) {
	return taskconfig.LoadCatalog()
}

func maybeHandleStartupUpdate(cmd *cobra.Command, opts rootOptions) {
	now := opts.now()
	resume := opts.consumeStartupResumeOutcome()
	if resume.Resumed {
		state := opts.loadStartupUpdateState()
		state.LastCheckedAt = now
		state.LastFailedAt = time.Time{}
		state.SkippedVersion = ""
		saveStartupUpdateState(cmd, opts, state)
		return
	}

	if !opts.isInteractive(cmd) || cliversion.Version == "dev" || !startupUpdateSupported(runtime.GOOS) {
		return
	}

	state := opts.loadStartupUpdateState()
	if recentlyFailed(state, now) || recentlyChecked(state, now) {
		return
	}

	result, err := opts.checkForStartupUpdate(cmd.Context())
	if err != nil {
		state.LastFailedAt = now
		saveStartupUpdateState(cmd, opts, state)
		writeStartupUpdateWarning(cmd, "Failed to check for updates: %v", err)
		return
	}

	if !result.UpdateAvailable {
		state.LastCheckedAt = now
		state.LastFailedAt = time.Time{}
		state.SkippedVersion = ""
		saveStartupUpdateState(cmd, opts, state)
		return
	}

	if state.SkippedVersion == result.LatestVersion {
		state.LastCheckedAt = now
		state.LastFailedAt = time.Time{}
		saveStartupUpdateState(cmd, opts, state)
		return
	}

	switch opts.promptForStartupUpdate(cmd, result) {
	case startupUpdateChoiceUpdateNow:
		fmt.Fprintf(cmd.ErrOrStderr(), "Updating muxagent... (v%s -> v%s)\n", displayStartupVersion(result.CurrentVersion), displayStartupVersion(result.LatestVersion))
		if err := opts.installStartupUpdate(cmd.Context(), result.LatestVersion); err != nil {
			state.LastFailedAt = now
			saveStartupUpdateState(cmd, opts, state)
			writeStartupUpdateWarning(cmd, "Automatic update failed: %v", err)
		}
	case startupUpdateChoiceSkipVersion:
		state.LastCheckedAt = now
		state.LastFailedAt = time.Time{}
		state.SkippedVersion = result.LatestVersion
		saveStartupUpdateState(cmd, opts, state)
	default:
		state.LastCheckedAt = now
		state.LastFailedAt = time.Time{}
		state.SkippedVersion = ""
		saveStartupUpdateState(cmd, opts, state)
	}
}

func defaultIsInteractive(cmd *cobra.Command) bool {
	in, inOK := cmd.InOrStdin().(*os.File)
	out, outOK := cmd.OutOrStdout().(*os.File)
	return inOK && outOK && isTerminalFile(in) && isTerminalFile(out)
}

func isTerminalFile(f *os.File) bool {
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return (info.Mode() & os.ModeCharDevice) != 0
}

func defaultPromptForStartupUpdate(cmd *cobra.Command, result update.StartupCheckResult) startupUpdateChoice {
	reader := bufio.NewReader(cmd.InOrStdin())
	for {
		fmt.Fprintf(cmd.OutOrStdout(), "Update available: v%s -> v%s\n", displayStartupVersion(result.CurrentVersion), displayStartupVersion(result.LatestVersion))
		fmt.Fprint(cmd.OutOrStdout(), "Choose [1] Update now, [2] Later, [3] Skip this version (default: 2): ")

		line, err := reader.ReadString('\n')
		choice, done := parseStartupUpdatePromptChoice(strings.TrimSpace(line), err)
		if done {
			return choice
		}
		if err != nil && !errors.Is(err, io.EOF) {
			writeStartupUpdateWarning(cmd, "Failed to read update choice: %v. Continuing without updating.", err)
			return startupUpdateChoiceLater
		}
		fmt.Fprintln(cmd.ErrOrStderr(), "Please enter 1, 2, or 3.")
		if errors.Is(err, io.EOF) {
			return startupUpdateChoiceLater
		}
	}
}

func parseStartupUpdatePromptChoice(input string, err error) (startupUpdateChoice, bool) {
	switch strings.ToLower(strings.TrimSpace(input)) {
	case "":
		if err != nil && !errors.Is(err, io.EOF) {
			return "", false
		}
		return startupUpdateChoiceLater, true
	case "2", "l", "later":
		return startupUpdateChoiceLater, true
	case "1", "u", "update", "update now":
		return startupUpdateChoiceUpdateNow, true
	case "3", "s", "skip", "skip this version":
		return startupUpdateChoiceSkipVersion, true
	default:
		if errors.Is(err, io.EOF) {
			return startupUpdateChoiceLater, true
		}
		return "", false
	}
}

func saveStartupUpdateState(cmd *cobra.Command, opts rootOptions, state appconfig.StartupUpdateState) {
	if _, err := opts.saveStartupUpdateState(state); err != nil {
		writeStartupUpdateWarning(cmd, "Failed to save update state: %v", err)
	}
}

func writeStartupUpdateWarning(cmd *cobra.Command, format string, args ...any) {
	fmt.Fprintf(cmd.ErrOrStderr(), "Warning: "+format+"\n", args...)
}

func recentlyChecked(state appconfig.StartupUpdateState, now time.Time) bool {
	if state.LastCheckedAt.IsZero() {
		return false
	}
	return now.Sub(state.LastCheckedAt) < startupUpdateCheckCadence
}

func recentlyFailed(state appconfig.StartupUpdateState, now time.Time) bool {
	if state.LastFailedAt.IsZero() {
		return false
	}
	return now.Sub(state.LastFailedAt) < startupUpdateFailureBackoff
}

func startupUpdateSupported(goos string) bool {
	return goos == "darwin" || goos == "linux"
}

func displayStartupVersion(raw string) string {
	return strings.TrimPrefix(raw, "v")
}

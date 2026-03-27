package main

import (
	"context"
	"fmt"
	"os"

	"github.com/LaLanMo/muxagent-cli/cmd/muxagent/acptest"
	"github.com/LaLanMo/muxagent-cli/cmd/muxagent/auth"
	"github.com/LaLanMo/muxagent-cli/cmd/muxagent/config"
	"github.com/LaLanMo/muxagent-cli/cmd/muxagent/daemon"
	"github.com/LaLanMo/muxagent-cli/cmd/muxagent/health"
	"github.com/LaLanMo/muxagent-cli/cmd/muxagent/update"
	appconfig "github.com/LaLanMo/muxagent-cli/internal/config"
	"github.com/LaLanMo/muxagent-cli/internal/taskconfig"
	"github.com/LaLanMo/muxagent-cli/internal/taskexecutor"
	"github.com/LaLanMo/muxagent-cli/internal/taskexecutor/claudeexec"
	"github.com/LaLanMo/muxagent-cli/internal/taskexecutor/codexexec"
	"github.com/LaLanMo/muxagent-cli/internal/taskruntime"
	"github.com/LaLanMo/muxagent-cli/internal/taskstore"
	"github.com/LaLanMo/muxagent-cli/internal/tasktui"
	cliversion "github.com/LaLanMo/muxagent-cli/internal/version"
	"github.com/LaLanMo/muxagent-cli/internal/worktree"
	"github.com/spf13/cobra"
)

type taskTUILaunchOptions struct {
	Runtime            appconfig.RuntimeID
	WorktreeAvailable  bool
	DefaultUseWorktree bool
}

type launchFuncWithOptions func(ctx context.Context, workDir string, launch taskTUILaunchOptions) error

type rootOptions struct {
	launchTUI launchFuncWithOptions
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
				taskexecutor.NewRouter(codexexec.New(""), claudeexec.New("")),
			)
			if err != nil {
				return err
			}
			defer service.Close()
			return tasktui.App{
				Service:                 service,
				WorkDir:                 workDir,
				ConfigCatalog:           catalog,
				LaunchRuntimeOverride:   launch.Runtime,
				WorktreeLaunchAvailable: launch.WorktreeAvailable,
				DefaultUseWorktree:      launch.DefaultUseWorktree,
				SaveTaskLaunchPreference: func(useWorktree bool) error {
					_, err := appconfig.SaveTaskLaunchPreferences(appconfig.TaskLaunchPreferences{UseWorktree: useWorktree})
					return err
				},
				Version: cliversion.CLIString(),
			}.Run(ctx)
		},
	})
}

func newRootCmd(opts rootOptions) *cobra.Command {
	var runtimeOverride string
	rootCmd := &cobra.Command{
		Use:     "muxagent",
		Short:   "MuxAgent CLI",
		Version: cliversion.CLIString(),
		RunE: func(cmd *cobra.Command, args []string) error {
			var runtime appconfig.RuntimeID
			if runtimeOverride != "" {
				runtime = appconfig.RuntimeID(runtimeOverride)
				if !appconfig.IsSupportedRuntime(runtime) {
					return fmt.Errorf("runtime %q is not supported", runtimeOverride)
				}
			}
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
			return opts.launchTUI(cmd.Context(), workDir, taskTUILaunchOptions{
				Runtime:            runtime,
				WorktreeAvailable:  worktreeAvailable,
				DefaultUseWorktree: worktreeAvailable && prefs.UseWorktree,
			})
		},
	}
	rootCmd.SetVersionTemplate("{{.Version}}\n")
	rootCmd.CompletionOptions.DisableDefaultCmd = true
	rootCmd.Flags().StringVar(&runtimeOverride, "runtime", "", "Task runtime override (codex or claude-code)")

	rootCmd.AddCommand(
		acptest.NewCmd(),
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

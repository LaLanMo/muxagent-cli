package main

import (
	"context"
	"os"

	"github.com/LaLanMo/muxagent-cli/cmd/muxagent/acptest"
	"github.com/LaLanMo/muxagent-cli/cmd/muxagent/auth"
	"github.com/LaLanMo/muxagent-cli/cmd/muxagent/config"
	"github.com/LaLanMo/muxagent-cli/cmd/muxagent/daemon"
	"github.com/LaLanMo/muxagent-cli/cmd/muxagent/health"
	"github.com/LaLanMo/muxagent-cli/cmd/muxagent/update"
	"github.com/LaLanMo/muxagent-cli/internal/taskconfig"
	"github.com/LaLanMo/muxagent-cli/internal/taskexecutor/codexexec"
	"github.com/LaLanMo/muxagent-cli/internal/taskruntime"
	"github.com/LaLanMo/muxagent-cli/internal/taskstore"
	"github.com/LaLanMo/muxagent-cli/internal/tasktui"
	cliversion "github.com/LaLanMo/muxagent-cli/internal/version"
	"github.com/spf13/cobra"
)

type launchFunc func(ctx context.Context, workDir, configPath string) error

type rootOptions struct {
	launchTUI launchFunc
}

func NewRootCmd() *cobra.Command {
	return newRootCmd(rootOptions{
		launchTUI: func(ctx context.Context, workDir, configPath string) error {
			workDir = taskstore.NormalizeWorkDir(workDir)
			launchConfig, err := loadTaskLaunchConfig(configPath)
			if err != nil {
				return err
			}
			service, err := taskruntime.NewService(workDir, configPath, codexexec.New(""))
			if err != nil {
				return err
			}
			defer service.Close()
			return tasktui.App{
				Service:        service,
				WorkDir:        workDir,
				ConfigOverride: configPath,
				LaunchConfig:   launchConfig,
				Version:        cliversion.CLIString(),
			}.Run(ctx)
		},
	})
}

func newRootCmd(opts rootOptions) *cobra.Command {
	var configPath string
	rootCmd := &cobra.Command{
		Use:     "muxagent",
		Short:   "MuxAgent CLI",
		Version: cliversion.CLIString(),
		RunE: func(cmd *cobra.Command, args []string) error {
			workDir, err := os.Getwd()
			if err != nil {
				return err
			}
			return opts.launchTUI(cmd.Context(), workDir, configPath)
		},
	}
	rootCmd.SetVersionTemplate("{{.Version}}\n")
	rootCmd.CompletionOptions.DisableDefaultCmd = true
	rootCmd.Flags().StringVarP(&configPath, "config", "c", "", "Task config override path for task-first TUI")

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

func loadTaskLaunchConfig(configPath string) (*taskconfig.Config, error) {
	if configPath == "" {
		return taskconfig.LoadDefault()
	}
	return taskconfig.Load(configPath)
}

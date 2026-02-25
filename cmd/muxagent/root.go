package main

import (
	"os"

	"github.com/LaLanMo/muxagent-cli/cmd/muxagent/acptest"
	"github.com/LaLanMo/muxagent-cli/cmd/muxagent/auth"
	"github.com/LaLanMo/muxagent-cli/cmd/muxagent/config"
	"github.com/LaLanMo/muxagent-cli/cmd/muxagent/daemon"
	"github.com/LaLanMo/muxagent-cli/cmd/muxagent/health"
	"github.com/spf13/cobra"
)

func NewRootCmd() *cobra.Command {
	rootCmd := &cobra.Command{
		Use:   "muxagent",
		Short: "MuxAgent CLI",
	}

	rootCmd.AddCommand(
		acptest.NewCmd(),
		auth.NewCmd(),
		config.NewCmd(),
		daemon.NewCmd(),
		health.NewCmd(),
	)

	return rootCmd
}

func Execute() {
	if err := NewRootCmd().Execute(); err != nil {
		os.Exit(1)
	}
}

package health

import (
	"context"
	"fmt"

	"github.com/LaLanMo/muxagent-cli/internal/config"
	"github.com/LaLanMo/muxagent-cli/internal/runtime"
	"github.com/spf13/cobra"
)

func NewCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "health",
		Short: "Check MuxAgent runtime health",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.LoadEffective()
			if err != nil {
				return err
			}
			runtimeConfig, err := cfg.ActiveRuntimeSettings()
			if err != nil {
				return err
			}
			client, err := runtime.NewClient(cfg.ActiveRuntime, runtimeConfig)
			if err != nil {
				return err
			}
			version, err := client.Health(context.Background())
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Runtime %s healthy (version %s)\n", cfg.ActiveRuntime, version)
			return nil
		},
	}
}

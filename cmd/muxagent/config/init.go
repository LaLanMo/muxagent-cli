package config

import (
	"fmt"

	cfg "github.com/LaLanMo/muxagent-cli/internal/config"
	"github.com/spf13/cobra"
)

func newInitCmd() *cobra.Command {
	var force bool
	var project bool

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Create default config",
		Long:  "Create a default config file. By default creates ~/.muxagent/config.json. Use --project to create ./.muxagent/config.json instead. The generated config includes claude-code and codex runtime entries.",
		RunE: func(cmd *cobra.Command, args []string) error {
			var targetPath string
			var err error

			if project {
				targetPath = cfg.ProjectConfigPath()
			} else {
				targetPath, err = cfg.UserConfigPath()
				if err != nil {
					return err
				}
			}

			// Check if file already exists
			if cfg.Exists(targetPath) && !force {
				return fmt.Errorf("config exists at %s (use --force to overwrite)", targetPath)
			}

			path, err := cfg.SaveTo(cfg.Default(), targetPath)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Created config at %s\n", path)
			return nil
		},
	}

	cmd.Flags().BoolVarP(&force, "force", "f", false, "Overwrite existing config file")
	cmd.Flags().BoolVarP(&project, "project", "p", false, "Create project-local config (./.muxagent/config.json)")

	return cmd
}

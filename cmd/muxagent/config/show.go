package config

import (
	"encoding/json"
	"fmt"

	cfg "github.com/LaLanMo/muxagent-cli/internal/config"
	"github.com/spf13/cobra"
)

func newShowCmd() *cobra.Command {
	var showPath bool

	cmd := &cobra.Command{
		Use:   "show",
		Short: "Print current config",
		Long:  "Print the effective merged config as JSON. Use --path to show config file paths instead.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if showPath {
				return showConfigPaths(cmd)
			}
			return showEffectiveConfig(cmd)
		},
	}

	cmd.Flags().BoolVar(&showPath, "path", false, "Show config file paths instead of config values")

	return cmd
}

func showConfigPaths(cmd *cobra.Command) error {
	userPath, err := cfg.UserConfigPath()
	if err != nil {
		return err
	}

	userStatus := "not found"
	if cfg.Exists(userPath) {
		userStatus = "exists"
	}

	projectPath := cfg.ProjectConfigPath()
	projectStatus := "not found"
	if cfg.Exists(projectPath) {
		projectStatus = "exists"
	}

	fmt.Fprintf(cmd.OutOrStdout(), "user:    %s (%s)\n", userPath, userStatus)
	fmt.Fprintf(cmd.OutOrStdout(), "project: %s (%s)\n", projectPath, projectStatus)
	return nil
}

func showEffectiveConfig(cmd *cobra.Command) error {
	cfgData, err := cfg.LoadEffective()
	if err != nil {
		return err
	}
	payload, err := json.MarshalIndent(cfgData, "", "  ")
	if err != nil {
		return err
	}
	fmt.Fprintln(cmd.OutOrStdout(), string(payload))
	return nil
}

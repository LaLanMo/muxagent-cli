package config

import "github.com/spf13/cobra"

func NewCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Manage MuxAgent config",
	}

	cmd.AddCommand(newInitCmd())
	cmd.AddCommand(newShowCmd())

	return cmd
}

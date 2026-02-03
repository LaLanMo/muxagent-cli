package daemon

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newStartCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "start",
		Short: "Start daemon in background",
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := startDaemonCommon()
			if err != nil {
				return err
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Daemon started at %s (state: %s)\n", result.Daemon.Address(), result.StatePath)
			// Note: lockFile is intentionally not closed here as the daemon owns it
			return nil
		},
	}
}

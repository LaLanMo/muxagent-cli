package daemon

import (
	"fmt"

	"github.com/LaLanMo/muxagent-cli/internal/config"
	"github.com/spf13/cobra"
)

func newStartSyncCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "start-sync",
		Short: "Run daemon in foreground",
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := startDaemonCommon()
			if err != nil {
				return err
			}
			defer config.ReleaseLock(result.LockFile)
			defer config.ClearState()

			fmt.Fprintf(cmd.OutOrStdout(), "Daemon running at %s (state: %s)\n", result.Daemon.Address(), result.StatePath)
			select {}
		},
	}
}

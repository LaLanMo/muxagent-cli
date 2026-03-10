package daemon

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/LaLanMo/muxagent-cli/internal/config"
	"github.com/spf13/cobra"
)

func newStopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Stop daemon",
		RunE: func(cmd *cobra.Command, args []string) error {
			state, err := config.LoadState()
			if err != nil {
				if os.IsNotExist(err) {
					fmt.Fprintln(cmd.OutOrStdout(), "Daemon is not running")
					return nil
				}
				return err
			}

			token, err := state.GetToken()
			if err != nil {
				// Can't authenticate to old daemon. Don't auto-kill (PID reuse risk,
				// skips Daemon.Stop() cleanup chain for ACP runtime children).
				config.ClearState()
				if isPIDAlive(state.PID) {
					// Keep lock file — prevents start from launching new daemon
					// while old one is alive (split-brain prevention).
					fmt.Fprintf(cmd.OutOrStdout(),
						"Daemon state had incompatible format.\n"+
							"Old daemon still running (PID %d). Stop it first, then start a new one:\n"+
							"  kill %d && muxagent daemon start\n",
						state.PID, state.PID)
				} else {
					cleanupLockFile()
					fmt.Fprintln(cmd.OutOrStdout(),
						"Daemon state had incompatible format, cleaned up (old process already exited)")
				}
				return nil
			}

			req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, fmt.Sprintf("http://%s/stop", state.Address), nil)
			if err != nil {
				return err
			}
			req.Header.Set("Authorization", "Bearer "+token)
			client := &http.Client{Timeout: 5 * time.Second}
			resp, err := client.Do(req)
			if err != nil {
				// Daemon may be dead but state file exists, clean up
				config.ClearState()
				cleanupLockFile()
				fmt.Fprintln(cmd.OutOrStdout(), "Daemon not responding, cleaned up state")
				return nil
			}
			defer resp.Body.Close()

			// Clean up state and lock files after successful stop
			if err := config.ClearState(); err != nil {
				return fmt.Errorf("clear state: %w", err)
			}
			cleanupLockFile()

			fmt.Fprintln(cmd.OutOrStdout(), "Daemon stopped")
			return nil
		},
	}
}

func cleanupLockFile() {
	path, err := config.StateLockPath()
	if err != nil {
		return
	}
	os.Remove(path)
}

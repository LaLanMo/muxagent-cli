package health

import (
	"fmt"
	"net/http"
	"time"

	"github.com/LaLanMo/muxagent-cli/internal/config"
	"github.com/spf13/cobra"
)

func NewCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "health",
		Short: "Check MuxAgent daemon health",
		RunE: func(cmd *cobra.Command, args []string) error {
			state, err := config.LoadState()
			if err != nil {
				return fmt.Errorf("daemon not running (no state file): %w", err)
			}

			token, err := state.GetToken()
			if err != nil {
				return fmt.Errorf("failed to read daemon token: %w", err)
			}

			client := &http.Client{Timeout: 3 * time.Second}
			req, err := http.NewRequest("GET", "http://"+state.Address+"/health", nil)
			if err != nil {
				return err
			}
			req.Header.Set("Authorization", "Bearer "+token)

			resp, err := client.Do(req)
			if err != nil {
				return fmt.Errorf("daemon not reachable at %s: %w", state.Address, err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				return fmt.Errorf("daemon returned status %d", resp.StatusCode)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Daemon healthy (pid %d, addr %s)\n", state.PID, state.Address)
			return nil
		},
	}
}

package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/LaLanMo/muxagent-cli/internal/config"
	"github.com/spf13/cobra"
)

func newEchoCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "echo",
		Short: "Send an echo from daemon to the connected client",
		RunE: func(cmd *cobra.Command, args []string) error {
			state, err := config.LoadState()
			if err != nil {
				if os.IsNotExist(err) {
					fmt.Fprintln(cmd.OutOrStdout(), "Daemon is not running (no state file)")
					return nil
				}
				return err
			}
			if state.PID > 0 && !isProcessAlive(state.PID) {
				fmt.Fprintf(cmd.OutOrStdout(), "Daemon is not running (stale state, pid %d dead)\n", state.PID)
				return nil
			}

			message, _ := cmd.Flags().GetString("message")
			payload := map[string]string{"message": message}
			body, err := json.Marshal(payload)
			if err != nil {
				return err
			}

			req, err := http.NewRequestWithContext(
				context.Background(),
				http.MethodPost,
				fmt.Sprintf("http://%s/echo", state.Address),
				bytes.NewReader(body),
			)
			if err != nil {
				return err
			}
			req.Header.Set("Authorization", "Bearer "+state.Token)
			req.Header.Set("Content-Type", "application/json")
			client := &http.Client{Timeout: 5 * time.Second}
			resp, err := client.Do(req)
			if err != nil {
				return err
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				return fmt.Errorf("echo failed: %s", resp.Status)
			}

			fmt.Fprintln(cmd.OutOrStdout(), "Echo sent")
			return nil
		},
	}

	cmd.Flags().String("message", "hello from daemon", "Echo message to send")

	return cmd
}

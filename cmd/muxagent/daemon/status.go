package daemon

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"syscall"
	"time"

	"github.com/LaLanMo/muxagent-cli/internal/config"
	"github.com/spf13/cobra"
)

func newStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Check daemon status",
		RunE: func(cmd *cobra.Command, args []string) error {
			state, err := config.LoadState()
			if err != nil {
				if os.IsNotExist(err) {
					fmt.Fprintln(cmd.OutOrStdout(), "Daemon is not running (no state file)")
					return nil
				}
				return err
			}

			// Validate PID is actually running
			if state.PID > 0 && !isProcessAlive(state.PID) {
				fmt.Fprintf(cmd.OutOrStdout(), "Daemon is not running (stale state, pid %d dead)\n", state.PID)
				return nil
			}

			// Check health endpoint
			token, err := state.GetToken()
			if err != nil {
				return fmt.Errorf("daemon has incompatible state format, run: muxagent daemon stop and follow its output")
			}

			req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, fmt.Sprintf("http://%s/health", state.Address), nil)
			if err != nil {
				return err
			}
			req.Header.Set("Authorization", "Bearer "+token)
			client := &http.Client{Timeout: 5 * time.Second}
			resp, err := client.Do(req)
			if err != nil {
				fmt.Fprintf(cmd.OutOrStdout(), "Daemon is not responding (pid %d)\n", state.PID)
				return nil
			}
			defer resp.Body.Close()

			// Calculate uptime
			uptime := ""
			if state.StartTime != "" {
				startTime, err := time.Parse(time.RFC3339, state.StartTime)
				if err == nil {
					uptime = formatDuration(time.Since(startTime))
				}
			}

			fmt.Fprintln(cmd.OutOrStdout(), "Daemon is running")
			fmt.Fprintf(cmd.OutOrStdout(), "  Address: %s\n", state.Address)
			if state.PID > 0 {
				fmt.Fprintf(cmd.OutOrStdout(), "  PID:     %d\n", state.PID)
			}
			if uptime != "" {
				fmt.Fprintf(cmd.OutOrStdout(), "  Uptime:  %s\n", uptime)
			}
			if state.StartedWithCLIVersion != "" {
				fmt.Fprintf(cmd.OutOrStdout(), "  Version: %s\n", state.StartedWithCLIVersion)
			}
			return nil
		},
	}
}

func isProcessAlive(pid int) bool {
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = process.Signal(syscall.Signal(0))
	return err == nil
}

func formatDuration(d time.Duration) string {
	days := int(d.Hours() / 24)
	hours := int(d.Hours()) % 24
	minutes := int(d.Minutes()) % 60
	seconds := int(d.Seconds()) % 60

	if days > 0 {
		return fmt.Sprintf("%dd %dh %dm", days, hours, minutes)
	}
	if hours > 0 {
		return fmt.Sprintf("%dh %dm %ds", hours, minutes, seconds)
	}
	if minutes > 0 {
		return fmt.Sprintf("%dm %ds", minutes, seconds)
	}
	return fmt.Sprintf("%ds", seconds)
}

package daemon

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/LaLanMo/muxagent-cli/internal/config"
	"github.com/spf13/cobra"
)

const daemonStopTimeout = 10 * time.Second

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
				return handleStopTransportFailure(state, cmd.OutOrStdout())
			}
			defer resp.Body.Close()

			if !waitForProcessExit(state.PID, daemonStopTimeout) {
				fmt.Fprintf(cmd.OutOrStdout(),
					"Daemon acknowledged stop but process %d did not exit within %s.\n"+
						"State retained to prevent a second daemon from starting.\n",
					state.PID, daemonStopTimeout)
				return nil
			}

			if err := clearStateAndLock(); err != nil {
				return err
			}

			fmt.Fprintln(cmd.OutOrStdout(), "Daemon stopped")
			return nil
		},
	}
}

func handleStopTransportFailure(state config.DaemonState, out io.Writer) error {
	if isPIDAlive(state.PID) {
		fmt.Fprintf(out,
			"Daemon is not responding (pid %d).\n"+
				"State retained to prevent a second daemon from starting.\n",
			state.PID)
		return nil
	}

	if err := clearStateAndLock(); err != nil {
		return err
	}
	fmt.Fprintln(out, "Daemon not responding, cleaned up stale state")
	return nil
}

func waitForProcessExit(pid int, timeout time.Duration) bool {
	if pid <= 0 {
		return true
	}

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !isPIDAlive(pid) {
			return true
		}
		time.Sleep(100 * time.Millisecond)
	}

	return !isPIDAlive(pid)
}

func clearStateAndLock() error {
	var errs bytes.Buffer

	if err := config.ClearState(); err != nil && !os.IsNotExist(err) {
		errs.WriteString(fmt.Sprintf("clear state: %v", err))
	}
	if err := cleanupLockFile(); err != nil && !os.IsNotExist(err) {
		if errs.Len() > 0 {
			errs.WriteString("; ")
		}
		errs.WriteString(fmt.Sprintf("clear lock: %v", err))
	}

	if errs.Len() > 0 {
		return fmt.Errorf(errs.String())
	}
	return nil
}

func cleanupLockFile() error {
	path, err := config.StateLockPath()
	if err != nil {
		return err
	}
	return os.Remove(path)
}

package daemon

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"github.com/LaLanMo/muxagent-cli/internal/auth"
	"github.com/LaLanMo/muxagent-cli/internal/config"
	"github.com/LaLanMo/muxagent-cli/internal/relayws"
	"github.com/spf13/cobra"
)

func newStartCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "start",
		Short: "Start daemon in background",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Fast-path existing live daemon before attempting background spawn.
			stale, existingPID, err := config.IsLockStale()
			if err != nil {
				return err
			}
			if existingPID != 0 && !stale {
				return fmt.Errorf("daemon already running (pid %d)", existingPID)
			}
			if stale {
				if err := config.CleanStaleLock(); err != nil {
					return fmt.Errorf("clean stale lock: %w", err)
				}
			}

			if !auth.HasCredentials() {
				fmt.Println("No credentials found. Starting authentication...")
				cfg, err := config.LoadEffective()
				if err != nil {
					return fmt.Errorf("load config: %w", err)
				}
				httpURL := relayws.HTTPURLFromWS(cfg.RelayURL)
				flow := auth.NewAuthFlow(httpURL)
				if _, err := flow.RunAuthFlow(cmd.Context(), func(qrURL string) error {
					fmt.Println("Scan this QR code with the muxagent mobile app:")
					if err := auth.QRTerminalOutput(os.Stdout, qrURL); err != nil {
						fmt.Println("(Could not display QR code)")
					}
					fmt.Printf("\nOr open this URL manually:\n%s\n\n", qrURL)
					fmt.Println("Waiting for approval...")
					return nil
				}); err != nil {
					return fmt.Errorf("authentication failed: %w", err)
				}
				fmt.Println("Authentication successful!")
			}

			exe, err := os.Executable()
			if err != nil {
				return fmt.Errorf("resolve executable: %w", err)
			}

			logPath, logFile, err := openDaemonLogFile()
			if err != nil {
				return err
			}
			defer logFile.Close()

			child := exec.Command(exe, "daemon", "start-sync")
			child.Stdout = logFile
			child.Stderr = logFile
			child.Stdin = nil
			child.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

			if err := child.Start(); err != nil {
				return fmt.Errorf("start daemon process: %w", err)
			}
			childPID := child.Process.Pid
			_ = child.Process.Release()

			readyState, err := waitForDaemonReady(childPID, 10*time.Second)
			if err != nil {
				return fmt.Errorf("daemon failed to start: %w (see log: %s)", err, logPath)
			}

			statePath, err := config.StatePath()
			if err != nil {
				statePath = "(unknown state path)"
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Daemon started at %s (state: %s)\n", readyState.Address, statePath)
			return nil
		},
	}
}

func openDaemonLogFile() (string, *os.File, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", nil, fmt.Errorf("resolve home dir: %w", err)
	}
	logPath := filepath.Join(home, ".muxagent", "daemon.log")
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		return "", nil, fmt.Errorf("create daemon log dir: %w", err)
	}
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return "", nil, fmt.Errorf("open daemon log file: %w", err)
	}
	return logPath, f, nil
}

func waitForDaemonReady(expectedPID int, timeout time.Duration) (config.DaemonState, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !isPIDAlive(expectedPID) {
			return config.DaemonState{}, fmt.Errorf("daemon process %d exited during startup", expectedPID)
		}

		state, err := config.LoadState()
		if err == nil && state.PID == expectedPID && isDaemonHealthy(state) {
			return state, nil
		}
		if err != nil && !os.IsNotExist(err) {
			return config.DaemonState{}, fmt.Errorf("load daemon state: %w", err)
		}

		time.Sleep(100 * time.Millisecond)
	}
	return config.DaemonState{}, fmt.Errorf("timeout waiting for daemon readiness")
}

func isDaemonHealthy(state config.DaemonState) bool {
	token, err := state.GetToken()
	if err != nil {
		token = state.Token
	}

	req, err := http.NewRequestWithContext(
		context.Background(),
		http.MethodGet,
		fmt.Sprintf("http://%s/health", state.Address),
		nil,
	)
	if err != nil {
		return false
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	client := &http.Client{Timeout: 500 * time.Millisecond}
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	return resp.StatusCode == http.StatusOK
}

func isPIDAlive(pid int) bool {
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return process.Signal(syscall.Signal(0)) == nil
}

package daemon

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/LaLanMo/muxagent-cli/internal/config"
	"github.com/LaLanMo/muxagent-cli/internal/daemon"
	"github.com/LaLanMo/muxagent-cli/internal/version"
)

// daemonStartResult holds the result of starting a daemon.
// The caller is responsible for cleanup based on their use case.
type daemonStartResult struct {
	Daemon    *daemon.Daemon
	LockFile  *os.File
	StatePath string
}

// startDaemonCommon contains the shared startup logic for both
// start (background) and start-sync (foreground) commands.
func startDaemonCommon() (*daemonStartResult, error) {
	// Load configuration
	cfg, err := config.LoadEffective()
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}

	// Check for existing lock
	stale, existingPID, err := config.IsLockStale()
	if err != nil {
		return nil, err
	}

	// Lock file exists
	if existingPID != 0 || stale {
		if !stale {
			return nil, fmt.Errorf("daemon already running (pid %d)", existingPID)
		}
		// Clean stale lock and proceed
		if err := config.CleanStaleLock(); err != nil {
			return nil, fmt.Errorf("clean stale lock: %w", err)
		}
	}

	pid := os.Getpid()
	lockFile, err := config.AcquireLock(pid)
	if err != nil {
		return nil, fmt.Errorf("acquire lock: %w", err)
	}

	daemonInstance := daemon.New(cfg.RelayURL)
	if err := daemonInstance.Start(); err != nil {
		config.ReleaseLock(lockFile)
		return nil, err
	}

	state := config.DaemonState{
		Address:               daemonInstance.Address(),
		PID:                   pid,
		StartTime:             time.Now().Format(time.RFC3339),
		StartedWithCLIVersion: version.Version,
	}
	if err := state.SetToken(daemonInstance.Token()); err != nil {
		daemonInstance.Stop(context.Background())
		config.ReleaseLock(lockFile)
		return nil, fmt.Errorf("encrypt token: %w", err)
	}

	statePath, err := config.SaveState(state)
	if err != nil {
		daemonInstance.Stop(context.Background())
		config.ReleaseLock(lockFile)
		return nil, err
	}

	return &daemonStartResult{
		Daemon:    daemonInstance,
		LockFile:  lockFile,
		StatePath: statePath,
	}, nil
}

package appserver

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/LaLanMo/muxagent-cli/internal/privdir"
)

const (
	lockFileName        = "appserver.lock"
	ensureLockFileName  = "appserver.ensure.lock"
	daemonStateFileName = "appserver-daemon-state.json"
	logFileName         = "appserver.log"
	workspacesFileName  = "workspaces.json"
)

func defaultStateDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".muxagent", "appserver"), nil
}

func resolveStateDir(override string) (string, error) {
	override = strings.TrimSpace(override)
	if override == "" {
		dir, err := defaultStateDir()
		if err != nil {
			return "", err
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		root := filepath.Join(home, ".muxagent")
		if err := privdir.EnsureWithin(dir, root); err != nil {
			return "", err
		}
		return dir, nil
	}
	if !filepath.IsAbs(override) {
		return "", fmt.Errorf("state dir must be an absolute path")
	}
	clean := filepath.Clean(override)
	if err := privdir.Ensure(clean); err != nil {
		return "", err
	}
	return clean, nil
}

func singletonLockPath(stateDir string) string {
	return filepath.Join(stateDir, lockFileName)
}

func ensureLockPath(stateDir string) string {
	return filepath.Join(stateDir, ensureLockFileName)
}

func daemonStatePath(stateDir string) string {
	return filepath.Join(stateDir, daemonStateFileName)
}

func daemonLogPath(stateDir string) string {
	return filepath.Join(stateDir, logFileName)
}

func workspacesFilePath(stateDir string) string {
	return filepath.Join(stateDir, workspacesFileName)
}

package appserver

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	appconfig "github.com/LaLanMo/muxagent-cli/internal/config"
	"github.com/LaLanMo/muxagent-cli/internal/privdir"
)

type DaemonEndpoint struct {
	appconfig.DaemonState
	InstanceID string `json:"instance_id"`
}

func EnsureLockPath(stateDir string) string {
	return ensureLockPath(stateDir)
}

func SingletonLockPath(stateDir string) string {
	return singletonLockPath(stateDir)
}

func DaemonStatePath(stateDir string) string {
	return daemonStatePath(stateDir)
}

func DaemonLogPath(stateDir string) string {
	return daemonLogPath(stateDir)
}

func LoadDaemonEndpoint(stateDir string) (DaemonEndpoint, error) {
	payload, err := os.ReadFile(daemonStatePath(stateDir))
	if err != nil {
		return DaemonEndpoint{}, err
	}

	var state DaemonEndpoint
	if err := json.Unmarshal(payload, &state); err != nil {
		return DaemonEndpoint{}, err
	}
	return state, nil
}

func SaveDaemonEndpoint(stateDir string, state DaemonEndpoint) error {
	path := daemonStatePath(stateDir)
	if err := privdir.Ensure(filepath.Dir(path)); err != nil {
		return err
	}

	payload, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}

	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, payload, 0o600); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

func ClearDaemonEndpoint(stateDir string, instanceID string) error {
	path := daemonStatePath(stateDir)
	instanceID = strings.TrimSpace(instanceID)
	if instanceID != "" {
		state, err := LoadDaemonEndpoint(stateDir)
		if err == nil && state.InstanceID != instanceID {
			return nil
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	err := os.Remove(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

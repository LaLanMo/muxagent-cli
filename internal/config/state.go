package config

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"

	"github.com/LaLanMo/muxagent-cli/internal/crypto"
)

const localKeyInfo = "muxagent-daemon-state-v1"

type DaemonState struct {
	Address               string `json:"address"`
	Token                 string `json:"token,omitempty"`           // Deprecated: use EncryptedToken
	EncryptedToken        string `json:"encrypted_token,omitempty"` // SecretBox sealed
	PID                   int    `json:"pid"`
	StartTime             string `json:"start_time"` // RFC3339
	StartedWithCLIVersion string `json:"started_with_cli_version"`
	LastHeartbeat         string `json:"last_heartbeat,omitempty"` // RFC3339
	LogPath               string `json:"log_path,omitempty"`
}

// SetToken encrypts and stores the token.
func (s *DaemonState) SetToken(token string) error {
	localKey, err := deriveLocalKey()
	if err != nil {
		// Fall back to plaintext if we can't derive key
		s.Token = token
		return nil
	}

	sealed, err := crypto.SecretBoxSeal([]byte(token), localKey)
	if err != nil {
		s.Token = token
		return nil
	}

	s.EncryptedToken = base64.StdEncoding.EncodeToString(sealed)
	s.Token = "" // Clear plaintext
	return nil
}

// GetToken decrypts and returns the token.
func (s *DaemonState) GetToken() (string, error) {
	// Try encrypted token first
	if s.EncryptedToken != "" {
		localKey, err := deriveLocalKey()
		if err != nil {
			return "", fmt.Errorf("failed to derive local key: %w", err)
		}

		sealed, err := base64.StdEncoding.DecodeString(s.EncryptedToken)
		if err != nil {
			return "", fmt.Errorf("failed to decode encrypted token: %w", err)
		}

		plaintext, err := crypto.SecretBoxOpen(sealed, localKey)
		if err != nil {
			return "", fmt.Errorf("failed to decrypt token: %w", err)
		}

		return string(plaintext), nil
	}

	// Fall back to plaintext token (backward compatibility)
	return s.Token, nil
}

// deriveLocalKey derives a machine-specific encryption key from system entropy.
func deriveLocalKey() (*[32]byte, error) {
	entropy := collectSystemEntropy()
	return crypto.DeriveKeyFromBytes(entropy, []byte(localKeyInfo))
}

// collectSystemEntropy gathers machine-specific data for key derivation.
func collectSystemEntropy() []byte {
	var parts [][]byte

	// Hostname
	hostname, _ := os.Hostname()
	parts = append(parts, []byte(hostname))

	// User home directory
	home, _ := os.UserHomeDir()
	parts = append(parts, []byte(home))

	// Machine ID (Linux) or similar
	if data, err := os.ReadFile("/etc/machine-id"); err == nil {
		parts = append(parts, data)
	}

	// macOS: use a combination of user and hostname
	if runtime.GOOS == "darwin" {
		parts = append(parts, []byte("darwin"))
	}

	// Current user
	parts = append(parts, []byte(os.Getenv("USER")))

	// Combine all parts
	var combined []byte
	for _, p := range parts {
		combined = append(combined, p...)
		combined = append(combined, 0) // separator
	}

	return combined
}

func StatePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".muxagent", "daemon.state.json"), nil
}

func StateLockPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".muxagent", "daemon.state.json.lock"), nil
}

func SaveState(state DaemonState) (string, error) {
	path, err := StatePath()
	if err != nil {
		return "", err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}

	payload, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return "", err
	}

	if err := os.WriteFile(path, payload, 0o600); err != nil {
		return "", err
	}

	return path, nil
}

func LoadState() (DaemonState, error) {
	path, err := StatePath()
	if err != nil {
		return DaemonState{}, err
	}

	payload, err := os.ReadFile(path)
	if err != nil {
		return DaemonState{}, err
	}

	var state DaemonState
	if err := json.Unmarshal(payload, &state); err != nil {
		return DaemonState{}, err
	}

	return state, nil
}

func ClearState() error {
	path, err := StatePath()
	if err != nil {
		return err
	}
	err = os.Remove(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// AcquireLock creates the lock file with O_CREATE|O_EXCL and writes the PID.
// Returns the open file handle (caller should keep it open while running).
func AcquireLock(pid int) (*os.File, error) {
	path, err := StateLockPath()
	if err != nil {
		return nil, err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return nil, fmt.Errorf("lock file exists: %s", path)
		}
		return nil, err
	}

	if _, err := f.WriteString(strconv.Itoa(pid)); err != nil {
		f.Close()
		os.Remove(path)
		return nil, err
	}

	return f, nil
}

// ReleaseLock closes the file handle and removes the lock file.
func ReleaseLock(f *os.File) error {
	if f == nil {
		return nil
	}
	path := f.Name()
	if err := f.Close(); err != nil {
		return err
	}
	return os.Remove(path)
}

// IsLockStale checks if the lock file exists and whether the PID in it is still alive.
// Returns (stale, pid, error). If no lock file exists, returns (false, 0, nil).
func IsLockStale() (bool, int, error) {
	path, err := StateLockPath()
	if err != nil {
		return false, 0, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, 0, nil
		}
		return false, 0, err
	}

	pidStr := strings.TrimSpace(string(data))
	pid, err := strconv.Atoi(pidStr)
	if err != nil {
		// Invalid PID in lock file, treat as stale
		return true, 0, nil
	}

	if !isProcessAlive(pid) {
		return true, pid, nil
	}

	return false, pid, nil
}

// CleanStaleLock removes the lock file if the PID in it is dead.
func CleanStaleLock() error {
	stale, _, err := IsLockStale()
	if err != nil {
		return err
	}
	if !stale {
		return nil
	}

	path, err := StateLockPath()
	if err != nil {
		return err
	}
	return os.Remove(path)
}

// isProcessAlive checks if a process with the given PID is running.
func isProcessAlive(pid int) bool {
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	// On Unix, FindProcess always succeeds. Send signal 0 to check if alive.
	err = process.Signal(syscall.Signal(0))
	return err == nil
}

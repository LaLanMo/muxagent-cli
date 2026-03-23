package manager

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"

	"github.com/LaLanMo/muxagent-cli/internal/acpprotocol"
	"github.com/LaLanMo/muxagent-cli/internal/privdir"
)

type sessionSnapshot struct {
	ConfigOptions []acpprotocol.SessionConfigOption `json:"configOptions,omitempty"`
}

type sessionSnapshotStore struct {
	path      string
	mu        sync.RWMutex
	snapshots map[string]map[string]sessionSnapshot
}

func newSessionSnapshotStore(path string) *sessionSnapshotStore {
	return &sessionSnapshotStore{
		path:      path,
		snapshots: make(map[string]map[string]sessionSnapshot),
	}
}

func defaultSessionSnapshotStorePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".muxagent", "session.snapshots.json"), nil
}

func (s *sessionSnapshotStore) Load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			s.snapshots = make(map[string]map[string]sessionSnapshot)
			return nil
		}
		return err
	}

	var snapshots map[string]map[string]sessionSnapshot
	if err := json.Unmarshal(data, &snapshots); err != nil {
		return err
	}
	if snapshots == nil {
		snapshots = make(map[string]map[string]sessionSnapshot)
	}
	s.snapshots = snapshots
	return nil
}

func (s *sessionSnapshotStore) Save() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saveLocked()
}

func (s *sessionSnapshotStore) Get(
	runtimeID string,
	sessionID string,
) (sessionSnapshot, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	runtimeSnapshots, ok := s.snapshots[runtimeID]
	if !ok {
		return sessionSnapshot{}, false
	}
	snapshot, ok := runtimeSnapshots[sessionID]
	if !ok {
		return sessionSnapshot{}, false
	}
	snapshot.ConfigOptions = cloneConfigOptions(snapshot.ConfigOptions)
	return snapshot, true
}

func (s *sessionSnapshotStore) Set(
	runtimeID string,
	sessionID string,
	snapshot sessionSnapshot,
) {
	s.mu.Lock()
	defer s.mu.Unlock()

	runtimeSnapshots, ok := s.snapshots[runtimeID]
	if !ok {
		runtimeSnapshots = make(map[string]sessionSnapshot)
		s.snapshots[runtimeID] = runtimeSnapshots
	}
	runtimeSnapshots[sessionID] = sessionSnapshot{
		ConfigOptions: cloneConfigOptions(snapshot.ConfigOptions),
	}
}

func (s *sessionSnapshotStore) Ensure(
	runtimeID string,
	sessionID string,
) error {
	if runtimeID == "" || sessionID == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	runtimeSnapshots := s.ensureRuntimeSnapshotsLocked(runtimeID)
	if _, exists := runtimeSnapshots[sessionID]; exists {
		return nil
	}
	runtimeSnapshots[sessionID] = sessionSnapshot{}
	return s.saveLocked()
}

func (s *sessionSnapshotStore) Put(
	runtimeID string,
	sessionID string,
	snapshot sessionSnapshot,
) error {
	if runtimeID == "" || sessionID == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	runtimeSnapshots := s.ensureRuntimeSnapshotsLocked(runtimeID)
	runtimeSnapshots[sessionID] = sessionSnapshot{
		ConfigOptions: cloneConfigOptions(snapshot.ConfigOptions),
	}
	return s.saveLocked()
}

func (s *sessionSnapshotStore) Update(
	runtimeID string,
	sessionID string,
	update func(*sessionSnapshot) bool,
) error {
	if runtimeID == "" || sessionID == "" || update == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	runtimeSnapshots := s.ensureRuntimeSnapshotsLocked(runtimeID)
	current := runtimeSnapshots[sessionID]
	current.ConfigOptions = cloneConfigOptions(current.ConfigOptions)
	if !update(&current) {
		return nil
	}
	runtimeSnapshots[sessionID] = sessionSnapshot{
		ConfigOptions: cloneConfigOptions(current.ConfigOptions),
	}
	return s.saveLocked()
}

func (s *sessionSnapshotStore) All() map[string]map[string]sessionSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make(map[string]map[string]sessionSnapshot, len(s.snapshots))
	for runtimeID, runtimeSnapshots := range s.snapshots {
		cloned := make(map[string]sessionSnapshot, len(runtimeSnapshots))
		for sessionID, snapshot := range runtimeSnapshots {
			cloned[sessionID] = sessionSnapshot{
				ConfigOptions: cloneConfigOptions(snapshot.ConfigOptions),
			}
		}
		out[runtimeID] = cloned
	}
	return out
}

func (s *sessionSnapshotStore) ensureRuntimeSnapshotsLocked(
	runtimeID string,
) map[string]sessionSnapshot {
	runtimeSnapshots, ok := s.snapshots[runtimeID]
	if !ok {
		runtimeSnapshots = make(map[string]sessionSnapshot)
		s.snapshots[runtimeID] = runtimeSnapshots
	}
	return runtimeSnapshots
}

func (s *sessionSnapshotStore) saveLocked() error {
	data, err := json.MarshalIndent(s.snapshots, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(s.path)
	if err := privdir.Ensure(dir); err != nil {
		return err
	}
	tempFile, err := os.CreateTemp(dir, "session-snapshots-*.json")
	if err != nil {
		return err
	}
	tempPath := tempFile.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tempPath)
		}
	}()
	if _, err := tempFile.Write(data); err != nil {
		_ = tempFile.Close()
		return err
	}
	if err := tempFile.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tempPath, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tempPath, s.path); err != nil {
		return err
	}
	cleanup = false
	return nil
}

func cloneConfigOptions(
	options []acpprotocol.SessionConfigOption,
) []acpprotocol.SessionConfigOption {
	if len(options) == 0 {
		return nil
	}
	data, err := json.Marshal(options)
	if err != nil {
		return append([]acpprotocol.SessionConfigOption(nil), options...)
	}
	var cloned []acpprotocol.SessionConfigOption
	if err := json.Unmarshal(data, &cloned); err != nil {
		return append([]acpprotocol.SessionConfigOption(nil), options...)
	}
	return cloned
}

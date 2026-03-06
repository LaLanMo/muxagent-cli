package worktree

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
)

// Mapping holds the worktree metadata for a session.
type Mapping struct {
	WorktreeID   string `json:"worktreeId"`
	WorktreePath string `json:"worktreePath"`
	RepoRoot     string `json:"repoRoot"`
	BranchName   string `json:"branchName"`
}

// Store persists session→worktree mappings to a JSON file.
type Store struct {
	path     string
	mu       sync.RWMutex
	mappings map[string]Mapping // sessionID → Mapping
}

// NewStore creates a new Store backed by the given file path.
func NewStore(path string) *Store {
	return &Store{
		path:     path,
		mappings: make(map[string]Mapping),
	}
}

// Load reads the mappings from disk. Returns nil (empty map) if file doesn't exist.
func (s *Store) Load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			s.mappings = make(map[string]Mapping)
			return nil
		}
		return err
	}

	var m map[string]Mapping
	if err := json.Unmarshal(data, &m); err != nil {
		return err
	}
	s.mappings = m
	return nil
}

// Save writes the current mappings to disk.
func (s *Store) Save() error {
	s.mu.RLock()
	data, err := json.MarshalIndent(s.mappings, "", "  ")
	s.mu.RUnlock()
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(s.path, data, 0o600)
}

// Set stores a mapping for the given session ID.
func (s *Store) Set(sessionID string, m Mapping) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.mappings[sessionID] = m
}

// Get returns the mapping for a session ID, or nil if not found.
func (s *Store) Get(sessionID string) *Mapping {
	s.mu.RLock()
	defer s.mu.RUnlock()
	m, ok := s.mappings[sessionID]
	if !ok {
		return nil
	}
	return &m
}

// Delete removes a mapping for the given session ID.
func (s *Store) Delete(sessionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.mappings, sessionID)
}

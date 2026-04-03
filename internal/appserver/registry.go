package appserver

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/LaLanMo/muxagent-cli/internal/privdir"
	"github.com/LaLanMo/muxagent-cli/internal/taskstore"
	"github.com/google/uuid"
)

const workspaceSourceUserAdded = "user_added"

type workspaceRecord struct {
	WorkspaceID  string    `json:"workspace_id"`
	Path         string    `json:"path"`
	DisplayName  string    `json:"display_name"`
	Source       string    `json:"source"`
	AddedAt      time.Time `json:"added_at"`
	LastOpenedAt time.Time `json:"last_opened_at"`
}

type workspaceRegistryFile struct {
	Workspaces []workspaceRecord `json:"workspaces"`
}

type workspaceRegistry struct {
	path string
	now  func() time.Time

	mu         sync.RWMutex
	workspaces []workspaceRecord
}

func newWorkspaceRegistry(path string, now func() time.Time) *workspaceRegistry {
	if now == nil {
		now = time.Now
	}
	return &workspaceRegistry{
		path:       path,
		now:        now,
		workspaces: make([]workspaceRecord, 0),
	}
}

func (r *workspaceRegistry) Load() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	data, err := os.ReadFile(r.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			r.workspaces = make([]workspaceRecord, 0)
			return nil
		}
		return err
	}

	var file workspaceRegistryFile
	if err := json.Unmarshal(data, &file); err != nil {
		return err
	}
	r.workspaces = normalizeWorkspaceRecords(file.Workspaces)
	return nil
}

func (r *workspaceRegistry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.workspaces)
}

func (r *workspaceRegistry) List() []workspaceRecord {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]workspaceRecord, len(r.workspaces))
	copy(out, r.workspaces)
	return out
}

func (r *workspaceRegistry) Get(workspaceID string) (workspaceRecord, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, workspace := range r.workspaces {
		if workspace.WorkspaceID == workspaceID {
			return workspace, true
		}
	}
	return workspaceRecord{}, false
}

func (r *workspaceRegistry) Add(path, displayName string) (workspaceRecord, bool, error) {
	canonicalPath, err := canonicalizeWorkspacePath(path)
	if err != nil {
		return workspaceRecord{}, false, err
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	now := r.now().UTC()
	for i, workspace := range r.workspaces {
		if workspace.Path != canonicalPath {
			continue
		}
		if strings.TrimSpace(displayName) != "" {
			workspace.DisplayName = normalizedDisplayName(displayName, canonicalPath)
		}
		workspace.LastOpenedAt = now
		r.workspaces[i] = workspace
		if err := r.saveLocked(); err != nil {
			return workspaceRecord{}, false, err
		}
		return workspace, false, nil
	}

	record := workspaceRecord{
		WorkspaceID:  "ws_" + uuid.NewString(),
		Path:         canonicalPath,
		DisplayName:  normalizedDisplayName(displayName, canonicalPath),
		Source:       workspaceSourceUserAdded,
		AddedAt:      now,
		LastOpenedAt: now,
	}
	r.workspaces = append(r.workspaces, record)
	r.workspaces = normalizeWorkspaceRecords(r.workspaces)
	if err := r.saveLocked(); err != nil {
		return workspaceRecord{}, false, err
	}
	return record, true, nil
}

func (r *workspaceRegistry) Update(workspaceID, displayName string) (workspaceRecord, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return workspaceRecord{}, fmt.Errorf("workspace_id is required")
	}
	displayName = strings.TrimSpace(displayName)
	if displayName == "" {
		return workspaceRecord{}, fmt.Errorf("display_name is required")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	for i, workspace := range r.workspaces {
		if workspace.WorkspaceID != workspaceID {
			continue
		}
		workspace.DisplayName = displayName
		r.workspaces[i] = workspace
		if err := r.saveLocked(); err != nil {
			return workspaceRecord{}, err
		}
		return workspace, nil
	}
	return workspaceRecord{}, os.ErrNotExist
}

func (r *workspaceRegistry) Remove(workspaceID string) (bool, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return false, fmt.Errorf("workspace_id is required")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	for i, workspace := range r.workspaces {
		if workspace.WorkspaceID != workspaceID {
			continue
		}
		r.workspaces = append(r.workspaces[:i], r.workspaces[i+1:]...)
		if err := r.saveLocked(); err != nil {
			return false, err
		}
		return true, nil
	}
	return false, nil
}

func (r *workspaceRegistry) saveLocked() error {
	payload, err := json.MarshalIndent(workspaceRegistryFile{Workspaces: r.workspaces}, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(r.path)
	if err := privdir.Ensure(dir); err != nil {
		return err
	}
	tempFile, err := os.CreateTemp(dir, "workspaces-*.json")
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
	if _, err := tempFile.Write(payload); err != nil {
		_ = tempFile.Close()
		return err
	}
	if err := tempFile.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tempPath, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tempPath, r.path); err != nil {
		return err
	}
	cleanup = false
	return nil
}

func canonicalizeWorkspacePath(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", fmt.Errorf("path is required")
	}
	if !filepath.IsAbs(path) {
		return "", fmt.Errorf("path must be an absolute path")
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("workspace unavailable: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("workspace is not a directory: %s", path)
	}
	return taskstore.NormalizeWorkDir(path), nil
}

func normalizedDisplayName(displayName, path string) string {
	displayName = strings.TrimSpace(displayName)
	if displayName != "" {
		return displayName
	}
	base := filepath.Base(path)
	if base == "." || base == string(filepath.Separator) || strings.TrimSpace(base) == "" {
		return path
	}
	return base
}

func normalizeWorkspaceRecords(records []workspaceRecord) []workspaceRecord {
	if len(records) == 0 {
		return make([]workspaceRecord, 0)
	}
	byPath := make(map[string]workspaceRecord, len(records))
	for _, record := range records {
		record.WorkspaceID = strings.TrimSpace(record.WorkspaceID)
		record.Path = taskstore.NormalizeWorkDir(strings.TrimSpace(record.Path))
		record.DisplayName = normalizedDisplayName(record.DisplayName, record.Path)
		if record.WorkspaceID == "" || record.Path == "" {
			continue
		}
		if record.Source == "" {
			record.Source = workspaceSourceUserAdded
		}
		if existing, ok := byPath[record.Path]; ok {
			if record.LastOpenedAt.After(existing.LastOpenedAt) {
				byPath[record.Path] = record
			}
			continue
		}
		byPath[record.Path] = record
	}
	out := make([]workspaceRecord, 0, len(byPath))
	for _, record := range byPath {
		out = append(out, record)
	}
	sort.Slice(out, func(i, j int) bool {
		left := out[i].LastOpenedAt
		if left.IsZero() {
			left = out[i].AddedAt
		}
		right := out[j].LastOpenedAt
		if right.IsZero() {
			right = out[j].AddedAt
		}
		if left.Equal(right) {
			return out[i].DisplayName < out[j].DisplayName
		}
		return left.After(right)
	})
	return out
}

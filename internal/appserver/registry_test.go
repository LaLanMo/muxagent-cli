package appserver

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWorkspaceRegistryAddListUpdateRemove(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
	workDir := t.TempDir()
	registry := newWorkspaceRegistry(filepath.Join(t.TempDir(), "workspaces.json"), func() time.Time { return now })
	if err := registry.Load(); err != nil {
		t.Fatalf("load registry: %v", err)
	}

	record, created, err := registry.Add(workDir, "")
	if err != nil {
		t.Fatalf("add workspace: %v", err)
	}
	if !created {
		t.Fatal("created = false, want true")
	}
	if record.DisplayName != filepath.Base(workDir) {
		t.Fatalf("display_name = %q, want %q", record.DisplayName, filepath.Base(workDir))
	}

	workspaces := registry.List()
	if len(workspaces) != 1 {
		t.Fatalf("workspace count = %d, want 1", len(workspaces))
	}

	updated, err := registry.Update(record.WorkspaceID, "cmdr")
	if err != nil {
		t.Fatalf("update display name: %v", err)
	}
	if updated.DisplayName != "cmdr" {
		t.Fatalf("display_name = %q, want cmdr", updated.DisplayName)
	}

	ok, err := registry.Remove(record.WorkspaceID)
	if err != nil {
		t.Fatalf("remove workspace: %v", err)
	}
	if !ok {
		t.Fatal("remove ok = false, want true")
	}
	if got := len(registry.List()); got != 0 {
		t.Fatalf("workspace count after remove = %d, want 0", got)
	}
}

func TestWorkspaceRegistryDedupesCanonicalPath(t *testing.T) {
	root := t.TempDir()
	realDir := filepath.Join(root, "real")
	linkDir := filepath.Join(root, "link")
	if err := os.MkdirAll(realDir, 0o755); err != nil {
		t.Fatalf("mkdir real: %v", err)
	}
	if err := os.Symlink(realDir, linkDir); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	registry := newWorkspaceRegistry(filepath.Join(t.TempDir(), "workspaces.json"), time.Now)
	if err := registry.Load(); err != nil {
		t.Fatalf("load registry: %v", err)
	}

	first, created, err := registry.Add(realDir, "real")
	if err != nil {
		t.Fatalf("add real: %v", err)
	}
	if !created {
		t.Fatal("created for first add = false, want true")
	}

	second, created, err := registry.Add(linkDir, "link")
	if err != nil {
		t.Fatalf("add link: %v", err)
	}
	if created {
		t.Fatal("created for second add = true, want false")
	}
	if second.WorkspaceID != first.WorkspaceID {
		t.Fatalf("workspace_id mismatch: got %q want %q", second.WorkspaceID, first.WorkspaceID)
	}
	if got := len(registry.List()); got != 1 {
		t.Fatalf("workspace count = %d, want 1", got)
	}
}

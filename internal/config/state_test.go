package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestSaveState_TightensParentDirPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("directory permissions are not portable on windows")
	}

	home := t.TempDir()
	t.Setenv("HOME", home)

	dir := filepath.Join(home, ".muxagent")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	_, err := SaveState(DaemonState{PID: 1234})
	if err != nil {
		t.Fatalf("SaveState: %v", err)
	}

	assertDirPerm(t, dir, 0o700)
}

func TestAcquireLock_TightensParentDirPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("directory permissions are not portable on windows")
	}

	home := t.TempDir()
	t.Setenv("HOME", home)

	dir := filepath.Join(home, ".muxagent")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	lock, err := AcquireLock(1234)
	if err != nil {
		t.Fatalf("AcquireLock: %v", err)
	}
	t.Cleanup(func() {
		if err := ReleaseLock(lock); err != nil {
			t.Fatalf("ReleaseLock: %v", err)
		}
	})

	assertDirPerm(t, dir, 0o700)
}

func TestSaveTaskLaunchPreferences_UsesTightPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file permissions are not portable on windows")
	}

	home := t.TempDir()
	t.Setenv("HOME", home)

	path, err := SaveTaskLaunchPreferences(TaskLaunchPreferences{UseWorktree: true})
	if err != nil {
		t.Fatalf("SaveTaskLaunchPreferences: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat(%q): %v", path, err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("permissions for %q = %04o, want %04o", path, got, 0o600)
	}
}

func TestLoadTaskLaunchPreferences_FallsBackToFalse(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	if got := LoadTaskLaunchPreferences(); got.UseWorktree {
		t.Fatalf("missing preferences should default to false")
	}

	path, err := TaskLaunchPreferencesPath()
	if err != nil {
		t.Fatalf("TaskLaunchPreferencesPath: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte("{"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if got := LoadTaskLaunchPreferences(); got.UseWorktree {
		t.Fatalf("corrupt preferences should default to false")
	}
}

func TestSaveStartupUpdateState_RoundTrips(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	now := time.Date(2026, time.March, 31, 12, 0, 0, 0, time.UTC)
	want := StartupUpdateState{
		LastCheckedAt:  now,
		LastFailedAt:   now.Add(-2 * time.Hour),
		SkippedVersion: "v1.2.3",
	}

	path, err := SaveStartupUpdateState(want)
	if err != nil {
		t.Fatalf("SaveStartupUpdateState: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("Stat(%q): %v", path, err)
	}

	got := LoadStartupUpdateState()
	if !got.LastCheckedAt.Equal(want.LastCheckedAt) {
		t.Fatalf("LastCheckedAt = %v, want %v", got.LastCheckedAt, want.LastCheckedAt)
	}
	if !got.LastFailedAt.Equal(want.LastFailedAt) {
		t.Fatalf("LastFailedAt = %v, want %v", got.LastFailedAt, want.LastFailedAt)
	}
	if got.SkippedVersion != want.SkippedVersion {
		t.Fatalf("SkippedVersion = %q, want %q", got.SkippedVersion, want.SkippedVersion)
	}
}

func TestSaveStartupUpdateState_UsesTightPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file permissions are not portable on windows")
	}

	home := t.TempDir()
	t.Setenv("HOME", home)

	path, err := SaveStartupUpdateState(StartupUpdateState{SkippedVersion: "v1.2.3"})
	if err != nil {
		t.Fatalf("SaveStartupUpdateState: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat(%q): %v", path, err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("permissions for %q = %04o, want %04o", path, got, 0o600)
	}
}

func TestLoadStartupUpdateState_FallsBackToZeroValue(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	if got := LoadStartupUpdateState(); !got.LastCheckedAt.IsZero() || !got.LastFailedAt.IsZero() || got.SkippedVersion != "" {
		t.Fatalf("missing state should return zero value, got %#v", got)
	}

	path, err := StartupUpdateStatePath()
	if err != nil {
		t.Fatalf("StartupUpdateStatePath: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte("{"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if got := LoadStartupUpdateState(); !got.LastCheckedAt.IsZero() || !got.LastFailedAt.IsZero() || got.SkippedVersion != "" {
		t.Fatalf("corrupt state should return zero value, got %#v", got)
	}
}

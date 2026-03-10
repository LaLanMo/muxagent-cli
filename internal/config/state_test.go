package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
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

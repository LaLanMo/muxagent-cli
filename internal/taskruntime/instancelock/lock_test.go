//go:build darwin || linux

package instancelock

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAcquireAndRelease(t *testing.T) {
	dir := t.TempDir()
	lock, err := Acquire(dir)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	// Lock file should exist with our PID.
	data, err := os.ReadFile(filepath.Join(dir, ".muxagent", lockFileName))
	if err != nil {
		t.Fatalf("read lock file: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("lock file is empty, expected PID")
	}
	if err := lock.Release(); err != nil {
		t.Fatalf("Release: %v", err)
	}
	// Lock file should be removed after release.
	if _, err := os.Stat(filepath.Join(dir, ".muxagent", lockFileName)); !os.IsNotExist(err) {
		t.Fatalf("lock file still exists after release")
	}
}

func TestSecondAcquireFails(t *testing.T) {
	dir := t.TempDir()
	lock1, err := Acquire(dir)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	defer lock1.Release()

	_, err = Acquire(dir)
	if err == nil {
		t.Fatal("expected second Acquire to fail, got nil")
	}
	want := "another muxagent instance is already running in this directory"
	if err.Error() != want {
		t.Fatalf("unexpected error: %q, want %q", err.Error(), want)
	}
}

func TestReacquireAfterRelease(t *testing.T) {
	dir := t.TempDir()
	lock1, err := Acquire(dir)
	if err != nil {
		t.Fatalf("first Acquire: %v", err)
	}
	if err := lock1.Release(); err != nil {
		t.Fatalf("Release: %v", err)
	}
	lock2, err := Acquire(dir)
	if err != nil {
		t.Fatalf("re-Acquire after release: %v", err)
	}
	defer lock2.Release()
}

func TestNilRelease(t *testing.T) {
	var lock *Lock
	if err := lock.Release(); err != nil {
		t.Fatalf("nil Release: %v", err)
	}
}

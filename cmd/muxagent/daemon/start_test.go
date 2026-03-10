package daemon

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestOpenDaemonLogFile_TightensParentDirPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("directory permissions are not portable on windows")
	}

	home := t.TempDir()
	t.Setenv("HOME", home)

	dir := filepath.Join(home, ".muxagent")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	path, file, err := openDaemonLogFile()
	if err != nil {
		t.Fatalf("openDaemonLogFile: %v", err)
	}
	t.Cleanup(func() {
		if err := file.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
	})

	if want := filepath.Join(dir, "daemon.log"); path != want {
		t.Fatalf("path = %q, want %q", path, want)
	}

	assertDirPerm(t, dir, 0o700)
}

func assertDirPerm(t *testing.T, path string, want os.FileMode) {
	t.Helper()

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat(%q): %v", path, err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Fatalf("permissions for %q = %04o, want %04o", path, got, want)
	}
}

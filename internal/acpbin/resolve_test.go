package acpbin

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/LaLanMo/muxagent-cli/internal/config"
)

func TestResolve_EnvOverride(t *testing.T) {
	// When env var overrides the command, Resolve should return it directly.
	t.Setenv("MUXAGENT_RUNTIMES_CLAUDE-CODE_COMMAND", "/usr/local/bin/custom-acp")

	cfg := config.Default()
	cfg.Runtimes[config.RuntimeClaudeCode] = config.RuntimeSettings{
		Command: "/usr/local/bin/custom-acp",
	}

	path, err := Resolve(cfg, nil)
	if err != nil {
		t.Fatalf("Resolve() error: %v", err)
	}
	if path != "/usr/local/bin/custom-acp" {
		t.Errorf("Resolve() = %q, want /usr/local/bin/custom-acp", path)
	}
}

func TestResolve_ManagedPathExists(t *testing.T) {
	// Create a fake managed binary
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	managedBin, err := ManagedPath()
	if err != nil {
		t.Fatal(err)
	}
	binDir := filepath.Dir(managedBin)
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(managedBin, []byte("fake"), 0o755); err != nil {
		t.Fatal(err)
	}

	cfg := config.Default()
	path, err := Resolve(cfg, nil)
	if err != nil {
		t.Fatalf("Resolve() error: %v", err)
	}
	if path != managedBin {
		t.Errorf("Resolve() = %q, want %q", path, managedBin)
	}
}

func TestResolve_RelativePathExists(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	exePath := filepath.Join(dir, "muxagent")

	prev := currentExecutablePath
	currentExecutablePath = func() (string, error) {
		return exePath, nil
	}
	t.Cleanup(func() {
		currentExecutablePath = prev
	})

	relativeBin := filepath.Join(dir, binaryName(runtime.GOOS))
	if err := os.WriteFile(relativeBin, []byte("fake"), 0o755); err != nil {
		t.Fatal(err)
	}

	cfg := config.Default()
	path, err := Resolve(cfg, nil)
	if err != nil {
		t.Fatalf("Resolve() error: %v", err)
	}
	if path != relativeBin {
		t.Errorf("Resolve() = %q, want %q", path, relativeBin)
	}
}

func TestResolveManaged_SkipsRelativePath(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	exePath := filepath.Join(dir, "muxagent")

	prev := currentExecutablePath
	currentExecutablePath = func() (string, error) {
		return exePath, nil
	}
	t.Cleanup(func() {
		currentExecutablePath = prev
	})

	relativeBin := filepath.Join(dir, binaryName(runtime.GOOS))
	if err := os.WriteFile(relativeBin, []byte("stale"), 0o755); err != nil {
		t.Fatal(err)
	}

	managedBin, err := ManagedPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(managedBin), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(managedBin, []byte("managed"), 0o755); err != nil {
		t.Fatal(err)
	}

	cfg := config.Default()
	path, err := ResolveManaged(cfg, nil)
	if err != nil {
		t.Fatalf("ResolveManaged() error: %v", err)
	}
	if path != managedBin {
		t.Errorf("ResolveManaged() = %q, want %q", path, managedBin)
	}
}

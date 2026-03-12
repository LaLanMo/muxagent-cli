package codexbin

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/LaLanMo/muxagent-cli/internal/config"
)

func TestResolve_EnvOverride(t *testing.T) {
	t.Setenv("MUXAGENT_RUNTIMES_CODEX_COMMAND", "/usr/local/bin/custom-codex-acp")

	cfg := config.Default()
	cfg.Runtimes[config.RuntimeCodex] = config.RuntimeSettings{
		Command: "/usr/local/bin/custom-codex-acp",
	}

	path, err := Resolve(cfg, nil)
	if err != nil {
		t.Fatalf("Resolve() error: %v", err)
	}
	if path != "/usr/local/bin/custom-codex-acp" {
		t.Fatalf("Resolve() = %q, want /usr/local/bin/custom-codex-acp", path)
	}
}

func TestResolve_ManagedPathExists(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	managedBin, err := ManagedPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(managedBin), 0o755); err != nil {
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
		t.Fatalf("Resolve() = %q, want %q", path, managedBin)
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
	t.Cleanup(func() { currentExecutablePath = prev })

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
		t.Fatalf("Resolve() = %q, want %q", path, relativeBin)
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
	t.Cleanup(func() { currentExecutablePath = prev })

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
		t.Fatalf("ResolveManaged() = %q, want %q", path, managedBin)
	}
}

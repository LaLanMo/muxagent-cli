package acpbin

import (
	"os"
	"path/filepath"
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

	binDir := filepath.Join(dir, ".muxagent", "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}

	managedBin := filepath.Join(binDir, "claude-agent-acp-"+ACPVersion)
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

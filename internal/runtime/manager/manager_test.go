package manager

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/LaLanMo/muxagent-cli/internal/acpbin"
	"github.com/LaLanMo/muxagent-cli/internal/config"
)

func TestResolveSettings_ClaudeInjectsWrapper(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	managedBin, err := acpbin.ManagedPath()
	if err != nil {
		t.Fatalf("ManagedPath: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(managedBin), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(managedBin, []byte("fake"), 0o755); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cfg := config.Default()
	m := New(cfg)

	got, err := m.resolveSettings(config.RuntimeClaudeCode, cfg.Runtimes[config.RuntimeClaudeCode])
	if err != nil {
		t.Fatalf("resolveSettings: %v", err)
	}
	if got.Command != managedBin {
		t.Fatalf("command = %q, want %q", got.Command, managedBin)
	}
	if got.Env["CLAUDE_CODE_EXECUTABLE"] == "" {
		t.Fatal("expected CLAUDE_CODE_EXECUTABLE wrapper to be injected")
	}
}

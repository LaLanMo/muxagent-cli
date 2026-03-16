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

	got, err := m.resolveSettings(config.RuntimeClaudeCode, cfg.Runtimes[config.RuntimeClaudeCode], "")
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

func TestResolveSettings_UsesSessionStartupCWDWhenRuntimeCWDUnset(t *testing.T) {
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

	got, err := m.resolveSettings(config.RuntimeCodex, cfg.Runtimes[config.RuntimeCodex], "/tmp/project")
	if err != nil {
		t.Fatalf("resolveSettings: %v", err)
	}
	if got.CWD != "/tmp/project" {
		t.Fatalf("cwd = %q, want /tmp/project", got.CWD)
	}
}

func TestResolveSettings_PrefersConfiguredRuntimeCWDOverSessionStartupCWD(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cfg := config.Default()
	cfg.Runtimes[config.RuntimeCodex] = config.RuntimeSettings{
		Command: "/custom/codex-acp",
		CWD:     "/configured/runtime",
	}
	m := New(cfg)

	got, err := m.resolveSettings(config.RuntimeCodex, cfg.Runtimes[config.RuntimeCodex], "/tmp/project")
	if err != nil {
		t.Fatalf("resolveSettings: %v", err)
	}
	if got.CWD != "/configured/runtime" {
		t.Fatalf("cwd = %q, want /configured/runtime", got.CWD)
	}
}

func TestSelectRuntimeStartupCWD_FallsBackToHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	got := selectRuntimeStartupCWD("", "")
	if got != home {
		t.Fatalf("cwd = %q, want %q", got, home)
	}
}

package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefault_ContainsBothRuntimes(t *testing.T) {
	cfg := Default()

	oc, ok := cfg.Runtimes[RuntimeOpenCode]
	if !ok {
		t.Fatal("default config missing opencode runtime")
	}
	if oc.Command != "opencode" {
		t.Errorf("opencode command = %q, want %q", oc.Command, "opencode")
	}
	if len(oc.Args) != 1 || oc.Args[0] != "acp" {
		t.Errorf("opencode args = %v, want [acp]", oc.Args)
	}

	cc, ok := cfg.Runtimes[RuntimeClaudeCode]
	if !ok {
		t.Fatal("default config missing claude-code runtime")
	}
	if cc.Command != "" {
		t.Errorf("claude-code command = %q, want empty (resolved at runtime)", cc.Command)
	}
	if v, exists := cc.Env["CLAUDECODE"]; !exists || v != "" {
		t.Errorf("claude-code Env[CLAUDECODE] = %q (exists=%v), want empty-string sentinel", v, exists)
	}
}

func TestActiveRuntimeSettings_ClaudeCode(t *testing.T) {
	cfg := Default()
	cfg.ActiveRuntime = RuntimeClaudeCode

	settings, err := cfg.ActiveRuntimeSettings()
	if err != nil {
		t.Fatalf("ActiveRuntimeSettings: %v", err)
	}
	if settings.Command != "" {
		t.Errorf("command = %q, want empty (resolved at runtime)", settings.Command)
	}
	if v, ok := settings.Env["CLAUDECODE"]; !ok || v != "" {
		t.Errorf("Env[CLAUDECODE] = %q (ok=%v), want empty-string", v, ok)
	}
}

func TestActiveRuntimeSettings_UnknownRuntime(t *testing.T) {
	cfg := Default()
	cfg.ActiveRuntime = "nonexistent"

	_, err := cfg.ActiveRuntimeSettings()
	if err == nil {
		t.Fatal("expected error for unknown runtime, got nil")
	}
	if want := `runtime "nonexistent" not configured`; err.Error() != want {
		t.Errorf("error = %q, want %q", err.Error(), want)
	}
}

func TestMergeConfig_OverlayClaudeCodeCommand(t *testing.T) {
	base := Default()
	overlay := Config{
		Runtimes: map[RuntimeID]RuntimeSettings{
			RuntimeClaudeCode: {
				Command: "/usr/local/bin/claude-agent-acp",
			},
		},
	}

	merged := mergeConfig(base, overlay)

	cc := merged.Runtimes[RuntimeClaudeCode]
	if cc.Command != "/usr/local/bin/claude-agent-acp" {
		t.Errorf("command = %q, want overlay value", cc.Command)
	}
	// Base Env should be preserved when overlay Env is empty.
	if v, ok := cc.Env["CLAUDECODE"]; !ok || v != "" {
		t.Errorf("Env[CLAUDECODE] = %q (ok=%v), want preserved empty-string sentinel", v, ok)
	}
}

func TestSaveAndLoad_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	cfg := Default()
	cfg.ActiveRuntime = RuntimeClaudeCode

	savedPath, err := SaveTo(cfg, path)
	if err != nil {
		t.Fatalf("SaveTo: %v", err)
	}
	if savedPath != path {
		t.Errorf("savedPath = %q, want %q", savedPath, path)
	}

	loaded, err := loadFile(path)
	if err != nil {
		t.Fatalf("loadFile: %v", err)
	}
	if loaded.ActiveRuntime != RuntimeClaudeCode {
		t.Errorf("ActiveRuntime = %q, want %q", loaded.ActiveRuntime, RuntimeClaudeCode)
	}
	if _, ok := loaded.Runtimes[RuntimeClaudeCode]; !ok {
		t.Error("loaded config missing claude-code runtime")
	}
	if _, ok := loaded.Runtimes[RuntimeOpenCode]; !ok {
		t.Error("loaded config missing opencode runtime")
	}
}

func TestApplyEnvOverrides_ClaudeCodeCommand(t *testing.T) {
	cfg := Default()

	t.Setenv("MUXAGENT_RUNTIMES_CLAUDE-CODE_COMMAND", "/custom/claude-agent-acp")
	result := applyEnvOverrides(cfg)

	cc := result.Runtimes[RuntimeClaudeCode]
	if cc.Command != "/custom/claude-agent-acp" {
		t.Errorf("command = %q, want %q", cc.Command, "/custom/claude-agent-acp")
	}

	// OpenCode should be unaffected.
	oc := result.Runtimes[RuntimeOpenCode]
	if oc.Command != "opencode" {
		t.Errorf("opencode command = %q, want %q", oc.Command, "opencode")
	}

	// Clean up: verify env var was read (Setenv auto-cleans via t.Cleanup).
	_ = os.Getenv("MUXAGENT_RUNTIMES_CLAUDE-CODE_COMMAND")
}

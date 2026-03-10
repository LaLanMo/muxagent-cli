package config

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"os"
	"path/filepath"
	"runtime"
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
		RelaySigningPublicKey: "relay-pub",
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
	if merged.RelaySigningPublicKey != "relay-pub" {
		t.Errorf("RelaySigningPublicKey = %q, want overlay value", merged.RelaySigningPublicKey)
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

func TestSaveTo_TightensParentDirPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("directory permissions are not portable on windows")
	}

	root := t.TempDir()
	dir := filepath.Join(root, ".muxagent")
	path := filepath.Join(dir, "config.json")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	_, err := SaveTo(Default(), path)
	if err != nil {
		t.Fatalf("SaveTo: %v", err)
	}

	assertDirPerm(t, dir, 0o700)
}

func TestApplyEnvOverrides_ClaudeCodeCommand(t *testing.T) {
	cfg := Default()

	t.Setenv("MUXAGENT_RUNTIMES_CLAUDE-CODE_COMMAND", "/custom/claude-agent-acp")
	t.Setenv("MUXAGENT_RELAY_SIGNING_PUBLIC_KEY", "relay-pub")
	result := applyEnvOverrides(cfg)

	cc := result.Runtimes[RuntimeClaudeCode]
	if cc.Command != "/custom/claude-agent-acp" {
		t.Errorf("command = %q, want %q", cc.Command, "/custom/claude-agent-acp")
	}
	if result.RelaySigningPublicKey != "relay-pub" {
		t.Errorf("RelaySigningPublicKey = %q, want %q", result.RelaySigningPublicKey, "relay-pub")
	}

	// OpenCode should be unaffected.
	oc := result.Runtimes[RuntimeOpenCode]
	if oc.Command != "opencode" {
		t.Errorf("opencode command = %q, want %q", oc.Command, "opencode")
	}

	// Clean up: verify env var was read (Setenv auto-cleans via t.Cleanup).
	_ = os.Getenv("MUXAGENT_RUNTIMES_CLAUDE-CODE_COMMAND")
}

func TestResolveRelaySigningPublicKey_RemoteRequiresPin(t *testing.T) {
	_, err := ResolveRelaySigningPublicKey("https://relay.example", "")
	if err == nil {
		t.Fatal("expected error for remote relay without pin")
	}
	if want := "pairing with remote relay requires relay_signing_public_key"; err.Error() != want {
		t.Fatalf("error = %q, want %q", err.Error(), want)
	}
}

func TestResolveRelaySigningPublicKey_LoopbackAllowsMissingPin(t *testing.T) {
	pub, err := ResolveRelaySigningPublicKey("http://localhost:8080", "")
	if err != nil {
		t.Fatalf("ResolveRelaySigningPublicKey: %v", err)
	}
	if pub != nil {
		t.Fatalf("pub = %v, want nil", pub)
	}
}

func TestResolveRelaySigningPublicKey_ValidatesEncodingAndLength(t *testing.T) {
	if _, err := ResolveRelaySigningPublicKey("https://relay.example", "bad-base64"); err == nil {
		t.Fatal("expected decode error")
	}

	shortKey := base64.StdEncoding.EncodeToString([]byte("short"))
	if _, err := ResolveRelaySigningPublicKey("https://relay.example", shortKey); err == nil {
		t.Fatal("expected length error")
	}
}

func TestResolveRelaySigningPublicKey_DecodesValidKey(t *testing.T) {
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}

	decoded, err := ResolveRelaySigningPublicKey("https://relay.example", base64.StdEncoding.EncodeToString(pub))
	if err != nil {
		t.Fatalf("ResolveRelaySigningPublicKey: %v", err)
	}
	if got := base64.StdEncoding.EncodeToString(decoded); got != base64.StdEncoding.EncodeToString(pub) {
		t.Fatalf("decoded key mismatch: got %q want %q", got, base64.StdEncoding.EncodeToString(pub))
	}
}

func TestIsLoopbackRelayURL(t *testing.T) {
	tests := []struct {
		name    string
		rawURL  string
		want    bool
		wantErr bool
	}{
		{name: "localhost", rawURL: "http://localhost:8080", want: true},
		{name: "localhost mixed case", rawURL: "http://LocalHost:8080", want: true},
		{name: "ipv4 loopback", rawURL: "https://127.0.0.1/ws", want: true},
		{name: "ipv6 loopback", rawURL: "http://[::1]:8080", want: true},
		{name: "remote host", rawURL: "https://relay.example/ws", want: false},
		{name: "invalid url", rawURL: "://bad", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := IsLoopbackRelayURL(tt.rawURL)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("IsLoopbackRelayURL: %v", err)
			}
			if got != tt.want {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestValidateConfig_RelayURLPolicy(t *testing.T) {
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	validKey := base64.StdEncoding.EncodeToString(pub)

	tests := []struct {
		name    string
		cfg     Config
		wantErr string
	}{
		{
			name: "remote ws rejected",
			cfg: Config{
				RelayURL:              "ws://relay.example/ws",
				RelaySigningPublicKey: validKey,
			},
			wantErr: "non-loopback relay_url must use wss://",
		},
		{
			name: "remote wss accepted",
			cfg: Config{
				RelayURL:              "wss://relay.example/ws",
				RelaySigningPublicKey: validKey,
			},
		},
		{
			name: "loopback ws accepted",
			cfg: Config{
				RelayURL: "ws://localhost:8080/ws",
			},
		},
		{
			name: "remote relay requires signing key",
			cfg: Config{
				RelayURL: "wss://relay.example/ws",
			},
			wantErr: "pairing with remote relay requires relay_signing_public_key",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateConfig(tt.cfg)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("validateConfig: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error %q", tt.wantErr)
			}
			if err.Error() != tt.wantErr {
				t.Fatalf("error = %q, want %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestLoadEffective_ValidatesRelaySigningPublicKeyFormat(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cwd := t.TempDir()
	prevWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	if err := os.Chdir(cwd); err != nil {
		t.Fatalf("Chdir: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(prevWD)
	})

	projectDir := filepath.Join(cwd, ".muxagent")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	configPath := filepath.Join(projectDir, "config.json")
	payload := `{"relay_signing_public_key":"bad-base64"}`
	if err := os.WriteFile(configPath, []byte(payload), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if _, err := LoadEffective(); err == nil {
		t.Fatal("expected validation error")
	}
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

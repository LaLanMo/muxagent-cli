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

func TestDefault_ContainsBuiltInRuntimes(t *testing.T) {
	cfg := Default()

	if cfg.RelayURL != defaultRelayURL {
		t.Fatalf("RelayURL = %q, want %q", cfg.RelayURL, defaultRelayURL)
	}
	if cfg.RelaySigningPublicKey != defaultRelaySigningPublicKey {
		t.Fatalf("RelaySigningPublicKey = %q, want %q", cfg.RelaySigningPublicKey, defaultRelaySigningPublicKey)
	}

	if _, ok := cfg.Runtimes[RuntimeOpenCode]; ok {
		t.Fatal("default config unexpectedly includes opencode runtime")
	}

	cc, ok := cfg.Runtimes[RuntimeClaudeCode]
	if !ok {
		t.Fatal("default config missing claude-code runtime")
	}
	if cc.Command != "" {
		t.Errorf("claude-code command = %q, want empty", cc.Command)
	}
	if v, exists := cc.Env["CLAUDECODE"]; !exists || v != "" {
		t.Errorf("claude-code Env[CLAUDECODE] = %q (exists=%v), want empty-string sentinel", v, exists)
	}

	codex, ok := cfg.Runtimes[RuntimeCodex]
	if !ok {
		t.Fatal("default config missing codex runtime")
	}
	if codex.Command != "" {
		t.Errorf("codex command = %q, want empty", codex.Command)
	}
}

func TestRuntimeSettingsFor(t *testing.T) {
	cfg := Default()

	settings, err := cfg.RuntimeSettingsFor(RuntimeCodex)
	if err != nil {
		t.Fatalf("RuntimeSettingsFor: %v", err)
	}
	if settings.Command != "" {
		t.Fatalf("command = %q, want empty", settings.Command)
	}
}

func TestRuntimeSettingsFor_UnknownRuntime(t *testing.T) {
	cfg := Default()

	_, err := cfg.RuntimeSettingsFor("nonexistent")
	if err == nil {
		t.Fatal("expected error for unknown runtime")
	}
	if want := `runtime "nonexistent" not configured`; err.Error() != want {
		t.Fatalf("error = %q, want %q", err.Error(), want)
	}
}

func TestConfiguredRuntimeIDs_Sorted(t *testing.T) {
	cfg := Config{
		Runtimes: map[RuntimeID]RuntimeSettings{
			RuntimeCodex:      {},
			RuntimeClaudeCode: {},
		},
	}

	ids := cfg.ConfiguredRuntimeIDs()
	if len(ids) != 2 {
		t.Fatalf("len(ids) = %d, want 2", len(ids))
	}
	if ids[0] != RuntimeClaudeCode || ids[1] != RuntimeCodex {
		t.Fatalf("ids = %v, want [claude-code codex]", ids)
	}
}

func TestMergeConfig_NilOverlayRuntimesPreservesBase(t *testing.T) {
	base := Default()
	overlay := Config{
		RelayURL: "ws://localhost:9999/ws",
	}

	merged := mergeConfig(base, overlay)

	if len(merged.Runtimes) != len(base.Runtimes) {
		t.Fatalf("runtime count = %d, want %d", len(merged.Runtimes), len(base.Runtimes))
	}
	if _, ok := merged.Runtimes[RuntimeClaudeCode]; !ok {
		t.Fatal("claude-code runtime missing after nil overlay")
	}
	if _, ok := merged.Runtimes[RuntimeCodex]; !ok {
		t.Fatal("codex runtime missing after nil overlay")
	}
	if merged.RelayURL != overlay.RelayURL {
		t.Fatalf("RelayURL = %q, want overlay value", merged.RelayURL)
	}
}

func TestMergeConfig_ExplicitRuntimesReplaceBase(t *testing.T) {
	base := Default()
	overlay := Config{
		Runtimes: map[RuntimeID]RuntimeSettings{
			RuntimeCodex: {
				CWD: "/tmp/codex",
			},
		},
	}

	merged := mergeConfig(base, overlay)

	if len(merged.Runtimes) != 1 {
		t.Fatalf("runtime count = %d, want 1", len(merged.Runtimes))
	}
	if _, ok := merged.Runtimes[RuntimeClaudeCode]; ok {
		t.Fatal("claude-code runtime should be removed by explicit runtime overlay")
	}
	codex, ok := merged.Runtimes[RuntimeCodex]
	if !ok {
		t.Fatal("codex runtime missing after overlay")
	}
	if codex.Command != "" {
		t.Fatalf("codex command = %q, want empty resolver-backed default", codex.Command)
	}
	if codex.CWD != "/tmp/codex" {
		t.Fatalf("codex cwd = %q, want overlay value", codex.CWD)
	}
}

func TestMergeConfig_RuntimeOverlayPreservesBaseFields(t *testing.T) {
	base := Default()
	overlay := Config{
		Runtimes: map[RuntimeID]RuntimeSettings{
			RuntimeClaudeCode: {
				Command: "/custom/claude-agent-acp",
			},
		},
	}

	merged := mergeConfig(base, overlay)
	cc := merged.Runtimes[RuntimeClaudeCode]
	if cc.Command != "/custom/claude-agent-acp" {
		t.Fatalf("command = %q, want overlay value", cc.Command)
	}
	if v, ok := cc.Env["CLAUDECODE"]; !ok || v != "" {
		t.Fatalf("Env[CLAUDECODE] = %q (ok=%v), want preserved sentinel", v, ok)
	}
}

func TestSaveAndLoad_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	cfg := Default()

	savedPath, err := SaveTo(cfg, path)
	if err != nil {
		t.Fatalf("SaveTo: %v", err)
	}
	if savedPath != path {
		t.Fatalf("savedPath = %q, want %q", savedPath, path)
	}

	loaded, err := loadFile(path)
	if err != nil {
		t.Fatalf("loadFile: %v", err)
	}
	if _, ok := loaded.Runtimes[RuntimeClaudeCode]; !ok {
		t.Fatal("loaded config missing claude-code runtime")
	}
	if _, ok := loaded.Runtimes[RuntimeCodex]; !ok {
		t.Fatal("loaded config missing codex runtime")
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

func TestApplyEnvOverrides_OnlyExistingRuntimes(t *testing.T) {
	cfg := Config{
		Runtimes: map[RuntimeID]RuntimeSettings{
			RuntimeCodex: {},
		},
	}

	t.Setenv("MUXAGENT_RUNTIMES_CODEX_COMMAND", "/custom/codex-acp")
	t.Setenv("MUXAGENT_RUNTIMES_CLAUDE-CODE_COMMAND", "/custom/claude-agent-acp")
	result := applyEnvOverrides(cfg)

	codex := result.Runtimes[RuntimeCodex]
	if codex.Command != "/custom/codex-acp" {
		t.Fatalf("codex command = %q, want env override", codex.Command)
	}
	if _, ok := result.Runtimes[RuntimeClaudeCode]; ok {
		t.Fatal("env override should not create claude-code runtime")
	}
}

func TestLoadEffective_UserRuntimesReplaceDefaults(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	userDir := filepath.Join(home, ".muxagent")
	if err := os.MkdirAll(userDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	userPath := filepath.Join(userDir, "config.json")
	payload := `{"runtimes":{"codex":{"cwd":"/tmp/project"}}}`
	if err := os.WriteFile(userPath, []byte(payload), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cfg, err := LoadEffective()
	if err != nil {
		t.Fatalf("LoadEffective: %v", err)
	}
	if len(cfg.Runtimes) != 1 {
		t.Fatalf("runtime count = %d, want 1", len(cfg.Runtimes))
	}
	if _, ok := cfg.Runtimes[RuntimeClaudeCode]; ok {
		t.Fatal("claude-code runtime should not survive explicit user runtimes")
	}
	codex := cfg.Runtimes[RuntimeCodex]
	if codex.Command != "" {
		t.Fatalf("codex command = %q, want empty resolver-backed default", codex.Command)
	}
	if codex.CWD != "/tmp/project" {
		t.Fatalf("codex cwd = %q, want user override", codex.CWD)
	}
}

func TestLoadEffective_ProjectRuntimesReplaceUser(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	userDir := filepath.Join(home, ".muxagent")
	if err := os.MkdirAll(userDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	userPath := filepath.Join(userDir, "config.json")
	userPayload := `{"runtimes":{"codex":{"cwd":"/tmp/codex"}}}`
	if err := os.WriteFile(userPath, []byte(userPayload), 0o600); err != nil {
		t.Fatalf("WriteFile user: %v", err)
	}

	cwd := t.TempDir()
	prevWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	if err := os.Chdir(cwd); err != nil {
		t.Fatalf("Chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(prevWD) })

	projectDir := filepath.Join(cwd, ".muxagent")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatalf("MkdirAll project: %v", err)
	}
	projectPath := filepath.Join(projectDir, "config.json")
	projectPayload := `{"runtimes":{"claude-code":{"command":"/tmp/claude-agent-acp"}}}`
	if err := os.WriteFile(projectPath, []byte(projectPayload), 0o600); err != nil {
		t.Fatalf("WriteFile project: %v", err)
	}

	cfg, err := LoadEffective()
	if err != nil {
		t.Fatalf("LoadEffective: %v", err)
	}
	if len(cfg.Runtimes) != 1 {
		t.Fatalf("runtime count = %d, want 1", len(cfg.Runtimes))
	}
	if _, ok := cfg.Runtimes[RuntimeCodex]; ok {
		t.Fatal("codex runtime should not survive explicit project runtimes")
	}
	cc := cfg.Runtimes[RuntimeClaudeCode]
	if cc.Command != "/tmp/claude-agent-acp" {
		t.Fatalf("claude command = %q, want project override", cc.Command)
	}
	if v, ok := cc.Env["CLAUDECODE"]; !ok || v != "" {
		t.Fatalf("Env[CLAUDECODE] = %q (ok=%v), want preserved sentinel", v, ok)
	}
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

func TestValidateConfig_RejectsEmptyRuntimeSet(t *testing.T) {
	cfg := Config{}

	err := validateConfig(cfg)
	if err == nil {
		t.Fatal("expected error for empty runtime set")
	}
	if want := "at least one runtime must be configured"; err.Error() != want {
		t.Fatalf("error = %q, want %q", err.Error(), want)
	}
}

func TestValidateConfig_RejectsUnsupportedRuntime(t *testing.T) {
	cfg := Config{
		Runtimes: map[RuntimeID]RuntimeSettings{
			RuntimeOpenCode: {},
		},
	}

	err := validateConfig(cfg)
	if err == nil {
		t.Fatal("expected error for unsupported runtime")
	}
	if want := `runtime "opencode" is not supported`; err.Error() != want {
		t.Fatalf("error = %q, want %q", err.Error(), want)
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
				Runtimes:              map[RuntimeID]RuntimeSettings{RuntimeCodex: {}},
				RelayURL:              "ws://relay.example/ws",
				RelaySigningPublicKey: validKey,
			},
			wantErr: "non-loopback relay_url must use wss://",
		},
		{
			name: "remote wss accepted",
			cfg: Config{
				Runtimes:              map[RuntimeID]RuntimeSettings{RuntimeCodex: {}},
				RelayURL:              "wss://relay.example/ws",
				RelaySigningPublicKey: validKey,
			},
		},
		{
			name: "loopback ws accepted",
			cfg: Config{
				Runtimes: map[RuntimeID]RuntimeSettings{RuntimeCodex: {}},
				RelayURL: "ws://localhost:8080/ws",
			},
		},
		{
			name: "remote relay requires signing key",
			cfg: Config{
				Runtimes: map[RuntimeID]RuntimeSettings{RuntimeCodex: {}},
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

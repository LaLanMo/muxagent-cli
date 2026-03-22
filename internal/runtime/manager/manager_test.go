package manager

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LaLanMo/muxagent-cli/internal/acpbin"
	"github.com/LaLanMo/muxagent-cli/internal/acpprotocol"
	"github.com/LaLanMo/muxagent-cli/internal/appwire"
	"github.com/LaLanMo/muxagent-cli/internal/config"
	"github.com/LaLanMo/muxagent-cli/internal/domain"
)

func TestResolveSettings_ClaudeInjectsWrapper(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Chdir(t.TempDir())

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
	t.Chdir(t.TempDir())
	t.Setenv("MUXAGENT_RUNTIMES_CODEX_COMMAND", filepath.Join(home, "codex-acp"))

	codexBin := filepath.Join(home, "codex-acp")
	if err := os.WriteFile(codexBin, []byte("fake"), 0o755); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

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
	cfg.Runtimes[config.RuntimeCodex] = config.RuntimeSettings{
		Command: codexBin,
	}
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
	t.Chdir(t.TempDir())
	command := filepath.Join(home, "codex-acp")
	t.Setenv("MUXAGENT_RUNTIMES_CODEX_COMMAND", command)
	if err := os.WriteFile(command, []byte("fake"), 0o755); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cfg := config.Default()
	cfg.Runtimes[config.RuntimeCodex] = config.RuntimeSettings{
		Command: command,
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

type fakeRuntimeClient struct {
	alive     bool
	loadErr   error
	listErr   error
	loadCalls int
	listCalls int
	stopCalls int
}

func (f *fakeRuntimeClient) Start(context.Context) error { return nil }
func (f *fakeRuntimeClient) Stop() error {
	f.stopCalls++
	f.alive = false
	return nil
}
func (f *fakeRuntimeClient) NewSession(context.Context, string, string) (acpprotocol.NewSessionResponse, error) {
	return acpprotocol.NewSessionResponse{}, nil
}
func (f *fakeRuntimeClient) LoadSession(context.Context, string, string, string, string) (acpprotocol.LoadSessionResponse, error) {
	f.loadCalls++
	return acpprotocol.LoadSessionResponse{}, f.loadErr
}
func (f *fakeRuntimeClient) ListSessions(context.Context, string) ([]domain.SessionSummary, error) {
	f.listCalls++
	return nil, f.listErr
}
func (f *fakeRuntimeClient) Prompt(context.Context, string, []domain.ContentBlock) (string, *domain.PromptUsage, error) {
	return "", nil, nil
}
func (f *fakeRuntimeClient) Cancel(context.Context, string) error { return nil }
func (f *fakeRuntimeClient) SetMode(context.Context, string, string) error {
	return nil
}
func (f *fakeRuntimeClient) SetConfigOption(context.Context, string, string, string) error {
	return nil
}
func (f *fakeRuntimeClient) ReplyPermission(context.Context, string, string, string) error {
	return nil
}
func (f *fakeRuntimeClient) Events() <-chan appwire.Event { return nil }
func (f *fakeRuntimeClient) IsAlive() bool                { return f.alive }

type fakeRuntimeError string

func (e fakeRuntimeError) Error() string { return string(e) }

func TestRuntimeClientAliveUsesHealthInterface(t *testing.T) {
	if runtimeClientAlive(&fakeRuntimeClient{alive: false}) {
		t.Fatal("expected dead runtime client to be reported as dead")
	}
	if !runtimeClientAlive(&fakeRuntimeClient{alive: true}) {
		t.Fatal("expected live runtime client to be reported as live")
	}
}

func TestShouldRetryRuntimeCallForStaleTransportErrors(t *testing.T) {
	if !shouldRetryRuntimeCall(
		&fakeRuntimeClient{alive: false},
		context.DeadlineExceeded,
	) {
		t.Fatal("expected dead runtime client to trigger retry")
	}
	if !shouldRetryRuntimeCall(
		&fakeRuntimeClient{alive: true},
		fakeRuntimeError("broken pipe"),
	) {
		t.Fatal("expected broken pipe to trigger retry")
	}
	if shouldRetryRuntimeCall(
		&fakeRuntimeClient{alive: true},
		fakeRuntimeError("invalid params"),
	) {
		t.Fatal("did not expect ordinary app error to trigger retry")
	}
}

func TestLoadSessionRetiresStaleRuntimeWithoutRetryingReplay(t *testing.T) {
	cfg := config.Default()
	m := New(cfg)

	client := &fakeRuntimeClient{
		alive:   true,
		loadErr: fakeRuntimeError("broken pipe"),
	}
	rid := config.RuntimeCodex
	m.runtimes[rid].client = client
	m.sessionRuntime["session-123"] = rid

	_, _, err := m.LoadSession(context.Background(), "", "session-123", "/tmp/project", "", "")
	if err == nil {
		t.Fatal("expected load session to fail")
	}
	if !strings.Contains(err.Error(), "broken pipe") {
		t.Fatalf("error = %v, want broken pipe", err)
	}
	if client.loadCalls != 1 {
		t.Fatalf("loadCalls = %d, want 1", client.loadCalls)
	}
	if client.stopCalls != 1 {
		t.Fatalf("stopCalls = %d, want 1", client.stopCalls)
	}
	if got := m.runtimes[rid].client; got != nil {
		t.Fatalf("runtime client = %#v, want nil after retirement", got)
	}
}

func TestResolveSessionsReturnsRestartFailureAfterRetiringStaleRuntime(t *testing.T) {
	t.Setenv("MUXAGENT_RUNTIMES_CODEX_COMMAND", "/definitely/missing-codex-acp")

	cfg := config.Default()
	cfg.Runtimes[config.RuntimeCodex] = config.RuntimeSettings{
		Command: "/definitely/missing-codex-acp",
		CWD:     "/tmp/project",
	}
	m := New(cfg)

	client := &fakeRuntimeClient{
		alive:   true,
		listErr: fakeRuntimeError("broken pipe"),
	}
	m.runtimes[config.RuntimeCodex].client = client

	_, err := m.ResolveSessions(context.Background(), string(config.RuntimeCodex), nil)
	if err == nil {
		t.Fatal("expected resolve sessions to fail")
	}
	if !strings.Contains(err.Error(), "restart failed") {
		t.Fatalf("error = %v, want restart failed context", err)
	}
	if !strings.Contains(err.Error(), "start runtime") {
		t.Fatalf("error = %v, want start runtime failure", err)
	}
	if client.listCalls != 1 {
		t.Fatalf("listCalls = %d, want 1", client.listCalls)
	}
	if client.stopCalls != 1 {
		t.Fatalf("stopCalls = %d, want 1", client.stopCalls)
	}
}

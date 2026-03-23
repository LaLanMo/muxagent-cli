package manager

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
	alive          bool
	newSessionResp acpprotocol.NewSessionResponse
	loadResp       acpprotocol.LoadSessionResponse
	listSessions   []domain.SessionSummary
	loadErr        error
	listErr        error
	loadCalls      int
	listCalls      int
	stopCalls      int
}

func (f *fakeRuntimeClient) Start(context.Context) error { return nil }
func (f *fakeRuntimeClient) Stop() error {
	f.stopCalls++
	f.alive = false
	return nil
}
func (f *fakeRuntimeClient) NewSession(context.Context, string, string) (acpprotocol.NewSessionResponse, error) {
	return f.newSessionResp, nil
}
func (f *fakeRuntimeClient) LoadSession(context.Context, string, string, string, string) (acpprotocol.LoadSessionResponse, error) {
	f.loadCalls++
	return f.loadResp, f.loadErr
}
func (f *fakeRuntimeClient) ListSessions(context.Context, string) ([]domain.SessionSummary, error) {
	f.listCalls++
	return f.listSessions, f.listErr
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

func TestResolveSessionsUsesStoredConfigOptionsAfterManagerRestart(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cfg := config.Default()
	m1 := New(cfg)
	modeCategory := "mode"
	m1.persistSessionSnapshot(
		"session-123",
		config.RuntimeCodex,
		[]acpprotocol.SessionConfigOption{{
			ID:           "mode",
			Name:         "Approval Preset",
			Type:         "select",
			Category:     &modeCategory,
			CurrentValue: "full-access",
		}},
	)

	m2 := New(cfg)
	client := &fakeRuntimeClient{
		alive: true,
		listSessions: []domain.SessionSummary{{
			SessionID: "session-123",
			CWD:       "/tmp/project",
			Title:     "Stored Session",
			Runtime:   string(config.RuntimeCodex),
			UpdatedAt: time.Now(),
		}},
	}
	m2.runtimes[config.RuntimeCodex].client = client

	sessions, err := m2.ResolveSessions(
		context.Background(),
		string(config.RuntimeCodex),
		[]string{"session-123"},
	)
	if err != nil {
		t.Fatalf("ResolveSessions: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("len(sessions) = %d, want 1", len(sessions))
	}
	if got := sessions[0].ConfigOptions; len(got) != 1 {
		t.Fatalf("len(configOptions) = %d, want 1", len(got))
	} else if got[0].CurrentValue != "full-access" {
		t.Fatalf("current mode = %q, want full-access", got[0].CurrentValue)
	}
	if got := m2.resolveRuntimeID("session-123", ""); got != config.RuntimeCodex {
		t.Fatalf("runtime = %q, want %q", got, config.RuntimeCodex)
	}
}

func TestSetModePersistsStoredConfigSnapshot(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cfg := config.Default()
	modeCategory := "mode"
	m1 := New(cfg)
	client := &fakeRuntimeClient{alive: true}
	m1.runtimes[config.RuntimeCodex].client = client
	m1.setSessionRuntime("session-123", config.RuntimeCodex)
	m1.persistSessionSnapshot(
		"session-123",
		config.RuntimeCodex,
		[]acpprotocol.SessionConfigOption{{
			ID:           "mode",
			Name:         "Approval Preset",
			Type:         "select",
			Category:     &modeCategory,
			CurrentValue: "read-only",
		}},
	)

	if err := m1.SetMode(context.Background(), "session-123", "full-access"); err != nil {
		t.Fatalf("SetMode: %v", err)
	}

	m2 := New(cfg)
	restartedClient := &fakeRuntimeClient{
		alive: true,
		listSessions: []domain.SessionSummary{{
			SessionID: "session-123",
			CWD:       "/tmp/project",
			Title:     "Stored Session",
			Runtime:   string(config.RuntimeCodex),
			UpdatedAt: time.Now(),
		}},
	}
	m2.runtimes[config.RuntimeCodex].client = restartedClient

	sessions, err := m2.ResolveSessions(
		context.Background(),
		string(config.RuntimeCodex),
		[]string{"session-123"},
	)
	if err != nil {
		t.Fatalf("ResolveSessions: %v", err)
	}
	if len(sessions) != 1 || len(sessions[0].ConfigOptions) != 1 {
		t.Fatalf("sessions = %#v, want one stored session with config", sessions)
	}
	if sessions[0].ConfigOptions[0].CurrentValue != "full-access" {
		t.Fatalf(
			"current mode = %q, want full-access",
			sessions[0].ConfigOptions[0].CurrentValue,
		)
	}
}

func TestSetModeBootstrapsStoredModeSnapshotWhenMissing(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cfg := config.Default()
	m1 := New(cfg)
	client := &fakeRuntimeClient{alive: true}
	m1.runtimes[config.RuntimeCodex].client = client
	m1.setSessionRuntime("session-123", config.RuntimeCodex)

	if err := m1.SetMode(context.Background(), "session-123", "full-access"); err != nil {
		t.Fatalf("SetMode: %v", err)
	}

	m2 := New(cfg)
	restartedClient := &fakeRuntimeClient{
		alive: true,
		listSessions: []domain.SessionSummary{{
			SessionID: "session-123",
			CWD:       "/tmp/project",
			Title:     "Stored Session",
			Runtime:   string(config.RuntimeCodex),
			UpdatedAt: time.Now(),
		}},
	}
	m2.runtimes[config.RuntimeCodex].client = restartedClient

	sessions, err := m2.ResolveSessions(
		context.Background(),
		string(config.RuntimeCodex),
		[]string{"session-123"},
	)
	if err != nil {
		t.Fatalf("ResolveSessions: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("len(sessions) = %d, want 1", len(sessions))
	}
	if got := findConfigOptionValue(sessions[0].ConfigOptions, "mode"); got != "full-access" {
		t.Fatalf("mode = %q, want full-access", got)
	}
}

func TestNewSessionEnrichesModeConfigFromRuntimeCatalog(t *testing.T) {
	cfg := config.Default()
	modelCategory := "model"
	m := New(cfg)
	client := &fakeRuntimeClient{
		alive: true,
		newSessionResp: acpprotocol.NewSessionResponse{
			SessionID: "session-123",
			ConfigOptions: []acpprotocol.SessionConfigOption{{
				ID:           "model",
				Name:         "Model",
				Type:         "select",
				Category:     &modelCategory,
				CurrentValue: "gpt-5",
			}},
		},
	}
	m.runtimes[config.RuntimeCodex].client = client

	sessionID, runtimeID, resp, err := m.NewSession(
		context.Background(),
		string(config.RuntimeCodex),
		"/tmp/project",
		"full-access",
	)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	if sessionID != "session-123" {
		t.Fatalf("sessionID = %q, want session-123", sessionID)
	}
	if runtimeID != string(config.RuntimeCodex) {
		t.Fatalf("runtimeID = %q, want %q", runtimeID, config.RuntimeCodex)
	}
	if got := findConfigOptionValue(resp.ConfigOptions, "mode"); got != "full-access" {
		t.Fatalf("mode = %q, want full-access", got)
	}
	modeOption := findConfigOption(resp.ConfigOptions, "mode")
	if modeOption == nil {
		t.Fatal("expected mode config option in new-session response")
	}
	if got := len(modeOption.Options.Flatten()); got != 3 {
		t.Fatalf("len(mode options) = %d, want 3", got)
	}

	snapshot, ok := m.sessionSnapshot(config.RuntimeCodex, "session-123")
	if !ok {
		t.Fatal("expected persisted session snapshot")
	}
	if got := findConfigOptionValue(snapshot.ConfigOptions, "mode"); got != "full-access" {
		t.Fatalf("stored mode = %q, want full-access", got)
	}
	if modeOption := findConfigOption(snapshot.ConfigOptions, "mode"); modeOption == nil || len(modeOption.Options.Flatten()) != 3 {
		t.Fatalf("stored mode option = %#v, want runtime catalog choices", modeOption)
	}
}

func TestLoadSessionEnrichesModeConfigFromStoredSnapshotAndRuntimeCatalog(t *testing.T) {
	cfg := config.Default()
	modelCategory := "model"
	m := New(cfg)
	m.setSessionRuntime("session-123", config.RuntimeCodex)
	m.persistSessionSnapshot(
		"session-123",
		config.RuntimeCodex,
		[]acpprotocol.SessionConfigOption{{
			ID:           "mode",
			Name:         "Mode",
			Type:         "select",
			CurrentValue: "full-access",
		}},
	)
	client := &fakeRuntimeClient{
		alive: true,
		loadResp: acpprotocol.LoadSessionResponse{
			ConfigOptions: []acpprotocol.SessionConfigOption{{
				ID:           "model",
				Name:         "Model",
				Type:         "select",
				Category:     &modelCategory,
				CurrentValue: "gpt-5",
			}},
		},
	}
	m.runtimes[config.RuntimeCodex].client = client

	runtimeID, resp, err := m.LoadSession(
		context.Background(),
		string(config.RuntimeCodex),
		"session-123",
		"/tmp/project",
		"",
		"",
	)
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	if runtimeID != string(config.RuntimeCodex) {
		t.Fatalf("runtimeID = %q, want %q", runtimeID, config.RuntimeCodex)
	}
	if got := findConfigOptionValue(resp.ConfigOptions, "mode"); got != "full-access" {
		t.Fatalf("mode = %q, want full-access", got)
	}
	modeOption := findConfigOption(resp.ConfigOptions, "mode")
	if modeOption == nil {
		t.Fatal("expected mode config option in load-session response")
	}
	if got := len(modeOption.Options.Flatten()); got != 3 {
		t.Fatalf("len(mode options) = %d, want 3", got)
	}
}

func TestMergeConfigOptionsByIDPreservesExistingChoicesWhenIncomingStubOnlyChangesCurrentValue(t *testing.T) {
	modeCategory := "mode"
	current := []acpprotocol.SessionConfigOption{{
		ID:           "mode",
		Name:         "Approval Preset",
		Type:         "select",
		Category:     &modeCategory,
		CurrentValue: "read-only",
		Options: acpprotocol.SessionConfigSelectOptions{
			Ungrouped: []acpprotocol.SessionConfigSelectOption{
				{Value: "read-only", Name: "Read Only"},
				{Value: "auto", Name: "Default"},
				{Value: "full-access", Name: "Full Access"},
			},
		},
	}}
	incoming := []acpprotocol.SessionConfigOption{{
		ID:           "mode",
		Name:         "Mode",
		Type:         "select",
		Category:     &modeCategory,
		CurrentValue: "full-access",
	}}

	merged := mergeConfigOptionsByID(current, incoming)
	if got := findConfigOptionValue(merged, "mode"); got != "full-access" {
		t.Fatalf("mode = %q, want full-access", got)
	}
	modeOption := findConfigOption(merged, "mode")
	if modeOption == nil {
		t.Fatal("expected merged mode option")
	}
	if got := len(modeOption.Options.Flatten()); got != 3 {
		t.Fatalf("len(mode options) = %d, want 3", got)
	}
}

func TestResolveSessionsMergesRuntimeConfigOptionsWithStoredSnapshot(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cfg := config.Default()
	modeCategory := "mode"
	modelCategory := "model"
	m1 := New(cfg)
	m1.persistSessionSnapshot(
		"session-123",
		config.RuntimeCodex,
		[]acpprotocol.SessionConfigOption{
			{
				ID:           "mode",
				Name:         "Approval Preset",
				Type:         "select",
				Category:     &modeCategory,
				CurrentValue: "full-access",
			},
			{
				ID:           "reasoning_effort",
				Name:         "Reasoning Effort",
				Type:         "select",
				CurrentValue: "high",
			},
		},
	)

	m2 := New(cfg)
	client := &fakeRuntimeClient{
		alive: true,
		listSessions: []domain.SessionSummary{{
			SessionID: "session-123",
			CWD:       "/tmp/project",
			Title:     "Stored Session",
			Runtime:   string(config.RuntimeCodex),
			UpdatedAt: time.Now(),
			ConfigOptions: []acpprotocol.SessionConfigOption{{
				ID:           "model",
				Name:         "Model",
				Type:         "select",
				Category:     &modelCategory,
				CurrentValue: "gpt-5",
			}},
		}},
	}
	m2.runtimes[config.RuntimeCodex].client = client

	sessions, err := m2.ResolveSessions(
		context.Background(),
		string(config.RuntimeCodex),
		[]string{"session-123"},
	)
	if err != nil {
		t.Fatalf("ResolveSessions: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("len(sessions) = %d, want 1", len(sessions))
	}
	if got := findConfigOptionValue(sessions[0].ConfigOptions, "mode"); got != "full-access" {
		t.Fatalf("mode = %q, want full-access", got)
	}
	if got := findConfigOptionValue(sessions[0].ConfigOptions, "model"); got != "gpt-5" {
		t.Fatalf("model = %q, want gpt-5", got)
	}
	if got := findConfigOptionValue(sessions[0].ConfigOptions, "reasoning_effort"); got != "high" {
		t.Fatalf("reasoning_effort = %q, want high", got)
	}

	m3 := New(cfg)
	snapshot, ok := m3.sessionSnapshot(config.RuntimeCodex, "session-123")
	if !ok {
		t.Fatal("expected stored snapshot after restart")
	}
	if got := findConfigOptionValue(snapshot.ConfigOptions, "mode"); got != "full-access" {
		t.Fatalf("stored mode = %q, want full-access", got)
	}
	if got := findConfigOptionValue(snapshot.ConfigOptions, "model"); got != "" {
		t.Fatalf("stored model = %q, want empty because resolve should be read-only", got)
	}
	if got := findConfigOptionValue(snapshot.ConfigOptions, "reasoning_effort"); got != "high" {
		t.Fatalf("stored reasoning_effort = %q, want high", got)
	}
}

func TestCaptureSessionSnapshotFromModeEventBootstrapsModeWhenMissing(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cfg := config.Default()
	m := New(cfg)
	m.setSessionRuntime("session-123", config.RuntimeCodex)

	m.captureSessionSnapshotFromEvent(appwire.Event{
		Type:      appwire.EventModeChanged,
		SessionID: "session-123",
		ModeChanged: &appwire.ModeChangedEvent{
			App: appwire.ModeChangedEventApp{CurrentModeID: "read-only"},
		},
	})

	snapshot, ok := m.sessionSnapshot(config.RuntimeCodex, "session-123")
	if !ok {
		t.Fatal("expected stored snapshot")
	}
	if got := findConfigOptionValue(snapshot.ConfigOptions, "mode"); got != "read-only" {
		t.Fatalf("mode = %q, want read-only", got)
	}
}

func TestResolveSessionsScopesStoredSnapshotsByRuntime(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cfg := config.Default()
	modeCategory := "mode"
	m1 := New(cfg)
	m1.persistSessionSnapshot(
		"session-123",
		config.RuntimeCodex,
		[]acpprotocol.SessionConfigOption{{
			ID:           "mode",
			Name:         "Approval Preset",
			Type:         "select",
			Category:     &modeCategory,
			CurrentValue: "full-access",
		}},
	)

	m2 := New(cfg)
	client := &fakeRuntimeClient{
		alive: true,
		listSessions: []domain.SessionSummary{{
			SessionID: "session-123",
			CWD:       "/tmp/project",
			Title:     "Other Runtime Session",
			Runtime:   string(config.RuntimeClaudeCode),
			UpdatedAt: time.Now(),
		}},
	}
	m2.runtimes[config.RuntimeClaudeCode].client = client

	sessions, err := m2.ResolveSessions(
		context.Background(),
		string(config.RuntimeClaudeCode),
		[]string{"session-123"},
	)
	if err != nil {
		t.Fatalf("ResolveSessions: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("len(sessions) = %d, want 1", len(sessions))
	}
	if len(sessions[0].ConfigOptions) != 0 {
		t.Fatalf("configOptions = %#v, want empty for different runtime", sessions[0].ConfigOptions)
	}
}

func findConfigOptionValue(
	options []acpprotocol.SessionConfigOption,
	id string,
) string {
	for _, option := range options {
		if option.ID == id {
			return option.CurrentValue
		}
	}
	return ""
}

func findConfigOption(
	options []acpprotocol.SessionConfigOption,
	id string,
) *acpprotocol.SessionConfigOption {
	for i := range options {
		if options[i].ID == id {
			return &options[i]
		}
	}
	return nil
}

package acp_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/LaLanMo/muxagent-cli/internal/acpprotocol"
	"github.com/LaLanMo/muxagent-cli/internal/appwire"
	"github.com/LaLanMo/muxagent-cli/internal/domain"
	"github.com/LaLanMo/muxagent-cli/internal/runtime/acp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func buildMockAgent(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "mockagent")
	cmd := exec.Command("go", "build", "-o", bin, "./testdata/mockagent")
	cmd.Dir = filepath.Join(getModuleRoot(t), "internal", "runtime", "acp")
	cmd.Stderr = os.Stderr
	require.NoError(t, cmd.Run(), "failed to build mockagent")
	return bin
}

func getModuleRoot(t *testing.T) string {
	t.Helper()
	// Walk up from the test file location to find go.mod
	dir, err := os.Getwd()
	require.NoError(t, err)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find module root (go.mod)")
		}
		dir = parent
	}
}

func collectEvents(ch <-chan appwire.Event, timeout time.Duration) []appwire.Event {
	var events []appwire.Event
	deadline := time.After(timeout)
	idle := time.NewTimer(500 * time.Millisecond)
	defer idle.Stop()
	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				return events
			}
			events = append(events, ev)
			// Reset idle timer on each event
			if !idle.Stop() {
				select {
				case <-idle.C:
				default:
				}
			}
			idle.Reset(500 * time.Millisecond)
		case <-idle.C:
			if len(events) > 0 {
				return events
			}
			// No events yet, keep waiting
			idle.Reset(500 * time.Millisecond)
		case <-deadline:
			return events
		}
	}
}

func newTestClient(t *testing.T, bin string) *acp.Client {
	t.Helper()
	client := acp.NewClient(acp.Config{
		Command: bin,
	})
	return startTestClient(t, client)
}

func newTestClientWithEnv(t *testing.T, bin string, env map[string]string) *acp.Client {
	t.Helper()
	client := acp.NewClient(acp.Config{
		Command: bin,
		Env:     env,
	})
	return startTestClient(t, client)
}

func startTestClient(t *testing.T, client *acp.Client) *acp.Client {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	require.NoError(t, client.Start(ctx))
	t.Cleanup(func() { client.Stop() })
	return client
}

func findEvent(events []appwire.Event, eventType appwire.EventType) *appwire.Event {
	for i := range events {
		if events[i].Type == eventType {
			return &events[i]
		}
	}
	return nil
}

func findToolEvent(events []appwire.Event, eventType appwire.EventType, match func(*appwire.ToolEvent) bool) *appwire.Event {
	for i := range events {
		if events[i].Type != eventType || events[i].Tool == nil {
			continue
		}
		if match == nil || match(events[i].Tool) {
			return &events[i]
		}
	}
	return nil
}

func eventCurrentModeID(event *appwire.Event) string {
	if event == nil || event.ModeChanged == nil {
		return ""
	}
	return event.ModeChanged.App.CurrentModeID
}

func findCurrentValue(opts []acpprotocol.SessionConfigOption, category string) string {
	for _, opt := range opts {
		if opt.Category != nil && *opt.Category == category {
			return opt.CurrentValue
		}
	}
	return ""
}

func TestClient_InitializeAndNewSession(t *testing.T) {
	bin := buildMockAgent(t)
	client := newTestClient(t, bin)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := client.NewSession(ctx, "/tmp", "")
	require.NoError(t, err)
	assert.Equal(t, "test-session-001", resp.SessionID)
}

func TestClient_NewSessionFallsBackToRuntimeModeWhenSetModeFails(t *testing.T) {
	bin := buildMockAgent(t)
	client := newTestClientWithEnv(t, bin, map[string]string{
		"MOCKAGENT_FAIL_SET_MODE": "1",
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := client.NewSession(ctx, "/tmp", domain.ModeAcceptEdits)
	require.NoError(t, err)
	assert.Equal(t, "test-session-001", resp.SessionID)
	assert.Equal(t, "default", findCurrentValue(resp.ConfigOptions, "mode"))

	events := collectEvents(client.Events(), 2*time.Second)
	modeEvent := findEvent(events, appwire.EventModeChanged)
	require.NotNil(t, modeEvent)
	assert.Equal(t, "default", eventCurrentModeID(modeEvent))
}

func TestClient_NewSessionReturnsRequestedModeWhenSetModeSucceeds(t *testing.T) {
	bin := buildMockAgent(t)
	client := newTestClient(t, bin)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := client.NewSession(ctx, "/tmp", domain.ModeAcceptEdits)
	require.NoError(t, err)
	assert.Equal(t, "test-session-001", resp.SessionID)
	assert.Equal(t, domain.ModeAcceptEdits, findCurrentValue(resp.ConfigOptions, "mode"))

	events := collectEvents(client.Events(), 2*time.Second)
	modeEvent := findEvent(events, appwire.EventModeChanged)
	require.NotNil(t, modeEvent)
	assert.Equal(t, domain.ModeAcceptEdits, eventCurrentModeID(modeEvent))
}

func TestClient_PromptStreamsEvents(t *testing.T) {
	bin := buildMockAgent(t)
	client := newTestClient(t, bin)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	resp, err := client.NewSession(ctx, "/tmp", "")
	require.NoError(t, err)

	// Start collecting events before prompt
	done := make(chan []appwire.Event, 1)
	go func() {
		done <- collectEvents(client.Events(), 5*time.Second)
	}()

	stopReason, _, err := client.Prompt(ctx, resp.SessionID, []domain.ContentBlock{
		{Type: "text", Text: "hello"},
	})
	require.NoError(t, err)
	assert.Equal(t, "end_turn", stopReason)

	// Give events time to propagate, then collect
	time.Sleep(200 * time.Millisecond)
	events := <-done

	// Verify we got the expected event types
	typeMap := make(map[appwire.EventType]int)
	for _, ev := range events {
		typeMap[ev.Type]++
	}

	assert.GreaterOrEqual(t, typeMap[appwire.EventMessageDelta], 2, "expected at least 2 message.delta events")
	assert.GreaterOrEqual(t, typeMap[appwire.EventToolStarted], 1, "expected at least 1 tool.started event")
	assert.GreaterOrEqual(t, typeMap[appwire.EventToolUpdated], 1, "expected at least 1 tool.updated event")
	assert.GreaterOrEqual(t, typeMap[appwire.EventToolCompleted], 1, "expected at least 1 tool.completed event")
	assert.GreaterOrEqual(t, typeMap[appwire.EventReasoning], 1, "expected at least 1 reasoning event")

	// Verify tool event details
	for _, ev := range events {
		if ev.Type == appwire.EventMessageDelta {
			require.NotNil(t, ev.MessagePart)
			require.NotNil(t, ev.MessagePart.ACP)
			assert.NotEmpty(t, ev.MessagePart.App.MessageID)
			assert.NotEmpty(t, ev.MessagePart.App.PartID)
			assert.Equal(t, appwire.MessageRoleAgent, ev.MessagePart.App.Role)
		}
		if ev.Type == appwire.EventToolStarted {
			require.NotNil(t, ev.Tool)
			require.NotNil(t, ev.Tool.ACP)
			assert.Equal(t, "call-001", ev.Tool.App.CallID)
			assert.Equal(t, "Bash", ev.Tool.App.Name)
			assert.Equal(t, "call-001", ev.Tool.ACP.ToolCallID)
			assert.NotEmpty(t, ev.Tool.App.MessageID)
		}
		if ev.Type == appwire.EventToolCompleted {
			require.NotNil(t, ev.Tool)
			require.NotNil(t, ev.Tool.ACP)
			assert.Equal(t, "file1.go\nfile2.go", ev.Tool.App.Output)
			assert.NotEmpty(t, ev.Tool.App.MessageID)
		}
		if ev.Type == appwire.EventReasoning {
			require.NotNil(t, ev.MessagePart)
			require.NotNil(t, ev.MessagePart.ACP)
			assert.NotEmpty(t, ev.MessagePart.App.MessageID)
			assert.NotEmpty(t, ev.MessagePart.App.PartID)
			assert.Equal(t, "reasoning", ev.MessagePart.App.PartType)
			assert.Contains(t, ev.MessagePart.App.Delta, "thinking")
		}
	}
}

func TestClient_PromptStreamsLocationsOnlyToolUpdate(t *testing.T) {
	bin := buildMockAgent(t)
	client := newTestClientWithEnv(t, bin, map[string]string{
		"MOCKAGENT_LOCATIONS_ONLY_TOOL_UPDATE": "1",
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	resp, err := client.NewSession(ctx, "/tmp", "")
	require.NoError(t, err)

	done := make(chan []appwire.Event, 1)
	go func() {
		done <- collectEvents(client.Events(), 5*time.Second)
	}()

	_, _, err = client.Prompt(ctx, resp.SessionID, []domain.ContentBlock{
		{Type: "text", Text: "show location update"},
	})
	require.NoError(t, err)

	time.Sleep(200 * time.Millisecond)
	events := <-done
	locationUpdate := findToolEvent(events, appwire.EventToolUpdated, func(tool *appwire.ToolEvent) bool {
		return len(tool.App.Locations) == 1 && tool.App.Locations[0].Path == "/tmp/output.txt"
	})
	require.NotNil(t, locationUpdate)
	assert.Equal(t, 7, *locationUpdate.Tool.App.Locations[0].Line)
}

func TestClient_PermissionFlow(t *testing.T) {
	bin := buildMockAgent(t)
	client := newTestClient(t, bin)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	resp, err := client.NewSession(ctx, "/tmp", "")
	require.NoError(t, err)

	// Monitor events for approval request
	approvalHandled := make(chan struct{})
	go func() {
		for ev := range client.Events() {
			if ev.Type == appwire.EventApprovalRequested {
				require.NotNil(t, ev.Approval)
				require.NotNil(t, ev.Approval.ACP)
				assert.Equal(t, "Bash", ev.Approval.App.Title)
				assert.Equal(t, "call-001", ev.Approval.App.ToolCallID)
				assert.Equal(t, "execute", ev.Approval.App.ToolKind)
				assert.Len(t, ev.Approval.ACP.Options, 2)

				// Reply with "once"
				err := client.ReplyPermission(ctx, resp.SessionID, ev.Approval.RequestID(), "once")
				assert.NoError(t, err)
				close(approvalHandled)
			}
		}
	}()

	// Send prompt that triggers permission flow
	stopReason, _, err := client.Prompt(ctx, resp.SessionID, []domain.ContentBlock{
		{Type: "text", Text: "test permission flow"},
	})
	require.NoError(t, err)
	assert.Equal(t, "end_turn", stopReason)

	// Verify permission was actually handled
	select {
	case <-approvalHandled:
		// OK
	case <-time.After(5 * time.Second):
		t.Fatal("permission request was never received")
	}
}

func TestClient_Cancel(t *testing.T) {
	bin := buildMockAgent(t)
	client := newTestClient(t, bin)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := client.NewSession(ctx, "/tmp", "")
	require.NoError(t, err)

	// Cancel should not hang or error (it's a notification)
	err = client.Cancel(ctx, resp.SessionID)
	assert.NoError(t, err)
}

func TestClient_CancelRespondsPendingPermission(t *testing.T) {
	bin := buildMockAgent(t)
	client := newTestClient(t, bin)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	resp, err := client.NewSession(ctx, "/tmp", "")
	require.NoError(t, err)

	approvalSeen := make(chan struct{}, 1)
	promptDone := make(chan error, 1)

	go func() {
		_, _, err := client.Prompt(ctx, resp.SessionID, []domain.ContentBlock{
			{Type: "text", Text: "trigger permission"},
		})
		promptDone <- err
	}()

	go func() {
		for ev := range client.Events() {
			if ev.Type == appwire.EventApprovalRequested {
				approvalSeen <- struct{}{}
				return
			}
		}
	}()

	select {
	case <-approvalSeen:
	case <-time.After(5 * time.Second):
		t.Fatal("approval request was never received")
	}

	err = client.Cancel(ctx, resp.SessionID)
	require.NoError(t, err)

	select {
	case err := <-promptDone:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("prompt did not resume after cancel response")
	}
}

func TestClient_LoadSessionReplaysHistory(t *testing.T) {
	bin := buildMockAgent(t)
	client := newTestClient(t, bin)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Start collecting events before load
	done := make(chan []appwire.Event, 1)
	go func() {
		done <- collectEvents(client.Events(), 3*time.Second)
	}()

	_, err := client.LoadSession(ctx, "test-session-001", "/tmp", "", "")
	require.NoError(t, err)

	// Give events time to propagate
	time.Sleep(200 * time.Millisecond)
	events := <-done

	// Verify replayed events
	typeMap := make(map[appwire.EventType]int)
	for _, ev := range events {
		typeMap[ev.Type]++
	}

	assert.GreaterOrEqual(t, typeMap[appwire.EventMessageDelta], 2, "expected replayed message chunks")
	assert.GreaterOrEqual(t, typeMap[appwire.EventToolStarted], 1, "expected replayed tool.started")
	assert.GreaterOrEqual(t, typeMap[appwire.EventToolCompleted], 1, "expected replayed tool.completed")
	assert.Equal(t, 1, typeMap[appwire.EventHistoryComplete], "expected exactly one history.complete")

	// Verify content
	var messageParts []string
	var roleMap = map[appwire.MessageRole]int{}
	for _, ev := range events {
		if ev.Type == appwire.EventMessageDelta && ev.MessagePart != nil {
			messageParts = append(messageParts, ev.MessagePart.App.Delta)
			assert.NotEmpty(t, ev.MessagePart.App.MessageID)
			assert.NotEmpty(t, ev.MessagePart.App.PartID)
			roleMap[ev.MessagePart.App.Role]++
		}
		if (ev.Type == appwire.EventToolStarted || ev.Type == appwire.EventToolCompleted) && ev.Tool != nil {
			assert.NotEmpty(t, ev.Tool.App.MessageID)
		}
	}
	assert.GreaterOrEqual(t, roleMap[appwire.MessageRoleUser], 1, "expected replayed user message chunk")
	assert.GreaterOrEqual(t, roleMap[appwire.MessageRoleAgent], 1, "expected replayed agent message chunk")
	assert.Contains(t, messageParts, "Hi ")
	assert.Contains(t, messageParts, "there")
	assert.Contains(t, messageParts, "History: ")
	assert.Contains(t, messageParts, "replayed message")
}

func TestClient_LoadSessionFailsWithoutHistoryCompleteWhenAgentExitsDuringReplay(t *testing.T) {
	bin := buildMockAgent(t)
	client := newTestClientWithEnv(t, bin, map[string]string{
		"MOCKAGENT_EXIT_DURING_LOAD": "1",
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	done := make(chan []appwire.Event, 1)
	go func() {
		done <- collectEvents(client.Events(), 2*time.Second)
	}()

	_, err := client.LoadSession(ctx, "test-session-001", "/tmp", "", "")
	require.Error(t, err)
	require.ErrorContains(t, err, "session/load")

	events := <-done
	for _, ev := range events {
		if ev.Type == appwire.EventHistoryComplete {
			t.Fatalf("unexpected history.complete after failed load: %#v", ev)
		}
	}
}

func TestClient_LoadSessionRejectsConcurrentReplay(t *testing.T) {
	bin := buildMockAgent(t)
	client := newTestClientWithEnv(t, bin, map[string]string{
		"MOCKAGENT_LOAD_DELAY_MS": "250",
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	firstDone := make(chan error, 1)
	go func() {
		_, err := client.LoadSession(ctx, "test-session-001", "/tmp", "", "")
		firstDone <- err
	}()
	time.Sleep(50 * time.Millisecond)

	_, err := client.LoadSession(ctx, "test-session-002", "/tmp", "", "")
	require.Error(t, err)
	require.ErrorContains(t, err, "session/load already in progress")

	require.NoError(t, <-firstDone)
}

func TestClient_LoadSessionReplaysCompletedToolCallWithDiff(t *testing.T) {
	bin := buildMockAgent(t)
	client := newTestClientWithEnv(t, bin, map[string]string{
		"MOCKAGENT_LOAD_TOOL_CALL_DIFF": "1",
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	done := make(chan []appwire.Event, 1)
	go func() {
		done <- collectEvents(client.Events(), 3*time.Second)
	}()

	_, err := client.LoadSession(ctx, "test-session-001", "/tmp", "", "")
	require.NoError(t, err)

	time.Sleep(200 * time.Millisecond)
	events := <-done

	completed := findToolEvent(events, appwire.EventToolCompleted, func(tool *appwire.ToolEvent) bool {
		return tool.App.CallID == "hist-tool-edit-1"
	})
	require.NotNil(t, completed)
	require.Len(t, completed.Tool.App.Diffs, 1)
	assert.Equal(t, "/workspace/file.txt", completed.Tool.App.Diffs[0].Path)
	assert.Equal(t, "after\n", completed.Tool.App.Diffs[0].NewText)
	assert.Equal(t, appwire.ToolStatusCompleted, completed.Tool.App.Status)
	assert.NotEmpty(t, completed.Tool.App.MessageID)
}

func TestClient_LoadSessionFallsBackToRuntimeModeWhenSetModeFails(t *testing.T) {
	bin := buildMockAgent(t)
	client := newTestClientWithEnv(t, bin, map[string]string{
		"MOCKAGENT_FAIL_SET_MODE": "1",
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	done := make(chan []appwire.Event, 1)
	go func() {
		done <- collectEvents(client.Events(), 3*time.Second)
	}()

	resp, err := client.LoadSession(ctx, "test-session-001", "/tmp", domain.ModeAcceptEdits, "")
	require.NoError(t, err)
	assert.Equal(t, "default", findCurrentValue(resp.ConfigOptions, "mode"))

	time.Sleep(200 * time.Millisecond)
	events := <-done
	modeEvent := findEvent(events, appwire.EventModeChanged)
	require.NotNil(t, modeEvent)
	assert.Equal(t, "default", eventCurrentModeID(modeEvent))
}

func TestClient_LoadSessionReturnsRequestedModeWhenSetModeSucceeds(t *testing.T) {
	bin := buildMockAgent(t)
	client := newTestClient(t, bin)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	done := make(chan []appwire.Event, 1)
	go func() {
		done <- collectEvents(client.Events(), 3*time.Second)
	}()

	resp, err := client.LoadSession(ctx, "test-session-001", "/tmp", domain.ModeAcceptEdits, "")
	require.NoError(t, err)
	assert.Equal(t, domain.ModeAcceptEdits, findCurrentValue(resp.ConfigOptions, "mode"))

	time.Sleep(200 * time.Millisecond)
	events := <-done
	modeEvent := findEvent(events, appwire.EventModeChanged)
	require.NotNil(t, modeEvent)
	assert.Equal(t, domain.ModeAcceptEdits, eventCurrentModeID(modeEvent))
}

func TestClient_SetModeEmitsWrappedModeChangedEvent(t *testing.T) {
	bin := buildMockAgent(t)
	client := newTestClient(t, bin)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := client.SetMode(ctx, "test-session-001", domain.ModeAcceptEdits)
	require.NoError(t, err)

	events := collectEvents(client.Events(), 2*time.Second)
	modeEvent := findEvent(events, appwire.EventModeChanged)
	require.NotNil(t, modeEvent)
	assert.Equal(t, domain.ModeAcceptEdits, eventCurrentModeID(modeEvent))
}

func TestClient_ListSessionsIncludesCachedConfigOptions(t *testing.T) {
	bin := buildMockAgent(t)
	client := startTestClient(t, acp.NewClient(acp.Config{
		Command:   bin,
		RuntimeID: "claude-code",
	}))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := client.NewSession(ctx, "/tmp", "")
	require.NoError(t, err)
	require.NoError(t, client.SetMode(ctx, "test-session-001", domain.ModeAcceptEdits))
	require.NoError(t, client.SetConfigOption(ctx, "test-session-001", "model", "opus"))

	sessions, err := client.ListSessions(ctx, "")
	require.NoError(t, err)

	var resolved *domain.SessionSummary
	for i := range sessions {
		if sessions[i].SessionID == "test-session-001" {
			resolved = &sessions[i]
			break
		}
	}
	require.NotNil(t, resolved)
	assert.Equal(t, "claude-code", resolved.Runtime)
	assert.Equal(t, domain.ModeAcceptEdits, findCurrentValue(resolved.ConfigOptions, "mode"))
	assert.Equal(t, "opus", findCurrentValue(resolved.ConfigOptions, "model"))
}

func TestClient_LoadSessionReturnsParseErrorForInvalidACPResponse(t *testing.T) {
	bin := buildMockAgent(t)
	client := newTestClientWithEnv(t, bin, map[string]string{
		"MOCKAGENT_INVALID_LOAD_RESPONSE": "1",
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := client.LoadSession(ctx, "test-session-001", "/tmp", "", "")
	require.ErrorContains(t, err, "parse session/load result")
}

func TestClient_SetConfigOptionReturnsParseErrorForInvalidACPResponse(t *testing.T) {
	bin := buildMockAgent(t)
	client := newTestClientWithEnv(t, bin, map[string]string{
		"MOCKAGENT_INVALID_SET_CONFIG_RESPONSE": "1",
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := client.SetConfigOption(ctx, "test-session-001", "model", "opus")
	require.ErrorContains(t, err, "parse session/set_config_option result")
}

func TestClient_EnvRemoval_CLAUDECODE(t *testing.T) {
	// Set CLAUDECODE in the test process so the child would inherit it.
	t.Setenv("CLAUDECODE", "1")

	bin := buildMockAgent(t)
	client := acp.NewClient(acp.Config{
		Command: bin,
		Env:     map[string]string{"CLAUDECODE": ""},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	require.NoError(t, client.Start(ctx))
	t.Cleanup(func() { client.Stop() })

	resp, err := client.NewSession(ctx, "/tmp", "")
	require.NoError(t, err)
	assert.NotEmpty(t, resp.SessionID)
}

func TestClient_RequiresAbsoluteCWD(t *testing.T) {
	client := acp.NewClient(acp.Config{})

	_, err := client.NewSession(context.Background(), "", "")
	require.ErrorContains(t, err, "cwd must be an absolute path")

	_, err = client.NewSession(context.Background(), "relative/path", "")
	require.ErrorContains(t, err, "cwd must be an absolute path")

	_, err = client.LoadSession(context.Background(), "sid", "", "", "")
	require.ErrorContains(t, err, "cwd must be an absolute path")

	_, err = client.LoadSession(context.Background(), "sid", "relative/path", "", "")
	require.ErrorContains(t, err, "cwd must be an absolute path")
}

func TestClientMarksTransportDeadAfterChildExit(t *testing.T) {
	bin := buildMockAgent(t)
	client := newTestClientWithEnv(
		t,
		bin,
		map[string]string{"MOCKAGENT_EXIT_AFTER_INITIALIZE": "1"},
	)

	require.Eventually(t, func() bool {
		return !client.IsAlive()
	}, 5*time.Second, 50*time.Millisecond)

	_, err := client.ListSessions(context.Background(), "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "transport stopped")

	require.Eventually(t, func() bool {
		_, ok := <-client.Events()
		return !ok
	}, 5*time.Second, 50*time.Millisecond)
}

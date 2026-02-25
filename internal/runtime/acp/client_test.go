package acp_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

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

func collectEvents(ch <-chan domain.Event, timeout time.Duration) []domain.Event {
	var events []domain.Event
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
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	require.NoError(t, client.Start(ctx))
	t.Cleanup(func() { client.Stop() })
	return client
}

func TestClient_InitializeAndNewSession(t *testing.T) {
	bin := buildMockAgent(t)
	client := newTestClient(t, bin)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	sessionID, err := client.NewSession(ctx, "/tmp")
	require.NoError(t, err)
	assert.Equal(t, "test-session-001", sessionID)
}

func TestClient_PromptStreamsEvents(t *testing.T) {
	bin := buildMockAgent(t)
	client := newTestClient(t, bin)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	sessionID, err := client.NewSession(ctx, "/tmp")
	require.NoError(t, err)

	// Start collecting events before prompt
	done := make(chan []domain.Event, 1)
	go func() {
		done <- collectEvents(client.Events(), 5*time.Second)
	}()

	stopReason, err := client.Prompt(ctx, sessionID, []domain.ContentBlock{
		{Type: "text", Text: "hello"},
	})
	require.NoError(t, err)
	assert.Equal(t, "end_turn", stopReason)

	// Give events time to propagate, then collect
	time.Sleep(200 * time.Millisecond)
	events := <-done

	// Verify we got the expected event types
	typeMap := make(map[domain.EventType]int)
	for _, ev := range events {
		typeMap[ev.Type]++
	}

	assert.GreaterOrEqual(t, typeMap[domain.EventMessageDelta], 2, "expected at least 2 message.delta events")
	assert.GreaterOrEqual(t, typeMap[domain.EventToolStarted], 1, "expected at least 1 tool.started event")
	assert.GreaterOrEqual(t, typeMap[domain.EventToolUpdated], 1, "expected at least 1 tool.updated event")
	assert.GreaterOrEqual(t, typeMap[domain.EventToolCompleted], 1, "expected at least 1 tool.completed event")
	assert.GreaterOrEqual(t, typeMap[domain.EventReasoning], 1, "expected at least 1 reasoning event")

	// Verify tool event details
	for _, ev := range events {
		if ev.Type == domain.EventToolStarted {
			require.NotNil(t, ev.Tool)
			assert.Equal(t, "call-001", ev.Tool.CallID)
			assert.Equal(t, "Bash", ev.Tool.Name)
		}
		if ev.Type == domain.EventToolCompleted {
			require.NotNil(t, ev.Tool)
			assert.Equal(t, "file1.go\nfile2.go", ev.Tool.Output)
		}
		if ev.Type == domain.EventReasoning {
			require.NotNil(t, ev.MessagePart)
			assert.Equal(t, "reasoning", ev.MessagePart.PartType)
			assert.Contains(t, ev.MessagePart.Delta, "thinking")
		}
	}
}

func TestClient_PermissionFlow(t *testing.T) {
	bin := buildMockAgent(t)
	client := newTestClient(t, bin)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	sessionID, err := client.NewSession(ctx, "/tmp")
	require.NoError(t, err)

	// Monitor events for approval request
	approvalHandled := make(chan struct{})
	go func() {
		for ev := range client.Events() {
			if ev.Type == domain.EventApprovalRequested {
				require.NotNil(t, ev.Approval)
				assert.Equal(t, "Bash", ev.Approval.ToolName)
				assert.Len(t, ev.Approval.Options, 2)

				// Reply with "once"
				err := client.ReplyPermission(ctx, sessionID, ev.Approval.ID, "once")
				assert.NoError(t, err)
				close(approvalHandled)
			}
		}
	}()

	// Send prompt that triggers permission flow
	stopReason, err := client.Prompt(ctx, sessionID, []domain.ContentBlock{
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

	sessionID, err := client.NewSession(ctx, "/tmp")
	require.NoError(t, err)

	// Cancel should not hang or error (it's a notification)
	err = client.Cancel(ctx, sessionID)
	assert.NoError(t, err)
}

func TestClient_LoadSessionReplaysHistory(t *testing.T) {
	bin := buildMockAgent(t)
	client := newTestClient(t, bin)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Start collecting events before load
	done := make(chan []domain.Event, 1)
	go func() {
		done <- collectEvents(client.Events(), 3*time.Second)
	}()

	err := client.LoadSession(ctx, "test-session-001", "/tmp")
	require.NoError(t, err)

	// Give events time to propagate
	time.Sleep(200 * time.Millisecond)
	events := <-done

	// Verify replayed events
	typeMap := make(map[domain.EventType]int)
	for _, ev := range events {
		typeMap[ev.Type]++
	}

	assert.GreaterOrEqual(t, typeMap[domain.EventMessageDelta], 2, "expected replayed message chunks")
	assert.GreaterOrEqual(t, typeMap[domain.EventToolStarted], 1, "expected replayed tool.started")
	assert.GreaterOrEqual(t, typeMap[domain.EventToolCompleted], 1, "expected replayed tool.completed")

	// Verify content
	var messageParts []string
	for _, ev := range events {
		if ev.Type == domain.EventMessageDelta && ev.MessagePart != nil {
			messageParts = append(messageParts, ev.MessagePart.Delta)
		}
	}
	assert.Contains(t, messageParts, "History: ")
	assert.Contains(t, messageParts, "replayed message")
}

package relayws

import (
	"context"
	"crypto/ed25519"
	crand "crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LaLanMo/muxagent-cli/internal/acpprotocol"
	"github.com/LaLanMo/muxagent-cli/internal/appwire"
	"github.com/LaLanMo/muxagent-cli/internal/domain"
	runtimemanager "github.com/LaLanMo/muxagent-cli/internal/runtime/manager"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
)

type blockingRuntime struct {
	loadStarted chan struct{}
	unblock     chan struct{}
}

func (r *blockingRuntime) RuntimeList() []runtimemanager.RuntimeInfo {
	return []runtimemanager.RuntimeInfo{{
		ID:    "claude-code",
		Label: "Claude Code",
		Ready: true,
	}}
}

func (r *blockingRuntime) NewSession(ctx context.Context, runtimeID, cwd string, permissionMode string) (string, string, acpprotocol.NewSessionResponse, error) {
	return "sid", runtimeID, acpprotocol.NewSessionResponse{SessionID: "sid"}, nil
}

func (r *blockingRuntime) LoadSession(ctx context.Context, runtimeID, sessionID, cwd, permissionMode, model string) (string, acpprotocol.LoadSessionResponse, error) {
	select {
	case r.loadStarted <- struct{}{}:
	default:
	}

	select {
	case <-ctx.Done():
		return "", acpprotocol.LoadSessionResponse{}, ctx.Err()
	case <-r.unblock:
		return runtimeID, acpprotocol.LoadSessionResponse{}, nil
	}
}

func (r *blockingRuntime) ResolveSessions(ctx context.Context, runtimeID string, sessionIDs []string) ([]domain.SessionSummary, error) {
	return nil, nil
}

func (r *blockingRuntime) Prompt(ctx context.Context, sessionID string, content []domain.ContentBlock) (string, *domain.PromptUsage, error) {
	return "stop", nil, nil
}

func (r *blockingRuntime) Cancel(ctx context.Context, sessionID string) error {
	return nil
}

func (r *blockingRuntime) SetMode(ctx context.Context, sessionID, modeID string) error {
	return nil
}

func (r *blockingRuntime) SetConfigOption(ctx context.Context, sessionID, configID, value string) error {
	return nil
}

func (r *blockingRuntime) ReplyPermission(ctx context.Context, sessionID, requestID, optionID string) error {
	return nil
}

func (r *blockingRuntime) PendingApprovals() []domain.ApprovalRequest { return nil }

func TestRunProcessesRPCWhileAnotherRPCIsBlocked(t *testing.T) {
	upgrader := websocket.Upgrader{}
	serverConn := make(chan *websocket.Conn, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		require.NoError(t, err)
		serverConn <- conn
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	clientConn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	defer clientConn.Close()

	relayConn := <-serverConn
	defer relayConn.Close()

	var key [32]byte
	key[0] = 1
	session := newSession("machine-1", key, 1)
	rt := &blockingRuntime{
		loadStarted: make(chan struct{}, 1),
		unblock:     make(chan struct{}),
	}
	client := &Client{
		conn:       clientConn,
		connEpoch:  1,
		machineID:  "machine-1",
		runtime:    rt,
		session:    session,
		sessionCWD: map[string]string{},
	}

	runErr := make(chan error, 1)
	go func() {
		runErr <- client.Run(context.Background())
	}()

	require.NoError(t, relayConn.WriteJSON(encryptRPC(t, session, "machine-1", "msg-load", "session.load", map[string]any{
		"sessionId": "sid",
		"cwd":       "/tmp",
		"runtime":   "claude-code",
	})))

	select {
	case <-rt.loadStarted:
	case <-time.After(time.Second):
		t.Fatal("session.load did not start")
	}

	require.NoError(t, relayConn.WriteJSON(encryptRPC(t, session, "machine-1", "msg-echo", "echo", map[string]any{
		"message": "hello",
	})))

	msg := readEncryptedMessage(t, relayConn)
	require.Equal(t, MessageTypeResponse, msg.Type)
	require.Equal(t, "msg-echo", msg.MsgID)

	payload := decryptResponse(t, session, msg)
	require.Equal(t, "", payload["error"])
	result, ok := payload["result"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "hello", result["message"])

	close(rt.unblock)

	msg = readEncryptedMessage(t, relayConn)
	require.Equal(t, MessageTypeResponse, msg.Type)
	require.Equal(t, "msg-load", msg.MsgID)

	payload = decryptResponse(t, session, msg)
	require.Equal(t, "", payload["error"])
	result, ok = payload["result"].(map[string]any)
	require.True(t, ok)
	app, ok := result["app"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, true, app["ok"])

	require.NoError(t, clientConn.Close())
	select {
	case <-runErr:
	case <-time.After(2 * time.Second):
		t.Fatal("relay run loop did not stop")
	}
}

// listingRuntime is a runtime mock that returns a fixed session list.
type listingRuntime struct {
	blockingRuntime
	sessions []domain.SessionSummary
}

func (r *listingRuntime) ResolveSessions(ctx context.Context, runtimeID string, sessionIDs []string) ([]domain.SessionSummary, error) {
	return r.sessions, nil
}

type promptResult struct {
	stopReason string
	usage      *domain.PromptUsage
	err        error
}

type promptRuntime struct {
	listingRuntime
	started     chan struct{}
	release     chan promptResult
	lastContent []domain.ContentBlock
}

func (r *promptRuntime) Prompt(ctx context.Context, sessionID string, content []domain.ContentBlock) (string, *domain.PromptUsage, error) {
	r.lastContent = append([]domain.ContentBlock(nil), content...)
	select {
	case r.started <- struct{}{}:
	default:
	}
	select {
	case <-ctx.Done():
		return "", nil, ctx.Err()
	case result := <-r.release:
		return result.stopReason, result.usage, result.err
	}
}

type actionRuntime struct {
	listingRuntime
	cancelSessionID string
	modeSessionID   string
	modeID          string
	configSessionID string
	configID        string
	configValue     string
	replySessionID  string
	replyRequestID  string
	replyOptionID   string
}

func (r *actionRuntime) Cancel(ctx context.Context, sessionID string) error {
	r.cancelSessionID = sessionID
	return nil
}

func (r *actionRuntime) SetMode(ctx context.Context, sessionID, modeID string) error {
	r.modeSessionID = sessionID
	r.modeID = modeID
	return nil
}

func (r *actionRuntime) SetConfigOption(ctx context.Context, sessionID, configID, value string) error {
	r.configSessionID = sessionID
	r.configID = configID
	r.configValue = value
	return nil
}

func (r *actionRuntime) ReplyPermission(ctx context.Context, sessionID, requestID, optionID string) error {
	r.replySessionID = sessionID
	r.replyRequestID = requestID
	r.replyOptionID = optionID
	return nil
}

type approvalsRuntime struct {
	blockingRuntime
	approvals []domain.ApprovalRequest
}

func (r *approvalsRuntime) PendingApprovals() []domain.ApprovalRequest {
	return append([]domain.ApprovalRequest(nil), r.approvals...)
}

type routingRuntime struct {
	blockingRuntime
	runtimes    []runtimemanager.RuntimeInfo
	lastRuntime string
}

func (r *routingRuntime) RuntimeList() []runtimemanager.RuntimeInfo {
	if len(r.runtimes) > 0 {
		return r.runtimes
	}
	return r.blockingRuntime.RuntimeList()
}

func (r *routingRuntime) NewSession(ctx context.Context, runtimeID, cwd string, permissionMode string) (string, string, acpprotocol.NewSessionResponse, error) {
	r.lastRuntime = runtimeID
	return "sid", runtimeID, acpprotocol.NewSessionResponse{SessionID: "sid"}, nil
}

func stringPtr(value string) *string {
	return &value
}

func TestRunHandlesSessionResolveRPC(t *testing.T) {
	upgrader := websocket.Upgrader{}
	serverConn := make(chan *websocket.Conn, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		require.NoError(t, err)
		serverConn <- conn
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	clientConn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	defer clientConn.Close()

	relayConn := <-serverConn
	defer relayConn.Close()

	var key [32]byte
	key[0] = 1
	session := newSession("machine-1", key, 1)
	rt := &listingRuntime{
		blockingRuntime: blockingRuntime{
			loadStarted: make(chan struct{}, 1),
			unblock:     make(chan struct{}),
		},
		sessions: []domain.SessionSummary{
			{
				SessionID: "sid-1",
				CWD:       "/tmp/project",
				Title:     "Generated title",
				UpdatedAt: time.Date(2026, time.March, 2, 12, 0, 0, 0, time.UTC),
			},
		},
	}
	client := &Client{
		conn:       clientConn,
		connEpoch:  1,
		machineID:  "machine-1",
		runtime:    rt,
		session:    session,
		sessionCWD: map[string]string{},
	}

	runErr := make(chan error, 1)
	go func() {
		runErr <- client.Run(context.Background())
	}()

	require.NoError(t, relayConn.WriteJSON(encryptRPC(t, session, "machine-1", "msg-list", "session.resolve", map[string]any{
		"sessionIds": []string{"sid-1"},
	})))

	msg := readEncryptedMessage(t, relayConn)
	require.Equal(t, MessageTypeResponse, msg.Type)
	require.Equal(t, "msg-list", msg.MsgID)

	payload := decryptResponse(t, session, msg)
	require.Equal(t, "", payload["error"])

	result, ok := payload["result"].(map[string]any)
	require.True(t, ok)
	list, ok := result["sessions"].([]any)
	require.True(t, ok)
	require.Len(t, list, 1)

	entry, ok := list[0].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "sid-1", entry["sessionId"])
	require.Equal(t, "/tmp/project", entry["cwd"])
	require.Equal(t, "Generated title", entry["title"])
	require.Equal(t, string(domain.SessionStatusIdle), entry["status"])

	require.NoError(t, clientConn.Close())
	select {
	case <-runErr:
	case <-time.After(2 * time.Second):
		t.Fatal("relay run loop did not stop")
	}
}

func TestRpcLoadSessionPreservesTrackedStatus(t *testing.T) {
	rt := &listingRuntime{
		blockingRuntime: blockingRuntime{
			loadStarted: make(chan struct{}, 1),
			unblock:     make(chan struct{}),
		},
		sessions: []domain.SessionSummary{
			{
				SessionID: "sid",
				CWD:       "/tmp/project",
				Title:     "Title",
				UpdatedAt: time.Now(),
			},
		},
	}
	client := &Client{
		runtime:       rt,
		sessionCWD:    map[string]string{},
		sessionStatus: map[string]domain.SessionStatus{"sid": domain.SessionStatusRunning},
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, errStr := client.rpcLoadSession(context.Background(), map[string]any{
			"sessionId": "sid",
			"cwd":       "/tmp/project",
			"runtime":   "claude-code",
		})
		require.Empty(t, errStr)
	}()

	select {
	case <-rt.loadStarted:
	case <-time.After(time.Second):
		t.Fatal("session.load did not start")
	}

	close(rt.unblock)

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("session.load did not finish")
	}

	result, errStr := client.rpcResolveSessions(context.Background(), map[string]any{
		"sessionIds": []any{"sid"},
	})
	require.Empty(t, errStr)
	require.Equal(t, string(domain.SessionStatusRunning), resolvedStatusFromRPCResult(t, result))
}

func TestSendEventBuffersLocalEventsAndTracksStatus(t *testing.T) {
	client := &Client{
		machineID:     "machine-1",
		runtime:       &listingRuntime{sessions: []domain.SessionSummary{{SessionID: "sid", CWD: "/tmp/project", Title: "Title", UpdatedAt: time.Now()}}},
		eventBuf:      NewEventBuffer(8),
		sessionStatus: map[string]domain.SessionStatus{},
	}

	err := client.SendEvent(appwire.Event{
		Type:      appwire.EventApprovalRequested,
		SessionID: "sid",
		At:        time.Now(),
		Approval:  &appwire.ApprovalRequest{App: appwire.ApprovalApp{RequestID: "req-1"}},
	})
	require.ErrorIs(t, err, ErrRelayNotConnected)

	result, errStr := client.rpcResolveSessions(context.Background(), map[string]any{
		"sessionIds": []any{"sid"},
	})
	require.Empty(t, errStr)
	status := resolvedStatusFromRPCResult(t, result)
	require.Equal(t, string(domain.SessionStatusWaitingApproval), status)

	err = client.SendEvent(appwire.Event{
		Type:      appwire.EventRunFinished,
		SessionID: "sid",
		At:        time.Now(),
		RunFinished: &appwire.RunFinishedEvent{
			App: appwire.RunFinishedEventApp{StopReason: "end_turn"},
		},
	})
	require.ErrorIs(t, err, ErrRelayNotConnected)

	result, errStr = client.rpcResolveSessions(context.Background(), map[string]any{
		"sessionIds": []any{"sid"},
	})
	require.Empty(t, errStr)
	status = resolvedStatusFromRPCResult(t, result)
	require.Equal(t, string(domain.SessionStatusIdle), status)

	events, complete := client.eventBuf.Since(0)
	require.True(t, complete)
	require.Len(t, events, 2)
	require.EqualValues(t, 1, events[0].Seq)
	require.Equal(t, appwire.EventApprovalRequested, events[0].Type)
	require.EqualValues(t, 2, events[1].Seq)
	require.Equal(t, appwire.EventRunFinished, events[1].Type)
}

func TestRpcPromptUpdatesResolvedStatus(t *testing.T) {
	rt := &promptRuntime{
		listingRuntime: listingRuntime{
			sessions: []domain.SessionSummary{
				{
					SessionID: "sid",
					CWD:       "/tmp/project",
					Title:     "Title",
					UpdatedAt: time.Now(),
				},
			},
		},
		started: make(chan struct{}, 1),
		release: make(chan promptResult, 1),
	}
	client := &Client{
		machineID:     "machine-1",
		runtime:       rt,
		eventBuf:      NewEventBuffer(8),
		sessionCWD:    map[string]string{"sid": "/tmp/project"},
		sessionStatus: map[string]domain.SessionStatus{},
	}

	result, errStr := client.rpcPrompt(context.Background(), map[string]any{
		"sessionId": "sid",
		"text":      "hello",
	})
	require.Empty(t, errStr)
	require.Equal(t, appwire.AcceptedResult{Accepted: true}, result)

	select {
	case <-rt.started:
	case <-time.After(time.Second):
		t.Fatal("prompt did not start")
	}

	resolveResult, errStr := client.rpcResolveSessions(context.Background(), map[string]any{
		"sessionIds": []any{"sid"},
	})
	require.Empty(t, errStr)
	require.Equal(t, string(domain.SessionStatusRunning), resolvedStatusFromRPCResult(t, resolveResult))

	rt.release <- promptResult{stopReason: "end_turn"}

	require.Eventually(t, func() bool {
		result, errStr := client.rpcResolveSessions(context.Background(), map[string]any{
			"sessionIds": []any{"sid"},
		})
		if errStr != "" {
			return false
		}
		return resolvedStatusFromRPCResult(t, result) == string(domain.SessionStatusIdle)
	}, time.Second, 10*time.Millisecond)

	events, complete := client.eventBuf.Since(0)
	require.True(t, complete)
	require.Len(t, events, 1)
	require.Equal(t, appwire.EventRunFinished, events[0].Type)
	require.NotNil(t, events[0].RunFinished)
	require.Equal(t, "end_turn", events[0].RunFinished.App.StopReason)
}

func TestRpcPromptParsesTypedContentBlocks(t *testing.T) {
	rt := &promptRuntime{
		listingRuntime: listingRuntime{
			sessions: []domain.SessionSummary{
				{
					SessionID: "sid",
					CWD:       "/tmp/project",
					Title:     "Title",
					UpdatedAt: time.Now(),
				},
			},
		},
		started: make(chan struct{}, 1),
		release: make(chan promptResult, 1),
	}
	client := &Client{
		machineID:     "machine-1",
		runtime:       rt,
		eventBuf:      NewEventBuffer(8),
		sessionCWD:    map[string]string{"sid": "/tmp/project"},
		sessionStatus: map[string]domain.SessionStatus{},
	}

	result, errStr := client.rpcPrompt(context.Background(), map[string]any{
		"sessionId": "sid",
		"content": []map[string]any{
			{
				"type":     "image",
				"mimeType": "image/png",
				"data":     "ZmFrZQ==",
			},
			{
				"type": "text",
				"text": "hello",
			},
		},
	})
	require.Empty(t, errStr)
	require.Equal(t, appwire.AcceptedResult{Accepted: true}, result)

	select {
	case <-rt.started:
	case <-time.After(time.Second):
		t.Fatal("prompt did not start")
	}

	require.Len(t, rt.lastContent, 2)
	require.Equal(t, "image", rt.lastContent[0].Type)
	require.Equal(t, "image/png", rt.lastContent[0].MimeType)
	require.Equal(t, "ZmFrZQ==", rt.lastContent[0].Data)
	require.Equal(t, "text", rt.lastContent[1].Type)
	require.Equal(t, "hello", rt.lastContent[1].Text)

	rt.release <- promptResult{stopReason: "end_turn"}
}

func TestRpcActionHandlersDecodeTypedParams(t *testing.T) {
	rt := &actionRuntime{}
	client := &Client{
		machineID: "machine-1",
		runtime:   rt,
	}

	result, errStr := client.rpcCancel(context.Background(), map[string]any{
		"sessionId": "sid-cancel",
	})
	require.Empty(t, errStr)
	require.Equal(t, appwire.OKResult{OK: true}, result)
	require.Equal(t, "sid-cancel", rt.cancelSessionID)

	result, errStr = client.rpcSetMode(context.Background(), map[string]any{
		"sessionId":      "sid-mode",
		"permissionMode": "read-only",
	})
	require.Empty(t, errStr)
	require.Equal(t, appwire.OKResult{OK: true}, result)
	require.Equal(t, "sid-mode", rt.modeSessionID)
	require.Equal(t, "read-only", rt.modeID)

	result, errStr = client.rpcSetConfigOption(context.Background(), map[string]any{
		"sessionId": "sid-config",
		"configId":  "model",
		"value":     "gpt-5.4",
	})
	require.Empty(t, errStr)
	require.Equal(t, appwire.OKResult{OK: true}, result)
	require.Equal(t, "sid-config", rt.configSessionID)
	require.Equal(t, "model", rt.configID)
	require.Equal(t, "gpt-5.4", rt.configValue)

	result, errStr = client.rpcReplyPermission(context.Background(), map[string]any{
		"sessionId": "sid-approval",
		"requestId": "req-1",
		"optionId":  "allow",
	})
	require.Empty(t, errStr)
	require.Equal(t, appwire.OKResult{OK: true}, result)
	require.Equal(t, "sid-approval", rt.replySessionID)
	require.Equal(t, "req-1", rt.replyRequestID)
	require.Equal(t, "allow", rt.replyOptionID)
}

func TestRpcPendingApprovalsMapsDomainApprovalIntoAppwire(t *testing.T) {
	rt := &approvalsRuntime{
		approvals: []domain.ApprovalRequest{{
			App: domain.ApprovalApp{
				RequestID: "req-1",
				Runtime:   "codex",
				ToolKind:  "execute",
				Title:     "Run touch /workspace/hello.txt",
				Command: &domain.ApprovalCommand{
					Argv:    []string{"touch", "/workspace/hello.txt"},
					Display: "touch hello.txt",
				},
				CWD: "/workspace",
			},
		}},
	}
	client := &Client{runtime: rt}

	result, errStr := client.rpcPendingApprovals(context.Background(), map[string]any{})
	require.Empty(t, errStr)

	wire, ok := result.(appwire.PendingApprovalsResult)
	require.True(t, ok)
	require.Len(t, wire.Approvals, 1)
	require.Equal(t, "req-1", wire.Approvals[0].App.RequestID)
	require.Equal(t, "codex", wire.Approvals[0].App.Runtime)
	require.Equal(t, "execute", wire.Approvals[0].App.ToolKind)
	require.NotNil(t, wire.Approvals[0].App.Command)
	require.Equal(t, "touch hello.txt", wire.Approvals[0].App.Command.Display)
	require.Equal(
		t,
		[]string{"touch", "/workspace/hello.txt"},
		wire.Approvals[0].App.Command.Argv,
	)
	require.Equal(t, "/workspace", wire.Approvals[0].App.CWD)
}

func TestRpcActionHandlersRejectMalformedTypedParams(t *testing.T) {
	client := &Client{runtime: &actionRuntime{}}

	tests := []struct {
		name   string
		call   func() (any, string)
		errMsg string
	}{
		{
			name: "cancel rejects invalid sessionId type",
			call: func() (any, string) {
				return client.rpcCancel(context.Background(), map[string]any{
					"sessionId": map[string]any{"bad": true},
				})
			},
			errMsg: "invalid cancel params:",
		},
		{
			name: "set mode rejects invalid permissionMode type",
			call: func() (any, string) {
				return client.rpcSetMode(context.Background(), map[string]any{
					"sessionId":      "sid",
					"permissionMode": []any{"bad"},
				})
			},
			errMsg: "invalid setMode params:",
		},
		{
			name: "set config rejects invalid value type",
			call: func() (any, string) {
				return client.rpcSetConfigOption(context.Background(), map[string]any{
					"sessionId": "sid",
					"configId":  "model",
					"value":     map[string]any{"bad": true},
				})
			},
			errMsg: "invalid setConfigOption params:",
		},
		{
			name: "reply permission rejects invalid optionId type",
			call: func() (any, string) {
				return client.rpcReplyPermission(context.Background(), map[string]any{
					"sessionId": "sid",
					"requestId": "req-1",
					"optionId":  123,
				})
			},
			errMsg: "invalid replyPermission params:",
		},
		{
			name: "resync rejects invalid lastSeq type",
			call: func() (any, string) {
				client.eventBuf = NewEventBuffer(8)
				return client.rpcResyncEvents(context.Background(), map[string]any{
					"lastSeq": "bad",
				})
			},
			errMsg: "invalid resync params:",
		},
		{
			name: "fs list rejects invalid path type",
			call: func() (any, string) {
				client.sessionCWD = map[string]string{"sid": t.TempDir()}
				return client.rpcFsList(context.Background(), map[string]any{
					"sessionId": "sid",
					"path":      []any{"bad"},
				})
			},
			errMsg: "invalid fs.list params:",
		},
		{
			name: "fs search rejects invalid query type",
			call: func() (any, string) {
				client.sessionCWD = map[string]string{"sid": t.TempDir()}
				return client.rpcFsSearch(context.Background(), map[string]any{
					"sessionId": "sid",
					"query":     []any{"bad"},
				})
			},
			errMsg: "invalid fs.search params:",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, errStr := tt.call()
			require.Contains(t, errStr, tt.errMsg)
		})
	}
}

func TestSendEventUsesRunFailedEnvelope(t *testing.T) {
	clientConn, relayConn, cleanup := newWSPair(t)
	defer cleanup()

	var key [32]byte
	key[0] = 1
	session := newSession("machine-1", key, 1)
	client := &Client{
		conn:      clientConn,
		connEpoch: 1,
		machineID: "machine-1",
		session:   session,
	}

	err := client.SendEvent(appwire.Event{
		Type:      appwire.EventRunFailed,
		SessionID: "sid",
		At:        time.Now(),
		RunFailed: &appwire.RunFailedEvent{
			App: appwire.RunFailedEventApp{
				Error: appwire.SessionError{
					Code:    "prompt_error",
					Message: "runtime failed",
				},
			},
		},
	})
	require.NoError(t, err)

	msg := readEncryptedMessage(t, relayConn)
	require.Equal(t, MessageTypeEvent, msg.Type)

	payload := decryptResponse(t, session, msg)
	require.Equal(t, string(appwire.EventRunFailed), payload["type"])
	require.Equal(t, "sid", payload["sessionId"])
	_, hasTopLevelError := payload["error"]
	require.False(t, hasTopLevelError)

	runFailed, ok := payload["runFailed"].(map[string]any)
	require.True(t, ok)
	app, ok := runFailed["app"].(map[string]any)
	require.True(t, ok)
	errorPayload, ok := app["error"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "prompt_error", errorPayload["code"])
	require.Equal(t, "runtime failed", errorPayload["message"])
}

func TestSendEventUsesApprovalEnvelope(t *testing.T) {
	clientConn, relayConn, cleanup := newWSPair(t)
	defer cleanup()

	var key [32]byte
	key[0] = 1
	session := newSession("machine-1", key, 1)
	client := &Client{
		conn:      clientConn,
		connEpoch: 1,
		machineID: "machine-1",
		session:   session,
	}

	err := client.SendEvent(appwire.Event{
		Type:      appwire.EventApprovalRequested,
		SessionID: "sid",
		At:        time.Now(),
		Approval: &appwire.ApprovalRequest{
			App: appwire.ApprovalApp{
				RequestID: "req-1",
				Runtime:   "codex",
				ToolKind:  "execute",
				Title:     "Run touch /workspace/hello.txt",
				Command: &appwire.ApprovalCommand{
					Argv:    []string{"touch", "hello.txt"},
					Display: "touch hello.txt",
				},
				CWD: "/workspace",
			},
		},
	})
	require.NoError(t, err)

	msg := readEncryptedMessage(t, relayConn)
	require.Equal(t, MessageTypeEvent, msg.Type)

	payload := decryptResponse(t, session, msg)
	require.Equal(t, string(appwire.EventApprovalRequested), payload["type"])
	require.Equal(t, "sid", payload["sessionId"])

	approval, ok := payload["approval"].(map[string]any)
	require.True(t, ok)
	app, ok := approval["app"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "req-1", app["requestId"])
	require.Equal(t, "codex", app["runtime"])
	require.Equal(t, "execute", app["toolKind"])
	require.Equal(t, "Run touch /workspace/hello.txt", app["title"])
	require.Equal(t, "/workspace", app["cwd"])

	command, ok := app["command"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "touch hello.txt", command["display"])
	require.Equal(t, []any{"touch", "hello.txt"}, command["argv"])
}

func TestSendEventUsesToolEnvelope(t *testing.T) {
	clientConn, relayConn, cleanup := newWSPair(t)
	defer cleanup()

	var key [32]byte
	key[0] = 1
	line := 3
	session := newSession("machine-1", key, 1)
	client := &Client{
		conn:      clientConn,
		connEpoch: 1,
		machineID: "machine-1",
		session:   session,
	}

	err := client.SendEvent(appwire.Event{
		Type:      appwire.EventToolCompleted,
		SessionID: "sid",
		At:        time.Now(),
		Tool: &appwire.ToolEvent{
			App: appwire.ToolEventApp{
				PartID:    "part-1",
				MessageID: "msg-1",
				CallID:    "call-1",
				Name:      "Bash",
				Kind:      "execute",
				Title:     "Run touch hello.txt",
				Status:    appwire.ToolStatusCompleted,
				Input: &appwire.ToolInput{
					Command: &appwire.ToolCommand{
						Argv:    []string{"touch", "hello.txt"},
						Display: "touch hello.txt",
					},
					FilePath:     "hello.txt",
					RawInputJSON: "{\"command\":[\"touch\",\"hello.txt\"]}",
				},
				Output: "done",
				ClaudeCode: &appwire.ClaudeCodeTool{
					ParentToolUseID: "parent-1",
					ToolName:        "bash",
				},
				Locations: []appwire.ToolLocation{{
					Path: "/workspace/hello.txt",
					Line: &line,
				}},
			},
		},
	})
	require.NoError(t, err)

	msg := readEncryptedMessage(t, relayConn)
	require.Equal(t, MessageTypeEvent, msg.Type)

	payload := decryptResponse(t, session, msg)
	require.Equal(t, string(appwire.EventToolCompleted), payload["type"])
	require.Equal(t, "sid", payload["sessionId"])

	tool, ok := payload["tool"].(map[string]any)
	require.True(t, ok)
	app, ok := tool["app"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "call-1", app["callId"])
	require.Equal(t, "execute", app["kind"])
	require.Equal(t, "completed", app["status"])
	require.Equal(t, "done", app["output"])

	input, ok := app["input"].(map[string]any)
	require.True(t, ok)
	command, ok := input["command"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "touch hello.txt", command["display"])
	require.Equal(t, []any{"touch", "hello.txt"}, command["argv"])
	require.Equal(t, "hello.txt", input["filePath"])
	require.Equal(t, "{\"command\":[\"touch\",\"hello.txt\"]}", input["rawInputJson"])

	claudeCode, ok := app["claudeCode"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "parent-1", claudeCode["parentToolUseId"])
	require.Equal(t, "bash", claudeCode["toolName"])

	locations, ok := app["locations"].([]any)
	require.True(t, ok)
	require.Len(t, locations, 1)
	location, ok := locations[0].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "/workspace/hello.txt", location["path"])
	require.Equal(t, float64(3), location["line"])
}

func TestSendEventUsesModeChangedEnvelope(t *testing.T) {
	clientConn, relayConn, cleanup := newWSPair(t)
	defer cleanup()

	var key [32]byte
	key[0] = 1
	session := newSession("machine-1", key, 1)
	client := &Client{
		conn:      clientConn,
		connEpoch: 1,
		machineID: "machine-1",
		session:   session,
	}

	err := client.SendEvent(appwire.Event{
		Type:      appwire.EventModeChanged,
		SessionID: "sid",
		At:        time.Now(),
		ModeChanged: &appwire.ModeChangedEvent{
			App: appwire.ModeChangedEventApp{CurrentModeID: "read-only"},
		},
	})
	require.NoError(t, err)

	msg := readEncryptedMessage(t, relayConn)
	require.Equal(t, MessageTypeEvent, msg.Type)

	payload := decryptResponse(t, session, msg)
	require.Equal(t, string(appwire.EventModeChanged), payload["type"])
	require.Equal(t, "sid", payload["sessionId"])
	_, hasData := payload["data"]
	require.False(t, hasData)

	modeChanged, ok := payload["modeChanged"].(map[string]any)
	require.True(t, ok)
	app, ok := modeChanged["app"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "read-only", app["currentModeId"])
}

func TestSendEventUsesConfigChangedEnvelope(t *testing.T) {
	clientConn, relayConn, cleanup := newWSPair(t)
	defer cleanup()

	var key [32]byte
	key[0] = 1
	session := newSession("machine-1", key, 1)
	client := &Client{
		conn:      clientConn,
		connEpoch: 1,
		machineID: "machine-1",
		session:   session,
	}

	err := client.SendEvent(appwire.Event{
		Type:      appwire.EventModelChanged,
		SessionID: "sid",
		At:        time.Now(),
		ConfigChanged: &appwire.ConfigChangedEvent{
			App: appwire.ConfigChangedEventApp{
				ConfigID:     "model",
				Category:     "model",
				CurrentValue: "gpt-5.4",
				Values: []appwire.SessionConfigValue{{
					Value: "gpt-5.4",
					Name:  "gpt-5.4",
				}},
			},
		},
	})
	require.NoError(t, err)

	msg := readEncryptedMessage(t, relayConn)
	require.Equal(t, MessageTypeEvent, msg.Type)

	payload := decryptResponse(t, session, msg)
	require.Equal(t, string(appwire.EventModelChanged), payload["type"])
	require.Equal(t, "sid", payload["sessionId"])
	_, hasData := payload["data"]
	require.False(t, hasData)

	configChanged, ok := payload["configChanged"].(map[string]any)
	require.True(t, ok)
	app, ok := configChanged["app"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "model", app["configId"])
	require.Equal(t, "gpt-5.4", app["currentValue"])
}

func TestSendEventUsesSessionStatusEnvelope(t *testing.T) {
	clientConn, relayConn, cleanup := newWSPair(t)
	defer cleanup()

	var key [32]byte
	key[0] = 1
	session := newSession("machine-1", key, 1)
	client := &Client{
		conn:      clientConn,
		connEpoch: 1,
		machineID: "machine-1",
		session:   session,
	}

	err := client.SendEvent(appwire.Event{
		Type:      appwire.EventSessionStatus,
		SessionID: "sid",
		At:        time.Now(),
		SessionInfo: &appwire.SessionStatusEvent{
			App: appwire.SessionStatusEventApp{
				ID:        "sid",
				Title:     "Example",
				Status:    appwire.SessionStatusRunning,
				Model:     "opus",
				MachineID: "machine-1",
				Runtime:   "claude-code",
				CWD:       "/workspace",
				Mode:      "default",
				CreatedAt: time.Unix(100, 0).UTC(),
				UpdatedAt: time.Unix(200, 0).UTC(),
			},
		},
	})
	require.NoError(t, err)

	msg := readEncryptedMessage(t, relayConn)
	require.Equal(t, MessageTypeEvent, msg.Type)

	payload := decryptResponse(t, session, msg)
	require.Equal(t, string(appwire.EventSessionStatus), payload["type"])
	require.Equal(t, "sid", payload["sessionId"])
	_, hasData := payload["data"]
	require.False(t, hasData)

	sessionStatus, ok := payload["sessionStatus"].(map[string]any)
	require.True(t, ok)
	app, ok := sessionStatus["app"].(map[string]any)
	require.True(t, ok)
	_, hasMetadata := app["metadata"]
	require.False(t, hasMetadata)
	require.Equal(t, "machine-1", app["machineId"])
	require.Equal(t, "claude-code", app["runtime"])
	require.Equal(t, "/workspace", app["cwd"])
	require.Equal(t, "default", app["mode"])
}

func TestRunHandlesRuntimeListRPC(t *testing.T) {
	upgrader := websocket.Upgrader{}
	serverConn := make(chan *websocket.Conn, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		require.NoError(t, err)
		serverConn <- conn
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	clientConn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	defer clientConn.Close()

	relayConn := <-serverConn
	defer relayConn.Close()

	var key [32]byte
	key[0] = 1
	session := newSession("machine-1", key, 1)
	rt := &routingRuntime{
		runtimes: []runtimemanager.RuntimeInfo{
			{
				ID:    "claude-code",
				Label: "Claude Code",
				Ready: true,
				ConfigOptions: []acpprotocol.SessionConfigOption{
					{
						ID:           "mode",
						Name:         "Approval Preset",
						Type:         "select",
						Category:     stringPtr("mode"),
						CurrentValue: "default",
						Options: acpprotocol.SessionConfigSelectOptions{
							Ungrouped: []acpprotocol.SessionConfigSelectOption{
								{Value: "default", Name: "Default"},
							},
						},
					},
				},
			},
			{
				ID:    "codex",
				Label: "Codex",
				Ready: true,
				ConfigOptions: []acpprotocol.SessionConfigOption{
					{
						ID:           "mode",
						Name:         "Approval Preset",
						Type:         "select",
						Category:     stringPtr("mode"),
						CurrentValue: "read-only",
						Options: acpprotocol.SessionConfigSelectOptions{
							Ungrouped: []acpprotocol.SessionConfigSelectOption{
								{Value: "read-only", Name: "Read Only"},
							},
						},
					},
				},
			},
		},
	}
	client := &Client{
		conn:       clientConn,
		connEpoch:  1,
		machineID:  "machine-1",
		runtime:    rt,
		session:    session,
		sessionCWD: map[string]string{},
	}

	runErr := make(chan error, 1)
	go func() {
		runErr <- client.Run(context.Background())
	}()

	require.NoError(t, relayConn.WriteJSON(encryptRPC(t, session, "machine-1", "msg-runtime-list", "runtime.list", nil)))

	msg := readEncryptedMessage(t, relayConn)
	require.Equal(t, MessageTypeResponse, msg.Type)
	require.Equal(t, "msg-runtime-list", msg.MsgID)

	payload := decryptResponse(t, session, msg)
	require.Equal(t, "", payload["error"])
	result, ok := payload["result"].(map[string]any)
	require.True(t, ok)
	runtimes, ok := result["runtimes"].([]any)
	require.True(t, ok)
	require.Len(t, runtimes, 2)
	first, ok := runtimes[0].(map[string]any)
	require.True(t, ok)
	configOptions, ok := first["configOptions"].([]any)
	require.True(t, ok)
	require.NotEmpty(t, configOptions)

	require.NoError(t, clientConn.Close())
	select {
	case <-runErr:
	case <-time.After(2 * time.Second):
		t.Fatal("relay run loop did not stop")
	}
}

func TestRunPassesRuntimeToSessionCreate(t *testing.T) {
	upgrader := websocket.Upgrader{}
	serverConn := make(chan *websocket.Conn, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		require.NoError(t, err)
		serverConn <- conn
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	clientConn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	defer clientConn.Close()

	relayConn := <-serverConn
	defer relayConn.Close()

	var key [32]byte
	key[0] = 1
	session := newSession("machine-1", key, 1)
	rt := &routingRuntime{}
	client := &Client{
		conn:       clientConn,
		connEpoch:  1,
		machineID:  "machine-1",
		runtime:    rt,
		session:    session,
		sessionCWD: map[string]string{},
	}

	runErr := make(chan error, 1)
	go func() {
		runErr <- client.Run(context.Background())
	}()

	require.NoError(t, relayConn.WriteJSON(encryptRPC(t, session, "machine-1", "msg-create", "session.create", map[string]any{
		"cwd":     "/tmp",
		"runtime": "codex",
	})))

	msg := readEncryptedMessage(t, relayConn)
	require.Equal(t, MessageTypeResponse, msg.Type)
	require.Equal(t, "msg-create", msg.MsgID)

	payload := decryptResponse(t, session, msg)
	require.Equal(t, "", payload["error"])
	result, ok := payload["result"].(map[string]any)
	require.True(t, ok)
	app, ok := result["app"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "codex", app["runtime"])
	acp, ok := result["acp"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "sid", acp["sessionId"])
	require.Equal(t, "codex", rt.lastRuntime)

	require.NoError(t, clientConn.Close())
	select {
	case <-runErr:
	case <-time.After(2 * time.Second):
		t.Fatal("relay run loop did not stop")
	}
}

func TestRpcCreateSessionRequiresRuntime(t *testing.T) {
	client := &Client{runtime: &routingRuntime{}}

	result, errStr := client.rpcCreateSession(context.Background(), map[string]any{
		"cwd": "/tmp",
	})
	require.Nil(t, result)
	require.Equal(t, "missing runtime", errStr)
}

func TestRpcSessionHandlersRejectMalformedTypedParams(t *testing.T) {
	client := &Client{
		runtime:    &routingRuntime{},
		sessionCWD: map[string]string{},
	}

	tests := []struct {
		name   string
		call   func() (any, string)
		errMsg string
	}{
		{
			name: "create rejects invalid useWorktree type",
			call: func() (any, string) {
				return client.rpcCreateSession(context.Background(), map[string]any{
					"cwd":         "/tmp",
					"runtime":     "codex",
					"useWorktree": "yes",
				})
			},
			errMsg: "invalid create params:",
		},
		{
			name: "load rejects invalid model type",
			call: func() (any, string) {
				return client.rpcLoadSession(context.Background(), map[string]any{
					"sessionId": "sid",
					"cwd":       "/tmp",
					"runtime":   "codex",
					"model":     []any{"bad"},
				})
			},
			errMsg: "invalid load params:",
		},
		{
			name: "resolve rejects invalid sessionIds type",
			call: func() (any, string) {
				return client.rpcResolveSessions(context.Background(), map[string]any{
					"sessionIds": []any{"sid", 1},
				})
			},
			errMsg: "invalid resolve params:",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, errStr := tt.call()
			require.Contains(t, errStr, tt.errMsg)
		})
	}
}

func TestRpcLoadSessionRequiresRuntime(t *testing.T) {
	client := &Client{
		runtime:    &blockingRuntime{loadStarted: make(chan struct{}, 1), unblock: make(chan struct{})},
		sessionCWD: map[string]string{},
	}

	result, errStr := client.rpcLoadSession(context.Background(), map[string]any{
		"sessionId": "sid",
		"cwd":       "/tmp",
	})
	require.Nil(t, result)
	require.Equal(t, "missing runtime", errStr)
}

func TestWriteForSessionErrors(t *testing.T) {
	var key [32]byte
	key[0] = 1
	session := newSession("machine-1", key, 1)
	msg := EncryptedMessage{Type: MessageTypeEvent}

	t.Run("relay not connected", func(t *testing.T) {
		client := &Client{
			connEpoch: 1,
			session:   session,
		}
		err := client.writeForSession(session, msg)
		require.ErrorIs(t, err, ErrRelayNotConnected)
	})

	t.Run("no active session", func(t *testing.T) {
		client := &Client{
			conn:      &websocket.Conn{},
			connEpoch: 1,
		}
		err := client.writeForSession(session, msg)
		require.ErrorIs(t, err, ErrNoActiveSession)
	})

	t.Run("stale session pointer", func(t *testing.T) {
		current := newSession("machine-1", key, 1)
		client := &Client{
			conn:      &websocket.Conn{},
			connEpoch: 1,
			session:   current,
		}
		err := client.writeForSession(session, msg)
		require.ErrorIs(t, err, ErrStaleRelaySession)
	})

	t.Run("stale session epoch", func(t *testing.T) {
		current := newSession("machine-1", key, 2)
		client := &Client{
			conn:      &websocket.Conn{},
			connEpoch: 2,
			session:   current,
		}
		err := client.writeForSession(session, msg)
		require.ErrorIs(t, err, ErrStaleRelaySession)
	})
}

func TestWriteProtocolForEpoch(t *testing.T) {
	t.Run("stale epoch", func(t *testing.T) {
		client := &Client{
			conn:      &websocket.Conn{},
			connEpoch: 2,
		}
		err := client.writeProtocolForEpoch(1, SessionAckMessage{Type: MessageTypeSessionAck})
		require.ErrorIs(t, err, ErrStaleRelaySession)
	})

	t.Run("relay not connected", func(t *testing.T) {
		client := &Client{connEpoch: 1}
		err := client.writeProtocolForEpoch(1, SessionAckMessage{Type: MessageTypeSessionAck})
		require.ErrorIs(t, err, ErrRelayNotConnected)
	})

	t.Run("writes to current connection", func(t *testing.T) {
		clientConn, relayConn, cleanup := newWSPair(t)
		defer cleanup()

		client := &Client{
			conn:      clientConn,
			connEpoch: 7,
		}
		require.NoError(t, client.writeProtocolForEpoch(7, SessionAckMessage{
			Type:                MessageTypeSessionAck,
			MachineID:           "machine-1",
			MachineEphemeralPub: "pub",
			Signature:           "sig",
		}))

		var msg SessionAckMessage
		require.NoError(t, relayConn.ReadJSON(&msg))
		require.Equal(t, MessageTypeSessionAck, msg.Type)
		require.Equal(t, "machine-1", msg.MachineID)
		require.Equal(t, "pub", msg.MachineEphemeralPub)
		require.Equal(t, "sig", msg.Signature)
	})
}

func TestInstallSessionRejectsStaleEpoch(t *testing.T) {
	var key [32]byte
	key[0] = 1
	client := &Client{
		connEpoch: 2,
	}
	err := client.installSession(newSession("machine-1", key, 1))
	require.ErrorIs(t, err, ErrStaleRelaySession)
}

func TestConnectHandshakeIsolation(t *testing.T) {
	upgrader := websocket.Upgrader{}
	registerSeen := make(chan struct{}, 1)
	challengeRespSeen := make(chan struct{}, 1)
	registeredGate := make(chan struct{})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		require.NoError(t, err)
		defer conn.Close()

		var reg RegisterMessage
		require.NoError(t, conn.ReadJSON(&reg))
		require.Equal(t, MessageTypeRegister, reg.Type)
		registerSeen <- struct{}{}

		require.NoError(t, conn.WriteJSON(ChallengeMessage{
			Type:  MessageTypeChallenge,
			Nonce: "nonce",
		}))

		var resp ChallengeResponseMessage
		require.NoError(t, conn.ReadJSON(&resp))
		require.Equal(t, MessageTypeChallengeResponse, resp.Type)
		challengeRespSeen <- struct{}{}

		require.NoError(t, conn.SetReadDeadline(time.Now().Add(200*time.Millisecond)))
		var extra map[string]any
		err = conn.ReadJSON(&extra)
		require.Error(t, err)
		require.True(t, isTimeoutErr(err), "expected read timeout, got %v", err)

		<-registeredGate
		require.NoError(t, conn.SetReadDeadline(time.Time{}))
		require.NoError(t, conn.WriteJSON(RegisteredMessage{
			Type:      MessageTypeRegistered,
			MachineID: "machine-1",
		}))
	}))
	defer server.Close()

	_, machineSignPriv, err := ed25519.GenerateKey(crand.Reader)
	require.NoError(t, err)

	client := &Client{
		relayURL:        "ws" + strings.TrimPrefix(server.URL, "http"),
		machineID:       "machine-1",
		hostname:        "host",
		machineSignPriv: machineSignPriv,
	}

	connectErr := make(chan error, 1)
	go func() {
		connectErr <- client.Connect(context.Background())
	}()

	select {
	case <-registerSeen:
	case <-time.After(2 * time.Second):
		t.Fatal("register was not received")
	}
	select {
	case <-challengeRespSeen:
	case <-time.After(2 * time.Second):
		t.Fatal("challenge response was not received")
	}

	err = client.SendEvent(appwire.Event{
		Type:      appwire.EventRunFinished,
		SessionID: "sid",
		At:        time.Now(),
	})
	require.ErrorIs(t, err, ErrRelayNotConnected)

	close(registeredGate)

	select {
	case err := <-connectErr:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("connect did not finish")
	}

	require.NoError(t, client.Close())
}

func TestOldHandleRPCDoesNotWriteToNewConnection(t *testing.T) {
	clientConn1, relayConn1, cleanup1 := newWSPair(t)
	defer cleanup1()

	var key [32]byte
	key[0] = 1
	oldSession := newSession("machine-1", key, 1)
	rt := &blockingRuntime{
		loadStarted: make(chan struct{}, 1),
		unblock:     make(chan struct{}),
	}
	client := &Client{
		conn:       clientConn1,
		connEpoch:  1,
		machineID:  "machine-1",
		runtime:    rt,
		session:    oldSession,
		sessionCWD: map[string]string{},
	}

	runErr := make(chan error, 1)
	go func() {
		runErr <- client.Run(context.Background())
	}()

	require.NoError(t, relayConn1.WriteJSON(encryptRPC(t, oldSession, "machine-1", "msg-load", "session.load", map[string]any{
		"sessionId": "sid",
		"cwd":       "/tmp",
		"runtime":   "claude-code",
	})))

	select {
	case <-rt.loadStarted:
	case <-time.After(time.Second):
		t.Fatal("session.load did not start")
	}

	require.NoError(t, client.Close())
	select {
	case <-runErr:
	case <-time.After(2 * time.Second):
		t.Fatal("run loop did not stop after close")
	}

	clientConn2, relayConn2, cleanup2 := newWSPair(t)
	defer cleanup2()

	newEpoch := client.publishActiveConnection(clientConn2)
	require.NoError(t, client.installSession(newSession("machine-1", key, newEpoch)))

	close(rt.unblock)

	require.NoError(t, relayConn2.SetReadDeadline(time.Now().Add(300*time.Millisecond)))
	var msg map[string]any
	err := relayConn2.ReadJSON(&msg)
	require.Error(t, err)
	require.True(t, isTimeoutErr(err), "expected timeout, got %v", err)
}

func TestManyHandleRPCDoNotWriteToNewConnection(t *testing.T) {
	clientConn1, relayConn1, cleanup1 := newWSPair(t)
	defer cleanup1()

	var key [32]byte
	key[0] = 1
	oldSession := newSession("machine-1", key, 1)
	const rpcCount = 5

	rt := &blockingRuntime{
		loadStarted: make(chan struct{}, rpcCount),
		unblock:     make(chan struct{}),
	}
	client := &Client{
		conn:       clientConn1,
		connEpoch:  1,
		machineID:  "machine-1",
		runtime:    rt,
		session:    oldSession,
		sessionCWD: map[string]string{},
	}

	runErr := make(chan error, 1)
	go func() {
		runErr <- client.Run(context.Background())
	}()

	for i := 0; i < rpcCount; i++ {
		require.NoError(t, relayConn1.WriteJSON(encryptRPC(
			t,
			oldSession,
			"machine-1",
			fmt.Sprintf("msg-load-%d", i),
			"session.load",
			map[string]any{
				"sessionId": fmt.Sprintf("sid-%d", i),
				"cwd":       "/tmp",
				"runtime":   "claude-code",
			},
		)))
	}

	for i := 0; i < rpcCount; i++ {
		select {
		case <-rt.loadStarted:
		case <-time.After(time.Second):
			t.Fatalf("session.load %d did not start", i)
		}
	}

	require.NoError(t, client.Close())
	select {
	case <-runErr:
	case <-time.After(2 * time.Second):
		t.Fatal("run loop did not stop after close")
	}

	clientConn2, relayConn2, cleanup2 := newWSPair(t)
	defer cleanup2()

	newEpoch := client.publishActiveConnection(clientConn2)
	require.NoError(t, client.installSession(newSession("machine-1", key, newEpoch)))

	close(rt.unblock)

	require.NoError(t, relayConn2.SetReadDeadline(time.Now().Add(300*time.Millisecond)))
	var msg map[string]any
	err := relayConn2.ReadJSON(&msg)
	require.Error(t, err)
	require.True(t, isTimeoutErr(err), "expected timeout, got %v", err)
}

func TestCloseAndSendEventConcurrent(t *testing.T) {
	clientConn, _, cleanup := newWSPair(t)
	defer cleanup()

	var key [32]byte
	key[0] = 1
	session := newSession("machine-1", key, 1)
	client := &Client{
		conn:      clientConn,
		connEpoch: 1,
		machineID: "machine-1",
		session:   session,
	}

	start := make(chan struct{})
	errCh := make(chan error, 2)

	go func() {
		<-start
		errCh <- client.SendEvent(appwire.Event{
			Type:      appwire.EventRunFinished,
			SessionID: "sid",
			At:        time.Now(),
		})
	}()
	go func() {
		<-start
		errCh <- client.Close()
	}()

	close(start)

	for i := 0; i < 2; i++ {
		err := <-errCh
		if err == nil {
			continue
		}
		if errors.Is(err, ErrRelayNotConnected) ||
			errors.Is(err, ErrNoActiveSession) ||
			errors.Is(err, ErrStaleRelaySession) {
			continue
		}
		if strings.Contains(err.Error(), "closed network connection") {
			continue
		}
		t.Fatalf("unexpected error: %v", err)
	}
}

func encryptRPC(
	t *testing.T,
	session *Session,
	machineID string,
	msgID string,
	method string,
	params map[string]any,
) EncryptedMessage {
	t.Helper()

	body, err := json.Marshal(RPCPayload{
		Method: method,
		Params: params,
	})
	require.NoError(t, err)

	nonce, ciphertext, err := session.encrypt(string(MessageTypeRPC), msgID, body)
	require.NoError(t, err)

	return EncryptedMessage{
		Type:       MessageTypeRPC,
		MachineID:  machineID,
		MsgID:      msgID,
		Nonce:      nonce,
		Ciphertext: ciphertext,
	}
}

func readEncryptedMessage(t *testing.T, conn *websocket.Conn) EncryptedMessage {
	t.Helper()

	require.NoError(t, conn.SetReadDeadline(time.Now().Add(2*time.Second)))

	var msg EncryptedMessage
	require.NoError(t, conn.ReadJSON(&msg))
	return msg
}

func decryptResponse(t *testing.T, session *Session, msg EncryptedMessage) map[string]any {
	t.Helper()

	body, err := session.decrypt(string(msg.Type), msg.MsgID, msg.Nonce, msg.Ciphertext)
	require.NoError(t, err)

	var payload map[string]any
	require.NoError(t, json.Unmarshal(body, &payload))
	return payload
}

func resolvedStatusFromRPCResult(t *testing.T, result any) string {
	t.Helper()

	body, err := json.Marshal(result)
	require.NoError(t, err)

	var payload struct {
		Sessions []struct {
			Status string `json:"status"`
		} `json:"sessions"`
	}
	require.NoError(t, json.Unmarshal(body, &payload))
	require.Len(t, payload.Sessions, 1)
	return payload.Sessions[0].Status
}

func newWSPair(t *testing.T) (*websocket.Conn, *websocket.Conn, func()) {
	t.Helper()

	upgrader := websocket.Upgrader{}
	serverConn := make(chan *websocket.Conn, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		require.NoError(t, err)
		serverConn <- conn
	}))

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	clientConn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)

	relayConn := <-serverConn
	cleanup := func() {
		_ = clientConn.Close()
		_ = relayConn.Close()
		server.Close()
	}

	return clientConn, relayConn, cleanup
}

func isTimeoutErr(err error) bool {
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}

// --- fs.list tests ---

func TestRpcFsList(t *testing.T) {
	root := t.TempDir()
	// Create project structure: src/, src/main.go, README.md, empty/
	require.NoError(t, os.MkdirAll(filepath.Join(root, "src"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "src", "main.go"), []byte("package main"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "README.md"), []byte("# hi"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "empty"), 0o755))

	client := &Client{
		sessionCWD: map[string]string{"sid": root},
	}
	ctx := context.Background()

	t.Run("list root directory", func(t *testing.T) {
		result, errStr := client.rpcFsList(ctx, map[string]any{
			"sessionId": "sid",
		})
		require.Empty(t, errStr)
		entries := result.(appwire.FsListResult).Entries

		// Should contain src (dir), empty (dir), README.md (file)
		names := make([]string, len(entries))
		for i, e := range entries {
			names[i] = e.Name
		}
		require.Contains(t, names, "src")
		require.Contains(t, names, "README.md")

		// Dirs before files
		firstFileIdx := -1
		lastDirIdx := -1
		for i, e := range entries {
			if e.IsDir && i > lastDirIdx {
				lastDirIdx = i
			}
			if !e.IsDir && (firstFileIdx == -1 || i < firstFileIdx) {
				firstFileIdx = i
			}
		}
		if firstFileIdx >= 0 && lastDirIdx >= 0 {
			require.Greater(t, firstFileIdx, lastDirIdx, "dirs should come before files")
		}
	})

	t.Run("list subdirectory", func(t *testing.T) {
		result, errStr := client.rpcFsList(ctx, map[string]any{
			"sessionId": "sid",
			"path":      "src",
		})
		require.Empty(t, errStr)
		entries := result.(appwire.FsListResult).Entries

		require.Len(t, entries, 1)
		require.Equal(t, "main.go", entries[0].Name)
		require.Equal(t, filepath.Join("src", "main.go"), entries[0].Path)
		require.False(t, entries[0].IsDir)
	})

	t.Run("path traversal rejected", func(t *testing.T) {
		_, errStr := client.rpcFsList(ctx, map[string]any{
			"sessionId": "sid",
			"path":      "../../etc",
		})
		require.Equal(t, "path outside project", errStr)
	})

	t.Run("symlink escape rejected", func(t *testing.T) {
		symPath := filepath.Join(root, "escape")
		require.NoError(t, os.Symlink(os.TempDir(), symPath))
		t.Cleanup(func() { os.Remove(symPath) })

		_, errStr := client.rpcFsList(ctx, map[string]any{
			"sessionId": "sid",
			"path":      "escape",
		})
		require.Equal(t, "symlink escape detected", errStr)
	})

	t.Run("empty directory", func(t *testing.T) {
		result, errStr := client.rpcFsList(ctx, map[string]any{
			"sessionId": "sid",
			"path":      "empty",
		})
		require.Empty(t, errStr)
		entries := result.(appwire.FsListResult).Entries
		require.Empty(t, entries)
	})

	t.Run("unknown session", func(t *testing.T) {
		_, errStr := client.rpcFsList(ctx, map[string]any{
			"sessionId": "nonexistent",
		})
		require.Equal(t, "unknown session", errStr)
	})

	t.Run("max entries limit", func(t *testing.T) {
		bigRoot := t.TempDir()
		for i := 0; i < 250; i++ {
			require.NoError(t, os.WriteFile(
				filepath.Join(bigRoot, fmt.Sprintf("file_%03d.txt", i)),
				[]byte("x"), 0o644,
			))
		}
		c := &Client{sessionCWD: map[string]string{"sid": bigRoot}}
		result, errStr := c.rpcFsList(ctx, map[string]any{
			"sessionId": "sid",
		})
		require.Empty(t, errStr)
		entries := result.(appwire.FsListResult).Entries
		require.LessOrEqual(t, len(entries), 200)
	})
}

// --- fs.search tests ---

func TestRpcFsSearch(t *testing.T) {
	root := t.TempDir()
	// Create project structure for search tests
	require.NoError(t, os.MkdirAll(filepath.Join(root, "src", "util"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "src", "controllers"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "src", "models"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "cmd", "main"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "src", "main.go"), []byte("package main"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "src", "util", "helper.go"), []byte("package util"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "src", "main_test.go"), []byte("package main"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "main.go"), []byte("package main"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "README.md"), []byte("# hi"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "Makefile"), []byte("all:"), 0o644))

	client := &Client{
		sessionCWD: map[string]string{"sid": root},
	}
	ctx := context.Background()

	t.Run("match file name", func(t *testing.T) {
		result, errStr := client.rpcFsSearch(ctx, map[string]any{
			"sessionId": "sid",
			"query":     "helper",
		})
		require.Empty(t, errStr)
		results := result.(appwire.FsSearchResult).Results

		require.Len(t, results, 1)
		require.Equal(t, filepath.Join("src", "util", "helper.go"), results[0].Path)
		require.False(t, results[0].IsDir)
	})

	t.Run("match directory name", func(t *testing.T) {
		result, errStr := client.rpcFsSearch(ctx, map[string]any{
			"sessionId": "sid",
			"query":     "model",
		})
		require.Empty(t, errStr)
		results := result.(appwire.FsSearchResult).Results

		paths := make([]string, len(results))
		for i, r := range results {
			paths[i] = r.Path
		}
		require.Contains(t, paths, filepath.Join("src", "models"))
	})

	t.Run("case insensitive", func(t *testing.T) {
		result, errStr := client.rpcFsSearch(ctx, map[string]any{
			"sessionId": "sid",
			"query":     "makefile",
		})
		require.Empty(t, errStr)
		results := result.(appwire.FsSearchResult).Results

		paths := make([]string, len(results))
		for i, r := range results {
			paths[i] = r.Path
		}
		require.Contains(t, paths, "Makefile")
	})

	t.Run("sort dirs first then short paths", func(t *testing.T) {
		result, errStr := client.rpcFsSearch(ctx, map[string]any{
			"sessionId": "sid",
			"query":     "main",
		})
		require.Empty(t, errStr)
		results := result.(appwire.FsSearchResult).Results

		require.GreaterOrEqual(t, len(results), 3)
		// First result should be a directory (cmd/main)
		require.True(t, results[0].IsDir, "first result should be a directory")
		// Among files, shorter path comes first
		fileResults := make([]appwire.FsSearchEntry, 0)
		for _, r := range results {
			if !r.IsDir {
				fileResults = append(fileResults, r)
			}
		}
		require.GreaterOrEqual(t, len(fileResults), 2)
		require.LessOrEqual(t, len(fileResults[0].Path), len(fileResults[1].Path),
			"shorter path should come first among files")
	})

	t.Run("max 50 results", func(t *testing.T) {
		bigRoot := t.TempDir()
		for i := 0; i < 100; i++ {
			require.NoError(t, os.WriteFile(
				filepath.Join(bigRoot, fmt.Sprintf("test_%03d.go", i)),
				[]byte("x"), 0o644,
			))
		}
		c := &Client{sessionCWD: map[string]string{"sid": bigRoot}}
		result, errStr := c.rpcFsSearch(ctx, map[string]any{
			"sessionId": "sid",
			"query":     "test",
		})
		require.Empty(t, errStr)
		results := result.(appwire.FsSearchResult).Results
		require.LessOrEqual(t, len(results), 50)
	})

	t.Run("empty query error", func(t *testing.T) {
		_, errStr := client.rpcFsSearch(ctx, map[string]any{
			"sessionId": "sid",
			"query":     "",
		})
		require.Equal(t, "missing query", errStr)
	})

	t.Run("missing query error", func(t *testing.T) {
		_, errStr := client.rpcFsSearch(ctx, map[string]any{
			"sessionId": "sid",
		})
		require.Equal(t, "missing query", errStr)
	})

	t.Run("unknown session", func(t *testing.T) {
		_, errStr := client.rpcFsSearch(ctx, map[string]any{
			"sessionId": "nonexistent",
			"query":     "main",
		})
		require.Equal(t, "unknown session", errStr)
	})
}

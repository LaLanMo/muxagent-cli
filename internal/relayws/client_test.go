package relayws

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/LaLanMo/muxagent-cli/internal/domain"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
)

type blockingRuntime struct {
	loadStarted chan struct{}
	unblock     chan struct{}
}

func (r *blockingRuntime) NewSession(ctx context.Context, cwd string, permissionMode string) (string, error) {
	return "sid", nil
}

func (r *blockingRuntime) LoadSession(ctx context.Context, sessionID, cwd, permissionMode string) error {
	select {
	case r.loadStarted <- struct{}{}:
	default:
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-r.unblock:
		return nil
	}
}

func (r *blockingRuntime) Prompt(ctx context.Context, sessionID string, content []domain.ContentBlock) (string, error) {
	return "stop", nil
}

func (r *blockingRuntime) Cancel(ctx context.Context, sessionID string) error {
	return nil
}

func (r *blockingRuntime) ReplyPermission(ctx context.Context, sessionID, requestID, optionID string) error {
	return nil
}

func (r *blockingRuntime) ListSessions(ctx context.Context, cwd string) ([]domain.SessionSummary, error) {
	return nil, nil
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
	session := newSession("machine-1", key)
	rt := &blockingRuntime{
		loadStarted: make(chan struct{}, 1),
		unblock:     make(chan struct{}),
	}
	client := &Client{
		conn:       clientConn,
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
	require.Equal(t, true, result["ok"])

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

func (r *listingRuntime) ListSessions(ctx context.Context, cwd string) ([]domain.SessionSummary, error) {
	return r.sessions, nil
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
	session := newSession("machine-1", key)
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

	require.NoError(t, clientConn.Close())
	select {
	case <-runErr:
	case <-time.After(2 * time.Second):
		t.Fatal("relay run loop did not stop")
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

package relayws

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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

func (r *blockingRuntime) SetMode(ctx context.Context, sessionID, modeID string) error {
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
		m := result.(map[string]any)
		entries := m["entries"].([]fsEntry)

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
		m := result.(map[string]any)
		entries := m["entries"].([]fsEntry)

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
		m := result.(map[string]any)
		entries := m["entries"].([]fsEntry)
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
		m := result.(map[string]any)
		entries := m["entries"].([]fsEntry)
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
		m := result.(map[string]any)
		results := m["results"].([]fsSearchResult)

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
		m := result.(map[string]any)
		results := m["results"].([]fsSearchResult)

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
		m := result.(map[string]any)
		results := m["results"].([]fsSearchResult)

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
		m := result.(map[string]any)
		results := m["results"].([]fsSearchResult)

		require.GreaterOrEqual(t, len(results), 3)
		// First result should be a directory (cmd/main)
		require.True(t, results[0].IsDir, "first result should be a directory")
		// Among files, shorter path comes first
		fileResults := make([]fsSearchResult, 0)
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
		m := result.(map[string]any)
		results := m["results"].([]fsSearchResult)
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

package relayws

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/LaLanMo/muxagent-cli/internal/auth"
	"github.com/LaLanMo/muxagent-cli/internal/crypto"
	"github.com/LaLanMo/muxagent-cli/internal/domain"
	"github.com/LaLanMo/muxagent-cli/internal/keyring"
	"github.com/LaLanMo/muxagent-cli/internal/worktree"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"golang.org/x/crypto/curve25519"
)

const (
	pingInterval = 15 * time.Second
	pongTimeout  = 10 * time.Second
	writeWait    = 5 * time.Second
)

var (
	ErrRelayNotConnected = errors.New("relay not connected")
	ErrNoActiveSession   = errors.New("no active session")
	ErrStaleRelaySession = errors.New("stale relay session")
)

// RuntimeClient is the subset of runtime.Client that the relay needs.
// Defined here to avoid a circular import with the runtime package.
type RuntimeClient interface {
	NewSession(ctx context.Context, cwd string, permissionMode string) (string, []domain.ConfigOption, error)
	LoadSession(ctx context.Context, sessionID, cwd, permissionMode, model string) ([]domain.ConfigOption, error)
	ListSessions(ctx context.Context, cwd string) ([]domain.SessionSummary, error)
	Prompt(ctx context.Context, sessionID string, content []domain.ContentBlock) (string, *domain.PromptUsage, error)
	Cancel(ctx context.Context, sessionID string) error
	SetMode(ctx context.Context, sessionID, modeID string) error
	SetConfigOption(ctx context.Context, sessionID, configID, value string) error
	ReplyPermission(ctx context.Context, sessionID, requestID, optionID string) error
	PendingApprovals() []domain.ApprovalRequest
}

type Client struct {
	relayURL  string
	machineID string
	hostname  string
	runtimeID string

	creds           *auth.Credentials
	machineSignPriv ed25519.PrivateKey
	keyring         *keyring.Manager

	// Lock ordering: stateMu must never be acquired while holding writeMu.
	// Callers may snapshot state under stateMu, release it, then serialize WS
	// frame writes under writeMu.
	stateMu   sync.RWMutex
	conn      *websocket.Conn
	connEpoch uint64
	writeMu   sync.Mutex

	session *Session

	runtime  RuntimeClient
	eventBuf *EventBuffer
	wtStore  *worktree.Store

	sessionCWDMu sync.RWMutex
	sessionCWD   map[string]string // sessionID → cwd
}

func NewMachineClient(
	relayURL, hostname, runtimeID string,
	creds *auth.Credentials,
	machineSignPriv ed25519.PrivateKey,
	keyringMgr *keyring.Manager,
	rt RuntimeClient,
	eventBuf *EventBuffer,
	wtStore *worktree.Store,
) (*Client, error) {
	if creds == nil {
		return nil, fmt.Errorf("credentials required")
	}
	if machineSignPriv == nil {
		return nil, fmt.Errorf("machine signing private key required")
	}
	if keyringMgr == nil {
		return nil, fmt.Errorf("keyring required")
	}
	return &Client{
		relayURL:        relayURL,
		machineID:       creds.MachineID,
		hostname:        hostname,
		runtimeID:       runtimeID,
		creds:           creds,
		machineSignPriv: machineSignPriv,
		keyring:         keyringMgr,
		runtime:         rt,
		eventBuf:        eventBuf,
		wtStore:         wtStore,
		sessionCWD:      make(map[string]string),
	}, nil
}

func (c *Client) HasSession() bool {
	c.stateMu.RLock()
	defer c.stateMu.RUnlock()
	return c.session != nil
}

func (c *Client) Connect(ctx context.Context) error {
	oldConn := c.detachActiveConnection()
	if oldConn != nil {
		_ = c.closeConn(oldConn)
	}

	conn, _, err := websocket.DefaultDialer.DialContext(ctx, c.relayURL, nil)
	if err != nil {
		return fmt.Errorf("dial relay: %w", err)
	}

	watcherDone := make(chan struct{})
	var watcherStopOnce sync.Once
	stopWatcher := func() {
		watcherStopOnce.Do(func() {
			close(watcherDone)
		})
	}
	defer stopWatcher()
	go func() {
		select {
		case <-ctx.Done():
			_ = conn.Close()
		case <-watcherDone:
		}
	}()

	if err := writeJSONTo(conn, RegisterMessage{
		Type:      MessageTypeRegister,
		Role:      RoleMachine,
		MachineID: c.machineID,
		Hostname:  c.hostname,
	}); err != nil {
		_ = conn.Close()
		return fmt.Errorf("send register: %w", err)
	}

	for {
		var raw json.RawMessage
		if err := conn.ReadJSON(&raw); err != nil {
			conn.Close()
			return fmt.Errorf("read register response: %w", err)
		}
		var envelope MessageEnvelope
		if err := json.Unmarshal(raw, &envelope); err != nil {
			continue
		}
		switch envelope.Type {
		case MessageTypeChallenge:
			var msg ChallengeMessage
			if err := json.Unmarshal(raw, &msg); err != nil || msg.Nonce == "" {
				_ = conn.Close()
				return fmt.Errorf("invalid challenge from relay")
			}
			signedMessage := crypto.BuildMachineAuthMessage(c.machineID, msg.Nonce)
			signature := crypto.SignBase64(signedMessage, c.machineSignPriv)
			if err := writeJSONTo(conn, ChallengeResponseMessage{
				Type:      MessageTypeChallengeResponse,
				Signature: signature,
			}); err != nil {
				_ = conn.Close()
				return fmt.Errorf("send challenge response: %w", err)
			}
		case MessageTypeRegistered:
			// Relay sends pings; we reply with pongs and reset the read deadline.
			// Overriding the default ping handler means we must send the pong ourselves.
			conn.SetPingHandler(func(appData string) error {
				_ = conn.SetReadDeadline(time.Now().Add(pingInterval + pongTimeout))
				return conn.WriteControl(websocket.PongMessage, []byte(appData), time.Now().Add(writeWait))
			})
			_ = conn.SetReadDeadline(time.Now().Add(pingInterval + pongTimeout))
			stopWatcher()
			if err := ctx.Err(); err != nil {
				_ = conn.Close()
				return err
			}
			c.publishActiveConnection(conn)
			return nil
		case MessageTypeError:
			var msg ErrorMessage
			if err := json.Unmarshal(raw, &msg); err != nil {
				_ = conn.Close()
				return fmt.Errorf("registration failed: unknown error")
			}
			_ = conn.Close()
			return fmt.Errorf("registration failed: %s", msg.Error)
		}
	}
}

func (c *Client) Run(ctx context.Context) error {
	conn, connEpoch := c.currentConnection()
	if conn == nil {
		return ErrRelayNotConnected
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		var raw json.RawMessage
		if err := conn.ReadJSON(&raw); err != nil {
			return fmt.Errorf("read message: %w", err)
		}

		var envelope MessageEnvelope
		if err := json.Unmarshal(raw, &envelope); err != nil {
			continue
		}

		switch envelope.Type {
		case MessageTypeSessionInit:
			var msg SessionInitMessage
			if err := json.Unmarshal(raw, &msg); err != nil {
				continue
			}
			if err := c.handleSessionInit(connEpoch, msg); err != nil && !isExpectedRelayDrop(err) {
				log.Printf("session-init error: %v", err)
			}
		case MessageTypeSessionEnd:
			var msg SessionEndMessage
			if err := json.Unmarshal(raw, &msg); err != nil {
				continue
			}
			c.handleSessionEnd(msg)
		case MessageTypeRPC:
			var enc EncryptedMessage
			if err := json.Unmarshal(raw, &enc); err != nil {
				continue
			}
			// RPCs like session.prompt can block for a long time while the agent runs.
			// Handle them off the read loop so follow-up requests like session.cancel
			// and session.load are still processed promptly.
			go c.handleRPC(connEpoch, enc)
		case MessageTypeResponse:
			var enc EncryptedMessage
			if err := json.Unmarshal(raw, &enc); err != nil {
				continue
			}
			c.handleResponse(enc)
		case MessageTypeEvent:
			continue
		case MessageTypeError:
			var msg ErrorMessage
			if err := json.Unmarshal(raw, &msg); err == nil {
				log.Printf("relay error: %s", msg.Error)
			}
		}
	}
}

func (c *Client) handleSessionInit(connEpoch uint64, msg SessionInitMessage) error {
	if msg.MachineID != c.machineID {
		return fmt.Errorf("machine_id mismatch")
	}
	claims, err := c.keyring.VerifyMachineToken(msg.MachineToken)
	if err != nil {
		return err
	}
	if claims.MachineID != c.machineID || claims.MasterID != c.creds.MasterID {
		return fmt.Errorf("machine token mismatch")
	}
	pub, ok := c.keyring.SignPub(claims.Fingerprint)
	if !ok {
		return fmt.Errorf("unauthorized signer")
	}
	sigBytes, err := base64.StdEncoding.DecodeString(msg.Signature)
	if err != nil {
		return err
	}
	initMsg := crypto.BuildSessionInitMessage(c.machineID, msg.ClientEphemeralPub)
	if !crypto.Verify(pub, []byte(initMsg), sigBytes) {
		return fmt.Errorf("invalid session-init signature")
	}

	clientEphemeralPub, err := base64.StdEncoding.DecodeString(msg.ClientEphemeralPub)
	if err != nil || len(clientEphemeralPub) != 32 {
		return fmt.Errorf("invalid client_ephemeral_pub")
	}

	machineEphemeralPub, machineEphemeralPriv, err := crypto.GenerateX25519KeyPair()
	if err != nil {
		return err
	}
	sharedSecret, err := curve25519.X25519(machineEphemeralPriv[:], clientEphemeralPub)
	if err != nil {
		return err
	}
	machineEphemeralPubB64 := base64.StdEncoding.EncodeToString(machineEphemeralPub[:])
	transcript := msg.ClientEphemeralPub + "|" + machineEphemeralPubB64 + "|" + c.machineID
	key, err := deriveSessionKey(sharedSecret, transcript)
	if err != nil {
		return err
	}

	ackMsg := crypto.BuildSessionAckMessage(c.machineID, machineEphemeralPubB64)
	ackSig := crypto.SignBase64(ackMsg, c.machineSignPriv)
	if err := c.writeProtocolForEpoch(connEpoch, SessionAckMessage{
		Type:                MessageTypeSessionAck,
		MachineID:           c.machineID,
		MachineEphemeralPub: machineEphemeralPubB64,
		Signature:           ackSig,
	}); err != nil {
		return err
	}

	return c.installSession(newSession(c.machineID, key, connEpoch))
}

// --- RPC routing ---

func (c *Client) handleRPC(connEpoch uint64, enc EncryptedMessage) {
	session := c.currentSession()
	if session == nil || session.connEpoch != connEpoch {
		return
	}
	plaintext, err := session.decrypt(string(enc.Type), enc.MsgID, enc.Nonce, enc.Ciphertext)
	if err != nil {
		return
	}
	var payload RPCPayload
	if err := json.Unmarshal(plaintext, &payload); err != nil {
		return
	}

	ctx := context.Background()
	var result any
	var respErr string

	switch payload.Method {
	case "session.create":
		result, respErr = c.rpcCreateSession(ctx, payload.Params)
	case "session.load":
		result, respErr = c.rpcLoadSession(ctx, payload.Params)
	case "session.resolve":
		result, respErr = c.rpcResolveSessions(ctx, payload.Params)
	case "session.prompt":
		result, respErr = c.rpcPrompt(ctx, payload.Params)
	case "session.cancel":
		result, respErr = c.rpcCancel(ctx, payload.Params)
	case "session.setMode":
		result, respErr = c.rpcSetMode(ctx, payload.Params)
	case "session.setConfigOption":
		result, respErr = c.rpcSetConfigOption(ctx, payload.Params)
	case "approval.reply":
		result, respErr = c.rpcReplyPermission(ctx, payload.Params)
	case "events.resync":
		result, respErr = c.rpcResyncEvents(ctx, payload.Params)
	case "approvals.pending":
		result, respErr = c.rpcPendingApprovals(ctx, payload.Params)
	case "fs.list":
		result, respErr = c.rpcFsList(ctx, payload.Params)
	case "fs.search":
		result, respErr = c.rpcFsSearch(ctx, payload.Params)
	case "echo":
		log.Printf("echo request from client: %v", payload.Params)
		result = payload.Params
	default:
		respErr = fmt.Sprintf("unknown method: %s", payload.Method)
	}

	respPayload := map[string]any{
		"result": result,
		"error":  respErr,
	}
	respBytes, err := json.Marshal(respPayload)
	if err != nil {
		log.Printf("rpc marshal response: %v", err)
		return
	}
	nonce, ciphertext, err := session.encrypt(string(MessageTypeResponse), enc.MsgID, respBytes)
	if err != nil {
		log.Printf("rpc encrypt response: %v", err)
		return
	}
	if err := c.writeForSession(session, EncryptedMessage{
		Type:       MessageTypeResponse,
		MachineID:  c.machineID,
		MsgID:      enc.MsgID,
		Nonce:      nonce,
		Ciphertext: ciphertext,
	}); err != nil && !isExpectedRelayDrop(err) {
		log.Printf("rpc write response: %v", err)
	}
}

// stringParam extracts a string from params with explicit type checking.
// Logs a warning if the key is present but has a non-string type.
func stringParam(params map[string]any, key string) string {
	if v, ok := params[key].(string); ok {
		return v
	}
	if params[key] != nil {
		log.Printf("[relay] param %q: expected string, got %T", key, params[key])
	}
	return ""
}

func (c *Client) rpcCreateSession(ctx context.Context, params map[string]any) (any, string) {
	if c.runtime == nil {
		return nil, "runtime not available"
	}
	cwd := stringParam(params, "cwd")
	if cwd == "" {
		return nil, "missing cwd"
	}
	// Expand ~ before any path operations (worktree, git, etc.)
	if strings.HasPrefix(cwd, "~/") || cwd == "~" {
		if home, err := os.UserHomeDir(); err == nil {
			cwd = filepath.Join(home, cwd[1:])
		}
	}
	permissionMode := stringParam(params, "permissionMode")
	var useWorktree bool
	if v, exists := params["useWorktree"]; exists {
		var ok bool
		if useWorktree, ok = v.(bool); !ok {
			return nil, "useWorktree must be a boolean"
		}
	}

	actualCWD := cwd
	var wtMapping *worktree.Mapping

	if useWorktree {
		repoRoot, err := worktree.FindRepoRoot(cwd)
		if err != nil {
			return nil, "worktree requires a git repository"
		}
		wtID, err := randomHex(8)
		if err != nil {
			return nil, fmt.Sprintf("failed to generate worktree id: %v", err)
		}
		wtPath, err := worktree.Create(repoRoot, wtID)
		if err != nil {
			return nil, fmt.Sprintf("failed to create worktree: %v", err)
		}
		// Preserve subdirectory offset within the repo.
		relPath, err := filepath.Rel(repoRoot, cwd)
		if err != nil {
			return nil, fmt.Sprintf("failed to compute relative path: %v", err)
		}
		actualCWD = filepath.Join(wtPath, relPath)
		wtMapping = &worktree.Mapping{
			WorktreeID:   wtID,
			WorktreePath: wtPath,
			RepoRoot:     repoRoot,
			BranchName:   "muxagent/" + wtID,
		}
	}

	sessionID, configOpts, err := c.runtime.NewSession(ctx, actualCWD, permissionMode)
	if err != nil {
		return nil, err.Error()
	}
	c.sessionCWDMu.Lock()
	c.sessionCWD[sessionID] = actualCWD
	c.sessionCWDMu.Unlock()

	if wtMapping != nil && c.wtStore != nil {
		c.wtStore.Set(sessionID, *wtMapping)
		if err := c.wtStore.Save(); err != nil {
			log.Printf("worktree store save: %v", err)
		}
	}

	resp := map[string]any{"sessionId": sessionID, "runtime": c.runtimeID, "cwd": actualCWD}
	if len(configOpts) > 0 {
		resp["configOptions"] = configOpts
		log.Printf("[relay] session.create response includes %d configOptions", len(configOpts))
	} else {
		log.Printf("[relay] session.create response has NO configOptions")
	}
	return resp, ""
}

func (c *Client) rpcLoadSession(ctx context.Context, params map[string]any) (any, string) {
	if c.runtime == nil {
		return nil, "runtime not available"
	}
	sessionID := stringParam(params, "sessionId")
	if sessionID == "" {
		return nil, "missing sessionId"
	}
	cwd := stringParam(params, "cwd")
	if cwd == "" {
		return nil, "missing cwd"
	}

	// If this session was created with a worktree, use that path instead.
	if c.wtStore != nil {
		if wt := c.wtStore.Get(sessionID); wt != nil {
			if _, err := os.Stat(wt.WorktreePath); err == nil {
				cwd = wt.WorktreePath
			} else {
				log.Printf("worktree path gone for session %s, using original cwd", sessionID)
			}
		}
	}

	permissionMode := stringParam(params, "permissionMode")
	model := stringParam(params, "model")
	configOpts, err := c.runtime.LoadSession(ctx, sessionID, cwd, permissionMode, model)
	if err != nil {
		return nil, err.Error()
	}
	c.sessionCWDMu.Lock()
	c.sessionCWD[sessionID] = cwd
	c.sessionCWDMu.Unlock()
	resp := map[string]any{"ok": true}
	if len(configOpts) > 0 {
		resp["configOptions"] = configOpts
	}
	return resp, ""
}

func (c *Client) rpcResolveSessions(ctx context.Context, params map[string]any) (any, string) {
	if c.runtime == nil {
		return nil, "runtime not available"
	}
	var rawIDs []any
	if v, ok := params["sessionIds"].([]any); ok {
		rawIDs = v
	} else if params["sessionIds"] != nil {
		log.Printf("[relay] param %q: expected []any, got %T", "sessionIds", params["sessionIds"])
	}
	wanted := make(map[string]struct{}, len(rawIDs))
	for _, item := range rawIDs {
		id, _ := item.(string)
		if id != "" {
			wanted[id] = struct{}{}
		}
	}
	all, err := c.runtime.ListSessions(ctx, "")
	if err != nil {
		return nil, err.Error()
	}
	// If caller provided specific IDs, filter to those only.
	if len(wanted) > 0 {
		filtered := make([]domain.SessionSummary, 0, len(wanted))
		for _, s := range all {
			if _, ok := wanted[s.SessionID]; ok {
				filtered = append(filtered, s)
			}
		}
		all = filtered
	}
	return map[string]any{"sessions": all}, ""
}

func (c *Client) rpcPrompt(ctx context.Context, params map[string]any) (any, string) {
	if c.runtime == nil {
		return nil, "runtime not available"
	}
	sessionID := stringParam(params, "sessionId")
	if sessionID == "" {
		return nil, "missing sessionId"
	}

	// Parse content blocks from params
	var content []domain.ContentBlock
	if contentRaw, ok := params["content"]; ok {
		contentBytes, err := json.Marshal(contentRaw)
		if err != nil {
			return nil, "invalid content: " + err.Error()
		}
		if err := json.Unmarshal(contentBytes, &content); err != nil {
			return nil, "invalid content: " + err.Error()
		}
	}

	// If no content blocks but there's a text field, create a text block
	if len(content) == 0 {
		if text, ok := params["text"].(string); ok && text != "" {
			content = []domain.ContentBlock{{Type: "text", Text: text}}
		}
	}

	// Look up the CWD for this session (saved during create/load)
	c.sessionCWDMu.RLock()
	cwd := c.sessionCWD[sessionID]
	c.sessionCWDMu.RUnlock()

	// Run the prompt asynchronously — return ACK immediately so the
	// Flutter client's RPC timeout doesn't fire.  Use context.Background()
	// because the handleRPC ctx lifetime ends when we return.
	go func() {
		stopReason, usage, err := c.runtime.Prompt(context.Background(), sessionID, content)
		now := time.Now()
		if err != nil {
			if evErr := c.SendEvent(domain.Event{
				Type:      domain.EventRunFailed,
				SessionID: sessionID,
				At:        now,
				Error:     &domain.SessionError{Code: "prompt_error", Message: err.Error()},
			}); evErr != nil && !isExpectedRelayDrop(evErr) {
				log.Printf("send run.failed event: %v", evErr)
			}
			// Don't call syncSessionStatus — run.failed already signals the error state.
			return
		}
		data := map[string]any{"stopReason": stopReason}
		if usage != nil {
			data["totalTokens"] = usage.TotalTokens
			data["inputTokens"] = usage.InputTokens
			data["outputTokens"] = usage.OutputTokens
			data["cachedReadTokens"] = usage.CachedReadTokens
			data["cachedWriteTokens"] = usage.CachedWriteTokens
		}
		if evErr := c.SendEvent(domain.Event{
			Type:      domain.EventRunFinished,
			SessionID: sessionID,
			At:        now,
			Data:      data,
		}); evErr != nil && !isExpectedRelayDrop(evErr) {
			log.Printf("send run.finished event: %v", evErr)
		}
		c.syncSessionStatus(context.Background(), sessionID, cwd)
	}()

	return map[string]any{"accepted": true}, ""
}

func (c *Client) rpcCancel(ctx context.Context, params map[string]any) (any, string) {
	if c.runtime == nil {
		return nil, "runtime not available"
	}
	sessionID := stringParam(params, "sessionId")
	if sessionID == "" {
		return nil, "missing sessionId"
	}
	if err := c.runtime.Cancel(ctx, sessionID); err != nil {
		return nil, err.Error()
	}
	return map[string]bool{"ok": true}, ""
}

func (c *Client) rpcSetMode(ctx context.Context, params map[string]any) (any, string) {
	if c.runtime == nil {
		return nil, "runtime not available"
	}
	sessionID := stringParam(params, "sessionId")
	if sessionID == "" {
		return nil, "missing sessionId"
	}
	modeID := stringParam(params, "permissionMode")
	if modeID == "" {
		return nil, "missing permissionMode"
	}
	if err := c.runtime.SetMode(ctx, sessionID, modeID); err != nil {
		return nil, err.Error()
	}
	return map[string]bool{"ok": true}, ""
}

func (c *Client) rpcSetConfigOption(ctx context.Context, params map[string]any) (any, string) {
	if c.runtime == nil {
		return nil, "runtime not available"
	}
	sessionID := stringParam(params, "sessionId")
	if sessionID == "" {
		return nil, "missing sessionId"
	}
	configID := stringParam(params, "configId")
	if configID == "" {
		return nil, "missing configId"
	}
	value := stringParam(params, "value")
	if value == "" {
		return nil, "missing value"
	}
	if err := c.runtime.SetConfigOption(ctx, sessionID, configID, value); err != nil {
		return nil, err.Error()
	}
	return map[string]bool{"ok": true}, ""
}

func (c *Client) rpcReplyPermission(ctx context.Context, params map[string]any) (any, string) {
	if c.runtime == nil {
		return nil, "runtime not available"
	}
	sessionID := stringParam(params, "sessionId")
	if sessionID == "" {
		return nil, "missing sessionId"
	}
	requestID := stringParam(params, "requestId")
	if requestID == "" {
		return nil, "missing requestId"
	}
	optionID := stringParam(params, "optionId")
	if optionID == "" {
		return nil, "missing optionId"
	}
	if err := c.runtime.ReplyPermission(ctx, sessionID, requestID, optionID); err != nil {
		return nil, err.Error()
	}
	if err := c.SendEvent(domain.Event{
		Type:      domain.EventApprovalReplied,
		SessionID: sessionID,
		At:        time.Now(),
		Approval:  &domain.ApprovalRequest{ID: requestID, SessionID: sessionID},
	}); err != nil && !isExpectedRelayDrop(err) {
		log.Printf("send approval.replied event: %v", err)
	}
	return map[string]bool{"ok": true}, ""
}

func (c *Client) rpcPendingApprovals(ctx context.Context, params map[string]any) (any, string) {
	if c.runtime == nil {
		return nil, "runtime not available"
	}
	approvals := c.runtime.PendingApprovals()
	return map[string]any{"approvals": approvals}, ""
}

func (c *Client) rpcResyncEvents(ctx context.Context, params map[string]any) (any, string) {
	if c.eventBuf == nil {
		return nil, "event buffer not available"
	}
	var lastSeq uint64
	if v, ok := params["lastSeq"].(float64); ok {
		lastSeq = uint64(v)
	}

	events, complete := c.eventBuf.Since(lastSeq)
	return map[string]any{
		"events":   events,
		"complete": complete,
		"seq":      c.eventBuf.Seq(),
	}, ""
}

// --- Filesystem RPCs ---

type fsEntry struct {
	Name  string `json:"name"`
	Path  string `json:"path"`
	IsDir bool   `json:"isDir"`
}

type fsSearchResult struct {
	Path  string `json:"path"`
	IsDir bool   `json:"isDir"`
}

// safePath resolves relPath under cwd and rejects traversal / symlink escapes.
func safePath(cwd, relPath string) (string, error) {
	if relPath == "" || relPath == "." {
		return filepath.EvalSymlinks(cwd)
	}
	target := filepath.Clean(filepath.Join(cwd, relPath))
	if !strings.HasPrefix(target, cwd+string(filepath.Separator)) && target != cwd {
		return "", fmt.Errorf("path outside project")
	}
	realTarget, err := filepath.EvalSymlinks(target)
	if err != nil {
		return "", err
	}
	realCWD, err := filepath.EvalSymlinks(cwd)
	if err != nil {
		return "", err
	}
	if !strings.HasPrefix(realTarget, realCWD+string(filepath.Separator)) && realTarget != realCWD {
		return "", fmt.Errorf("symlink escape detected")
	}
	return realTarget, nil
}

func (c *Client) rpcFsList(_ context.Context, params map[string]any) (any, string) {
	sessionID := stringParam(params, "sessionId")
	if sessionID == "" {
		return nil, "missing sessionId"
	}
	c.sessionCWDMu.RLock()
	cwd, ok := c.sessionCWD[sessionID]
	c.sessionCWDMu.RUnlock()
	if !ok || cwd == "" {
		return nil, "unknown session"
	}

	relPath := stringParam(params, "path")
	target, err := safePath(cwd, relPath)
	if err != nil {
		if strings.Contains(err.Error(), "path outside project") || strings.Contains(err.Error(), "symlink escape") {
			return nil, err.Error()
		}
		return nil, err.Error()
	}

	dirEntries, err := os.ReadDir(target)
	if err != nil {
		return nil, err.Error()
	}

	sort.Slice(dirEntries, func(i, j int) bool {
		di, dj := dirEntries[i].IsDir(), dirEntries[j].IsDir()
		if di != dj {
			return di
		}
		return dirEntries[i].Name() < dirEntries[j].Name()
	})

	const maxEntries = 200
	entries := make([]fsEntry, 0, min(len(dirEntries), maxEntries))
	// Normalize relPath so entry paths are clean.
	if relPath == "." {
		relPath = ""
	}
	for _, e := range dirEntries {
		if len(entries) >= maxEntries {
			break
		}
		entryPath := e.Name()
		if relPath != "" {
			entryPath = filepath.Join(relPath, e.Name())
		}
		entries = append(entries, fsEntry{
			Name:  e.Name(),
			Path:  entryPath,
			IsDir: e.IsDir(),
		})
	}
	return map[string]any{"entries": entries}, ""
}

func (c *Client) rpcFsSearch(ctx context.Context, params map[string]any) (any, string) {
	sessionID := stringParam(params, "sessionId")
	if sessionID == "" {
		return nil, "missing sessionId"
	}
	query := stringParam(params, "query")
	if query == "" {
		return nil, "missing query"
	}
	c.sessionCWDMu.RLock()
	cwd, ok := c.sessionCWD[sessionID]
	c.sessionCWDMu.RUnlock()
	if !ok || cwd == "" {
		return nil, "unknown session"
	}

	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	const maxResults = 50
	lowerQuery := strings.ToLower(query)
	var results []fsSearchResult

	_ = filepath.WalkDir(cwd, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if d.IsDir() && d.Name() == ".git" {
			return filepath.SkipDir
		}
		if path == cwd {
			return nil
		}
		if strings.Contains(strings.ToLower(d.Name()), lowerQuery) {
			rel, _ := filepath.Rel(cwd, path)
			results = append(results, fsSearchResult{
				Path:  rel,
				IsDir: d.IsDir(),
			})
		}
		return nil
	})

	sort.Slice(results, func(i, j int) bool {
		di, dj := results[i].IsDir, results[j].IsDir
		if di != dj {
			return di
		}
		return len(results[i].Path) < len(results[j].Path)
	})

	if len(results) > maxResults {
		results = results[:maxResults]
	}
	return map[string]any{"results": results}, ""
}

func (c *Client) syncSessionStatus(ctx context.Context, sessionID, cwd string) {
	if c.runtime == nil {
		return
	}
	all, err := c.runtime.ListSessions(ctx, "")
	if err != nil {
		log.Printf("syncSessionStatus: list sessions: %v", err)
		return
	}
	for _, s := range all {
		if s.SessionID == sessionID {
			if err := c.SendEvent(domain.Event{
				Type:      domain.EventSessionStatus,
				SessionID: sessionID,
				At:        s.UpdatedAt,
				Session: &domain.Session{
					ID:        s.SessionID,
					Title:     s.Title,
					Status:    domain.SessionStatusDone,
					CreatedAt: s.UpdatedAt,
					UpdatedAt: s.UpdatedAt,
					Metadata:  map[string]any{"cwd": s.CWD},
				},
			}); err != nil && !isExpectedRelayDrop(err) {
				log.Printf("send session.status event: %v", err)
			}
			return
		}
	}
}

// --- Event forwarding ---

// SendEvent encrypts a domain event and sends it to the connected client via WS.
func (c *Client) SendEvent(event domain.Event) error {
	c.stateMu.RLock()
	session := c.session
	connPresent := c.conn != nil
	c.stateMu.RUnlock()
	if !connPresent {
		return ErrRelayNotConnected
	}
	if session == nil {
		return ErrNoActiveSession
	}

	msgID := uuid.New().String()
	body, err := json.Marshal(event)
	if err != nil {
		return err
	}
	nonce, ciphertext, err := session.encrypt(string(MessageTypeEvent), msgID, body)
	if err != nil {
		return err
	}
	msg := EncryptedMessage{
		Type:       MessageTypeEvent,
		MachineID:  c.machineID,
		MsgID:      msgID,
		Nonce:      nonce,
		Ciphertext: ciphertext,
	}
	switch event.Type {
	case domain.EventApprovalRequested, domain.EventRunFailed, domain.EventRunFinished:
		msg.Hint = &EventHint{Event: string(event.Type)}
	}
	return c.writeForSession(session, msg)
}

// --- Response handling ---

func (c *Client) handleResponse(enc EncryptedMessage) {
	session := c.currentSession()
	if session == nil {
		return
	}
	plaintext, err := session.decrypt(string(enc.Type), enc.MsgID, enc.Nonce, enc.Ciphertext)
	if err != nil {
		return
	}
	var payload map[string]any
	if err := json.Unmarshal(plaintext, &payload); err != nil {
		return
	}
	log.Printf("response from client: %v", payload)
}

func (c *Client) handleSessionEnd(msg SessionEndMessage) {
	if msg.MachineID == "" || msg.MachineID != c.machineID {
		return
	}
	c.stateMu.Lock()
	c.session = nil
	c.stateMu.Unlock()
	log.Printf("session ended by client")
}

// SendEcho sends an echo RPC (kept for backward compatibility).
func (c *Client) SendEcho(params map[string]any) error {
	c.stateMu.RLock()
	session := c.session
	connPresent := c.conn != nil
	c.stateMu.RUnlock()
	if !connPresent {
		return ErrRelayNotConnected
	}
	if session == nil {
		return ErrNoActiveSession
	}
	msgID := uuid.New().String()
	body, err := json.Marshal(RPCPayload{
		Method: "echo",
		Params: params,
	})
	if err != nil {
		return err
	}
	nonce, ciphertext, err := session.encrypt(string(MessageTypeRPC), msgID, body)
	if err != nil {
		return err
	}
	return c.writeForSession(session, EncryptedMessage{
		Type:       MessageTypeRPC,
		MachineID:  c.machineID,
		MsgID:      msgID,
		Nonce:      nonce,
		Ciphertext: ciphertext,
	})
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("random bytes: %w", err)
	}
	return hex.EncodeToString(b)[:n], nil
}

func (c *Client) Close() error {
	oldConn := c.detachActiveConnection()
	if oldConn == nil {
		return nil
	}
	return c.closeConn(oldConn)
}

func (c *Client) currentConnection() (*websocket.Conn, uint64) {
	c.stateMu.RLock()
	defer c.stateMu.RUnlock()
	return c.conn, c.connEpoch
}

func (c *Client) currentSession() *Session {
	c.stateMu.RLock()
	defer c.stateMu.RUnlock()
	return c.session
}

func (c *Client) detachActiveConnection() *websocket.Conn {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	return c.detachActiveConnectionLocked()
}

func (c *Client) detachActiveConnectionLocked() *websocket.Conn {
	oldConn := c.conn
	c.conn = nil
	c.session = nil
	c.connEpoch++
	return oldConn
}

func (c *Client) publishActiveConnection(conn *websocket.Conn) uint64 {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	return c.publishActiveConnectionLocked(conn)
}

func (c *Client) publishActiveConnectionLocked(conn *websocket.Conn) uint64 {
	c.connEpoch++
	c.conn = conn
	c.session = nil
	return c.connEpoch
}

func (c *Client) installSession(session *Session) error {
	if session == nil {
		return ErrNoActiveSession
	}

	c.stateMu.Lock()
	defer c.stateMu.Unlock()

	if c.connEpoch != session.connEpoch {
		return ErrStaleRelaySession
	}
	if c.conn == nil {
		return ErrRelayNotConnected
	}

	c.session = session
	return nil
}

func (c *Client) writeProtocolForEpoch(epoch uint64, v any) error {
	c.stateMu.RLock()
	currentEpoch := c.connEpoch
	conn := c.conn
	c.stateMu.RUnlock()

	if currentEpoch != epoch {
		return ErrStaleRelaySession
	}
	if conn == nil {
		return ErrRelayNotConnected
	}

	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return writeJSONTo(conn, v)
}

func (c *Client) writeForSession(session *Session, v any) error {
	if session == nil {
		return ErrNoActiveSession
	}

	c.stateMu.RLock()
	currentEpoch := c.connEpoch
	conn := c.conn
	currentSession := c.session
	c.stateMu.RUnlock()

	if currentEpoch != session.connEpoch {
		return ErrStaleRelaySession
	}
	if conn == nil {
		return ErrRelayNotConnected
	}
	if currentSession == nil {
		return ErrNoActiveSession
	}
	if currentSession != session {
		return ErrStaleRelaySession
	}

	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return writeJSONTo(conn, v)
}

func (c *Client) closeConn(conn *websocket.Conn) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return conn.Close()
}

func writeJSONTo(conn *websocket.Conn, v any) error {
	return conn.WriteJSON(v)
}

func isExpectedRelayDrop(err error) bool {
	return errors.Is(err, ErrRelayNotConnected) ||
		errors.Is(err, ErrNoActiveSession) ||
		errors.Is(err, ErrStaleRelaySession)
}

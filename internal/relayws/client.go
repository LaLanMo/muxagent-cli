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

	"github.com/LaLanMo/muxagent-cli/internal/acpprotocol"
	"github.com/LaLanMo/muxagent-cli/internal/appwire"
	"github.com/LaLanMo/muxagent-cli/internal/appwireconv"
	"github.com/LaLanMo/muxagent-cli/internal/auth"
	"github.com/LaLanMo/muxagent-cli/internal/crypto"
	"github.com/LaLanMo/muxagent-cli/internal/domain"
	"github.com/LaLanMo/muxagent-cli/internal/keyring"
	runtimemanager "github.com/LaLanMo/muxagent-cli/internal/runtime/manager"
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
	RuntimeList() []runtimemanager.RuntimeInfo
	NewSession(ctx context.Context, runtimeID, cwd, permissionMode string) (string, string, acpprotocol.NewSessionResponse, error)
	LoadSession(ctx context.Context, runtimeID, sessionID, cwd, permissionMode, model string) (string, acpprotocol.LoadSessionResponse, error)
	ResolveSessions(ctx context.Context, runtimeID string, sessionIDs []string) ([]domain.SessionSummary, error)
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

	statusMu      sync.RWMutex
	sessionStatus map[string]domain.SessionStatus // sessionID → daemon-tracked status
}

func NewMachineClient(
	relayURL, hostname string,
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
		creds:           creds,
		machineSignPriv: machineSignPriv,
		keyring:         keyringMgr,
		runtime:         rt,
		eventBuf:        eventBuf,
		wtStore:         wtStore,
		sessionCWD:      make(map[string]string),
		sessionStatus:   make(map[string]domain.SessionStatus),
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

	session := newSession(c.machineID, key, connEpoch)
	if err := c.installSession(session); err != nil {
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
		c.clearSession(session)
		return err
	}
	return nil
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
	var payload appwire.RPCRequest
	if err := json.Unmarshal(plaintext, &payload); err != nil {
		return
	}

	ctx := context.Background()
	var result any
	var respErr string

	switch payload.Method {
	case "runtime.list":
		result, respErr = c.rpcRuntimeList(ctx)
	case "session.create":
		params, err := appwire.DecodeCreateSessionParams(payload.Params)
		if err != nil {
			respErr = "invalid create params: " + err.Error()
			break
		}
		result, respErr = c.rpcCreateSession(ctx, params)
	case "session.load":
		params, err := appwire.DecodeLoadSessionParams(payload.Params)
		if err != nil {
			respErr = "invalid load params: " + err.Error()
			break
		}
		result, respErr = c.rpcLoadSession(ctx, params)
	case "session.resolve":
		params, err := appwire.DecodeResolveSessionsParams(payload.Params)
		if err != nil {
			respErr = "invalid resolve params: " + err.Error()
			break
		}
		result, respErr = c.rpcResolveSessions(ctx, params)
	case "session.prompt":
		params, err := appwire.DecodePromptParams(payload.Params)
		if err != nil {
			respErr = "invalid prompt params: " + err.Error()
			break
		}
		result, respErr = c.rpcPrompt(ctx, params)
	case "session.cancel":
		params, err := appwire.DecodeCancelParams(payload.Params)
		if err != nil {
			respErr = "invalid cancel params: " + err.Error()
			break
		}
		result, respErr = c.rpcCancel(ctx, params)
	case "session.setMode":
		params, err := appwire.DecodeSetModeParams(payload.Params)
		if err != nil {
			respErr = "invalid setMode params: " + err.Error()
			break
		}
		result, respErr = c.rpcSetMode(ctx, params)
	case "session.setConfigOption":
		params, err := appwire.DecodeSetConfigOptionParams(payload.Params)
		if err != nil {
			respErr = "invalid setConfigOption params: " + err.Error()
			break
		}
		result, respErr = c.rpcSetConfigOption(ctx, params)
	case "approval.reply":
		params, err := appwire.DecodeReplyPermissionParams(payload.Params)
		if err != nil {
			respErr = "invalid replyPermission params: " + err.Error()
			break
		}
		result, respErr = c.rpcReplyPermission(ctx, params)
	case "events.resync":
		params, err := appwire.DecodeResyncEventsParams(payload.Params)
		if err != nil {
			respErr = "invalid resync params: " + err.Error()
			break
		}
		result, respErr = c.rpcResyncEvents(ctx, params)
	case "approvals.pending":
		result, respErr = c.rpcPendingApprovals(ctx)
	case "fs.list":
		params, err := appwire.DecodeFsListParams(payload.Params)
		if err != nil {
			respErr = "invalid fs.list params: " + err.Error()
			break
		}
		result, respErr = c.rpcFsList(ctx, params)
	case "fs.search":
		params, err := appwire.DecodeFsSearchParams(payload.Params)
		if err != nil {
			respErr = "invalid fs.search params: " + err.Error()
			break
		}
		result, respErr = c.rpcFsSearch(ctx, params)
	case "echo":
		params, err := appwire.DecodeEchoParams(payload.Params)
		if err != nil {
			respErr = "invalid echo params: " + err.Error()
			break
		}
		log.Printf("echo request from client: %v", params)
		result = params
	default:
		respErr = fmt.Sprintf("unknown method: %s", payload.Method)
	}

	respBytes, err := appwire.MarshalRPCResponse(result, respErr)
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

func (c *Client) rpcRuntimeList(_ context.Context) (any, string) {
	if c.runtime == nil {
		return nil, "runtime not available"
	}
	runtimes := c.runtime.RuntimeList()
	items := make([]appwire.RuntimeInfo, 0, len(runtimes))
	for _, runtime := range runtimes {
		items = append(items, appwire.RuntimeInfo{
			ID:            runtime.ID,
			Label:         runtime.Label,
			Ready:         runtime.Ready,
			ConfigOptions: runtime.ConfigOptions,
		})
	}
	return appwire.RuntimeListResult{Runtimes: items}, ""
}

func (c *Client) rpcCreateSession(ctx context.Context, params appwire.CreateSessionParams) (any, string) {
	if c.runtime == nil {
		return nil, "runtime not available"
	}
	cwd := params.CWD
	if cwd == "" {
		return nil, "missing cwd"
	}
	// Expand ~ before any path operations (worktree, git, etc.)
	if strings.HasPrefix(cwd, "~/") || cwd == "~" {
		if home, err := os.UserHomeDir(); err == nil {
			cwd = filepath.Join(home, cwd[1:])
		}
	}
	permissionMode := params.PermissionMode
	requestedRuntime := c.resolveRequestedRuntime(params.Runtime)
	if requestedRuntime == "" {
		return nil, "missing runtime"
	}
	useWorktree := params.UseWorktree

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

	sessionID, actualRuntime, acpResp, err := c.runtime.NewSession(ctx, requestedRuntime, actualCWD, permissionMode)
	if err != nil {
		return nil, err.Error()
	}
	c.setSessionStatus(sessionID, domain.SessionStatusIdle)
	c.sessionCWDMu.Lock()
	c.sessionCWD[sessionID] = actualCWD
	c.sessionCWDMu.Unlock()

	if wtMapping != nil && c.wtStore != nil {
		c.wtStore.Set(sessionID, *wtMapping)
		if err := c.wtStore.Save(); err != nil {
			log.Printf("worktree store save: %v", err)
		}
	}

	resp := appwire.SessionCreateResult{
		App: appwire.SessionCreateResultApp{
			Runtime: actualRuntime,
			CWD:     actualCWD,
		},
		ACP: acpResp,
	}
	if len(acpResp.ConfigOptions) > 0 {
		log.Printf("[relay] session.create response includes %d configOptions", len(acpResp.ConfigOptions))
	} else {
		log.Printf("[relay] session.create response has NO configOptions")
	}
	return resp, ""
}

func (c *Client) rpcLoadSession(ctx context.Context, params appwire.LoadSessionParams) (any, string) {
	if c.runtime == nil {
		return nil, "runtime not available"
	}
	sessionID := params.SessionID
	if sessionID == "" {
		return nil, "missing sessionId"
	}
	cwd := params.CWD
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

	permissionMode := params.PermissionMode
	model := params.Model
	requestedRuntime := c.resolveRequestedRuntime(params.Runtime)
	if requestedRuntime == "" {
		return nil, "missing runtime"
	}
	actualRuntime, acpResp, err := c.runtime.LoadSession(ctx, requestedRuntime, sessionID, cwd, permissionMode, model)
	if err != nil {
		return nil, err.Error()
	}
	c.ensureSessionStatus(sessionID, domain.SessionStatusIdle)
	c.sessionCWDMu.Lock()
	c.sessionCWD[sessionID] = cwd
	c.sessionCWDMu.Unlock()
	resp := appwire.SessionLoadResult{
		App: appwire.SessionLoadResultApp{
			OK:      true,
			Runtime: actualRuntime,
		},
		ACP: acpResp,
	}
	return resp, ""
}

func (c *Client) rpcResolveSessions(ctx context.Context, params appwire.ResolveSessionsParams) (any, string) {
	if c.runtime == nil {
		return nil, "runtime not available"
	}
	wanted := make(map[string]struct{}, len(params.SessionIDs))
	for _, id := range params.SessionIDs {
		if id != "" {
			wanted[id] = struct{}{}
		}
	}
	runtimeID := params.Runtime
	targetIDs := make([]string, 0, len(wanted))
	for id := range wanted {
		targetIDs = append(targetIDs, id)
	}
	all, err := c.runtime.ResolveSessions(ctx, runtimeID, targetIDs)
	if err != nil {
		return nil, err.Error()
	}
	present := make(map[string]struct{}, len(all))
	for _, s := range all {
		present[s.SessionID] = struct{}{}
	}
	if len(wanted) > 0 {
		for id := range wanted {
			if _, ok := present[id]; !ok {
				c.clearSessionStatus(id)
			}
		}
	} else {
		c.clearMissingSessionStatuses(present)
	}
	// If caller provided specific IDs, filter to those only.
	if len(wanted) > 0 {
		filtered := make([]appwire.ResolvedSession, 0, len(wanted))
		for _, s := range all {
			if _, ok := wanted[s.SessionID]; ok {
				filtered = append(filtered, appwire.ResolvedSession{
					SessionID: s.SessionID,
					CWD:       s.CWD,
					Title:     s.Title,
					UpdatedAt: s.UpdatedAt,
					Status:    sessionStatusToWire(c.resolvedSessionStatus(s.SessionID)),
				})
			}
		}
		return appwire.SessionResolveResult{Sessions: filtered}, ""
	}
	resolved := make([]appwire.ResolvedSession, 0, len(all))
	for _, s := range all {
		resolved = append(resolved, appwire.ResolvedSession{
			SessionID: s.SessionID,
			CWD:       s.CWD,
			Title:     s.Title,
			UpdatedAt: s.UpdatedAt,
			Status:    sessionStatusToWire(c.resolvedSessionStatus(s.SessionID)),
		})
	}
	return appwire.SessionResolveResult{Sessions: resolved}, ""
}

func (c *Client) resolveRequestedRuntime(runtimeID string) string {
	runtimeID = strings.TrimSpace(runtimeID)
	if runtimeID != "" || c.runtime == nil {
		return runtimeID
	}

	runtimes := c.runtime.RuntimeList()
	for _, runtimeInfo := range runtimes {
		id := strings.TrimSpace(runtimeInfo.ID)
		if id == "claude-code" {
			return id
		}
	}

	return ""
}

func (c *Client) rpcPrompt(ctx context.Context, params appwire.PromptParams) (any, string) {
	if c.runtime == nil {
		return nil, "runtime not available"
	}
	sessionID := params.SessionID
	if sessionID == "" {
		return nil, "missing sessionId"
	}
	content := appwireconv.ContentBlocksFromWire(params.Content)

	// If no content blocks but there's a text field, create a text block
	if len(content) == 0 {
		if params.Text != "" {
			content = []domain.ContentBlock{{Type: "text", Text: params.Text}}
		}
	}

	c.setSessionStatus(sessionID, domain.SessionStatusRunning)

	// Run the prompt asynchronously — return ACK immediately so the
	// Flutter client's RPC timeout doesn't fire.  Use context.Background()
	// because the handleRPC ctx lifetime ends when we return.
	go func() {
		stopReason, usage, err := c.runtime.Prompt(context.Background(), sessionID, content)
		now := time.Now()
		if err != nil {
			if evErr := c.SendEvent(appwire.Event{
				Type:      appwire.EventRunFailed,
				SessionID: sessionID,
				At:        now,
				RunFailed: &appwire.RunFailedEvent{
					App: appwire.RunFailedEventApp{
						Error: appwire.SessionError{
							Code:    "prompt_error",
							Message: err.Error(),
						},
					},
				},
			}); evErr != nil && !isExpectedRelayDrop(evErr) {
				log.Printf("send run.failed event: %v", evErr)
			}
			return
		}
		runFinished := appwire.RunFinishedEventApp{StopReason: stopReason}
		if usage != nil {
			runFinished.TotalTokens = usage.TotalTokens
			runFinished.InputTokens = usage.InputTokens
			runFinished.OutputTokens = usage.OutputTokens
			runFinished.CachedReadTokens = usage.CachedReadTokens
			runFinished.CachedWriteTokens = usage.CachedWriteTokens
		}
		if evErr := c.SendEvent(appwire.Event{
			Type:      appwire.EventRunFinished,
			SessionID: sessionID,
			At:        now,
			RunFinished: &appwire.RunFinishedEvent{
				App: runFinished,
			},
		}); evErr != nil && !isExpectedRelayDrop(evErr) {
			log.Printf("send run.finished event: %v", evErr)
		}
	}()

	return appwire.AcceptedResult{Accepted: true}, ""
}

func (c *Client) rpcCancel(ctx context.Context, params appwire.CancelParams) (any, string) {
	if c.runtime == nil {
		return nil, "runtime not available"
	}
	if params.SessionID == "" {
		return nil, "missing sessionId"
	}
	if err := c.runtime.Cancel(ctx, params.SessionID); err != nil {
		return nil, err.Error()
	}
	return appwire.OKResult{OK: true}, ""
}

func (c *Client) rpcSetMode(ctx context.Context, params appwire.SetModeParams) (any, string) {
	if c.runtime == nil {
		return nil, "runtime not available"
	}
	if params.SessionID == "" {
		return nil, "missing sessionId"
	}
	if params.PermissionMode == "" {
		return nil, "missing permissionMode"
	}
	if err := c.runtime.SetMode(ctx, params.SessionID, params.PermissionMode); err != nil {
		return nil, err.Error()
	}
	return appwire.OKResult{OK: true}, ""
}

func (c *Client) rpcSetConfigOption(ctx context.Context, params appwire.SetConfigOptionParams) (any, string) {
	if c.runtime == nil {
		return nil, "runtime not available"
	}
	if params.SessionID == "" {
		return nil, "missing sessionId"
	}
	if params.ConfigID == "" {
		return nil, "missing configId"
	}
	if params.Value == "" {
		return nil, "missing value"
	}
	if err := c.runtime.SetConfigOption(ctx, params.SessionID, params.ConfigID, params.Value); err != nil {
		return nil, err.Error()
	}
	return appwire.OKResult{OK: true}, ""
}

func (c *Client) rpcReplyPermission(ctx context.Context, params appwire.ReplyPermissionParams) (any, string) {
	if c.runtime == nil {
		return nil, "runtime not available"
	}
	if params.SessionID == "" {
		return nil, "missing sessionId"
	}
	if params.RequestID == "" {
		return nil, "missing requestId"
	}
	if params.OptionID == "" {
		return nil, "missing optionId"
	}
	if err := c.runtime.ReplyPermission(ctx, params.SessionID, params.RequestID, params.OptionID); err != nil {
		return nil, err.Error()
	}
	if err := c.SendEvent(appwire.Event{
		Type:      appwire.EventApprovalReplied,
		SessionID: params.SessionID,
		At:        time.Now(),
		Approval: &appwire.ApprovalRequest{
			App: appwire.ApprovalApp{RequestID: params.RequestID},
		},
	}); err != nil && !isExpectedRelayDrop(err) {
		log.Printf("send approval.replied event: %v", err)
	}
	return appwire.OKResult{OK: true}, ""
}

func (c *Client) rpcPendingApprovals(ctx context.Context) (any, string) {
	if c.runtime == nil {
		return nil, "runtime not available"
	}
	approvals := c.runtime.PendingApprovals()
	return appwire.PendingApprovalsResult{
		Approvals: appwireconv.ApprovalRequestsFromDomain(approvals),
	}, ""
}

func (c *Client) rpcResyncEvents(ctx context.Context, params appwire.ResyncEventsParams) (any, string) {
	if c.eventBuf == nil {
		return nil, "event buffer not available"
	}
	events, complete := c.eventBuf.Since(params.LastSeq)
	return appwire.ResyncEventsResult{
		Events:   events,
		Complete: complete,
		Seq:      c.eventBuf.Seq(),
	}, ""
}

// --- Filesystem RPCs ---

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

func (c *Client) rpcFsList(_ context.Context, params appwire.FsListParams) (any, string) {
	if params.SessionID == "" {
		return nil, "missing sessionId"
	}
	c.sessionCWDMu.RLock()
	cwd, ok := c.sessionCWD[params.SessionID]
	c.sessionCWDMu.RUnlock()
	if !ok || cwd == "" {
		return nil, "unknown session"
	}

	relPath := params.Path
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
	entries := make([]appwire.FsEntry, 0, min(len(dirEntries), maxEntries))
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
		entries = append(entries, appwire.FsEntry{
			Name:  e.Name(),
			Path:  entryPath,
			IsDir: e.IsDir(),
		})
	}
	return appwire.FsListResult{Entries: entries}, ""
}

func (c *Client) rpcFsSearch(ctx context.Context, params appwire.FsSearchParams) (any, string) {
	if params.SessionID == "" {
		return nil, "missing sessionId"
	}
	if params.Query == "" {
		return nil, "missing query"
	}
	c.sessionCWDMu.RLock()
	cwd, ok := c.sessionCWD[params.SessionID]
	c.sessionCWDMu.RUnlock()
	if !ok || cwd == "" {
		return nil, "unknown session"
	}

	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	const maxResults = 50
	lowerQuery := strings.ToLower(params.Query)
	var results []appwire.FsSearchEntry

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
			results = append(results, appwire.FsSearchEntry{
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
	return appwire.FsSearchResult{Results: results}, ""
}

// --- Event forwarding ---

// SendEvent encrypts an app transport event and sends it to the connected client via WS.
func (c *Client) SendEvent(event appwire.Event) error {
	c.applyEventStatus(event)
	if c.eventBuf != nil {
		event = c.eventBuf.Push(event)
	}

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
	body, err := marshalEvent(event)
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
	case appwire.EventApprovalRequested, appwire.EventRunFailed, appwire.EventRunFinished:
		msg.Hint = &EventHint{Event: string(event.Type)}
	}
	return c.writeForSession(session, msg)
}

func (c *Client) applyEventStatus(event appwire.Event) {
	if event.SessionID == "" {
		return
	}
	switch event.Type {
	case appwire.EventApprovalRequested:
		c.setSessionStatus(event.SessionID, domain.SessionStatusWaitingApproval)
	case appwire.EventApprovalReplied:
		c.setSessionStatus(event.SessionID, domain.SessionStatusRunning)
	case appwire.EventRunFinished:
		c.setSessionStatus(event.SessionID, domain.SessionStatusIdle)
	case appwire.EventRunFailed:
		c.setSessionStatus(event.SessionID, domain.SessionStatusError)
	case appwire.EventSessionStatus:
		if event.SessionInfo != nil {
			c.setSessionStatus(event.SessionID, sessionStatusFromWire(event.SessionInfo.App.Status))
		}
	}
}

func (c *Client) setSessionStatus(sessionID string, status domain.SessionStatus) {
	if sessionID == "" {
		return
	}
	c.statusMu.Lock()
	defer c.statusMu.Unlock()
	if c.sessionStatus == nil {
		c.sessionStatus = make(map[string]domain.SessionStatus)
	}
	c.sessionStatus[sessionID] = status
}

func (c *Client) ensureSessionStatus(sessionID string, status domain.SessionStatus) {
	if sessionID == "" {
		return
	}
	c.statusMu.Lock()
	defer c.statusMu.Unlock()
	if c.sessionStatus == nil {
		c.sessionStatus = make(map[string]domain.SessionStatus)
	}
	if _, ok := c.sessionStatus[sessionID]; ok {
		return
	}
	c.sessionStatus[sessionID] = status
}

func (c *Client) resolvedSessionStatus(sessionID string) domain.SessionStatus {
	c.statusMu.RLock()
	defer c.statusMu.RUnlock()
	if status, ok := c.sessionStatus[sessionID]; ok {
		return status
	}
	return domain.SessionStatusIdle
}

func (c *Client) clearSessionStatus(sessionID string) {
	if sessionID == "" {
		return
	}
	c.statusMu.Lock()
	defer c.statusMu.Unlock()
	delete(c.sessionStatus, sessionID)
}

func (c *Client) clearMissingSessionStatuses(present map[string]struct{}) {
	c.statusMu.Lock()
	defer c.statusMu.Unlock()
	for sessionID := range c.sessionStatus {
		if _, ok := present[sessionID]; !ok {
			delete(c.sessionStatus, sessionID)
		}
	}
}

func sessionStatusToWire(status domain.SessionStatus) appwire.SessionStatus {
	return appwire.SessionStatus(status)
}

func sessionStatusFromWire(status appwire.SessionStatus) domain.SessionStatus {
	return domain.SessionStatus(status)
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
	body, err := appwire.MarshalRPCRequest("echo", params)
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

func (c *Client) clearSession(session *Session) {
	if session == nil {
		return
	}

	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	if c.session == session {
		c.session = nil
	}
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

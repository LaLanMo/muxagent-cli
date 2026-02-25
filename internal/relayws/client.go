package relayws

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/LaLanMo/muxagent-cli/internal/auth"
	"github.com/LaLanMo/muxagent-cli/internal/crypto"
	"github.com/LaLanMo/muxagent-cli/internal/domain"
	"github.com/LaLanMo/muxagent-cli/internal/keyring"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"golang.org/x/crypto/curve25519"
)

// RuntimeClient is the subset of runtime.Client that the relay needs.
// Defined here to avoid a circular import with the runtime package.
type RuntimeClient interface {
	NewSession(ctx context.Context, cwd string) (string, error)
	LoadSession(ctx context.Context, sessionID, cwd string) error
	ListSessions(ctx context.Context) ([]domain.SessionSummary, error)
	Prompt(ctx context.Context, sessionID string, content []domain.ContentBlock) (string, error)
	Cancel(ctx context.Context, sessionID string) error
	ReplyPermission(ctx context.Context, sessionID, requestID, optionID string) error
}

type Client struct {
	relayURL  string
	machineID string
	hostname  string

	creds           *auth.Credentials
	machineSignPriv ed25519.PrivateKey
	keyring         *keyring.Manager

	conn    *websocket.Conn
	writeMu sync.Mutex

	sessionMu sync.RWMutex
	session   *Session

	runtime  RuntimeClient
	eventBuf *EventBuffer
}

func NewMachineClient(
	relayURL, hostname string,
	creds *auth.Credentials,
	machineSignPriv ed25519.PrivateKey,
	keyringMgr *keyring.Manager,
	rt RuntimeClient,
	eventBuf *EventBuffer,
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
	}, nil
}

func (c *Client) HasSession() bool {
	c.sessionMu.RLock()
	defer c.sessionMu.RUnlock()
	return c.session != nil
}

func (c *Client) Connect(ctx context.Context) error {
	conn, _, err := websocket.DefaultDialer.Dial(c.relayURL, nil)
	if err != nil {
		return fmt.Errorf("dial relay: %w", err)
	}
	c.conn = conn

	if err := c.writeJSON(RegisterMessage{
		Type:      MessageTypeRegister,
		Role:      RoleMachine,
		MachineID: c.machineID,
		Hostname:  c.hostname,
	}); err != nil {
		conn.Close()
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
				conn.Close()
				return fmt.Errorf("invalid challenge from relay")
			}
			signedMessage := crypto.BuildMachineAuthMessage(c.machineID, msg.Nonce)
			signature := crypto.SignBase64(signedMessage, c.machineSignPriv)
			if err := c.writeJSON(ChallengeResponseMessage{
				Type:      MessageTypeChallengeResponse,
				Signature: signature,
			}); err != nil {
				conn.Close()
				return fmt.Errorf("send challenge response: %w", err)
			}
		case MessageTypeRegistered:
			return nil
		case MessageTypeError:
			var msg ErrorMessage
			if err := json.Unmarshal(raw, &msg); err != nil {
				conn.Close()
				return fmt.Errorf("registration failed: unknown error")
			}
			conn.Close()
			return fmt.Errorf("registration failed: %s", msg.Error)
		}
	}
}

func (c *Client) Run(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		var raw json.RawMessage
		if err := c.conn.ReadJSON(&raw); err != nil {
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
			if err := c.handleSessionInit(msg); err != nil {
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
			c.handleRPC(enc)
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

func (c *Client) handleSessionInit(msg SessionInitMessage) error {
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

	c.sessionMu.Lock()
	c.session = newSession(c.machineID, key)
	c.sessionMu.Unlock()

	ackMsg := crypto.BuildSessionAckMessage(c.machineID, machineEphemeralPubB64)
	ackSig := crypto.SignBase64(ackMsg, c.machineSignPriv)
	if err := c.writeJSON(SessionAckMessage{
		Type:                MessageTypeSessionAck,
		MachineID:           c.machineID,
		MachineEphemeralPub: machineEphemeralPubB64,
		Signature:           ackSig,
	}); err != nil {
		return err
	}

	return nil
}

// --- RPC routing ---

func (c *Client) handleRPC(enc EncryptedMessage) {
	session := c.currentSession()
	if session == nil {
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
	case "session.list":
		result, respErr = c.rpcListSessions(ctx)
	case "session.prompt":
		result, respErr = c.rpcPrompt(ctx, payload.Params)
	case "session.cancel":
		result, respErr = c.rpcCancel(ctx, payload.Params)
	case "approval.reply":
		result, respErr = c.rpcReplyPermission(ctx, payload.Params)
	case "events.resync":
		result, respErr = c.rpcResyncEvents(ctx, payload.Params)
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
	respBytes, _ := json.Marshal(respPayload)
	nonce, ciphertext, err := session.encrypt(string(MessageTypeResponse), enc.MsgID, respBytes)
	if err != nil {
		return
	}
	_ = c.writeJSON(EncryptedMessage{
		Type:       MessageTypeResponse,
		MachineID:  c.machineID,
		MsgID:      enc.MsgID,
		Nonce:      nonce,
		Ciphertext: ciphertext,
	})
}

func (c *Client) rpcCreateSession(ctx context.Context, params map[string]any) (any, string) {
	if c.runtime == nil {
		return nil, "runtime not available"
	}
	cwd, _ := params["cwd"].(string)
	if cwd == "" {
		return nil, "missing cwd"
	}
	sessionID, err := c.runtime.NewSession(ctx, cwd)
	if err != nil {
		return nil, err.Error()
	}
	return map[string]string{"sessionId": sessionID}, ""
}

func (c *Client) rpcLoadSession(ctx context.Context, params map[string]any) (any, string) {
	if c.runtime == nil {
		return nil, "runtime not available"
	}
	sessionID, _ := params["sessionId"].(string)
	if sessionID == "" {
		return nil, "missing sessionId"
	}
	cwd, _ := params["cwd"].(string)
	if cwd == "" {
		return nil, "missing cwd"
	}
	if err := c.runtime.LoadSession(ctx, sessionID, cwd); err != nil {
		return nil, err.Error()
	}
	return map[string]bool{"ok": true}, ""
}

func (c *Client) rpcListSessions(ctx context.Context) (any, string) {
	if c.runtime == nil {
		return nil, "runtime not available"
	}
	sessions, err := c.runtime.ListSessions(ctx)
	if err != nil {
		return nil, err.Error()
	}

	result := make([]map[string]any, 0, len(sessions))
	for _, session := range sessions {
		result = append(result, map[string]any{
			"sessionId": session.SessionID,
			"cwd":       session.CWD,
			"title":     session.Title,
			"updatedAt": session.UpdatedAt.Format(time.RFC3339Nano),
		})
	}
	return map[string]any{"sessions": result}, ""
}

func (c *Client) rpcPrompt(ctx context.Context, params map[string]any) (any, string) {
	if c.runtime == nil {
		return nil, "runtime not available"
	}
	sessionID, _ := params["sessionId"].(string)
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

	stopReason, err := c.runtime.Prompt(ctx, sessionID, content)
	now := time.Now()
	if err != nil {
		_ = c.SendEvent(domain.Event{
			Type:      domain.EventRunFailed,
			SessionID: sessionID,
			At:        now,
			Error:     &domain.SessionError{Code: "prompt_error", Message: err.Error()},
		})
		return nil, err.Error()
	}
	_ = c.SendEvent(domain.Event{
		Type:      domain.EventRunFinished,
		SessionID: sessionID,
		At:        now,
		Data:      map[string]any{"stopReason": stopReason},
	})
	return map[string]string{"stopReason": stopReason}, ""
}

func (c *Client) rpcCancel(ctx context.Context, params map[string]any) (any, string) {
	if c.runtime == nil {
		return nil, "runtime not available"
	}
	sessionID, _ := params["sessionId"].(string)
	if sessionID == "" {
		return nil, "missing sessionId"
	}
	if err := c.runtime.Cancel(ctx, sessionID); err != nil {
		return nil, err.Error()
	}
	return map[string]bool{"ok": true}, ""
}

func (c *Client) rpcReplyPermission(ctx context.Context, params map[string]any) (any, string) {
	if c.runtime == nil {
		return nil, "runtime not available"
	}
	sessionID, _ := params["sessionId"].(string)
	if sessionID == "" {
		return nil, "missing sessionId"
	}
	requestID, _ := params["requestId"].(string)
	if requestID == "" {
		return nil, "missing requestId"
	}
	optionID, _ := params["optionId"].(string)
	if optionID == "" {
		return nil, "missing optionId"
	}
	if err := c.runtime.ReplyPermission(ctx, sessionID, requestID, optionID); err != nil {
		return nil, err.Error()
	}
	return map[string]bool{"ok": true}, ""
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

// --- Event forwarding ---

// SendEvent encrypts a domain event and sends it to the connected client via WS.
func (c *Client) SendEvent(event domain.Event) error {
	session := c.currentSession()
	if session == nil {
		return fmt.Errorf("no active session")
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
	return c.writeJSON(EncryptedMessage{
		Type:       MessageTypeEvent,
		MachineID:  c.machineID,
		MsgID:      msgID,
		Nonce:      nonce,
		Ciphertext: ciphertext,
	})
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
	c.sessionMu.Lock()
	c.session = nil
	c.sessionMu.Unlock()
	log.Printf("session ended by client")
}

// SendEcho sends an echo RPC (kept for backward compatibility).
func (c *Client) SendEcho(params map[string]any) error {
	session := c.currentSession()
	if session == nil {
		return fmt.Errorf("no active session")
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
	return c.writeJSON(EncryptedMessage{
		Type:       MessageTypeRPC,
		MachineID:  c.machineID,
		MsgID:      msgID,
		Nonce:      nonce,
		Ciphertext: ciphertext,
	})
}

func (c *Client) Close() error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

func (c *Client) writeJSON(v any) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return c.conn.WriteJSON(v)
}

func (c *Client) currentSession() *Session {
	c.sessionMu.RLock()
	defer c.sessionMu.RUnlock()
	return c.session
}

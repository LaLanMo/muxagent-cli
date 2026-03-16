package acp

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/LaLanMo/muxagent-cli/internal/acpprotocol"
	"github.com/LaLanMo/muxagent-cli/internal/appwire"
	"github.com/LaLanMo/muxagent-cli/internal/appwireconv"
	"github.com/LaLanMo/muxagent-cli/internal/domain"
	"github.com/google/uuid"
)

// Config holds the configuration for an ACP client.
type Config struct {
	RuntimeID string
	Command   string
	Args      []string
	CWD       string
	Env       map[string]string
}

// sessionMsgState tracks streamed message IDs for a session when ACP updates
// do not include stable message/part IDs.
type sessionMsgState struct {
	agentMsgID  string
	agentPartID string
	userMsgID   string
	userPartID  string
}

// Client implements runtime.Client over ACP (JSON-RPC 2.0 over stdio).
type Client struct {
	cfg       Config
	transport *Transport
	events    chan appwire.Event

	permMu      sync.Mutex
	pendingPerm map[string]*pendingPermission // requestID (string) → pending

	msgMu      sync.Mutex
	sessionMsg map[string]*sessionMsgState // sessionID → current streaming state
}

type pendingPermission struct {
	rpcID   int64 // the JSON-RPC request ID from agent
	request domain.ApprovalRequest
}

// NewClient creates a new ACP client with the given configuration.
func NewClient(cfg Config) *Client {
	return &Client{
		cfg:         cfg,
		events:      make(chan appwire.Event, 256),
		pendingPerm: make(map[string]*pendingPermission),
		sessionMsg:  make(map[string]*sessionMsgState),
	}
}

// Start spawns the agent process and performs ACP initialization.
func (c *Client) Start(ctx context.Context) error {
	c.transport = NewTransport(c.cfg.Command, c.cfg.Args, c.cfg.CWD, c.cfg.Env)
	if err := c.transport.Start(ctx); err != nil {
		return fmt.Errorf("acp start: %w", err)
	}

	// ACP initialize handshake
	initParams := map[string]any{
		"protocolVersion": 1,
		"clientCapabilities": map[string]any{
			"fs": map[string]any{
				"readTextFile":  false,
				"writeTextFile": false,
			},
		},
		"clientInfo": map[string]any{
			"name":    "muxagent",
			"version": "1.0.0",
		},
	}
	result, err := c.transport.Call(ctx, "initialize", initParams)
	if err != nil {
		_ = c.transport.Stop()
		return fmt.Errorf("acp initialize: %w", err)
	}
	log.Printf("[acp] initialized: %s", string(result))

	// Start notification and request handlers
	go c.handleNotifications()
	go c.handleRequests()

	return nil
}

// Stop terminates the agent process.
func (c *Client) Stop() error {
	if c.transport != nil {
		return c.transport.Stop()
	}
	return nil
}

// NewSession creates a new ACP session. If permissionMode is non-empty and
// differs from "default", the mode is applied via the standard ACP
// session/set_mode RPC immediately after creation.
func (c *Client) NewSession(ctx context.Context, cwd string, permissionMode string) (acpprotocol.NewSessionResponse, error) {
	resolved, err := expandAndValidateCWD(cwd)
	if err != nil {
		return acpprotocol.NewSessionResponse{}, err
	}
	params := map[string]any{
		"cwd":        resolved,
		"mcpServers": []any{},
		"_meta": map[string]any{
			"claudeCode": map[string]any{
				"options": map[string]any{
					"env": map[string]any{
						"CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS": "0",
					},
				},
			},
		},
	}
	result, err := c.transport.Call(ctx, "session/new", params)
	if err != nil {
		return acpprotocol.NewSessionResponse{}, fmt.Errorf("session/new: %w", err)
	}
	log.Printf("[acp] session/new raw result: %s", string(result))

	var resp acpprotocol.NewSessionResponse
	if err := json.Unmarshal(result, &resp); err != nil {
		return acpprotocol.NewSessionResponse{}, fmt.Errorf("parse session/new result: %w", err)
	}

	// Apply permission mode if requested and different from default.
	modeApplied := false
	if domain.IsNonDefaultMode(permissionMode) {
		_, err := c.transport.Call(ctx, "session/set_mode", map[string]any{
			"sessionId": resp.SessionID,
			"modeId":    permissionMode,
		})
		if err != nil {
			log.Printf("[acp] failed to set permission mode %q: %v", permissionMode, err)
		} else {
			modeApplied = true
		}
	}
	if modeApplied {
		setConfigOptionCurrentValue(resp.ConfigOptions, "mode", permissionMode)
	}

	if ev := modeEvent(resp.SessionID, resolveCurrentModeID(permissionMode, modeApplied, resp.Modes, resp.ConfigOptions), nil); ev != nil {
		c.emit(*ev)
	}

	// Emit initial model info from configOptions.
	if ev := configOptionEvent(resp.SessionID, resp.ConfigOptions, "model", appwire.EventModelChanged, nil); ev != nil {
		c.emit(*ev)
	}

	log.Printf("[acp] NewSession configOptions: %d items", len(resp.ConfigOptions))
	for _, opt := range resp.ConfigOptions {
		log.Printf(
			"[acp]   option: id=%s category=%s currentValue=%s options=%d",
			opt.ID,
			configOptionCategory(opt),
			opt.CurrentValue,
			len(opt.Options.Flatten()),
		)
	}
	return resp, nil
}

// LoadSession loads an existing session. History is replayed via session/update notifications.
// If permissionMode is non-default, it calls session/set_mode after loading.
func (c *Client) LoadSession(ctx context.Context, sessionID, cwd, permissionMode, model string) (acpprotocol.LoadSessionResponse, error) {
	resolved, err := expandAndValidateCWD(cwd)
	if err != nil {
		return acpprotocol.LoadSessionResponse{}, err
	}

	c.msgMu.Lock()
	c.sessionMsg[sessionID] = &sessionMsgState{}
	c.msgMu.Unlock()

	params := map[string]any{
		"sessionId":  sessionID,
		"cwd":        resolved,
		"mcpServers": []any{},
	}
	loadResult, err := c.transport.Call(ctx, "session/load", params)
	if err != nil {
		return acpprotocol.LoadSessionResponse{}, fmt.Errorf("session/load: %w", err)
	}
	log.Printf("[acp] session/load raw result: %s", string(loadResult))

	var loadResp acpprotocol.LoadSessionResponse
	if loadResult != nil {
		if err := json.Unmarshal(loadResult, &loadResp); err != nil {
			return acpprotocol.LoadSessionResponse{}, fmt.Errorf("parse session/load result: %w", err)
		}
	}

	// Re-apply permission mode if requested and different from default.
	modeApplied := false
	if domain.IsNonDefaultMode(permissionMode) {
		_, err := c.transport.Call(ctx, "session/set_mode", map[string]any{
			"sessionId": sessionID,
			"modeId":    permissionMode,
		})
		if err != nil {
			log.Printf("[acp] failed to restore permission mode %q on load: %v", permissionMode, err)
		} else {
			modeApplied = true
		}
	}
	if modeApplied {
		setConfigOptionCurrentValue(loadResp.ConfigOptions, "mode", permissionMode)
	}

	// Re-apply model if non-default.
	if model != "" && model != "default" {
		_, err := c.transport.Call(ctx, "session/set_config_option", map[string]any{
			"sessionId": sessionID,
			"configId":  "model",
			"value":     model,
		})
		if err != nil {
			log.Printf("[acp] failed to restore model %q on load: %v", model, err)
		}
	}

	if ev := modeEvent(sessionID, resolveCurrentModeID(permissionMode, modeApplied, loadResp.Modes, loadResp.ConfigOptions), nil); ev != nil {
		c.emit(*ev)
	}

	// If model was re-applied, override the currentValue in configOptions.
	if model != "" && model != "default" {
		setConfigOptionCurrentValue(loadResp.ConfigOptions, "model", model)
	}

	if ev := configOptionEvent(sessionID, loadResp.ConfigOptions, "model", appwire.EventModelChanged, nil); ev != nil {
		c.emit(*ev)
	}

	return loadResp, nil
}

// ListSessions calls session/list on the ACP agent and returns session summaries.
func (c *Client) ListSessions(ctx context.Context, cwd string) ([]domain.SessionSummary, error) {
	params := map[string]any{}
	if cwd != "" {
		params["cwd"] = cwd
	}
	result, err := c.transport.Call(ctx, "session/list", params)
	if err != nil {
		return nil, fmt.Errorf("session/list: %w", err)
	}

	var resp struct {
		Sessions []struct {
			SessionID string `json:"sessionId"`
			CWD       string `json:"cwd"`
			Title     string `json:"title"`
			UpdatedAt string `json:"updatedAt"`
		} `json:"sessions"`
	}
	if err := json.Unmarshal(result, &resp); err != nil {
		return nil, fmt.Errorf("parse session/list result: %w", err)
	}

	summaries := make([]domain.SessionSummary, 0, len(resp.Sessions))
	for _, s := range resp.Sessions {
		updatedAt, _ := time.Parse(time.RFC3339Nano, s.UpdatedAt)
		if updatedAt.IsZero() {
			updatedAt = time.Now()
		}
		summaries = append(summaries, domain.SessionSummary{
			SessionID: s.SessionID,
			CWD:       s.CWD,
			Title:     s.Title,
			UpdatedAt: updatedAt,
		})
	}
	return summaries, nil
}

// Prompt sends a prompt to the agent. Returns the stop reason and usage when the agent finishes.
func (c *Client) Prompt(ctx context.Context, sessionID string, content []domain.ContentBlock) (string, *domain.PromptUsage, error) {
	// Assign fresh IDs for this agent response turn.
	c.msgMu.Lock()
	state := c.ensureSessionMsgStateLocked(sessionID)
	state.agentMsgID = uuid.NewString()
	state.agentPartID = uuid.NewString()
	c.msgMu.Unlock()

	params := map[string]any{
		"sessionId": sessionID,
		"prompt":    content,
	}
	result, err := c.transport.Call(ctx, "session/prompt", params)
	if err != nil {
		return "", nil, fmt.Errorf("session/prompt: %w", err)
	}

	var resp struct {
		StopReason string              `json:"stopReason"`
		Usage      *domain.PromptUsage `json:"usage"`
	}
	if err := json.Unmarshal(result, &resp); err != nil {
		return "", nil, fmt.Errorf("parse session/prompt result: %w", err)
	}
	return resp.StopReason, resp.Usage, nil
}

// Cancel sends a cancel notification for the given session.
func (c *Client) Cancel(ctx context.Context, sessionID string) error {
	if err := c.transport.Notify("session/cancel", map[string]any{
		"sessionId": sessionID,
	}); err != nil {
		return err
	}
	if err := c.cancelPendingPermissions(sessionID); err != nil {
		return fmt.Errorf("cancel pending permissions: %w", err)
	}
	return nil
}

// SetMode changes the permission mode for the given session.
func (c *Client) SetMode(ctx context.Context, sessionID, modeID string) error {
	_, err := c.transport.Call(ctx, "session/set_mode", map[string]any{
		"sessionId": sessionID,
		"modeId":    modeID,
	})
	if err != nil {
		return fmt.Errorf("session/set_mode: %w", err)
	}
	if ev := modeEvent(sessionID, modeID, nil); ev != nil {
		c.emit(*ev)
	}
	return nil
}

// SetConfigOption changes a config option (e.g. model) for the given session.
func (c *Client) SetConfigOption(ctx context.Context, sessionID, configID, value string) error {
	log.Printf("[acp] SetConfigOption: sessionID=%s configID=%s value=%s", sessionID, configID, value)
	result, err := c.transport.Call(ctx, "session/set_config_option", map[string]any{
		"sessionId": sessionID,
		"configId":  configID,
		"value":     value,
	})
	if err != nil {
		return fmt.Errorf("session/set_config_option: %w", err)
	}
	log.Printf("[acp] SetConfigOption raw result: %s", string(result))
	// Parse response configOptions and emit events.
	var resp acpprotocol.SetSessionConfigOptionResponse
	if result != nil {
		if err := json.Unmarshal(result, &resp); err != nil {
			return fmt.Errorf("parse session/set_config_option result: %w", err)
		}
	}
	if ev := configOptionEvent(sessionID, resp.ConfigOptions, "model", appwire.EventModelChanged, nil); ev != nil {
		c.emit(*ev)
	}
	return nil
}

// ReplyPermission responds to a pending permission request from the agent.
func (c *Client) ReplyPermission(ctx context.Context, sessionID, requestID, optionID string) error {
	c.permMu.Lock()
	perm, ok := c.pendingPerm[requestID]
	if ok {
		delete(c.pendingPerm, requestID)
	}
	c.permMu.Unlock()

	if !ok {
		return fmt.Errorf("no pending permission request with ID %q", requestID)
	}

	resp := selectedPermissionResponse(optionID)
	return c.transport.Respond(perm.rpcID, resp)
}

// PendingApprovals returns a snapshot of all pending approval requests.
func (c *Client) PendingApprovals() []domain.ApprovalRequest {
	c.permMu.Lock()
	defer c.permMu.Unlock()
	result := make([]domain.ApprovalRequest, 0, len(c.pendingPerm))
	for _, perm := range c.pendingPerm {
		result = append(result, perm.request)
	}
	return result
}

// Events returns the channel for receiving app transport events.
func (c *Client) Events() <-chan appwire.Event {
	return c.events
}

// handleNotifications processes ACP notifications from the agent.
func (c *Client) handleNotifications() {
	for notif := range c.transport.Notifications() {
		switch notif.Method {
		case "session/update":
			c.handleSessionUpdate(notif.Params)
		default:
			log.Printf("[acp] unhandled notification: %s", notif.Method)
		}
	}
}

// handleRequests processes ACP requests from the agent (e.g. permission requests).
func (c *Client) handleRequests() {
	for req := range c.transport.Requests() {
		switch req.Method {
		case "session/request_permission":
			c.handlePermissionRequest(req)
		default:
			log.Printf("[acp] unhandled agent request: %s", req.Method)
			// Respond with error for unknown methods
			if req.ID != nil {
				_ = c.transport.Respond(*req.ID, map[string]any{
					"error": fmt.Sprintf("unknown method: %s", req.Method),
				})
			}
		}
	}
}

// handlePermissionRequest processes a permission request from the agent.
func (c *Client) handlePermissionRequest(req *IncomingMessage) {
	if req.ID == nil {
		return
	}

	var permReq acpprotocol.RequestPermissionRequest
	if err := json.Unmarshal(req.Params, &permReq); err != nil {
		log.Printf("[acp] failed to parse permission request: %v", err)
		_ = c.transport.Respond(*req.ID, selectedPermissionResponse("reject"))
		return
	}

	requestID := strconv.FormatInt(*req.ID, 10)
	approval := buildApprovalRequest(requestID, c.cfg.RuntimeID, permReq, time.Now())

	// Store pending permission
	c.permMu.Lock()
	c.pendingPerm[requestID] = &pendingPermission{
		rpcID:   *req.ID,
		request: approval,
	}
	c.permMu.Unlock()

	// Emit approval event for the mobile client
	c.emit(appwire.Event{
		Type:      appwire.EventApprovalRequested,
		SessionID: permReq.SessionID,
		At:        time.Now(),
		Approval:  appwireconv.ApprovalRequestPtrFromDomain(&approval),
	})
}

// handleSessionUpdate processes an ACP session/update notification.
func (c *Client) handleSessionUpdate(params json.RawMessage) {
	var envelope struct {
		SessionID string `json:"sessionId"`
		Update    struct {
			SessionUpdate string          `json:"sessionUpdate"`
			Raw           json.RawMessage `json:"-"`
		} `json:"update"`
	}
	if err := json.Unmarshal(params, &envelope); err != nil {
		log.Printf("[acp] failed to parse session/update: %v", err)
		return
	}

	// Re-extract the full update object
	var full struct {
		SessionID string          `json:"sessionId"`
		Update    json.RawMessage `json:"update"`
	}
	if err := json.Unmarshal(params, &full); err != nil {
		return
	}

	sessionID := envelope.SessionID
	updateType := domain.SessionUpdateType(envelope.Update.SessionUpdate)

	switch updateType {
	case domain.UpdateAgentMessageChunk:
		c.handleAgentMessageChunk(sessionID, full.Update)
	case domain.UpdateAgentThoughtChunk:
		c.handleAgentThoughtChunk(sessionID, full.Update)
	case domain.UpdateUserMessageChunk:
		c.handleUserMessageChunk(sessionID, full.Update)
	case domain.UpdateToolCall:
		log.Printf("[acp] tool_call: %s", string(full.Update))
		c.handleToolCall(sessionID, full.Update)
	case domain.UpdateToolCallUpdate:
		log.Printf("[acp] tool_call_update: %s", string(full.Update))
		c.handleToolCallUpdate(sessionID, full.Update)
	case domain.UpdatePlan:
		c.handlePlan(sessionID, full.Update)
	case domain.UpdateCurrentMode:
		c.handleCurrentModeUpdate(sessionID, full.Update)
	case domain.UpdateUsageUpdate:
		c.handleUsageUpdate(sessionID, full.Update)
	case domain.UpdateSessionInfo:
		// Future-proof: ready for session_info_update when runtimes emit it.
		log.Printf("[acp] session_info_update for %s (no-op)", sessionID)
	case domain.UpdateConfigOption:
		c.handleConfigOptionUpdate(sessionID, full.Update)
	default:
		// available_commands_update etc — ignore
		log.Printf("[acp] ignored session/update type: %s", updateType)
	}
}

func (c *Client) handleAgentMessageChunk(sessionID string, raw json.RawMessage) {
	var update acpprotocol.ContentChunk
	if err := json.Unmarshal(raw, &update); err != nil {
		return
	}
	c.msgMu.Lock()
	state := c.ensureSessionMsgStateLocked(sessionID)
	// A new assistant chunk closes the previous user chunk grouping.
	state.userMsgID = ""
	state.userPartID = ""
	msgID := ""
	if update.MessageID != nil {
		msgID = *update.MessageID
	}
	if msgID == "" {
		if state.agentMsgID == "" {
			state.agentMsgID = uuid.NewString()
		}
		msgID = state.agentMsgID
	} else {
		if state.agentMsgID != msgID {
			state.agentPartID = ""
		}
		state.agentMsgID = msgID
	}
	if state.agentPartID == "" {
		state.agentPartID = uuid.NewString()
	}
	partID := state.agentPartID
	c.msgMu.Unlock()

	c.emit(messagePartEvent(appwire.EventMessageDelta, sessionID, &update, appwire.MessagePartEventApp{
		MessageID: msgID,
		PartID:    partID,
		Role:      appwire.MessageRoleAgent,
		Delta:     contentChunkDisplayText(update.Content),
		PartType:  "text",
	}))
}

func (c *Client) handleAgentThoughtChunk(sessionID string, raw json.RawMessage) {
	var update acpprotocol.ContentChunk
	if err := json.Unmarshal(raw, &update); err != nil {
		return
	}
	c.msgMu.Lock()
	state := c.ensureSessionMsgStateLocked(sessionID)
	msgID := ""
	if update.MessageID != nil {
		msgID = *update.MessageID
	}
	if msgID == "" {
		if state.agentMsgID == "" {
			state.agentMsgID = uuid.NewString()
		}
		msgID = state.agentMsgID
	} else {
		state.agentMsgID = msgID
	}
	if state.agentMsgID == "" {
		state.agentMsgID = uuid.NewString()
	}
	// Clear agentPartID so any text chunk after a thought chunk creates
	// a new message part instead of appending to pre-thought text.
	state.agentPartID = ""
	c.msgMu.Unlock()

	c.emit(messagePartEvent(appwire.EventReasoning, sessionID, &update, appwire.MessagePartEventApp{
		MessageID: msgID,
		PartID:    uuid.NewString(),
		Role:      appwire.MessageRoleAgent,
		Delta:     contentChunkDisplayText(update.Content),
		PartType:  "reasoning",
	}))
}

func (c *Client) handleUserMessageChunk(sessionID string, raw json.RawMessage) {
	var update acpprotocol.ContentChunk
	if err := json.Unmarshal(raw, &update); err != nil {
		return
	}

	c.msgMu.Lock()
	state := c.ensureSessionMsgStateLocked(sessionID)
	msgID := ""
	if update.MessageID != nil {
		msgID = *update.MessageID
	}
	if msgID == "" {
		if state.userMsgID == "" {
			state.userMsgID = uuid.NewString()
		}
		msgID = state.userMsgID
	} else {
		if state.userMsgID != msgID {
			state.userPartID = ""
		}
		state.userMsgID = msgID
	}
	if state.userPartID == "" {
		state.userPartID = uuid.NewString()
	}
	partID := state.userPartID
	state.userMsgID = msgID
	state.userPartID = partID
	// A new user message marks the next assistant response boundary.
	state.agentMsgID = ""
	state.agentPartID = ""
	c.msgMu.Unlock()

	c.emit(messagePartEvent(appwire.EventMessageDelta, sessionID, &update, appwire.MessagePartEventApp{
		MessageID: msgID,
		PartID:    partID,
		Role:      appwire.MessageRoleUser,
		Delta:     contentChunkDisplayText(update.Content),
		PartType:  "text",
	}))
}

func (c *Client) handleToolCall(sessionID string, raw json.RawMessage) {
	var update acpprotocol.ToolCallUpdate
	if err := json.Unmarshal(raw, &update); err != nil {
		return
	}
	if update.ToolCallID == "" || update.Title == nil || *update.Title == "" {
		return
	}
	c.msgMu.Lock()
	state := c.ensureSessionMsgStateLocked(sessionID)
	if state.agentMsgID == "" {
		state.agentMsgID = uuid.NewString()
	}
	msgID := state.agentMsgID
	// Each tool gets its own partID and clears agentPartID so the next
	// text chunk after a tool call creates a fresh message part instead
	// of appending to the pre-tool text.
	partID := uuid.NewString()
	state.agentPartID = ""
	c.msgMu.Unlock()

	var locations []appwire.ToolLocation
	for _, loc := range update.Locations {
		line := intPtrFromUint32(loc.Line)
		locations = append(locations, appwire.ToolLocation{Path: loc.Path, Line: line})
	}

	c.emit(appwire.Event{
		Type:      appwire.EventToolStarted,
		SessionID: sessionID,
		At:        time.Now(),
		Tool: &appwire.ToolEvent{
			ACP: &update,
			App: appwire.ToolEventApp{
				PartID:     partID,
				MessageID:  msgID,
				CallID:     update.ToolCallID,
				Name:       *update.Title,
				Kind:       stringPtrValue(update.Kind),
				Title:      *update.Title,
				Status:     appwire.ToolStatusPending,
				Input:      toolInputFromRaw(update.RawInput),
				ClaudeCode: claudeCodeToolFromMeta(update.Meta),
				Locations:  locations,
			},
		},
	})
}

func (c *Client) handleToolCallUpdate(sessionID string, raw json.RawMessage) {
	var update acpprotocol.ToolCallUpdate
	if err := json.Unmarshal(raw, &update); err != nil {
		return
	}

	// Skip updates that carry no actionable fields (e.g. only _meta).
	if update.Status == nil &&
		update.Title == nil &&
		update.Kind == nil &&
		update.Content == nil &&
		update.Locations == nil &&
		len(update.RawInput) == 0 &&
		len(update.RawOutput) == 0 {
		return
	}

	c.msgMu.Lock()
	state := c.ensureSessionMsgStateLocked(sessionID)
	if state.agentMsgID == "" {
		state.agentMsgID = uuid.NewString()
	}
	msgID := state.agentMsgID
	partID := uuid.NewString()
	state.agentPartID = ""
	c.msgMu.Unlock()

	var locations []appwire.ToolLocation
	for _, loc := range update.Locations {
		line := intPtrFromUint32(loc.Line)
		locations = append(locations, appwire.ToolLocation{Path: loc.Path, Line: line})
	}

	toolEvent := appwire.ToolEvent{
		ACP: &update,
		App: appwire.ToolEventApp{
			PartID:     partID,
			MessageID:  msgID,
			CallID:     update.ToolCallID,
			Name:       stringValue(update.Title),
			Kind:       stringPtrValue(update.Kind),
			Title:      stringValue(update.Title),
			Input:      toolInputFromRaw(update.RawInput),
			ClaudeCode: claudeCodeToolFromMeta(update.Meta),
			Locations:  locations,
		},
	}

	var eventType appwire.EventType

	switch toolCallStatusValue(update.Status) {
	case "in_progress":
		eventType = appwire.EventToolUpdated
		toolEvent.App.Status = appwire.ToolStatusInProgress
	case "completed":
		eventType = appwire.EventToolCompleted
		toolEvent.App.Status = appwire.ToolStatusCompleted
		// rawOutput can be a string or an object — handle both.
		toolEvent.App.Output = extractRawOutput(update.RawOutput)
		if toolEvent.App.Output == "" {
			toolEvent.App.Output = extractTextFromContent(rawJSONFromMessages(update.Content))
		}
		toolEvent.App.Diffs = extractDiffsFromContent(rawJSONFromMessages(update.Content))
	case "failed":
		eventType = appwire.EventToolFailed
		toolEvent.App.Status = appwire.ToolStatusFailed
		toolEvent.App.Error = extractRawOutput(update.RawOutput)
		if toolEvent.App.Error == "" {
			toolEvent.App.Error = extractTextFromContent(rawJSONFromMessages(update.Content))
		}
	default:
		eventType = appwire.EventToolUpdated
		toolEvent.App.Status = appwire.ToolStatusInProgress
	}

	c.emit(appwire.Event{
		Type:      eventType,
		SessionID: sessionID,
		At:        time.Now(),
		Tool:      &toolEvent,
	})
}

func (c *Client) handlePlan(sessionID string, raw json.RawMessage) {
	var update acpprotocol.PlanUpdate
	if err := json.Unmarshal(raw, &update); err != nil {
		log.Printf("[acp] failed to parse plan update: %v", err)
		return
	}
	if ev := planEvent(sessionID, &update); ev != nil {
		c.emit(*ev)
	}
}

func (c *Client) handleCurrentModeUpdate(sessionID string, raw json.RawMessage) {
	var update acpprotocol.CurrentModeUpdate
	if err := json.Unmarshal(raw, &update); err != nil {
		log.Printf("[acp] failed to parse current_mode_update: %v", err)
		return
	}
	if update.CurrentModeID == "" {
		return
	}
	if ev := modeEvent(sessionID, update.CurrentModeID, &update); ev != nil {
		c.emit(*ev)
	}
}

func (c *Client) handleConfigOptionUpdate(sessionID string, raw json.RawMessage) {
	var update acpprotocol.ConfigOptionUpdate
	if err := json.Unmarshal(raw, &update); err != nil {
		log.Printf("[acp] failed to parse config_option_update: %v", err)
		return
	}
	if ev := configOptionEvent(sessionID, update.ConfigOptions, "model", appwire.EventModelChanged, &update); ev != nil {
		c.emit(*ev)
	}
	if ev := configOptionEvent(sessionID, update.ConfigOptions, "mode", appwire.EventModeChanged, &update); ev != nil {
		c.emit(*ev)
	}
}

func (c *Client) handleUsageUpdate(sessionID string, raw json.RawMessage) {
	var update acpprotocol.UsageUpdate
	if err := json.Unmarshal(raw, &update); err != nil {
		log.Printf("[acp] failed to parse usage_update: %v", err)
		return
	}
	if ev := usageEvent(sessionID, &update); ev != nil {
		c.emit(*ev)
	}
}

func (c *Client) emit(ev appwire.Event) {
	select {
	case c.events <- ev:
	default:
		log.Printf("[acp] event channel full, dropping event: %s", ev.Type)
	}
}

func (c *Client) ensureSessionMsgStateLocked(sessionID string) *sessionMsgState {
	state := c.sessionMsg[sessionID]
	if state != nil {
		return state
	}
	state = &sessionMsgState{}
	c.sessionMsg[sessionID] = state
	return state
}

func (c *Client) cancelPendingPermissions(sessionID string) error {
	c.permMu.Lock()
	toCancel := make([]*pendingPermission, 0)
	for id, perm := range c.pendingPerm {
		if perm.request.SessionID() != sessionID {
			continue
		}
		toCancel = append(toCancel, perm)
		delete(c.pendingPerm, id)
	}
	c.permMu.Unlock()

	var firstErr error
	for _, perm := range toCancel {
		err := c.transport.Respond(perm.rpcID, cancelledPermissionResponse())
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// configOptionEvent builds an event for a config option matching the given category.
// Returns nil if no matching option is found.
func configOptionEvent(
	sessionID string,
	opts []acpprotocol.SessionConfigOption,
	category string,
	eventType appwire.EventType,
	acpUpdate *acpprotocol.ConfigOptionUpdate,
) *appwire.Event {
	for _, opt := range opts {
		if configOptionCategory(opt) != category {
			continue
		}
		flattened := opt.Options.Flatten()
		values := make([]appwire.SessionConfigValue, 0, len(flattened))
		for _, v := range flattened {
			values = append(values, appwire.SessionConfigValue{
				Value:       v.Value,
				Name:        v.Name,
				Description: v.Description,
			})
		}
		if eventType == appwire.EventModeChanged {
			return &appwire.Event{
				Type:      eventType,
				SessionID: sessionID,
				At:        time.Now(),
				ModeChanged: &appwire.ModeChangedEvent{
					ACPConfigOption: acpUpdate,
					App: appwire.ModeChangedEventApp{
						CurrentModeID: opt.CurrentValue,
					},
				},
			}
		}
		return &appwire.Event{
			Type:      eventType,
			SessionID: sessionID,
			At:        time.Now(),
			ConfigChanged: &appwire.ConfigChangedEvent{
				ACP: acpUpdate,
				App: appwire.ConfigChangedEventApp{
					ConfigID:     opt.ID,
					Category:     category,
					CurrentValue: opt.CurrentValue,
					Values:       values,
				},
			},
		}
	}
	return nil
}

func resolveCurrentModeID(requestedMode string, requestedApplied bool, modes *acpprotocol.SessionModeState, opts []acpprotocol.SessionConfigOption) string {
	if requestedApplied && requestedMode != "" {
		return requestedMode
	}
	if modes != nil && modes.CurrentModeID != "" {
		return modes.CurrentModeID
	}
	for _, opt := range opts {
		if configOptionCategory(opt) == "mode" && opt.CurrentValue != "" {
			return opt.CurrentValue
		}
	}
	return ""
}

func setConfigOptionCurrentValue(opts []acpprotocol.SessionConfigOption, category, value string) {
	if value == "" {
		return
	}
	for i, opt := range opts {
		if configOptionCategory(opt) == category {
			opts[i].CurrentValue = value
			return
		}
	}
}

func configOptionCategory(opt acpprotocol.SessionConfigOption) string {
	if opt.Category == nil {
		return ""
	}
	return *opt.Category
}

func modeEvent(
	sessionID, modeID string,
	acpUpdate *acpprotocol.CurrentModeUpdate,
) *appwire.Event {
	if modeID == "" {
		return nil
	}
	return &appwire.Event{
		Type:      appwire.EventModeChanged,
		SessionID: sessionID,
		At:        time.Now(),
		ModeChanged: &appwire.ModeChangedEvent{
			ACPCurrentMode: acpUpdate,
			App: appwire.ModeChangedEventApp{
				CurrentModeID: modeID,
			},
		},
	}
}

func expandAndValidateCWD(cwd string) (string, error) {
	if strings.HasPrefix(cwd, "~/") || cwd == "~" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("cannot resolve home directory: %w", err)
		}
		cwd = filepath.Join(home, cwd[1:])
	}
	if cwd == "" || !filepath.IsAbs(cwd) {
		return "", fmt.Errorf("cwd must be an absolute path")
	}
	return cwd, nil
}

package acp

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"sync"
	"time"

	"github.com/LaLanMo/muxagent-cli/internal/domain"
	"github.com/google/uuid"
)

// Config holds the configuration for an ACP client.
type Config struct {
	Command string
	Args    []string
	CWD     string
	Env     map[string]string
}

// sessionMsgState tracks the current agent message being streamed for a session.
type sessionMsgState struct {
	agentMsgID string // ID for the current agent response message
	textPartID string // ID for the current text part within the message
}

// Client implements runtime.Client over ACP (JSON-RPC 2.0 over stdio).
type Client struct {
	cfg       Config
	transport *Transport
	events    chan domain.Event

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
		events:      make(chan domain.Event, 256),
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

// NewSession creates a new ACP session.
func (c *Client) NewSession(ctx context.Context, cwd string) (string, error) {
	if cwd == "" {
		cwd = "."
	}
	params := map[string]any{
		"cwd":        cwd,
		"mcpServers": []any{},
	}
	result, err := c.transport.Call(ctx, "session/new", params)
	if err != nil {
		return "", fmt.Errorf("session/new: %w", err)
	}

	var resp struct {
		SessionID string `json:"sessionId"`
	}
	if err := json.Unmarshal(result, &resp); err != nil {
		return "", fmt.Errorf("parse session/new result: %w", err)
	}
	return resp.SessionID, nil
}

// LoadSession loads an existing session. History is replayed via session/update notifications.
func (c *Client) LoadSession(ctx context.Context, sessionID, cwd string) error {
	if cwd == "" {
		cwd = "."
	}
	params := map[string]any{
		"sessionId":  sessionID,
		"cwd":        cwd,
		"mcpServers": []any{},
	}
	_, err := c.transport.Call(ctx, "session/load", params)
	if err != nil {
		return fmt.Errorf("session/load: %w", err)
	}
	return nil
}

// Prompt sends a prompt to the agent. Returns the stop reason when the agent finishes.
func (c *Client) Prompt(ctx context.Context, sessionID string, content []domain.ContentBlock) (string, error) {
	// Assign fresh IDs for this agent response turn.
	c.msgMu.Lock()
	c.sessionMsg[sessionID] = &sessionMsgState{
		agentMsgID: uuid.NewString(),
		textPartID: uuid.NewString(),
	}
	c.msgMu.Unlock()
	defer func() {
		c.msgMu.Lock()
		delete(c.sessionMsg, sessionID)
		c.msgMu.Unlock()
	}()

	params := map[string]any{
		"sessionId": sessionID,
		"prompt":    content,
	}
	result, err := c.transport.Call(ctx, "session/prompt", params)
	if err != nil {
		return "", fmt.Errorf("session/prompt: %w", err)
	}

	var resp struct {
		StopReason string `json:"stopReason"`
	}
	if err := json.Unmarshal(result, &resp); err != nil {
		return "", fmt.Errorf("parse session/prompt result: %w", err)
	}
	return resp.StopReason, nil
}

// Cancel sends a cancel notification for the given session.
func (c *Client) Cancel(ctx context.Context, sessionID string) error {
	return c.transport.Notify("session/cancel", map[string]any{
		"sessionId": sessionID,
	})
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

	resp := domain.PermissionResponse{
		Outcome: domain.PermOutcome{
			Outcome:  "selected",
			OptionID: optionID,
		},
	}
	return c.transport.Respond(perm.rpcID, resp)
}

// Events returns the channel for receiving domain events.
func (c *Client) Events() <-chan domain.Event {
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
		case "session/requestPermission":
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

	var permReq domain.PermissionRequest
	if err := json.Unmarshal(req.Params, &permReq); err != nil {
		log.Printf("[acp] failed to parse permission request: %v", err)
		_ = c.transport.Respond(*req.ID, domain.PermissionResponse{
			Outcome: domain.PermOutcome{Outcome: "selected", OptionID: "reject"},
		})
		return
	}

	// Build an approval request for the mobile client
	requestID := strconv.FormatInt(*req.ID, 10)
	approval := domain.ApprovalRequest{
		ID:        requestID,
		SessionID: permReq.SessionID,
		CreatedAt: time.Now(),
		Options:   permReq.Options,
	}

	if permReq.ToolCall != nil {
		approval.ToolName = permReq.ToolCall.Title
		approval.Title = permReq.ToolCall.Title
		approval.Kind = permReq.ToolCall.Kind
		approval.Input = permReq.ToolCall.RawInput
	}

	// Store pending permission
	c.permMu.Lock()
	c.pendingPerm[requestID] = &pendingPermission{
		rpcID:   *req.ID,
		request: approval,
	}
	c.permMu.Unlock()

	// Emit approval event for the mobile client
	c.emit(domain.Event{
		Type:      domain.EventApprovalRequested,
		SessionID: permReq.SessionID,
		At:        time.Now(),
		Approval:  &approval,
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
		c.handleToolCall(sessionID, full.Update)
	case domain.UpdateToolCallUpdate:
		c.handleToolCallUpdate(sessionID, full.Update)
	case domain.UpdatePlan:
		c.handlePlan(sessionID, full.Update)
	default:
		// available_commands_update, current_mode_update, config_option_update — ignore
	}
}

func (c *Client) handleAgentMessageChunk(sessionID string, raw json.RawMessage) {
	var update struct {
		MessageID string `json:"messageId"`
		PartID    string `json:"partId"`
		Content   struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(raw, &update); err != nil {
		return
	}
	c.msgMu.Lock()
	state := c.sessionMsg[sessionID]
	c.msgMu.Unlock()

	var msgID, partID string
	if state != nil {
		msgID = state.agentMsgID
		partID = state.textPartID
	}
	c.emit(domain.Event{
		Type:      domain.EventMessageDelta,
		SessionID: sessionID,
		At:        time.Now(),
		MessagePart: &domain.MessagePartEvent{
			MessageID: msgID,
			PartID:    partID,
			Delta:     update.Content.Text,
			PartType:  "text",
		},
	})
}

func (c *Client) handleAgentThoughtChunk(sessionID string, raw json.RawMessage) {
	var update struct {
		Content struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(raw, &update); err != nil {
		return
	}
	c.emit(domain.Event{
		Type:      domain.EventReasoning,
		SessionID: sessionID,
		At:        time.Now(),
		MessagePart: &domain.MessagePartEvent{
			Delta:    update.Content.Text,
			PartType: "reasoning",
		},
	})
}

func (c *Client) handleUserMessageChunk(sessionID string, raw json.RawMessage) {
	var update struct {
		Content struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(raw, &update); err != nil {
		return
	}
	c.emit(domain.Event{
		Type:      domain.EventMessageDelta,
		SessionID: sessionID,
		At:        time.Now(),
		MessagePart: &domain.MessagePartEvent{
			Delta:    update.Content.Text,
			PartType: "user",
		},
	})
}

func (c *Client) handleToolCall(sessionID string, raw json.RawMessage) {
	var update struct {
		ToolCallID string `json:"toolCallId"`
		Title      string `json:"title"`
		Kind       string `json:"kind"`
		Status     string `json:"status"`
	}
	if err := json.Unmarshal(raw, &update); err != nil {
		return
	}
	c.emit(domain.Event{
		Type:      domain.EventToolStarted,
		SessionID: sessionID,
		At:        time.Now(),
		Tool: &domain.ToolEvent{
			CallID: update.ToolCallID,
			Name:   update.Title,
			Title:  update.Title,
			Status: domain.ToolStatusPending,
		},
	})
}

func (c *Client) handleToolCallUpdate(sessionID string, raw json.RawMessage) {
	var update struct {
		ToolCallID string         `json:"toolCallId"`
		Status     string         `json:"status"`
		Kind       string         `json:"kind"`
		Title      string         `json:"title"`
		RawInput   map[string]any `json:"rawInput"`
		RawOutput  map[string]any `json:"rawOutput"`
		Content    json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(raw, &update); err != nil {
		return
	}

	toolEvent := domain.ToolEvent{
		CallID: update.ToolCallID,
		Name:   update.Title,
		Title:  update.Title,
		Input:  update.RawInput,
	}

	var eventType domain.EventType

	switch update.Status {
	case "in_progress":
		eventType = domain.EventToolUpdated
		toolEvent.Status = domain.ToolStatusInProgress
	case "completed":
		eventType = domain.EventToolCompleted
		toolEvent.Status = domain.ToolStatusCompleted
		// Extract output from rawOutput or content
		if update.RawOutput != nil {
			if output, ok := update.RawOutput["output"].(string); ok {
				toolEvent.Output = output
			}
		}
		if toolEvent.Output == "" {
			toolEvent.Output = extractTextFromContent(update.Content)
		}
	case "failed":
		eventType = domain.EventToolFailed
		toolEvent.Status = domain.ToolStatusFailed
		// Extract error from rawOutput or content
		if update.RawOutput != nil {
			if errStr, ok := update.RawOutput["error"].(string); ok {
				toolEvent.Error = errStr
			}
		}
		if toolEvent.Error == "" {
			toolEvent.Error = extractTextFromContent(update.Content)
		}
	default:
		eventType = domain.EventToolUpdated
		toolEvent.Status = domain.ToolStatusInProgress
	}

	c.emit(domain.Event{
		Type:      eventType,
		SessionID: sessionID,
		At:        time.Now(),
		Tool:      &toolEvent,
	})
}

func (c *Client) handlePlan(sessionID string, raw json.RawMessage) {
	var update struct {
		Entries json.RawMessage `json:"entries"`
	}
	if err := json.Unmarshal(raw, &update); err != nil {
		return
	}
	c.emit(domain.Event{
		Type:      domain.EventPlanUpdated,
		SessionID: sessionID,
		At:        time.Now(),
		Data:      map[string]any{"entries": json.RawMessage(update.Entries)},
	})
}

func (c *Client) emit(ev domain.Event) {
	select {
	case c.events <- ev:
	default:
		log.Printf("[acp] event channel full, dropping event: %s", ev.Type)
	}
}

// extractTextFromContent extracts text from ACP content array.
// Content is an array of objects like [{"type":"content","content":{"type":"text","text":"..."}}]
func extractTextFromContent(raw json.RawMessage) string {
	if raw == nil {
		return ""
	}
	var items []struct {
		Type    string `json:"type"`
		Content struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(raw, &items); err != nil {
		return ""
	}
	for _, item := range items {
		if item.Content.Text != "" {
			return item.Content.Text
		}
	}
	return ""
}

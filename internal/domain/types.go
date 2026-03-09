package domain

import "time"

// --- Session ---

type SessionStatus string

const (
	SessionStatusIdle            SessionStatus = "idle"
	SessionStatusRunning         SessionStatus = "running"
	SessionStatusWaitingApproval SessionStatus = "waiting_approval"
	SessionStatusError           SessionStatus = "error"
	SessionStatusDone            SessionStatus = "done"
)

type Session struct {
	ID        string         `json:"id"`
	Title     string         `json:"title"`
	Status    SessionStatus  `json:"status"`
	Model     string         `json:"model,omitempty"`
	Cost      *CostInfo      `json:"cost,omitempty"`
	CreatedAt time.Time      `json:"createdAt"`
	UpdatedAt time.Time      `json:"updatedAt"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

// SessionSummary is a lightweight ACP session list entry.
type SessionSummary struct {
	SessionID string    `json:"sessionId"`
	CWD       string    `json:"cwd"`
	Title     string    `json:"title"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// --- Cost ---

type CostInfo struct {
	CostAmount   float64 `json:"costAmount,omitempty"`
	CostCurrency string  `json:"costCurrency,omitempty"`
	TotalTokens  int64   `json:"totalTokens,omitempty"`
}

// PromptUsage holds cumulative token usage returned by ACP PromptResponse.
type PromptUsage struct {
	InputTokens       int64 `json:"inputTokens"`
	OutputTokens      int64 `json:"outputTokens"`
	CachedReadTokens  int64 `json:"cachedReadTokens"`
	CachedWriteTokens int64 `json:"cachedWriteTokens"`
	TotalTokens       int64 `json:"totalTokens"`
}

// --- Tool ---

type ToolStatus string

const (
	ToolStatusPending    ToolStatus = "pending"
	ToolStatusInProgress ToolStatus = "in_progress"
	ToolStatusCompleted  ToolStatus = "completed"
	ToolStatusFailed     ToolStatus = "failed"
)

type ToolActivity struct {
	ID       string         `json:"id"`
	Name     string         `json:"name"`
	Kind     string         `json:"kind,omitempty"`
	Status   ToolStatus     `json:"status"`
	Title    string         `json:"title,omitempty"`
	Input    map[string]any `json:"input,omitempty"`
	Output   string         `json:"output,omitempty"`
	Error    string         `json:"error,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

// --- Message ---

type MessageRole string

const (
	MessageRoleUser  MessageRole = "user"
	MessageRoleAgent MessageRole = "agent"
)

type PartType string

const (
	PartTypeText      PartType = "text"
	PartTypeReasoning PartType = "reasoning"
	PartTypeFile      PartType = "file"
	PartTypeTool      PartType = "tool"
	PartTypeData      PartType = "data"
)

type MediaPart struct {
	URL      string `json:"url,omitempty"`
	Base64   string `json:"base64,omitempty"`
	MimeType string `json:"mimeType,omitempty"`
	Name     string `json:"name,omitempty"`
	Size     int64  `json:"size,omitempty"`
}

type MessagePart struct {
	Type  PartType       `json:"type"`
	Text  string         `json:"text,omitempty"`
	Media *MediaPart     `json:"media,omitempty"`
	Tool  *ToolActivity  `json:"tool,omitempty"`
	Data  map[string]any `json:"data,omitempty"`
}

type Message struct {
	ID        string         `json:"id"`
	SessionID string         `json:"sessionId"`
	Role      MessageRole    `json:"role"`
	Parts     []MessagePart  `json:"parts"`
	Cost      *CostInfo      `json:"cost,omitempty"`
	Model     string         `json:"model,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
	CreatedAt time.Time      `json:"createdAt"`
}

// --- Approval ---

type ApprovalRequest struct {
	ID         string         `json:"id"`
	SessionID  string         `json:"sessionId"`
	ToolCallID string         `json:"toolCallId,omitempty"`
	ToolName   string         `json:"toolName"`
	Title      string         `json:"title"`
	Kind       string         `json:"kind,omitempty"`
	Input      map[string]any `json:"input,omitempty"`
	Options    []PermOption   `json:"options,omitempty"`
	CreatedAt  time.Time      `json:"createdAt"`
}

// --- ACP types ---

// SessionUpdateType is the discriminated union type for ACP session/update notifications.
type SessionUpdateType string

const (
	UpdateUserMessageChunk  SessionUpdateType = "user_message_chunk"
	UpdateAgentMessageChunk SessionUpdateType = "agent_message_chunk"
	UpdateAgentThoughtChunk SessionUpdateType = "agent_thought_chunk"
	UpdateToolCall          SessionUpdateType = "tool_call"
	UpdateToolCallUpdate    SessionUpdateType = "tool_call_update"
	UpdatePlan              SessionUpdateType = "plan"
	UpdateAvailableCommands SessionUpdateType = "available_commands_update"
	UpdateCurrentMode       SessionUpdateType = "current_mode_update"
	UpdateConfigOption      SessionUpdateType = "config_option_update"
	UpdateSessionInfo       SessionUpdateType = "session_info_update"
	UpdateUsageUpdate       SessionUpdateType = "usage_update"
)

// ContentBlock is an ACP prompt input block.
type ContentBlock struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	MimeType string `json:"mimeType,omitempty"`
	Data     string `json:"data,omitempty"`
	URI      string `json:"uri,omitempty"`
}

// PermToolCall describes a tool call in an ACP permission request.
type PermToolCall struct {
	ToolCallID string         `json:"toolCallId"`
	Title      string         `json:"title"`
	Status     string         `json:"status"`
	Kind       string         `json:"kind,omitempty"`
	RawInput   map[string]any `json:"rawInput,omitempty"`
}

// PermOption describes an available response option in an ACP permission request.
type PermOption struct {
	OptionID string `json:"optionId"`
	Kind     string `json:"kind"`
	Name     string `json:"name"`
}

// PermissionRequest is the full ACP permission request from agent to client.
type PermissionRequest struct {
	SessionID string        `json:"sessionId"`
	ToolCall  *PermToolCall `json:"toolCall,omitempty"`
	Options   []PermOption  `json:"options"`
}

// PermissionResponse is the ACP permission response from client to agent.
type PermissionResponse struct {
	Outcome PermOutcome `json:"outcome"`
}

// PermOutcome describes the selected permission outcome.
type PermOutcome struct {
	Outcome  string `json:"outcome"`
	OptionID string `json:"optionId,omitempty"`
}

// --- Events ---

type EventType string

const (
	EventMessageDelta      EventType = "message.delta"
	EventMessageFinal      EventType = "message.final"
	EventToolStarted       EventType = "tool.started"
	EventToolUpdated       EventType = "tool.updated"
	EventToolCompleted     EventType = "tool.completed"
	EventToolFailed        EventType = "tool.failed"
	EventReasoning         EventType = "reasoning"
	EventApprovalRequested EventType = "approval.requested"
	EventApprovalReplied   EventType = "approval.replied"
	EventSessionStatus     EventType = "session.status"
	EventRunFailed         EventType = "run.failed"
	EventRunFinished       EventType = "run.finished"
	EventConnectionState   EventType = "connection.state"
	EventPlanUpdated       EventType = "plan.updated"
	EventModeChanged       EventType = "mode.changed"
	EventModelChanged      EventType = "model.changed"
	EventUsageUpdate       EventType = "usage.update"
)

type MessagePartEvent struct {
	PartID    string      `json:"partId"`
	MessageID string      `json:"messageId"`
	Role      MessageRole `json:"role,omitempty"`
	Delta     string      `json:"delta"`
	PartType  string      `json:"partType"`
	FullText  string      `json:"fullText"`
}

type ToolDiff struct {
	Path    string  `json:"path"`
	OldText *string `json:"oldText,omitempty"`
	NewText string  `json:"newText"`
}

type ToolLocation struct {
	Path string `json:"path"`
	Line *int   `json:"line,omitempty"`
}

type ToolEvent struct {
	PartID    string         `json:"partId"`
	MessageID string         `json:"messageId"`
	CallID    string         `json:"callId"`
	Name      string         `json:"name"`
	Kind      string         `json:"kind,omitempty"`
	Title     string         `json:"title,omitempty"`
	Status    ToolStatus     `json:"status"`
	Input     map[string]any `json:"input,omitempty"`
	Output    string         `json:"output,omitempty"`
	Error     string         `json:"error,omitempty"`
	Diffs     []ToolDiff     `json:"diffs,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
	Locations []ToolLocation `json:"locations,omitempty"`
}

type SessionError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type Event struct {
	Type      EventType `json:"type"`
	SessionID string    `json:"sessionId,omitempty"`
	Seq       uint64    `json:"seq"`
	At        time.Time `json:"at"`

	MessagePart *MessagePartEvent `json:"messagePart,omitempty"`
	Message     *Message          `json:"message,omitempty"`
	Tool        *ToolEvent        `json:"tool,omitempty"`
	Approval    *ApprovalRequest  `json:"approval,omitempty"`
	Session     *Session          `json:"session,omitempty"`
	Error       *SessionError     `json:"error,omitempty"`
	Data        map[string]any    `json:"data,omitempty"`
}

// ConfigOptionValue is one option within a config select.
type ConfigOptionValue struct {
	Value       string `json:"value"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// ConfigOption is an ACP configOptions entry (e.g. model select).
type ConfigOption struct {
	ID       string              `json:"id"`
	Type     string              `json:"type"` // "select"
	Category string              `json:"category,omitempty"`
	Label    string              `json:"label,omitempty"`
	CurrentValue string              `json:"currentValue"`
	Options      []ConfigOptionValue `json:"options,omitempty"`
}

type ConnectionState string

const (
	ConnectionConnected    ConnectionState = "connected"
	ConnectionDisconnected ConnectionState = "disconnected"
	ConnectionReconnecting ConnectionState = "reconnecting"
)

package domain

import (
	"time"

	"github.com/LaLanMo/muxagent-cli/internal/acpprotocol"
)

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
	ACP *acpprotocol.RequestPermissionRequest `json:"acp,omitempty"`
	App ApprovalApp                           `json:"app"`
}

type ApprovalApp struct {
	RequestID  string           `json:"requestId"`
	CreatedAt  *time.Time       `json:"createdAt,omitempty"`
	Runtime    string           `json:"runtime,omitempty"`
	ToolCallID string           `json:"toolCallId,omitempty"`
	ToolKind   string           `json:"toolKind,omitempty"`
	Title      string           `json:"title,omitempty"`
	BodyText   string           `json:"bodyText,omitempty"`
	Command    *ApprovalCommand `json:"command,omitempty"`
	CWD        string           `json:"cwd,omitempty"`
	Reason     string           `json:"reason,omitempty"`
	Plan       *ApprovalPlan    `json:"plan,omitempty"`
}

type ApprovalCommand struct {
	Argv    []string `json:"argv"`
	Display string   `json:"display,omitempty"`
}

type ApprovalPlan struct {
	Markdown       string                  `json:"markdown,omitempty"`
	AllowedPrompts []ApprovalAllowedPrompt `json:"allowedPrompts,omitempty"`
}

type ApprovalAllowedPrompt struct {
	Prompt string `json:"prompt"`
}

func (r ApprovalRequest) RequestID() string {
	return r.App.RequestID
}

func (r ApprovalRequest) SessionID() string {
	if r.ACP == nil {
		return ""
	}
	return r.ACP.SessionID
}

func (r ApprovalRequest) ToolCallID() string {
	if r.App.ToolCallID != "" {
		return r.App.ToolCallID
	}
	if r.ACP == nil {
		return ""
	}
	return r.ACP.ToolCall.ToolCallID
}

// --- ACP session/update types ---

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

type MessagePartEventApp struct {
	PartID    string      `json:"partId"`
	MessageID string      `json:"messageId"`
	Role      MessageRole `json:"role,omitempty"`
	Delta     string      `json:"delta"`
	PartType  string      `json:"partType"`
	FullText  string      `json:"fullText"`
}

type MessagePartEvent struct {
	ACP *acpprotocol.ContentChunk `json:"acp,omitempty"`
	App MessagePartEventApp       `json:"app"`
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

type ToolEventApp struct {
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

type ToolEvent struct {
	ACP *acpprotocol.ToolCallUpdate `json:"acp,omitempty"`
	App ToolEventApp                `json:"app"`
}

type PlanEntryApp struct {
	Content  string `json:"content"`
	Status   string `json:"status"`
	Priority string `json:"priority"`
}

type PlanEventApp struct {
	Entries []PlanEntryApp `json:"entries"`
}

type PlanEvent struct {
	ACP *acpprotocol.PlanUpdate `json:"acp,omitempty"`
	App PlanEventApp            `json:"app"`
}

type UsageEventApp struct {
	ContextUsed  int64    `json:"contextUsed"`
	ContextSize  int64    `json:"contextSize"`
	CostAmount   *float64 `json:"costAmount,omitempty"`
	CostCurrency *string  `json:"costCurrency,omitempty"`
}

type UsageEvent struct {
	ACP *acpprotocol.UsageUpdate `json:"acp,omitempty"`
	App UsageEventApp            `json:"app"`
}

type RunFinishedEventApp struct {
	StopReason        string `json:"stopReason"`
	InputTokens       int64  `json:"inputTokens,omitempty"`
	OutputTokens      int64  `json:"outputTokens,omitempty"`
	CachedReadTokens  int64  `json:"cachedReadTokens,omitempty"`
	CachedWriteTokens int64  `json:"cachedWriteTokens,omitempty"`
	TotalTokens       int64  `json:"totalTokens,omitempty"`
}

type RunFinishedEvent struct {
	App RunFinishedEventApp `json:"app"`
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
	Plan        *PlanEvent        `json:"plan,omitempty"`
	Usage       *UsageEvent       `json:"usage,omitempty"`
	RunFinished *RunFinishedEvent `json:"runFinished,omitempty"`
	Session     *Session          `json:"session,omitempty"`
	Error       *SessionError     `json:"error,omitempty"`
	Data        map[string]any    `json:"data,omitempty"`
}

type ConnectionState string

const (
	ConnectionConnected    ConnectionState = "connected"
	ConnectionDisconnected ConnectionState = "disconnected"
	ConnectionReconnecting ConnectionState = "reconnecting"
)

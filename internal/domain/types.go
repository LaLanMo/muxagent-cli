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

// SessionSummary is a lightweight ACP session list entry.
type SessionSummary struct {
	SessionID     string                            `json:"sessionId"`
	CWD           string                            `json:"cwd"`
	Title         string                            `json:"title"`
	Runtime       string                            `json:"runtime,omitempty"`
	UpdatedAt     time.Time                         `json:"updatedAt"`
	ConfigOptions []acpprotocol.SessionConfigOption `json:"configOptions,omitempty"`
}

// PromptUsage holds cumulative token usage returned by ACP PromptResponse.
type PromptUsage struct {
	InputTokens       int64 `json:"inputTokens"`
	OutputTokens      int64 `json:"outputTokens"`
	CachedReadTokens  int64 `json:"cachedReadTokens"`
	CachedWriteTokens int64 `json:"cachedWriteTokens"`
	TotalTokens       int64 `json:"totalTokens"`
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

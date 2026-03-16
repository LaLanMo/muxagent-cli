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
	ID        string        `json:"id"`
	Title     string        `json:"title"`
	Status    SessionStatus `json:"status"`
	Model     string        `json:"model,omitempty"`
	Cost      *CostInfo     `json:"cost,omitempty"`
	CreatedAt time.Time     `json:"createdAt"`
	UpdatedAt time.Time     `json:"updatedAt"`
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
	ID         string          `json:"id"`
	Name       string          `json:"name"`
	Kind       string          `json:"kind,omitempty"`
	Status     ToolStatus      `json:"status"`
	Title      string          `json:"title,omitempty"`
	Input      *ToolInput      `json:"input,omitempty"`
	Output     string          `json:"output,omitempty"`
	Error      string          `json:"error,omitempty"`
	ClaudeCode *ClaudeCodeTool `json:"claudeCode,omitempty"`
}

type ClaudeCodeTool struct {
	ParentToolUseID string `json:"parentToolUseId,omitempty"`
	ToolName        string `json:"toolName,omitempty"`
}

type ToolInput struct {
	Description  string         `json:"description,omitempty"`
	Command      *ToolCommand   `json:"command,omitempty"`
	FilePath     string         `json:"filePath,omitempty"`
	SourcePath   string         `json:"sourcePath,omitempty"`
	TargetPath   string         `json:"targetPath,omitempty"`
	Pattern      string         `json:"pattern,omitempty"`
	URL          string         `json:"url,omitempty"`
	Mode         string         `json:"mode,omitempty"`
	Edit         *ToolEditInput `json:"edit,omitempty"`
	RawInputJSON string         `json:"rawInputJson,omitempty"`
}

type ToolCommand struct {
	Argv    []string `json:"argv,omitempty"`
	Display string   `json:"display,omitempty"`
}

type ToolEditInput struct {
	FilePath  string `json:"filePath,omitempty"`
	OldString string `json:"oldString,omitempty"`
	NewString string `json:"newString,omitempty"`
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
)

type MediaPart struct {
	URL      string `json:"url,omitempty"`
	Base64   string `json:"base64,omitempty"`
	MimeType string `json:"mimeType,omitempty"`
	Name     string `json:"name,omitempty"`
	Size     int64  `json:"size,omitempty"`
}

type MessagePart struct {
	Type  PartType      `json:"type"`
	Text  string        `json:"text,omitempty"`
	Media *MediaPart    `json:"media,omitempty"`
	Tool  *ToolActivity `json:"tool,omitempty"`
}

type Message struct {
	ID        string        `json:"id"`
	SessionID string        `json:"sessionId"`
	Role      MessageRole   `json:"role"`
	Parts     []MessagePart `json:"parts"`
	Cost      *CostInfo     `json:"cost,omitempty"`
	Model     string        `json:"model,omitempty"`
	CreatedAt time.Time     `json:"createdAt"`
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

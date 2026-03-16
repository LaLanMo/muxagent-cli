package appwire

import (
	"time"

	"github.com/LaLanMo/muxagent-cli/internal/acpprotocol"
)

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

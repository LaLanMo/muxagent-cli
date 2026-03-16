package appwire

import (
	"time"

	"github.com/LaLanMo/muxagent-cli/internal/acpprotocol"
	"github.com/LaLanMo/muxagent-cli/internal/domain"
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

func ApprovalRequestFromDomain(request domain.ApprovalRequest) ApprovalRequest {
	return ApprovalRequest{
		ACP: request.ACP,
		App: approvalAppFromDomain(request.App),
	}
}

func ApprovalRequestPtrFromDomain(request *domain.ApprovalRequest) *ApprovalRequest {
	if request == nil {
		return nil
	}
	wire := ApprovalRequestFromDomain(*request)
	return &wire
}

func ApprovalRequestsFromDomain(requests []domain.ApprovalRequest) []ApprovalRequest {
	if len(requests) == 0 {
		return nil
	}
	result := make([]ApprovalRequest, 0, len(requests))
	for _, request := range requests {
		result = append(result, ApprovalRequestFromDomain(request))
	}
	return result
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

func approvalAppFromDomain(app domain.ApprovalApp) ApprovalApp {
	return ApprovalApp{
		RequestID:  app.RequestID,
		CreatedAt:  app.CreatedAt,
		Runtime:    app.Runtime,
		ToolCallID: app.ToolCallID,
		ToolKind:   app.ToolKind,
		Title:      app.Title,
		BodyText:   app.BodyText,
		Command:    approvalCommandFromDomain(app.Command),
		CWD:        app.CWD,
		Reason:     app.Reason,
		Plan:       approvalPlanFromDomain(app.Plan),
	}
}

func approvalCommandFromDomain(command *domain.ApprovalCommand) *ApprovalCommand {
	if command == nil {
		return nil
	}
	return &ApprovalCommand{
		Argv:    append([]string(nil), command.Argv...),
		Display: command.Display,
	}
}

func approvalPlanFromDomain(plan *domain.ApprovalPlan) *ApprovalPlan {
	if plan == nil {
		return nil
	}
	wire := &ApprovalPlan{
		Markdown: plan.Markdown,
	}
	if len(plan.AllowedPrompts) > 0 {
		wire.AllowedPrompts = make([]ApprovalAllowedPrompt, 0, len(plan.AllowedPrompts))
		for _, prompt := range plan.AllowedPrompts {
			wire.AllowedPrompts = append(wire.AllowedPrompts, ApprovalAllowedPrompt{
				Prompt: prompt.Prompt,
			})
		}
	}
	return wire
}

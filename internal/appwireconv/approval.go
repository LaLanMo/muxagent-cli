package appwireconv

import (
	"github.com/LaLanMo/muxagent-cli/internal/appwire"
	"github.com/LaLanMo/muxagent-cli/internal/domain"
)

func ApprovalRequestFromDomain(request domain.ApprovalRequest) appwire.ApprovalRequest {
	return appwire.ApprovalRequest{
		ACP: request.ACP,
		App: approvalAppFromDomain(request.App),
	}
}

func ApprovalRequestPtrFromDomain(request *domain.ApprovalRequest) *appwire.ApprovalRequest {
	if request == nil {
		return nil
	}
	wire := ApprovalRequestFromDomain(*request)
	return &wire
}

func ApprovalRequestsFromDomain(requests []domain.ApprovalRequest) []appwire.ApprovalRequest {
	if len(requests) == 0 {
		return nil
	}
	result := make([]appwire.ApprovalRequest, 0, len(requests))
	for _, request := range requests {
		result = append(result, ApprovalRequestFromDomain(request))
	}
	return result
}

func approvalAppFromDomain(app domain.ApprovalApp) appwire.ApprovalApp {
	return appwire.ApprovalApp{
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

func approvalCommandFromDomain(command *domain.ApprovalCommand) *appwire.ApprovalCommand {
	if command == nil {
		return nil
	}
	return &appwire.ApprovalCommand{
		Argv:    append([]string(nil), command.Argv...),
		Display: command.Display,
	}
}

func approvalPlanFromDomain(plan *domain.ApprovalPlan) *appwire.ApprovalPlan {
	if plan == nil {
		return nil
	}
	wire := &appwire.ApprovalPlan{
		Markdown: plan.Markdown,
	}
	if len(plan.AllowedPrompts) > 0 {
		wire.AllowedPrompts = make([]appwire.ApprovalAllowedPrompt, 0, len(plan.AllowedPrompts))
		for _, prompt := range plan.AllowedPrompts {
			wire.AllowedPrompts = append(wire.AllowedPrompts, appwire.ApprovalAllowedPrompt{
				Prompt: prompt.Prompt,
			})
		}
	}
	return wire
}

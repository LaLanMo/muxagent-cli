package acp

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/LaLanMo/muxagent-cli/internal/acpprotocol"
	"github.com/LaLanMo/muxagent-cli/internal/domain"
)

func buildApprovalRequest(
	requestID string,
	runtimeID string,
	permReq acpprotocol.RequestPermissionRequest,
	createdAt time.Time,
) domain.ApprovalRequest {
	app := domain.ApprovalApp{
		RequestID:  requestID,
		CreatedAt:  &createdAt,
		Runtime:    runtimeID,
		ToolCallID: permReq.ToolCall.ToolCallID,
	}

	if permReq.ToolCall.Title != nil {
		app.Title = *permReq.ToolCall.Title
	}
	if permReq.ToolCall.Kind != nil {
		app.ToolKind = string(*permReq.ToolCall.Kind)
	}

	app.BodyText = flattenToolCallContent(permReq.ToolCall.Content)
	normalizePlanFields(&app, permReq.ToolCall.RawInput)

	switch runtimeID {
	case "codex":
		normalizeCodexApproval(&app, permReq.ToolCall.RawInput)
	}

	return domain.ApprovalRequest{
		ACP: &permReq,
		App: app,
	}
}

func selectedPermissionResponse(optionID string) acpprotocol.RequestPermissionResponse {
	return acpprotocol.RequestPermissionResponse{
		Outcome: acpprotocol.RequestPermissionOutcome{
			Outcome:  "selected",
			OptionID: &optionID,
		},
	}
}

func cancelledPermissionResponse() acpprotocol.RequestPermissionResponse {
	return acpprotocol.RequestPermissionResponse{
		Outcome: acpprotocol.RequestPermissionOutcome{
			Outcome: "cancelled",
		},
	}
}

func flattenToolCallContent(content []json.RawMessage) string {
	lines := make([]string, 0, len(content))
	for _, item := range content {
		var payload struct {
			Type    string          `json:"type"`
			Content json.RawMessage `json:"content"`
		}
		if err := json.Unmarshal(item, &payload); err != nil {
			continue
		}
		if payload.Type != "content" {
			continue
		}

		var block struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}
		if err := json.Unmarshal(payload.Content, &block); err != nil {
			continue
		}
		if block.Type != "text" {
			continue
		}
		text := strings.TrimSpace(block.Text)
		if text == "" {
			continue
		}
		lines = append(lines, text)
	}
	return strings.Join(lines, "\n\n")
}

type stringListOrString []string

func (s *stringListOrString) UnmarshalJSON(data []byte) error {
	var list []string
	if err := json.Unmarshal(data, &list); err == nil {
		*s = list
		return nil
	}

	var single string
	if err := json.Unmarshal(data, &single); err == nil {
		if single == "" {
			*s = nil
		} else {
			*s = []string{single}
		}
		return nil
	}

	*s = nil
	return nil
}

type codexApprovalInput struct {
	Command stringListOrString `json:"command"`
	CWD     string             `json:"cwd"`
	Reason  string             `json:"reason"`
}

func normalizeCodexApproval(app *domain.ApprovalApp, rawInput json.RawMessage) {
	if len(rawInput) == 0 {
		return
	}

	var input codexApprovalInput
	if err := json.Unmarshal(rawInput, &input); err != nil {
		return
	}

	if input.CWD != "" {
		app.CWD = input.CWD
	}
	if strings.TrimSpace(input.Reason) != "" {
		app.Reason = strings.TrimSpace(input.Reason)
	}
	if len(input.Command) == 0 {
		return
	}

	argv := make([]string, 0, len(input.Command))
	for _, arg := range input.Command {
		if arg == "" {
			continue
		}
		argv = append(argv, arg)
	}
	if len(argv) == 0 {
		return
	}

	app.Command = &domain.ApprovalCommand{
		Argv:    argv,
		Display: formatCommandDisplay(argv, input.CWD),
	}
}

type rawInputPlan struct {
	Plan           string `json:"plan"`
	AllowedPrompts []struct {
		Prompt string `json:"prompt"`
	} `json:"allowedPrompts"`
}

func normalizePlanFields(app *domain.ApprovalApp, rawInput json.RawMessage) {
	if len(rawInput) == 0 {
		return
	}

	var input rawInputPlan
	if err := json.Unmarshal(rawInput, &input); err != nil {
		return
	}

	if strings.TrimSpace(input.Plan) == "" && len(input.AllowedPrompts) == 0 {
		return
	}

	plan := &domain.ApprovalPlan{
		Markdown: strings.TrimSpace(input.Plan),
	}
	for _, prompt := range input.AllowedPrompts {
		text := strings.TrimSpace(prompt.Prompt)
		if text == "" {
			continue
		}
		plan.AllowedPrompts = append(plan.AllowedPrompts, domain.ApprovalAllowedPrompt{
			Prompt: text,
		})
	}
	app.Plan = plan
}

func formatCommandDisplay(argv []string, cwd string) string {
	if len(argv) == 0 {
		return ""
	}

	parts := make([]string, 0, len(argv))
	for _, arg := range argv {
		display := arg
		if cwd != "" {
			prefix := cwd
			if !strings.HasSuffix(prefix, "/") {
				prefix += "/"
			}
			if strings.HasPrefix(display, prefix) {
				display = strings.TrimPrefix(display, prefix)
			}
		}
		parts = append(parts, quoteCommandArg(display))
	}
	return strings.Join(parts, " ")
}

func quoteCommandArg(arg string) string {
	if arg == "" {
		return "''"
	}
	if strings.ContainsAny(arg, " \t\n\"'`$") {
		return strconv.Quote(arg)
	}
	return arg
}

package taskexecutor

import (
	"fmt"
	"path/filepath"
	"strings"
	"unicode"
)

type StreamEventKind string

const (
	StreamEventKindRaw     StreamEventKind = "raw"
	StreamEventKindMessage StreamEventKind = "message"
	StreamEventKindTool    StreamEventKind = "tool"
	StreamEventKindPlan    StreamEventKind = "plan"
	StreamEventKindUsage   StreamEventKind = "usage"
)

type MessageRole string

const (
	MessageRoleAssistant MessageRole = "assistant"
	MessageRoleUser      MessageRole = "user"
)

type MessagePartType string

const (
	MessagePartTypeText      MessagePartType = "text"
	MessagePartTypeReasoning MessagePartType = "reasoning"
)

type ToolKind string

const (
	ToolKindShell            ToolKind = "shell"
	ToolKindSearch           ToolKind = "search"
	ToolKindRead             ToolKind = "read"
	ToolKindEdit             ToolKind = "edit"
	ToolKindWrite            ToolKind = "write"
	ToolKindFetch            ToolKind = "fetch"
	ToolKindFileChange       ToolKind = "file_change"
	ToolKindStructuredOutput ToolKind = "structured_output"
	ToolKindOther            ToolKind = "other"
)

type ToolStatus string

const (
	ToolStatusPending    ToolStatus = "pending"
	ToolStatusInProgress ToolStatus = "in_progress"
	ToolStatusCompleted  ToolStatus = "completed"
	ToolStatusFailed     ToolStatus = "failed"
)

type MessagePart struct {
	MessageID string
	PartID    string
	Role      MessageRole
	Type      MessagePartType
	Text      string
}

type ToolDiff struct {
	Path    string
	OldText *string
	NewText string
}

type ToolCall struct {
	CallID        string
	ParentCallID  string
	Name          string
	Kind          ToolKind
	Title         string
	Status        ToolStatus
	InputSummary  string
	OutputText    string
	ErrorText     string
	Paths         []string
	Diffs         []ToolDiff
	RawInputJSON  string
	RawOutputJSON string
}

type PlanStep struct {
	Text   string
	Status string
}

type PlanSnapshot struct {
	PlanID string
	Steps  []PlanStep
}

type UsageSnapshot struct {
	InputTokens       int64
	CachedInputTokens int64
	OutputTokens      int64
	TotalTokens       int64
	DurationMS        int64
}

type StreamEvent struct {
	Kind      StreamEventKind
	SessionID string
	Raw       string
	Message   *MessagePart
	Tool      *ToolCall
	Plan      *PlanSnapshot
	Usage     *UsageSnapshot
}

func (e StreamEvent) StableKey() string {
	switch e.Kind {
	case StreamEventKindMessage:
		if e.Message == nil {
			return ""
		}
		if e.Message.MessageID != "" || e.Message.PartID != "" {
			return fmt.Sprintf("message:%s:%s:%s", e.Message.Role, e.Message.MessageID, e.Message.PartID)
		}
		return fmt.Sprintf("message:%s:%s:%s", e.Message.Role, e.Message.Type, collapseWhitespace(e.Message.Text))
	case StreamEventKindTool:
		if e.Tool == nil {
			return ""
		}
		if e.Tool.CallID != "" {
			return "tool:" + e.Tool.CallID
		}
		return fmt.Sprintf("tool:%s:%s:%s", e.Tool.Kind, e.Tool.Name, collapseWhitespace(e.Tool.InputSummary))
	case StreamEventKindPlan:
		if e.Plan == nil {
			return ""
		}
		if e.Plan.PlanID != "" {
			return "plan:" + e.Plan.PlanID
		}
		return "plan"
	case StreamEventKindUsage:
		return "usage"
	case StreamEventKindRaw:
		if e.Raw != "" {
			return "raw:" + e.Raw
		}
	}
	return ""
}

func (e StreamEvent) Summary() string {
	switch e.Kind {
	case StreamEventKindMessage:
		if e.Message == nil {
			return ""
		}
		text := collapseWhitespace(e.Message.Text)
		if text == "" {
			return ""
		}
		switch e.Message.Type {
		case MessagePartTypeReasoning:
			return "thinking: " + text
		default:
			if e.Message.Role == MessageRoleUser {
				return "user: " + text
			}
			return "assistant: " + text
		}
	case StreamEventKindTool:
		if e.Tool == nil {
			return ""
		}
		label := e.Tool.DisplayLabel()
		subject := e.Tool.DisplaySubject()
		status := toolStatusText(e.Tool.Status)
		switch {
		case label != "" && status != "" && subject != "":
			return fmt.Sprintf("%s %s: %s", label, status, subject)
		case label != "" && subject != "":
			return fmt.Sprintf("%s: %s", label, subject)
		case label != "" && status != "":
			return fmt.Sprintf("%s %s", label, status)
		case subject != "":
			return subject
		default:
			return label
		}
	case StreamEventKindPlan:
		if e.Plan == nil {
			return ""
		}
		total := len(e.Plan.Steps)
		if total == 0 {
			return "plan updated"
		}
		completed := 0
		next := ""
		for _, step := range e.Plan.Steps {
			if strings.EqualFold(step.Status, "completed") {
				completed++
				continue
			}
			if next == "" {
				next = collapseWhitespace(step.Text)
			}
		}
		if next != "" {
			return fmt.Sprintf("plan: %d/%d complete, next %s", completed, total, next)
		}
		return fmt.Sprintf("plan: %d/%d complete", completed, total)
	case StreamEventKindUsage:
		if e.Usage == nil {
			return ""
		}
		total := e.Usage.TotalTokens
		if total == 0 {
			total = e.Usage.InputTokens + e.Usage.OutputTokens
		}
		switch {
		case e.Usage.InputTokens > 0 || e.Usage.OutputTokens > 0:
			return fmt.Sprintf("usage: %d in, %d out", e.Usage.InputTokens, e.Usage.OutputTokens)
		case total > 0:
			return fmt.Sprintf("usage: %d tokens", total)
		}
	case StreamEventKindRaw:
		return collapseWhitespace(e.Raw)
	}
	return ""
}

func MergeStreamEvent(existing, next StreamEvent) StreamEvent {
	merged := existing
	if next.Kind != "" {
		merged.Kind = next.Kind
	}
	if next.SessionID != "" {
		merged.SessionID = next.SessionID
	}
	if next.Raw != "" {
		merged.Raw = next.Raw
	}
	if next.Message != nil {
		if merged.Message == nil {
			message := *next.Message
			merged.Message = &message
		} else {
			if next.Message.MessageID != "" {
				merged.Message.MessageID = next.Message.MessageID
			}
			if next.Message.PartID != "" {
				merged.Message.PartID = next.Message.PartID
			}
			if next.Message.Role != "" {
				merged.Message.Role = next.Message.Role
			}
			if next.Message.Type != "" {
				merged.Message.Type = next.Message.Type
			}
			if next.Message.Text != "" {
				merged.Message.Text = next.Message.Text
			}
		}
	}
	if next.Tool != nil {
		if merged.Tool == nil {
			tool := *next.Tool
			merged.Tool = &tool
		} else {
			if next.Tool.CallID != "" {
				merged.Tool.CallID = next.Tool.CallID
			}
			if next.Tool.ParentCallID != "" {
				merged.Tool.ParentCallID = next.Tool.ParentCallID
			}
			if shouldReplaceToolName(merged.Tool.Name, next.Tool.Name) {
				merged.Tool.Name = next.Tool.Name
			}
			if shouldReplaceToolKind(merged.Tool.Kind, next.Tool.Kind) {
				merged.Tool.Kind = next.Tool.Kind
			}
			if next.Tool.Title != "" {
				merged.Tool.Title = next.Tool.Title
			}
			if next.Tool.Status != "" {
				merged.Tool.Status = next.Tool.Status
			}
			if shouldReplaceToolInputSummary(merged.Tool.InputSummary, next.Tool.InputSummary) {
				merged.Tool.InputSummary = next.Tool.InputSummary
			}
			if next.Tool.OutputText != "" {
				merged.Tool.OutputText = next.Tool.OutputText
			}
			if next.Tool.ErrorText != "" {
				merged.Tool.ErrorText = next.Tool.ErrorText
			}
			if len(next.Tool.Paths) > 0 {
				merged.Tool.Paths = append([]string(nil), next.Tool.Paths...)
			}
			if len(next.Tool.Diffs) > 0 {
				merged.Tool.Diffs = append([]ToolDiff(nil), next.Tool.Diffs...)
			}
			if next.Tool.RawInputJSON != "" {
				merged.Tool.RawInputJSON = next.Tool.RawInputJSON
			}
			if next.Tool.RawOutputJSON != "" {
				merged.Tool.RawOutputJSON = next.Tool.RawOutputJSON
			}
		}
	}
	if next.Plan != nil {
		plan := *next.Plan
		merged.Plan = &plan
	}
	if next.Usage != nil {
		usage := *next.Usage
		merged.Usage = &usage
	}
	return merged
}

func (t *ToolCall) DisplayLabel() string {
	if t == nil {
		return ""
	}
	return toolLabel(t.Kind, t.Name)
}

func (t *ToolCall) DisplaySubject() string {
	if t == nil {
		return ""
	}
	subject := collapseWhitespace(t.InputSummary)
	if subject == "" && len(t.Paths) > 0 {
		subject = strings.Join(compactPaths(t.Paths), ", ")
	}
	if subject == "" {
		subject = collapseWhitespace(t.Title)
	}
	return subject
}

func toolLabel(kind ToolKind, fallback string) string {
	switch kind {
	case ToolKindShell:
		return "shell"
	case ToolKindSearch:
		return "search"
	case ToolKindRead:
		return "read"
	case ToolKindEdit:
		return "edit"
	case ToolKindWrite:
		return "write"
	case ToolKindFetch:
		return "fetch"
	case ToolKindFileChange:
		return "files"
	case ToolKindStructuredOutput:
		return "structured output"
	default:
		if label := prettifyToolName(fallback); label != "" {
			return label
		}
		if kind == ToolKindOther || kind == "" {
			return "tool"
		}
		return prettifyToolName(string(kind))
	}
}

func shouldReplaceToolKind(existing, next ToolKind) bool {
	if next == "" || next == existing {
		return false
	}
	if existing == "" {
		return true
	}
	return toolKindSpecificity(next) > toolKindSpecificity(existing)
}

func toolKindSpecificity(kind ToolKind) int {
	switch kind {
	case "", ToolKindOther:
		return 0
	default:
		return 1
	}
}

func shouldReplaceToolName(existing, next string) bool {
	existing = strings.TrimSpace(existing)
	next = strings.TrimSpace(next)
	if next == "" || strings.EqualFold(existing, next) {
		return false
	}
	if existing == "" {
		return true
	}
	if isGenericToolName(existing) && !isGenericToolName(next) {
		return true
	}
	return false
}

func isGenericToolName(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "", "tool", "tool_result":
		return true
	default:
		return false
	}
}

func shouldReplaceToolInputSummary(existing, next string) bool {
	existing = strings.TrimSpace(existing)
	next = strings.TrimSpace(next)
	if next == "" {
		return false
	}
	if existing == "" {
		return true
	}
	return toolInputSummarySpecificity(next) > toolInputSummarySpecificity(existing)
}

func toolInputSummarySpecificity(summary string) int {
	summary = strings.TrimSpace(summary)
	if summary == "" {
		return 0
	}
	if strings.HasPrefix(summary, "{") || strings.HasPrefix(summary, "[") {
		return 1
	}
	return 2
}

func prettifyToolName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	var builder strings.Builder
	runes := []rune(name)
	for i, r := range runes {
		if r == '_' || r == '-' || r == '.' {
			if builder.Len() > 0 {
				last, _ := utf8LastRune(&builder)
				if last != ' ' {
					builder.WriteRune(' ')
				}
			}
			continue
		}
		if shouldInsertToolNameSpace(runes, i) {
			last, _ := utf8LastRune(&builder)
			if last != ' ' {
				builder.WriteRune(' ')
			}
		}
		builder.WriteRune(r)
	}
	return strings.ToLower(strings.Join(strings.Fields(builder.String()), " "))
}

func shouldInsertToolNameSpace(runes []rune, idx int) bool {
	if idx <= 0 || idx >= len(runes) {
		return false
	}
	current := runes[idx]
	if !unicode.IsUpper(current) {
		return false
	}
	prev := runes[idx-1]
	if prev == '_' || prev == '-' || prev == '.' || unicode.IsSpace(prev) {
		return false
	}
	if unicode.IsLower(prev) || unicode.IsDigit(prev) {
		return true
	}
	if idx+1 < len(runes) && unicode.IsLower(runes[idx+1]) {
		return unicode.IsUpper(prev)
	}
	return false
}

func utf8LastRune(builder *strings.Builder) (rune, bool) {
	text := builder.String()
	if text == "" {
		return 0, false
	}
	runes := []rune(text)
	return runes[len(runes)-1], true
}

func toolStatusText(status ToolStatus) string {
	switch status {
	case ToolStatusInProgress, ToolStatusPending:
		return "running"
	case ToolStatusFailed:
		return "failed"
	default:
		return ""
	}
}

func compactPaths(paths []string) []string {
	items := make([]string, 0, len(paths))
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		base := filepath.Base(path)
		if base == "." || base == string(filepath.Separator) || base == "" {
			items = append(items, path)
			continue
		}
		items = append(items, base)
	}
	return items
}

func collapseWhitespace(text string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
}

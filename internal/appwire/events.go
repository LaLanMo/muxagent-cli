package appwire

import (
	"encoding/json"
	"time"

	"github.com/LaLanMo/muxagent-cli/internal/acpprotocol"
)

type EventType string

const (
	EventMessageDelta      EventType = "message.delta"
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
	PartID     string          `json:"partId"`
	MessageID  string          `json:"messageId"`
	CallID     string          `json:"callId"`
	Name       string          `json:"name"`
	Kind       string          `json:"kind,omitempty"`
	Title      string          `json:"title,omitempty"`
	Status     ToolStatus      `json:"status"`
	Input      *ToolInput      `json:"input,omitempty"`
	Output     string          `json:"output,omitempty"`
	Error      string          `json:"error,omitempty"`
	Diffs      []ToolDiff      `json:"diffs,omitempty"`
	ClaudeCode *ClaudeCodeTool `json:"claudeCode,omitempty"`
	Locations  []ToolLocation  `json:"locations,omitempty"`
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

type RunFailedEventApp struct {
	Error SessionError `json:"error"`
}

type RunFailedEvent struct {
	App RunFailedEventApp `json:"app"`
}

type SessionStatusEventApp struct {
	ID        string        `json:"id"`
	Title     string        `json:"title"`
	Status    SessionStatus `json:"status"`
	Model     string        `json:"model,omitempty"`
	Cost      *CostInfo     `json:"cost,omitempty"`
	MachineID string        `json:"machineId,omitempty"`
	Runtime   string        `json:"runtime,omitempty"`
	CWD       string        `json:"cwd,omitempty"`
	Mode      string        `json:"mode,omitempty"`
	CreatedAt time.Time     `json:"createdAt"`
	UpdatedAt time.Time     `json:"updatedAt"`
}

type SessionStatusEvent struct {
	App SessionStatusEventApp `json:"app"`
}

type ModeChangedEventApp struct {
	CurrentModeID string `json:"currentModeId"`
}

type ModeChangedEvent struct {
	ACPCurrentMode  *acpprotocol.CurrentModeUpdate  `json:"-"`
	ACPConfigOption *acpprotocol.ConfigOptionUpdate `json:"-"`
	App             ModeChangedEventApp             `json:"app"`
}

type modeChangedEventWire struct {
	App ModeChangedEventApp `json:"app"`
	ACP json.RawMessage     `json:"acp,omitempty"`
}

func (e ModeChangedEvent) MarshalJSON() ([]byte, error) {
	wire := modeChangedEventWire{App: e.App}

	switch {
	case e.ACPCurrentMode != nil:
		payload, err := json.Marshal(e.ACPCurrentMode)
		if err != nil {
			return nil, err
		}
		wire.ACP = payload
	case e.ACPConfigOption != nil:
		payload, err := json.Marshal(e.ACPConfigOption)
		if err != nil {
			return nil, err
		}
		wire.ACP = payload
	}

	return json.Marshal(wire)
}

type SessionConfigValue struct {
	Value       string  `json:"value"`
	Name        string  `json:"name"`
	Description *string `json:"description,omitempty"`
}

type ConfigChangedEventApp struct {
	ConfigID     string               `json:"configId"`
	CurrentValue string               `json:"currentValue"`
	Category     string               `json:"category,omitempty"`
	Values       []SessionConfigValue `json:"values,omitempty"`
}

type ConfigChangedEvent struct {
	ACP *acpprotocol.ConfigOptionUpdate `json:"-"`
	App ConfigChangedEventApp           `json:"app"`
}

type configChangedEventWire struct {
	App ConfigChangedEventApp `json:"app"`
	ACP json.RawMessage       `json:"acp,omitempty"`
}

func (e ConfigChangedEvent) MarshalJSON() ([]byte, error) {
	wire := configChangedEventWire{App: e.App}
	if e.ACP != nil {
		payload, err := json.Marshal(e.ACP)
		if err != nil {
			return nil, err
		}
		wire.ACP = payload
	}
	return json.Marshal(wire)
}

type Event struct {
	Type      EventType
	SessionID string
	Seq       uint64
	At        time.Time

	MessagePart   *MessagePartEvent
	Tool          *ToolEvent
	Approval      *ApprovalRequest
	Plan          *PlanEvent
	Usage         *UsageEvent
	RunFinished   *RunFinishedEvent
	RunFailed     *RunFailedEvent
	SessionInfo   *SessionStatusEvent
	ModeChanged   *ModeChangedEvent
	ConfigChanged *ConfigChangedEvent
}

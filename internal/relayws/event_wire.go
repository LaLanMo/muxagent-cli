package relayws

import (
	"encoding/json"
	"time"

	"github.com/LaLanMo/muxagent-cli/internal/appwire"
)

type eventEnvelope struct {
	Type          appwire.EventType           `json:"type"`
	SessionID     string                      `json:"sessionId,omitempty"`
	Seq           uint64                      `json:"seq"`
	At            time.Time                   `json:"at"`
	MessagePart   *appwire.MessagePartEvent   `json:"messagePart,omitempty"`
	Tool          *appwire.ToolEvent          `json:"tool,omitempty"`
	Approval      *appwire.ApprovalRequest    `json:"approval,omitempty"`
	Plan          *appwire.PlanEvent          `json:"plan,omitempty"`
	Usage         *appwire.UsageEvent         `json:"usage,omitempty"`
	RunFinished   *appwire.RunFinishedEvent   `json:"runFinished,omitempty"`
	RunFailed     *appwire.RunFailedEvent     `json:"runFailed,omitempty"`
	SessionInfo   *appwire.SessionStatusEvent `json:"sessionStatus,omitempty"`
	ModeChanged   *appwire.ModeChangedEvent   `json:"modeChanged,omitempty"`
	ConfigChanged *appwire.ConfigChangedEvent `json:"configChanged,omitempty"`
}

func marshalEvent(event appwire.Event) ([]byte, error) {
	envelope := eventEnvelope{
		Type:          event.Type,
		SessionID:     event.SessionID,
		Seq:           event.Seq,
		At:            event.At,
		MessagePart:   event.MessagePart,
		Tool:          event.Tool,
		Approval:      event.Approval,
		Plan:          event.Plan,
		Usage:         event.Usage,
		RunFinished:   event.RunFinished,
		RunFailed:     event.RunFailed,
		SessionInfo:   event.SessionInfo,
		ModeChanged:   event.ModeChanged,
		ConfigChanged: event.ConfigChanged,
	}
	return json.Marshal(envelope)
}

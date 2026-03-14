package relayws

import (
	"encoding/json"
	"time"

	"github.com/LaLanMo/muxagent-cli/internal/domain"
)

type eventEnvelope struct {
	Type          domain.EventType           `json:"type"`
	SessionID     string                     `json:"sessionId,omitempty"`
	Seq           uint64                     `json:"seq"`
	At            time.Time                  `json:"at"`
	MessagePart   *domain.MessagePartEvent   `json:"messagePart,omitempty"`
	Tool          *domain.ToolEvent          `json:"tool,omitempty"`
	Approval      *domain.ApprovalRequest    `json:"approval,omitempty"`
	Plan          *domain.PlanEvent          `json:"plan,omitempty"`
	Usage         *domain.UsageEvent         `json:"usage,omitempty"`
	RunFinished   *domain.RunFinishedEvent   `json:"runFinished,omitempty"`
	RunFailed     *domain.RunFailedEvent     `json:"runFailed,omitempty"`
	SessionInfo   *domain.SessionStatusEvent `json:"sessionStatus,omitempty"`
	ModeChanged   *modeChangedWire           `json:"modeChanged,omitempty"`
	ConfigChanged *configChangedWire         `json:"configChanged,omitempty"`
}

type modeChangedWire struct {
	App domain.ModeChangedEventApp `json:"app"`
	ACP any                        `json:"acp,omitempty"`
}

type configChangedWire struct {
	App domain.ConfigChangedEventApp `json:"app"`
	ACP any                          `json:"acp,omitempty"`
}

func marshalEvent(event domain.Event) ([]byte, error) {
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
		ModeChanged:   modeChangedWireFromDomain(event.ModeChanged),
		ConfigChanged: configChangedWireFromDomain(event.ConfigChanged),
	}
	return json.Marshal(envelope)
}

func modeChangedWireFromDomain(event *domain.ModeChangedEvent) *modeChangedWire {
	if event == nil {
		return nil
	}

	var acp any
	switch {
	case event.ACPCurrentMode != nil:
		acp = event.ACPCurrentMode
	case event.ACPConfigOption != nil:
		acp = event.ACPConfigOption
	}

	return &modeChangedWire{
		App: event.App,
		ACP: acp,
	}
}

func configChangedWireFromDomain(event *domain.ConfigChangedEvent) *configChangedWire {
	if event == nil {
		return nil
	}

	var acp any
	if event.ACP != nil {
		acp = event.ACP
	}

	return &configChangedWire{
		App: event.App,
		ACP: acp,
	}
}

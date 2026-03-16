package relayws

import (
	"encoding/json"
	"time"

	"github.com/LaLanMo/muxagent-cli/internal/appwire"
	"github.com/LaLanMo/muxagent-cli/internal/domain"
)

type eventEnvelope struct {
	Type          appwire.EventType           `json:"type"`
	SessionID     string                      `json:"sessionId,omitempty"`
	Seq           uint64                      `json:"seq"`
	At            time.Time                   `json:"at"`
	MessagePart   *appwire.MessagePartEvent   `json:"messagePart,omitempty"`
	Tool          *appwire.ToolEvent          `json:"tool,omitempty"`
	Approval      *domain.ApprovalRequest     `json:"approval,omitempty"`
	Plan          *appwire.PlanEvent          `json:"plan,omitempty"`
	Usage         *appwire.UsageEvent         `json:"usage,omitempty"`
	RunFinished   *appwire.RunFinishedEvent   `json:"runFinished,omitempty"`
	RunFailed     *appwire.RunFailedEvent     `json:"runFailed,omitempty"`
	SessionInfo   *appwire.SessionStatusEvent `json:"sessionStatus,omitempty"`
	ModeChanged   *modeChangedWire            `json:"modeChanged,omitempty"`
	ConfigChanged *configChangedWire          `json:"configChanged,omitempty"`
}

type modeChangedWire struct {
	App appwire.ModeChangedEventApp `json:"app"`
	ACP any                         `json:"acp,omitempty"`
}

type configChangedWire struct {
	App appwire.ConfigChangedEventApp `json:"app"`
	ACP any                           `json:"acp,omitempty"`
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
		ModeChanged:   modeChangedWireFromDomain(event.ModeChanged),
		ConfigChanged: configChangedWireFromDomain(event.ConfigChanged),
	}
	return json.Marshal(envelope)
}

func modeChangedWireFromDomain(event *appwire.ModeChangedEvent) *modeChangedWire {
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

func configChangedWireFromDomain(event *appwire.ConfigChangedEvent) *configChangedWire {
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

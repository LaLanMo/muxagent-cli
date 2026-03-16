package acp

import (
	"encoding/json"
	"time"

	"github.com/LaLanMo/muxagent-cli/internal/acpprotocol"
	"github.com/LaLanMo/muxagent-cli/internal/appwire"
)

func messagePartEvent(
	eventType appwire.EventType,
	sessionID string,
	update *acpprotocol.ContentChunk,
	app appwire.MessagePartEventApp,
) appwire.Event {
	return appwire.Event{
		Type:      eventType,
		SessionID: sessionID,
		At:        time.Now(),
		MessagePart: &appwire.MessagePartEvent{
			ACP: update,
			App: app,
		},
	}
}

func contentChunkDisplayText(raw json.RawMessage) string {
	var content struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &content); err != nil {
		return ""
	}

	switch content.Type {
	case "", "text":
		return content.Text
	case "image":
		return "[Image]"
	case "audio":
		return "[Audio]"
	case "resource", "resource_link":
		return "[Resource]"
	default:
		return "[Content]"
	}
}

package acpprotocol

import (
	"encoding/json"
	"fmt"
)

type ContentChunk struct {
	Meta          Meta            `json:"_meta,omitempty"`
	SessionUpdate string          `json:"sessionUpdate,omitempty"`
	MessageID     *string         `json:"messageId,omitempty"`
	Content       json.RawMessage `json:"content"`
}

func (c *ContentChunk) UnmarshalJSON(data []byte) error {
	type alias ContentChunk
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	if _, ok := fields["content"]; !ok {
		return fmt.Errorf("missing required field %q", "content")
	}

	var decoded alias
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*c = ContentChunk(decoded)
	return nil
}

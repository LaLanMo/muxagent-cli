package acpprotocol

import (
	"bytes"
	"encoding/json"
)

type SessionMode struct {
	Meta        Meta    `json:"_meta,omitempty"`
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Description *string `json:"description,omitempty"`
}

type SessionModeState struct {
	Meta           Meta          `json:"_meta,omitempty"`
	CurrentModeID  string        `json:"currentModeId"`
	AvailableModes []SessionMode `json:"availableModes"`
}

type SessionConfigSelectOption struct {
	Meta        Meta    `json:"_meta,omitempty"`
	Value       string  `json:"value"`
	Name        string  `json:"name"`
	Description *string `json:"description,omitempty"`
}

type SessionConfigSelectGroup struct {
	Meta    Meta                        `json:"_meta,omitempty"`
	Group   string                      `json:"group"`
	Name    string                      `json:"name"`
	Options []SessionConfigSelectOption `json:"options"`
}

type SessionConfigSelectOptions struct {
	Ungrouped []SessionConfigSelectOption
	Grouped   []SessionConfigSelectGroup
}

func (o *SessionConfigSelectOptions) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		*o = SessionConfigSelectOptions{}
		return nil
	}

	var rawItems []json.RawMessage
	if err := json.Unmarshal(trimmed, &rawItems); err != nil {
		return err
	}
	if len(rawItems) == 0 {
		*o = SessionConfigSelectOptions{}
		return nil
	}

	var probe map[string]json.RawMessage
	if err := json.Unmarshal(rawItems[0], &probe); err != nil {
		return err
	}
	if _, hasGroup := probe["group"]; hasGroup {
		var grouped []SessionConfigSelectGroup
		if err := json.Unmarshal(trimmed, &grouped); err != nil {
			return err
		}
		*o = SessionConfigSelectOptions{Grouped: grouped}
		return nil
	}

	var ungrouped []SessionConfigSelectOption
	if err := json.Unmarshal(trimmed, &ungrouped); err != nil {
		return err
	}
	*o = SessionConfigSelectOptions{Ungrouped: ungrouped}
	return nil
}

func (o SessionConfigSelectOptions) MarshalJSON() ([]byte, error) {
	if len(o.Grouped) > 0 {
		return json.Marshal(o.Grouped)
	}
	return json.Marshal(o.Ungrouped)
}

func (o SessionConfigSelectOptions) Flatten() []SessionConfigSelectOption {
	if len(o.Grouped) == 0 {
		return append([]SessionConfigSelectOption(nil), o.Ungrouped...)
	}
	flat := make([]SessionConfigSelectOption, 0, len(o.Grouped)*2)
	for _, group := range o.Grouped {
		flat = append(flat, group.Options...)
	}
	return flat
}

type SessionConfigOption struct {
	Meta         Meta                       `json:"_meta,omitempty"`
	ID           string                     `json:"id"`
	Name         string                     `json:"name"`
	Description  *string                    `json:"description,omitempty"`
	Category     *string                    `json:"category,omitempty"`
	Type         string                     `json:"type"`
	CurrentValue string                     `json:"currentValue"`
	Options      SessionConfigSelectOptions `json:"options"`
}

type ConfigOptionUpdate struct {
	Meta          Meta                  `json:"_meta,omitempty"`
	SessionUpdate string                `json:"sessionUpdate,omitempty"`
	ConfigOptions []SessionConfigOption `json:"configOptions"`
}

type CurrentModeUpdate struct {
	Meta          Meta   `json:"_meta,omitempty"`
	SessionUpdate string `json:"sessionUpdate,omitempty"`
	CurrentModeID string `json:"currentModeId"`
}

type NewSessionResponse struct {
	Meta          Meta                  `json:"_meta,omitempty"`
	SessionID     string                `json:"sessionId"`
	Modes         *SessionModeState     `json:"modes,omitempty"`
	ConfigOptions []SessionConfigOption `json:"configOptions,omitempty"`
}

type LoadSessionResponse struct {
	Meta          Meta                  `json:"_meta,omitempty"`
	Modes         *SessionModeState     `json:"modes,omitempty"`
	ConfigOptions []SessionConfigOption `json:"configOptions,omitempty"`
}

type SetSessionConfigOptionResponse struct {
	Meta          Meta                  `json:"_meta,omitempty"`
	ConfigOptions []SessionConfigOption `json:"configOptions"`
}

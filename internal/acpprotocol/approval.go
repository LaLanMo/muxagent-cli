package acpprotocol

import "encoding/json"

type Meta map[string]any

type PermissionOptionKind string

const (
	PermissionOptionKindAllowOnce    PermissionOptionKind = "allow_once"
	PermissionOptionKindAllowAlways  PermissionOptionKind = "allow_always"
	PermissionOptionKindRejectOnce   PermissionOptionKind = "reject_once"
	PermissionOptionKindRejectAlways PermissionOptionKind = "reject_always"
)

type ToolKind string

type ToolCallStatus string

type PermissionOption struct {
	Meta     Meta                 `json:"_meta,omitempty"`
	OptionID string               `json:"optionId"`
	Name     string               `json:"name"`
	Kind     PermissionOptionKind `json:"kind"`
}

type ToolCallLocation struct {
	Meta Meta    `json:"_meta,omitempty"`
	Path string  `json:"path"`
	Line *uint32 `json:"line,omitempty"`
}

type ToolCallUpdate struct {
	Meta       Meta               `json:"_meta,omitempty"`
	ToolCallID string             `json:"toolCallId"`
	Title      *string            `json:"title,omitempty"`
	Kind       *ToolKind          `json:"kind,omitempty"`
	Status     *ToolCallStatus    `json:"status,omitempty"`
	Content    []json.RawMessage  `json:"content,omitempty"`
	Locations  []ToolCallLocation `json:"locations,omitempty"`
	RawInput   json.RawMessage    `json:"rawInput,omitempty"`
	RawOutput  json.RawMessage    `json:"rawOutput,omitempty"`
}

type RequestPermissionRequest struct {
	Meta      Meta               `json:"_meta,omitempty"`
	SessionID string             `json:"sessionId"`
	ToolCall  ToolCallUpdate     `json:"toolCall"`
	Options   []PermissionOption `json:"options"`
}

type RequestPermissionOutcome struct {
	Meta     Meta    `json:"_meta,omitempty"`
	Outcome  string  `json:"outcome"`
	OptionID *string `json:"optionId,omitempty"`
}

type RequestPermissionResponse struct {
	Meta    Meta                     `json:"_meta,omitempty"`
	Outcome RequestPermissionOutcome `json:"outcome"`
}

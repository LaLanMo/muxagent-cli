package domain

import "time"

type SessionStatus string

const (
	SessionStatusRunning         SessionStatus = "running"
	SessionStatusWaitingApproval SessionStatus = "waiting_approval"
	SessionStatusError           SessionStatus = "error"
	SessionStatusDone            SessionStatus = "done"
)

type PartType string

const (
	PartTypeText       PartType = "text"
	PartTypeImage      PartType = "image"
	PartTypeAudio      PartType = "audio"
	PartTypeFile       PartType = "file"
	PartTypeToolCall   PartType = "tool_call"
	PartTypeToolResult PartType = "tool_result"
	PartTypeReasoning  PartType = "reasoning"
	PartTypeData       PartType = "data"
)

type MessageRole string

const (
	MessageRoleUser   MessageRole = "user"
	MessageRoleSystem MessageRole = "system"
	MessageRoleAgent  MessageRole = "agent"
)

type MediaPart struct {
	URL      string `json:"url,omitempty"`
	Base64   string `json:"base64,omitempty"`
	MimeType string `json:"mimeType,omitempty"`
	Name     string `json:"name,omitempty"`
	Path     string `json:"path,omitempty"`
	Size     int64  `json:"size,omitempty"`
}

type ToolCallPart struct {
	ID    string         `json:"id,omitempty"`
	Name  string         `json:"name,omitempty"`
	Input map[string]any `json:"input,omitempty"`
}

type ToolResultPart struct {
	ToolCallID string         `json:"toolCallId,omitempty"`
	Content    []MessagePart  `json:"content,omitempty"`
	Output     string         `json:"output,omitempty"`
	Data       map[string]any `json:"data,omitempty"`
}

type MessagePart struct {
	Type       PartType        `json:"type"`
	Text       string          `json:"text,omitempty"`
	Media      *MediaPart      `json:"media,omitempty"`
	ToolCall   *ToolCallPart   `json:"toolCall,omitempty"`
	ToolResult *ToolResultPart `json:"toolResult,omitempty"`
	Data       map[string]any  `json:"data,omitempty"`
}

type Session struct {
	ID        string        `json:"id"`
	Title     string        `json:"title"`
	Status    SessionStatus `json:"status"`
	CreatedAt time.Time     `json:"createdAt"`
	UpdatedAt time.Time     `json:"updatedAt"`
}

type Message struct {
	ID        string         `json:"id"`
	SessionID string         `json:"sessionId"`
	Role      MessageRole    `json:"role"`
	Parts     []MessagePart  `json:"parts"`
	Metadata  map[string]any `json:"metadata,omitempty"`
	CreatedAt time.Time      `json:"createdAt"`
	UpdatedAt time.Time      `json:"updatedAt,omitempty"`
}

type ApprovalRequest struct {
	ID        string         `json:"id"`
	SessionID string         `json:"sessionId"`
	Title     string         `json:"title"`
	Metadata  map[string]any `json:"metadata"`
	Patterns  []string       `json:"patterns"`
	CallID    string         `json:"callId"`
	MessageID string         `json:"messageId"`
	CreatedAt time.Time      `json:"createdAt"`
}

type EventType string

const (
	EventMessageDelta      EventType = "message.delta"
	EventMessageFinal      EventType = "message.final"
	EventApprovalRequested EventType = "approval.requested"
	EventApprovalReplied   EventType = "approval.replied"
	EventSessionStatus     EventType = "session.status"
	EventRunFailed         EventType = "run.failed"
	EventRunFinished       EventType = "run.finished"
	EventConnectionState   EventType = "connection.state"
)

// MessagePartEvent represents a streaming text delta event
type MessagePartEvent struct {
	PartID    string `json:"partId"`
	MessageID string `json:"messageId"`
	Delta     string `json:"delta"`    // Incremental text
	PartType  string `json:"partType"` // "text", "tool", etc.
	FullText  string `json:"fullText"` // Accumulated text so far
}

// SessionError represents an error that occurred in a session
type SessionError struct {
	Name    string `json:"name"`
	Message string `json:"message"`
}

type Event struct {
	Type      EventType `json:"type"`
	SessionID string    `json:"sessionId,omitempty"`
	At        time.Time `json:"at"`

	// Rich event data (only one populated based on Type)
	MessagePart *MessagePartEvent `json:"messagePart,omitempty"`
	Message     *Message          `json:"message,omitempty"`
	Approval    *ApprovalRequest  `json:"approval,omitempty"`
	Session     *Session          `json:"session,omitempty"`
	Error       *SessionError     `json:"error,omitempty"`
}

type ConnectionState string

const (
	ConnectionConnected    ConnectionState = "connected"
	ConnectionDisconnected ConnectionState = "disconnected"
	ConnectionReconnecting ConnectionState = "reconnecting"
)

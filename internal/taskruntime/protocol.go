package taskruntime

import (
	appconfig "github.com/LaLanMo/muxagent-cli/internal/config"
	"github.com/LaLanMo/muxagent-cli/internal/taskconfig"
	"github.com/LaLanMo/muxagent-cli/internal/taskdomain"
)

type CommandType string

const (
	CommandStartTask       CommandType = "task.start"
	CommandSubmitInput     CommandType = "task.submit_input"
	CommandRetryNode       CommandType = "task.retry_node"
	CommandContinueBlocked CommandType = "task.continue_blocked"
	CommandShutdown        CommandType = "task.shutdown"
)

type RunCommand struct {
	Type        CommandType
	TaskID      string
	NodeRunID   string
	Description string
	WorkDir     string
	ConfigPath  string
	Runtime     appconfig.RuntimeID
	Payload     map[string]interface{}
	Force       bool
}

type EventType string

const (
	EventTaskCreated    EventType = "task.created"
	EventNodeStarted    EventType = "node.started"
	EventNodeProgress   EventType = "node.progress"
	EventNodeCompleted  EventType = "node.completed"
	EventNodeFailed     EventType = "node.failed"
	EventInputRequested EventType = "node.input_requested"
	EventTaskCompleted  EventType = "task.completed"
	EventTaskFailed     EventType = "task.failed"
	EventCommandError   EventType = "command.error"
)

type RunEvent struct {
	Type         EventType
	TaskID       string
	NodeRunID    string
	NodeName     string
	TaskView     *taskdomain.TaskView
	Progress     *ProgressInfo
	InputRequest *InputRequest
	Error        *RunError
}

type ProgressInfo struct {
	Message   string
	SessionID string
}

type RunError struct {
	Message string
}

type InputRequest struct {
	Kind          InputKind
	TaskID        string
	NodeRunID     string
	NodeName      string
	Schema        *taskconfig.JSONSchema
	Questions     []taskdomain.ClarificationQuestion
	ArtifactPaths []string
}

type InputKind string

const (
	InputKindHumanNode     InputKind = "human_node"
	InputKindClarification InputKind = "clarification"
)

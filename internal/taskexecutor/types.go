package taskexecutor

import (
	"context"

	"github.com/LaLanMo/muxagent-cli/internal/taskconfig"
	"github.com/LaLanMo/muxagent-cli/internal/taskdomain"
)

type Request struct {
	Task                taskdomain.Task
	NodeRun             taskdomain.NodeRun
	NodeDefinition      taskconfig.NodeDefinition
	ClarificationConfig taskconfig.ClarificationConfig
	ConfigPath          string
	SchemaPath          string
	WorkDir             string
	ArtifactDir         string
	Prompt              string
	ResultSchema        taskconfig.JSONSchema
}

type Progress struct {
	Message   string
	SessionID string
}

type ResultKind string

const (
	ResultKindClarification ResultKind = "clarification"
	ResultKindResult        ResultKind = "result"
)

type Result struct {
	SessionID     string
	Kind          ResultKind
	Result        map[string]interface{}
	Clarification *taskdomain.ClarificationRequest
}

type Executor interface {
	Execute(ctx context.Context, req Request, progress func(Progress)) (Result, error)
}

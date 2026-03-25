package taskexecutor

import (
	"context"
	"fmt"

	appconfig "github.com/LaLanMo/muxagent-cli/internal/config"
)

type Router struct {
	executors map[appconfig.RuntimeID]Executor
}

func NewRouter(codex Executor, claude Executor) *Router {
	return &Router{
		executors: map[appconfig.RuntimeID]Executor{
			appconfig.RuntimeCodex:      codex,
			appconfig.RuntimeClaudeCode: claude,
		},
	}
}

func (r *Router) Execute(ctx context.Context, req Request, progress func(Progress)) (Result, error) {
	runtime := req.Runtime
	if runtime == "" {
		runtime = appconfig.RuntimeCodex
	}
	executor, ok := r.executors[runtime]
	if !ok || executor == nil {
		return Result{}, fmt.Errorf("runtime %q is not configured for task execution", runtime)
	}
	return executor.Execute(ctx, req, progress)
}

package taskexecutor

import (
	"context"
	"fmt"

	appconfig "github.com/LaLanMo/muxagent-cli/internal/config"
)

type Router struct {
	executors map[appconfig.RuntimeID]Executor
}

func NewRouter(codex Executor, claude Executor, opencode Executor) *Router {
	return &Router{
		executors: map[appconfig.RuntimeID]Executor{
			appconfig.RuntimeCodex:      codex,
			appconfig.RuntimeClaudeCode: claude,
			appconfig.RuntimeOpenCode:   opencode,
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

func (r *Router) Close() error {
	if r == nil {
		return nil
	}
	executors := make([]Executor, 0, len(r.executors))
	for _, executor := range r.executors {
		executors = append(executors, executor)
	}
	return CloseAll(executors...)
}

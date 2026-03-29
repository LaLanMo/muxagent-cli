package tasktui

import (
	"context"
	"errors"

	tea "charm.land/bubbletea/v2"

	"github.com/LaLanMo/muxagent-cli/internal/taskconfig"
	"github.com/LaLanMo/muxagent-cli/internal/taskdomain"
	"github.com/LaLanMo/muxagent-cli/internal/taskruntime"
)

type RuntimeService interface {
	Run(ctx context.Context) error
	Events() <-chan taskruntime.RunEvent
	Dispatch(cmd taskruntime.RunCommand)
	ListTaskViews(ctx context.Context, workDir string) ([]taskdomain.TaskView, error)
	LoadTaskView(ctx context.Context, taskID string) (taskdomain.TaskView, *taskconfig.Config, error)
	BuildInputRequest(ctx context.Context, taskID, nodeRunID string) (*taskruntime.InputRequest, error)
	PrepareShutdown(ctx context.Context) error
	Close() error
}

type App struct {
	Service                  RuntimeService
	WorkDir                  string
	ConfigCatalog            *taskconfig.Catalog
	WorktreeLaunchAvailable  bool
	DefaultUseWorktree       bool
	SaveTaskLaunchPreference func(bool) error
	Version                  string
}

func (a App) Run(ctx context.Context) error {
	runtimeCtx, cancel := context.WithCancel(ctx)
	runDone := make(chan error, 1)
	go func() {
		runDone <- a.Service.Run(runtimeCtx)
	}()

	model := NewModelWithCatalog(a.Service, a.WorkDir, a.ConfigCatalog, a.Version)
	model.worktreeLaunchAvailable = a.WorktreeLaunchAvailable
	model.rememberedUseWorktree = a.WorktreeLaunchAvailable && a.DefaultUseWorktree
	model.newTask.useWorktree = model.rememberedUseWorktree
	model.saveTaskLaunchPreference = a.SaveTaskLaunchPreference
	model.syncComponents()
	_, err := tea.NewProgram(model, tea.WithContext(ctx)).Run()
	shutdownErr := a.Service.PrepareShutdown(context.Background())
	cancel()
	runErr := <-runDone
	if err != nil {
		return err
	}
	if shutdownErr != nil {
		return shutdownErr
	}
	if runErr != nil && !errors.Is(runErr, context.Canceled) {
		return runErr
	}
	return nil
}

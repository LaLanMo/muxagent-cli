package appserver

import (
	"context"
	"errors"
	"sync"

	"github.com/LaLanMo/muxagent-cli/internal/taskexecutor"
	"github.com/LaLanMo/muxagent-cli/internal/taskexecutor/claudeexec"
	"github.com/LaLanMo/muxagent-cli/internal/taskexecutor/codexexec"
	"github.com/LaLanMo/muxagent-cli/internal/taskexecutor/opencodehttp"
	"github.com/LaLanMo/muxagent-cli/internal/taskruntime"
)

type runtimeService interface {
	Run(ctx context.Context) error
	Events() <-chan taskruntime.RunEvent
	Dispatch(cmd taskruntime.RunCommand)
	PrepareShutdown(ctx context.Context) error
	Close() error
}

type runtimeServiceFactory func(workDir string) (runtimeService, error)

type runtimeManager struct {
	factory runtimeServiceFactory
	onEvent func(workspaceID string, event taskruntime.RunEvent)

	mu     sync.Mutex
	actors map[string]*workspaceActor
}

type workspaceActor struct {
	workspaceID string
	workDir     string
	service     runtimeService
	cancel      context.CancelFunc

	mu        sync.Mutex
	running   bool
	lastError string
}

func defaultRuntimeServiceFactory(workDir string) (runtimeService, error) {
	return taskruntime.NewService(
		workDir,
		taskexecutor.NewRouter(codexexec.New(""), claudeexec.New(""), opencodehttp.New("")),
	)
}

func newRuntimeManager(factory runtimeServiceFactory, onEvent func(workspaceID string, event taskruntime.RunEvent)) *runtimeManager {
	if factory == nil {
		factory = defaultRuntimeServiceFactory
	}
	return &runtimeManager{
		factory: factory,
		onEvent: onEvent,
		actors:  map[string]*workspaceActor{},
	}
}

func (m *runtimeManager) dispatch(workspace workspaceRecord, cmd taskruntime.RunCommand) error {
	actor, err := m.ensure(workspace)
	if err != nil {
		return err
	}
	actor.service.Dispatch(cmd)
	return nil
}

func (m *runtimeManager) snapshot(workspaceID string) workspaceActorDTO {
	m.mu.Lock()
	actor, ok := m.actors[workspaceID]
	m.mu.Unlock()
	if !ok {
		return workspaceActorDTO{State: "cold"}
	}
	actor.mu.Lock()
	defer actor.mu.Unlock()
	state := "active"
	if !actor.running {
		state = "error"
		if actor.lastError == "" {
			state = "cold"
		}
	}
	return workspaceActorDTO{
		State:     state,
		LastError: actor.lastError,
	}
}

func (m *runtimeManager) remove(workspaceID string) error {
	m.mu.Lock()
	actor, ok := m.actors[workspaceID]
	if ok {
		delete(m.actors, workspaceID)
	}
	m.mu.Unlock()
	if !ok {
		return nil
	}
	return closeWorkspaceActor(actor)
}

func (m *runtimeManager) closeAll() error {
	m.mu.Lock()
	actors := make([]*workspaceActor, 0, len(m.actors))
	for _, actor := range m.actors {
		actors = append(actors, actor)
	}
	m.actors = map[string]*workspaceActor{}
	m.mu.Unlock()

	var errs []error
	for _, actor := range actors {
		if err := closeWorkspaceActor(actor); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (m *runtimeManager) prepareShutdownAll(ctx context.Context) error {
	m.mu.Lock()
	actors := make([]*workspaceActor, 0, len(m.actors))
	for _, actor := range m.actors {
		actors = append(actors, actor)
	}
	m.mu.Unlock()

	var errs []error
	for _, actor := range actors {
		if err := actor.service.PrepareShutdown(ctx); err != nil && !errors.Is(err, context.Canceled) {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (m *runtimeManager) ensure(workspace workspaceRecord) (*workspaceActor, error) {
	m.mu.Lock()
	if actor, ok := m.actors[workspace.WorkspaceID]; ok {
		if actor.isRunning() {
			m.mu.Unlock()
			return actor, nil
		}
		delete(m.actors, workspace.WorkspaceID)
		m.mu.Unlock()
		_ = closeWorkspaceActor(actor)
	} else {
		m.mu.Unlock()
	}

	service, err := m.factory(workspace.Path)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	actor := &workspaceActor{
		workspaceID: workspace.WorkspaceID,
		workDir:     workspace.Path,
		service:     service,
		cancel:      cancel,
		running:     true,
	}

	m.mu.Lock()
	m.actors[workspace.WorkspaceID] = actor
	m.mu.Unlock()

	go m.runActor(ctx, actor)
	go m.forwardActorEvents(ctx, actor)
	return actor, nil
}

func (m *runtimeManager) runActor(ctx context.Context, actor *workspaceActor) {
	err := actor.service.Run(ctx)
	actor.mu.Lock()
	actor.running = false
	if err != nil && !errors.Is(err, context.Canceled) {
		actor.lastError = err.Error()
	}
	actor.mu.Unlock()
}

func (m *runtimeManager) forwardActorEvents(ctx context.Context, actor *workspaceActor) {
	for {
		select {
		case <-ctx.Done():
			return
		case event := <-actor.service.Events():
			if m.onEvent != nil {
				m.onEvent(actor.workspaceID, event)
			}
		}
	}
}

func closeWorkspaceActor(actor *workspaceActor) error {
	if actor == nil {
		return nil
	}
	actor.cancel()
	actor.mu.Lock()
	actor.running = false
	actor.mu.Unlock()
	return actor.service.Close()
}

func (a *workspaceActor) isRunning() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.running
}

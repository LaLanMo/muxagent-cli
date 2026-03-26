package tasktui

import (
	"context"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/LaLanMo/muxagent-cli/internal/taskconfig"
	"github.com/LaLanMo/muxagent-cli/internal/taskdomain"
	"github.com/LaLanMo/muxagent-cli/internal/taskruntime"
	"github.com/stretchr/testify/require"
)

func retryTUIConfig(maxIterations int) *taskconfig.Config {
	deny := false
	return &taskconfig.Config{
		Version: 1,
		Topology: taskconfig.Topology{
			MaxIterations: maxIterations,
			Entry:         "implement",
			Nodes: []taskconfig.NodeRef{
				{Name: "implement", MaxIterations: maxIterations},
			},
		},
		NodeDefinitions: map[string]taskconfig.NodeDefinition{
			"implement": {
				Type: taskconfig.NodeTypeAgent,
				ResultSchema: taskconfig.JSONSchema{
					Type:                 "object",
					AdditionalProperties: &deny,
					Required:             []string{"file_paths"},
					Properties: map[string]*taskconfig.JSONSchema{
						"file_paths": {Type: "array", Items: &taskconfig.JSONSchema{Type: "string"}},
					},
				},
			},
		},
	}
}

func typeText(t *testing.T, model Model, value string) Model {
	t.Helper()
	for _, r := range value {
		next, _ := model.Update(tea.KeyPressMsg{Text: string(r), Code: r})
		model = next.(Model)
	}
	return model
}

func openNewTaskModal(t *testing.T, model Model) Model {
	t.Helper()
	if len(model.tasks) > 0 {
		next, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyUp})
		model = next.(Model)
	}
	next, cmd := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = next.(Model)
	require.Equal(t, ScreenNewTask, model.screen)
	if cmd != nil {
		if msg := cmd(); msg != nil {
			next, _ := model.Update(msg)
			model = next.(Model)
		}
	}
	return model
}

func submitNewTaskModal(t *testing.T, model Model) (Model, tea.Cmd) {
	t.Helper()
	next, cmd := model.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	model = next.(Model)
	return model, cmd
}

func timePtr(ts time.Time) *time.Time {
	return &ts
}

func strippedView(view string) string {
	return ansi.Strip(view)
}

func taskStatusForID(tasks []taskdomain.TaskView, taskID string) taskdomain.TaskStatus {
	for _, task := range tasks {
		if task.Task.ID == taskID {
			return task.Status
		}
	}
	return ""
}

type fakeService struct {
	events     chan taskruntime.RunEvent
	dispatched []taskruntime.RunCommand
	tasks      []taskdomain.TaskView
	openViews  map[string]taskdomain.TaskView
	inputs     map[string]*taskruntime.InputRequest
}

func (f *fakeService) Run(ctx context.Context) error       { return nil }
func (f *fakeService) Events() <-chan taskruntime.RunEvent { return f.events }
func (f *fakeService) Dispatch(cmd taskruntime.RunCommand) { f.dispatched = append(f.dispatched, cmd) }
func (f *fakeService) ListTaskViews(ctx context.Context, workDir string) ([]taskdomain.TaskView, error) {
	return append([]taskdomain.TaskView(nil), f.tasks...), nil
}
func (f *fakeService) LoadTaskView(ctx context.Context, taskID string) (taskdomain.TaskView, *taskconfig.Config, error) {
	return f.openViews[taskID], nil, nil
}
func (f *fakeService) BuildInputRequest(ctx context.Context, taskID, nodeRunID string) (*taskruntime.InputRequest, error) {
	return f.inputs[nodeRunID], nil
}
func (f *fakeService) PrepareShutdown(ctx context.Context) error { return nil }
func (f *fakeService) Close() error                              { return nil }

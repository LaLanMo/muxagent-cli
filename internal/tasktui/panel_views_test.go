package tasktui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/LaLanMo/muxagent-cli/internal/taskdomain"
	"github.com/LaLanMo/muxagent-cli/internal/taskruntime"
)

func TestOpaqueMeasuredPanelTextUsesReadableMeasure(t *testing.T) {
	rendered := strippedView(renderOpaqueMeasuredPanelText(
		140,
		lipgloss.NewStyle(),
		strings.Repeat("This panel paragraph should wrap to a readable measure. ", 8),
	))

	longest := 0
	for _, line := range strings.Split(rendered, "\n") {
		line = strings.TrimRight(line, " ")
		longest = max(longest, ansi.StringWidth(line))
	}

	assert.Greater(t, longest, 0)
	assert.LessOrEqual(t, longest, detailBodyMeasureWidth(140))
}

func TestFailurePanelKeepsPanelSurfaceBehindStyledContent(t *testing.T) {
	tests := []struct {
		name     string
		setup    func(*Model)
		title    string
		bodySnip string
	}{
		{
			name: "failed",
			setup: func(model *Model) {
				model.currentConfig = retryTUIConfig(2)
				model.current = &taskdomain.TaskView{
					Task:            taskdomain.Task{ID: "task-1", Description: "Broken task"},
					Status:          taskdomain.TaskStatusFailed,
					CurrentNodeName: "implement",
					NodeRuns: []taskdomain.NodeRunView{
						{NodeRun: taskdomain.NodeRun{
							ID:          "run-1",
							TaskID:      "task-1",
							NodeName:    "implement",
							Status:      taskdomain.NodeRunFailed,
							StartedAt:   time.Now().UTC(),
							CompletedAt: timePtr(time.Now().UTC()),
						}},
					},
				}
				model.errorText = "executor failed"
				model.screen = ScreenFailed
			},
			title:    "Task failed",
			bodySnip: "executor failed",
		},
		{
			name: "blocked",
			setup: func(model *Model) {
				model.currentConfig = retryTUIConfig(1)
				model.current = &taskdomain.TaskView{
					Task:            taskdomain.Task{ID: "task-1", Description: "Blocked task"},
					Status:          taskdomain.TaskStatusFailed,
					CurrentNodeName: "implement",
					NodeRuns: []taskdomain.NodeRunView{
						{NodeRun: taskdomain.NodeRun{
							ID:          "run-1",
							TaskID:      "task-1",
							NodeName:    "implement",
							Status:      taskdomain.NodeRunDone,
							StartedAt:   time.Now().UTC(),
							CompletedAt: timePtr(time.Now().UTC()),
						}},
					},
					BlockedSteps: []taskdomain.BlockedStep{{
						NodeName:  "implement",
						Iteration: 2,
						Reason:    "node \"implement\" exceeded max_iterations",
						TriggeredBy: &taskdomain.TriggeredBy{
							NodeRunID: "run-1",
							Reason:    "edge: implement -> implement",
						},
						CreatedAt: time.Now().UTC().Add(time.Second),
					}},
				}
				model.screen = ScreenFailed
			},
			title:    "Task blocked",
			bodySnip: "implement is blocked before execution.",
		},
	}

	panelBgFragment := strings.TrimSuffix(strings.TrimPrefix(ansi.NewStyle().BackgroundColor(tuiTheme.Surface.Panel).String(), "\x1b["), "m")

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model := NewModel(&fakeService{events: make(chan taskruntime.RunEvent, 8)}, "/tmp/project", "", nil, "v0.1.0")
			next, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 32})
			model = next.(Model)
			tt.setup(&model)
			model.syncComponents()

			panel := model.renderFailurePanel(panelSurface{
				Rect:      surfaceRect{Width: 72},
				MaxHeight: 12,
			})

			assertLineHasSurfaceBackgroundAfterText(t, panel, tt.title, panelBgFragment)
			assertLineHasSurfaceBackgroundAfterText(t, panel, tt.bodySnip, panelBgFragment)
		})
	}
}

func assertLineHasSurfaceBackgroundAfterText(t *testing.T, view, text, bgFragment string) {
	t.Helper()

	for _, line := range strings.Split(view, "\n") {
		index := strings.Index(line, text)
		if index < 0 {
			continue
		}
		tail := line[index+len(text):]
		assert.Contains(t, tail, bgFragment)
		return
	}

	require.Failf(t, "line not found", "missing %q in rendered view", text)
}

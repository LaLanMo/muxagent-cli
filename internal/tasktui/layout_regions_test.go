package tasktui

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/LaLanMo/muxagent-cli/internal/taskdomain"
	"github.com/LaLanMo/muxagent-cli/internal/taskruntime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSyncComponentsUsesSharedLayoutRegions(t *testing.T) {
	tempDir := t.TempDir()
	artifactPath := filepath.Join(tempDir, "summary.md")
	require.NoError(t, os.WriteFile(artifactPath, []byte("# Summary\n\n- Ship it\n"), 0o644))

	model := NewModel(&fakeService{events: make(chan taskruntime.RunEvent, 8)}, tempDir, "", nil, "v0.1.0")
	next, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 32})
	model = next.(Model)
	model.tasks = []taskdomain.TaskView{
		{
			Task:   taskdomain.Task{ID: "task-1", Description: "Implement login", WorkDir: tempDir},
			Status: taskdomain.TaskStatusRunning,
		},
	}
	model.current = &taskdomain.TaskView{
		Task:          taskdomain.Task{ID: "task-1", Description: "Implement login", WorkDir: tempDir},
		Status:        taskdomain.TaskStatusRunning,
		ArtifactPaths: []string{artifactPath},
		NodeRuns: []taskdomain.NodeRunView{
			{
				NodeRun: taskdomain.NodeRun{
					ID:        "run-1",
					TaskID:    "task-1",
					NodeName:  "implement",
					Status:    taskdomain.NodeRunRunning,
					StartedAt: time.Now().UTC(),
				},
			},
		},
	}
	model.screen = ScreenRunning
	model.syncComponents()

	metrics := model.computeScreenMetrics()

	taskListHeader := model.renderTaskListHeader(metrics.innerWidth)
	taskListFooter := model.renderTaskListFooter(surfaceRect{Width: metrics.innerWidth})
	taskListLayout := model.computeTaskListScreenLayout(taskListHeader, taskListFooter)
	assert.Equal(t, taskListLayout.innerWidth, model.taskList.Width())
	assert.Equal(t, taskListLayout.bodyHeight, model.taskList.Height())

	model.screen = ScreenNewTask
	model.syncComponents()
	newTaskHeader := model.renderAppHeader(metrics.innerWidth)
	newTaskFooter := renderFooterHintBar(metrics.innerWidth, model.newTaskModalHint())
	newTaskLayout := model.computeNewTaskScreenLayout(newTaskHeader, newTaskFooter)
	assert.Equal(t, editorFieldInnerWidth(max(18, newTaskLayout.modalInnerWidth)), model.editor.input.Width())
	assert.Equal(t, newTaskLayout.editorRows, model.editor.Height())

	model.screen = ScreenRunning
	model.syncComponents()

	contentWidth := detailContentWidth(metrics.innerWidth)
	snapshot := model.computeDetailLayoutSnapshot()

	assert.Equal(t, contentWidth, snapshot.ContentWidth)
	assert.Equal(t, snapshot.Surfaces.Timeline.Width, model.detailViewport.Width())
	assert.Equal(t, snapshot.Surfaces.Timeline.Height, model.detailViewport.Height())
	assert.Equal(t, snapshot.Surfaces.Preview.Width, model.artifactPreview.Width())
	assert.Equal(t, snapshot.Surfaces.Preview.Height, model.artifactPreview.Height())
	assert.Equal(t, lipgloss.Height(snapshot.Footer), snapshot.Surfaces.Footer.Height)
	assert.Equal(t, lipgloss.Height(snapshot.PanelView.View), snapshot.Surfaces.Panel.Rect.Height)

	occupied := snapshot.Surfaces.Timeline.Height
	if snapshot.Surfaces.Launcher.Height > 0 {
		occupied += snapshot.Surfaces.Launcher.Height + 1
	}
	if snapshot.Surfaces.Panel.Rect.Height > 0 {
		occupied += snapshot.Surfaces.Panel.Rect.Height + 1
	}
	assert.Equal(t, snapshot.Frame.bodyHeight, occupied)
	assert.Equal(t, model.renderDetailTimeline(snapshot.Surfaces.Timeline), model.detailViewport.GetContent())
}

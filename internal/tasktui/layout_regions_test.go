package tasktui

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

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

	contentWidth := detailContentWidth(metrics.innerWidth)
	detailHeader := model.renderDetailHeader(contentWidth)
	detailFooter := model.renderDetailFooter(surfaceRect{Width: contentWidth})
	frame := model.computeDetailFrameLayout(contentWidth, detailHeader, detailFooter)
	panel := model.renderDetailPanel(model.computeDetailPanelSurface(frame))
	bodyLayout := model.computeDetailBodyLayout(frame, panel)
	fileLines := model.renderArtifactFileLines(max(18, bodyLayout.previewWidth-6), artifactVisibleCapacity(len(model.artifactItems)))
	_, previewBlockHeight := artifactPaneLayout(bodyLayout.topBodyHeight, len(fileLines))

	assert.Equal(t, bodyLayout.detailWidth, model.detailViewport.Width())
	assert.Equal(t, bodyLayout.detailHeight, model.detailViewport.Height())
	assert.Equal(t, max(12, bodyLayout.previewWidth-6), model.artifactPreview.Width())
	assert.Equal(t, max(3, previewBlockHeight-2), model.artifactPreview.Height())
	assert.Equal(t, model.renderDetailTimeline(surfaceRect{Width: bodyLayout.detailWidth, Height: bodyLayout.detailHeight}), model.detailViewport.GetContent())
}

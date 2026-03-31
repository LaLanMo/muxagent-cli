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

	next, _ = model.Update(tea.WindowSizeMsg{Width: 40, Height: 24})
	model = next.(Model)
	model.screen = ScreenNewTask
	model.syncComponents()
	narrowMetrics := model.computeScreenMetrics()
	newTaskHeader := model.renderAppHeader(narrowMetrics.innerWidth)
	newTaskFooter := model.renderNewTaskFooter(surfaceRect{Width: narrowMetrics.innerWidth})
	newTaskLayout := model.computeNewTaskScreenLayout(newTaskHeader, newTaskFooter)
	assert.Equal(t, lipgloss.Height(newTaskFooter), newTaskLayout.footerHeight)
	assert.Equal(t, 2, newTaskLayout.footerHeight)
	assert.Equal(t, editorFieldInnerWidth(max(18, newTaskLayout.modalInnerWidth)), model.editor.input.Width())
	assert.Equal(t, newTaskLayout.editorRows, model.editor.Height())

	next, _ = model.Update(tea.WindowSizeMsg{Width: 120, Height: 32})
	model = next.(Model)
	model.screen = ScreenRunning
	model.syncComponents()
	metrics = model.computeScreenMetrics()

	contentWidth := detailContentWidth(metrics.innerWidth, model.activeDetailTab)
	snapshot := model.computeDetailLayoutSnapshot()

	assert.Equal(t, contentWidth, snapshot.ContentWidth)
	assert.True(t, snapshot.Surfaces.TimelineSplit)
	assert.Equal(t, snapshot.Surfaces.Timeline.Width, model.detailViewport.Width())
	assert.Equal(t, snapshot.Surfaces.Timeline.Height, model.detailViewport.Height())
	assert.Equal(t, snapshot.Surfaces.LiveOutputViewport.Width, model.liveOutput.Width())
	assert.Equal(t, snapshot.Surfaces.LiveOutputViewport.Height, model.liveOutput.Height())
	assert.Equal(t, snapshot.Surfaces.Preview.Width, model.artifactPreview.Width())
	assert.Equal(t, snapshot.Surfaces.Preview.Height, model.artifactPreview.Height())
	assert.Equal(t, lipgloss.Height(snapshot.Footer), snapshot.Surfaces.Footer.Height)
	assert.Equal(t, lipgloss.Height(snapshot.PanelView.View), snapshot.Surfaces.Panel.Rect.Height)
	assert.Equal(t, artifactPaneSidebarWidth(snapshot.Body.detailWidth), snapshot.Surfaces.Timeline.Width)
	assert.Equal(t, snapshot.Body.topBodyHeight, snapshot.Surfaces.Timeline.Height)
	assert.Equal(t, snapshot.Body.topBodyHeight, snapshot.Surfaces.LiveOutputPane.Height)
	assert.Equal(t, snapshot.Body.detailWidth-snapshot.Surfaces.Timeline.Width-1, snapshot.Surfaces.LiveOutputPane.Width)

	occupied := snapshot.Surfaces.Timeline.Height
	if snapshot.Surfaces.Panel.Rect.Height > 0 {
		occupied += snapshot.Surfaces.Panel.Rect.Height + 1
	}
	assert.Equal(t, snapshot.Frame.bodyHeight, occupied)
	assert.Equal(t, model.renderDetailTimeline(snapshot.Surfaces.Timeline), model.detailViewport.GetContent())
	assert.Equal(t, model.renderLiveOutputContent(snapshot.Surfaces.LiveOutputViewport), model.liveOutput.GetContent())
}

func TestFailedDetailPanelUsesContentDrivenHeight(t *testing.T) {
	tempDir := t.TempDir()
	artifactPath := filepath.Join(tempDir, "failure.md")
	require.NoError(t, os.WriteFile(artifactPath, []byte("# Failure\n\n- retry implement\n"), 0o644))

	model := NewModel(&fakeService{events: make(chan taskruntime.RunEvent, 8)}, tempDir, "", nil, "v0.1.0")
	next, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 32})
	model = next.(Model)
	model.currentConfig = retryTUIConfig(2)
	model.current = &taskdomain.TaskView{
		Task:            taskdomain.Task{ID: "task-1", Description: "Retry failed implement", WorkDir: tempDir},
		Status:          taskdomain.TaskStatusFailed,
		CurrentNodeName: "implement",
		ArtifactPaths:   []string{artifactPath},
		NodeRuns: []taskdomain.NodeRunView{
			{
				NodeRun: taskdomain.NodeRun{
					ID:          "run-1",
					TaskID:      "task-1",
					NodeName:    "implement",
					Status:      taskdomain.NodeRunFailed,
					StartedAt:   time.Now().UTC(),
					CompletedAt: timePtr(time.Now().UTC()),
				},
				ArtifactPaths: []string{artifactPath},
			},
		},
	}
	model.errorText = "executor failed"
	model.screen = ScreenFailed
	model.syncComponents()

	snapshot := model.computeDetailLayoutSnapshot()

	assert.Equal(t, snapshot.Surfaces.Panel.MaxHeight, model.computeDetailPanelSurface(snapshot.Frame).MaxHeight)
	assert.Less(t, lipgloss.Height(snapshot.PanelView.View), snapshot.Surfaces.Panel.MaxHeight)
	assert.Equal(t, lipgloss.Height(snapshot.PanelView.View), snapshot.Surfaces.Panel.Rect.Height)
}

func TestArtifactTabUsesFullWidthPreviewSurface(t *testing.T) {
	tempDir := t.TempDir()
	artifactPath := filepath.Join(tempDir, "summary.md")
	require.NoError(t, os.WriteFile(artifactPath, []byte("# Summary\n\n- Ship it\n"), 0o644))

	model := NewModel(&fakeService{events: make(chan taskruntime.RunEvent, 8)}, tempDir, "", nil, "v0.1.0")
	next, _ := model.Update(tea.WindowSizeMsg{Width: 180, Height: 32})
	model = next.(Model)
	model.current = &taskdomain.TaskView{
		Task:          taskdomain.Task{ID: "task-1", Description: "Inspect artifact layout", WorkDir: tempDir},
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
	model.activeDetailTab = DetailTabArtifacts
	model.focusRegion = FocusRegionArtifactFiles
	model.syncComponents()

	metrics := model.computeScreenMetrics()
	snapshot := model.computeDetailLayoutSnapshot()
	expectedPreview := artifactPanePreviewRect(snapshot.Surfaces.Artifact.Rect.Width, snapshot.Surfaces.Artifact.Rect.Height)

	assert.Equal(t, metrics.innerWidth, snapshot.ContentWidth)
	assert.Equal(t, expectedPreview.Width, snapshot.Surfaces.Preview.Width)
	assert.Equal(t, expectedPreview.Height, snapshot.Surfaces.Preview.Height)
	assert.Equal(t, expectedPreview.Width, model.artifactPreview.Width())
	assert.Equal(t, expectedPreview.Height, model.artifactPreview.Height())
}

func TestArtifactPreviewWidthTracksTerminalWidth(t *testing.T) {
	tempDir := t.TempDir()
	artifactPath := filepath.Join(tempDir, "summary.md")
	require.NoError(t, os.WriteFile(artifactPath, []byte("# Summary\n\n- Ship it\n"), 0o644))

	buildModel := func(width int) Model {
		model := NewModel(&fakeService{events: make(chan taskruntime.RunEvent, 8)}, tempDir, "", nil, "v0.1.0")
		next, _ := model.Update(tea.WindowSizeMsg{Width: width, Height: 32})
		model = next.(Model)
		model.current = &taskdomain.TaskView{
			Task:          taskdomain.Task{ID: "task-1", Description: "Inspect artifact layout", WorkDir: tempDir},
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
		model.activeDetailTab = DetailTabArtifacts
		model.focusRegion = FocusRegionArtifactFiles
		model.syncComponents()
		return model
	}

	narrow := buildModel(96)
	wide := buildModel(149)
	extraWide := buildModel(180)

	assert.Greater(t, wide.artifactPreview.Width(), narrow.artifactPreview.Width())
	assert.Greater(t, extraWide.artifactPreview.Width(), wide.artifactPreview.Width())
}

func TestArtifactTabUsesFullWidthAndFullPreviewHeight(t *testing.T) {
	tempDir := t.TempDir()
	artifactPath := filepath.Join(tempDir, "summary.md")
	require.NoError(t, os.WriteFile(artifactPath, []byte("# Summary\n\n- Ship it\n"), 0o644))

	model := NewModel(&fakeService{events: make(chan taskruntime.RunEvent, 8)}, tempDir, "", nil, "v0.1.0")
	next, _ := model.Update(tea.WindowSizeMsg{Width: 180, Height: 36})
	model = next.(Model)
	model.current = &taskdomain.TaskView{
		Task:          taskdomain.Task{ID: "task-1", Description: "Inspect wide artifact preview", WorkDir: tempDir},
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
	model.activeDetailTab = DetailTabArtifacts
	model.focusRegion = FocusRegionArtifactFiles
	model.syncComponents()

	snapshot := model.computeDetailLayoutSnapshot()
	expectedPreview := artifactPanePreviewRect(snapshot.Surfaces.Artifact.Rect.Width, snapshot.Surfaces.Artifact.Rect.Height)

	assert.Equal(t, snapshot.Frame.innerWidth, snapshot.ContentWidth)
	assert.Equal(t, expectedPreview.Width, snapshot.Surfaces.Preview.Width)
	assert.Equal(t, expectedPreview.Height, snapshot.Surfaces.Preview.Height)
	assert.Equal(t, expectedPreview.Width, model.artifactPreview.Width())
	assert.Equal(t, expectedPreview.Height, model.artifactPreview.Height())
	assert.Equal(t, snapshot.Surfaces.Artifact.Rect.Height-1, snapshot.Surfaces.Preview.Height)
}

func TestTimelineDetailUsesSplitSurfaceOnlyWhileRunning(t *testing.T) {
	tempDir := t.TempDir()
	buildModel := func(status taskdomain.NodeRunStatus, screen Screen) Model {
		model := NewModel(&fakeService{events: make(chan taskruntime.RunEvent, 8)}, tempDir, "", nil, "v0.1.0")
		next, _ := model.Update(tea.WindowSizeMsg{Width: 180, Height: 32})
		model = next.(Model)
		model.current = &taskdomain.TaskView{
			Task:   taskdomain.Task{ID: "task-1", Description: "Inspect wide timeline layout", WorkDir: tempDir},
			Status: taskdomain.TaskStatusRunning,
			NodeRuns: []taskdomain.NodeRunView{
				{
					NodeRun: taskdomain.NodeRun{
						ID:        "run-1",
						TaskID:    "task-1",
						NodeName:  "implement",
						Status:    status,
						StartedAt: time.Now().UTC(),
					},
				},
			},
		}
		model.screen = screen
		model.activeDetailTab = DetailTabTimeline
		model.focusRegion = FocusRegionDetail
		model.syncComponents()
		return model
	}

	running := buildModel(taskdomain.NodeRunRunning, ScreenRunning)
	runningSnapshot := running.computeDetailLayoutSnapshot()
	assert.True(t, runningSnapshot.Surfaces.TimelineSplit)
	assert.Equal(t, artifactPaneSidebarWidth(runningSnapshot.Body.detailWidth), runningSnapshot.Surfaces.Timeline.Width)
	assert.Equal(t, runningSnapshot.Surfaces.Timeline.Width, running.detailViewport.Width())

	completed := buildModel(taskdomain.NodeRunDone, ScreenComplete)
	completedSnapshot := completed.computeDetailLayoutSnapshot()
	assert.False(t, completedSnapshot.Surfaces.TimelineSplit)
	assert.Equal(t, completedSnapshot.ContentWidth, completedSnapshot.Surfaces.Timeline.Width)
	assert.Zero(t, completedSnapshot.Surfaces.LiveOutputPane.Width)
	assert.Zero(t, completedSnapshot.Surfaces.LiveOutputViewport.Width)
}

func TestWideDetailPanelEditorUsesSoftWidthCap(t *testing.T) {
	tempDir := t.TempDir()
	model := NewModel(&fakeService{events: make(chan taskruntime.RunEvent, 8)}, tempDir, "", nil, "v0.1.0")
	next, _ := model.Update(tea.WindowSizeMsg{Width: 180, Height: 32})
	model = next.(Model)
	model.current = &taskdomain.TaskView{
		Task:   taskdomain.Task{ID: "task-1", Description: "Review wide approval layout", WorkDir: tempDir},
		Status: taskdomain.TaskStatusAwaitingUser,
		NodeRuns: []taskdomain.NodeRunView{
			{NodeRun: taskdomain.NodeRun{ID: "run-1", TaskID: "task-1", NodeName: "approve_plan", Status: taskdomain.NodeRunAwaitingUser}},
		},
	}
	model.currentInput = &taskruntime.InputRequest{
		Kind:      taskruntime.InputKindHumanNode,
		TaskID:    "task-1",
		NodeRunID: "run-1",
		NodeName:  "approve_plan",
	}
	model.screen = ScreenApproval
	model.approval.choice = 1
	model.syncComponents()

	snapshot := model.computeDetailLayoutSnapshot()
	panelInnerWidth := max(18, snapshot.Surfaces.Panel.Rect.Width-tuiTheme.Panel.Warning.GetHorizontalFrameSize())

	assert.Equal(t, detailFormMeasureWidth(panelInnerWidth), snapshot.Editor.FieldWidth)
	assert.Less(t, snapshot.Editor.FieldWidth, panelInnerWidth)
	assert.Equal(t, editorFieldInnerWidth(snapshot.Editor.FieldWidth), model.editor.input.Width())
}

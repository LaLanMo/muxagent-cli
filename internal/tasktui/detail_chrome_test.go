package tasktui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/LaLanMo/muxagent-cli/internal/taskconfig"
	"github.com/LaLanMo/muxagent-cli/internal/taskdomain"
	"github.com/LaLanMo/muxagent-cli/internal/taskruntime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLongTaskDescriptionsKeepDetailFooterVisible(t *testing.T) {
	tempDir := t.TempDir()
	artifactPath := filepath.Join(tempDir, "plan.md")
	require.NoError(t, os.WriteFile(artifactPath, []byte("# Plan\n\n- keep footer visible\n"), 0o644))

	longDescription := strings.TrimSpace(strings.Repeat(
		"Investigate why the detail screen footer disappears when the task title is long and artifacts are visible. ",
		3,
	))

	tests := []struct {
		name   string
		width  int
		height int
		setup  func(*Model)
		want   []string
	}{
		{
			name:   "running with split artifacts",
			width:  120,
			height: 32,
			setup: func(model *Model) {
				model.current = &taskdomain.TaskView{
					Task:          taskdomain.Task{ID: "task-1", Description: longDescription, WorkDir: tempDir},
					Status:        taskdomain.TaskStatusRunning,
					ArtifactPaths: []string{artifactPath},
					NodeRuns: []taskdomain.NodeRunView{
						{NodeRun: taskdomain.NodeRun{ID: "run-1", TaskID: "task-1", NodeName: "implement", Status: taskdomain.NodeRunRunning, StartedAt: time.Now().UTC()}},
					},
				}
				model.screen = ScreenRunning
			},
			want: []string{"Shift+Tab artifacts", "Ctrl+C quit", "elapsed:", "Output · implement", "thread: pending"},
		},
		{
			name:   "approval with split artifacts",
			width:  120,
			height: 32,
			setup: func(model *Model) {
				model.current = &taskdomain.TaskView{
					Task:          taskdomain.Task{ID: "task-1", Description: longDescription, WorkDir: tempDir},
					Status:        taskdomain.TaskStatusAwaitingUser,
					ArtifactPaths: []string{artifactPath},
					NodeRuns: []taskdomain.NodeRunView{
						{NodeRun: taskdomain.NodeRun{ID: "run-1", TaskID: "task-1", NodeName: "approve_plan", Status: taskdomain.NodeRunAwaitingUser}},
					},
				}
				model.currentInput = &taskruntime.InputRequest{
					Kind:          taskruntime.InputKindHumanNode,
					TaskID:        "task-1",
					NodeRunID:     "run-1",
					NodeName:      "approve_plan",
					ArtifactPaths: []string{artifactPath},
				}
				model.setDetailScreen(ScreenApproval, true)
			},
			want: []string{"Approve this plan?", "Ctrl+C quit", "Files", "Preview ·"},
		},
		{
			name:   "clarification with split artifacts",
			width:  120,
			height: 32,
			setup: func(model *Model) {
				model.current = &taskdomain.TaskView{
					Task:          taskdomain.Task{ID: "task-1", Description: longDescription, WorkDir: tempDir},
					Status:        taskdomain.TaskStatusAwaitingUser,
					ArtifactPaths: []string{artifactPath},
					NodeRuns: []taskdomain.NodeRunView{
						{NodeRun: taskdomain.NodeRun{ID: "run-1", TaskID: "task-1", NodeName: "draft_plan", Status: taskdomain.NodeRunAwaitingUser}},
					},
				}
				model.currentInput = &taskruntime.InputRequest{
					Kind:          taskruntime.InputKindClarification,
					TaskID:        "task-1",
					NodeRunID:     "run-1",
					NodeName:      "draft_plan",
					ArtifactPaths: []string{artifactPath},
					Questions: []taskdomain.ClarificationQuestion{{
						Question:     "Which path should we take next?",
						WhyItMatters: "The next step depends on the selected path.",
						Options: []taskdomain.ClarificationOption{
							{Label: "A", Description: "Option A"},
							{Label: "B", Description: "Option B"},
						},
					}},
				}
				model.screen = ScreenClarification
			},
			want: []string{"Question 1/1", "Ctrl+C quit", "Enter choose"},
		},
		{
			name:   "failed with split artifacts",
			width:  120,
			height: 32,
			setup: func(model *Model) {
				model.currentConfig = retryTUIConfig(2)
				model.current = &taskdomain.TaskView{
					Task:            taskdomain.Task{ID: "task-1", Description: longDescription, WorkDir: tempDir},
					Status:          taskdomain.TaskStatusFailed,
					CurrentNodeName: "implement",
					ArtifactPaths:   []string{artifactPath},
					NodeRuns: []taskdomain.NodeRunView{
						{NodeRun: taskdomain.NodeRun{ID: "run-1", TaskID: "task-1", NodeName: "implement", Status: taskdomain.NodeRunFailed, StartedAt: time.Now().UTC(), CompletedAt: timePtr(time.Now().UTC())}, ArtifactPaths: []string{artifactPath}},
					},
				}
				model.errorText = "interrupted by user"
				model.screen = ScreenFailed
			},
			want: []string{"Task failed", "Retry step", "Ctrl+C quit"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model := NewModel(&fakeService{events: make(chan taskruntime.RunEvent, 8)}, tempDir, "", nil, "v0.1.0")
			next, _ := model.Update(tea.WindowSizeMsg{Width: tt.width, Height: tt.height})
			model = next.(Model)
			tt.setup(&model)
			model.syncComponents()

			view := model.View().Content
			stripped := strippedView(view)
			innerWidth, _ := innerSize(tt.width, tt.height)
			contentWidth := detailContentWidth(innerWidth, model.activeDetailTab)
			header := strippedView(model.renderDetailHeader(contentWidth))
			headerLines := strings.Split(header, "\n")

			for _, want := range tt.want {
				assert.Contains(t, stripped, want)
			}
			for _, line := range headerLines {
				assert.LessOrEqual(t, ansi.StringWidth(line), contentWidth)
			}
			require.GreaterOrEqual(t, len(headerLines), 2)
			assert.LessOrEqual(t, ansi.StringWidth(strings.TrimRight(headerLines[0], " ")), detailTitleMeasureWidth(contentWidth))
			assert.LessOrEqual(t, ansi.StringWidth(strings.TrimRight(headerLines[1], " ")), detailTitleMeasureWidth(contentWidth))
		})
	}
}

func TestDetailHeaderShowsWorktreeSummaryAndConnectorFreeStageStrip(t *testing.T) {
	tempDir := t.TempDir()
	now := time.Now().UTC()

	model := NewModel(&fakeService{events: make(chan taskruntime.RunEvent, 8)}, tempDir, "", nil, "v0.1.0")
	next, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 32})
	model = next.(Model)
	model.currentConfig = &taskconfig.Config{
		Topology: taskconfig.Topology{
			Nodes: []taskconfig.NodeRef{
				{Name: "draft_plan"},
				{Name: "review_plan"},
				{Name: "approve_plan"},
				{Name: "implement"},
				{Name: "verify"},
				{Name: "done"},
			},
		},
	}
	model.current = &taskdomain.TaskView{
		Task: taskdomain.Task{
			ID:           "task-1",
			Description:  "Review a worktree-backed task",
			ConfigAlias:  "reviewer",
			WorkDir:      tempDir,
			ExecutionDir: filepath.Join("/tmp", "worktrees", "task-1"),
		},
		Status:          taskdomain.TaskStatusAwaitingUser,
		CurrentNodeName: "approve_plan",
		CurrentNodeType: taskconfig.NodeTypeHuman,
		NodeRuns: []taskdomain.NodeRunView{
			{NodeRun: taskdomain.NodeRun{ID: "run-1", TaskID: "task-1", NodeName: "draft_plan", Status: taskdomain.NodeRunDone, StartedAt: now}},
			{NodeRun: taskdomain.NodeRun{ID: "run-2", TaskID: "task-1", NodeName: "review_plan", Status: taskdomain.NodeRunDone, StartedAt: now.Add(time.Minute)}},
			{NodeRun: taskdomain.NodeRun{ID: "run-3", TaskID: "task-1", NodeName: "approve_plan", Status: taskdomain.NodeRunAwaitingUser, StartedAt: now.Add(2 * time.Minute)}},
		},
	}
	model.screen = ScreenApproval
	model.syncComponents()

	innerWidth, _ := innerSize(120, 32)
	header := strippedView(model.renderDetailHeader(detailContentWidth(innerWidth, model.activeDetailTab)))

	assert.Contains(t, header, "awaiting approval")
	assert.Contains(t, header, "at approve_plan")
	assert.Contains(t, header, "worktree")
	assert.Contains(t, header, "config reviewer")
	assert.Contains(t, header, "✓ draft_plan")
	assert.Contains(t, header, "✓ review_plan")
	assert.Contains(t, header, "● approve_plan")
	assert.Contains(t, header, "○ implement")
	assert.NotContains(t, header, "→")
	assert.NotContains(t, header, filepath.Join("/tmp", "worktrees", "task-1"))
}

func TestWideDetailHeaderKeepsMeasuredTitleInsideFullWidthFrame(t *testing.T) {
	tempDir := t.TempDir()
	longDescription := strings.TrimSpace(strings.Repeat(
		"Inspect how the detail header behaves when the terminal is intentionally very wide. ",
		4,
	))

	model := NewModel(&fakeService{events: make(chan taskruntime.RunEvent, 8)}, tempDir, "", nil, "v0.1.0")
	next, _ := model.Update(tea.WindowSizeMsg{Width: 180, Height: 32})
	model = next.(Model)
	model.current = &taskdomain.TaskView{
		Task:   taskdomain.Task{ID: "task-1", Description: longDescription, WorkDir: tempDir},
		Status: taskdomain.TaskStatusRunning,
		NodeRuns: []taskdomain.NodeRunView{
			{NodeRun: taskdomain.NodeRun{ID: "run-1", TaskID: "task-1", NodeName: "implement", Status: taskdomain.NodeRunRunning, StartedAt: time.Now().UTC()}},
		},
	}
	model.screen = ScreenRunning
	model.syncComponents()

	innerWidth, _ := innerSize(180, 32)
	header := strippedView(model.renderDetailHeader(detailContentWidth(innerWidth, model.activeDetailTab)))
	headerLines := strings.Split(header, "\n")

	require.GreaterOrEqual(t, len(headerLines), 4)
	assert.LessOrEqual(t, ansi.StringWidth(strings.TrimRight(headerLines[0], " ")), detailTitleMeasureWidth(innerWidth))
	assert.LessOrEqual(t, ansi.StringWidth(strings.TrimRight(headerLines[1], " ")), detailTitleMeasureWidth(innerWidth))
	assert.Equal(t, innerWidth, ansi.StringWidth(headerLines[len(headerLines)-1]))
}

func TestDetailHeaderShowsDirectParentLineage(t *testing.T) {
	tempDir := t.TempDir()

	model := NewModel(&fakeService{events: make(chan taskruntime.RunEvent, 8)}, tempDir, "", nil, "v0.1.0")
	next, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 32})
	model = next.(Model)
	model.current = &taskdomain.TaskView{
		Task: taskdomain.Task{
			ID:          "task-child",
			Description: "Child task",
			WorkDir:     tempDir,
		},
		Status:                taskdomain.TaskStatusRunning,
		CurrentNodeName:       "implement",
		ParentTaskID:          "task-parent",
		ParentTaskDescription: "Parent task",
		NodeRuns: []taskdomain.NodeRunView{
			{NodeRun: taskdomain.NodeRun{ID: "run-1", TaskID: "task-child", NodeName: "implement", Status: taskdomain.NodeRunRunning, StartedAt: time.Now().UTC()}},
		},
	}
	model.screen = ScreenRunning
	model.syncComponents()

	innerWidth, _ := innerSize(120, 32)
	header := strippedView(model.renderDetailHeader(detailContentWidth(innerWidth, model.activeDetailTab)))

	assert.Contains(t, header, "follow-up of Parent task")
}

package tasktui

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/LaLanMo/muxagent-cli/internal/taskdomain"
	"github.com/LaLanMo/muxagent-cli/internal/taskruntime"
)

func buildCompletedFollowUpModel(t *testing.T, withArtifact bool) (Model, *fakeService, string) {
	t.Helper()
	tempDir := t.TempDir()
	service := &fakeService{events: make(chan taskruntime.RunEvent, 8)}
	model := NewModel(service, tempDir, "", nil, "v0.1.0")
	next, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 32})
	model = next.(Model)

	artifactPath := ""
	artifactPaths := []string(nil)
	if withArtifact {
		artifactPath = filepath.Join(tempDir, "summary.md")
		require.NoError(t, os.WriteFile(artifactPath, []byte("# Summary\n"), 0o644))
		artifactPaths = []string{artifactPath}
	}

	now := time.Now().UTC()
	model.current = &taskdomain.TaskView{
		Task:            taskdomain.Task{ID: "task-parent", Description: "Parent task", WorkDir: tempDir},
		Status:          taskdomain.TaskStatusDone,
		CurrentNodeName: "done",
		ArtifactPaths:   artifactPaths,
		NodeRuns: []taskdomain.NodeRunView{
			{
				NodeRun: taskdomain.NodeRun{
					ID:          "run-done",
					TaskID:      "task-parent",
					NodeName:    "done",
					Status:      taskdomain.NodeRunDone,
					StartedAt:   now,
					CompletedAt: timePtr(now),
				},
				ArtifactPaths: artifactPaths,
			},
		},
	}
	model.activeTaskID = "task-parent"
	model.screen = ScreenComplete
	model.focusRegion = FocusRegionDetail
	model.syncComponents()
	return model, service, artifactPath
}

func TestCompleteScreenShowsFollowUpPanelAndDetailHint(t *testing.T) {
	model, _, _ := buildCompletedFollowUpModel(t, false)

	view := strippedView(model.View().Content)
	assert.Contains(t, view, "Continue from this task")
	assert.Contains(t, view, "Creates a new linked task and carries over this task's context.")
	assert.Contains(t, view, "Follow-up request")
	assert.Contains(t, view, "Ctrl+X hide")
	assert.Contains(t, view, "Tab continue")
}

func TestCompleteFollowUpFocusedEditorShowsCursor(t *testing.T) {
	model, _, _ := buildCompletedFollowUpModel(t, false)
	model.focusRegion = FocusRegionActionPanel
	model.followUp.choice = followUpRowInput
	model.syncComponents()
	_ = model.syncInputFocus()
	model = typeText(t, model, "Continue")

	view := model.View()
	require.NotNil(t, view.Cursor)
}

func TestCompleteFollowUpCtrlXHidesAndRestoresPanelDraft(t *testing.T) {
	model, _, _ := buildCompletedFollowUpModel(t, false)
	model.focusRegion = FocusRegionActionPanel
	model.followUp.choice = followUpRowInput
	model.syncComponents()
	_ = model.syncInputFocus()
	model = typeText(t, model, "Continue with release prep")

	next, _ := model.Update(tea.KeyPressMsg{Code: 'x', Mod: tea.ModCtrl})
	model = next.(Model)

	assert.True(t, model.followUp.hidden)
	assert.Equal(t, FocusRegionDetail, model.focusRegion)
	assert.Equal(t, []FocusRegion{FocusRegionDetail}, model.availableFocusRegions())
	assert.Equal(t, "", model.editor.Slot())

	hiddenView := strippedView(model.View().Content)
	assert.NotContains(t, hiddenView, "Continue from this task")
	assert.NotContains(t, hiddenView, "Tab continue")
	assert.Contains(t, hiddenView, "Ctrl+X continue")

	next, _ = model.Update(tea.KeyPressMsg{Code: 'x', Mod: tea.ModCtrl})
	model = next.(Model)

	assert.False(t, model.followUp.hidden)
	assert.Equal(t, FocusRegionDetail, model.focusRegion)
	assert.Contains(t, strippedView(model.View().Content), "Continue from this task")

	next, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	model = next.(Model)
	assert.Equal(t, FocusRegionActionPanel, model.focusRegion)
	assert.Equal(t, followUpEditorSlot("task-parent"), model.editor.Slot())
	assert.Equal(t, "Continue with release prep", model.editor.Value())
}

func TestCompleteFollowUpRawCtrlXControlCodeHidesPanel(t *testing.T) {
	model, _, _ := buildCompletedFollowUpModel(t, false)
	model.focusRegion = FocusRegionDetail
	model.syncComponents()

	next, _ := model.Update(tea.KeyPressMsg{Code: 0x18})
	model = next.(Model)

	assert.True(t, model.followUp.hidden)
	assert.Equal(t, FocusRegionDetail, model.focusRegion)
	assert.NotContains(t, strippedView(model.View().Content), "Continue from this task")
}

func TestCompleteFooterDoesNotRepeatConfigAlias(t *testing.T) {
	model, _, _ := buildCompletedFollowUpModel(t, false)
	model.current.Task.ConfigAlias = "reviewer"

	header := strippedView(model.renderDetailHeader(100))
	footer := strippedView(model.renderCompleteFooter(surfaceRect{Width: 100}))

	assert.Contains(t, header, "config reviewer")
	assert.NotContains(t, footer, "config reviewer")
	assert.Contains(t, footer, "artifacts")
}

func TestCompleteFollowUpArtifactsTabRoundTripPreservesDraftAndRefocusesEditor(t *testing.T) {
	model, _, _ := buildCompletedFollowUpModel(t, true)
	model.activeDetailTab = DetailTabArtifacts
	model.focusRegion = FocusRegionActionPanel
	model.followUp.choice = followUpRowInput
	model.syncComponents()
	_ = model.syncInputFocus()
	model.editor.SetValue("Continue with release prep")

	require.True(t, model.editor.Focused())
	assert.Equal(t, followUpEditorSlot("task-parent"), model.editor.Slot())

	next, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	model = next.(Model)
	assert.Equal(t, FocusRegionArtifactFiles, model.focusRegion)
	assert.False(t, model.editor.Focused())

	next, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	model = next.(Model)
	assert.Equal(t, FocusRegionArtifactPreview, model.focusRegion)
	assert.False(t, model.editor.Focused())

	next, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	model = next.(Model)
	assert.Equal(t, FocusRegionActionPanel, model.focusRegion)
	assert.Equal(t, followUpRowInput, model.followUp.choice)
	assert.True(t, model.editor.Focused())
	assert.Equal(t, "Continue with release prep", model.editor.Value())
}

func TestCompleteFollowUpCtrlJInsertsNewlineAndEnterSubmits(t *testing.T) {
	model, service, _ := buildCompletedFollowUpModel(t, false)
	model.focusRegion = FocusRegionActionPanel
	model.followUp.choice = followUpRowInput
	model.syncComponents()
	_ = model.syncInputFocus()

	model = typeText(t, model, "Continue with release")
	next, cmd := model.Update(tea.KeyPressMsg{Code: 'j', Mod: tea.ModCtrl})
	model = next.(Model)
	if cmd != nil {
		_ = cmd()
	}
	model = typeText(t, model, "prep")

	assert.Equal(t, "Continue with release\nprep", model.editor.Value())
	assert.Empty(t, service.dispatched)

	next, cmd = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = next.(Model)
	require.NotNil(t, cmd)
	require.Nil(t, cmd())
	require.Len(t, service.dispatched, 1)
	assert.Equal(t, taskruntime.CommandStartFollowUp, service.dispatched[0].Type)
	assert.Equal(t, "task-parent", service.dispatched[0].ParentTaskID)
	assert.Equal(t, "Continue with release\nprep", service.dispatched[0].Description)
	assert.Equal(t, ScreenComplete, model.screen)
	require.NotNil(t, model.pendingRuntimeCmd)
	assert.Equal(t, pendingRuntimeCommandStartFollowUp, model.pendingRuntimeCmd.kind)
}

func TestCompleteFollowUpCommandErrorRestoresPanelAndKeepsDraft(t *testing.T) {
	model, service, _ := buildCompletedFollowUpModel(t, false)
	model.focusRegion = FocusRegionActionPanel
	model.followUp.choice = followUpRowInput
	model.syncComponents()
	_ = model.syncInputFocus()

	model = typeText(t, model, "Continue with release prep")
	next, cmd := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = next.(Model)
	require.NotNil(t, cmd)
	require.Nil(t, cmd())
	require.Len(t, service.dispatched, 1)

	next, _ = model.Update(taskruntime.RunEvent{
		Type:   taskruntime.EventCommandError,
		TaskID: "task-parent",
		Error:  &taskruntime.RunError{Message: "cannot start follow-up"},
	})
	model = next.(Model)

	assert.Equal(t, ScreenComplete, model.screen)
	assert.Equal(t, FocusRegionActionPanel, model.focusRegion)
	assert.Equal(t, followUpRowInput, model.followUp.choice)
	assert.Equal(t, "Continue with release prep", model.editor.Value())
	assert.Equal(t, "cannot start follow-up", model.errorText)
	assert.Nil(t, model.pendingRuntimeCmd)
}

func TestCompleteFollowUpCommandErrorKeepsHiddenState(t *testing.T) {
	model, service, _ := buildCompletedFollowUpModel(t, false)
	model.focusRegion = FocusRegionActionPanel
	model.followUp.choice = followUpRowInput
	model.syncComponents()
	_ = model.syncInputFocus()

	model = typeText(t, model, "Continue with release prep")
	next, cmd := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = next.(Model)
	require.NotNil(t, cmd)
	require.Nil(t, cmd())
	require.Len(t, service.dispatched, 1)

	next, _ = model.Update(tea.KeyPressMsg{Code: 'x', Mod: tea.ModCtrl})
	model = next.(Model)
	assert.True(t, model.followUp.hidden)

	next, _ = model.Update(taskruntime.RunEvent{
		Type:   taskruntime.EventCommandError,
		TaskID: "task-parent",
		Error:  &taskruntime.RunError{Message: "cannot start follow-up"},
	})
	model = next.(Model)

	assert.Equal(t, ScreenComplete, model.screen)
	assert.Equal(t, FocusRegionDetail, model.focusRegion)
	assert.True(t, model.followUp.hidden)
	assert.Equal(t, "cannot start follow-up", model.errorText)
	assert.Nil(t, model.pendingRuntimeCmd)
}

func TestCompleteFollowUpTaskCompletedReshowsPanel(t *testing.T) {
	model, _, artifactPath := buildCompletedFollowUpModel(t, true)
	model.followUp.hidden = true
	model.syncComponents()

	now := time.Now().UTC()
	completed := taskdomain.TaskView{
		Task: taskdomain.Task{
			ID:          "task-parent",
			Description: "Parent task",
			WorkDir:     model.workDir,
		},
		Status:          taskdomain.TaskStatusDone,
		CurrentNodeName: "done",
		ArtifactPaths:   []string{artifactPath},
		NodeRuns: []taskdomain.NodeRunView{
			{
				NodeRun: taskdomain.NodeRun{
					ID:          "run-done",
					TaskID:      "task-parent",
					NodeName:    "done",
					Status:      taskdomain.NodeRunDone,
					StartedAt:   now,
					CompletedAt: timePtr(now),
				},
				ArtifactPaths: []string{artifactPath},
			},
		},
	}

	next, _ := model.Update(taskruntime.RunEvent{
		Type:     taskruntime.EventTaskCompleted,
		TaskID:   "task-parent",
		NodeName: "done",
		TaskView: &completed,
	})
	model = next.(Model)

	assert.False(t, model.followUp.hidden)
	assert.Contains(t, strippedView(model.View().Content), "Continue from this task")
}

func TestCompleteFollowUpTaskCreatedActivatesChildTaskAndClearsDraft(t *testing.T) {
	model, service, _ := buildCompletedFollowUpModel(t, false)
	model.focusRegion = FocusRegionActionPanel
	model.followUp.choice = followUpRowInput
	model.syncComponents()
	_ = model.syncInputFocus()

	model = typeText(t, model, "Continue with release prep")
	next, cmd := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = next.(Model)
	require.NotNil(t, cmd)
	require.Nil(t, cmd())
	require.Len(t, service.dispatched, 1)

	now := time.Now().UTC()
	childView := taskdomain.TaskView{
		Task: taskdomain.Task{
			ID:          "task-child",
			Description: "Continue with release prep",
			WorkDir:     model.workDir,
			CreatedAt:   now,
			UpdatedAt:   now,
		},
		Status:          taskdomain.TaskStatusRunning,
		CurrentNodeName: "draft_plan",
		NodeRuns: []taskdomain.NodeRunView{
			{NodeRun: taskdomain.NodeRun{ID: "run-child", TaskID: "task-child", NodeName: "draft_plan", Status: taskdomain.NodeRunRunning, StartedAt: now}},
		},
	}

	next, _ = model.Update(taskruntime.RunEvent{
		Type:     taskruntime.EventTaskCreated,
		TaskID:   "task-child",
		TaskView: &childView,
	})
	model = next.(Model)

	require.NotNil(t, model.current)
	assert.Equal(t, "task-child", model.current.Task.ID)
	assert.Equal(t, "task-child", model.activeTaskID)
	assert.Equal(t, ScreenRunning, model.screen)
	assert.Equal(t, FocusRegionDetail, model.focusRegion)
	assert.Equal(t, "", model.editor.Slot())
	_, ok := model.editor.DraftValue(followUpEditorSlot("task-parent"))
	assert.False(t, ok)
}

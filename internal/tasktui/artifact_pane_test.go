package tasktui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/LaLanMo/muxagent-cli/internal/taskdomain"
	"github.com/LaLanMo/muxagent-cli/internal/taskruntime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApprovalScreenShowsExpandedArtifactsPaneAndPreview(t *testing.T) {
	tempDir := t.TempDir()
	planPath := filepath.Join(tempDir, "plan.md")
	apiPath := filepath.Join(tempDir, "api.md")
	require.NoError(t, os.WriteFile(planPath, []byte("# Plan\n\n1. Do the thing\n"), 0o644))
	require.NoError(t, os.WriteFile(apiPath, []byte("# API\n\n- endpoint\n"), 0o644))

	model := NewModel(&fakeService{events: make(chan taskruntime.RunEvent, 8)}, tempDir, "", nil, "v0.1.0")
	next, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 32})
	model = next.(Model)
	model.current = &taskdomain.TaskView{
		Task:   taskdomain.Task{ID: "task-1", Description: "Implement login", WorkDir: tempDir},
		Status: taskdomain.TaskStatusAwaitingUser,
		NodeRuns: []taskdomain.NodeRunView{
			{NodeRun: taskdomain.NodeRun{ID: "run-1", TaskID: "task-1", NodeName: "approve_plan", Status: taskdomain.NodeRunAwaitingUser}},
		},
	}
	model.currentInput = &taskruntime.InputRequest{
		Kind:          taskruntime.InputKindHumanNode,
		TaskID:        "task-1",
		NodeRunID:     "run-1",
		NodeName:      "approve_plan",
		ArtifactPaths: []string{planPath, apiPath},
	}
	model.screen = ScreenApproval
	model.syncComponents()

	// Switch to artifacts tab
	model.activeDetailTab = DetailTabArtifacts
	model.focusRegion = FocusRegionArtifactFiles
	model.syncComponents()

	view := strippedView(model.View().Content)
	assert.Contains(t, view, "Shift+Tab timeline")
	assert.Contains(t, view, "Files")
	assert.Contains(t, view, "Preview · plan.md")
	assert.Contains(t, view, "Plan")
	assert.NotContains(t, view, "# Plan")
}

func TestRunningDetailLetsUserSwitchArtifactSelection(t *testing.T) {
	tempDir := t.TempDir()
	oldPath := filepath.Join(tempDir, "old.md")
	newPath := filepath.Join(tempDir, "new.md")
	require.NoError(t, os.WriteFile(oldPath, []byte("# Old\n"), 0o644))
	require.NoError(t, os.WriteFile(newPath, []byte("# New\n"), 0o644))

	model := NewModel(&fakeService{events: make(chan taskruntime.RunEvent, 8)}, tempDir, "", nil, "v0.1.0")
	next, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 32})
	model = next.(Model)
	model.current = &taskdomain.TaskView{
		Task:          taskdomain.Task{ID: "task-1", Description: "Implement login", WorkDir: tempDir},
		Status:        taskdomain.TaskStatusRunning,
		ArtifactPaths: []string{oldPath, newPath},
		NodeRuns: []taskdomain.NodeRunView{
			{NodeRun: taskdomain.NodeRun{ID: "run-1", TaskID: "task-1", NodeName: "implement", Status: taskdomain.NodeRunRunning, StartedAt: time.Now().UTC()}},
		},
	}
	model.screen = ScreenRunning
	model.syncComponents()

	// Switch to artifacts tab to see the files
	model.activeDetailTab = DetailTabArtifacts
	model.focusRegion = FocusRegionArtifactFiles
	model.syncComponents()

	view := strippedView(model.View().Content)
	assert.Contains(t, view, "Preview · new.md")
	assert.Contains(t, view, "New")
	assert.NotContains(t, view, "# New")

	// Navigate up in file list
	next, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	model = next.(Model)
	view = strippedView(model.View().Content)
	assert.Contains(t, view, "Preview · old.md")
	assert.Contains(t, view, "Old")
	assert.NotContains(t, view, "# Old")
}

func TestCompletedTaskOpenedFromListShowsArtifactsPaneImmediately(t *testing.T) {
	tempDir := t.TempDir()
	artifactPath := filepath.Join(tempDir, "summary.md")
	require.NoError(t, os.WriteFile(artifactPath, []byte("# Summary\n"), 0o644))

	view := taskdomain.TaskView{
		Task:          taskdomain.Task{ID: "task-1", Description: "Implement login", WorkDir: tempDir},
		Status:        taskdomain.TaskStatusDone,
		ArtifactPaths: []string{artifactPath},
		NodeRuns: []taskdomain.NodeRunView{
			{NodeRun: taskdomain.NodeRun{ID: "run-1", TaskID: "task-1", NodeName: "verify", Status: taskdomain.NodeRunDone, StartedAt: time.Now().UTC()}},
		},
	}
	service := &fakeService{
		events: make(chan taskruntime.RunEvent, 8),
		tasks:  []taskdomain.TaskView{view},
		openViews: map[string]taskdomain.TaskView{
			"task-1": view,
		},
	}
	model := NewModel(service, tempDir, "", nil, "v0.1.0")
	next, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 32})
	model = next.(Model)
	next, _ = model.Update(tasksLoadedMsg{tasks: service.tasks})
	model = next.(Model)

	model = openFirstTaskFromList(t, model)

	screen := strippedView(model.View().Content)
	assert.Equal(t, ScreenComplete, model.screen)
	assert.Equal(t, DetailTabTimeline, model.activeDetailTab)
	assert.Equal(t, FocusRegionDetail, model.focusRegion)
	assert.Contains(t, screen, "Shift+Tab artifacts")
	assert.Contains(t, screen, "Esc back")
	assert.Contains(t, screen, "Ctrl+C quit")
}

func TestTabSwitchingBetweenTimelineAndArtifacts(t *testing.T) {
	tempDir := t.TempDir()
	artifactPath := filepath.Join(tempDir, "summary.md")
	require.NoError(t, os.WriteFile(artifactPath, []byte("# Summary\n\n- Ship it\n"), 0o644))

	model := NewModel(&fakeService{events: make(chan taskruntime.RunEvent, 8)}, tempDir, "", nil, "v0.1.0")
	next, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 32})
	model = next.(Model)
	model.current = &taskdomain.TaskView{
		Task:          taskdomain.Task{ID: "task-1", Description: "Implement login", WorkDir: tempDir},
		Status:        taskdomain.TaskStatusDone,
		ArtifactPaths: []string{artifactPath},
		NodeRuns: []taskdomain.NodeRunView{
			{NodeRun: taskdomain.NodeRun{ID: "run-1", TaskID: "task-1", NodeName: "verify", Status: taskdomain.NodeRunDone, StartedAt: time.Now().UTC()}},
		},
	}
	model.screen = ScreenComplete
	model.syncComponents()

	assert.Equal(t, FocusRegionDetail, model.focusRegion)
	assert.Equal(t, DetailTabTimeline, model.activeDetailTab)

	// Press Shift+Tab to switch to artifacts tab
	next, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	model = next.(Model)
	assert.Equal(t, DetailTabArtifacts, model.activeDetailTab)
	assert.Equal(t, FocusRegionArtifactFiles, model.focusRegion)

	screen := strippedView(model.View().Content)
	assert.Contains(t, screen, "Files")
	assert.Contains(t, screen, "Preview · summary.md")

	// Press Shift+Tab to switch back to timeline
	next, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	model = next.(Model)
	assert.Equal(t, DetailTabTimeline, model.activeDetailTab)
	assert.Equal(t, FocusRegionDetail, model.focusRegion)
}

func TestClarificationPanelVisibleAfterTabRoundTrip(t *testing.T) {
	tempDir := t.TempDir()
	art1 := filepath.Join(tempDir, "review-1.md")
	art2 := filepath.Join(tempDir, "implementation-1.md")
	art3 := filepath.Join(tempDir, "verify-1.md")
	require.NoError(t, os.WriteFile(art1, []byte("# Review\n\nLGTM\n"), 0o644))
	require.NoError(t, os.WriteFile(art2, []byte("# Impl\n\nDone\n"), 0o644))
	require.NoError(t, os.WriteFile(art3, []byte("# Verify\n\nPassed\n"), 0o644))

	model := NewModel(&fakeService{events: make(chan taskruntime.RunEvent, 8)}, tempDir, "", nil, "v0.1.0")
	next, _ := model.Update(tea.WindowSizeMsg{Width: 149, Height: 39})
	model = next.(Model)
	model.current = &taskdomain.TaskView{
		Task:   taskdomain.Task{ID: "task-1", Description: "Clarification with artifacts", WorkDir: tempDir},
		Status: taskdomain.TaskStatusAwaitingUser,
		NodeRuns: []taskdomain.NodeRunView{
			{NodeRun: taskdomain.NodeRun{ID: "run-r", TaskID: "task-1", NodeName: "review_plan", Status: taskdomain.NodeRunDone, StartedAt: time.Now().UTC()}, ArtifactPaths: []string{art1}},
			{NodeRun: taskdomain.NodeRun{ID: "run-a", TaskID: "task-1", NodeName: "approve_plan", Status: taskdomain.NodeRunDone, StartedAt: time.Now().UTC()}},
			{NodeRun: taskdomain.NodeRun{ID: "run-1", TaskID: "task-1", NodeName: "implement", Status: taskdomain.NodeRunAwaitingUser, StartedAt: time.Now().UTC()}, ArtifactPaths: []string{art2, art3}},
		},
		ArtifactPaths: []string{art1, art2, art3},
	}
	model.currentInput = &taskruntime.InputRequest{
		Kind:          taskruntime.InputKindClarification,
		TaskID:        "task-1",
		NodeRunID:     "run-1",
		NodeName:      "implement",
		ArtifactPaths: []string{art1, art2, art3},
		Questions: []taskdomain.ClarificationQuestion{{
			Question:     "Which path should we take?",
			WhyItMatters: "Determines the next implementation step.",
			Options: []taskdomain.ClarificationOption{
				{Label: "A", Description: "First approach"},
				{Label: "B", Description: "Second approach"},
				{Label: "C", Description: "Third approach"},
				{Label: "D", Description: "Other approach"},
			},
			MultiSelect: true,
		}},
	}
	model.screen = ScreenClarification
	model.syncComponents()

	// Verify panel visible on timeline tab
	view := strippedView(model.View().Content)
	assert.Contains(t, view, "Question 1/1", "panel should be visible before tab switch")

	// Switch to artifacts tab
	next, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	model = next.(Model)
	assert.Equal(t, DetailTabArtifacts, model.activeDetailTab)

	// Switch back to timeline
	next, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	model = next.(Model)
	assert.Equal(t, DetailTabTimeline, model.activeDetailTab)
	assert.Equal(t, FocusRegionDetail, model.focusRegion)

	view = strippedView(model.View().Content)
	t.Logf("View after round-trip:\n%s", view)
	assert.Contains(t, view, "Question 1/1", "panel should be visible after tab round-trip")
}

func TestClarificationArtifactsIncludePendingInputMarkdownWithoutPrioritizingIt(t *testing.T) {
	tempDir := t.TempDir()
	reviewPath := filepath.Join(tempDir, "review.md")
	inputPath := filepath.Join(tempDir, "input.md")
	require.NoError(t, os.WriteFile(reviewPath, []byte("# Review\n\nLGTM\n"), 0o644))
	require.NoError(t, os.WriteFile(inputPath, []byte("# Clarification History\n\n## Exchange 1\n\nAnswer: pending\n"), 0o644))

	model := NewModel(&fakeService{events: make(chan taskruntime.RunEvent, 8)}, tempDir, "", nil, "v0.1.0")
	next, _ := model.Update(tea.WindowSizeMsg{Width: 140, Height: 36})
	model = next.(Model)
	model.current = &taskdomain.TaskView{
		Task:          taskdomain.Task{ID: "task-1", Description: "Clarification with pending input", WorkDir: tempDir},
		Status:        taskdomain.TaskStatusAwaitingUser,
		ArtifactPaths: []string{reviewPath},
		NodeRuns: []taskdomain.NodeRunView{
			{NodeRun: taskdomain.NodeRun{ID: "run-1", TaskID: "task-1", NodeName: "review_plan", Status: taskdomain.NodeRunDone, StartedAt: time.Now().UTC()}, ArtifactPaths: []string{reviewPath}},
			{NodeRun: taskdomain.NodeRun{ID: "run-2", TaskID: "task-1", NodeName: "implement", Status: taskdomain.NodeRunAwaitingUser, StartedAt: time.Now().UTC()}},
		},
	}
	model.currentInput = &taskruntime.InputRequest{
		Kind:          taskruntime.InputKindClarification,
		TaskID:        "task-1",
		NodeRunID:     "run-2",
		NodeName:      "implement",
		ArtifactPaths: []string{reviewPath, inputPath},
		Questions: []taskdomain.ClarificationQuestion{{
			Question:     "Which path should we take?",
			WhyItMatters: "Determines the next implementation step.",
			Options:      []taskdomain.ClarificationOption{{Label: "A", Description: "First approach"}},
		}},
	}
	model.screen = ScreenClarification
	model.activeDetailTab = DetailTabArtifacts
	model.focusRegion = FocusRegionArtifactFiles
	model.syncComponents()

	require.Len(t, model.artifactItems, 2)
	assert.Equal(t, reviewPath, model.artifactItems[0].Path)
	assert.Equal(t, inputPath, model.artifactItems[1].Path)
	assert.Equal(t, 0, model.artifactIndex)

	view := strippedView(model.View().Content)
	assert.Contains(t, view, "Preview · review_plan (#1) · review.md")
	assert.Contains(t, view, "LGTM")
}

func TestArtifactTabCyclesViaTab(t *testing.T) {
	tempDir := t.TempDir()
	artifactPath := filepath.Join(tempDir, "summary.md")
	require.NoError(t, os.WriteFile(artifactPath, []byte("# Summary\n\n- Ship it\n"), 0o644))

	model := NewModel(&fakeService{events: make(chan taskruntime.RunEvent, 8)}, tempDir, "", nil, "v0.1.0")
	next, _ := model.Update(tea.WindowSizeMsg{Width: 96, Height: 24})
	model = next.(Model)
	model.current = &taskdomain.TaskView{
		Task:          taskdomain.Task{ID: "task-1", Description: "Implement login", WorkDir: tempDir},
		Status:        taskdomain.TaskStatusRunning,
		ArtifactPaths: []string{artifactPath},
		NodeRuns: []taskdomain.NodeRunView{
			{NodeRun: taskdomain.NodeRun{ID: "run-1", TaskID: "task-1", NodeName: "implement", Status: taskdomain.NodeRunRunning, StartedAt: time.Now().UTC()}},
		},
	}
	model.screen = ScreenRunning
	model.activeDetailTab = DetailTabArtifacts
	model.focusRegion = FocusRegionArtifactFiles
	model.syncComponents()

	assert.Equal(t, DetailTabArtifacts, model.activeDetailTab)

	// Tab cycles from files to preview
	next, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	model = next.(Model)
	assert.Equal(t, FocusRegionArtifactPreview, model.focusRegion)

	// Tab cycles from preview back to files
	next, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	model = next.(Model)
	assert.Equal(t, FocusRegionArtifactFiles, model.focusRegion)
	assert.Equal(t, DetailTabArtifacts, model.activeDetailTab)

	// Press Shift+Tab to switch back to timeline
	next, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	model = next.(Model)
	assert.Equal(t, DetailTabTimeline, model.activeDetailTab)
	assert.Equal(t, FocusRegionDetail, model.focusRegion)
}

func TestArtifactsTabCyclesOnlyBetweenArtifactPanesAcrossDetailScreens(t *testing.T) {
	tests := []struct {
		name            string
		screen          Screen
		status          taskdomain.TaskStatus
		inputKind       taskruntime.InputKind
		wantFilesFooter string
		wantPrevFooter  string
	}{
		{
			name:            "running",
			screen:          ScreenRunning,
			status:          taskdomain.TaskStatusRunning,
			wantFilesFooter: "↑↓ files  c copy path  Esc back  Tab artifacts  Shift+Tab timeline",
			wantPrevFooter:  "↑↓ scroll  c copy  Esc back  Tab files  Shift+Tab timeline",
		},
		{
			name:            "complete",
			screen:          ScreenComplete,
			status:          taskdomain.TaskStatusDone,
			wantFilesFooter: "↑↓ files  c copy path  Esc back  Tab artifacts  Shift+Tab timeline",
			wantPrevFooter:  "↑↓ scroll  c copy  Esc back  Tab files  Shift+Tab timeline",
		},
		{
			name:            "approval",
			screen:          ScreenApproval,
			status:          taskdomain.TaskStatusAwaitingUser,
			inputKind:       taskruntime.InputKindHumanNode,
			wantFilesFooter: "↑↓ files  c copy path  Esc back  Tab artifacts  Shift+Tab timeline",
			wantPrevFooter:  "↑↓ scroll  c copy  Esc back  Tab files  Shift+Tab timeline",
		},
		{
			name:            "clarification",
			screen:          ScreenClarification,
			status:          taskdomain.TaskStatusAwaitingUser,
			inputKind:       taskruntime.InputKindClarification,
			wantFilesFooter: "↑↓ files  c copy path  Esc back  Tab artifacts  Shift+Tab timeline",
			wantPrevFooter:  "↑↓ scroll  c copy  Esc back  Tab files  Shift+Tab timeline",
		},
		{
			name:            "failed",
			screen:          ScreenFailed,
			status:          taskdomain.TaskStatusFailed,
			wantFilesFooter: "↑↓ files  c copy path  Esc back  Tab artifacts  Shift+Tab timeline",
			wantPrevFooter:  "↑↓ scroll  c copy  Esc back  Tab files  Shift+Tab timeline",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model := artifactTabTestModel(t, tt.screen, tt.status, tt.inputKind)
			assert.Equal(t, []FocusRegion{FocusRegionArtifactFiles, FocusRegionArtifactPreview}, model.availableFocusRegions())

			footer := strippedView(model.renderDetailFooter(surfaceRect{Width: detailContentWidth(120, model.activeDetailTab)}))
			assert.Contains(t, footer, tt.wantFilesFooter)
			if tt.screen == ScreenApproval {
				assert.NotContains(t, footer, "Enter confirm")
			}

			next, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyTab})
			model = next.(Model)
			assert.Equal(t, FocusRegionArtifactPreview, model.focusRegion)

			footer = strippedView(model.renderDetailFooter(surfaceRect{Width: detailContentWidth(120, model.activeDetailTab)}))
			assert.Contains(t, footer, tt.wantPrevFooter)
			if tt.screen == ScreenApproval {
				assert.NotContains(t, footer, "Enter confirm")
			}

			next, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyTab})
			model = next.(Model)
			assert.Equal(t, FocusRegionArtifactFiles, model.focusRegion)

			next, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
			model = next.(Model)
			assert.Equal(t, DetailTabTimeline, model.activeDetailTab)
		})
	}
}

func TestApprovalArtifactsPaneIgnoresEnterConfirm(t *testing.T) {
	model := artifactTabTestModel(t, ScreenApproval, taskdomain.TaskStatusAwaitingUser, taskruntime.InputKindHumanNode)
	model.approval.choice = 1

	next, cmd := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = next.(Model)

	assert.Nil(t, cmd)
	assert.False(t, model.submittingInput)
	assert.Equal(t, 1, model.approval.choice)
	assert.Equal(t, FocusRegionArtifactFiles, model.focusRegion)
	assert.Equal(t, DetailTabArtifacts, model.activeDetailTab)
}

func TestArtifactCopyCopiesPathAndRawContents(t *testing.T) {
	capturePath := installFakeClipboard(t)
	model := artifactTabTestModel(t, ScreenComplete, taskdomain.TaskStatusDone, "")
	wantPath := selectedArtifactPath(model.artifactItems, model.artifactIndex)
	wantContents := "# Summary\n\n- Ship it\n"

	next, _ := model.Update(tea.KeyPressMsg{Text: "c", Code: 'c'})
	model = next.(Model)
	gotPath, err := os.ReadFile(capturePath)
	require.NoError(t, err)
	assert.Equal(t, wantPath, string(gotPath))
	assert.Empty(t, model.artifactErrorText)

	next, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	model = next.(Model)
	next, _ = model.Update(tea.KeyPressMsg{Text: "c", Code: 'c'})
	model = next.(Model)
	gotContents, err := os.ReadFile(capturePath)
	require.NoError(t, err)
	assert.Equal(t, wantContents, string(gotContents))
	assert.Empty(t, model.artifactErrorText)
}

func TestArtifactCopyShowsTransientFooterFeedback(t *testing.T) {
	model := artifactTabTestModel(t, ScreenComplete, taskdomain.TaskStatusDone, "")

	next, cmd := model.Update(tea.KeyPressMsg{Text: "c", Code: 'c'})
	model = next.(Model)
	require.NotNil(t, cmd)
	assert.Equal(t, "copied", model.artifactCopyStatus)

	footer := strippedView(model.renderDetailFooter(surfaceRect{Width: detailContentWidth(120, model.activeDetailTab)}))
	assert.Contains(t, footer, "↑↓ files  copied  Esc back  Tab artifacts  Shift+Tab timeline")
	assert.NotContains(t, footer, "c copy path")

	next, _ = model.Update(artifactCopyFeedbackExpiredMsg{token: model.artifactCopyToken})
	model = next.(Model)
	assert.Empty(t, model.artifactCopyStatus)

	footer = strippedView(model.renderDetailFooter(surfaceRect{Width: detailContentWidth(120, model.activeDetailTab)}))
	assert.Contains(t, footer, "↑↓ files  c copy path  Esc back  Tab artifacts  Shift+Tab timeline")
}

func TestArtifactPreviewCopyShowsTransientFooterFeedback(t *testing.T) {
	model := artifactTabTestModel(t, ScreenComplete, taskdomain.TaskStatusDone, "")

	next, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	model = next.(Model)

	next, cmd := model.Update(tea.KeyPressMsg{Text: "c", Code: 'c'})
	model = next.(Model)
	require.NotNil(t, cmd)
	assert.Equal(t, "copied", model.artifactCopyStatus)

	footer := strippedView(model.renderDetailFooter(surfaceRect{Width: detailContentWidth(120, model.activeDetailTab)}))
	assert.Contains(t, footer, "↑↓ scroll  copied  Esc back  Tab files  Shift+Tab timeline")
	assert.NotContains(t, footer, "c copy")

	next, _ = model.Update(artifactCopyFeedbackExpiredMsg{token: model.artifactCopyToken})
	model = next.(Model)
	assert.Empty(t, model.artifactCopyStatus)

	footer = strippedView(model.renderDetailFooter(surfaceRect{Width: detailContentWidth(120, model.activeDetailTab)}))
	assert.Contains(t, footer, "↑↓ scroll  c copy  Esc back  Tab files  Shift+Tab timeline")
}

func TestArtifactCopyFailureBannerClearsOnSelectionChange(t *testing.T) {
	setTaskTUIRuntimePath(t)

	tempDir := t.TempDir()
	firstPath := filepath.Join(tempDir, "first.md")
	secondPath := filepath.Join(tempDir, "second.md")
	require.NoError(t, os.WriteFile(firstPath, []byte("# First\n"), 0o644))
	require.NoError(t, os.WriteFile(secondPath, []byte("# Second\n"), 0o644))

	model := NewModel(&fakeService{events: make(chan taskruntime.RunEvent, 8)}, tempDir, "", nil, "v0.1.0")
	next, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 32})
	model = next.(Model)
	model.current = &taskdomain.TaskView{
		Task:          taskdomain.Task{ID: "task-1", Description: "Copy artifacts", WorkDir: tempDir},
		Status:        taskdomain.TaskStatusDone,
		ArtifactPaths: []string{firstPath, secondPath},
		NodeRuns: []taskdomain.NodeRunView{
			{NodeRun: taskdomain.NodeRun{ID: "run-1", TaskID: "task-1", NodeName: "verify", Status: taskdomain.NodeRunDone, StartedAt: time.Now().UTC()}},
		},
	}
	model.screen = ScreenComplete
	model.activeDetailTab = DetailTabArtifacts
	model.focusRegion = FocusRegionArtifactFiles
	model.syncComponents()

	next, _ = model.Update(tea.KeyPressMsg{Text: "c", Code: 'c'})
	model = next.(Model)
	assert.Contains(t, model.artifactErrorText, "Unable to copy artifact path")

	view := strippedView(model.View().Content)
	assert.Contains(t, view, "Unable to copy artifact path")

	next, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	model = next.(Model)
	assert.Empty(t, model.artifactErrorText)
}

func TestArtifactCopyFailureClearsSuccessFeedback(t *testing.T) {
	_ = installFakeClipboard(t)
	model := artifactTabTestModel(t, ScreenComplete, taskdomain.TaskStatusDone, "")

	next, cmd := model.Update(tea.KeyPressMsg{Text: "c", Code: 'c'})
	model = next.(Model)
	require.NotNil(t, cmd)
	assert.Equal(t, "copied", model.artifactCopyStatus)

	setTaskTUIRuntimePath(t)

	next, _ = model.Update(tea.KeyPressMsg{Text: "c", Code: 'c'})
	model = next.(Model)
	assert.Empty(t, model.artifactCopyStatus)
	assert.Contains(t, model.artifactErrorText, "Unable to copy artifact path")

	footer := strippedView(model.renderDetailFooter(surfaceRect{Width: detailContentWidth(120, model.activeDetailTab)}))
	assert.Contains(t, footer, "↑↓ files  c copy path  Esc back  Tab artifacts  Shift+Tab timeline")
	assert.NotContains(t, footer, "copied")
}

func TestArtifactPaneFocusReusesSharedDividerWithoutShiftingPreview(t *testing.T) {
	tempDir := t.TempDir()
	artifactPath := filepath.Join(tempDir, "summary.md")
	require.NoError(t, os.WriteFile(artifactPath, []byte("# Summary\n\n- Ship it\n"), 0o644))

	model := NewModel(&fakeService{events: make(chan taskruntime.RunEvent, 8)}, tempDir, "", nil, "v0.1.0")
	next, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 32})
	model = next.(Model)
	model.current = &taskdomain.TaskView{
		Task:          taskdomain.Task{ID: "task-1", Description: "Implement login", WorkDir: tempDir},
		Status:        taskdomain.TaskStatusRunning,
		ArtifactPaths: []string{artifactPath},
		NodeRuns: []taskdomain.NodeRunView{
			{NodeRun: taskdomain.NodeRun{ID: "run-1", TaskID: "task-1", NodeName: "implement", Status: taskdomain.NodeRunRunning, StartedAt: time.Now().UTC()}},
		},
	}
	model.screen = ScreenRunning
	model.activeDetailTab = DetailTabArtifacts
	model.focusRegion = FocusRegionArtifactFiles
	model.syncComponents()

	findPreviewLine := func(view string) string {
		for _, line := range strings.Split(view, "\n") {
			if strings.Contains(line, "Preview · summary.md") {
				return line
			}
		}
		t.Fatalf("preview header not found in view:\n%s", view)
		return ""
	}

	filesLine := findPreviewLine(strippedView(model.View().Content))
	filesIndex := strings.Index(filesLine, "Preview · summary.md")
	require.GreaterOrEqual(t, filesIndex, 0)
	assert.Equal(t, 2, strings.Count(filesLine[:filesIndex], "│"))
	assert.True(t, strings.Contains(filesLine[:filesIndex], "│"), "files border should remain present")

	next, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	model = next.(Model)
	assert.Equal(t, FocusRegionArtifactPreview, model.focusRegion)

	previewLine := findPreviewLine(strippedView(model.View().Content))
	previewIndex := strings.Index(previewLine, "Preview · summary.md")
	require.GreaterOrEqual(t, previewIndex, 0)
	assert.Equal(t, 2, strings.Count(previewLine[:previewIndex], "│"))
	assert.Equal(t, filesIndex, previewIndex)
}

func TestTabHintShowsInFooter(t *testing.T) {
	tempDir := t.TempDir()
	artifactPath := filepath.Join(tempDir, "summary.md")
	require.NoError(t, os.WriteFile(artifactPath, []byte("# Summary\n"), 0o644))

	model := NewModel(&fakeService{events: make(chan taskruntime.RunEvent, 8)}, tempDir, "", nil, "v0.1.0")
	next, _ := model.Update(tea.WindowSizeMsg{Width: 96, Height: 24})
	model = next.(Model)
	model.current = &taskdomain.TaskView{
		Task:          taskdomain.Task{ID: "task-1", Description: "Implement login", WorkDir: tempDir},
		Status:        taskdomain.TaskStatusDone,
		ArtifactPaths: []string{artifactPath},
		NodeRuns: []taskdomain.NodeRunView{
			{NodeRun: taskdomain.NodeRun{ID: "run-1", TaskID: "task-1", NodeName: "verify", Status: taskdomain.NodeRunDone, StartedAt: time.Now().UTC()}},
		},
	}
	model.screen = ScreenComplete
	model.syncComponents()

	screen := strippedView(model.View().Content)
	assert.Contains(t, screen, "Shift+Tab artifacts")
	assert.NotContains(t, screen, "[1] Timeline")
	assert.NotContains(t, screen, "[2] Artifacts")
	assert.Contains(t, screen, "Ctrl+C quit")
}

func TestArtifactPaneShowsCompactFileWindow(t *testing.T) {
	model := NewModel(&fakeService{events: make(chan taskruntime.RunEvent, 8)}, "/tmp/project", "", nil, "v0.1.0")
	model.artifactItems = []artifactItem{
		{Label: "one"}, {Label: "two"}, {Label: "three"}, {Label: "four"}, {Label: "five"}, {Label: "six"}, {Label: "seven"},
	}
	model.artifactIndex = 3

	lines := model.renderArtifactFileLines(80, artifactVisibleCapacity(len(model.artifactItems)))
	assert.Equal(t, 3, artifactVisibleCapacity(len(model.artifactItems)))
	assert.Len(t, lines, 5)
	assert.Contains(t, ansi.Strip(lines[0]), "earlier file")
	assert.Contains(t, ansi.Strip(lines[len(lines)-1]), "more file")
}

func TestArtifactDisplayPathKeepsArtifactRunDirectory(t *testing.T) {
	workDir := t.TempDir()
	taskPath := filepath.Join(workDir, ".muxagent", "tasks", "83c937d0-4c2b-4342-b20d-ab32733421fd", "artifacts", "01-draft_plan", "plan.md")
	nonTaskPath := filepath.Join(workDir, "docs", "notes.md")

	assert.Equal(t, ".muxagent/tasks/83c937d0/artifacts/01-draft_plan/plan.md", artifactDisplayPath(taskPath, workDir))
	assert.Equal(t, filepath.ToSlash(filepath.Join("docs", "notes.md")), artifactDisplayPath(nonTaskPath, workDir))
}

func TestFormatArtifactFileLabel(t *testing.T) {
	tests := []struct {
		name  string
		item  artifactItem
		width int
		want  string
	}{
		{
			name:  "left truncates unlabeled path",
			item:  artifactItem{DisplayPath: "artifacts/01-draft_plan/plan-1.md"},
			width: 24,
			want:  "…01-draft_plan/plan-1.md",
		},
		{
			name: "keeps provenance when width allows",
			item: artifactItem{
				SourceLabel: "draft_plan (#1)",
				DisplayPath: "artifacts/01-draft_plan/plan-1.md",
			},
			width: 42,
			want:  "draft_plan (#1) · …01-draft_plan/plan-1.md",
		},
		{
			name:  "short path stays unchanged",
			item:  artifactItem{DisplayPath: "plan.md"},
			width: 20,
			want:  "plan.md",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, formatArtifactFileLabel(tt.item, tt.width))
		})
	}
}

func TestRenderArtifactFileLinesShowsSuffixVisibleSelectedRow(t *testing.T) {
	model := NewModel(&fakeService{events: make(chan taskruntime.RunEvent, 8)}, "/tmp/project", "", nil, "v0.1.0")
	model.artifactItems = []artifactItem{{DisplayPath: "artifacts/01-draft_plan/plan-1.md"}}
	model.artifactIndex = 0

	lines := model.renderArtifactFileLines(26, 1)
	require.Len(t, lines, 1)
	assert.Equal(t, "> …01-draft_plan/plan-1.md", ansi.Strip(lines[0]))
}

func TestArtifactPaneLabelsFilesWithSourceNodeAndIteration(t *testing.T) {
	tempDir := t.TempDir()
	firstPlan := filepath.Join(tempDir, ".muxagent", "tasks", "task-1", "todo-first.md")
	secondPlan := filepath.Join(tempDir, ".muxagent", "tasks", "task-1", "todo-second.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(firstPlan), 0o755))
	require.NoError(t, os.WriteFile(firstPlan, []byte("# First\n"), 0o644))
	require.NoError(t, os.WriteFile(secondPlan, []byte("# Second\n"), 0o644))

	model := NewModel(&fakeService{events: make(chan taskruntime.RunEvent, 8)}, tempDir, "", nil, "v0.1.0")
	next, _ := model.Update(tea.WindowSizeMsg{Width: 132, Height: 34})
	model = next.(Model)
	model.current = &taskdomain.TaskView{
		Task:          taskdomain.Task{ID: "task-1", Description: "Implement login", WorkDir: tempDir},
		Status:        taskdomain.TaskStatusDone,
		ArtifactPaths: []string{firstPlan, secondPlan},
		NodeRuns: []taskdomain.NodeRunView{
			{NodeRun: taskdomain.NodeRun{ID: "run-1", TaskID: "task-1", NodeName: "draft_plan", Status: taskdomain.NodeRunDone}, ArtifactPaths: []string{firstPlan}},
			{NodeRun: taskdomain.NodeRun{ID: "run-2", TaskID: "task-1", NodeName: "review_plan", Status: taskdomain.NodeRunDone}},
			{NodeRun: taskdomain.NodeRun{ID: "run-3", TaskID: "task-1", NodeName: "draft_plan", Status: taskdomain.NodeRunDone}, ArtifactPaths: []string{secondPlan}},
		},
	}
	model.screen = ScreenRunning
	model.activeDetailTab = DetailTabArtifacts
	model.focusRegion = FocusRegionArtifactFiles
	model.syncComponents()

	view := strippedView(model.View().Content)
	assert.Contains(t, view, "draft_plan (#1)")
	assert.Contains(t, view, "draft_plan (#2)")
}

func TestMarkdownArtifactPreviewRendersMarkdownInsteadOfRawSource(t *testing.T) {
	tempDir := t.TempDir()
	artifactPath := filepath.Join(tempDir, "summary.md")
	require.NoError(t, os.WriteFile(artifactPath, []byte("# Summary\n\n- Ship it\n"), 0o644))

	model := NewModel(&fakeService{events: make(chan taskruntime.RunEvent, 8)}, tempDir, "", nil, "v0.1.0")
	next, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 32})
	model = next.(Model)
	model.current = &taskdomain.TaskView{
		Task:          taskdomain.Task{ID: "task-1", Description: "Implement login", WorkDir: tempDir},
		Status:        taskdomain.TaskStatusRunning,
		ArtifactPaths: []string{artifactPath},
		NodeRuns: []taskdomain.NodeRunView{
			{NodeRun: taskdomain.NodeRun{ID: "run-1", TaskID: "task-1", NodeName: "implement", Status: taskdomain.NodeRunRunning, StartedAt: time.Now().UTC()}},
		},
	}
	model.screen = ScreenRunning
	model.activeDetailTab = DetailTabArtifacts
	model.focusRegion = FocusRegionArtifactFiles
	model.syncComponents()

	view := strippedView(model.View().Content)
	assert.Contains(t, view, "Summary")
	assert.Contains(t, view, "Ship it")
	assert.NotContains(t, view, "# Summary")
}

func TestMarkdownArtifactPreviewFormatsDocumentStructure(t *testing.T) {
	tempDir := t.TempDir()
	artifactPath := filepath.Join(tempDir, "notes.md")
	content := "# Release Notes\n\n## Highlights\n\n- Faster startup\n- Better preview\n\n> Review before shipping.\n\n```go\nfmt.Println(\"ok\")\n```\n"
	require.NoError(t, os.WriteFile(artifactPath, []byte(content), 0o644))

	model := NewModel(&fakeService{events: make(chan taskruntime.RunEvent, 8)}, tempDir, "", nil, "v0.1.0")
	next, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 32})
	model = next.(Model)
	model.current = &taskdomain.TaskView{
		Task:          taskdomain.Task{ID: "task-1", Description: "Implement login", WorkDir: tempDir},
		Status:        taskdomain.TaskStatusRunning,
		ArtifactPaths: []string{artifactPath},
		NodeRuns: []taskdomain.NodeRunView{
			{NodeRun: taskdomain.NodeRun{ID: "run-1", TaskID: "task-1", NodeName: "implement", Status: taskdomain.NodeRunRunning, StartedAt: time.Now().UTC()}},
		},
	}
	model.screen = ScreenRunning
	model.activeDetailTab = DetailTabArtifacts
	model.focusRegion = FocusRegionArtifactFiles
	model.syncComponents()

	view := strippedView(model.View().Content)
	assert.Contains(t, view, "Release Notes")
	assert.Contains(t, view, "Highlights")
	assert.Contains(t, view, "Faster startup")
	assert.Contains(t, view, "Review before shipping.")
	assert.Contains(t, view, "fmt.Println(\"ok\")")
	assert.NotContains(t, view, "# Release Notes")
	assert.NotContains(t, view, "```go")
}

func TestArtifactMarkdownPreviewTracksViewportWidth(t *testing.T) {
	rendered, err := renderArtifactMarkdown(
		"# Artifact Preview\n\nThis paragraph should stay within a readable column width even when the viewport is much wider than the markdown surface needs for comfortable reading.",
		140,
	)
	require.NoError(t, err)

	maxLineWidth := 0
	for _, line := range strings.Split(rendered, "\n") {
		maxLineWidth = max(maxLineWidth, ansi.StringWidth(line))
	}
	assert.Equal(t, 140, artifactMarkdownWidth(140))
	assert.LessOrEqual(t, maxLineWidth, 140)
	assert.Greater(t, maxLineWidth, 76)
}

func TestArtifactMarkdownThemeUsesPrimaryReaderEmphasis(t *testing.T) {
	cfg := buildMarkdownTheme().Artifact

	require.NotNil(t, cfg.BlockQuote.StylePrimitive.BackgroundColor)
	assert.Equal(t, "#16202D", *cfg.BlockQuote.StylePrimitive.BackgroundColor)
	require.NotNil(t, cfg.BlockQuote.IndentToken)
	assert.Equal(t, "▎ ", *cfg.BlockQuote.IndentToken)

	require.NotNil(t, cfg.Strong.Color)
	assert.Equal(t, "#FFF8EE", *cfg.Strong.Color)
	require.NotNil(t, cfg.Strong.Bold)
	assert.True(t, *cfg.Strong.Bold)

	require.NotNil(t, cfg.Code.StylePrimitive.BackgroundColor)
	assert.Equal(t, "#243244", *cfg.Code.StylePrimitive.BackgroundColor)
	require.NotNil(t, cfg.Code.StylePrimitive.Bold)
	assert.True(t, *cfg.Code.StylePrimitive.Bold)

	require.NotNil(t, cfg.CodeBlock.Chroma)
	require.NotNil(t, cfg.CodeBlock.Chroma.Comment.Color)
	assert.Equal(t, "#AAB8C7", *cfg.CodeBlock.Chroma.Comment.Color)
}

func artifactTabTestModel(t *testing.T, screen Screen, status taskdomain.TaskStatus, inputKind taskruntime.InputKind) Model {
	t.Helper()

	tempDir := t.TempDir()
	artifactPath := filepath.Join(tempDir, "summary.md")
	require.NoError(t, os.WriteFile(artifactPath, []byte("# Summary\n\n- Ship it\n"), 0o644))

	model := NewModel(&fakeService{events: make(chan taskruntime.RunEvent, 8)}, tempDir, "", nil, "v0.1.0")
	next, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 32})
	model = next.(Model)
	model.current = &taskdomain.TaskView{
		Task:          taskdomain.Task{ID: "task-1", Description: "Implement login", WorkDir: tempDir},
		Status:        status,
		ArtifactPaths: []string{artifactPath},
		NodeRuns: []taskdomain.NodeRunView{
			{NodeRun: taskdomain.NodeRun{ID: "run-1", TaskID: "task-1", NodeName: "implement", Status: taskdomain.NodeRunRunning, StartedAt: time.Now().UTC()}},
		},
	}
	if screen == ScreenFailed {
		model.current.NodeRuns[0].Status = taskdomain.NodeRunFailed
	}
	if inputKind != "" {
		model.currentInput = &taskruntime.InputRequest{
			Kind:          inputKind,
			TaskID:        "task-1",
			NodeRunID:     "run-1",
			NodeName:      "implement",
			ArtifactPaths: []string{artifactPath},
			Questions: []taskdomain.ClarificationQuestion{{
				Question:     "Which path should we take?",
				WhyItMatters: "Determines the next implementation step.",
				Options:      []taskdomain.ClarificationOption{{Label: "A", Description: "First approach"}},
			}},
		}
	}
	model.screen = screen
	model.activeDetailTab = DetailTabArtifacts
	model.focusRegion = FocusRegionArtifactFiles
	model.syncComponents()
	return model
}

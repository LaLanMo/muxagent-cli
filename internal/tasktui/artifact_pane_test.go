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
	assert.Contains(t, view, "1 timeline")
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

	next, cmd := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = next.(Model)
	require.NotNil(t, cmd)
	msg := cmd()
	require.IsType(t, taskOpenedMsg{}, msg)
	next, _ = model.Update(msg)
	model = next.(Model)

	screen := strippedView(model.View().Content)
	assert.Equal(t, ScreenComplete, model.screen)
	assert.Equal(t, DetailTabTimeline, model.activeDetailTab)
	assert.Equal(t, FocusRegionDetail, model.focusRegion)
	assert.Contains(t, screen, "2 artifacts")
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

	// Press 2 to switch to artifacts tab
	next, _ = model.Update(tea.KeyPressMsg{Code: '2'})
	model = next.(Model)
	assert.Equal(t, DetailTabArtifacts, model.activeDetailTab)
	assert.Equal(t, FocusRegionArtifactFiles, model.focusRegion)

	screen := strippedView(model.View().Content)
	assert.Contains(t, screen, "Files")
	assert.Contains(t, screen, "Preview · summary.md")

	// Press 1 to switch back to timeline
	next, _ = model.Update(tea.KeyPressMsg{Code: '1'})
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
	next, _ = model.Update(tea.KeyPressMsg{Code: '2'})
	model = next.(Model)
	assert.Equal(t, DetailTabArtifacts, model.activeDetailTab)

	// Switch back to timeline
	next, _ = model.Update(tea.KeyPressMsg{Code: '1'})
	model = next.(Model)
	assert.Equal(t, DetailTabTimeline, model.activeDetailTab)
	assert.Equal(t, FocusRegionDetail, model.focusRegion)

	view = strippedView(model.View().Content)
	t.Logf("View after round-trip:\n%s", view)
	assert.Contains(t, view, "Question 1/1", "panel should be visible after tab round-trip")
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

	// Press 1 to switch back to timeline
	next, _ = model.Update(tea.KeyPressMsg{Code: '1'})
	model = next.(Model)
	assert.Equal(t, DetailTabTimeline, model.activeDetailTab)
	assert.Equal(t, FocusRegionDetail, model.focusRegion)
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
	assert.Contains(t, screen, "2 artifacts")
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
			{NodeRun: taskdomain.NodeRun{ID: "run-1", TaskID: "task-1", NodeName: "upsert_plan", Status: taskdomain.NodeRunDone}, ArtifactPaths: []string{firstPlan}},
			{NodeRun: taskdomain.NodeRun{ID: "run-2", TaskID: "task-1", NodeName: "review_plan", Status: taskdomain.NodeRunDone}},
			{NodeRun: taskdomain.NodeRun{ID: "run-3", TaskID: "task-1", NodeName: "upsert_plan", Status: taskdomain.NodeRunDone}, ArtifactPaths: []string{secondPlan}},
		},
	}
	model.screen = ScreenRunning
	model.activeDetailTab = DetailTabArtifacts
	model.focusRegion = FocusRegionArtifactFiles
	model.syncComponents()

	view := strippedView(model.View().Content)
	assert.Contains(t, view, "upsert_plan (#1)")
	assert.Contains(t, view, "upsert_plan (#2)")
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

func TestArtifactMarkdownPreviewCapsReadableWidth(t *testing.T) {
	rendered, err := renderArtifactMarkdown(
		"# Artifact Preview\n\nThis paragraph should stay within a readable column width even when the viewport is much wider than the markdown surface needs for comfortable reading.",
		140,
	)
	require.NoError(t, err)

	maxLineWidth := 0
	for _, line := range strings.Split(rendered, "\n") {
		maxLineWidth = max(maxLineWidth, ansi.StringWidth(line))
	}
	assert.LessOrEqual(t, maxLineWidth, artifactMarkdownWidth(140))
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

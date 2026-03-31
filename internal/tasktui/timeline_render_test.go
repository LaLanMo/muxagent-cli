package tasktui

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/LaLanMo/muxagent-cli/internal/taskconfig"
	"github.com/LaLanMo/muxagent-cli/internal/taskdomain"
	"github.com/LaLanMo/muxagent-cli/internal/taskexecutor"
	"github.com/LaLanMo/muxagent-cli/internal/taskruntime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDetailScreenShowsLatestFourRunningStreamMessagesAndThreadID(t *testing.T) {
	model := NewModel(&fakeService{events: make(chan taskruntime.RunEvent, 8)}, "/tmp/project", "", nil, "v0.1.0")
	next, _ := model.Update(tea.WindowSizeMsg{Width: 100, Height: 28})
	model = next.(Model)
	now := time.Now().UTC()
	model.current = &taskdomain.TaskView{
		Task:   taskdomain.Task{ID: "task-1", Description: "Implement login"},
		Status: taskdomain.TaskStatusRunning,
		NodeRuns: []taskdomain.NodeRunView{
			{NodeRun: taskdomain.NodeRun{ID: "run-1", TaskID: "task-1", NodeName: "implement", Status: taskdomain.NodeRunRunning, StartedAt: now}},
		},
	}
	model.activeTaskID = "task-1"
	model.screen = ScreenRunning
	model.handleEvent(taskruntime.RunEvent{
		Type:      taskruntime.EventNodeProgress,
		TaskID:    "task-1",
		NodeRunID: "run-1",
		NodeName:  "implement",
		Progress:  &taskruntime.ProgressInfo{SessionID: "thread-123"},
	})
	for _, message := range []string{"stream-one", "stream-two", "stream-three", "stream-four", "stream-five"} {
		model.handleEvent(taskruntime.RunEvent{
			Type:      taskruntime.EventNodeProgress,
			TaskID:    "task-1",
			NodeRunID: "run-1",
			NodeName:  "implement",
			Progress:  &taskruntime.ProgressInfo{Message: message},
		})
	}
	model.syncComponents()

	view := strippedView(model.View().Content)
	assert.Contains(t, view, "▌ ● implement")
	assert.Contains(t, view, "thread: thread-123")
	assert.NotContains(t, view, "stream-one")
	assert.Contains(t, view, "stream-two")
	assert.Contains(t, view, "stream-three")
	assert.Contains(t, view, "stream-four")
	assert.Contains(t, view, "stream-five")
}

func TestProgressLinesTruncateLongMessagesInsteadOfWrapping(t *testing.T) {
	lines := progressLines([]string{
		`{"type":"item.updated","message":"` + strings.Repeat("artifact stream ", 12) + `"}`,
	}, 18)

	require.Len(t, lines, 1)
	stripped := ansi.Strip(lines[0])
	assert.NotContains(t, stripped, "\n")
	assert.Contains(t, stripped, "…")
}

func TestDetailScreenRendersStructuredProgressEvents(t *testing.T) {
	model := NewModel(&fakeService{events: make(chan taskruntime.RunEvent, 8)}, "/tmp/project", "", nil, "v0.1.0")
	next, _ := model.Update(tea.WindowSizeMsg{Width: 100, Height: 28})
	model = next.(Model)
	now := time.Now().UTC()
	model.current = &taskdomain.TaskView{
		Task:   taskdomain.Task{ID: "task-1", Description: "Implement login"},
		Status: taskdomain.TaskStatusRunning,
		NodeRuns: []taskdomain.NodeRunView{
			{NodeRun: taskdomain.NodeRun{ID: "run-1", TaskID: "task-1", NodeName: "implement", Status: taskdomain.NodeRunRunning, StartedAt: now}},
		},
	}
	model.activeTaskID = "task-1"
	model.screen = ScreenRunning
	model.handleEvent(taskruntime.RunEvent{
		Type:      taskruntime.EventNodeProgress,
		TaskID:    "task-1",
		NodeRunID: "run-1",
		NodeName:  "implement",
		Progress: &taskruntime.ProgressInfo{
			SessionID: "thread-123",
			Events: []taskexecutor.StreamEvent{
				{
					Kind: taskexecutor.StreamEventKindPlan,
					Plan: &taskexecutor.PlanSnapshot{PlanID: "plan-1", Steps: []taskexecutor.PlanStep{
						{Text: "Inspect repo", Status: "completed"},
						{Text: "Update file", Status: "pending"},
					}},
				},
				{
					Kind: taskexecutor.StreamEventKindTool,
					Tool: &taskexecutor.ToolCall{
						CallID:       "tool-1",
						Kind:         taskexecutor.ToolKindEdit,
						Status:       taskexecutor.ToolStatusCompleted,
						InputSummary: "/tmp/project/sample.txt",
					},
				},
				{
					Kind: taskexecutor.StreamEventKindMessage,
					Message: &taskexecutor.MessagePart{
						Role: taskexecutor.MessageRoleAssistant,
						Type: taskexecutor.MessagePartTypeText,
						Text: "Updated sample.txt and wrote notes.",
					},
				},
			},
		},
	})
	model.syncComponents()

	view := strippedView(model.View().Content)
	assert.Contains(t, view, "thread: thread-123")
	assert.Contains(t, view, "plan: 1/2 complete, next Update file")
	assert.Contains(t, view, "✓ edit  /tmp/project/sample.txt")
	assert.NotContains(t, view, "edit done")
	assert.Contains(t, view, "assistant: Updated sample.txt and wrote notes.")
}

func TestDetailScreenPreservesCodexBaselineLabels(t *testing.T) {
	model := NewModel(&fakeService{events: make(chan taskruntime.RunEvent, 8)}, "/tmp/project", "", nil, "v0.1.0")
	next, _ := model.Update(tea.WindowSizeMsg{Width: 100, Height: 28})
	model = next.(Model)
	now := time.Now().UTC()
	model.current = &taskdomain.TaskView{
		Task:   taskdomain.Task{ID: "task-1", Description: "Implement login"},
		Status: taskdomain.TaskStatusRunning,
		NodeRuns: []taskdomain.NodeRunView{
			{NodeRun: taskdomain.NodeRun{ID: "run-1", TaskID: "task-1", NodeName: "implement", Status: taskdomain.NodeRunRunning, StartedAt: now}},
		},
	}
	model.activeTaskID = "task-1"
	model.screen = ScreenRunning
	model.handleEvent(taskruntime.RunEvent{
		Type:      taskruntime.EventNodeProgress,
		TaskID:    "task-1",
		NodeRunID: "run-1",
		NodeName:  "implement",
		Progress: &taskruntime.ProgressInfo{
			SessionID: "thread-123",
			Events: []taskexecutor.StreamEvent{
				{
					Kind: taskexecutor.StreamEventKindPlan,
					Plan: &taskexecutor.PlanSnapshot{PlanID: "plan-1", Steps: []taskexecutor.PlanStep{
						{Text: "planning changes", Status: "completed"},
						{Text: "editing files", Status: "pending"},
					}},
				},
				{
					Kind: taskexecutor.StreamEventKindTool,
					Tool: &taskexecutor.ToolCall{
						CallID:       "tool-1",
						Kind:         taskexecutor.ToolKindShell,
						Name:         "command_execution",
						Status:       taskexecutor.ToolStatusCompleted,
						InputSummary: "/bin/zsh -lc 'go test ./...'",
					},
				},
				{
					Kind: taskexecutor.StreamEventKindTool,
					Tool: &taskexecutor.ToolCall{
						CallID:       "tool-2",
						Kind:         taskexecutor.ToolKindFileChange,
						Name:         "file_change",
						Status:       taskexecutor.ToolStatusCompleted,
						InputSummary: "A artifact.md",
					},
				},
				{
					Kind: taskexecutor.StreamEventKindMessage,
					Message: &taskexecutor.MessagePart{
						Role: taskexecutor.MessageRoleAssistant,
						Type: taskexecutor.MessagePartTypeText,
						Text: "wrapping up",
					},
				},
			},
		},
	})
	model.syncComponents()

	view := strippedView(model.View().Content)
	assert.Contains(t, view, "plan: 1/2 complete, next editing files")
	assert.Contains(t, view, "✓ shell  /bin/zsh -lc 'go test ./...'")
	assert.Contains(t, view, "✓ files  A artifact.md")
	assert.Contains(t, view, "assistant: wrapping up")
	assert.NotContains(t, view, "command execution")
	assert.NotContains(t, view, "other")
}

func TestCompletedDetailShowsThreadIDWithoutOldStreamMessages(t *testing.T) {
	model := NewModel(&fakeService{events: make(chan taskruntime.RunEvent, 8)}, "/tmp/project", "", nil, "v0.1.0")
	next, _ := model.Update(tea.WindowSizeMsg{Width: 100, Height: 28})
	model = next.(Model)
	now := time.Now().UTC()
	model.current = &taskdomain.TaskView{
		Task:   taskdomain.Task{ID: "task-1", Description: "Implement login"},
		Status: taskdomain.TaskStatusDone,
		NodeRuns: []taskdomain.NodeRunView{
			{NodeRun: taskdomain.NodeRun{ID: "run-1", TaskID: "task-1", NodeName: "implement", Status: taskdomain.NodeRunDone, SessionID: "thread-123", StartedAt: now, CompletedAt: timePtr(now.Add(time.Minute))}},
		},
	}
	model.screen = ScreenComplete
	model.progressByRun["run-1"] = []string{"old-stream"}
	model.syncComponents()

	view := strippedView(model.View().Content)
	assert.Contains(t, view, "thread: thread-123")
	assert.NotContains(t, view, "old-stream")
}

func TestDetailScreenShowsIterationLabelsForRepeatedNodeRuns(t *testing.T) {
	model := NewModel(&fakeService{events: make(chan taskruntime.RunEvent, 8)}, "/tmp/project", "", nil, "v0.1.0")
	next, _ := model.Update(tea.WindowSizeMsg{Width: 100, Height: 28})
	model = next.(Model)
	now := time.Now().UTC()
	model.current = &taskdomain.TaskView{
		Task:   taskdomain.Task{ID: "task-1", Description: "Implement login"},
		Status: taskdomain.TaskStatusDone,
		NodeRuns: []taskdomain.NodeRunView{
			{NodeRun: taskdomain.NodeRun{ID: "run-1", TaskID: "task-1", NodeName: "draft_plan", Status: taskdomain.NodeRunDone, StartedAt: now}},
			{NodeRun: taskdomain.NodeRun{ID: "run-2", TaskID: "task-1", NodeName: "review_plan", Status: taskdomain.NodeRunDone, StartedAt: now.Add(time.Minute)}},
			{NodeRun: taskdomain.NodeRun{ID: "run-3", TaskID: "task-1", NodeName: "draft_plan", Status: taskdomain.NodeRunDone, StartedAt: now.Add(2 * time.Minute)}},
		},
	}
	model.screen = ScreenComplete
	model.syncComponents()

	view := strippedView(model.View().Content)
	assert.Contains(t, view, "✓ draft_plan")
	assert.Contains(t, view, "✓ draft_plan (#2)")
	assert.Contains(t, view, "✓ review_plan")
	assert.NotContains(t, view, "review_plan (#")
}

func TestTaskListMetaUsesHashIterationSuffixForRepeatedCurrentNode(t *testing.T) {
	view := taskdomain.TaskView{
		Task:            taskdomain.Task{ID: "task-1", Description: "Implement login"},
		Status:          taskdomain.TaskStatusAwaitingUser,
		CurrentNodeName: "approve_plan",
		NodeRuns: []taskdomain.NodeRunView{
			{NodeRun: taskdomain.NodeRun{ID: "run-1", NodeName: "draft_plan"}},
			{NodeRun: taskdomain.NodeRun{ID: "run-2", NodeName: "review_plan"}},
			{NodeRun: taskdomain.NodeRun{ID: "run-3", NodeName: "approve_plan"}},
			{NodeRun: taskdomain.NodeRun{ID: "run-4", NodeName: "draft_plan"}},
			{NodeRun: taskdomain.NodeRun{ID: "run-5", NodeName: "review_plan"}},
			{NodeRun: taskdomain.NodeRun{ID: "run-6", NodeName: "approve_plan"}},
		},
	}

	meta := taskListMeta(view)
	assert.Contains(t, meta, "approve_plan (#2)")
}

func TestSummarizeNodeRunIncludesApprovalFeedback(t *testing.T) {
	run := taskdomain.NodeRunView{
		NodeRun: taskdomain.NodeRun{
			ID:       "run-1",
			NodeName: "approve_plan",
			Result: map[string]interface{}{
				"approved": true,
				"feedback": "Looks good to me",
			},
		},
	}

	summary := summarizeNodeRun(run, nil)

	assert.Equal(t, "approved: true · feedback: Looks good to me", summary)
}

func TestTaskListMetaPlacesWorktreeBeforeConfig(t *testing.T) {
	view := taskdomain.TaskView{
		Task: taskdomain.Task{
			ID:           "task-1",
			Description:  "Implement login",
			ConfigAlias:  "reviewer",
			WorkDir:      "/tmp/project",
			ExecutionDir: "/tmp/worktrees/task-1",
		},
		Status:          taskdomain.TaskStatusRunning,
		CurrentNodeName: "implement",
	}

	meta := taskListMeta(view)
	worktreeIndex := strings.Index(meta, "worktree")
	configIndex := strings.Index(meta, "config reviewer")

	require.NotEqual(t, -1, worktreeIndex)
	require.NotEqual(t, -1, configIndex)
	assert.Less(t, worktreeIndex, configIndex)
	assert.Contains(t, meta, "at implement")
}

func TestTaskListDelegateRendersSelectedRowAsFullWidthBlock(t *testing.T) {
	delegate := taskListDelegate{}
	model := newTaskListModel()
	model.SetSize(48, 8)
	model.SetItems([]list.Item{
		taskListItem{
			view: taskdomain.TaskView{
				Task:   taskdomain.Task{ID: "task-1", Description: "create a hello.txt file in this dir"},
				Status: taskdomain.TaskStatusDone,
			},
		},
	})
	model.Select(0)

	var buf bytes.Buffer
	delegate.Render(&buf, model, 0, model.Items()[0])
	lines := strings.Split(strings.TrimSuffix(ansi.Strip(buf.String()), "\n"), "\n")
	require.Len(t, lines, 2)
	assert.Contains(t, lines[0], "❯ done")
	assert.Equal(t, model.Width(), ansi.StringWidth(lines[0]))
	assert.Equal(t, model.Width(), ansi.StringWidth(lines[1]))
}

func TestTaskListDelegateRendersAwaitingChipAndActionCopy(t *testing.T) {
	delegate := taskListDelegate{}

	render := func(view taskdomain.TaskView) string {
		model := newTaskListModel()
		model.SetSize(96, 8)
		model.SetItems([]list.Item{taskListItem{view: view}})
		model.Select(0)
		var buf bytes.Buffer
		delegate.Render(&buf, model, 0, model.Items()[0])
		return ansi.Strip(buf.String())
	}

	approval := render(taskdomain.TaskView{
		Task:            taskdomain.Task{ID: "task-1", Description: "approve task"},
		Status:          taskdomain.TaskStatusAwaitingUser,
		CurrentNodeType: taskconfig.NodeTypeHuman,
		CurrentNodeName: "approve_plan",
	})
	input := render(taskdomain.TaskView{
		Task:            taskdomain.Task{ID: "task-2", Description: "clarify task"},
		Status:          taskdomain.TaskStatusAwaitingUser,
		CurrentNodeType: taskconfig.NodeTypeAgent,
		CurrentNodeName: "draft_plan",
	})

	assert.Contains(t, approval, "awaiting approval")
	assert.Contains(t, approval, "approve task")
	assert.Contains(t, approval, "approve_plan")
	assert.NotContains(t, approval, "awaiting clarification")

	assert.Contains(t, input, "awaiting clarification")
	assert.Contains(t, input, "clarify task")
	assert.Contains(t, input, "draft_plan")
	assert.NotContains(t, input, "awaiting approval")
}

func TestTaskListDelegateUsesNeutralTitleStyleForRunningTasks(t *testing.T) {
	delegate := taskListDelegate{}
	model := newTaskListModel()
	model.SetSize(64, 8)
	model.SetItems([]list.Item{
		taskListItem{
			view: taskdomain.TaskView{
				Task:   taskdomain.Task{ID: "task-1", Description: "running task"},
				Status: taskdomain.TaskStatusRunning,
			},
		},
	})
	model.Select(0)

	var buf bytes.Buffer
	delegate.Render(&buf, model, 0, model.Items()[0])
	raw := buf.String()

	assert.Contains(t, ansi.Strip(raw), "❯ running running task")
	assert.Contains(t, raw, tuiTheme.Text.Body.Render("running task"))
	assert.NotContains(t, raw, tuiTheme.Status.Running.Render("running task"))
}

func TestTaskListDelegateShowsOnlyFirstLineOfMultilineDescriptions(t *testing.T) {
	delegate := taskListDelegate{}
	model := newTaskListModel()
	model.SetSize(72, 8)
	model.SetItems([]list.Item{
		taskListItem{
			view: taskdomain.TaskView{
				Task:            taskdomain.Task{ID: "task-1", Description: "first line\nsecond line"},
				Status:          taskdomain.TaskStatusRunning,
				CurrentNodeName: "implement",
			},
		},
	})
	model.Select(0)

	var buf bytes.Buffer
	delegate.Render(&buf, model, 0, model.Items()[0])
	lines := strings.Split(strings.TrimSuffix(ansi.Strip(buf.String()), "\n"), "\n")

	require.Len(t, lines, 2)
	assert.Contains(t, lines[0], "❯ running first line")
	assert.NotContains(t, ansi.Strip(buf.String()), "second line")
	assert.Contains(t, lines[1], "at implement")
}

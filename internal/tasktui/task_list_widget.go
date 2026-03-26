package tasktui

import (
	"fmt"
	"io"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/LaLanMo/muxagent-cli/internal/taskconfig"
	"github.com/LaLanMo/muxagent-cli/internal/taskdomain"
	"github.com/charmbracelet/x/ansi"
)

func newTaskListModel() list.Model {
	delegate := taskListDelegate{}
	model := list.New(nil, delegate, 0, 0)
	model.SetShowTitle(false)
	model.SetShowFilter(false)
	model.SetFilteringEnabled(false)
	model.SetShowStatusBar(false)
	model.SetShowHelp(false)
	model.SetShowPagination(false)
	model.DisableQuitKeybindings()
	model.Styles.NoItems = tuiTheme.Text.Empty
	model.KeyMap.CursorUp = key.NewBinding(key.WithKeys("up"))
	model.KeyMap.CursorDown = key.NewBinding(key.WithKeys("down"))
	model.KeyMap.NextPage = key.NewBinding()
	model.KeyMap.PrevPage = key.NewBinding()
	model.KeyMap.GoToStart = key.NewBinding()
	model.KeyMap.GoToEnd = key.NewBinding()
	model.KeyMap.Filter = key.NewBinding()
	model.KeyMap.ClearFilter = key.NewBinding()
	model.KeyMap.ShowFullHelp = key.NewBinding()
	model.KeyMap.CloseFullHelp = key.NewBinding()
	model.KeyMap.Quit = key.NewBinding()
	model.KeyMap.ForceQuit = key.NewBinding()
	return model
}

type taskListItem struct {
	action taskListAction
	title  string
	meta   string
	view   taskdomain.TaskView
}

func (i taskListItem) FilterValue() string {
	if i.action != taskListActionNone {
		return strings.TrimSpace(i.title + " " + i.meta)
	}
	return strings.TrimSpace(string(i.view.Status) + " " + i.view.Task.Description + " " + i.view.CurrentNodeName)
}

type taskListDelegate struct{}

func (d taskListDelegate) Height() int {
	return 2
}

func (d taskListDelegate) Spacing() int {
	return 1
}

func (d taskListDelegate) Update(msg tea.Msg, m *list.Model) tea.Cmd {
	return nil
}

func (d taskListDelegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	entry, ok := item.(taskListItem)
	if !ok {
		return
	}
	width := m.Width()
	if width <= 0 {
		width = 80
	}
	selected := index == m.Index()
	rowWidth := width
	contentWidth := max(12, rowWidth-2)
	rowStyle := taskListRowStyle(entry, selected, rowWidth)
	if entry.action != taskListActionNone {
		marker := "  "
		if selected {
			marker = "❯ "
		}
		title := fitLine(marker+tuiTheme.TaskList.Title.Render(entry.title), contentWidth)
		meta := fitLine("  "+tuiTheme.TaskList.Secondary.Render(entry.meta), contentWidth)
		fmt.Fprint(w, rowStyle.Render(lipgloss.JoinVertical(lipgloss.Left, title, meta)))
		return
	}
	statusText, statusStyle := taskStatusLabel(entry.view)
	marker := "  "
	if selected {
		marker = "❯ "
	}
	titleStyle := tuiTheme.Text.Body
	if entry.view.Status == taskdomain.TaskStatusRunning {
		titleStyle = tuiTheme.Status.Running
	}
	title := ansi.Truncate(entry.view.Task.Description, max(8, contentWidth-len(statusText)-4), "…")
	top := fitLine(statusStyle.Render(marker+statusText)+" "+titleStyle.Render(title), contentWidth)
	meta := fitLine("  "+tuiTheme.TaskList.Secondary.Render(taskListMeta(entry.view)), contentWidth)
	fmt.Fprint(w, rowStyle.Render(lipgloss.JoinVertical(lipgloss.Left, top, meta)))
}

func taskListRowStyle(entry taskListItem, selected bool, rowWidth int) lipgloss.Style {
	rowStyle := lipgloss.NewStyle().Width(rowWidth).Padding(0, 1)
	switch {
	case entry.action != taskListActionNone:
		rowStyle = rowStyle.Background(tuiTheme.TaskList.ActionBg)
		if selected {
			rowStyle = rowStyle.Background(tuiTheme.TaskList.SelectedBg)
		}
	case entry.view.Status == taskdomain.TaskStatusAwaitingUser:
		rowStyle = rowStyle.Background(tuiTheme.TaskList.AwaitingBg)
	case selected:
		rowStyle = rowStyle.Background(tuiTheme.TaskList.SelectedBg)
	}
	return rowStyle
}

func taskAwaitingLabel(view taskdomain.TaskView) string {
	switch view.CurrentNodeType {
	case taskconfig.NodeTypeHuman:
		return "awaiting approval"
	default:
		return "awaiting clarification"
	}
}

func taskStatusLabel(view taskdomain.TaskView) (string, lipgloss.Style) {
	if view.Status == taskdomain.TaskStatusFailed && view.CurrentIssue != nil && view.CurrentIssue.Kind == taskdomain.TaskIssueBlockedStep {
		return "blocked", tuiTheme.Status.Awaiting
	}
	switch view.Status {
	case taskdomain.TaskStatusDone:
		return "done", tuiTheme.Status.Done
	case taskdomain.TaskStatusFailed:
		return "failed", tuiTheme.Status.Failed
	case taskdomain.TaskStatusAwaitingUser:
		return taskAwaitingLabel(view), tuiTheme.Status.Awaiting
	default:
		return "running", tuiTheme.Status.Running
	}
}

func taskListMeta(view taskdomain.TaskView) string {
	nodeLabel := "starting"
	if view.CurrentNodeName != "" {
		nodeLabel = "at " + currentNodeListLabel(view)
	}
	if view.Status == taskdomain.TaskStatusDone {
		nodeLabel = "completed"
	}
	if view.Status == taskdomain.TaskStatusFailed {
		if view.CurrentIssue != nil && view.CurrentIssue.Kind == taskdomain.TaskIssueBlockedStep {
			nodeLabel = "blocked at " + currentNodeListLabel(view)
		} else {
			nodeLabel = "failed at " + currentNodeListLabel(view)
		}
	}
	return nodeLabel + " · " + relativeTime(taskTimestamp(view))
}

func currentNodeListLabel(view taskdomain.TaskView) string {
	nodeName := firstNonEmpty(view.CurrentNodeName, "task")
	if nodeName == "task" {
		return nodeName
	}
	if view.CurrentIssue != nil && view.CurrentIssue.Kind == taskdomain.TaskIssueBlockedStep && view.CurrentIssue.NodeName == nodeName {
		if view.CurrentIssue.Iteration <= 1 {
			return nodeName
		}
		return fmt.Sprintf("%s (#%d)", nodeName, view.CurrentIssue.Iteration)
	}
	ordinal := 0
	for _, run := range view.NodeRuns {
		if run.NodeName == nodeName {
			ordinal++
		}
	}
	if ordinal <= 1 {
		return nodeName
	}
	return fmt.Sprintf("%s (#%d)", nodeName, ordinal)
}

func taskTimestamp(view taskdomain.TaskView) time.Time {
	if view.Status == taskdomain.TaskStatusFailed && view.CurrentIssue != nil {
		return view.CurrentIssue.OccurredAt
	}
	latest := view.Task.UpdatedAt
	if len(view.NodeRuns) == 0 {
		return latest
	}
	latestRun := view.NodeRuns[len(view.NodeRuns)-1]
	if latestRun.CompletedAt != nil {
		latest = latestRun.CompletedAt.UTC()
	} else {
		latest = latestRun.StartedAt.UTC()
	}
	return latest
}

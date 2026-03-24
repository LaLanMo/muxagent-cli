package tasktui

import (
	"fmt"
	"image/color"
	"io"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/LaLanMo/muxagent-cli/internal/taskdomain"
	"github.com/charmbracelet/x/ansi"
)

var tuiTheme = newTheme()

type theme struct {
	bg                   color.Color
	panelBg              color.Color
	artifactPaneBg       color.Color
	artifactBlock        color.Color
	artifactRailBg       color.Color
	borderMuted          color.Color
	text                 color.Color
	muted                color.Color
	subtle               color.Color
	running              color.Color
	done                 color.Color
	failed               color.Color
	awaiting             color.Color
	awaitingRowBg        color.Color
	successBg            color.Color
	streamBg             color.Color
	streamBorder         color.Color
	artifactPane         lipgloss.Style
	artifactHeader       lipgloss.Style
	artifactHint         lipgloss.Style
	artifactDivider      lipgloss.Style
	artifactBlockStyle   lipgloss.Style
	artifactBlockTitle   lipgloss.Style
	artifactFileActive   lipgloss.Style
	artifactFileInactive lipgloss.Style
	artifactPreviewText  lipgloss.Style
	artifactEmpty        lipgloss.Style
	artifactRail         lipgloss.Style
	artifactRailBadge    lipgloss.Style
	artifactRailDots     lipgloss.Style
	artifactRailHint     lipgloss.Style
	canvas               lipgloss.Style
	brand                lipgloss.Style
	version              lipgloss.Style
	taskLabel            lipgloss.Style
	body                 lipgloss.Style
	mutedText            lipgloss.Style
	subtleText           lipgloss.Style
	runningText          lipgloss.Style
	doneText             lipgloss.Style
	failedText           lipgloss.Style
	awaitingText         lipgloss.Style
	lineMuted            lipgloss.Style
	divider              lipgloss.Style
	emptyState           lipgloss.Style
	footerHint           lipgloss.Style
	footerStrong         lipgloss.Style
	successLine          lipgloss.Style
	modal                lipgloss.Style
	modalTitle           lipgloss.Style
	modalSubtitle        lipgloss.Style
	inputChrome          lipgloss.Style
	panel                lipgloss.Style
	panelWarning         lipgloss.Style
	panelDanger          lipgloss.Style
	panelTitle           lipgloss.Style
	panelBody            lipgloss.Style
	streamPanel          lipgloss.Style
	streamThread         lipgloss.Style
	streamJSON           lipgloss.Style
	optionActive         lipgloss.Style
	optionInactive       lipgloss.Style
}

func newTheme() theme {
	bg := lipgloss.Color("#090909")
	panelBg := lipgloss.Color("#1A1A1A")
	artifactPaneBg := lipgloss.Color("#151D2A")
	artifactBlock := lipgloss.Color("#0B111B")
	artifactRailBg := lipgloss.Color("#17202C")
	borderMuted := lipgloss.Color("#303030")
	text := lipgloss.Color("#ECE7DF")
	muted := lipgloss.Color("#8A857F")
	subtle := lipgloss.Color("#5F5A54")
	running := lipgloss.Color("#D77757")
	done := lipgloss.Color("#4EBA65")
	failed := lipgloss.Color("#FF6B80")
	awaiting := lipgloss.Color("#FFC107")
	awaitingRowBg := lipgloss.Color("#2A2000")
	streamBg := lipgloss.Color("#1A1A1A")
	streamBorder := lipgloss.Color("#343C4C")

	return theme{
		bg:             bg,
		panelBg:        panelBg,
		artifactPaneBg: artifactPaneBg,
		artifactBlock:  artifactBlock,
		artifactRailBg: artifactRailBg,
		borderMuted:    borderMuted,
		text:           text,
		muted:          muted,
		subtle:         subtle,
		running:        running,
		done:           done,
		failed:         failed,
		awaiting:       awaiting,
		awaitingRowBg:  awaitingRowBg,
		successBg:      lipgloss.Color("#102113"),
		streamBg:       streamBg,
		streamBorder:   streamBorder,
		artifactPane: lipgloss.NewStyle().
			Background(artifactPaneBg).
			Padding(0, 1),
		artifactHeader: lipgloss.NewStyle().
			Foreground(text).
			Bold(true),
		artifactHint: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#94A3B8")),
		artifactDivider: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#314155")),
		artifactBlockStyle: lipgloss.NewStyle().
			Background(artifactBlock).
			Padding(0, 1),
		artifactBlockTitle: lipgloss.NewStyle().
			Foreground(text).
			Bold(true),
		artifactFileActive: lipgloss.NewStyle().
			Foreground(awaiting).
			Bold(true),
		artifactFileInactive: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#CBD5E1")),
		artifactPreviewText: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#E2E8F0")),
		artifactEmpty: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#94A3B8")),
		artifactRail: lipgloss.NewStyle().
			Background(artifactRailBg).
			Border(lipgloss.NormalBorder(), false, false, false, true).
			BorderForeground(lipgloss.Color("#94A3B8")).
			Padding(1, 1),
		artifactRailBadge: lipgloss.NewStyle().
			Foreground(text).
			Background(lipgloss.Color("#64748B")).
			Bold(true).
			Padding(0, 1),
		artifactRailDots: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#CBD5E1")),
		artifactRailHint: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#CBD5E1")),
		canvas: lipgloss.NewStyle().
			Foreground(text).
			Background(bg).
			Padding(1, 2),
		brand: lipgloss.NewStyle().
			Foreground(text).
			Bold(true),
		version: lipgloss.NewStyle().
			Foreground(subtle),
		taskLabel: lipgloss.NewStyle().
			Foreground(text).
			Bold(true),
		body: lipgloss.NewStyle().
			Foreground(text),
		mutedText: lipgloss.NewStyle().
			Foreground(muted),
		subtleText: lipgloss.NewStyle().
			Foreground(subtle),
		runningText: lipgloss.NewStyle().
			Foreground(running),
		doneText: lipgloss.NewStyle().
			Foreground(done),
		failedText: lipgloss.NewStyle().
			Foreground(failed),
		awaitingText: lipgloss.NewStyle().
			Foreground(awaiting),
		lineMuted: lipgloss.NewStyle().
			Foreground(subtle),
		divider: lipgloss.NewStyle().
			Foreground(borderMuted),
		emptyState: lipgloss.NewStyle().
			Foreground(muted),
		footerHint: lipgloss.NewStyle().
			Foreground(subtle),
		footerStrong: lipgloss.NewStyle().
			Foreground(muted),
		successLine: lipgloss.NewStyle().
			Foreground(done).
			Bold(true),
		modal: lipgloss.NewStyle().
			Background(panelBg).
			Padding(1, 2).
			Width(58),
		modalTitle: lipgloss.NewStyle().
			Foreground(text).
			Bold(true),
		modalSubtitle: lipgloss.NewStyle().
			Foreground(muted),
		inputChrome: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#77716B")).
			Padding(0, 1),
		panel: lipgloss.NewStyle().
			Background(panelBg).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(borderMuted).
			Padding(1, 2),
		panelWarning: lipgloss.NewStyle().
			Background(panelBg).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(awaiting).
			Padding(1, 2),
		panelDanger: lipgloss.NewStyle().
			Background(panelBg).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(failed).
			Padding(1, 2),
		panelTitle: lipgloss.NewStyle().
			Foreground(text).
			Bold(true),
		panelBody: lipgloss.NewStyle().
			Foreground(muted),
		streamPanel: func() lipgloss.Style {
			border := lipgloss.RoundedBorder()
			border.Left = "▌"
			return lipgloss.NewStyle().
				Background(streamBg).
				BorderStyle(border).
				BorderTopForeground(streamBorder).
				BorderRightForeground(streamBorder).
				BorderBottomForeground(streamBorder).
				BorderLeftForeground(running).
				Padding(0, 1)
		}(),
		streamThread: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#A4AFBF")).
			Background(streamBg),
		streamJSON: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#D7DFEA")).
			Background(streamBg),
		optionActive: lipgloss.NewStyle().
			Bold(true).
			Foreground(awaiting),
		optionInactive: lipgloss.NewStyle().
			Foreground(muted),
	}
}

type appKeyMap struct {
	quit            key.Binding
	newTask         key.Binding
	open            key.Binding
	back            key.Binding
	confirm         key.Binding
	up              key.Binding
	down            key.Binding
	left            key.Binding
	right           key.Binding
	previewUp       key.Binding
	previewDown     key.Binding
	toggleArtifacts key.Binding
	retry           key.Binding
	forceRetry      key.Binding
}

func newAppKeyMap() appKeyMap {
	return appKeyMap{
		quit:            key.NewBinding(key.WithKeys("ctrl+c")),
		newTask:         key.NewBinding(key.WithKeys("ctrl+n")),
		open:            key.NewBinding(key.WithKeys("enter")),
		back:            key.NewBinding(key.WithKeys("esc")),
		confirm:         key.NewBinding(key.WithKeys("enter")),
		up:              key.NewBinding(key.WithKeys("up")),
		down:            key.NewBinding(key.WithKeys("down")),
		left:            key.NewBinding(key.WithKeys("left")),
		right:           key.NewBinding(key.WithKeys("right")),
		previewUp:       key.NewBinding(key.WithKeys("ctrl+u")),
		previewDown:     key.NewBinding(key.WithKeys("ctrl+d")),
		toggleArtifacts: key.NewBinding(key.WithKeys("tab")),
		retry:           key.NewBinding(key.WithKeys("r")),
		forceRetry:      key.NewBinding(key.WithKeys("R")),
	}
}

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
	model.Styles.NoItems = tuiTheme.emptyState
	model.KeyMap.CursorUp = key.NewBinding(key.WithKeys("up"))
	model.KeyMap.CursorDown = key.NewBinding(key.WithKeys("down"))
	model.KeyMap.NextPage = key.NewBinding(key.WithKeys("pgdown"))
	model.KeyMap.PrevPage = key.NewBinding(key.WithKeys("pgup"))
	model.KeyMap.GoToStart = key.NewBinding(key.WithKeys("home"))
	model.KeyMap.GoToEnd = key.NewBinding(key.WithKeys("end"))
	model.KeyMap.Filter = key.NewBinding()
	model.KeyMap.ClearFilter = key.NewBinding()
	model.KeyMap.ShowFullHelp = key.NewBinding()
	model.KeyMap.CloseFullHelp = key.NewBinding()
	model.KeyMap.Quit = key.NewBinding()
	model.KeyMap.ForceQuit = key.NewBinding()
	return model
}

func newTaskTextArea() textarea.Model {
	input := textarea.New()
	styles := textarea.DefaultDarkStyles()
	styles.Focused.Prompt = lipgloss.NewStyle().Foreground(tuiTheme.muted)
	styles.Focused.Text = lipgloss.NewStyle().Foreground(tuiTheme.text)
	styles.Focused.Placeholder = lipgloss.NewStyle().Foreground(tuiTheme.subtle)
	styles.Focused.CursorLine = lipgloss.NewStyle()
	styles.Focused.LineNumber = lipgloss.NewStyle().Foreground(tuiTheme.subtle)
	styles.Focused.CursorLineNumber = lipgloss.NewStyle().Foreground(tuiTheme.muted)
	styles.Blurred = styles.Focused
	styles.Cursor.Color = tuiTheme.text
	input.SetStyles(styles)
	input.Prompt = ""
	input.Placeholder = "Describe your task..."
	input.CharLimit = 512
	input.ShowLineNumbers = false
	input.SetHeight(1)
	input.MaxHeight = 0
	input.KeyMap.InsertNewline = key.NewBinding(key.WithKeys("ctrl+j"))
	return input
}

func newDetailTextArea() textarea.Model {
	input := textarea.New()
	styles := textarea.DefaultDarkStyles()
	styles.Focused.Prompt = lipgloss.NewStyle().Foreground(tuiTheme.muted)
	styles.Focused.Text = lipgloss.NewStyle().Foreground(tuiTheme.text)
	styles.Focused.Placeholder = lipgloss.NewStyle().Foreground(tuiTheme.subtle)
	styles.Focused.CursorLine = lipgloss.NewStyle()
	styles.Focused.LineNumber = lipgloss.NewStyle().Foreground(tuiTheme.subtle)
	styles.Focused.CursorLineNumber = lipgloss.NewStyle().Foreground(tuiTheme.muted)
	styles.Blurred = styles.Focused
	styles.Cursor.Color = tuiTheme.text
	input.SetStyles(styles)
	input.Prompt = ""
	input.Placeholder = "Type feedback..."
	input.CharLimit = 512
	input.ShowLineNumbers = false
	input.SetHeight(1)
	input.MaxHeight = 0
	input.KeyMap.InsertNewline = key.NewBinding(key.WithKeys("ctrl+j"))
	return input
}

func newArtifactPreviewViewport() viewport.Model {
	model := viewport.New()
	model.SoftWrap = true
	model.FillHeight = true
	model.KeyMap.Up = key.NewBinding()
	model.KeyMap.Down = key.NewBinding()
	model.KeyMap.PageUp = key.NewBinding()
	model.KeyMap.PageDown = key.NewBinding()
	model.KeyMap.HalfPageUp = key.NewBinding(key.WithKeys("ctrl+u"))
	model.KeyMap.HalfPageDown = key.NewBinding(key.WithKeys("ctrl+d"))
	model.KeyMap.Left = key.NewBinding()
	model.KeyMap.Right = key.NewBinding()
	return model
}

func newDetailViewport() viewport.Model {
	model := viewport.New()
	model.SoftWrap = true
	model.FillHeight = true
	model.KeyMap.Up = key.NewBinding(key.WithKeys("up"))
	model.KeyMap.Down = key.NewBinding(key.WithKeys("down"))
	model.KeyMap.PageUp = key.NewBinding(key.WithKeys("pgup"))
	model.KeyMap.PageDown = key.NewBinding(key.WithKeys("pgdown"))
	model.KeyMap.HalfPageUp = key.NewBinding(key.WithKeys("ctrl+u"))
	model.KeyMap.HalfPageDown = key.NewBinding(key.WithKeys("ctrl+d"))
	model.KeyMap.Left = key.NewBinding()
	model.KeyMap.Right = key.NewBinding()
	return model
}

type taskListItem struct {
	view taskdomain.TaskView
}

func (i taskListItem) FilterValue() string {
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
	statusText, statusStyle := taskStatusLabel(entry.view.Status)
	marker := "  "
	if selected {
		marker = "❯ "
	}
	titleStyle := tuiTheme.body
	if entry.view.Status == taskdomain.TaskStatusRunning {
		titleStyle = tuiTheme.runningText
	}
	title := ansi.Truncate(entry.view.Task.Description, max(8, contentWidth-len(statusText)-4), "…")
	top := fitLine(statusStyle.Render(marker+statusText)+" "+titleStyle.Render(title), contentWidth)
	meta := fitLine("  "+tuiTheme.mutedText.Render(taskListMeta(entry.view)), contentWidth)

	rowStyle := lipgloss.NewStyle().Width(rowWidth).Padding(0, 1)
	if entry.view.Status == taskdomain.TaskStatusAwaitingUser {
		rowStyle = rowStyle.Background(tuiTheme.awaitingRowBg)
	}
	fmt.Fprint(w, rowStyle.Render(lipgloss.JoinVertical(lipgloss.Left, top, meta)))
}

func taskStatusLabel(status taskdomain.TaskStatus) (string, lipgloss.Style) {
	switch status {
	case taskdomain.TaskStatusDone:
		return "done", tuiTheme.doneText
	case taskdomain.TaskStatusFailed:
		return "failed", tuiTheme.failedText
	case taskdomain.TaskStatusAwaitingUser:
		return "awaiting", tuiTheme.awaitingText
	default:
		return "running", tuiTheme.runningText
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
		nodeLabel = "failed at " + currentNodeListLabel(view)
	}
	return nodeLabel + " · " + relativeTime(taskTimestamp(view))
}

func currentNodeListLabel(view taskdomain.TaskView) string {
	nodeName := firstNonEmpty(view.CurrentNodeName, "task")
	if nodeName == "task" {
		return nodeName
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
	if len(view.NodeRuns) == 0 {
		return view.Task.UpdatedAt
	}
	latest := view.NodeRuns[len(view.NodeRuns)-1]
	if latest.CompletedAt != nil {
		return latest.CompletedAt.UTC()
	}
	return latest.StartedAt.UTC()
}

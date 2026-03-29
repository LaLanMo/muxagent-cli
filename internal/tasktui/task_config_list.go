package tasktui

import (
	"fmt"
	"io"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

type taskConfigListItem struct {
	summary taskConfigSummary
}

func newTaskConfigListModel() list.Model {
	delegate := taskConfigListDelegate{}
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

func (i taskConfigListItem) FilterValue() string {
	return strings.TrimSpace(i.summary.Alias + " " + i.summary.Runtime + " " + i.summary.Description + " " + i.summary.LoadErr)
}

type taskConfigListDelegate struct{}

func (d taskConfigListDelegate) Height() int { return 2 }

func (d taskConfigListDelegate) Spacing() int { return 1 }

func (d taskConfigListDelegate) Update(msg tea.Msg, m *list.Model) tea.Cmd { return nil }

func (d taskConfigListDelegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	entry, ok := item.(taskConfigListItem)
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
	rowStyle := lipgloss.NewStyle().Width(rowWidth).Padding(0, 1)
	if selected {
		rowStyle = rowStyle.Background(tuiTheme.TaskList.SelectedBg)
	}

	marker := "  "
	if selected {
		marker = "❯ "
	}
	status := ""
	statusStyle := tuiTheme.Header.MetaValue
	switch {
	case entry.summary.LoadErr != "":
		status = "invalid"
		statusStyle = tuiTheme.Status.Failed
	case entry.summary.IsDefault:
		status = "default"
		statusStyle = tuiTheme.Status.Done
	}
	title := tuiTheme.TaskList.Title.Render(entry.summary.Alias)
	if status != "" {
		title = title + " " + statusStyle.Render("["+status+"]")
	}
	top := fitLine(marker+title, contentWidth)

	metaParts := []string{}
	metaParts = append(metaParts, entry.summary.ownershipLabel())
	if entry.summary.Description != "" {
		metaParts = append(metaParts, entry.summary.Description)
	}
	if entry.summary.Runtime != "" {
		metaParts = append(metaParts, entry.summary.Runtime)
	}
	if len(entry.summary.NodeNames) > 0 {
		metaParts = append(metaParts, strings.Join(entry.summary.NodeNames, ", "))
	}
	if entry.summary.LoadErr != "" {
		metaParts = append(metaParts, truncateInline(entry.summary.LoadErr, max(18, contentWidth/2)))
	}
	meta := fitLine("  "+tuiTheme.TaskList.Secondary.Render(strings.Join(metaParts, " · ")), contentWidth)
	fmt.Fprint(w, rowStyle.Render(lipgloss.JoinVertical(lipgloss.Left, top, meta)))
}

func truncateInline(text string, width int) string {
	text = strings.TrimSpace(strings.ReplaceAll(text, "\n", " "))
	if text == "" {
		return ""
	}
	return ansi.Truncate(text, max(1, width), "…")
}

func (s taskConfigSummary) ownershipLabel() string {
	if s.Builtin {
		return "builtin"
	}
	return "custom"
}

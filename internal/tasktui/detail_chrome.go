package tasktui

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/LaLanMo/muxagent-cli/internal/taskdomain"
)

func (m Model) renderAppHeader(width int) string {
	brand := tuiTheme.Header.Brand.Render("muxagent")
	version := tuiTheme.Header.Version.Render(" " + normalizeVersionLabel(m.version))
	return fitLine(brand+version, width)
}

func (m Model) renderDetailHeader(width int) string {
	if m.current == nil {
		return fitLine(tuiTheme.Header.TaskLabel.Render("Task"), width)
	}
	title := tuiTheme.Header.TaskLabel.Render(clampWrappedText("Task: "+m.current.Task.Description, detailTitleMeasureWidth(width), 2))
	summary := m.renderDetailSummaryLine(width)
	lineage := m.renderDetailLineageLine(width)
	stageStrip := m.renderDetailStageStrip(width)
	divider := tuiTheme.App.Divider.Render(strings.Repeat("─", max(8, width)))
	lines := []string{title}
	if strings.TrimSpace(summary) != "" {
		lines = append(lines, summary)
	}
	if strings.TrimSpace(lineage) != "" {
		lines = append(lines, lineage)
	}
	if strings.TrimSpace(stageStrip) != "" {
		lines = append(lines, stageStrip)
	}
	lines = append(lines, divider)
	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

func clampWrappedText(text string, width, maxLines int) string {
	width = max(1, width)
	maxLines = max(1, maxLines)
	lines := strings.Split(ansi.Wrap(text, width, ""), "\n")
	lines = trimTrailingBlank(lines)
	if len(lines) <= maxLines {
		return strings.Join(lines, "\n")
	}
	lines = append([]string(nil), lines[:maxLines]...)
	lines[maxLines-1] = ansi.Truncate(lines[maxLines-1], width, "…")
	return strings.Join(lines, "\n")
}

func (m Model) renderDetailSummaryLine(width int) string {
	if m.current == nil {
		return ""
	}
	statusText, statusStyle := taskStatusLabel(*m.current)
	parts := []string{statusStyle.Render(statusText)}
	if label := detailCurrentNodeSummary(*m.current); label != "" {
		parts = append(parts, tuiTheme.Header.MetaValue.Render(label))
	}
	if taskUsesWorktree(m.current.Task) {
		parts = append(parts, tuiTheme.Header.MetaStrong.Render("worktree"))
	}
	if alias := strings.TrimSpace(m.current.Task.ConfigAlias); alias != "" {
		parts = append(parts, tuiTheme.Header.MetaValue.Render("config "+alias))
	}
	return fitLine(ansi.Truncate(strings.Join(parts, tuiTheme.Header.MetaLabel.Render(" · ")), width, "…"), width)
}

func (m Model) renderDetailLineageLine(width int) string {
	if m.current == nil || strings.TrimSpace(m.current.ParentTaskID) == "" {
		return ""
	}
	label := taskListPrimaryDescription(strings.TrimSpace(m.current.ParentTaskDescription))
	if label == "" {
		label = "(no description)"
	}
	line := tuiTheme.Header.MetaLabel.Render("follow-up of ") + tuiTheme.Header.MetaStrong.Render(label)
	return fitLine(ansi.Truncate(line, width, "…"), width)
}

func detailCurrentNodeSummary(view taskdomain.TaskView) string {
	if view.Status == taskdomain.TaskStatusDone {
		return ""
	}
	if view.CurrentNodeName == "" {
		return "starting"
	}
	return "at " + currentNodeListLabel(view)
}

func (m Model) renderDetailStageStrip(width int) string {
	cfg := m.currentConfig
	if cfg == nil {
		cfg = m.selectedTaskConfig()
	}
	if cfg == nil {
		return ""
	}
	states := map[string]string{}
	for _, run := range m.current.NodeRuns {
		switch run.Status {
		case taskdomain.NodeRunDone:
			if states[run.NodeName] == "" {
				states[run.NodeName] = "done"
			}
		case taskdomain.NodeRunFailed:
			states[run.NodeName] = "failed"
		case taskdomain.NodeRunAwaitingUser:
			states[run.NodeName] = "current"
		case taskdomain.NodeRunRunning:
			states[run.NodeName] = "current"
		}
	}
	for _, step := range m.current.BlockedSteps {
		states[step.NodeName] = "blocked"
	}
	if m.current != nil && m.current.Status == taskdomain.TaskStatusDone {
		states[m.current.CurrentNodeName] = "done"
	}
	if m.current != nil && (m.current.Status == taskdomain.TaskStatusRunning || m.current.Status == taskdomain.TaskStatusAwaitingUser) {
		states[m.current.CurrentNodeName] = "current"
	}
	if m.current != nil && m.current.Status == taskdomain.TaskStatusFailed {
		if m.current.CurrentIssue != nil && m.current.CurrentIssue.Kind == taskdomain.TaskIssueBlockedStep {
			states[m.current.CurrentNodeName] = "blocked"
		} else {
			states[m.current.CurrentNodeName] = "failed"
		}
	}

	parts := make([]string, 0, len(cfg.Topology.Nodes))
	for _, node := range cfg.Topology.Nodes {
		parts = append(parts, renderDAGNode(node.Name, states[node.Name]))
	}
	return fitLine(ansi.Truncate(strings.Join(parts, "   "), width, "…"), width)
}

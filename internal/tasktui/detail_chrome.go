package tasktui

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/LaLanMo/muxagent-cli/internal/taskdomain"
)

func (m Model) renderAppHeader(width int) string {
	brand := tuiTheme.brand.Render("muxagent")
	version := tuiTheme.version.Render(" " + normalizeVersionLabel(m.version))
	return fitLine(brand+version, width)
}

func (m Model) renderDetailHeader(width int) string {
	if m.current == nil {
		return fitLine(tuiTheme.taskLabel.Render("Task"), width)
	}
	title := tuiTheme.taskLabel.Render("Task: " + m.current.Task.Description)
	dag := m.renderDAG(width)
	divider := tuiTheme.divider.Render(strings.Repeat("─", max(8, width)))
	return lipgloss.JoinVertical(lipgloss.Left, title, dag, divider)
}

func (m Model) renderDAG(width int) string {
	cfg := m.currentConfig
	if cfg == nil {
		cfg = m.launchConfig
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

	parts := make([]string, 0, len(cfg.Topology.Nodes)*2)
	for i, node := range cfg.Topology.Nodes {
		parts = append(parts, renderDAGNode(node.Name, states[node.Name]))
		if i < len(cfg.Topology.Nodes)-1 {
			parts = append(parts, tuiTheme.lineMuted.Render(" → "))
		}
	}
	return ansi.Truncate(strings.Join(parts, ""), width, "…")
}

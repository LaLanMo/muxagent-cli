package tasktui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/LaLanMo/muxagent-cli/internal/taskdomain"
)

func renderDAGNode(name, state string) string {
	switch state {
	case "done":
		return renderNodeStatusLabel(tuiTheme.doneText, "✓", name, tuiTheme.body)
	case "failed":
		return renderNodeStatusLabel(tuiTheme.failedText, "×", name, tuiTheme.body)
	case "blocked":
		return renderNodeStatusLabel(tuiTheme.awaitingText, "!", name, tuiTheme.body)
	case "current":
		return renderNodeStatusLabel(tuiTheme.awaitingText, "●", name, tuiTheme.body)
	default:
		return tuiTheme.subtleText.Render("○ " + name)
	}
}

func renderNodeStatusLabel(iconStyle lipgloss.Style, icon, label string, labelStyle lipgloss.Style) string {
	return lipgloss.JoinHorizontal(
		lipgloss.Left,
		iconStyle.Render(icon),
		labelStyle.Render(" "+label),
	)
}

func renderTimelineHeadline(iconStyle lipgloss.Style, icon, label, status, meta string) string {
	parts := []string{
		iconStyle.Render(icon),
		tuiTheme.body.Render(" " + label),
	}
	if status != "" {
		parts = append(parts, tuiTheme.mutedText.Render("  "+status))
	}
	if meta != "" {
		parts = append(parts, tuiTheme.mutedText.Render("  "+meta))
	}
	return lipgloss.JoinHorizontal(lipgloss.Left, parts...)
}

func summarizeNodeRun(run taskdomain.NodeRunView, current *taskdomain.TaskView) string {
	if run.Status == taskdomain.NodeRunFailed && run.FailureReason != "" {
		return taskdomain.DisplayFailureReason(run.FailureReason)
	}
	if run.Result != nil {
		if approved, ok := run.Result["approved"].(bool); ok {
			return fmt.Sprintf("approved: %t", approved)
		}
		if passed, ok := run.Result["passed"].(bool); ok {
			return fmt.Sprintf("passed: %t", passed)
		}
		if feedback, ok := run.Result["feedback"].(string); ok && feedback != "" {
			return "feedback: " + feedback
		}
	}
	if len(run.ArtifactPaths) > 0 {
		paths := make([]string, 0, len(run.ArtifactPaths))
		for _, path := range run.ArtifactPaths {
			paths = append(paths, shortenPath(path, currentWorkDir(current)))
		}
		return strings.Join(paths, ", ")
	}
	return ""
}

func progressLines(progress []string, width int) []string {
	lines := []string{}
	lineWidth := max(8, width)
	for _, item := range progress {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		lines = append(lines, tuiTheme.streamJSON.Render(ansi.Truncate(item, lineWidth, "…")))
	}
	return lines
}

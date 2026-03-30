package tasktui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/LaLanMo/muxagent-cli/internal/taskdomain"
	"github.com/LaLanMo/muxagent-cli/internal/taskexecutor"
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

func progressEventLines(events []taskexecutor.StreamEvent, width int) []string {
	lines := make([]string, 0, len(events))
	lineWidth := max(8, width)
	for _, event := range events {
		if event.Kind == taskexecutor.StreamEventKindTool {
			if line := renderToolEventLine(event, lineWidth); line != "" {
				lines = append(lines, line)
			}
			continue
		}
		summary := strings.TrimSpace(event.Summary())
		if summary == "" {
			continue
		}
		style := tuiTheme.streamJSON
		if event.Kind == taskexecutor.StreamEventKindPlan || event.Kind == taskexecutor.StreamEventKindUsage {
			style = tuiTheme.streamThread
		}
		if event.Message != nil && event.Message.Type == taskexecutor.MessagePartTypeReasoning {
			style = tuiTheme.streamThread
		}
		lines = append(lines, style.Render(ansi.Truncate(summary, lineWidth, "…")))
	}
	return lines
}

func renderToolEventLine(event taskexecutor.StreamEvent, width int) string {
	if event.Tool == nil {
		return ""
	}
	label := strings.TrimSpace(event.Tool.DisplayLabel())
	subject := strings.TrimSpace(event.Tool.DisplaySubject())
	if label == "" && subject == "" {
		return ""
	}
	icon, iconStyle := toolStatusVisual(event.Tool.Status)
	parts := make([]string, 0, 3)
	if icon != "" {
		parts = append(parts, iconStyle.Render(icon))
	}
	if label != "" {
		prefix := label
		if len(parts) > 0 {
			prefix = " " + prefix
		}
		parts = append(parts, tuiTheme.body.Render(prefix))
	}
	if subject != "" {
		spacing := " "
		if label != "" {
			spacing = "  "
		}
		parts = append(parts, tuiTheme.mutedText.Render(spacing+subject))
	}
	return ansi.Truncate(lipgloss.JoinHorizontal(lipgloss.Left, parts...), width, "…")
}

func toolStatusVisual(status taskexecutor.ToolStatus) (string, lipgloss.Style) {
	switch status {
	case taskexecutor.ToolStatusCompleted:
		return "✓", tuiTheme.Status.Done
	case taskexecutor.ToolStatusFailed:
		return "×", tuiTheme.Status.Failed
	case taskexecutor.ToolStatusInProgress, taskexecutor.ToolStatusPending:
		return "●", tuiTheme.Status.Running
	default:
		return "•", tuiTheme.Text.Subtle
	}
}

package tasktui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/LaLanMo/muxagent-cli/internal/taskdomain"
	"github.com/LaLanMo/muxagent-cli/internal/taskruntime"
)

func (m Model) renderDetailTimeline(surface surfaceRect) string {
	width := surface.Width
	if m.current == nil {
		lines := []string{}
		if m.startupText != "" {
			lines = append(lines, tuiTheme.Status.Running.Render("● "+m.startupText))
		}
		if m.errorText != "" {
			lines = append(lines, tuiTheme.Status.Failed.Render("× "+m.errorText))
		}
		return strings.Join(lines, "\n")
	}
	if len(m.current.NodeRuns) == 0 {
		lines := []string{}
		if m.startupText != "" {
			lines = append(lines, tuiTheme.Status.Running.Render("● "+m.startupText))
		}
		if m.errorText != "" {
			lines = append(lines, tuiTheme.Status.Failed.Render("× "+m.errorText))
		}
		return strings.Join(lines, "\n")
	}

	lines := make([]string, 0, (len(m.current.NodeRuns)+len(m.current.BlockedSteps))*3)
	for _, entry := range detailTimelineEntries(*m.current) {
		switch {
		case entry.run != nil:
			lines = append(lines, m.renderNodeRunBlock(*entry.run, width)...)
		case entry.blocked != nil:
			lines = append(lines, m.renderBlockedStepBlock(*entry.blocked, width)...)
		}
		lines = append(lines, "")
	}
	if m.screen == ScreenComplete {
		lines = append(lines, tuiTheme.Status.Success.Render("✓ Task completed successfully"))
	}
	if m.screen == ScreenFailed && m.errorText != "" && (m.current.CurrentIssue == nil || m.current.CurrentIssue.Kind != taskdomain.TaskIssueBlockedStep) {
		lines = append(lines, tuiTheme.Status.Failed.Render("× "+m.errorText))
	}
	return strings.Join(trimTrailingBlank(lines), "\n")
}

func (m Model) renderNodeRunBlock(run taskdomain.NodeRunView, width int) []string {
	timeLabel := relativeTime(nodeRunTimestamp(run))
	nodeLabel := m.nodeRunLabel(run)
	switch run.Status {
	case taskdomain.NodeRunDone:
		lines := []string{renderTimelineHeadline(tuiTheme.Status.Done, "✓", nodeLabel, "", timeLabel)}
		if summary := summarizeNodeRun(run, m.current); summary != "" {
			lines = append(lines, tuiTheme.Text.Muted.Render("  ↳ "+summary))
		}
		if sessionID := m.nodeRunSessionID(run); sessionID != "" {
			lines = append(lines, tuiTheme.Text.Subtle.Render("  ↳ thread: "+sessionID))
		}
		return lines
	case taskdomain.NodeRunFailed:
		lines := []string{renderTimelineHeadline(tuiTheme.Status.Failed, "×", nodeLabel, "failed", timeLabel)}
		if summary := summarizeNodeRun(run, m.current); summary != "" {
			lines = append(lines, tuiTheme.Text.Muted.Render("  ↳ "+summary))
		}
		if sessionID := m.nodeRunSessionID(run); sessionID != "" {
			lines = append(lines, tuiTheme.Text.Subtle.Render("  ↳ thread: "+sessionID))
		}
		return lines
	case taskdomain.NodeRunAwaitingUser:
		waitLabel := "awaiting input"
		artifactPaths := append([]string(nil), run.ArtifactPaths...)
		if m.currentInput != nil && run.ID == m.currentInput.NodeRunID {
			if m.currentInput.Kind == taskruntime.InputKindHumanNode {
				waitLabel = "awaiting approval"
			}
			if len(m.currentInput.ArtifactPaths) > 0 {
				artifactPaths = append([]string(nil), m.currentInput.ArtifactPaths...)
			}
		}
		lines := []string{renderTimelineHeadline(tuiTheme.Status.Awaiting, "●", nodeLabel, waitLabel, "")}
		if len(artifactPaths) > 0 {
			lines = append(lines, tuiTheme.Text.Muted.Render("  Review artifacts in the pane →"))
		}
		if sessionID := m.nodeRunSessionID(run); sessionID != "" {
			lines = append(lines, tuiTheme.Text.Subtle.Render("  ↳ thread: "+sessionID))
		}
		return lines
	default:
		return []string{m.renderRunningStreamPanel(run, nodeLabel, width)}
	}
}

func (m Model) renderBlockedStepBlock(step taskdomain.BlockedStep, width int) []string {
	timeLabel := relativeTime(step.CreatedAt)
	return []string{
		renderTimelineHeadline(tuiTheme.Status.Awaiting, "!", blockedStepLabel(step), "blocked", timeLabel),
		tuiTheme.Text.Muted.Render("  ↳ " + step.Reason),
	}
}

func (m Model) renderRunningStreamPanel(run taskdomain.NodeRunView, nodeLabel string, width int) string {
	panelWidth := max(24, width)
	contentWidth := max(12, panelWidth-4)
	lines := []string{
		renderTimelineHeadline(tuiTheme.Status.Running, "●", nodeLabel, "running…", ""),
	}
	if sessionID := m.nodeRunSessionID(run); sessionID != "" {
		lines = append(lines, tuiTheme.Stream.Thread.Render(ansi.Wrap("thread: "+sessionID, contentWidth, "")))
	}
	if events := m.streamByRun[run.ID]; len(events) > 0 {
		for _, line := range progressEventLines(events, contentWidth) {
			lines = append(lines, line)
		}
	} else {
		for _, line := range progressLines(m.progressByRun[run.ID], contentWidth) {
			lines = append(lines, line)
		}
	}
	return tuiTheme.Stream.Panel.Width(panelWidth).Render(strings.Join(lines, "\n"))
}

func (m Model) nodeRunSessionID(run taskdomain.NodeRunView) string {
	if sessionID := m.sessionByRun[run.ID]; sessionID != "" {
		return sessionID
	}
	return run.SessionID
}

func (m Model) nodeRunLabel(run taskdomain.NodeRunView) string {
	if m.current == nil {
		return run.NodeName
	}
	total := 0
	ordinal := 0
	for _, candidate := range m.current.NodeRuns {
		if candidate.NodeName != run.NodeName {
			continue
		}
		total++
		if candidate.ID == run.ID {
			ordinal = total
		}
	}
	if total <= 1 || ordinal == 0 {
		return run.NodeName
	}
	return fmt.Sprintf("%s (#%d)", run.NodeName, ordinal)
}

func (m Model) currentFailureMessage() string {
	if m.current == nil {
		return ""
	}
	if m.current.CurrentIssue != nil {
		return m.current.CurrentIssue.Reason
	}
	return ""
}

package tasktui

import (
	"fmt"
	"sort"
	"strings"

	"charm.land/lipgloss/v2"
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
			lines = append(lines, m.renderNodeRunBlock(*entry.run)...)
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

func (m Model) renderNodeRunBlock(run taskdomain.NodeRunView) []string {
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
		showArtifactHint := len(run.ArtifactPaths) > 0
		if m.currentInput != nil && run.ID == m.currentInput.NodeRunID {
			if m.currentInput.Kind == taskruntime.InputKindHumanNode {
				waitLabel = "awaiting approval"
			}
			showArtifactHint = artifactPaneHasVisibleArtifacts(m.current, m.currentInput)
		}
		lines := []string{renderTimelineHeadline(tuiTheme.Status.Awaiting, "●", nodeLabel, waitLabel, "")}
		if showArtifactHint {
			lines = append(lines, tuiTheme.Text.Muted.Render("  Review artifacts in the pane →"))
		}
		if sessionID := m.nodeRunSessionID(run); sessionID != "" {
			lines = append(lines, tuiTheme.Text.Subtle.Render("  ↳ thread: "+sessionID))
		}
		return lines
	default:
		lines := []string{renderTimelineHeadline(tuiTheme.Status.Running, "●", nodeLabel, "running…", "")}
		if active := m.activeRunningNodeRun(); active != nil && active.ID == run.ID && m.timelineSplitEnabled() {
			lines = append(lines, tuiTheme.Text.Muted.Render("  ↳ live output →"))
		}
		return lines
	}
}

func (m Model) renderBlockedStepBlock(step taskdomain.BlockedStep, width int) []string {
	timeLabel := relativeTime(step.CreatedAt)
	return []string{
		renderTimelineHeadline(tuiTheme.Status.Awaiting, "!", blockedStepLabel(step), "blocked", timeLabel),
		tuiTheme.Text.Muted.Render("  ↳ " + step.Reason),
	}
}

func (m Model) timelineSplitEnabled() bool {
	return m.activeDetailTab == DetailTabTimeline && m.activeRunningNodeRun() != nil
}

func (m Model) activeRunningNodeRun() *taskdomain.NodeRunView {
	if m.current == nil {
		return nil
	}
	winner := -1
	for i := range m.current.NodeRuns {
		run := &m.current.NodeRuns[i]
		if run.Status != taskdomain.NodeRunRunning {
			continue
		}
		if winner < 0 {
			winner = i
			continue
		}
		candidate := &m.current.NodeRuns[winner]
		switch {
		case run.StartedAt.After(candidate.StartedAt):
			winner = i
		case run.StartedAt.Equal(candidate.StartedAt) && run.ID > candidate.ID:
			winner = i
		}
	}
	if winner < 0 {
		return nil
	}
	return &m.current.NodeRuns[winner]
}

func (m Model) renderLiveOutputContent(surface surfaceRect) string {
	run := m.activeRunningNodeRun()
	if run == nil {
		return ""
	}
	contentWidth := max(12, surface.Width)
	lines := []string{}
	if events := m.streamByRun[run.ID]; len(events) > 0 {
		lines = append(lines, progressEventLines(events, contentWidth)...)
	} else if progress := m.progressByRun[run.ID]; len(progress) > 0 {
		lines = append(lines, progressLines(progress, contentWidth)...)
	} else {
		lines = append(lines, tuiTheme.Artifact.Empty.Render("Waiting for live output…"))
	}
	return strings.Join(lines, "\n")
}

func (m Model) renderLiveOutputPane(surface surfaceRect) string {
	if surface.Width <= 0 || surface.Height <= 0 {
		return ""
	}
	run := m.activeRunningNodeRun()
	if run == nil {
		return lipgloss.Place(surface.Width, surface.Height, lipgloss.Left, lipgloss.Top, "")
	}
	title := m.renderArtifactPaneTitle("Output · "+m.nodeRunLabel(*run), false)
	threadLine := tuiTheme.Stream.Thread.Render(ansi.Wrap(m.liveOutputThreadLabel(*run), max(12, surface.Width-2), ""))
	bodyHeight := max(1, surface.Height-2)
	body := lipgloss.Place(max(10, surface.Width-2), bodyHeight, lipgloss.Left, lipgloss.Top, m.liveOutput.View())
	return lipgloss.NewStyle().
		Width(surface.Width).
		Height(surface.Height).
		PaddingLeft(1).
		Render(lipgloss.JoinVertical(lipgloss.Left, title, threadLine, body))
}

func (m Model) liveOutputThreadLabel(run taskdomain.NodeRunView) string {
	if sessionID := m.nodeRunSessionID(run); sessionID != "" {
		return "thread: " + sessionID
	}
	return "thread: pending"
}

func (m Model) liveOutputSnapshot() []string {
	run := m.activeRunningNodeRun()
	if run == nil {
		return nil
	}
	lines := []string{m.liveOutputThreadLabel(*run)}
	if events := m.streamByRun[run.ID]; len(events) > 0 {
		for _, line := range progressEventLines(events, 400) {
			lines = append(lines, ansi.Strip(line))
		}
	} else {
		lines = append(lines, m.progressByRun[run.ID]...)
	}
	return lines
}

func (m Model) sortedRunningNodeRuns() []taskdomain.NodeRunView {
	if m.current == nil {
		return nil
	}
	runs := make([]taskdomain.NodeRunView, 0, len(m.current.NodeRuns))
	for _, run := range m.current.NodeRuns {
		if run.Status == taskdomain.NodeRunRunning {
			runs = append(runs, run)
		}
	}
	sort.Slice(runs, func(i, j int) bool {
		if runs[i].StartedAt.Equal(runs[j].StartedAt) {
			return runs[i].ID > runs[j].ID
		}
		return runs[i].StartedAt.After(runs[j].StartedAt)
	})
	return runs
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

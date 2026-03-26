package tasktui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/LaLanMo/muxagent-cli/internal/taskdomain"
)

const quitHint = "Ctrl+C quit"

func joinHintParts(parts ...string) string {
	filtered := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		filtered = append(filtered, part)
	}
	return strings.Join(filtered, "  ")
}

func renderFooterHintText(text string) string {
	return tuiTheme.Footer.Hint.Render(text)
}

func renderFooterHintBar(width int, left string) string {
	return joinHorizontal(renderFooterHintText(left), renderFooterHintText(quitHint), width)
}

func renderFooterWithStats(width int, left, right, hint string) string {
	lines := make([]string, 0, 2)
	if strings.TrimSpace(left) != "" || strings.TrimSpace(right) != "" {
		lines = append(lines, joinHorizontal(tuiTheme.Footer.Strong.Render(left), tuiTheme.Footer.Strong.Render(right), width))
	}
	lines = append(lines, renderFooterHintBar(width, hint))
	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

func renderPanelFooter(width int, panel, hint string) string {
	return lipgloss.JoinVertical(lipgloss.Left, panel, renderFooterHintBar(width, hint))
}

func (m Model) taskListHint() string {
	return joinHintParts("↑↓ navigate", "Enter select")
}

func (m Model) newTaskModalHint() string {
	return joinHintParts("Enter newline", "Tab start", "Esc cancel")
}

func (m Model) renderTaskListFooter(surface surfaceRect) string {
	return renderFooterHintBar(surface.Width, m.taskListHint())
}

func (m Model) detailFooterReservedHeight() int {
	switch m.screen {
	case ScreenRunning, ScreenComplete:
		return 2
	default:
		return 1
	}
}

func (m Model) renderDetailFooter(surface surfaceRect) string {
	return m.renderDetailFooterForLayout(surface, m.currentArtifactLayoutMode())
}

func (m Model) renderDetailFooterForLayout(surface surfaceRect, mode artifactLayoutMode) string {
	if m.current == nil {
		return m.renderStatsFooter(surface, "", "", "Esc back")
	}
	switch m.screen {
	case ScreenApproval:
		return m.renderApprovalFooterForLayout(surface, mode)
	case ScreenClarification:
		return m.renderClarificationFooterForLayout(surface, mode)
	case ScreenComplete:
		return m.renderStatsFooter(surface, taskSummaryLeft(m.current, m.currentConfig), taskSummaryRight(m.current), m.detailHintForLayout("Esc back", mode))
	case ScreenFailed:
		return m.renderFailureFooterForLayout(surface, mode)
	default:
		left := fmt.Sprintf("%d runs · %d artifacts", len(m.current.NodeRuns), len(m.current.ArtifactPaths))
		right := "elapsed: " + taskElapsed(m.current)
		return m.renderStatsFooter(surface, left, right, m.detailHintForLayout("Esc back", mode))
	}
}

func (m Model) renderStatsFooter(surface surfaceRect, left, right, hint string) string {
	return renderFooterWithStats(surface.Width, left, right, hint)
}

func (m Model) renderFailureFooter(surface surfaceRect) string {
	return m.renderFailureFooterForLayout(surface, m.currentArtifactLayoutMode())
}

func (m Model) renderFailureFooterForLayout(surface surfaceRect, mode artifactLayoutMode) string {
	return renderFooterHintBar(surface.Width, m.failureHintForLayout(mode))
}

func (m Model) renderFailurePanel(surface panelSurface) string {
	width := surface.Rect.Width
	maxHeight := max(1, surface.MaxHeight)
	recovery := m.currentRecoveryTarget()
	title := "Task failed"
	panelBase := tuiTheme.Panel.Danger
	body := firstNonEmpty(m.errorText, m.currentFailureMessage(), "Review the failed node output and try again.")
	if recovery != nil && recovery.Kind == taskdomain.RecoveryTargetBlockedStep {
		title = "Task blocked"
		panelBase = tuiTheme.Panel.Warning
		body = fmt.Sprintf("%s is blocked before execution.\n\n%s", recovery.NodeName, recovery.Reason)
	} else if recovery != nil && !recovery.RetryAllowed {
		body += fmt.Sprintf("\n\nRetry limit reached for %s (%d/%d).", recovery.NodeName, recovery.NextIteration-1, recovery.MaxIterations)
	}
	panelStyle := panelBase.Width(width).MaxHeight(maxHeight)
	innerWidth := max(1, width-panelBase.GetHorizontalFrameSize())
	innerHeight := max(1, maxHeight-panelStyle.GetVerticalFrameSize())
	content := []string{tuiTheme.Panel.Title.Render(title), ""}
	actionBlock := []string{}
	if actions := m.availableFailureActions(); len(actions) > 0 {
		actionBlock = append(actionBlock, "", tuiTheme.Text.Muted.Render("Select an action:"))
		items := make([]choiceItem, 0, len(actions))
		focusedIndex := 0
		for i, action := range actions {
			if action == m.failure.action {
				focusedIndex = i
			}
			items = append(items, choiceItem{
				Label:     failureActionLabel(action),
				Indicator: choiceIndicatorAction,
				Enabled:   true,
			})
		}
		actionBlock = append(actionBlock, renderChoiceItems(innerWidth, focusedIndex, m.focusRegion == FocusRegionActionPanel, items)...)
	}
	fixedHeight := lipgloss.Height(strings.Join(append([]string{}, content...), "\n")) + lipgloss.Height(strings.Join(actionBlock, "\n"))
	bodyBudget := max(1, innerHeight-fixedHeight)
	bodyLines := wrapPanelBody(body, innerWidth)
	bodyLines = truncateWrappedPanelLines(bodyLines, bodyBudget, innerWidth)
	content = append(content, tuiTheme.Panel.Body.Render(strings.Join(bodyLines, "\n")))
	content = append(content, actionBlock...)
	panel := panelStyle.Render(strings.Join(content, "\n"))
	return panel
}

func (m Model) failureHint() string {
	return m.failureHintForLayout(m.currentArtifactLayoutMode())
}

func (m Model) failureHintForLayout(mode artifactLayoutMode) string {
	actions := m.availableFailureActions()
	if m.focusRegion == FocusRegionActionPanel && len(actions) > 0 {
		return joinHintParts("↑↓ actions", "Enter confirm", "Tab detail", "Esc back")
	}
	return m.detailHintForLayout("Esc back", mode)
}

func (m Model) nextFocusHint() string {
	return m.nextFocusHintForLayout(m.currentArtifactLayoutMode())
}

func (m Model) nextFocusHintForLayout(mode artifactLayoutMode) string {
	regions := m.availableFocusRegionsForLayout(mode)
	if len(regions) <= 1 {
		return ""
	}
	index := focusRegionIndex(regions, m.focusRegion)
	next := regions[0]
	if index >= 0 {
		next = regions[(index+1)%len(regions)]
	}
	switch next {
	case FocusRegionChoices:
		return "Tab type"
	case FocusRegionActionPanel:
		return "Tab actions"
	case FocusRegionComposer:
		return "Tab composer"
	case FocusRegionDetail:
		return "Tab detail"
	case FocusRegionArtifactLauncher:
		return "Tab artifacts"
	case FocusRegionArtifactFiles:
		return "Tab artifacts"
	case FocusRegionArtifactPreview:
		return "Tab next pane"
	default:
		return ""
	}
}

func (m Model) detailHint(base string) string {
	return m.detailHintForLayout(base, m.currentArtifactLayoutMode())
}

func (m Model) detailHintForLayout(base string, mode artifactLayoutMode) string {
	switch m.focusRegion {
	case FocusRegionDetail:
		parts := []string{"↑↓ scroll"}
		if base != "" {
			parts = append(parts, base)
		}
		if next := m.nextFocusHintForLayout(mode); next != "" {
			parts = append(parts, next)
		}
		return joinHintParts(parts...)
	case FocusRegionArtifactLauncher:
		parts := []string{"Enter open", "Esc detail"}
		if next := m.nextFocusHintForLayout(mode); next != "" {
			parts = append(parts, next)
		}
		return joinHintParts(parts...)
	case FocusRegionArtifactFiles:
		return joinHintParts("↑↓ files", "Enter preview", "Esc detail")
	case FocusRegionArtifactPreview:
		return joinHintParts("↑↓ scroll", "Esc detail")
	case FocusRegionActionPanel:
		parts := []string{}
		if base != "" {
			parts = append(parts, base)
		}
		if next := m.nextFocusHintForLayout(mode); next != "" {
			parts = append(parts, next)
		}
		return joinHintParts(parts...)
	case FocusRegionChoices:
		parts := []string{}
		if base != "" {
			parts = append(parts, base)
		}
		if next := m.nextFocusHintForLayout(mode); next != "" {
			parts = append(parts, next)
		}
		return joinHintParts(parts...)
	case FocusRegionComposer:
		if m.screen == ScreenNewTask {
			return joinHintParts("Enter newline", "Tab start", "Esc cancel")
		}
		parts := []string{"Enter newline", "Esc choices"}
		if next := m.nextFocusHintForLayout(mode); next != "" {
			parts = append(parts, next)
		}
		return joinHintParts(parts...)
	default:
		return joinHintParts(base)
	}
}

func (m Model) renderDetailPanel(surface panelSurface) string {
	return m.buildDetailPanelForSurface(surface, m.detailEditorSurfaceSpec(surface)).View
}

func wrapPanelBody(text string, width int) []string {
	width = max(1, width)
	rawLines := strings.Split(text, "\n")
	lines := make([]string, 0, len(rawLines))
	for _, line := range rawLines {
		if strings.TrimSpace(line) == "" {
			lines = append(lines, "")
			continue
		}
		lines = append(lines, strings.Split(ansi.Wrap(line, width, ""), "\n")...)
	}
	return trimTrailingBlank(lines)
}

func truncateWrappedPanelLines(lines []string, maxLines, width int) []string {
	if len(lines) <= maxLines {
		return lines
	}
	if maxLines <= 0 {
		return nil
	}
	lines = append([]string(nil), lines[:maxLines]...)
	lines[maxLines-1] = ansi.Truncate(lines[maxLines-1], max(1, width), "…")
	return lines
}

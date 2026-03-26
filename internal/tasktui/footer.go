package tasktui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"

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

func (m Model) renderDetailFooter(surface surfaceRect) string {
	if m.current == nil {
		return m.renderStatsFooter(surface, "", "", "Esc back")
	}
	switch m.screen {
	case ScreenApproval:
		return m.renderApprovalFooter(surface)
	case ScreenClarification:
		return m.renderClarificationFooter(surface)
	case ScreenComplete:
		return m.renderStatsFooter(surface, taskSummaryLeft(m.current, m.currentConfig), taskSummaryRight(m.current), m.detailHint("Esc back"))
	case ScreenFailed:
		return m.renderFailureFooter(surface)
	default:
		left := fmt.Sprintf("%d runs · %d artifacts", len(m.current.NodeRuns), len(m.current.ArtifactPaths))
		right := "elapsed: " + taskElapsed(m.current)
		return m.renderStatsFooter(surface, left, right, m.detailHint("Esc back"))
	}
}

func (m Model) renderStatsFooter(surface surfaceRect, left, right, hint string) string {
	return renderFooterWithStats(surface.Width, left, right, hint)
}

func (m Model) renderFailureFooter(surface surfaceRect) string {
	return renderFooterHintBar(surface.Width, m.failureHint())
}

func (m Model) renderFailurePanel(surface panelSurface) string {
	width := surface.Rect.Width
	recovery := m.currentRecoveryTarget()
	title := "Task failed"
	panelStyle := tuiTheme.Panel.Danger
	body := firstNonEmpty(m.errorText, m.currentFailureMessage(), "Review the failed node output and try again.")
	if recovery != nil && recovery.Kind == taskdomain.RecoveryTargetBlockedStep {
		title = "Task blocked"
		panelStyle = tuiTheme.Panel.Warning
		body = fmt.Sprintf("%s is blocked before execution.\n\n%s", recovery.NodeName, recovery.Reason)
	} else if recovery != nil && !recovery.RetryAllowed {
		body += fmt.Sprintf("\n\nRetry limit reached for %s (%d/%d).", recovery.NodeName, recovery.NextIteration-1, recovery.MaxIterations)
	}
	content := []string{
		tuiTheme.Panel.Title.Render(title),
		"",
		tuiTheme.Panel.Body.Render(body),
	}
	if actions := m.availableFailureActions(); len(actions) > 0 {
		content = append(content, "", tuiTheme.Text.Muted.Render("Select an action:"))
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
		content = append(content, renderChoiceItems(width, focusedIndex, m.focusRegion == FocusRegionActionPanel, items)...)
	}
	panel := panelStyle.Width(width).Render(strings.Join(content, "\n"))
	return panel
}

func (m Model) failureHint() string {
	actions := m.availableFailureActions()
	if m.focusRegion == FocusRegionActionPanel && len(actions) > 0 {
		return joinHintParts("↑↓ actions", "Enter confirm", "Tab detail", "Esc back")
	}
	return m.detailHint("Esc back")
}

func (m Model) nextFocusHint() string {
	regions := m.availableFocusRegions()
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
	case FocusRegionArtifactFiles, FocusRegionArtifactPreview:
		return "Tab next pane"
	default:
		return ""
	}
}

func (m Model) detailHint(base string) string {
	switch m.focusRegion {
	case FocusRegionDetail:
		parts := []string{"↑↓ scroll"}
		if base != "" {
			parts = append(parts, base)
		}
		if next := m.nextFocusHint(); next != "" {
			parts = append(parts, next)
		}
		return joinHintParts(parts...)
	case FocusRegionArtifactLauncher:
		parts := []string{"Enter open", "Esc detail"}
		if next := m.nextFocusHint(); next != "" {
			parts = append(parts, next)
		}
		return joinHintParts(parts...)
	case FocusRegionArtifactFiles:
		parts := []string{"↑↓ files", "Enter preview", "Esc detail"}
		if next := m.nextFocusHint(); next != "" {
			parts = append(parts, next)
		}
		return joinHintParts(parts...)
	case FocusRegionArtifactPreview:
		parts := []string{"↑↓ scroll", "Esc detail"}
		if next := m.nextFocusHint(); next != "" {
			parts = append(parts, next)
		}
		return joinHintParts(parts...)
	case FocusRegionActionPanel:
		parts := []string{}
		if base != "" {
			parts = append(parts, base)
		}
		if next := m.nextFocusHint(); next != "" {
			parts = append(parts, next)
		}
		return joinHintParts(parts...)
	case FocusRegionChoices:
		parts := []string{}
		if base != "" {
			parts = append(parts, base)
		}
		if next := m.nextFocusHint(); next != "" {
			parts = append(parts, next)
		}
		return joinHintParts(parts...)
	case FocusRegionComposer:
		if m.screen == ScreenNewTask {
			return joinHintParts("Enter newline", "Tab start", "Esc cancel")
		}
		parts := []string{"Enter newline", "Esc choices"}
		if next := m.nextFocusHint(); next != "" {
			parts = append(parts, next)
		}
		return joinHintParts(parts...)
	default:
		return joinHintParts(base)
	}
}

func (m Model) renderDetailPanel(surface panelSurface) string {
	switch m.screen {
	case ScreenApproval:
		return m.renderApprovalPanel(surface)
	case ScreenClarification:
		return m.renderClarificationPanel(surface)
	case ScreenFailed:
		return m.renderFailurePanel(surface)
	default:
		return ""
	}
}

package tasktui

import (
	"fmt"
	"path/filepath"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/LaLanMo/muxagent-cli/internal/taskconfig"
)

func (m Model) renderNewTaskModal(layout newTaskScreenLayout) string {
	modalStyle := tuiTheme.modal.Width(layout.modalWidth)
	innerWidth := layout.modalInnerWidth
	return modalStyle.Render(m.renderNewTaskPanelBody(innerWidth))
}

func (m Model) renderNewTaskPanelBody(innerWidth int) string {
	inputWidth := max(18, innerWidth-2)
	input := tuiTheme.Form.Input.Width(inputWidth).Render(m.newTaskInput.View())
	actionLabel := "Start task"
	if strings.TrimSpace(m.newTaskInput.Value()) == "" {
		actionLabel = "Start task (add a description first)"
	}
	action := renderChoiceLine(m.focusRegion == FocusRegionActionPanel, actionLabel)
	return lipgloss.JoinVertical(
		lipgloss.Left,
		tuiTheme.modalTitle.Render("New Task"),
		tuiTheme.modalSubtitle.Render(m.newTaskSubtitle()),
		"",
		input,
		"",
		action,
		"",
		renderFooterHintText(m.newTaskModalHint()),
	)
}

func (m Model) newTaskSubtitle() string {
	cfg := m.launchConfig
	if cfg == nil {
		cfg, _ = taskconfig.LoadDefault()
	}
	subtitle := "default config"
	if cfg != nil {
		nodeNames := make([]string, 0, len(cfg.Topology.Nodes))
		for _, node := range cfg.Topology.Nodes {
			nodeNames = append(nodeNames, node.Name)
		}
		subtitle += " · runtime " + string(m.effectiveLaunchRuntime()) + " · " + strings.Join(nodeNames, ", ")
	}
	if m.configOverride != "" {
		subtitle = "custom config · " + filepath.Base(m.configOverride) + " · runtime " + string(m.effectiveLaunchRuntime())
	}
	return subtitle
}

func (m Model) renderApprovalPanel(surface panelSurface) string {
	width := surface.Rect.Width
	options := renderChoiceItems(width, m.approval.choice, m.focusRegion == FocusRegionActionPanel, []choiceItem{
		{Label: "Yes, approve", Indicator: choiceIndicatorPlain},
		{Label: "No, reject with feedback", Indicator: choiceIndicatorPlain},
	})
	content := []string{
		tuiTheme.Panel.Title.Render("Approve this plan?"),
		"",
	}
	content = append(content, options...)
	if m.approval.choice == 1 {
		content = append(content, "", tuiTheme.Form.Input.Render(m.detailInput.View()))
	}
	if m.errorText != "" {
		content = append(content, "", tuiTheme.Status.Failed.Render("× "+m.errorText))
	}
	return tuiTheme.Panel.Warning.Width(width).Render(strings.Join(content, "\n"))
}

func (m Model) renderClarificationPanel(surface panelSurface) string {
	if m.currentInput == nil || len(m.currentInput.Questions) == 0 {
		return ""
	}
	width := surface.Rect.Width
	question := m.currentInput.Questions[m.clarification.question]
	panelStyle := tuiTheme.Panel.Warning.Width(width)
	innerWidth := max(1, width-panelStyle.GetHorizontalFrameSize())
	title := tuiTheme.Panel.Title.Width(innerWidth).Render(fmt.Sprintf("Question %d/%d", m.clarification.question+1, len(m.currentInput.Questions)))
	body := tuiTheme.Panel.Body.Width(innerWidth).Render(question.Question)
	why := tuiTheme.Text.Muted.Width(innerWidth).Render(question.WhyItMatters)
	header := []string{title, body, why, ""}

	items := make([]choiceItem, 0, len(question.Options))
	for _, option := range question.Options {
		item := choiceItem{
			Label: option.Label + " · " + option.Description,
		}
		if question.MultiSelect {
			item.Indicator = choiceIndicatorChecklist
			item.Selected = clarificationAnswerContains(m.clarification.answers, m.clarification.question, option.Label)
		} else {
			item.Indicator = choiceIndicatorRadio
			item.Selected = clarificationSelectedOption(question, clarificationAnswerAt(m.clarification.answers, m.clarification.question)) == option.Label
		}
		items = append(items, item)
	}
	optionLines := renderChoiceItems(innerWidth, m.clarification.option, m.focusRegion == FocusRegionChoices, items)
	actionLabel := "Continue"
	if m.clarification.question == len(m.currentInput.Questions)-1 {
		actionLabel = "Submit answers"
	}
	otherBlock := []string{
		"",
		tuiTheme.Text.Muted.Render("Other"),
		tuiTheme.Form.Input.Width(innerWidth).Render(m.detailInput.View()),
		"",
		renderActionLine(m.focusRegion == FocusRegionActionPanel, m.canAdvanceClarification(question), actionLabel),
	}
	extra := make([]string, 0, len(otherBlock)+2)
	extra = append(extra, otherBlock...)
	if m.errorText != "" {
		extra = append(extra, "", tuiTheme.Status.Failed.Render("× "+m.errorText))
	}
	headerHeight := lipgloss.Height(strings.Join(header, "\n"))
	extraHeight := lipgloss.Height(strings.Join(extra, "\n"))
	optionHeights := make([]int, 0, len(optionLines))
	for _, line := range optionLines {
		optionHeights = append(optionHeights, lipgloss.Height(line))
	}
	visibleOptionHeight := max(1, surface.MaxHeight-headerHeight-extraHeight)
	start, end := clarificationVisibleWindowByHeight(optionHeights, m.clarification.option, visibleOptionHeight)
	content := append([]string{}, header...)
	content = append(content, optionLines[start:end]...)
	content = append(content, extra...)
	return panelStyle.Render(strings.Join(content, "\n"))
}

func clarificationVisibleWindowByHeight(heights []int, selected, budget int) (int, int) {
	if len(heights) == 0 {
		return 0, 0
	}
	if budget <= 0 {
		return clamp(selected, 0, len(heights)-1), clamp(selected+1, 1, len(heights))
	}
	total := 0
	for _, h := range heights {
		total += h
	}
	if total <= budget {
		return 0, len(heights)
	}
	selected = clamp(selected, 0, len(heights)-1)
	bestStart, bestEnd, bestUsed := selected, selected+1, heights[selected]
	for start := 0; start < len(heights); start++ {
		used := 0
		end := start
		for end < len(heights) && used+heights[end] <= budget {
			used += heights[end]
			end++
		}
		if end == start {
			end = start + 1
			used = heights[start]
		}
		if selected < start || selected >= end {
			continue
		}
		if used > bestUsed {
			bestStart, bestEnd, bestUsed = start, end, used
		}
	}
	return bestStart, bestEnd
}

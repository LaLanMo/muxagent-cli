package tasktui

import (
	"fmt"
	"path/filepath"
	"strings"

	"charm.land/lipgloss/v2"
)

type builtEditorField struct {
	View           string
	ContentOffsetX int
	ContentOffsetY int
}

type builtPanel struct {
	View          string
	EditorOffsetX int
	EditorOffsetY int
	HasEditor     bool
}

func renderOpaquePanelSurface(width int, content string) string {
	return lipgloss.NewStyle().
		Width(width).
		Background(tuiTheme.panelBg).
		Render(content)
}

func renderOpaqueMeasuredPanelText(width int, style lipgloss.Style, text string) string {
	measureWidth := detailBodyMeasureWidth(width)
	wrapped := strings.Join(wrapPanelBody(text, measureWidth), "\n")
	return renderOpaquePanelSurface(width, style.Render(wrapped))
}

func (m Model) buildEditorField(spec editorSurfaceSpec) builtEditorField {
	width := max(18, spec.FieldWidth)
	focused := m.focusRegion == FocusRegionComposer && m.activeEditorSlot() != ""
	labelStyle := tuiTheme.Form.InputLabel
	frameStyle := tuiTheme.Form.InputBlurred
	if focused {
		labelStyle = tuiTheme.Form.InputLabelHot
		frameStyle = tuiTheme.Form.InputFocused
	}
	labelView := renderOpaquePanelSurface(width, labelStyle.Render(spec.Label))
	frameView := frameStyle.Width(width).Render(m.editor.View())
	lines := []string{labelView, frameView}
	if strings.TrimSpace(spec.Caption) != "" {
		lines = append(lines, renderOpaquePanelSurface(width, tuiTheme.Form.InputCaption.Render(spec.Caption)))
	}
	return builtEditorField{
		View:           lipgloss.JoinVertical(lipgloss.Left, lines...),
		ContentOffsetX: frameStyle.GetBorderLeftSize() + frameStyle.GetPaddingLeft(),
		ContentOffsetY: lipgloss.Height(labelView) + frameStyle.GetBorderTopSize() + frameStyle.GetPaddingTop(),
	}
}

func (m Model) renderEditorField(width int, label, caption string) string {
	return m.buildEditorField(editorSurfaceSpec{
		editorBindingSpec: editorBindingSpec{
			Visible: true,
			Label:   label,
			Caption: caption,
		},
		FieldWidth: width,
	}).View
}

func (m Model) renderNewTaskModal(layout newTaskScreenLayout) string {
	modalStyle := tuiTheme.modal.Width(layout.modalWidth)
	return modalStyle.Render(m.buildNewTaskPanel(layout.modalInnerWidth).View)
}

func (m Model) renderNewTaskPanelBody(innerWidth int) string {
	return m.buildNewTaskPanel(innerWidth).View
}

func (m Model) buildNewTaskPanel(innerWidth int) builtPanel {
	spec := m.currentEditorSurfaceSpec()
	if !spec.Visible {
		spec = editorSurfaceSpec{
			editorBindingSpec: editorBindingSpec{
				Visible: true,
				Label:   "Task description",
			},
			FieldWidth: max(18, innerWidth),
		}
	}
	input := m.buildEditorField(spec)
	title := renderOpaquePanelSurface(innerWidth, tuiTheme.modalTitle.Render("New Task"))
	subtitle := renderOpaquePanelSurface(innerWidth, tuiTheme.modalSubtitle.Render(m.newTaskSubtitle()))
	blank := renderOpaquePanelSurface(innerWidth, "")
	lines := []string{
		title,
		subtitle,
		blank,
		input.View,
	}
	if strings.TrimSpace(m.errorText) != "" {
		lines = append(lines, blank, renderOpaquePanelSurface(innerWidth, tuiTheme.Status.Failed.Render("× "+m.errorText)))
	}
	view := lipgloss.JoinVertical(lipgloss.Left, lines...)
	prefixHeight := lipgloss.Height(strings.Join([]string{title, subtitle, ""}, "\n"))
	return builtPanel{
		View:          view,
		EditorOffsetX: tuiTheme.modal.GetPaddingLeft() + input.ContentOffsetX,
		EditorOffsetY: tuiTheme.modal.GetPaddingTop() + prefixHeight + input.ContentOffsetY,
		HasEditor:     true,
	}
}

func (m Model) newTaskSubtitle() string {
	entry := m.selectedTaskConfigEntry()
	cfg := entry.Config
	subtitle := "config " + entry.Alias
	if runtime := strings.TrimSpace(string(m.effectiveLaunchRuntime())); runtime != "" {
		subtitle += " · runtime " + runtime
	}
	if m.worktreeLaunchAvailable {
		subtitle += " · worktree " + m.newTaskWorktreeStatusLabel()
	}
	if cfg != nil && len(cfg.Topology.Nodes) > 0 {
		nodeNames := make([]string, 0, len(cfg.Topology.Nodes))
		for _, node := range cfg.Topology.Nodes {
			nodeNames = append(nodeNames, node.Name)
		}
		subtitle += " · " + strings.Join(nodeNames, ", ")
	} else if entry.Path != "" {
		subtitle += " · source " + filepath.Base(entry.Path)
	}
	return subtitle
}

func (m Model) renderApprovalPanel(surface panelSurface) string {
	return m.buildApprovalPanel(surface, m.detailEditorSurfaceSpec(surface)).View
}

func (m Model) buildDetailPanelForSurface(surface panelSurface, editorSpec editorSurfaceSpec) builtPanel {
	switch m.screen {
	case ScreenApproval:
		return m.buildApprovalPanel(surface, editorSpec)
	case ScreenClarification:
		return m.buildClarificationPanel(surface, editorSpec)
	case ScreenFailed:
		return builtPanel{View: m.renderFailurePanel(surface)}
	default:
		return builtPanel{}
	}
}

func (m Model) buildApprovalPanel(surface panelSurface, editorSpec editorSurfaceSpec) builtPanel {
	width := surface.Rect.Width
	panelStyle := tuiTheme.Panel.Warning.Width(width).MaxHeight(max(1, surface.MaxHeight))
	innerWidth := max(1, width-panelStyle.GetHorizontalFrameSize())
	title := renderOpaquePanelSurface(innerWidth, tuiTheme.Panel.Title.Render("Approve this plan?"))
	options := renderChoiceItems(innerWidth, m.approval.choice, m.focusRegion == FocusRegionActionPanel, []choiceItem{
		{Label: "Yes, approve", Indicator: choiceIndicatorPlain},
		{Label: "No, reject with feedback", Indicator: choiceIndicatorPlain},
	})
	content := []string{
		title,
		"",
	}
	content = append(content, options...)
	build := builtPanel{}
	if m.approval.choice == 1 {
		spec := editorSpec
		if !spec.Visible {
			spec = editorSurfaceSpec{
				editorBindingSpec: editorBindingSpec{
					Visible: true,
					Label:   "Feedback",
				},
				FieldWidth: detailFormMeasureWidth(innerWidth),
			}
		}
		input := m.buildEditorField(spec)
		prefixHeight := lipgloss.Height(strings.Join(append(append([]string{}, content...), ""), "\n"))
		content = append(content, "", input.View)
		build.EditorOffsetX = panelFrameLeft(tuiTheme.Panel.Warning) + input.ContentOffsetX
		build.EditorOffsetY = panelFrameTop(tuiTheme.Panel.Warning) + prefixHeight + input.ContentOffsetY
		build.HasEditor = true
	}
	if m.errorText != "" {
		candidate := append(append([]string{}, content...), "", renderOpaquePanelSurface(innerWidth, tuiTheme.Status.Failed.Render("× "+m.errorText)))
		if lipgloss.Height(strings.Join(candidate, "\n")) <= surface.MaxHeight {
			content = candidate
		}
	}
	build.View = panelStyle.Render(strings.Join(content, "\n"))
	return build
}

func (m Model) renderClarificationPanel(surface panelSurface) string {
	return m.buildClarificationPanel(surface, m.detailEditorSurfaceSpec(surface)).View
}

func (m Model) buildClarificationPanel(surface panelSurface, editorSpec editorSurfaceSpec) builtPanel {
	if m.currentInput == nil || len(m.currentInput.Questions) == 0 {
		return builtPanel{}
	}
	width := surface.Rect.Width
	question := m.currentInput.Questions[m.clarification.question]
	panelStyle := tuiTheme.Panel.Warning.Width(width).MaxHeight(max(1, surface.MaxHeight))
	innerWidth := max(1, width-panelStyle.GetHorizontalFrameSize())
	title := renderOpaquePanelSurface(innerWidth, tuiTheme.Panel.Title.Render(fmt.Sprintf("Question %d/%d", m.clarification.question+1, len(m.currentInput.Questions))))
	body := renderOpaqueMeasuredPanelText(innerWidth, tuiTheme.Panel.Body, question.Question)
	why := renderOpaqueMeasuredPanelText(innerWidth, tuiTheme.Text.Muted, question.WhyItMatters)
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
	spec := editorSpec
	if !spec.Visible {
		spec = editorSurfaceSpec{
			editorBindingSpec: editorBindingSpec{
				Visible: true,
				Label:   "Other",
			},
			FieldWidth: detailFormMeasureWidth(innerWidth),
			Rows:       1,
		}
	}
	input := m.buildEditorField(spec)
	otherBlock := []string{
		"",
		input.View,
		renderActionLine(m.focusRegion == FocusRegionActionPanel, m.canAdvanceClarification(question), actionLabel),
	}
	extra := make([]string, 0, len(otherBlock)+2)
	extra = append(extra, otherBlock...)
	if m.errorText != "" {
		candidate := append(append([]string{}, extra...), "", renderOpaquePanelSurface(innerWidth, tuiTheme.Status.Failed.Render("× "+m.errorText)))
		if lipgloss.Height(strings.Join(append(append([]string{}, header...), optionLines...), "\n"))+lipgloss.Height(strings.Join(candidate, "\n")) <= surface.MaxHeight {
			extra = candidate
		}
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
	prefixHeight := lipgloss.Height(strings.Join(append(append([]string{}, header...), optionLines[start:end]...), "\n")) + 1
	return builtPanel{
		View:          panelStyle.Render(strings.Join(content, "\n")),
		EditorOffsetX: panelFrameLeft(tuiTheme.Panel.Warning) + input.ContentOffsetX,
		EditorOffsetY: panelFrameTop(tuiTheme.Panel.Warning) + prefixHeight + input.ContentOffsetY,
		HasEditor:     true,
	}
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

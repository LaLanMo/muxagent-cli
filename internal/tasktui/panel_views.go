package tasktui

import (
	"fmt"
	"path/filepath"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
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

type builtInlineRow struct {
	Lines         []string
	HasEditor     bool
	EditorOffsetX int
	EditorOffsetY int
}

func renderOpaquePanelSurface(width int, content string) string {
	return lipgloss.NewStyle().
		Width(width).
		Background(tuiTheme.Surface.Panel).
		Render(content)
}

func renderOpaquePanelText(width int, style lipgloss.Style, text string) string {
	return renderOpaquePanelSurface(width, style.Render(text))
}

func renderOpaquePanelLines(width int, style lipgloss.Style, lines []string) []string {
	rendered := make([]string, 0, len(lines))
	for _, line := range lines {
		rendered = append(rendered, renderOpaquePanelText(width, style, line))
	}
	return rendered
}

func renderOpaqueMeasuredPanelText(width int, style lipgloss.Style, text string) string {
	measureWidth := detailBodyMeasureWidth(width)
	wrapped := strings.Join(wrapPanelBody(text, measureWidth), "\n")
	return renderOpaquePanelText(width, style, wrapped)
}

func (m Model) buildEditorField(spec editorSurfaceSpec) builtEditorField {
	width := max(18, spec.FieldWidth)
	focused := m.shouldFocusActiveEditor() && m.activeEditorSlot() != ""
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

func (m Model) buildInlineEditorRow(panelWidth, fieldWidth int, label string, focused, expanded bool) builtInlineRow {
	fieldWidth = clamp(fieldWidth, 1, max(1, panelWidth-2))
	lines := []string{renderOpaquePanelSurface(panelWidth, renderChoiceLine(focused, label))}
	if !expanded {
		return builtInlineRow{Lines: lines}
	}
	frameStyle := tuiTheme.Form.InputBlurred
	if focused {
		frameStyle = tuiTheme.Form.InputFocused
	}
	frameView := frameStyle.Width(fieldWidth).Render(m.editor.View())
	for _, line := range strings.Split(frameView, "\n") {
		lines = append(lines, renderOpaquePanelSurface(panelWidth, "  "+line))
	}
	return builtInlineRow{
		Lines:         lines,
		HasEditor:     true,
		EditorOffsetX: 2 + frameStyle.GetBorderLeftSize() + frameStyle.GetPaddingLeft(),
		EditorOffsetY: 1 + frameStyle.GetBorderTopSize() + frameStyle.GetPaddingTop(),
	}
}

func (m Model) renderNewTaskModal(layout newTaskScreenLayout) string {
	modalStyle := tuiTheme.Modal.Frame.Width(layout.modalWidth)
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
	title := renderOpaquePanelSurface(innerWidth, tuiTheme.Modal.Title.Render("New Task"))
	subtitle := renderOpaquePanelSurface(innerWidth, tuiTheme.Modal.Subtitle.Render(m.newTaskSubtitle()))
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
		EditorOffsetX: tuiTheme.Modal.Frame.GetPaddingLeft() + input.ContentOffsetX,
		EditorOffsetY: tuiTheme.Modal.Frame.GetPaddingTop() + prefixHeight + input.ContentOffsetY,
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
	content := []string{title, ""}
	feedbackExpanded := m.approval.choice == approvalRowFeedback || approvalHasFeedbackText(m.editor.Value())
	rows := []builtInlineRow{
		{Lines: []string{renderOpaquePanelSurface(innerWidth, renderActionLine(m.focusRegion == FocusRegionActionPanel && m.approval.choice == approvalRowApprove, true, m.approvalActionLabel(true)))}},
		{Lines: []string{renderOpaquePanelSurface(innerWidth, renderActionLine(m.focusRegion == FocusRegionActionPanel && m.approval.choice == approvalRowReject, true, m.approvalActionLabel(false)))}},
		m.buildInlineEditorRow(innerWidth, editorSpec.FieldWidth, "Feedback (optional)", m.focusRegion == FocusRegionActionPanel && m.approval.choice == approvalRowFeedback, feedbackExpanded),
	}
	build := builtPanel{}
	prefixHeight := lipgloss.Height(strings.Join(content, "\n"))
	for i, row := range rows {
		if i > 0 {
			content = append(content, "")
			prefixHeight++
		}
		if row.HasEditor {
			build.HasEditor = true
			build.EditorOffsetX = panelFrameLeft(tuiTheme.Panel.Warning) + row.EditorOffsetX
			build.EditorOffsetY = panelFrameTop(tuiTheme.Panel.Warning) + prefixHeight + row.EditorOffsetY
		}
		content = append(content, row.Lines...)
		prefixHeight += lipgloss.Height(strings.Join(row.Lines, "\n"))
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
	innerHeight := max(1, surface.MaxHeight-panelStyle.GetVerticalFrameSize())
	header := []string{}
	if nav := m.renderClarificationQuestionHeader(innerWidth); nav != "" {
		header = append(header, nav, "")
	}
	title := renderOpaquePanelSurface(innerWidth, tuiTheme.Panel.Title.Render(fmt.Sprintf("Question %d/%d", m.clarification.question+1, len(m.currentInput.Questions))))
	body := renderOpaqueMeasuredPanelText(innerWidth, tuiTheme.Panel.Body, question.Question)
	why := renderOpaqueMeasuredPanelText(innerWidth, tuiTheme.Text.Muted, question.WhyItMatters)
	header = append(header, title, body, why, "")
	rows := make([]builtInlineRow, 0, m.clarificationRowCount(question))
	for i, option := range question.Options {
		item := choiceItem{
			Label: option.Label + " · " + option.Description,
		}
		if question.MultiSelect {
			item.Indicator = choiceIndicatorChecklist
			item.Selected = clarificationAnswerContains(m.clarification.answers, m.clarification.question, option.Label)
		} else {
			item.Indicator = choiceIndicatorRadio
			item.Selected = !m.clarificationOtherSelected(question) && clarificationSelectedOption(question, clarificationAnswerAt(m.clarification.answers, m.clarification.question)) == option.Label
		}
		rows = append(rows, builtInlineRow{
			Lines: []string{renderOpaquePanelSurface(innerWidth, renderChoiceItemLine(m.focusRegion == FocusRegionChoices && m.clarification.option == i, item))},
		})
	}
	otherIndicator := choiceIndicatorRadio
	if question.MultiSelect {
		otherIndicator = choiceIndicatorChecklist
	}
	otherLabel := "Other"
	switch otherIndicator {
	case choiceIndicatorChecklist:
		otherLabel = renderChecklistLabel(m.clarificationOtherSelected(question), otherLabel)
	default:
		otherLabel = renderRadioLabel(m.clarificationOtherSelected(question), otherLabel)
	}
	otherExpanded := m.clarification.option == clarificationOtherRowIndex(question) || strings.TrimSpace(m.editor.Value()) != ""
	rows = append(rows, m.buildInlineEditorRow(innerWidth, editorSpec.FieldWidth, otherLabel, m.focusRegion == FocusRegionChoices && m.clarification.option == clarificationOtherRowIndex(question), otherExpanded))
	if !m.clarificationHasQuestionNavigator() {
		actionLabel := "Continue"
		if m.clarification.question == len(m.currentInput.Questions)-1 {
			actionLabel = "Submit answers"
		}
		rows = append(rows, builtInlineRow{
			Lines: []string{renderOpaquePanelSurface(innerWidth, renderActionLine(m.focusRegion == FocusRegionChoices && m.clarification.option == clarificationContinueRowIndex(question), m.canAdvanceClarification(question), actionLabel))},
		})
	}

	headerHeight := lipgloss.Height(strings.Join(header, "\n"))
	rowHeights := make([]int, 0, len(rows))
	for _, row := range rows {
		rowHeights = append(rowHeights, lipgloss.Height(strings.Join(row.Lines, "\n")))
	}
	errorLines := []string{}
	if m.errorText != "" {
		errorLines = append(errorLines, "", renderOpaquePanelSurface(innerWidth, tuiTheme.Status.Failed.Render("× "+m.errorText)))
	}
	errorHeight := lipgloss.Height(strings.Join(errorLines, "\n"))
	visibleRowHeight := max(1, innerHeight-headerHeight-errorHeight)
	start, end := clarificationVisibleWindowByHeight(rowHeights, m.clarification.option, visibleRowHeight)

	content := append([]string{}, header...)
	build := builtPanel{}
	prefixHeight := lipgloss.Height(strings.Join(header, "\n"))
	for rowIndex := start; rowIndex < end; rowIndex++ {
		row := rows[rowIndex]
		if row.HasEditor {
			build.HasEditor = true
			build.EditorOffsetX = panelFrameLeft(tuiTheme.Panel.Warning) + row.EditorOffsetX
			build.EditorOffsetY = panelFrameTop(tuiTheme.Panel.Warning) + prefixHeight + row.EditorOffsetY
		}
		content = append(content, row.Lines...)
		prefixHeight += lipgloss.Height(strings.Join(row.Lines, "\n"))
	}
	if len(errorLines) > 0 {
		content = append(content, errorLines...)
	}
	build.View = panelStyle.Render(strings.Join(content, "\n"))
	return build
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

func (m Model) renderClarificationQuestionHeader(width int) string {
	if !m.clarificationHasQuestionNavigator() || m.currentInput == nil {
		return ""
	}
	parts := make([]string, 0, len(m.currentInput.Questions))
	for index, question := range m.currentInput.Questions {
		label := clarificationQuestionHeaderLabel(question.Question, index)
		switch {
		case m.clarification.headerSelection == index:
			parts = append(parts, tuiTheme.Form.OptionActive.Render("▣ "+label))
		case m.clarificationQuestionAnswered(index):
			parts = append(parts, tuiTheme.Status.Done.Render("✓ "+label))
		default:
			parts = append(parts, tuiTheme.Text.Muted.Render("□ "+label))
		}
	}
	submitLabel := tuiTheme.Text.Muted.Render("□ Submit")
	if m.canSubmitClarification() {
		submitLabel = tuiTheme.Status.Done.Render("✓ Submit")
	}
	if m.clarificationSubmitSelected() {
		submitLabel = tuiTheme.Form.OptionActive.Render("▣ Submit")
	}
	left := strings.Join(parts, "  ")
	return renderOpaquePanelSurface(width, joinHorizontal(left, submitLabel, width))
}

func clarificationQuestionHeaderLabel(text string, index int) string {
	text = strings.TrimSpace(strings.ReplaceAll(text, "\n", " "))
	if text == "" {
		return fmt.Sprintf("Question %d", index+1)
	}
	return ansi.Truncate(text, 18, "…")
}

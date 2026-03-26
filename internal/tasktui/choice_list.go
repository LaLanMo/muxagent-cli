package tasktui

import "charm.land/lipgloss/v2"

type choiceIndicator int

const (
	choiceIndicatorPlain choiceIndicator = iota
	choiceIndicatorChecklist
	choiceIndicatorRadio
	choiceIndicatorAction
)

type choiceItem struct {
	Label     string
	Indicator choiceIndicator
	Selected  bool
	Enabled   bool
}

func moveSelection(index, delta, count int) int {
	if count <= 0 {
		return 0
	}
	return clamp(index+delta, 0, count-1)
}

func selectionWindow(total, selected, rows int) (start, end int) {
	if total <= rows {
		return 0, total
	}
	start = clamp(selected-(rows/2), 0, max(0, total-rows))
	end = min(total, start+rows)
	if end-start < rows {
		start = max(0, end-rows)
	}
	return start, end
}

func renderChoiceItems(width int, focusedIndex int, focused bool, items []choiceItem) []string {
	lines := make([]string, 0, len(items))
	for i, item := range items {
		lines = append(lines, lipgloss.NewStyle().Width(width).Render(renderChoiceItemLine(focused && i == focusedIndex, item)))
	}
	return lines
}

func renderChoiceItemLine(focused bool, item choiceItem) string {
	label := item.Label
	switch item.Indicator {
	case choiceIndicatorChecklist:
		label = renderChecklistLabel(item.Selected, label)
	case choiceIndicatorRadio:
		label = renderRadioLabel(item.Selected, label)
	case choiceIndicatorAction:
		return renderActionLine(focused, item.Enabled, label)
	}
	return renderChoiceLine(focused, label)
}

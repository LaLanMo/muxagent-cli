package tasktui

import "charm.land/lipgloss/v2"

func editorFieldFrameWidth() int {
	return max(
		tuiTheme.Form.InputFocused.GetHorizontalFrameSize(),
		tuiTheme.Form.InputBlurred.GetHorizontalFrameSize(),
	)
}

func editorFieldInnerWidth(outerWidth int) int {
	return max(1, outerWidth-editorFieldFrameWidth())
}

func editorFieldFrameLeft() int {
	return max(
		tuiTheme.Form.InputFocused.GetBorderLeftSize()+tuiTheme.Form.InputFocused.GetPaddingLeft(),
		tuiTheme.Form.InputBlurred.GetBorderLeftSize()+tuiTheme.Form.InputBlurred.GetPaddingLeft(),
	)
}

func editorFieldFrameTop() int {
	return max(
		tuiTheme.Form.InputFocused.GetBorderTopSize()+tuiTheme.Form.InputFocused.GetPaddingTop(),
		tuiTheme.Form.InputBlurred.GetBorderTopSize()+tuiTheme.Form.InputBlurred.GetPaddingTop(),
	)
}

func panelFrameLeft(styles ...lipgloss.Style) int {
	left := 0
	for _, style := range styles {
		left = max(left, style.GetBorderLeftSize()+style.GetPaddingLeft())
	}
	return left
}

func panelFrameTop(styles ...lipgloss.Style) int {
	top := 0
	for _, style := range styles {
		top = max(top, style.GetBorderTopSize()+style.GetPaddingTop())
	}
	return top
}

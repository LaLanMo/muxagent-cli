package tasktui

func renderChoiceLine(selected bool, label string) string {
	if selected {
		return tuiTheme.Form.OptionActive.Render("> " + label)
	}
	return tuiTheme.Form.OptionMuted.Render("  " + label)
}

func renderChecklistLabel(selected bool, label string) string {
	if selected {
		return "[x] " + label
	}
	return "[ ] " + label
}

func renderRadioLabel(selected bool, label string) string {
	if selected {
		return "(*) " + label
	}
	return "( ) " + label
}

func renderActionLine(focused, enabled bool, label string) string {
	prefix := "  "
	if focused {
		prefix = "> "
	}
	if !enabled {
		return tuiTheme.Text.Muted.Render(prefix + label)
	}
	if focused {
		return tuiTheme.Form.OptionActive.Render(prefix + label)
	}
	return tuiTheme.Form.OptionMuted.Render(prefix + label)
}

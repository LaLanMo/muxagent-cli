package tasktui

import (
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/viewport"
	"charm.land/lipgloss/v2"
)

func newStyledTextArea(placeholder string) textarea.Model {
	input := textarea.New()
	styles := textarea.DefaultDarkStyles()
	styles.Focused.Prompt = lipgloss.NewStyle().Foreground(tuiTheme.muted)
	styles.Focused.Text = lipgloss.NewStyle().Foreground(tuiTheme.text)
	styles.Focused.Placeholder = lipgloss.NewStyle().Foreground(tuiTheme.subtle)
	styles.Focused.CursorLine = lipgloss.NewStyle()
	styles.Focused.LineNumber = lipgloss.NewStyle().Foreground(tuiTheme.subtle)
	styles.Focused.CursorLineNumber = lipgloss.NewStyle().Foreground(tuiTheme.muted)
	styles.Blurred = styles.Focused
	styles.Cursor.Color = tuiTheme.text
	input.SetStyles(styles)
	input.Prompt = ""
	input.Placeholder = placeholder
	input.CharLimit = 512
	input.ShowLineNumbers = false
	input.SetHeight(1)
	input.MaxHeight = 0
	return input
}

func newArtifactPreviewViewport() viewport.Model {
	model := viewport.New()
	model.SoftWrap = true
	model.FillHeight = true
	model.KeyMap.Up = key.NewBinding(key.WithKeys("up"))
	model.KeyMap.Down = key.NewBinding(key.WithKeys("down"))
	model.KeyMap.PageUp = key.NewBinding()
	model.KeyMap.PageDown = key.NewBinding()
	model.KeyMap.HalfPageUp = key.NewBinding()
	model.KeyMap.HalfPageDown = key.NewBinding()
	model.KeyMap.Left = key.NewBinding()
	model.KeyMap.Right = key.NewBinding()
	return model
}

func newDetailViewport() viewport.Model {
	model := viewport.New()
	model.SoftWrap = true
	model.FillHeight = true
	model.KeyMap.Up = key.NewBinding(key.WithKeys("up"))
	model.KeyMap.Down = key.NewBinding(key.WithKeys("down"))
	model.KeyMap.PageUp = key.NewBinding()
	model.KeyMap.PageDown = key.NewBinding()
	model.KeyMap.HalfPageUp = key.NewBinding()
	model.KeyMap.HalfPageDown = key.NewBinding()
	model.KeyMap.Left = key.NewBinding()
	model.KeyMap.Right = key.NewBinding()
	return model
}

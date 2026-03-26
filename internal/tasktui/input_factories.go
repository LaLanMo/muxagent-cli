package tasktui

import (
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

func newStyledTextArea(placeholder string) textarea.Model {
	input := textarea.New()
	styles := textarea.DefaultDarkStyles()
	focusedBase := lipgloss.NewStyle().
		Foreground(tuiTheme.text).
		Background(tuiTheme.inputBgFocused)
	blurredBase := lipgloss.NewStyle().
		Foreground(tuiTheme.text).
		Background(tuiTheme.inputBgBlurred)
	styles.Focused.Base = focusedBase
	styles.Focused.Prompt = focusedBase.Foreground(tuiTheme.awaiting)
	styles.Focused.Text = focusedBase
	styles.Focused.Placeholder = focusedBase.Foreground(tuiTheme.halfMuted)
	styles.Focused.CursorLine = focusedBase
	styles.Focused.LineNumber = focusedBase.Foreground(tuiTheme.subtle)
	styles.Focused.CursorLineNumber = focusedBase.Foreground(tuiTheme.awaiting)
	styles.Blurred.Base = blurredBase
	styles.Blurred.Prompt = blurredBase.Foreground(tuiTheme.subtle)
	styles.Blurred.Text = blurredBase.Foreground(tuiTheme.text)
	styles.Blurred.Placeholder = blurredBase.Foreground(tuiTheme.subtle)
	styles.Blurred.CursorLine = blurredBase
	styles.Blurred.LineNumber = blurredBase.Foreground(tuiTheme.subtle)
	styles.Blurred.CursorLineNumber = blurredBase.Foreground(tuiTheme.subtle)
	styles.Cursor.Color = tuiTheme.text
	styles.Cursor.Shape = tea.CursorBar
	styles.Cursor.Blink = true
	input.SetStyles(styles)
	input.Prompt = ""
	input.Placeholder = placeholder
	input.CharLimit = 512
	input.ShowLineNumbers = false
	input.SetVirtualCursor(false)
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

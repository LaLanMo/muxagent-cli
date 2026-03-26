package tasktui

import (
	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
)

type ComposerSpec struct {
	Placeholder string
	CharLimit   int
	MinRows     int
	MaxRows     int
}

type ComposerModel struct {
	input textarea.Model
	spec  ComposerSpec
}

func newComposerModel(spec ComposerSpec) ComposerModel {
	input := newStyledTextArea(spec.Placeholder)
	if spec.CharLimit > 0 {
		input.CharLimit = spec.CharLimit
	}
	return ComposerModel{
		input: input,
		spec:  normalizeComposerSpec(spec),
	}
}

func normalizeComposerSpec(spec ComposerSpec) ComposerSpec {
	if spec.Placeholder == "" {
		spec.Placeholder = "Type here..."
	}
	if spec.MinRows <= 0 {
		spec.MinRows = 1
	}
	if spec.MaxRows <= 0 {
		spec.MaxRows = 10
	}
	return spec
}

func (c *ComposerModel) Update(msg tea.Msg) tea.Cmd {
	var focusCmd tea.Cmd
	if !c.input.Focused() {
		focusCmd = c.input.Focus()
	}
	if keyMsg, ok := msg.(tea.KeyPressMsg); ok {
		c.preGrow(keyMsg)
	}
	var cmd tea.Cmd
	c.input, cmd = c.input.Update(msg)
	c.syncHeight()
	return tea.Batch(focusCmd, cmd)
}

func (c *ComposerModel) preGrow(msg tea.KeyPressMsg) {
	if !keyMatches(msg, c.input.KeyMap.InsertNewline) {
		return
	}
	need := c.visualLineCount() + 1
	h := clamp(need, c.spec.MinRows, c.spec.MaxRows)
	if h > c.input.Height() {
		c.input.SetHeight(h)
	}
}

func (c *ComposerModel) syncHeight() {
	lines := c.visualLineCount()
	h := clamp(lines, c.spec.MinRows, c.spec.MaxRows)

	if offset := c.input.ScrollYOffset(); offset > 0 && lines <= h {
		row, col := c.input.Line(), c.input.Column()
		val := c.input.Value()
		c.input.SetValue(val)
		c.input.MoveToBegin()
		for range row {
			c.input.CursorDown()
		}
		c.input.SetCursorColumn(col)
	}

	if h != c.input.Height() {
		c.input.SetHeight(h)
	}
}

func (c ComposerModel) visualLineCount() int {
	return composerVisualLineCount(c.input.Value(), c.input.Width())
}

func (c *ComposerModel) Focus() tea.Cmd {
	return c.input.Focus()
}

func (c *ComposerModel) Blur() {
	c.input.Blur()
}

func (c ComposerModel) Focused() bool {
	return c.input.Focused()
}

func (c ComposerModel) View() string {
	return c.input.View()
}

func (c ComposerModel) Value() string {
	return c.input.Value()
}

func (c *ComposerModel) SetValue(value string) {
	c.input.SetValue(value)
	c.syncHeight()
}

func (c *ComposerModel) Reset() {
	c.input.Reset()
	c.input.SetHeight(c.spec.MinRows)
}

func (c *ComposerModel) SetWidth(width int) {
	c.input.SetWidth(width)
	c.syncHeight()
}

func (c *ComposerModel) SetPlaceholder(placeholder string) {
	c.spec.Placeholder = placeholder
	c.input.Placeholder = placeholder
}

func (c ComposerModel) Placeholder() string {
	return c.input.Placeholder
}

func (c ComposerModel) Height() int {
	return c.input.Height()
}

func (c ComposerModel) LineCount() int {
	return c.input.LineCount()
}

func (c ComposerModel) ScrollYOffset() int {
	return c.input.ScrollYOffset()
}

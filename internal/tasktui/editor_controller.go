package tasktui

import (
	"fmt"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"

	"github.com/LaLanMo/muxagent-cli/internal/taskruntime"
)

type EditorSpec struct {
	Placeholder string
	Rows        int
}

type EditorController struct {
	input textarea.Model
	spec  EditorSpec
	slot  string
	draft map[string]string
}

func newEditorController(spec EditorSpec) EditorController {
	spec = normalizeEditorSpec(spec)
	input := newStyledTextArea(spec.Placeholder)
	input.SetHeight(spec.Rows)
	return EditorController{
		input: input,
		spec:  spec,
		draft: map[string]string{},
	}
}

func normalizeEditorSpec(spec EditorSpec) EditorSpec {
	if spec.Placeholder == "" {
		spec.Placeholder = "Type here..."
	}
	if spec.Rows <= 0 {
		spec.Rows = 6
	}
	return spec
}

func (e *EditorController) Update(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	e.input, cmd = e.input.Update(msg)
	if e.slot != "" {
		e.draft[e.slot] = e.input.Value()
	}
	return cmd
}

func (e *EditorController) Focus() tea.Cmd {
	return e.input.Focus()
}

func (e *EditorController) Blur() {
	e.input.Blur()
}

func (e EditorController) Focused() bool {
	return e.input.Focused()
}

func (e EditorController) View() string {
	return e.input.View()
}

func (e EditorController) Value() string {
	return e.input.Value()
}

func (e *EditorController) SetValue(value string) {
	e.input.SetValue(value)
	if e.slot != "" {
		e.draft[e.slot] = value
	}
}

func (e *EditorController) Reset() {
	e.input.Reset()
	e.input.SetHeight(e.spec.Rows)
	if e.slot != "" {
		e.draft[e.slot] = ""
	}
}

func (e *EditorController) SetWidth(width int) {
	e.input.SetWidth(width)
}

func (e *EditorController) SetRows(rows int) {
	e.spec.Rows = max(1, rows)
	e.input.SetHeight(e.spec.Rows)
}

func (e *EditorController) SetPlaceholder(placeholder string) {
	e.spec.Placeholder = placeholder
	e.input.Placeholder = placeholder
}

func (e EditorController) Placeholder() string {
	return e.input.Placeholder
}

func (e EditorController) Height() int {
	return e.input.Height()
}

func (e EditorController) Width() int {
	return e.input.Width()
}

func (e EditorController) Cursor() *tea.Cursor {
	return e.input.Cursor()
}

func (e EditorController) LineCount() int {
	return e.input.LineCount()
}

func (e EditorController) ScrollYOffset() int {
	return e.input.ScrollYOffset()
}

func (e *EditorController) EnsureSlot(slot, value string) {
	if slot == "" {
		return
	}
	if _, ok := e.draft[slot]; ok {
		return
	}
	e.draft[slot] = value
	if e.slot == slot {
		e.input.SetValue(value)
	}
}

func (e *EditorController) SetSlot(slot string) {
	if e.slot == slot {
		return
	}
	if e.slot != "" {
		e.draft[e.slot] = e.input.Value()
	}
	e.slot = slot
	if slot == "" {
		e.input.Reset()
		e.input.SetHeight(e.spec.Rows)
		return
	}
	e.input.SetValue(e.draft[slot])
}

func (e *EditorController) ClearSlot(slot string) {
	delete(e.draft, slot)
	if e.slot == slot {
		e.input.Reset()
		e.input.SetHeight(e.spec.Rows)
	}
}

func (e *EditorController) ClearAll() {
	e.draft = map[string]string{}
	e.slot = ""
	e.input.Reset()
	e.input.SetHeight(e.spec.Rows)
}

func (e EditorController) Slot() string {
	return e.slot
}

const (
	editorSlotNewTask = "new-task"
)

func approvalEditorSlot(input *taskruntime.InputRequest) string {
	if input == nil || input.TaskID == "" || input.NodeRunID == "" {
		return ""
	}
	return fmt.Sprintf("approval:%s:%s", input.TaskID, input.NodeRunID)
}

func clarificationEditorSlot(input *taskruntime.InputRequest, question int) string {
	if input == nil || input.TaskID == "" || input.NodeRunID == "" {
		return ""
	}
	return fmt.Sprintf("clarification:%s:%s:%d", input.TaskID, input.NodeRunID, question)
}

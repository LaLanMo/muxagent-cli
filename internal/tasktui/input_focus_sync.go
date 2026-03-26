package tasktui

import tea "charm.land/bubbletea/v2"

func (m *Model) syncInputFocus() tea.Cmd {
	var cmds []tea.Cmd
	if m.dialog != nil {
		m.editor.Blur()
		return nil
	}
	if m.focusRegion == FocusRegionComposer && m.activeEditorSlot() != "" {
		cmds = append(cmds, m.editor.Focus())
	} else {
		m.editor.Blur()
	}
	return tea.Batch(cmds...)
}

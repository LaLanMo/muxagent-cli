package tasktui

import (
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

func (m Model) editorCursor() *tea.Cursor {
	if m.dialog != nil || !m.editor.Focused() || m.activeEditorSlot() == "" {
		return nil
	}

	cur := m.editor.Cursor()
	if cur == nil {
		return nil
	}

	offsetX, offsetY, ok := m.editorCursorOffset()
	if !ok {
		return nil
	}

	cur.Position.X += offsetX
	cur.Position.Y += offsetY
	return cur
}

func (m Model) editorCursorOffset() (int, int, bool) {
	switch m.screen {
	case ScreenTaskConfigs:
		return m.taskConfigFormEditorCursorOffset()
	case ScreenNewTask:
		return m.newTaskEditorCursorOffset()
	case ScreenApproval:
		return m.approvalEditorCursorOffset()
	case ScreenClarification:
		return m.clarificationEditorCursorOffset()
	default:
		return 0, 0, false
	}
}

func (m Model) taskConfigFormEditorCursorOffset() (int, int, bool) {
	if m.taskConfigs.form == nil {
		return 0, 0, false
	}
	metrics := m.computeScreenMetrics()
	modalWidth := m.taskConfigFormModalWidth(metrics.innerWidth)
	panel := m.buildTaskConfigFormPanel(modalWidth)
	if !panel.HasEditor {
		return 0, 0, false
	}
	modal := tuiTheme.modal.Width(modalWidth).Render(panel.View)
	modalX := tuiTheme.canvas.GetPaddingLeft() + max(0, (metrics.innerWidth-lipgloss.Width(modal))/2)
	modalY := tuiTheme.canvas.GetPaddingTop() + max(0, (metrics.innerHeight-lipgloss.Height(modal))/2)
	return modalX + panel.EditorOffsetX, modalY + panel.EditorOffsetY, true
}

func (m Model) newTaskEditorCursorOffset() (int, int, bool) {
	spec := m.currentEditorSurfaceSpec()
	if !spec.Visible {
		return 0, 0, false
	}
	metrics := m.computeScreenMetrics()
	header := m.renderAppHeader(metrics.innerWidth)
	footer := renderFooterHintBar(metrics.innerWidth, m.newTaskModalHint())
	layout := m.computeNewTaskScreenLayout(header, footer)
	panel := m.buildNewTaskPanel(layout.modalInnerWidth)
	if !panel.HasEditor {
		return 0, 0, false
	}

	modal := tuiTheme.modal.Width(layout.modalWidth).Render(panel.View)
	modalX := tuiTheme.canvas.GetPaddingLeft() + max(0, (layout.innerWidth-layout.modalWidth)/2)
	modalY := tuiTheme.canvas.GetPaddingTop() + layout.headerHeight + max(0, (layout.bodyHeight-lipgloss.Height(modal))/2)
	return modalX + panel.EditorOffsetX, modalY + panel.EditorOffsetY, true
}

func (m Model) approvalEditorCursorOffset() (int, int, bool) {
	snapshot := m.computeDetailLayoutSnapshot()
	if !snapshot.PanelView.HasEditor {
		return 0, 0, false
	}
	frame := snapshot.Frame
	panel := snapshot.PanelView
	surfaces := snapshot.Surfaces

	panelX := tuiTheme.canvas.GetPaddingLeft() + max(0, (frame.innerWidth-frame.contentWidth)/2)
	panelY := tuiTheme.canvas.GetPaddingTop() + frame.headerHeight + surfaces.Body.topBodyHeight + 1
	return panelX + panel.EditorOffsetX, panelY + panel.EditorOffsetY, true
}

func (m Model) clarificationEditorCursorOffset() (int, int, bool) {
	snapshot := m.computeDetailLayoutSnapshot()
	if !snapshot.PanelView.HasEditor {
		return 0, 0, false
	}
	frame := snapshot.Frame
	panel := snapshot.PanelView
	surfaces := snapshot.Surfaces

	panelX := tuiTheme.canvas.GetPaddingLeft() + max(0, (frame.innerWidth-frame.contentWidth)/2)
	panelY := tuiTheme.canvas.GetPaddingTop() + frame.headerHeight + surfaces.Body.topBodyHeight + 1
	return panelX + panel.EditorOffsetX, panelY + panel.EditorOffsetY, true
}

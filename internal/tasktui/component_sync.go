package tasktui

import "charm.land/bubbles/v2/list"

func (m *Model) syncComponents() {
	m.syncTaskList()
	m.syncArtifactPane()
	m.normalizeFailureAction()
	m.normalizeFocusRegion()
	m.syncEditorState()
	m.syncInputWidths()
	m.syncDetailViewport()
}

func (m *Model) syncTaskList() {
	selectedID := ""
	selectedAction := taskListActionNone
	previousItemCount := len(m.taskList.Items())
	if selected, ok := m.taskList.SelectedItem().(taskListItem); ok {
		selectedAction = selected.action
		if selected.action == taskListActionNone {
			selectedID = selected.view.Task.ID
		}
	}
	items := make([]list.Item, 0, len(m.tasks)+1)
	items = append(items, m.newTaskListActionItem())
	for _, view := range m.tasks {
		items = append(items, taskListItem{view: view})
	}
	cmd := m.taskList.SetItems(items)
	if cmd != nil {
		_ = cmd()
	}
	if len(items) == 0 {
		return
	}
	if selectedAction == taskListActionNewTask && previousItemCount > 1 {
		m.taskList.Select(0)
		return
	}
	if selectedID != "" {
		for i, item := range items {
			entry := item.(taskListItem)
			if entry.action == taskListActionNone && entry.view.Task.ID == selectedID {
				m.taskList.Select(i)
				return
			}
		}
	}
	if len(m.tasks) > 0 {
		m.taskList.Select(clamp(max(1, m.taskList.Index()), 1, len(items)-1))
		return
	}
	m.taskList.Select(clamp(m.taskList.Index(), 0, len(items)-1))
}

func (m Model) newTaskListActionItem() taskListItem {
	meta := "Create a new task in this working directory."
	if len(m.tasks) == 0 {
		meta = "No tasks in this working directory yet. Press Enter to start one."
	}
	return taskListItem{
		action: taskListActionNewTask,
		title:  "new task",
		meta:   meta,
	}
}

func (m *Model) syncArtifactPane() {
	selectedPath := selectedArtifactPath(m.artifactItems, m.artifactIndex)
	m.artifactItems = buildArtifactItems(m.workDir, m.current, m.currentInput)
	if len(m.artifactItems) == 0 {
		m.artifactIndex = 0
		m.artifactDrillIn = false
		m.artifactPreview.SetContent("")
		m.artifactPreview.GotoTop()
		return
	}
	if selectedPath != "" {
		for i, item := range m.artifactItems {
			if item.Path == selectedPath {
				m.artifactIndex = i
				return
			}
		}
	}
	m.artifactIndex = defaultArtifactIndex(m.artifactItems, m.screen, m.currentInput)
}

func (m *Model) syncInputWidths() {
	spec := m.currentEditorSurfaceSpec()
	if !spec.Visible {
		return
	}
	m.editor.SetWidth(editorFieldInnerWidth(spec.FieldWidth))
	m.editor.SetRows(spec.Rows)
}

func (m *Model) syncDetailViewport() {
	metrics := m.computeScreenMetrics()
	contentWidth := detailContentWidth(metrics.innerWidth)
	header := m.renderDetailHeader(contentWidth)
	footer := m.renderDetailFooter(surfaceRect{Width: contentWidth})
	frame := m.computeDetailFrameLayout(contentWidth, header, footer)
	panel := m.renderDetailPanel(m.computeDetailPanelSurface(frame))
	surfaces := m.computeDetailScreenSurfaces(frame, panel)

	m.detailViewport.SetWidth(surfaces.Timeline.Width)
	m.detailViewport.SetHeight(surfaces.Timeline.Height)
	m.detailViewport.SetContent(m.renderDetailTimeline(surfaces.Timeline))
	if m.autoScrollDetail {
		m.detailViewport.GotoBottom()
		m.autoScrollDetail = false
	}
	m.syncArtifactPreview(surfaces.Body.previewWidth, surfaces.Body.topBodyHeight)

	taskListHeader := m.renderTaskListHeader(metrics.innerWidth)
	taskListFooter := m.renderTaskListFooter(surfaceRect{Width: metrics.innerWidth})
	taskListLayout := m.computeTaskListScreenLayout(taskListHeader, taskListFooter)
	m.taskList.SetSize(taskListLayout.innerWidth, taskListLayout.bodyHeight)
}

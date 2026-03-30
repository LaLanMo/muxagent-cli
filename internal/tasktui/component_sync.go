package tasktui

import "charm.land/bubbles/v2/list"

func (m *Model) syncComponents() {
	m.syncTaskList()
	m.syncConfigList()
	m.syncArtifactPane()
	m.normalizeFailureAction()
	m.normalizeFocusRegion()
	m.syncEditorState()
	m.syncInputWidths()
	m.syncDetailViewport()
}

func (m *Model) syncTaskList() {
	const actionItemCount = 2
	selectedID := ""
	selectedAction := taskListActionNone
	if selected, ok := selectedTaskListItem(m.taskList); ok {
		selectedAction = selected.action
		if selected.action == taskListActionNone {
			selectedID = selected.view.Task.ID
		}
	}
	items := make([]list.Item, 0, len(m.tasks)+actionItemCount)
	items = append(items, m.newTaskListActionItem())
	items = append(items, m.manageTaskConfigsListActionItem())
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
	for i, item := range items {
		entry := item.(taskListItem)
		if entry.action == selectedAction && selectedAction != taskListActionNone {
			m.taskList.Select(i)
			return
		}
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
		m.taskList.Select(clamp(max(actionItemCount, m.taskList.Index()), actionItemCount, len(items)-1))
		return
	}
	m.taskList.Select(clamp(m.taskList.Index(), 0, len(items)-1))
}

func (m *Model) syncConfigList() {
	selectedAlias := m.taskConfigs.selectedAlias
	if selectedAlias == "" {
		if selected, ok := selectedTaskConfigListItem(m.configList); ok {
			selectedAlias = selected.summary.Alias
		}
	}
	items := make([]list.Item, 0, len(m.taskConfigs.entries))
	for _, entry := range m.taskConfigs.entries {
		items = append(items, taskConfigListItem{summary: entry})
	}
	cmd := m.configList.SetItems(items)
	if cmd != nil {
		_ = cmd()
	}
	if len(items) == 0 {
		return
	}
	for i, item := range items {
		entry := item.(taskConfigListItem)
		if entry.summary.Alias == selectedAlias {
			m.configList.Select(i)
			m.taskConfigs.selectedAlias = entry.summary.Alias
			return
		}
	}
	m.configList.Select(clamp(m.configList.Index(), 0, len(items)-1))
	if selected, ok := selectedTaskConfigListItem(m.configList); ok {
		m.taskConfigs.selectedAlias = selected.summary.Alias
	}
}

func (m Model) newTaskListActionItem() taskListItem {
	meta := "Create a new task with config " + m.selectedTaskConfigAlias() + "."
	if len(m.tasks) == 0 {
		meta = "No tasks in this working directory yet. Press Enter to start one with config " + m.selectedTaskConfigAlias() + "."
	}
	return taskListItem{
		action: taskListActionNewTask,
		title:  "new task",
		meta:   meta,
	}
}

func (m Model) manageTaskConfigsListActionItem() taskListItem {
	meta := "Manage task config bundles and defaults."
	return taskListItem{
		action: taskListActionManageConfigs,
		title:  "task configs",
		meta:   meta,
	}
}

func (m *Model) syncArtifactPane() {
	selectedPath := selectedArtifactPath(m.artifactItems, m.artifactIndex)
	previousPreviewPath := m.artifactPreviewPath
	m.artifactItems = buildArtifactItems(m.workDir, m.current, m.currentInput)
	if len(m.artifactItems) == 0 {
		m.artifactIndex = 0
		m.artifactErrorText = ""
		m.artifactPreviewPath = ""
		m.artifactPreview.SetContent("")
		m.artifactPreview.GotoTop()
		return
	}
	if selectedPath != "" {
		for i, item := range m.artifactItems {
			if item.Path == selectedPath {
				m.artifactIndex = i
				if item.Path != previousPreviewPath {
					m.artifactErrorText = ""
				}
				return
			}
		}
	}
	m.artifactIndex = defaultArtifactIndex(m.artifactItems, m.screen, m.currentInput)
	if selectedArtifactPath(m.artifactItems, m.artifactIndex) != previousPreviewPath {
		m.artifactErrorText = ""
	}
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
	snapshot := m.computeDetailLayoutSnapshot()
	surfaces := snapshot.Surfaces

	m.detailViewport.SetWidth(surfaces.Timeline.Width)
	m.detailViewport.SetHeight(surfaces.Timeline.Height)
	m.detailViewport.SetContent(m.renderDetailTimeline(surfaces.Timeline))
	if m.autoScrollDetail {
		m.detailViewport.GotoBottom()
		m.autoScrollDetail = false
	}
	m.syncArtifactPreview(surfaces.Preview)

	taskListHeader := m.renderTaskListHeader(metrics.innerWidth)
	taskListFooter := m.renderTaskListFooter(surfaceRect{Width: metrics.innerWidth})
	taskListLayout := m.computeTaskListScreenLayout(taskListHeader, taskListFooter)
	m.taskList.SetSize(taskListLayout.innerWidth, taskListLayout.bodyHeight)

	configListHeader := m.renderTaskConfigListHeader(metrics.innerWidth)
	configListFooter := m.renderTaskConfigListFooter(surfaceRect{Width: metrics.innerWidth})
	configListLayout := m.computeTaskListScreenLayout(configListHeader, configListFooter)
	m.configList.SetSize(configListLayout.innerWidth, configListLayout.bodyHeight)
}

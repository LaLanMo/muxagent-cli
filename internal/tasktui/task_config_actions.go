package tasktui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/LaLanMo/muxagent-cli/internal/taskconfig"
)

func (m *Model) openTaskConfigs() tea.Cmd {
	if m.screen != ScreenTaskConfigs {
		m.returnScreen = m.screen
	}
	m.taskConfigs.pending = true
	m.taskConfigs.errorText = ""
	m.taskConfigs.statusText = ""
	m.taskConfigs.form = nil
	m.taskConfigs.confirm = nil
	m.taskConfigs.selectedAlias = firstNonEmpty(m.taskConfigs.selectedAlias, m.selectedConfigAlias, m.configCatalog.DefaultAlias)
	m.setScreen(ScreenTaskConfigs)
	m.syncComponents()
	return tea.Batch(
		m.syncInputFocus(),
		m.loadTaskConfigCatalogCmd(m.taskConfigs.selectedAlias, ""),
	)
}

func (m *Model) closeTaskConfigs() tea.Cmd {
	m.closeTaskConfigForm()
	m.taskConfigs.confirm = nil
	m.setScreen(m.returnScreen)
	if m.screen == ScreenTaskConfigs {
		m.setScreen(ScreenTaskList)
	}
	m.syncComponents()
	return m.syncInputFocus()
}

func (m Model) loadTaskConfigCatalogCmd(preferredAlias, taskSelectionAlias string) tea.Cmd {
	return func() tea.Msg {
		catalog, err := taskconfig.LoadCatalog()
		if err != nil {
			return taskConfigCatalogLoadedMsg{err: err}
		}
		reg, err := taskconfig.LoadRegistry()
		if err != nil {
			return taskConfigCatalogLoadedMsg{err: err}
		}
		entries := buildTaskConfigSummaries(catalog, reg)
		selectedAlias := firstNonEmpty(preferredAlias, catalog.DefaultAlias)
		if !hasTaskConfigSummary(entries, selectedAlias) {
			selectedAlias = firstAvailableTaskConfigAlias(entries)
		}
		return taskConfigCatalogLoadedMsg{
			catalog:            catalog,
			entries:            entries,
			selectedAlias:      selectedAlias,
			taskSelectionAlias: taskSelectionAlias,
		}
	}
}

func buildTaskConfigSummaries(catalog *taskconfig.Catalog, reg taskconfig.Registry) []taskConfigSummary {
	if catalog == nil {
		return nil
	}
	summaries := make([]taskConfigSummary, 0, len(catalog.Entries))
	loadedEntries := make([]taskconfig.CatalogEntry, 0, len(catalog.Entries))
	for _, entry := range catalog.Entries {
		summary := taskConfigSummary{
			Alias:      entry.Alias,
			ConfigPath: entry.Path,
			IsDefault:  entry.Alias == catalog.DefaultAlias,
			BuiltinID:  entry.BuiltinID,
			Builtin:    entry.Builtin,
		}
		for _, regEntry := range reg.Configs {
			if regEntry.Alias == entry.Alias {
				summary.BundlePath = regEntry.Path
				break
			}
		}
		cfg, err := entry.LoadConfig()
		if err != nil {
			summary.LoadErr = err.Error()
		} else {
			entry.Config = cfg
			summary.Runtime = runtimeDisplayLabel(cfg.Runtime)
			summary.Description = cfg.Description
			for _, node := range cfg.Topology.Nodes {
				summary.NodeNames = append(summary.NodeNames, node.Name)
			}
		}
		loadedEntries = append(loadedEntries, entry)
		summaries = append(summaries, summary)
	}
	catalog.Entries = loadedEntries
	return summaries
}

func firstAvailableTaskConfigAlias(entries []taskConfigSummary) string {
	for _, entry := range entries {
		if strings.TrimSpace(entry.Alias) != "" {
			return entry.Alias
		}
	}
	return ""
}

func hasTaskConfigSummary(entries []taskConfigSummary, alias string) bool {
	for _, entry := range entries {
		if entry.Alias == alias {
			return true
		}
	}
	return false
}

func (m Model) selectedManagedTaskConfig() (taskConfigSummary, bool) {
	if selected, ok := selectedTaskConfigListItem(m.configList); ok {
		return selected.summary, true
	}
	for _, entry := range m.taskConfigs.entries {
		if entry.Alias == m.taskConfigs.selectedAlias {
			return entry, true
		}
	}
	return taskConfigSummary{}, false
}

func (m *Model) openCloneTaskConfigForm() tea.Cmd {
	selected, ok := m.selectedManagedTaskConfig()
	if !ok {
		return nil
	}
	slot := "task-config-clone:" + selected.Alias
	m.taskConfigs.form = &taskConfigFormState{
		Mode:        taskConfigFormClone,
		SourceAlias: selected.Alias,
		Title:       "Clone Task Config",
		Label:       "New alias",
		Placeholder: "reviewer",
		SubmitLabel: "Clone",
		Slot:        slot,
	}
	m.taskConfigs.confirm = nil
	m.taskConfigs.errorText = ""
	m.taskConfigs.statusText = ""
	m.editor.ClearSlot(slot)
	m.focusRegion = FocusRegionComposer
	m.syncComponents()
	return m.syncInputFocus()
}

func (m *Model) openRenameTaskConfigForm() tea.Cmd {
	selected, ok := m.selectedManagedTaskConfig()
	if !ok {
		return nil
	}
	if selected.Builtin {
		m.taskConfigs.errorText = fmt.Sprintf("config %q cannot be renamed", selected.Alias)
		m.syncComponents()
		return nil
	}
	slot := "task-config-rename:" + selected.Alias
	m.taskConfigs.form = &taskConfigFormState{
		Mode:        taskConfigFormRename,
		SourceAlias: selected.Alias,
		Title:       "Rename Task Config Alias",
		Label:       "Alias",
		Placeholder: "deep-review",
		SubmitLabel: "Rename",
		Slot:        slot,
		SeedValue:   selected.Alias,
	}
	m.taskConfigs.confirm = nil
	m.taskConfigs.errorText = ""
	m.taskConfigs.statusText = ""
	m.editor.ClearSlot(slot)
	m.focusRegion = FocusRegionComposer
	m.syncComponents()
	return m.syncInputFocus()
}

func (m *Model) openDeleteTaskConfigConfirm() tea.Cmd {
	selected, ok := m.selectedManagedTaskConfig()
	if !ok {
		return nil
	}
	if selected.Builtin {
		m.taskConfigs.errorText = fmt.Sprintf("config %q cannot be deleted", selected.Alias)
		m.syncComponents()
		return nil
	}
	body := "Delete config " + selected.Alias
	if selected.BundlePath != "" {
		body += " and remove bundle " + selected.BundlePath
	}
	body += "? Existing tasks keep their materialized task-local config snapshots."
	m.taskConfigs.confirm = &taskConfigConfirmState{
		Alias:        selected.Alias,
		Title:        "Delete Task Config",
		Body:         body,
		ConfirmLabel: "Delete",
	}
	m.taskConfigs.form = nil
	m.taskConfigs.errorText = ""
	m.taskConfigs.statusText = ""
	m.focusRegion = FocusRegionNone
	m.syncComponents()
	return m.syncInputFocus()
}

func (m *Model) closeTaskConfigForm() {
	if m.taskConfigs.form != nil {
		m.editor.ClearSlot(m.taskConfigs.form.Slot)
	}
	m.taskConfigs.form = nil
}

func (m *Model) submitTaskConfigForm() tea.Cmd {
	form := m.taskConfigs.form
	if form == nil {
		return nil
	}
	alias := strings.TrimSpace(m.editor.Value())
	if alias == "" {
		form.ErrorText = "alias is required"
		m.syncComponents()
		return nil
	}
	form.ErrorText = ""
	m.taskConfigs.errorText = ""
	m.taskConfigs.statusText = ""
	sourceAlias := form.SourceAlias
	mode := form.Mode

	switch mode {
	case taskConfigFormClone:
		source, ok := m.configCatalog.Entry(sourceAlias)
		if !ok {
			form.ErrorText = "source config no longer exists"
			m.syncComponents()
			return nil
		}
		if _, err := taskconfig.CloneConfig(alias, source.Path); err != nil {
			form.ErrorText = err.Error()
			m.syncComponents()
			return nil
		}
		m.taskConfigs.pending = true
		m.closeTaskConfigForm()
		m.syncComponents()
		return m.loadTaskConfigCatalogCmd(alias, alias)
	case taskConfigFormRename:
		updateTaskSelection := ""
		if m.selectedConfigAlias == sourceAlias {
			updateTaskSelection = alias
		}
		if _, err := taskconfig.RenameConfigAlias(sourceAlias, alias); err != nil {
			form.ErrorText = err.Error()
			m.syncComponents()
			return nil
		}
		m.taskConfigs.pending = true
		m.closeTaskConfigForm()
		m.syncComponents()
		return m.loadTaskConfigCatalogCmd(alias, updateTaskSelection)
	default:
		return nil
	}
}

func (m *Model) submitSetDefaultTaskConfig() tea.Cmd {
	selected, ok := m.selectedManagedTaskConfig()
	if !ok {
		return nil
	}
	if selected.IsDefault {
		m.taskConfigs.errorText = ""
		m.taskConfigs.statusText = fmt.Sprintf("config %q is already the default", selected.Alias)
		m.syncComponents()
		return nil
	}
	if strings.TrimSpace(selected.LoadErr) != "" {
		m.taskConfigs.errorText = fmt.Sprintf("config %q is invalid and cannot be the default", selected.Alias)
		m.taskConfigs.statusText = ""
		m.syncComponents()
		return nil
	}
	if _, err := taskconfig.SetDefaultConfig(selected.Alias); err != nil {
		m.taskConfigs.errorText = err.Error()
		m.taskConfigs.statusText = ""
		m.syncComponents()
		return nil
	}
	m.taskConfigs.pending = true
	m.taskConfigs.errorText = ""
	m.taskConfigs.statusText = fmt.Sprintf("config %q is now the default", selected.Alias)
	m.syncComponents()
	return m.loadTaskConfigCatalogCmd(selected.Alias, selected.Alias)
}

func (m *Model) submitDeleteTaskConfig() tea.Cmd {
	confirm := m.taskConfigs.confirm
	if confirm == nil {
		return nil
	}
	selectedAlias := confirm.Alias
	updateTaskSelection := ""
	if m.selectedConfigAlias == selectedAlias {
		updateTaskSelection = taskconfig.DefaultAlias
	}
	if _, err := taskconfig.DeleteConfig(selectedAlias); err != nil {
		m.taskConfigs.errorText = err.Error()
		m.taskConfigs.statusText = ""
		m.syncComponents()
		return nil
	}
	m.taskConfigs.confirm = nil
	m.taskConfigs.pending = true
	m.taskConfigs.errorText = ""
	m.taskConfigs.statusText = ""
	m.syncComponents()
	return m.loadTaskConfigCatalogCmd(taskconfig.DefaultAlias, updateTaskSelection)
}

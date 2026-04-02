package tasktui

import (
	"errors"
	"fmt"
	"strings"

	appconfig "github.com/LaLanMo/muxagent-cli/internal/config"
	"github.com/LaLanMo/muxagent-cli/internal/taskconfig"
)

func configCatalogOrDefault(catalog *taskconfig.Catalog) *taskconfig.Catalog {
	if catalog != nil && len(catalog.Entries) > 0 {
		if catalog.DefaultAlias == "" {
			catalog.DefaultAlias = taskconfig.DefaultAlias
		}
		return catalog
	}
	loaded, err := taskconfig.LoadCatalog()
	if err == nil && loaded != nil && len(loaded.Entries) > 0 {
		return loaded
	}
	fallback, err := taskconfig.EmbeddedBuiltinCatalog()
	if err == nil && fallback != nil && len(fallback.Entries) > 0 {
		return fallback
	}
	return &taskconfig.Catalog{
		DefaultAlias: taskconfig.DefaultAlias,
	}
}

func (m Model) selectedTaskConfigEntry() taskconfig.CatalogEntry {
	if entry, ok := m.configCatalog.Entry(m.selectedConfigAlias); ok {
		if entry.Config == nil && strings.TrimSpace(entry.Path) != "" {
			if cfg, err := entry.LoadConfig(); err == nil {
				entry.Config = cfg
			}
		}
		return entry
	}
	entry, err := m.configCatalog.DefaultEntry()
	if err == nil {
		if entry.Config == nil && strings.TrimSpace(entry.Path) != "" {
			if cfg, loadErr := entry.LoadConfig(); loadErr == nil {
				entry.Config = cfg
			}
		}
		return entry
	}
	fallback, err := taskconfig.EmbeddedBuiltinCatalog()
	if err == nil {
		if entry, ok := fallback.Entry(taskconfig.DefaultAlias); ok {
			return entry
		}
	}
	return taskconfig.CatalogEntry{
		Alias:  taskconfig.DefaultAlias,
		Config: &taskconfig.Config{},
	}
}

func (m Model) selectedTaskConfig() *taskconfig.Config {
	return m.selectedTaskConfigEntry().Config
}

func (m Model) selectedTaskConfigAlias() string {
	return m.selectedTaskConfigEntry().Alias
}

func (m Model) selectedTaskConfigPath() string {
	return m.selectedTaskConfigEntry().Path
}

func taskConfigRuntime(cfg *taskconfig.Config) appconfig.RuntimeID {
	if cfg == nil {
		return ""
	}
	runtime, err := taskconfig.ResolveRuntime(cfg)
	if err != nil {
		return ""
	}
	return runtime
}

func (m *Model) cycleTaskConfig(delta int) bool {
	entries := m.configCatalog.Entries
	if len(entries) <= 1 {
		return false
	}
	if delta == 0 {
		return false
	}
	index := 0
	for i, entry := range entries {
		if entry.Alias == m.selectedConfigAlias {
			index = i
			break
		}
	}
	step := 1
	if delta < 0 {
		step = -1
	}
	for attempts := 0; attempts < len(entries)-1; attempts++ {
		index = (index + step) % len(entries)
		if index < 0 {
			index += len(entries)
		}
		next := entries[index].Alias
		if next == m.selectedConfigAlias {
			continue
		}
		if !m.taskConfigIsLaunchable(next) {
			continue
		}
		m.selectedConfigAlias = next
		return true
	}
	return false
}

func (m Model) effectiveLaunchRuntime() appconfig.RuntimeID {
	return taskConfigRuntime(m.selectedTaskConfig())
}

func (m Model) taskConfigSummary(alias string) (taskConfigSummary, bool) {
	for _, entry := range m.taskConfigs.entries {
		if entry.Alias == alias {
			return entry, true
		}
	}
	return taskConfigSummary{}, false
}

func (m *Model) loadTaskConfigEntry(alias string) (taskconfig.CatalogEntry, bool, error) {
	for i, entry := range m.configCatalog.Entries {
		if entry.Alias != alias {
			continue
		}
		if entry.Config != nil {
			return entry, true, nil
		}
		cfg, err := entry.LoadConfig()
		if err != nil {
			return taskconfig.CatalogEntry{}, true, err
		}
		entry.Config = cfg
		m.configCatalog.Entries[i] = entry
		return entry, true, nil
	}
	return taskconfig.CatalogEntry{}, false, nil
}

func (m *Model) taskConfigIsLaunchable(alias string) bool {
	alias = strings.TrimSpace(alias)
	if alias == "" {
		return false
	}
	if summary, ok := m.taskConfigSummary(alias); ok && strings.TrimSpace(summary.LoadErr) != "" {
		return false
	}
	_, ok, err := m.loadTaskConfigEntry(alias)
	return ok && err == nil
}

func (m *Model) firstLaunchableTaskConfigAlias(preferred ...string) string {
	seen := map[string]struct{}{}
	candidates := append([]string(nil), preferred...)
	candidates = append(candidates, m.configCatalog.DefaultAlias)
	for _, alias := range candidates {
		alias = strings.TrimSpace(alias)
		if alias == "" {
			continue
		}
		if _, ok := seen[alias]; ok {
			continue
		}
		seen[alias] = struct{}{}
		if m.taskConfigIsLaunchable(alias) {
			return alias
		}
	}
	for _, entry := range m.configCatalog.Entries {
		if _, ok := seen[entry.Alias]; ok {
			continue
		}
		seen[entry.Alias] = struct{}{}
		if m.taskConfigIsLaunchable(entry.Alias) {
			return entry.Alias
		}
	}
	return ""
}

func (m *Model) launchTaskConfigEntry() (taskconfig.CatalogEntry, error) {
	alias := strings.TrimSpace(m.selectedConfigAlias)
	if alias != "" {
		if summary, ok := m.taskConfigSummary(alias); ok && strings.TrimSpace(summary.LoadErr) != "" {
			return taskconfig.CatalogEntry{}, fmt.Errorf("task config %q is invalid: %s", alias, summary.LoadErr)
		}
		if entry, ok, err := m.loadTaskConfigEntry(alias); err != nil {
			return taskconfig.CatalogEntry{}, fmt.Errorf("task config %q is invalid: %w", alias, err)
		} else if ok {
			return entry, nil
		}
	}
	fallbackAlias := m.firstLaunchableTaskConfigAlias()
	if fallbackAlias == "" {
		return taskconfig.CatalogEntry{}, errors.New("no valid task config available")
	}
	entry, _, err := m.loadTaskConfigEntry(fallbackAlias)
	if err != nil {
		return taskconfig.CatalogEntry{}, fmt.Errorf("task config %q is invalid: %w", fallbackAlias, err)
	}
	m.selectedConfigAlias = fallbackAlias
	return entry, nil
}

type followUpConfigOption struct {
	Selection followUpConfigSelection
	Config    *taskconfig.Config
}

func (m Model) inheritedFollowUpConfigSelection() followUpConfigSelection {
	if m.current == nil {
		return followUpConfigSelection{}
	}
	return followUpConfigSelection{
		Alias:     strings.TrimSpace(m.current.Task.ConfigAlias),
		Path:      strings.TrimSpace(m.current.Task.ConfigPath),
		Inherited: true,
	}
}

func (m *Model) seedFollowUpConfigSelection() {
	m.followUp.config = m.inheritedFollowUpConfigSelection()
}

func (m Model) selectedFollowUpConfigSelection() followUpConfigSelection {
	selection := m.followUp.config
	if strings.TrimSpace(selection.Alias) == "" && strings.TrimSpace(selection.Path) == "" {
		return m.inheritedFollowUpConfigSelection()
	}
	return selection
}

func (m Model) followUpConfigOptions() []followUpConfigOption {
	inherited := m.inheritedFollowUpConfigSelection()
	options := make([]followUpConfigOption, 0, len(m.configCatalog.Entries)+1)
	inheritedRepresented := false
	for _, entry := range m.configCatalog.Entries {
		if !m.taskConfigIsLaunchable(entry.Alias) {
			continue
		}
		cfg := entry.Config
		if cfg == nil {
			loaded, err := entry.LoadConfig()
			if err != nil {
				continue
			}
			cfg = loaded
		}
		if inherited.Alias != "" && inherited.Path != "" && entry.Alias == inherited.Alias && entry.Path == inherited.Path {
			inheritedRepresented = true
		}
		options = append(options, followUpConfigOption{
			Selection: followUpConfigSelection{
				Alias: entry.Alias,
				Path:  entry.Path,
			},
			Config: cfg,
		})
	}
	if inherited.Alias != "" && inherited.Path != "" && !inheritedRepresented {
		options = append([]followUpConfigOption{{
			Selection: inherited,
			Config:    m.currentConfig,
		}}, options...)
	}
	return options
}

func followUpConfigSelectionEqual(left, right followUpConfigSelection) bool {
	return left.Alias == right.Alias && left.Path == right.Path && left.Inherited == right.Inherited
}

func (m Model) followUpConfigOptionIndex(selection followUpConfigSelection, options []followUpConfigOption) int {
	for i, option := range options {
		if followUpConfigSelectionEqual(selection, option.Selection) {
			return i
		}
	}
	for i, option := range options {
		if selection.Alias == option.Selection.Alias && selection.Path == option.Selection.Path {
			return i
		}
	}
	if !selection.Inherited {
		for i, option := range options {
			if selection.Alias == option.Selection.Alias {
				return i
			}
		}
	}
	return -1
}

func (m *Model) cycleFollowUpConfig(delta int) bool {
	options := m.followUpConfigOptions()
	if len(options) <= 1 || delta == 0 {
		return false
	}
	index := m.followUpConfigOptionIndex(m.selectedFollowUpConfigSelection(), options)
	if index < 0 {
		index = 0
	}
	step := 1
	if delta < 0 {
		step = -1
	}
	index = (index + step) % len(options)
	if index < 0 {
		index += len(options)
	}
	m.followUp.config = options[index].Selection
	return true
}

func (m Model) selectedFollowUpConfig() (*taskconfig.Config, string) {
	selection := m.selectedFollowUpConfigSelection()
	if selection.Inherited {
		return m.currentConfig, selection.Path
	}
	entry, ok := m.configCatalog.Entry(selection.Alias)
	if !ok {
		return nil, selection.Path
	}
	cfg := entry.Config
	if cfg == nil {
		loaded, err := entry.LoadConfig()
		if err == nil {
			cfg = loaded
		}
	}
	return cfg, firstNonEmpty(entry.Path, selection.Path)
}

func (m *Model) followUpLaunchConfigSelection() (followUpConfigSelection, error) {
	selection := m.selectedFollowUpConfigSelection()
	selection.Alias = strings.TrimSpace(selection.Alias)
	selection.Path = strings.TrimSpace(selection.Path)
	if selection.Alias == "" {
		return followUpConfigSelection{}, errors.New("follow-up task config alias is required")
	}
	if selection.Path == "" {
		return followUpConfigSelection{}, errors.New("follow-up task config path is required")
	}
	if selection.Inherited {
		return selection, nil
	}
	if summary, ok := m.taskConfigSummary(selection.Alias); ok && strings.TrimSpace(summary.LoadErr) != "" {
		return followUpConfigSelection{}, fmt.Errorf("task config %q is invalid: %s", selection.Alias, summary.LoadErr)
	}
	entry, ok, err := m.loadTaskConfigEntry(selection.Alias)
	if err != nil {
		return followUpConfigSelection{}, fmt.Errorf("task config %q is invalid: %w", selection.Alias, err)
	}
	if !ok {
		return followUpConfigSelection{}, fmt.Errorf("task config %q is no longer available", selection.Alias)
	}
	return followUpConfigSelection{
		Alias: entry.Alias,
		Path:  entry.Path,
	}, nil
}

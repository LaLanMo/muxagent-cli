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
	cfg := m.selectedTaskConfig()
	if cfg == nil {
		return ""
	}
	runtime, err := taskconfig.ResolveRuntime(cfg)
	if err != nil {
		return ""
	}
	return runtime
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

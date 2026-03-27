package tasktui

import (
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
	defaultPath, err := taskconfig.DefaultConfigPath()
	if err != nil {
		defaultPath = ""
	}
	cfg, err := taskconfig.LoadDefault()
	if err != nil {
		cfg = &taskconfig.Config{}
	}
	return &taskconfig.Catalog{
		DefaultAlias: taskconfig.DefaultAlias,
		Entries: []taskconfig.CatalogEntry{{
			Alias:  taskconfig.DefaultAlias,
			Path:   defaultPath,
			Config: cfg,
		}},
	}
}

func (m Model) selectedTaskConfigEntry() taskconfig.CatalogEntry {
	if entry, ok := m.configCatalog.Entry(m.selectedConfigAlias); ok {
		return entry
	}
	entry, err := m.configCatalog.DefaultEntry()
	if err == nil {
		return entry
	}
	defaultPath, pathErr := taskconfig.DefaultConfigPath()
	if pathErr != nil {
		defaultPath = ""
	}
	return taskconfig.CatalogEntry{
		Alias:  taskconfig.DefaultAlias,
		Path:   defaultPath,
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
	index := 0
	for i, entry := range entries {
		if entry.Alias == m.selectedConfigAlias {
			index = i
			break
		}
	}
	index = (index + delta) % len(entries)
	if index < 0 {
		index += len(entries)
	}
	next := entries[index].Alias
	if next == m.selectedConfigAlias {
		return false
	}
	m.selectedConfigAlias = next
	return true
}

func (m Model) effectiveLaunchRuntime() appconfig.RuntimeID {
	if m.launchRuntimeOverride != "" {
		runtime, err := taskconfig.ResolveRuntime(m.launchRuntimeOverride, nil)
		if err != nil {
			return ""
		}
		return runtime
	}
	cfg := m.selectedTaskConfig()
	if cfg == nil {
		return ""
	}
	runtime, err := taskconfig.ResolveRuntime("", cfg)
	if err != nil {
		return ""
	}
	return runtime
}

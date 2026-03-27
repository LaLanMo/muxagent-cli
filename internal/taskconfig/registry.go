package taskconfig

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/LaLanMo/muxagent-cli/internal/privdir"
)

const builtinDefaultAlias = "default"
const managedConfigFile = "config.yaml"
const managedDefaultBundleDir = builtinDefaultAlias

const DefaultAlias = builtinDefaultAlias

type Registry struct {
	DefaultAlias string          `json:"default_alias,omitempty"`
	Configs      []RegistryEntry `json:"configs,omitempty"`
}

type RegistryEntry struct {
	Alias string `json:"alias"`
	Path  string `json:"path"`
}

type Catalog struct {
	DefaultAlias string
	Entries      []CatalogEntry
}

type CatalogEntry struct {
	Alias  string
	Path   string
	Config *Config
}

func TaskConfigDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".muxagent", "taskconfigs"), nil
}

func RegistryPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".muxagent", "taskconfig.json"), nil
}

func DefaultConfigPath() (string, error) {
	defaultBundlePath, err := DefaultBundlePath()
	if err != nil {
		return "", err
	}
	return filepath.Join(defaultBundlePath, managedConfigFile), nil
}

func DefaultBundlePath() (string, error) {
	taskConfigDir, err := TaskConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(taskConfigDir, managedDefaultBundleDir), nil
}

func LoadRegistry() (Registry, error) {
	path, err := RegistryPath()
	if err != nil {
		return Registry{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Registry{}, nil
		}
		return Registry{}, err
	}
	var reg Registry
	if err := json.Unmarshal(data, &reg); err != nil {
		return Registry{}, err
	}
	return normalizeRegistry(reg)
}

func SaveRegistry(reg Registry) (string, error) {
	normalized, err := normalizeRegistry(reg)
	if err != nil {
		return "", err
	}
	path, err := RegistryPath()
	if err != nil {
		return "", err
	}
	if err := privdir.Ensure(filepath.Dir(path)); err != nil {
		return "", err
	}
	payload, err := json.MarshalIndent(normalized, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		return "", err
	}
	return path, nil
}

func LoadCatalog() (*Catalog, error) {
	if _, err := EnsureManagedDefaultAssets(); err != nil {
		return nil, err
	}
	if err := EnsureExplicitDefaultRegistryEntry(); err != nil {
		return nil, err
	}
	reg, err := LoadRegistry()
	if err != nil {
		return nil, err
	}
	entries := make([]CatalogEntry, 0, len(reg.Configs))
	taskConfigDir, err := TaskConfigDir()
	if err != nil {
		return nil, err
	}
	if entry, ok := registryEntryByAlias(reg.Configs, builtinDefaultAlias); ok {
		_, fullPath, err := resolveRegistryEntryPath(taskConfigDir, entry.Path)
		if err != nil {
			return nil, fmt.Errorf("registry entry %q: %w", entry.Alias, err)
		}
		entries = append(entries, CatalogEntry{
			Alias: entry.Alias,
			Path:  fullPath,
		})
	}
	for _, entry := range reg.Configs {
		if entry.Alias == builtinDefaultAlias {
			continue
		}
		_, fullPath, err := resolveRegistryEntryPath(taskConfigDir, entry.Path)
		if err != nil {
			return nil, fmt.Errorf("registry entry %q: %w", entry.Alias, err)
		}
		entries = append(entries, CatalogEntry{
			Alias: entry.Alias,
			Path:  fullPath,
		})
	}
	defaultAlias := firstNonEmptyAlias(reg.DefaultAlias, builtinDefaultAlias)
	catalog := &Catalog{
		DefaultAlias: defaultAlias,
		Entries:      entries,
	}
	if _, ok := catalog.Entry(defaultAlias); !ok {
		catalog.DefaultAlias = builtinDefaultAlias
	}
	return catalog, nil
}

func (c *Catalog) Entry(alias string) (CatalogEntry, bool) {
	if c == nil {
		return CatalogEntry{}, false
	}
	for _, entry := range c.Entries {
		if entry.Alias == alias {
			return entry, true
		}
	}
	return CatalogEntry{}, false
}

func (c *Catalog) DefaultEntry() (CatalogEntry, error) {
	entry, ok := c.Entry(firstNonEmptyAlias(c.DefaultAlias, builtinDefaultAlias))
	if !ok {
		entry, ok = c.Entry(builtinDefaultAlias)
	}
	if !ok {
		return CatalogEntry{}, fmt.Errorf("default task config alias %q does not exist", firstNonEmptyAlias(c.DefaultAlias, builtinDefaultAlias))
	}
	return entry, nil
}

func (e CatalogEntry) LoadConfig() (*Config, error) {
	if e.Config != nil {
		return e.Config, nil
	}
	path := strings.TrimSpace(e.Path)
	if path == "" {
		return nil, errors.New("task config path is required")
	}
	return Load(path)
}

func normalizeRegistry(reg Registry) (Registry, error) {
	result := Registry{
		DefaultAlias: strings.TrimSpace(reg.DefaultAlias),
		Configs:      make([]RegistryEntry, 0, len(reg.Configs)),
	}
	seen := map[string]struct{}{}
	for _, entry := range reg.Configs {
		alias := strings.TrimSpace(entry.Alias)
		if alias == "" {
			return Registry{}, errors.New("task config alias is required")
		}
		if _, exists := seen[alias]; exists {
			return Registry{}, fmt.Errorf("duplicate task config alias %q", alias)
		}
		seen[alias] = struct{}{}
		path, err := normalizeRegistryBundlePath(entry.Path)
		if err != nil {
			return Registry{}, fmt.Errorf("task config %q: %w", alias, err)
		}
		result.Configs = append(result.Configs, RegistryEntry{
			Alias: alias,
			Path:  path,
		})
	}
	sort.Slice(result.Configs, func(i, j int) bool {
		return result.Configs[i].Alias < result.Configs[j].Alias
	})
	if result.DefaultAlias == "" {
		result.DefaultAlias = builtinDefaultAlias
	}
	if _, exists := seen[result.DefaultAlias]; !exists {
		result.DefaultAlias = builtinDefaultAlias
	}
	return result, nil
}

func resolveRegistryEntryPath(taskConfigDir, relPath string) (string, string, error) {
	clean, err := normalizeRegistryBundlePath(relPath)
	if err != nil {
		return "", "", err
	}
	fullPath := filepath.Join(taskConfigDir, clean)
	rel, err := filepath.Rel(taskConfigDir, fullPath)
	if err != nil {
		return "", "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", "", errors.New("path must stay within taskconfigs directory")
	}
	return clean, filepath.Join(fullPath, managedConfigFile), nil
}

func normalizeRegistryBundlePath(rawPath string) (string, error) {
	clean := filepath.Clean(strings.TrimSpace(rawPath))
	if clean == "." || clean == "" {
		return "", errors.New("path is required")
	}
	if filepath.IsAbs(clean) {
		return "", errors.New("path must be relative")
	}
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", errors.New("path must stay within taskconfigs directory")
	}
	switch strings.ToLower(filepath.Ext(clean)) {
	case ".yaml", ".yml":
		return "", errors.New("path must point to a bundle directory")
	}
	if clean == "." || clean == "" {
		return "", errors.New("path is required")
	}
	return filepath.ToSlash(clean), nil
}

func EnsureManagedDefaultAssets() (string, error) {
	taskConfigDir, err := TaskConfigDir()
	if err != nil {
		return "", err
	}
	if err := privdir.Ensure(taskConfigDir); err != nil {
		return "", err
	}
	defaultBundlePath := filepath.Join(taskConfigDir, managedDefaultBundleDir)
	defaultConfigPath := filepath.Join(defaultBundlePath, managedConfigFile)
	if err := ensureManagedAssetFile(defaultConfigPath, defaultConfigAsset); err != nil {
		return "", err
	}
	if err := fs.WalkDir(defaultsFS, defaultPromptsDir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		relPath, err := filepath.Rel(defaultPromptsDir, path)
		if err != nil {
			return err
		}
		destPath := filepath.Join(defaultBundlePath, "prompts", relPath)
		return ensureManagedAssetFile(destPath, path)
	}); err != nil {
		return "", err
	}
	return defaultConfigPath, nil
}

func EnsureExplicitDefaultRegistryEntry() error {
	regPath, err := RegistryPath()
	if err != nil {
		return err
	}
	reg, err := LoadRegistry()
	if err != nil {
		return err
	}
	if reg.DefaultAlias == "" {
		reg.DefaultAlias = builtinDefaultAlias
	}
	if _, ok := registryEntryByAlias(reg.Configs, builtinDefaultAlias); !ok {
		reg.Configs = append(reg.Configs, RegistryEntry{
			Alias: builtinDefaultAlias,
			Path:  managedDefaultBundleDir,
		})
	}
	if _, err := os.Stat(regPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	_, err = SaveRegistry(reg)
	return err
}

func registryEntryByAlias(entries []RegistryEntry, alias string) (RegistryEntry, bool) {
	for _, entry := range entries {
		if entry.Alias == alias {
			return entry, true
		}
	}
	return RegistryEntry{}, false
}

func ensureManagedAssetFile(destPath, assetPath string) error {
	if _, err := os.Stat(destPath); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return err
	}
	data, err := defaultsFS.ReadFile(assetPath)
	if err != nil {
		return err
	}
	return os.WriteFile(destPath, data, 0o644)
}

func firstNonEmptyAlias(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

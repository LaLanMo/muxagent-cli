package taskconfig

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

func CloneConfig(alias, sourceConfigPath string) (*Catalog, error) {
	alias = strings.TrimSpace(alias)
	sourceConfigPath = strings.TrimSpace(sourceConfigPath)
	if alias == "" {
		return nil, errors.New("task config alias is required")
	}
	if sourceConfigPath == "" {
		return nil, errors.New("source task config path is required")
	}
	if err := validateManagedConfigPath(sourceConfigPath); err != nil {
		return nil, err
	}

	reg, err := LoadRegistry()
	if err != nil {
		return nil, err
	}
	if _, ok := registryEntryByAlias(reg.Configs, alias); ok {
		return nil, fmt.Errorf("task config alias %q already exists", alias)
	}

	taskConfigDir, err := TaskConfigDir()
	if err != nil {
		return nil, err
	}
	bundlePath := nextAvailableBundlePath(reg, alias)
	destDir := filepath.Join(taskConfigDir, filepath.FromSlash(bundlePath))
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.RemoveAll(destDir)
		}
	}()
	if err := copyDir(filepath.Dir(sourceConfigPath), destDir); err != nil {
		return nil, err
	}

	reg.Configs = append(reg.Configs, RegistryEntry{
		Alias: alias,
		Path:  bundlePath,
	})
	if _, err := SaveRegistry(reg); err != nil {
		return nil, err
	}
	cleanup = false
	return LoadCatalog()
}

func RenameConfigAlias(currentAlias, nextAlias string) (*Catalog, error) {
	currentAlias = strings.TrimSpace(currentAlias)
	nextAlias = strings.TrimSpace(nextAlias)
	if currentAlias == "" {
		return nil, errors.New("current task config alias is required")
	}
	if nextAlias == "" {
		return nil, errors.New("new task config alias is required")
	}
	if currentAlias == nextAlias {
		return LoadCatalog()
	}

	reg, err := LoadRegistry()
	if err != nil {
		return nil, err
	}
	index := registryEntryIndex(reg.Configs, currentAlias)
	if index < 0 {
		return nil, fmt.Errorf("task config alias %q does not exist", currentAlias)
	}
	if _, ok := registryEntryByAlias(reg.Configs, nextAlias); ok {
		return nil, fmt.Errorf("task config alias %q already exists", nextAlias)
	}
	entry := reg.Configs[index]
	if isBuiltinEntry(entry) {
		return nil, fmt.Errorf("task config alias %q cannot be renamed", currentAlias)
	}

	rollback := func() {}
	if entry.Path == managedDefaultBundleDir {
		taskConfigDir, err := TaskConfigDir()
		if err != nil {
			return nil, err
		}
		oldDir := filepath.Join(taskConfigDir, filepath.FromSlash(entry.Path))
		nextPath := nextAvailableBundlePath(registryWithoutIndex(reg, index), nextAlias)
		newDir := filepath.Join(taskConfigDir, filepath.FromSlash(nextPath))
		if err := os.Rename(oldDir, newDir); err != nil {
			return nil, err
		}
		rollback = func() {
			_ = os.Rename(newDir, oldDir)
		}
		reg.Configs[index].Path = nextPath
	}
	reg.Configs[index].Alias = nextAlias
	if reg.DefaultAlias == currentAlias {
		reg.DefaultAlias = nextAlias
	}
	if _, err := SaveRegistry(reg); err != nil {
		rollback()
		return nil, err
	}
	return LoadCatalog()
}

func SetDefaultConfig(alias string) (*Catalog, error) {
	alias = strings.TrimSpace(alias)
	if alias == "" {
		return nil, errors.New("task config alias is required")
	}
	reg, err := LoadRegistry()
	if err != nil {
		return nil, err
	}
	if _, ok := registryEntryByAlias(reg.Configs, alias); !ok {
		return nil, fmt.Errorf("task config alias %q does not exist", alias)
	}
	reg.DefaultAlias = alias
	if _, err := SaveRegistry(reg); err != nil {
		return nil, err
	}
	return LoadCatalog()
}

func DeleteConfig(alias string) (*Catalog, error) {
	alias = strings.TrimSpace(alias)
	if alias == "" {
		return nil, errors.New("task config alias is required")
	}

	reg, err := LoadRegistry()
	if err != nil {
		return nil, err
	}
	index := registryEntryIndex(reg.Configs, alias)
	if index < 0 {
		return nil, fmt.Errorf("task config alias %q does not exist", alias)
	}
	entry := reg.Configs[index]
	if isBuiltinEntry(entry) {
		return nil, fmt.Errorf("task config alias %q cannot be deleted", alias)
	}
	reg.Configs = append(reg.Configs[:index], reg.Configs[index+1:]...)
	if reg.DefaultAlias == alias {
		reg.DefaultAlias = DefaultAlias
	}
	if _, err := SaveRegistry(reg); err != nil {
		return nil, err
	}

	taskConfigDir, err := TaskConfigDir()
	if err != nil {
		return nil, err
	}
	// Best-effort cleanup. The registry update is the critical step; an orphaned
	// bundle is less damaging than a registry entry pointing at a missing path.
	_ = os.RemoveAll(filepath.Join(taskConfigDir, filepath.FromSlash(entry.Path)))
	return LoadCatalog()
}

func BundlePathForConfigPath(configPath string) (string, error) {
	configPath = strings.TrimSpace(configPath)
	if configPath == "" {
		return "", errors.New("task config path is required")
	}
	taskConfigDir, err := TaskConfigDir()
	if err != nil {
		return "", err
	}
	bundleDir := filepath.Dir(configPath)
	rel, err := filepath.Rel(taskConfigDir, bundleDir)
	if err != nil {
		return "", err
	}
	if rel == "." || rel == "" {
		return "", errors.New("task config path must be inside a bundle directory")
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errors.New("task config path must stay within taskconfigs directory")
	}
	return filepath.ToSlash(rel), nil
}

func registryEntryIndex(entries []RegistryEntry, alias string) int {
	for i, entry := range entries {
		if entry.Alias == alias {
			return i
		}
	}
	return -1
}

func nextAvailableBundlePath(reg Registry, alias string) string {
	base := slugifyBundlePath(alias)
	if base == "" {
		base = "config"
	}
	seen := map[string]struct{}{}
	for _, entry := range reg.Configs {
		seen[canonicalRegistryBundlePathKey(entry.Path)] = struct{}{}
	}
	candidate := base
	for suffix := 2; ; suffix++ {
		if _, exists := seen[canonicalRegistryBundlePathKey(candidate)]; !exists {
			return candidate
		}
		candidate = fmt.Sprintf("%s-%d", base, suffix)
	}
}

func nextAvailableAlias(reg Registry, base string) string {
	base = strings.TrimSpace(base)
	if base == "" {
		base = "config"
	}
	seen := map[string]struct{}{}
	for _, entry := range reg.Configs {
		seen[entry.Alias] = struct{}{}
	}
	candidate := base
	for suffix := 2; ; suffix++ {
		if _, exists := seen[candidate]; !exists {
			return candidate
		}
		candidate = fmt.Sprintf("%s-%d", base, suffix)
	}
}

func slugifyBundlePath(alias string) string {
	alias = strings.TrimSpace(strings.ToLower(alias))
	var builder strings.Builder
	lastDash := false
	for _, r := range alias {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			builder.WriteRune(r)
			lastDash = false
		case r == '-' || r == '_' || unicode.IsSpace(r):
			if builder.Len() == 0 || lastDash {
				continue
			}
			builder.WriteByte('-')
			lastDash = true
		}
	}
	result := strings.Trim(builder.String(), "-")
	if result == "" {
		return "config"
	}
	return result
}

func copyDir(sourceDir, destDir string) error {
	info, err := os.Stat(sourceDir)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("source path %q is not a directory", sourceDir)
	}
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return err
	}
	return filepath.Walk(sourceDir, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(sourceDir, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		target := filepath.Join(destDir, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
}

func validateManagedConfigPath(configPath string) error {
	if filepath.Base(configPath) != managedConfigFile {
		return fmt.Errorf("source task config path %q must point to %s", configPath, managedConfigFile)
	}
	info, err := os.Stat(configPath)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return fmt.Errorf("source task config path %q must be a file", configPath)
	}
	if _, err := BundlePathForConfigPath(configPath); err != nil {
		return fmt.Errorf("source task config path %q must be inside a managed taskconfig bundle", configPath)
	}
	return nil
}

func registryWithoutIndex(reg Registry, index int) Registry {
	if index < 0 || index >= len(reg.Configs) {
		return reg
	}
	clone := Registry{
		DefaultAlias: reg.DefaultAlias,
		Configs:      append([]RegistryEntry(nil), reg.Configs...),
	}
	clone.Configs = append(clone.Configs[:index], clone.Configs[index+1:]...)
	return clone
}

package taskconfig

import (
	"fmt"
	"io/fs"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

const (
	BuiltinIDDefault    = "default"
	BuiltinIDPlanOnly   = "plan-only"
	BuiltinIDAutonomous = "autonomous"
	BuiltinIDYolo       = "yolo"
)

type BuiltinDef struct {
	ID           string
	DisplayOrder int
}

var builtinDefs = []BuiltinDef{
	{ID: BuiltinIDDefault, DisplayOrder: 10},
	{ID: BuiltinIDPlanOnly, DisplayOrder: 20},
	{ID: BuiltinIDAutonomous, DisplayOrder: 30},
	{ID: BuiltinIDYolo, DisplayOrder: 40},
}

func BuiltinDefs() []BuiltinDef {
	result := make([]BuiltinDef, len(builtinDefs))
	copy(result, builtinDefs)
	return result
}

func IsBuiltinID(id string) bool {
	id = strings.TrimSpace(id)
	for _, def := range builtinDefs {
		if def.ID == id {
			return true
		}
	}
	return false
}

func builtinDisplayOrder(id string) int {
	id = strings.TrimSpace(id)
	for _, def := range builtinDefs {
		if def.ID == id {
			return def.DisplayOrder
		}
	}
	return 0
}

func isBuiltinEntry(entry RegistryEntry) bool {
	return IsBuiltinID(entry.BuiltinID)
}

func builtinConfigAsset(id string) string {
	return fmt.Sprintf("defaults/%s.yaml", id)
}

func builtinBundlePath(id string) string {
	if id == BuiltinIDDefault {
		return managedDefaultBundleDir
	}
	return "builtin/" + id
}

func loadEmbeddedBuiltinConfig(id string) (*Config, error) {
	if !IsBuiltinID(id) {
		return nil, fs.ErrNotExist
	}
	return loadEmbeddedConfig(builtinConfigAsset(id))
}

func builtinAssetFiles(id string) ([]string, error) {
	files := []string{builtinConfigAsset(id)}
	err := fs.WalkDir(defaultsFS, defaultPromptsDir, func(current string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		files = append(files, current)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	return files, nil
}

func builtinPromptDestPath(id, assetPath string) (string, error) {
	if assetPath == builtinConfigAsset(id) {
		return managedConfigFile, nil
	}
	prefix := strings.TrimSuffix(defaultPromptsDir, "/") + "/"
	if !strings.HasPrefix(assetPath, prefix) {
		return "", fs.ErrNotExist
	}
	return path.Join("prompts", strings.TrimPrefix(assetPath, prefix)), nil
}

func EmbeddedBuiltinCatalog() (*Catalog, error) {
	taskConfigDir, err := TaskConfigDir()
	if err != nil {
		return nil, err
	}
	catalog := &Catalog{
		DefaultAlias: DefaultAlias,
		Entries:      make([]CatalogEntry, 0, len(builtinDefs)),
	}
	for _, def := range builtinDefs {
		cfg, err := loadEmbeddedBuiltinConfig(def.ID)
		if err != nil {
			return nil, err
		}
		catalog.Entries = append(catalog.Entries, CatalogEntry{
			Alias:     def.ID,
			Path:      filepath.Join(taskConfigDir, builtinBundlePath(def.ID), managedConfigFile),
			Config:    cfg,
			BuiltinID: def.ID,
			Builtin:   true,
		})
	}
	return catalog, nil
}

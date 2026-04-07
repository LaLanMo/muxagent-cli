package appserver

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"sort"
	"strings"

	appconfig "github.com/LaLanMo/muxagent-cli/internal/config"
	"github.com/LaLanMo/muxagent-cli/internal/taskconfig"
	"gopkg.in/yaml.v3"
)

type configLookup struct {
	catalog  *taskconfig.Catalog
	registry taskconfig.Registry
	entry    taskconfig.CatalogEntry
}

func (s *Server) loadRuntimeConfig() (appconfig.Config, error) {
	if s.loadConfig == nil {
		return appconfig.Config{}, fmt.Errorf("runtime config loader is not configured")
	}
	return s.loadConfig()
}

func (s *Server) configLookup(alias string) (configLookup, *rpcError) {
	alias = strings.TrimSpace(alias)
	if alias == "" {
		return configLookup{}, &rpcError{Code: errorCodeInvalidParams, Message: "alias is required"}
	}
	catalog, err := s.loadCatalog()
	if err != nil {
		return configLookup{}, &rpcError{Code: errorCodeInternalError, Message: err.Error()}
	}
	registry, err := s.loadRegistry()
	if err != nil {
		return configLookup{}, &rpcError{Code: errorCodeInternalError, Message: err.Error()}
	}
	entry, ok := catalog.Entry(alias)
	if !ok {
		return configLookup{}, &rpcError{Code: errorCodeInvalidParams, Message: fmt.Sprintf("task config alias %q does not exist", alias)}
	}
	return configLookup{
		catalog:  catalog,
		registry: registry,
		entry:    entry,
	}, nil
}

func (s *Server) loadConfigDetail(alias string) (configDetailDTO, *rpcError) {
	lookup, rpcErr := s.configLookup(alias)
	if rpcErr != nil {
		return configDetailDTO{}, rpcErr
	}
	runtimeCfg, err := s.loadRuntimeConfig()
	if err != nil {
		return configDetailDTO{}, &rpcError{Code: errorCodeInternalError, Message: err.Error()}
	}
	return buildConfigDetailDTO(lookup.entry, lookup.catalog, lookup.registry, runtimeCfg), nil
}

func buildConfigDetailDTO(entry taskconfig.CatalogEntry, catalog *taskconfig.Catalog, reg taskconfig.Registry, runtimeCfg appconfig.Config) configDetailDTO {
	dto := configDetailDTO{
		Alias:      entry.Alias,
		ConfigPath: entry.Path,
		BuiltinID:  entry.BuiltinID,
		Builtin:    entry.Builtin,
	}
	if catalog != nil {
		dto.IsDefault = entry.Alias == catalog.DefaultAlias
	}
	for _, regEntry := range reg.Configs {
		if regEntry.Alias == entry.Alias {
			dto.BundlePath = regEntry.Path
			break
		}
	}
	if dto.BundlePath == "" {
		if bundlePath, err := taskconfig.BundlePathForConfigPath(entry.Path); err == nil {
			dto.BundlePath = bundlePath
		}
	}
	if revision, err := configRevision(entry.Path); err == nil {
		dto.Revision = revision
	}
	if explicit, err := configRuntimeExplicit(entry.Path); err == nil {
		dto.RuntimeExplicit = explicit
	}
	cfg, err := entry.LoadConfig()
	if err != nil {
		dto.LoadError = err.Error()
		dto.Launchable = false
		return dto
	}
	dto.Config = cfg
	dto.RuntimeID = cfg.Runtime
	dto.RuntimeName = runtimeDisplayName(cfg.Runtime)
	dto.RuntimeConfigured = runtimeConfigured(runtimeCfg, cfg.Runtime)
	dto.Description = cfg.Description
	for _, node := range cfg.Topology.Nodes {
		dto.NodeNames = append(dto.NodeNames, node.Name)
	}
	dto.Launchable = dto.RuntimeConfigured
	return dto
}

func validateConfigDraft(runtimeCfg appconfig.Config, cfg *taskconfig.Config) (*taskconfig.Config, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config is required")
	}
	normalized := *cfg
	if err := taskconfig.Validate(&normalized); err != nil {
		return nil, err
	}
	if !runtimeConfigured(runtimeCfg, normalized.Runtime) {
		return nil, fmt.Errorf("runtime %q is not configured", normalized.Runtime)
	}
	return &normalized, nil
}

func runtimeConfigured(cfg appconfig.Config, runtimeID appconfig.RuntimeID) bool {
	if strings.TrimSpace(string(runtimeID)) == "" {
		return false
	}
	_, ok := cfg.Runtimes[runtimeID]
	return ok
}

func configRuntimeExplicit(configPath string) (bool, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return false, err
	}
	var raw struct {
		Runtime *appconfig.RuntimeID `yaml:"runtime"`
	}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return false, err
	}
	return raw.Runtime != nil && strings.TrimSpace(string(*raw.Runtime)) != "", nil
}

func configRevision(configPath string) (string, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func runtimeListDTOs(cfg appconfig.Config) []runtimeEntryDTO {
	ids := cfg.ConfiguredRuntimeIDs()
	items := make([]runtimeEntryDTO, 0, len(ids))
	for _, id := range ids {
		settings := cfg.Runtimes[id]
		envKeys := make([]string, 0, len(settings.Env))
		for key := range settings.Env {
			envKeys = append(envKeys, key)
		}
		sort.Strings(envKeys)
		items = append(items, runtimeEntryDTO{
			RuntimeID:   id,
			RuntimeName: runtimeDisplayName(id),
			Command:     settings.Command,
			Args:        append([]string(nil), settings.Args...),
			CWD:         settings.CWD,
			EnvKeys:     envKeys,
		})
	}
	return items
}

func configConflictRPCError(alias, currentRevision string) *rpcError {
	return &rpcError{
		Code:    errorCodeConfigConflict,
		Message: "config revision mismatch",
		Data: map[string]any{
			"alias":            alias,
			"current_revision": currentRevision,
		},
	}
}

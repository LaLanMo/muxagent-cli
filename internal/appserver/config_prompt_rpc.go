package appserver

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/LaLanMo/muxagent-cli/internal/taskconfig"
)

type configPromptLookup struct {
	lookup       configLookup
	config       *taskconfig.Config
	nodeName     string
	definition   taskconfig.NodeDefinition
	promptPath   string
	resolvedPath string
}

func (s *Server) configPromptLookup(alias, nodeName string) (configPromptLookup, *rpcError) {
	lookup, rpcErr := s.configLookup(alias)
	if rpcErr != nil {
		return configPromptLookup{}, rpcErr
	}
	nodeName = strings.TrimSpace(nodeName)
	if nodeName == "" {
		return configPromptLookup{}, &rpcError{Code: errorCodeInvalidParams, Message: "node_name is required"}
	}
	cfg, err := lookup.entry.LoadConfig()
	if err != nil {
		return configPromptLookup{}, &rpcError{Code: errorCodeInternalError, Message: err.Error()}
	}
	definition, ok := cfg.NodeDefinitions[nodeName]
	if !ok {
		return configPromptLookup{}, &rpcError{Code: errorCodeInvalidParams, Message: fmt.Sprintf("node %q does not exist in config %q", nodeName, lookup.entry.Alias)}
	}
	if definition.Type != taskconfig.NodeTypeAgent {
		return configPromptLookup{}, &rpcError{Code: errorCodeInvalidParams, Message: fmt.Sprintf("node %q does not support prompt editing", nodeName)}
	}
	promptPath, resolvedPath, err := resolveConfigPromptPath(lookup.entry.Path, definition)
	if err != nil {
		return configPromptLookup{}, &rpcError{Code: errorCodeInvalidParams, Message: err.Error()}
	}
	return configPromptLookup{
		lookup:       lookup,
		config:       cfg,
		nodeName:     nodeName,
		definition:   definition,
		promptPath:   promptPath,
		resolvedPath: resolvedPath,
	}, nil
}

func (s *Server) loadConfigPrompt(alias, nodeName string) (configPromptDTO, *rpcError) {
	lookup, rpcErr := s.configPromptLookup(alias, nodeName)
	if rpcErr != nil {
		return configPromptDTO{}, rpcErr
	}
	return buildConfigPromptDTO(lookup)
}

func buildConfigPromptDTO(lookup configPromptLookup) (configPromptDTO, *rpcError) {
	content, err := os.ReadFile(lookup.resolvedPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return configPromptDTO{}, &rpcError{Code: errorCodeInternalError, Message: err.Error()}
	}
	dto := configPromptDTO{
		Alias:        lookup.lookup.entry.Alias,
		NodeName:     lookup.nodeName,
		NodeType:     string(lookup.definition.Type),
		Path:         lookup.promptPath,
		ResolvedPath: lookup.resolvedPath,
		Content:      string(content),
		ReadOnly:     false,
		Builtin:      lookup.lookup.entry.Builtin,
	}
	if len(content) > 0 || err == nil {
		dto.Revision = hashBytes(content)
	}
	return dto, nil
}

func resolveConfigPromptPath(configPath string, def taskconfig.NodeDefinition) (string, string, error) {
	raw := strings.TrimSpace(def.SystemPrompt)
	if raw == "" {
		return "", "", fmt.Errorf("node prompt path is not set")
	}
	resolved := filepath.Clean(taskconfig.ResolvePromptPath(configPath, def))
	bundleDir := filepath.Dir(configPath)
	rel, err := filepath.Rel(bundleDir, resolved)
	if err != nil {
		return "", "", err
	}
	if rel == "." || rel == "" {
		return "", "", fmt.Errorf("prompt path is invalid")
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", "", fmt.Errorf("prompt path must stay within the config bundle")
	}
	return filepath.ToSlash(rel), resolved, nil
}

func promptRevision(promptPath string) (string, error) {
	data, err := os.ReadFile(promptPath)
	if err != nil {
		return "", err
	}
	return hashBytes(data), nil
}

func hashBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func configPromptConflictRPCError(alias, nodeName, currentRevision string) *rpcError {
	return &rpcError{
		Code:    errorCodeConfigConflict,
		Message: "prompt revision mismatch",
		Data: map[string]any{
			"alias":            alias,
			"node_name":        nodeName,
			"current_revision": currentRevision,
		},
	}
}

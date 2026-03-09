package acpbin

import (
	"os"

	"github.com/LaLanMo/muxagent-cli/internal/config"
)

// Resolve determines the path to the ACP binary using this decision tree:
//  1. If user explicitly overrode the runtime command → return that
//  2. If claude-agent-acp exists next to the CLI binary → use it
//  3. If ~/.muxagent/bin/claude-agent-acp-{version} exists → use it
//  4. Otherwise → download, verify, extract → return managed path
//
// After a successful resolve, old versions are cleaned up.
func Resolve(cfg config.Config, progressFn func(ProgressEvent)) (string, error) {
	// Step 1: User explicitly overrode the command
	if config.IsRuntimeCommandOverridden(config.RuntimeClaudeCode) {
		settings := cfg.Runtimes[config.RuntimeClaudeCode]
		return settings.Command, nil
	}

	// Step 2: Binary next to CLI executable (side-by-side distribution)
	if rel, err := RelativePath(); err == nil {
		if info, err := os.Stat(rel); err == nil && info.Mode().IsRegular() {
			return rel, nil
		}
	}

	// Step 3: Already downloaded managed binary
	managed, err := ManagedPath()
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(managed); err == nil {
		go Cleanup() // clean old versions in background
		return managed, nil
	}

	// Step 4: Download
	path, err := Download(progressFn)
	if err != nil {
		return "", err
	}

	go Cleanup()
	return path, nil
}

package codexbin

import (
	"os"

	"github.com/LaLanMo/muxagent-cli/internal/config"
)

// Resolve determines the path to the codex-acp binary using this decision tree:
//  1. If user explicitly overrode the runtime command → return that
//  2. If codex-acp exists next to the CLI binary → use it
//  3. If ~/.muxagent/bin/codex-acp-{version} exists → use it
//  4. Otherwise → download, verify, extract → return managed path
func Resolve(cfg config.Config, progressFn func(ProgressEvent)) (string, error) {
	return resolve(cfg, progressFn, true)
}

// ResolveManaged skips the side-by-side lookup.
func ResolveManaged(cfg config.Config, progressFn func(ProgressEvent)) (string, error) {
	return resolve(cfg, progressFn, false)
}

func resolve(cfg config.Config, progressFn func(ProgressEvent), includeRelative bool) (string, error) {
	if config.IsRuntimeCommandOverridden(config.RuntimeCodex) {
		settings := cfg.Runtimes[config.RuntimeCodex]
		return settings.Command, nil
	}

	if includeRelative {
		if rel, err := RelativePath(); err == nil {
			if info, err := os.Stat(rel); err == nil && info.Mode().IsRegular() {
				return rel, nil
			}
		}
	}

	managed, err := ManagedPath()
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(managed); err == nil {
		go Cleanup()
		return managed, nil
	}

	path, err := Download(progressFn)
	if err != nil {
		return "", err
	}
	go Cleanup()
	return path, nil
}

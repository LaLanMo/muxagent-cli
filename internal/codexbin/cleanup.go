package codexbin

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Cleanup removes old versioned codex-acp binaries from ~/.muxagent/bin/,
// keeping only the current version.
func Cleanup() {
	dir, err := BinDir()
	if err != nil {
		return
	}

	platform, err := Platform()
	if err != nil {
		return
	}
	currentName := managedBinaryName(runtime.GOOS, ACPVersion, platform)

	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}

	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, "codex-acp-") && name != currentName {
			_ = os.Remove(filepath.Join(dir, name))
		}
		if strings.HasPrefix(name, ".tmp-") {
			_ = os.Remove(filepath.Join(dir, name))
		}
	}
}

package acpbin

import (
	"os"
	"path/filepath"
	"strings"
)

// Cleanup removes old versioned ACP binaries from ~/.muxagent/bin/,
// keeping only the current version.
func Cleanup() {
	dir, err := BinDir()
	if err != nil {
		return
	}

	currentName := "claude-agent-acp-" + ACPVersion

	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}

	for _, e := range entries {
		name := e.Name()
		// Remove old versioned binaries
		if strings.HasPrefix(name, "claude-agent-acp-") && name != currentName {
			_ = os.Remove(filepath.Join(dir, name))
		}
		// Remove leftover temp files from failed downloads
		if strings.HasPrefix(name, ".tmp-") {
			_ = os.Remove(filepath.Join(dir, name))
		}
	}
}

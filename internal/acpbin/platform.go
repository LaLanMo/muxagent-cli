package acpbin

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/LaLanMo/muxagent-cli/internal/privdir"
)

var currentExecutablePath = func() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(exe)
}

// Platform returns the ACP release platform string for the current OS/arch.
// Examples: "darwin-arm64", "linux-x64", "linux-x64-musl".
func Platform() (string, error) {
	arch, err := archName()
	if err != nil {
		return "", err
	}

	switch runtime.GOOS {
	case "darwin", "windows":
		return runtime.GOOS + "-" + arch, nil
	case "linux":
		plat := "linux-" + arch
		if isMusl() {
			plat += "-musl"
		}
		return plat, nil
	default:
		return "", fmt.Errorf("unsupported platform: %s/%s", runtime.GOOS, runtime.GOARCH)
	}
}

func archName() (string, error) {
	switch runtime.GOARCH {
	case "amd64":
		return "x64", nil
	case "arm64":
		return "arm64", nil
	default:
		return "", fmt.Errorf("unsupported platform: %s/%s", runtime.GOOS, runtime.GOARCH)
	}
}

// isMusl returns true if the system uses musl libc (Alpine, etc.).
func isMusl() bool {
	matches, _ := filepath.Glob("/lib/ld-musl-*")
	return len(matches) > 0
}

// ArchiveExt returns the archive extension for the current OS.
func ArchiveExt() string {
	if runtime.GOOS == "linux" {
		return ".tar.gz"
	}
	return ".zip"
}

// BinDir returns ~/.muxagent/bin/, creating it if needed.
func BinDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".muxagent", "bin")
	if err := privdir.EnsureWithin(dir, filepath.Join(home, ".muxagent")); err != nil {
		return "", err
	}
	return dir, nil
}

// ManagedPath returns the versioned binary path: ~/.muxagent/bin/claude-agent-acp-{version}
func ManagedPath() (string, error) {
	dir, err := BinDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, managedBinaryName(runtime.GOOS, ACPVersion)), nil
}

// RelativePath returns the path to claude-agent-acp next to the current executable.
func RelativePath() (string, error) {
	exe, err := currentExecutablePath()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(exe), binaryName(runtime.GOOS)), nil
}

// DownloadURL returns the GitHub release download URL for the given platform.
func DownloadURL(platform string) string {
	ext := ".zip"
	if len(platform) >= 5 && platform[:5] == "linux" {
		ext = ".tar.gz"
	}
	return fmt.Sprintf(
		"https://github.com/zed-industries/claude-agent-acp/releases/download/v%s/claude-agent-acp-%s%s",
		ACPVersion, platform, ext,
	)
}

func binaryName(goos string) string {
	name := "claude-agent-acp"
	if goos == "windows" {
		name += ".exe"
	}
	return name
}

func managedBinaryName(goos, version string) string {
	name := "claude-agent-acp-" + version
	if goos == "windows" {
		name += ".exe"
	}
	return name
}

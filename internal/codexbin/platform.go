package codexbin

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

// Platform returns the codex-acp release target triple for the current OS/arch.
func Platform() (string, error) {
	switch runtime.GOOS {
	case "darwin":
		switch runtime.GOARCH {
		case "arm64":
			return "aarch64-apple-darwin", nil
		case "amd64":
			return "x86_64-apple-darwin", nil
		}
	case "linux":
		abi := "gnu"
		if isMusl() {
			abi = "musl"
		}
		switch runtime.GOARCH {
		case "arm64":
			return "aarch64-unknown-linux-" + abi, nil
		case "amd64":
			return "x86_64-unknown-linux-" + abi, nil
		}
	case "windows":
		switch runtime.GOARCH {
		case "arm64":
			return "aarch64-pc-windows-msvc", nil
		case "amd64":
			return "x86_64-pc-windows-msvc", nil
		}
	}
	return "", fmt.Errorf("unsupported platform: %s/%s", runtime.GOOS, runtime.GOARCH)
}

func isMusl() bool {
	matches, _ := filepath.Glob("/lib/ld-musl-*")
	return len(matches) > 0
}

func ArchiveExt() string {
	if runtime.GOOS == "windows" {
		return ".zip"
	}
	return ".tar.gz"
}

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

// ManagedPath returns the versioned binary path: ~/.muxagent/bin/codex-acp-{version}
func ManagedPath() (string, error) {
	dir, err := BinDir()
	if err != nil {
		return "", err
	}
	platform, err := Platform()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, managedBinaryName(runtime.GOOS, ACPVersion, platform)), nil
}

// RelativePath returns the path to codex-acp next to the current executable.
func RelativePath() (string, error) {
	exe, err := currentExecutablePath()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(exe), binaryName(runtime.GOOS)), nil
}

func DownloadURL(platform string) string {
	return fmt.Sprintf(
		"https://github.com/zed-industries/codex-acp/releases/download/v%s/codex-acp-%s-%s%s",
		ACPVersion, ACPVersion, platform, ArchiveExt(),
	)
}

func binaryName(goos string) string {
	name := "codex-acp"
	if goos == "windows" {
		name += ".exe"
	}
	return name
}

func managedBinaryName(goos, version, platform string) string {
	name := "codex-acp-" + version + "-" + platform
	if goos == "windows" {
		name += ".exe"
	}
	return name
}

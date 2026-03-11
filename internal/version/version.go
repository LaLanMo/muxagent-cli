package version

import (
	"fmt"
	"runtime/debug"
)

const shortRevisionLength = 12

var readBuildInfo = debug.ReadBuildInfo

// Version is the CLI version. Set via ldflags at build time:
// go build -ldflags "-X github.com/LaLanMo/muxagent-cli/internal/version.Version=1.0.0"
var Version = "dev"

// Display returns the user-facing CLI version string.
func Display() string {
	if Version != "dev" {
		return Version
	}

	if revision := vcsRevision(); revision != "" {
		return fmt.Sprintf("dev (%s)", revision)
	}

	return Version
}

// CLIString returns the full user-facing CLI version line.
func CLIString() string {
	return "muxagent version " + Display()
}

func vcsRevision() string {
	info, ok := readBuildInfo()
	if !ok || info == nil {
		return ""
	}

	for _, setting := range info.Settings {
		if setting.Key != "vcs.revision" || setting.Value == "" {
			continue
		}
		if len(setting.Value) <= shortRevisionLength {
			return setting.Value
		}
		return setting.Value[:shortRevisionLength]
	}

	return ""
}

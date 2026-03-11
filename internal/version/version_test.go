package version

import (
	"runtime/debug"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDisplayReleaseVersion(t *testing.T) {
	originalVersion := Version
	originalReadBuildInfo := readBuildInfo
	t.Cleanup(func() {
		Version = originalVersion
		readBuildInfo = originalReadBuildInfo
	})

	Version = "v1.2.3"
	readBuildInfo = func() (*debug.BuildInfo, bool) {
		t.Fatal("release version should not read build info")
		return nil, false
	}

	assert.Equal(t, "v1.2.3", Display())
}

func TestDisplayDevVersionWithRevision(t *testing.T) {
	originalVersion := Version
	originalReadBuildInfo := readBuildInfo
	t.Cleanup(func() {
		Version = originalVersion
		readBuildInfo = originalReadBuildInfo
	})

	Version = "dev"
	readBuildInfo = func() (*debug.BuildInfo, bool) {
		return &debug.BuildInfo{
			Settings: []debug.BuildSetting{
				{Key: "vcs.revision", Value: "1234567890abcdef"},
			},
		}, true
	}

	assert.Equal(t, "dev (1234567890ab)", Display())
}

func TestDisplayDevVersionWithoutBuildInfo(t *testing.T) {
	originalVersion := Version
	originalReadBuildInfo := readBuildInfo
	t.Cleanup(func() {
		Version = originalVersion
		readBuildInfo = originalReadBuildInfo
	})

	Version = "dev"
	readBuildInfo = func() (*debug.BuildInfo, bool) {
		return nil, false
	}

	assert.Equal(t, "dev", Display())
}

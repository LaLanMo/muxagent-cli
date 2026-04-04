package codex

import (
	"testing"

	"github.com/LaLanMo/muxagent-cli/internal/taskexecutor/codexappserver"
	"github.com/LaLanMo/muxagent-cli/internal/taskexecutor/codexexec"
	"github.com/stretchr/testify/assert"
)

func TestNewWithModeDefaultsToExec(t *testing.T) {
	executor := NewWithMode("", "")
	_, ok := executor.(*codexappserver.Executor)
	assert.True(t, ok)
}

func TestNewWithModeUsesAppServerAliases(t *testing.T) {
	tests := []string{"appserver", "app-server", " AppServer "}
	for _, mode := range tests {
		t.Run(mode, func(t *testing.T) {
			executor := NewWithMode("", mode)
			_, ok := executor.(*codexappserver.Executor)
			assert.True(t, ok)
		})
	}
}

func TestNewWithModeFallsBackToExecOnUnknownValue(t *testing.T) {
	executor := NewWithMode("", "mystery")
	_, ok := executor.(*codexappserver.Executor)
	assert.True(t, ok)
}

func TestNewWithModeSupportsExplicitExec(t *testing.T) {
	executor := NewWithMode("", "exec")
	_, ok := executor.(*codexexec.Executor)
	assert.True(t, ok)
}

package codex

import (
	"os"
	"strings"

	"github.com/LaLanMo/muxagent-cli/internal/taskexecutor"
	"github.com/LaLanMo/muxagent-cli/internal/taskexecutor/codexappserver"
	"github.com/LaLanMo/muxagent-cli/internal/taskexecutor/codexexec"
)

const (
	EnvExecutorMode = "MUXAGENT_CODEX_EXECUTOR"

	ModeAppServer = "appserver"
	ModeExec      = "exec"
)

func New(binaryPath string) taskexecutor.Executor {
	return NewWithMode(binaryPath, os.Getenv(EnvExecutorMode))
}

func NewWithMode(binaryPath, mode string) taskexecutor.Executor {
	switch normalizeMode(mode) {
	case ModeAppServer:
		return codexappserver.New(binaryPath)
	default:
		return codexexec.New(binaryPath)
	}
}

func normalizeMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "", ModeAppServer, "app-server":
		return ModeAppServer
	case ModeExec:
		return ModeExec
	default:
		return ModeAppServer
	}
}

package acpbin

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/LaLanMo/muxagent-cli/internal/config"
)

const claudeCodeExecutableEnv = "CLAUDE_CODE_EXECUTABLE"

// InjectClaudeCodeExecutable ensures Claude ACP runtimes spawn their internal
// Claude CLI through a wrapper that adds the required --cli flag.
// Callers are expected to invoke this only for the Claude Code runtime after
// they have resolved the runtime command.
func InjectClaudeCodeExecutable(settings config.RuntimeSettings) (config.RuntimeSettings, error) {
	if !needsClaudeCodeExecutableWrapper(settings) {
		return settings, nil
	}

	wrapperPath, err := ensureClaudeCodeExecutableWrapper(settings.Command)
	if err != nil {
		return config.RuntimeSettings{}, err
	}

	settings.Env = cloneEnv(settings.Env)
	settings.Env[claudeCodeExecutableEnv] = wrapperPath
	return settings, nil
}

func needsClaudeCodeExecutableWrapper(settings config.RuntimeSettings) bool {
	if strings.TrimSpace(settings.Command) == "" {
		return false
	}
	return strings.TrimSpace(settings.Env[claudeCodeExecutableEnv]) == ""
}

func ensureClaudeCodeExecutableWrapper(command string) (string, error) {
	dir, err := BinDir()
	if err != nil {
		return "", err
	}

	name := claudeCodeWrapperName(command)
	path := filepath.Join(dir, name)
	content := claudeCodeWrapperContent(command)

	if existing, err := os.ReadFile(path); err == nil && string(existing) == content {
		return path, nil
	}

	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		return "", err
	}
	return path, nil
}

func claudeCodeWrapperName(command string) string {
	sum := sha256.Sum256([]byte(command))
	suffix := ".sh"
	if runtime.GOOS == "windows" {
		suffix = ".cmd"
	}
	return "claude-code-exec-" + hex.EncodeToString(sum[:6]) + suffix
}

func claudeCodeWrapperContent(command string) string {
	if runtime.GOOS == "windows" {
		return "@echo off\r\n\"" + strings.ReplaceAll(command, "\"", "\"\"") + "\" --cli %*\r\n"
	}
	return "#!/bin/sh\nexec " + shellQuote(command) + " --cli \"$@\"\n"
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}

func cloneEnv(env map[string]string) map[string]string {
	if len(env) == 0 {
		return map[string]string{}
	}
	cloned := make(map[string]string, len(env))
	for k, v := range env {
		cloned[k] = v
	}
	return cloned
}

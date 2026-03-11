package acpbin

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/LaLanMo/muxagent-cli/internal/config"
)

func TestInjectClaudeCodeExecutable(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	tests := []struct {
		name   string
		input  config.RuntimeSettings
		verify func(t *testing.T, got config.RuntimeSettings, err error)
	}{
		{
			name: "injects wrapper for claude agent acp",
			input: config.RuntimeSettings{
				Command: "/tmp/claude-agent-acp",
				Env:     map[string]string{"CLAUDECODE": ""},
			},
			verify: func(t *testing.T, got config.RuntimeSettings, err error) {
				if err != nil {
					t.Fatalf("InjectClaudeCodeExecutable() error = %v", err)
				}
				wrapperPath := got.Env[claudeCodeExecutableEnv]
				if wrapperPath == "" {
					t.Fatalf("expected %s to be set", claudeCodeExecutableEnv)
				}
				data, readErr := os.ReadFile(wrapperPath)
				if readErr != nil {
					t.Fatalf("read wrapper: %v", readErr)
				}
				content := string(data)
				if !strings.Contains(content, "--cli") {
					t.Fatalf("wrapper missing --cli: %q", content)
				}
				if !strings.Contains(content, "claude-agent-acp") {
					t.Fatalf("wrapper missing target command: %q", content)
				}
				if got.Env["CLAUDECODE"] != "" {
					t.Fatalf("expected CLAUDECODE env to be preserved")
				}
				if runtime.GOOS == "windows" {
					if filepath.Ext(wrapperPath) != ".cmd" {
						t.Fatalf("expected .cmd wrapper, got %q", wrapperPath)
					}
				} else if filepath.Ext(wrapperPath) != ".sh" {
					t.Fatalf("expected .sh wrapper, got %q", wrapperPath)
				}
			},
		},
		{
			name: "respects existing executable override",
			input: config.RuntimeSettings{
				Command: "/tmp/claude-agent-acp",
				Env: map[string]string{
					claudeCodeExecutableEnv: "/custom/wrapper",
				},
			},
			verify: func(t *testing.T, got config.RuntimeSettings, err error) {
				if err != nil {
					t.Fatalf("InjectClaudeCodeExecutable() error = %v", err)
				}
				if got.Env[claudeCodeExecutableEnv] != "/custom/wrapper" {
					t.Fatalf("expected existing wrapper override to remain unchanged, got %q", got.Env[claudeCodeExecutableEnv])
				}
			},
		},
		{
			name: "skips unrelated commands",
			input: config.RuntimeSettings{
				Command: "/usr/local/bin/opencode",
			},
			verify: func(t *testing.T, got config.RuntimeSettings, err error) {
				if err != nil {
					t.Fatalf("InjectClaudeCodeExecutable() error = %v", err)
				}
				if got.Env != nil && got.Env[claudeCodeExecutableEnv] != "" {
					t.Fatalf("expected no wrapper injection, got %q", got.Env[claudeCodeExecutableEnv])
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := InjectClaudeCodeExecutable(tt.input)
			tt.verify(t, got, err)
		})
	}
}

func TestInjectClaudeCodeExecutable_StableWrapperPath(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	settings := config.RuntimeSettings{Command: "/tmp/claude-agent-acp"}
	first, err := InjectClaudeCodeExecutable(settings)
	if err != nil {
		t.Fatalf("first InjectClaudeCodeExecutable() error = %v", err)
	}
	second, err := InjectClaudeCodeExecutable(settings)
	if err != nil {
		t.Fatalf("second InjectClaudeCodeExecutable() error = %v", err)
	}

	if first.Env[claudeCodeExecutableEnv] != second.Env[claudeCodeExecutableEnv] {
		t.Fatalf("expected stable wrapper path, got %q and %q", first.Env[claudeCodeExecutableEnv], second.Env[claudeCodeExecutableEnv])
	}
}

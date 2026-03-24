package main

import (
	"bytes"
	"context"
	"errors"
	"testing"

	cliversion "github.com/LaLanMo/muxagent-cli/internal/version"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRootVersionFlag(t *testing.T) {
	cmd := NewRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"--version"})

	err := cmd.Execute()
	require.NoError(t, err)
	assert.Equal(t, cliversion.CLIString()+"\n", out.String())
}

func TestRootVersionShorthandFlag(t *testing.T) {
	cmd := NewRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"-v"})

	err := cmd.Execute()
	require.NoError(t, err)
	assert.Equal(t, cliversion.CLIString()+"\n", out.String())
}

func TestVersionCommand(t *testing.T) {
	cmd := NewRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"version"})

	err := cmd.Execute()
	require.NoError(t, err)
	assert.Equal(t, cliversion.CLIString()+"\n", out.String())
}

func TestVersionCommandRejectsExtraArgs(t *testing.T) {
	cmd := NewRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"version", "extra"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown command \"extra\" for \"muxagent version\"")
}

func TestRootHelpIncludesVersionEntryPoints(t *testing.T) {
	cmd := NewRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"--help"})

	err := cmd.Execute()
	require.NoError(t, err)
	assert.Contains(t, out.String(), "version     Show muxagent version")
	assert.Contains(t, out.String(), "-v, --version")
}

func TestRootHelpOmitsCompletionCommand(t *testing.T) {
	cmd := NewRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"--help"})

	err := cmd.Execute()
	require.NoError(t, err)
	assert.NotContains(t, out.String(), "completion")
	assert.Contains(t, out.String(), "auth")
	assert.Contains(t, out.String(), "daemon")
	assert.Contains(t, out.String(), "health")
}

func TestCompletionCommandUnavailable(t *testing.T) {
	cmd := NewRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"completion"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown command \"completion\" for \"muxagent\"")
}

func TestRootLaunchesTaskTUIOnBareInvocation(t *testing.T) {
	var (
		called     bool
		gotWorkDir string
		gotConfig  string
	)
	cmd := newRootCmd(rootOptions{
		launchTUI: func(ctx context.Context, workDir, configPath string) error {
			called = true
			gotWorkDir = workDir
			gotConfig = configPath
			return nil
		},
	})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(nil)

	err := cmd.Execute()
	require.NoError(t, err)
	require.True(t, called)
	assert.NotEmpty(t, gotWorkDir)
	assert.Empty(t, gotConfig)
}

func TestRootPassesConfigOverrideToTaskTUI(t *testing.T) {
	var gotConfig string
	cmd := newRootCmd(rootOptions{
		launchTUI: func(ctx context.Context, workDir, configPath string) error {
			gotConfig = configPath
			return nil
		},
	})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"-c", "./my-config.yaml"})

	err := cmd.Execute()
	require.NoError(t, err)
	assert.Equal(t, "./my-config.yaml", gotConfig)
}

func TestRootPropagatesTaskTUILaunchError(t *testing.T) {
	cmd := newRootCmd(rootOptions{
		launchTUI: func(ctx context.Context, workDir, configPath string) error {
			return errors.New("boom")
		},
	})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(nil)

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "boom")
}

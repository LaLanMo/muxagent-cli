package appserver

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/LaLanMo/muxagent-cli/internal/taskstore"
	"github.com/stretchr/testify/require"
)

func TestResolveAppServerWorkDirRequiresAbsolutePath(t *testing.T) {
	prevWD, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(t.TempDir()))
	t.Cleanup(func() {
		require.NoError(t, os.Chdir(prevWD))
	})

	_, err = resolveAppServerWorkDir("workspace")
	require.ErrorContains(t, err, "workdir must be an absolute path")
}

func TestResolveAppServerWorkDirRejectsNonDirectory(t *testing.T) {
	filePath := filepath.Join(t.TempDir(), "workspace.txt")
	require.NoError(t, os.WriteFile(filePath, []byte("not a dir"), 0o644))

	_, err := resolveAppServerWorkDir(filePath)
	require.ErrorContains(t, err, "workdir is not a directory")
}

func TestResolveAppServerWorkDirNormalizesExistingDirectory(t *testing.T) {
	workDir := t.TempDir()

	got, err := resolveAppServerWorkDir(workDir)
	require.NoError(t, err)
	require.Equal(t, taskstore.NormalizeWorkDir(workDir), got)
}

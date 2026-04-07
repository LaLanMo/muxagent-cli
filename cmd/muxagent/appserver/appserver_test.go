package appserver

import (
	"bytes"
	"io"
	"path/filepath"
	"testing"
)

func TestNewCmdDoesNotExposeLegacyWorkDirFlag(t *testing.T) {
	cmd := NewCmd()
	if flag := cmd.Flags().Lookup("workdir"); flag != nil {
		t.Fatalf("workdir flag unexpectedly present")
	}
}

func TestNewCmdKeepsHiddenStateDirFlag(t *testing.T) {
	cmd := NewCmd()
	flag := cmd.PersistentFlags().Lookup("state-dir")
	if flag == nil {
		t.Fatalf("state-dir flag missing")
	}
	if !flag.Hidden {
		t.Fatalf("state-dir flag should stay hidden")
	}
}

func TestNewCmdRunsV2WithStateDirAndEOF(t *testing.T) {
	cmd := NewCmd()
	cmd.SetArgs([]string{"--state-dir", filepath.Join(t.TempDir(), "appserver")})
	cmd.SetIn(bytes.NewReader(nil))
	cmd.SetOut(io.Discard)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
}

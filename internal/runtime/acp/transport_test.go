package acp

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestBuildEnv_NoOverrides(t *testing.T) {
	base := []string{"HOME=/home/user", "PATH=/usr/bin"}
	result := buildEnv(base, nil)

	if !slices.Equal(result, base) {
		t.Errorf("got %v, want %v", result, base)
	}
}

func TestBuildEnv_SetOnly(t *testing.T) {
	base := []string{"HOME=/home/user"}
	result := buildEnv(base, map[string]string{"FOO": "bar"})

	if len(result) != 2 {
		t.Fatalf("len = %d, want 2", len(result))
	}
	if result[0] != "HOME=/home/user" {
		t.Errorf("result[0] = %q, want HOME=/home/user", result[0])
	}
	if result[1] != "FOO=bar" {
		t.Errorf("result[1] = %q, want FOO=bar", result[1])
	}
}

func TestBuildEnv_RemoveVar(t *testing.T) {
	base := []string{"HOME=/home/user", "CLAUDECODE=1", "PATH=/usr/bin"}
	result := buildEnv(base, map[string]string{"CLAUDECODE": ""})

	for _, entry := range result {
		if entry == "CLAUDECODE=1" || entry == "CLAUDECODE=" {
			t.Errorf("CLAUDECODE should be removed, found %q", entry)
		}
	}
	if len(result) != 2 {
		t.Errorf("len = %d, want 2; got %v", len(result), result)
	}
}

func TestBuildEnv_RemoveAndSet(t *testing.T) {
	base := []string{"CLAUDECODE=1", "HOME=/home/user"}
	result := buildEnv(base, map[string]string{
		"CLAUDECODE": "",
		"ADDED":      "yes",
	})

	// CLAUDECODE should be gone, ADDED should be present.
	found := map[string]bool{}
	for _, entry := range result {
		if entry == "CLAUDECODE=1" || entry == "CLAUDECODE=" {
			t.Errorf("CLAUDECODE should be removed, found %q", entry)
		}
		if entry == "HOME=/home/user" {
			found["HOME"] = true
		}
		if entry == "ADDED=yes" {
			found["ADDED"] = true
		}
	}
	if !found["HOME"] {
		t.Error("HOME missing from result")
	}
	if !found["ADDED"] {
		t.Error("ADDED missing from result")
	}
	if len(result) != 2 {
		t.Errorf("len = %d, want 2; got %v", len(result), result)
	}
}

func TestBuildEnv_RemoveNonexistentVar(t *testing.T) {
	base := []string{"HOME=/home/user", "PATH=/usr/bin"}
	result := buildEnv(base, map[string]string{"NOEXIST": ""})

	if len(result) != 2 {
		t.Errorf("len = %d, want 2; got %v", len(result), result)
	}
	if !slices.Equal(result, base) {
		t.Errorf("got %v, want %v (removing nonexistent should be no-op)", result, base)
	}
}

func TestBuildEnv_OverrideExistingVar(t *testing.T) {
	base := []string{"HOME=/home/user", "FOO=old"}
	result := buildEnv(base, map[string]string{"FOO": "new"})

	// exec.Cmd uses last-wins semantics, so both entries are present.
	if len(result) != 3 {
		t.Fatalf("len = %d, want 3; got %v", len(result), result)
	}
	if result[2] != "FOO=new" {
		t.Errorf("result[2] = %q, want FOO=new", result[2])
	}
}

func TestTransportStopReapsProcessAfterWriteFailure(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "close-stdin-and-wait.sh")
	ready := filepath.Join(dir, "stdin-closed.ready")
	scriptBody := "#!/bin/sh\nready_file=\"$1\"\nexec 0<&-\n: > \"$ready_file\"\ntrap 'exit 0' TERM\nwhile :; do sleep 1; done\n"
	if err := os.WriteFile(script, []byte(scriptBody), 0o755); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	transport := NewTransport(script, []string{ready}, dir, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := transport.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	require.Eventually(t, func() bool {
		_, err := os.Stat(ready)
		return err == nil
	}, 5*time.Second, 25*time.Millisecond, "expected child to confirm stdin closure")

	notifyErr := transport.Notify("session/cancel", map[string]any{
		"sessionId": "test-session-001",
	})
	require.Error(t, notifyErr, "expected notify to fail after child closed stdin")
	if transport.IsAlive() {
		t.Fatal("expected transport to be marked dead after write failure")
	}

	if err := transport.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if transport.cmd == nil || transport.cmd.ProcessState == nil || !transport.cmd.ProcessState.Exited() {
		t.Fatal("expected Stop to reap the child process")
	}
}

package daemon

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/LaLanMo/muxagent-cli/internal/config"
)

func TestHandleStopTransportFailure_RetainsStateForLiveProcess(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	statePath := filepath.Join(home, ".muxagent", "daemon.state.json")
	lockPath := filepath.Join(home, ".muxagent", "daemon.state.json.lock")
	if err := os.MkdirAll(filepath.Dir(statePath), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(statePath, []byte("{}"), 0o600); err != nil {
		t.Fatalf("write state: %v", err)
	}
	if err := os.WriteFile(lockPath, []byte(strconv.Itoa(os.Getpid())), 0o600); err != nil {
		t.Fatalf("write lock: %v", err)
	}

	state := config.DaemonState{PID: os.Getpid()}
	var out bytes.Buffer
	if err := handleStopTransportFailure(state, &out); err != nil {
		t.Fatalf("handleStopTransportFailure returned error: %v", err)
	}

	if _, err := os.Stat(statePath); err != nil {
		t.Fatalf("state file should be retained: %v", err)
	}
	if _, err := os.Stat(lockPath); err != nil {
		t.Fatalf("lock file should be retained: %v", err)
	}
	if !bytes.Contains(out.Bytes(), []byte("State retained")) {
		t.Fatalf("expected retention message, got %q", out.String())
	}
}

func TestHandleStopTransportFailure_CleansStateForDeadProcess(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	statePath := filepath.Join(home, ".muxagent", "daemon.state.json")
	lockPath := filepath.Join(home, ".muxagent", "daemon.state.json.lock")
	if err := os.MkdirAll(filepath.Dir(statePath), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(statePath, []byte("{}"), 0o600); err != nil {
		t.Fatalf("write state: %v", err)
	}
	if err := os.WriteFile(lockPath, []byte("999999"), 0o600); err != nil {
		t.Fatalf("write lock: %v", err)
	}

	state := config.DaemonState{PID: deadPID()}
	var out bytes.Buffer
	if err := handleStopTransportFailure(state, &out); err != nil {
		t.Fatalf("handleStopTransportFailure returned error: %v", err)
	}

	if _, err := os.Stat(statePath); !os.IsNotExist(err) {
		t.Fatalf("state file should be removed, err=%v", err)
	}
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Fatalf("lock file should be removed, err=%v", err)
	}
	if !bytes.Contains(out.Bytes(), []byte("cleaned up stale state")) {
		t.Fatalf("expected cleanup message, got %q", out.String())
	}
}

func deadPID() int {
	pid := os.Getpid() + 100000
	for isPIDAlive(pid) {
		pid++
	}
	return pid
}

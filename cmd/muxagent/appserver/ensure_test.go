package appserver

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	internalappserver "github.com/LaLanMo/muxagent-cli/internal/appserver"
	appconfig "github.com/LaLanMo/muxagent-cli/internal/config"
)

func TestResolveLiveEndpointKeepsStateWhenPIDIsAlive(t *testing.T) {
	stateDir := filepath.Join(t.TempDir(), "appserver")
	endpoint := internalappserver.DaemonEndpoint{
		DaemonState: appconfig.DaemonState{
			Address: "127.0.0.1:12345",
			PID:     42,
		},
		InstanceID: "instance-1",
	}
	if err := internalappserver.SaveDaemonEndpoint(stateDir, endpoint); err != nil {
		t.Fatalf("save daemon endpoint: %v", err)
	}

	wantErr := errors.New("probe timeout")
	_, reuse, err := resolveLiveEndpoint(stateDir, func(internalappserver.DaemonEndpoint) error {
		return wantErr
	}, func(pid int) bool {
		return pid == endpoint.PID
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("resolveLiveEndpoint error = %v, want %v", err, wantErr)
	}
	if reuse {
		t.Fatal("reuse = true, want false")
	}
	if _, err := internalappserver.LoadDaemonEndpoint(stateDir); err != nil {
		t.Fatalf("daemon endpoint unexpectedly cleared: %v", err)
	}
}

func TestResolveLiveEndpointClearsStateWhenPIDIsDead(t *testing.T) {
	stateDir := filepath.Join(t.TempDir(), "appserver")
	endpoint := internalappserver.DaemonEndpoint{
		DaemonState: appconfig.DaemonState{
			Address: "127.0.0.1:12345",
			PID:     42,
		},
		InstanceID: "instance-1",
	}
	if err := internalappserver.SaveDaemonEndpoint(stateDir, endpoint); err != nil {
		t.Fatalf("save daemon endpoint: %v", err)
	}

	resolved, reuse, err := resolveLiveEndpoint(stateDir, func(internalappserver.DaemonEndpoint) error {
		return errors.New("dial tcp: connection refused")
	}, func(int) bool {
		return false
	})
	if err != nil {
		t.Fatalf("resolveLiveEndpoint error: %v", err)
	}
	if reuse {
		t.Fatal("reuse = true, want false")
	}
	if resolved != (internalappserver.DaemonEndpoint{}) {
		t.Fatalf("resolved endpoint = %#v, want zero value", resolved)
	}
	if _, err := internalappserver.LoadDaemonEndpoint(stateDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("daemon endpoint err = %v, want os.ErrNotExist", err)
	}
}

func TestProbeEndpointTimesOutWhenPeerDoesNotRespond(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	accepted := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		close(accepted)
		defer conn.Close()
		<-time.After(2 * daemonProbeTimeout)
	}()

	start := time.Now()
	err = probeEndpoint(ln.Addr().String(), "token", "")
	if err == nil {
		t.Fatal("probeEndpoint error = nil, want timeout")
	}
	if elapsed := time.Since(start); elapsed > 3*daemonProbeTimeout {
		t.Fatalf("probeEndpoint elapsed = %s, want <= %s", elapsed, 3*daemonProbeTimeout)
	}
	<-accepted
	<-done
}

package daemon

import (
	"context"
	"testing"
	"time"
)

func TestDaemonStopClosesDoneChannel(t *testing.T) {
	d := New("ws://localhost:8080/ws")

	if err := d.Stop(context.Background()); err != nil {
		t.Fatalf("Stop returned error: %v", err)
	}

	select {
	case <-d.Done():
	case <-time.After(time.Second):
		t.Fatal("daemon done channel was not closed")
	}

	if err := d.Stop(context.Background()); err != nil {
		t.Fatalf("second Stop returned error: %v", err)
	}
}

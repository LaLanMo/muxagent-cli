package acp

import (
	"testing"

	"github.com/LaLanMo/muxagent-cli/internal/appwire"
)

func TestClientEmitAfterCloseDoesNotPanic(t *testing.T) {
	client := NewClient(Config{})

	client.closeEvents()

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("emit panicked after events were closed: %v", r)
		}
	}()

	client.emit(appwire.Event{Type: appwire.EventModeChanged})
}

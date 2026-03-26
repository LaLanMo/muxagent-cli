package acp

import (
	"encoding/json"
	"testing"

	"github.com/LaLanMo/muxagent-cli/internal/acpprotocol"
	"github.com/LaLanMo/muxagent-cli/internal/appwire"
)

func toolStatusPtr(status acpprotocol.ToolCallStatus) *acpprotocol.ToolCallStatus {
	return &status
}

func toolKindPtr(kind acpprotocol.ToolKind) *acpprotocol.ToolKind {
	return &kind
}

func TestNormalizeSessionConfigOptionsSynthesizesModeOptionFromModes(t *testing.T) {
	modes := &acpprotocol.SessionModeState{
		CurrentModeID: "full-access",
		AvailableModes: []acpprotocol.SessionMode{
			{ID: "full-access", Name: "Full Access"},
			{ID: "read-only", Name: "Read Only"},
		},
	}

	options := normalizeSessionConfigOptions(nil, modes)
	if len(options) != 1 {
		t.Fatalf("len(options) = %d, want 1", len(options))
	}
	if got := configOptionCategory(options[0]); got != "mode" {
		t.Fatalf("category = %q, want mode", got)
	}
	if options[0].CurrentValue != "full-access" {
		t.Fatalf("currentValue = %q, want full-access", options[0].CurrentValue)
	}
	if len(options[0].Options.Flatten()) != 2 {
		t.Fatalf("len(flattened options) = %d, want 2", len(options[0].Options.Flatten()))
	}
}

func TestModeConfigOptionEventCarriesModeConfigOption(t *testing.T) {
	options := normalizeSessionConfigOptions(
		nil,
		&acpprotocol.SessionModeState{
			CurrentModeID: "read-only",
			AvailableModes: []acpprotocol.SessionMode{
				{ID: "full-access", Name: "Full Access"},
				{ID: "read-only", Name: "Read Only"},
			},
		},
	)

	ev := modeConfigOptionEvent("session-123", "read-only", options)
	if ev == nil || ev.ModeChanged == nil {
		t.Fatal("expected mode changed event")
	}
	if ev.ModeChanged.ACPConfigOption == nil {
		t.Fatal("expected ACPConfigOption on mode changed event")
	}
	if len(ev.ModeChanged.ACPConfigOption.ConfigOptions) != 1 {
		t.Fatalf(
			"len(configOptions) = %d, want 1",
			len(ev.ModeChanged.ACPConfigOption.ConfigOptions),
		)
	}
	if got := configOptionCategory(ev.ModeChanged.ACPConfigOption.ConfigOptions[0]); got != "mode" {
		t.Fatalf("category = %q, want mode", got)
	}
}

func TestModeConfigOptionEventSynthesizesStubWhenOptionsMissing(t *testing.T) {
	ev := modeConfigOptionEvent("session-123", "read-only", nil)
	if ev == nil || ev.ModeChanged == nil {
		t.Fatal("expected mode changed event")
	}
	if ev.ModeChanged.ACPConfigOption == nil {
		t.Fatal("expected ACPConfigOption on mode changed event")
	}
	if len(ev.ModeChanged.ACPConfigOption.ConfigOptions) != 1 {
		t.Fatalf(
			"len(configOptions) = %d, want 1",
			len(ev.ModeChanged.ACPConfigOption.ConfigOptions),
		)
	}
	option := ev.ModeChanged.ACPConfigOption.ConfigOptions[0]
	if got := configOptionCategory(option); got != "mode" {
		t.Fatalf("category = %q, want mode", got)
	}
	if option.CurrentValue != "read-only" {
		t.Fatalf("currentValue = %q, want read-only", option.CurrentValue)
	}
	if len(option.Options.Flatten()) != 0 {
		t.Fatalf("len(flattened options) = %d, want 0", len(option.Options.Flatten()))
	}
}

func TestBuildToolEventPreservesDiffsOnPendingToolCall(t *testing.T) {
	eventType, toolEvent := buildToolEvent(
		"msg-1",
		"part-1",
		&acpprotocol.ToolCallUpdate{
			ToolCallID: "call-1",
			Kind:       toolKindPtr(acpprotocol.ToolKind("edit")),
			Status:     toolStatusPtr(acpprotocol.ToolCallStatus("pending")),
			RawInput: json.RawMessage(
				`{"file_path":"/workspace/file.txt","old_string":"before","new_string":"after"}`,
			),
			Content: []json.RawMessage{
				json.RawMessage(
					`{"type":"diff","path":"/workspace/file.txt","oldText":"before\n","newText":"after\n"}`,
				),
			},
		},
	)

	if eventType != appwire.EventToolStarted {
		t.Fatalf("eventType = %q, want %q", eventType, appwire.EventToolStarted)
	}
	if toolEvent.App.Status != appwire.ToolStatusPending {
		t.Fatalf("status = %q, want %q", toolEvent.App.Status, appwire.ToolStatusPending)
	}
	if len(toolEvent.App.Diffs) != 1 {
		t.Fatalf("len(diffs) = %d, want 1", len(toolEvent.App.Diffs))
	}
	if toolEvent.App.Diffs[0].Path != "/workspace/file.txt" {
		t.Fatalf("diff path = %q, want /workspace/file.txt", toolEvent.App.Diffs[0].Path)
	}
	if toolEvent.App.Output != "" {
		t.Fatalf("output = %q, want empty", toolEvent.App.Output)
	}
}

func TestBuildToolEventCompletedReadStillExtractsOutput(t *testing.T) {
	eventType, toolEvent := buildToolEvent(
		"msg-1",
		"part-1",
		&acpprotocol.ToolCallUpdate{
			ToolCallID: "call-read-1",
			Kind:       toolKindPtr(acpprotocol.ToolKind("read")),
			Status:     toolStatusPtr(acpprotocol.ToolCallStatus("completed")),
			RawOutput:  json.RawMessage(`"line 1\nline 2"`),
		},
	)

	if eventType != appwire.EventToolCompleted {
		t.Fatalf("eventType = %q, want %q", eventType, appwire.EventToolCompleted)
	}
	if toolEvent.App.Status != appwire.ToolStatusCompleted {
		t.Fatalf("status = %q, want %q", toolEvent.App.Status, appwire.ToolStatusCompleted)
	}
	if toolEvent.App.Output != "line 1\nline 2" {
		t.Fatalf("output = %q, want line 1\\nline 2", toolEvent.App.Output)
	}
	if toolEvent.App.Diffs != nil {
		t.Fatalf("diffs = %#v, want nil", toolEvent.App.Diffs)
	}
}

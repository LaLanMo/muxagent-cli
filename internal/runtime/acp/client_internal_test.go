package acp

import (
	"testing"

	"github.com/LaLanMo/muxagent-cli/internal/acpprotocol"
)

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

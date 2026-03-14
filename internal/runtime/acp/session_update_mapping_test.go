package acp

import (
	"encoding/json"
	"testing"

	"github.com/LaLanMo/muxagent-cli/internal/domain"
	"github.com/stretchr/testify/require"
)

func TestHandlePlanEmitsACPAndAppPayloads(t *testing.T) {
	client := NewClient(Config{})

	client.handlePlan("session-1", json.RawMessage(`{
		"sessionUpdate": "plan",
		"_meta": {"source": "codex"},
		"entries": [
			{"content": "Inspect event payloads", "status": "completed", "priority": "high"},
			{"content": "Refactor usage updates", "status": "in_progress", "priority": "medium"}
		]
	}`))

	select {
	case event := <-client.Events():
		require.Equal(t, domain.EventPlanUpdated, event.Type)
		require.NotNil(t, event.Plan)
		require.NotNil(t, event.Plan.ACP)
		require.Equal(t, "plan", event.Plan.ACP.SessionUpdate)
		require.Len(t, event.Plan.ACP.Entries, 2)
		require.Equal(t, "Inspect event payloads", event.Plan.App.Entries[0].Content)
		require.Equal(t, "in_progress", event.Plan.App.Entries[1].Status)
		require.Equal(t, "medium", event.Plan.App.Entries[1].Priority)
	default:
		t.Fatal("expected plan.updated event")
	}
}

func TestHandleUsageUpdateEmitsACPAndAppPayloads(t *testing.T) {
	client := NewClient(Config{})

	client.handleUsageUpdate("session-2", json.RawMessage(`{
		"sessionUpdate": "usage_update",
		"used": 53000,
		"size": 200000,
		"cost": {"amount": 0.045, "currency": "USD"}
	}`))

	select {
	case event := <-client.Events():
		require.Equal(t, domain.EventUsageUpdate, event.Type)
		require.NotNil(t, event.Usage)
		require.NotNil(t, event.Usage.ACP)
		require.Equal(t, "usage_update", event.Usage.ACP.SessionUpdate)
		require.Equal(t, int64(53000), event.Usage.App.ContextUsed)
		require.Equal(t, int64(200000), event.Usage.App.ContextSize)
		require.NotNil(t, event.Usage.App.CostAmount)
		require.NotNil(t, event.Usage.App.CostCurrency)
		require.Equal(t, 0.045, *event.Usage.App.CostAmount)
		require.Equal(t, "USD", *event.Usage.App.CostCurrency)
	default:
		t.Fatal("expected usage.update event")
	}
}

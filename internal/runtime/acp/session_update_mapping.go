package acp

import (
	"time"

	"github.com/LaLanMo/muxagent-cli/internal/acpprotocol"
	"github.com/LaLanMo/muxagent-cli/internal/appwire"
)

func planEvent(sessionID string, update *acpprotocol.PlanUpdate) *appwire.Event {
	if update == nil {
		return nil
	}

	entries := make([]appwire.PlanEntryApp, 0, len(update.Entries))
	for _, entry := range update.Entries {
		entries = append(entries, appwire.PlanEntryApp{
			Content:  entry.Content,
			Status:   string(entry.Status),
			Priority: string(entry.Priority),
		})
	}

	return &appwire.Event{
		Type:      appwire.EventPlanUpdated,
		SessionID: sessionID,
		At:        time.Now(),
		Plan: &appwire.PlanEvent{
			ACP: update,
			App: appwire.PlanEventApp{Entries: entries},
		},
	}
}

func usageEvent(sessionID string, update *acpprotocol.UsageUpdate) *appwire.Event {
	if update == nil {
		return nil
	}

	app := appwire.UsageEventApp{
		ContextUsed: update.Used,
		ContextSize: update.Size,
	}
	if update.Cost != nil {
		app.CostAmount = &update.Cost.Amount
		app.CostCurrency = &update.Cost.Currency
	}

	return &appwire.Event{
		Type:      appwire.EventUsageUpdate,
		SessionID: sessionID,
		At:        time.Now(),
		Usage: &appwire.UsageEvent{
			ACP: update,
			App: app,
		},
	}
}

package acp

import (
	"time"

	"github.com/LaLanMo/muxagent-cli/internal/acpprotocol"
	"github.com/LaLanMo/muxagent-cli/internal/domain"
)

func planEvent(sessionID string, update *acpprotocol.PlanUpdate) *domain.Event {
	if update == nil {
		return nil
	}

	entries := make([]domain.PlanEntryApp, 0, len(update.Entries))
	for _, entry := range update.Entries {
		entries = append(entries, domain.PlanEntryApp{
			Content:  entry.Content,
			Status:   string(entry.Status),
			Priority: string(entry.Priority),
		})
	}

	return &domain.Event{
		Type:      domain.EventPlanUpdated,
		SessionID: sessionID,
		At:        time.Now(),
		Plan: &domain.PlanEvent{
			ACP: update,
			App: domain.PlanEventApp{Entries: entries},
		},
	}
}

func usageEvent(sessionID string, update *acpprotocol.UsageUpdate) *domain.Event {
	if update == nil {
		return nil
	}

	app := domain.UsageEventApp{
		ContextUsed: update.Used,
		ContextSize: update.Size,
	}
	if update.Cost != nil {
		app.CostAmount = &update.Cost.Amount
		app.CostCurrency = &update.Cost.Currency
	}

	return &domain.Event{
		Type:      domain.EventUsageUpdate,
		SessionID: sessionID,
		At:        time.Now(),
		Usage: &domain.UsageEvent{
			ACP: update,
			App: app,
		},
	}
}

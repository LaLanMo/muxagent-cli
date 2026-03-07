package runtime

import (
	"context"

	"github.com/LaLanMo/muxagent-cli/internal/domain"
)

// Client is the interface for interacting with an ACP-compatible agent runtime.
type Client interface {
	Start(ctx context.Context) error
	Stop() error

	NewSession(ctx context.Context, cwd string, permissionMode string) (string, []domain.ConfigOption, error)
	LoadSession(ctx context.Context, sessionID, cwd, permissionMode, model string) ([]domain.ConfigOption, error)
	ListSessions(ctx context.Context, cwd string) ([]domain.SessionSummary, error)
	Prompt(ctx context.Context, sessionID string, content []domain.ContentBlock) (string, error)
	Cancel(ctx context.Context, sessionID string) error
	SetMode(ctx context.Context, sessionID, modeID string) error
	SetConfigOption(ctx context.Context, sessionID, configID, value string) error
	ReplyPermission(ctx context.Context, sessionID, requestID, optionID string) error

	Events() <-chan domain.Event
}

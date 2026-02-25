package runtime

import (
	"context"

	"github.com/LaLanMo/muxagent-cli/internal/domain"
)

// Client is the interface for interacting with an ACP-compatible agent runtime.
type Client interface {
	Start(ctx context.Context) error
	Stop() error

	NewSession(ctx context.Context, cwd string) (string, error)
	LoadSession(ctx context.Context, sessionID, cwd string) error
	Prompt(ctx context.Context, sessionID string, content []domain.ContentBlock) (string, error)
	Cancel(ctx context.Context, sessionID string) error
	ReplyPermission(ctx context.Context, sessionID, requestID, optionID string) error

	Events() <-chan domain.Event
}

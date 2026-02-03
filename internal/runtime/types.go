package runtime

import (
	"context"
	"fmt"

	"github.com/LaLanMo/muxagent-cli/internal/config"
	"github.com/LaLanMo/muxagent-cli/internal/domain"
	"github.com/LaLanMo/muxagent-cli/internal/runtime/opencode"
)

type Client interface {
	Health(ctx context.Context) (string, error)
	ListSessions(ctx context.Context) ([]domain.Session, error)
	ListMessages(ctx context.Context, sessionID string) ([]domain.Message, error)
	SendMessage(ctx context.Context, sessionID string, parts []domain.MessagePart) (domain.Message, error)
	ListApprovals(ctx context.Context) ([]domain.ApprovalRequest, error)
	ReplyApproval(ctx context.Context, requestID string, approved bool) error
	StreamEvents(ctx context.Context) (<-chan domain.Event, <-chan error)
}

func NewClient(id config.RuntimeID, settings config.RuntimeSettings) (Client, error) {
	switch id {
	case config.RuntimeOpenCode:
		return opencode.NewClient(settings.BaseURL, settings.Auth.Username, settings.Auth.Password), nil
	default:
		return nil, fmt.Errorf("unsupported runtime %q - only %q is supported", id, config.RuntimeOpenCode)
	}
}

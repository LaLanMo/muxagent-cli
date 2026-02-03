package opencode

import (
	"context"
	"encoding/base64"
	"fmt"
	"time"

	"github.com/LaLanMo/muxagent-cli/internal/domain"
	opencode "github.com/sst/opencode-sdk-go"
	"github.com/sst/opencode-sdk-go/option"
)

type Client struct {
	sdk      *opencode.Client
	baseURL  string
	username string
	password string
}

func NewClient(baseURL, username, password string) *Client {
	opts := []option.RequestOption{option.WithBaseURL(baseURL)}

	if password != "" {
		creds := base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("%s:%s", username, password)))
		opts = append(opts, option.WithHeader("Authorization", "Basic "+creds))
	}

	return &Client{
		sdk:      opencode.NewClient(opts...),
		baseURL:  baseURL,
		username: username,
		password: password,
	}
}

func (c *Client) Health(ctx context.Context) (string, error) {
	var response struct {
		Healthy bool   `json:"healthy"`
		Version string `json:"version"`
	}

	if err := c.sdk.Get(ctx, "global/health", nil, &response); err != nil {
		return "", err
	}
	return response.Version, nil
}

func (c *Client) ListSessions(ctx context.Context) ([]domain.Session, error) {
	res, err := c.sdk.Session.List(ctx, opencode.SessionListParams{})
	if err != nil {
		return nil, err
	}

	sessions := make([]domain.Session, 0, len(*res))
	for _, session := range *res {
		sessions = append(sessions, domain.Session{
			ID:        session.ID,
			Title:     session.Title,
			Status:    domain.SessionStatusRunning,
			CreatedAt: time.UnixMilli(int64(session.Time.Created)),
			UpdatedAt: time.UnixMilli(int64(session.Time.Updated)),
		})
	}

	return sessions, nil
}

func (c *Client) ListMessages(ctx context.Context, sessionID string) ([]domain.Message, error) {
	res, err := c.sdk.Session.Messages(ctx, sessionID, opencode.SessionMessagesParams{})
	if err != nil {
		return nil, err
	}

	messages := make([]domain.Message, 0, len(*res))
	for _, entry := range *res {
		message := entry.Info
		parts := mapParts(entry.Parts)
		if message.Role == opencode.MessageRoleUser {
			if user, ok := message.AsUnion().(opencode.UserMessage); ok {
				if user.Summary.Body != "" {
					parts = append(parts, domain.MessagePart{Type: domain.PartTypeText, Text: user.Summary.Body})
				}
			}
		}

		role := domain.MessageRoleAgent
		switch message.Role {
		case opencode.MessageRoleUser:
			role = domain.MessageRoleUser
		case opencode.MessageRoleAssistant:
			role = domain.MessageRoleAgent
		}

		messages = append(messages, domain.Message{
			ID:        message.ID,
			SessionID: message.SessionID,
			Role:      role,
			Parts:     parts,
			Metadata:  map[string]any{},
			CreatedAt: time.UnixMilli(int64(resolveMessageTime(message.Time))),
		})
	}

	return messages, nil
}

func (c *Client) SendMessage(ctx context.Context, sessionID string, parts []domain.MessagePart) (domain.Message, error) {
	promptParts := make([]opencode.SessionPromptParamsPartUnion, 0, len(parts))
	for _, part := range parts {
		promptPart := opencode.SessionPromptParamsPart{}
		switch part.Type {
		case domain.PartTypeText:
			promptPart.Type = opencode.F(opencode.SessionPromptParamsPartsTypeText)
			promptPart.Text = opencode.F(part.Text)
		case domain.PartTypeFile:
			promptPart.Type = opencode.F(opencode.SessionPromptParamsPartsTypeFile)
			if part.Media != nil {
				promptPart.Name = opencode.F(part.Media.Name)
				promptPart.Mime = opencode.F(part.Media.MimeType)
				promptPart.URL = opencode.F(part.Media.URL)
			}
		default:
			continue
		}
		promptParts = append(promptParts, promptPart)
	}

	params := opencode.SessionPromptParams{
		Parts: opencode.F(promptParts),
	}
	response, err := c.sdk.Session.Prompt(ctx, sessionID, params)
	if err != nil {
		return domain.Message{}, err
	}

	message := response.Info
	role := domain.MessageRoleAgent

	return domain.Message{
		ID:        message.ID,
		SessionID: message.SessionID,
		Role:      role,
		Parts:     mapParts(response.Parts),
		Metadata:  map[string]any{},
		CreatedAt: time.UnixMilli(int64(resolveMessageTime(message.Time))),
	}, nil
}

func (c *Client) ListApprovals(ctx context.Context) ([]domain.ApprovalRequest, error) {
	var response []struct {
		ID         string         `json:"id"`
		SessionID  string         `json:"sessionID"`
		Permission string         `json:"permission"`
		Patterns   []string       `json:"patterns"`
		Metadata   map[string]any `json:"metadata"`
		Tool       struct {
			MessageID string `json:"messageID"`
			CallID    string `json:"callID"`
		} `json:"tool"`
		Time struct{ Created float64 } `json:"time"`
	}

	if err := c.sdk.Get(ctx, "permission", nil, &response); err != nil {
		return nil, err
	}

	approvals := make([]domain.ApprovalRequest, 0, len(response))
	for _, item := range response {
		approvals = append(approvals, domain.ApprovalRequest{
			ID:        item.ID,
			SessionID: item.SessionID,
			Title:     item.Permission,
			Metadata:  item.Metadata,
			Patterns:  item.Patterns,
			CallID:    item.Tool.CallID,
			MessageID: item.Tool.MessageID,
			CreatedAt: time.UnixMilli(int64(item.Time.Created)),
		})
	}

	return approvals, nil
}

func (c *Client) ReplyApproval(ctx context.Context, requestID string, approved bool) error {
	response := "reject"
	if approved {
		response = "once"
	}
	body := map[string]any{
		"reply": response,
	}
	return c.sdk.Post(ctx, fmt.Sprintf("permission/%s/reply", requestID), body, nil)
}

func (c *Client) StreamEvents(ctx context.Context) (<-chan domain.Event, <-chan error) {
	events := make(chan domain.Event)
	errs := make(chan error, 1)

	stream := c.sdk.Event.ListStreaming(ctx, opencode.EventListParams{})

	go func() {
		defer close(events)
		defer close(errs)

		for stream.Next() {
			payload := stream.Current()
			event, ok := mapEvent(payload)
			if !ok {
				continue
			}
			events <- event
		}
		if err := stream.Err(); err != nil {
			errs <- err
		}
	}()

	return events, errs
}

func mapEvent(payload opencode.EventListResponse) (domain.Event, bool) {
	event := domain.Event{At: time.Now()}

	switch e := payload.AsUnion().(type) {
	case opencode.EventListResponseEventMessagePartUpdated:
		event.Type = domain.EventMessageDelta
		event.SessionID = e.Properties.Part.SessionID
		event.MessagePart = &domain.MessagePartEvent{
			PartID:    e.Properties.Part.ID,
			MessageID: e.Properties.Part.MessageID,
			Delta:     e.Properties.Delta,
			PartType:  string(e.Properties.Part.Type),
			FullText:  e.Properties.Part.Text,
		}
		return event, true

	case opencode.EventListResponseEventMessageUpdated:
		event.Type = domain.EventMessageFinal
		event.SessionID = e.Properties.Info.SessionID
		event.Message = mapMessageFromInfo(e.Properties.Info)
		return event, true

	case opencode.EventListResponseEventPermissionUpdated:
		event.Type = domain.EventApprovalRequested
		event.SessionID = e.Properties.SessionID
		event.Approval = &domain.ApprovalRequest{
			ID:        e.Properties.ID,
			SessionID: e.Properties.SessionID,
			Title:     e.Properties.Title,
			Metadata:  e.Properties.Metadata,
			CallID:    e.Properties.CallID,
			MessageID: e.Properties.MessageID,
			CreatedAt: time.UnixMilli(int64(e.Properties.Time.Created)),
		}
		return event, true

	case opencode.EventListResponseEventPermissionReplied:
		event.Type = domain.EventApprovalReplied
		event.SessionID = e.Properties.SessionID
		return event, true

	case opencode.EventListResponseEventSessionUpdated:
		event.Type = domain.EventSessionStatus
		event.SessionID = e.Properties.Info.ID
		event.Session = mapSessionFromInfo(e.Properties.Info)
		return event, true

	case opencode.EventListResponseEventSessionCreated:
		event.Type = domain.EventSessionStatus
		event.SessionID = e.Properties.Info.ID
		event.Session = mapSessionFromInfo(e.Properties.Info)
		return event, true

	case opencode.EventListResponseEventSessionDeleted:
		event.Type = domain.EventSessionStatus
		event.SessionID = e.Properties.Info.ID
		event.Session = mapSessionFromInfo(e.Properties.Info)
		return event, true

	case opencode.EventListResponseEventSessionIdle:
		event.Type = domain.EventSessionStatus
		event.SessionID = e.Properties.SessionID
		return event, true

	case opencode.EventListResponseEventSessionError:
		event.Type = domain.EventRunFailed
		event.SessionID = e.Properties.SessionID
		event.Error = &domain.SessionError{
			Name: string(e.Properties.Error.Name),
		}
		// Try to extract message from error data if available
		if data, ok := e.Properties.Error.Data.(map[string]any); ok {
			if msg, ok := data["message"].(string); ok {
				event.Error.Message = msg
			}
		}
		return event, true

	case opencode.EventListResponseEventSessionCompacted:
		event.Type = domain.EventSessionStatus
		event.SessionID = e.Properties.SessionID
		return event, true

	case opencode.EventListResponseEventServerConnected:
		event.Type = domain.EventConnectionState
		return event, true

	default:
		return event, false
	}
}

func mapMessageFromInfo(info opencode.Message) *domain.Message {
	role := domain.MessageRoleAgent
	switch info.Role {
	case opencode.MessageRoleUser:
		role = domain.MessageRoleUser
	case opencode.MessageRoleAssistant:
		role = domain.MessageRoleAgent
	}

	return &domain.Message{
		ID:        info.ID,
		SessionID: info.SessionID,
		Role:      role,
		Metadata:  map[string]any{},
		CreatedAt: time.UnixMilli(int64(resolveMessageTime(info.Time))),
	}
}

func mapSessionFromInfo(info opencode.Session) *domain.Session {
	return &domain.Session{
		ID:        info.ID,
		Title:     info.Title,
		Status:    domain.SessionStatusRunning,
		CreatedAt: time.UnixMilli(int64(info.Time.Created)),
		UpdatedAt: time.UnixMilli(int64(info.Time.Updated)),
	}
}

func mapParts(parts []opencode.Part) []domain.MessagePart {
	mapped := make([]domain.MessagePart, 0, len(parts))
	for _, part := range parts {
		if part.Type == opencode.PartTypeText || part.Type == opencode.PartTypeReasoning {
			mapped = append(mapped, domain.MessagePart{
				Type: domain.PartTypeText,
				Text: part.Text,
			})
			continue
		}
		if part.Type == opencode.PartTypeFile {
			mapped = append(mapped, domain.MessagePart{
				Type: domain.PartTypeFile,
				Media: &domain.MediaPart{
					Name:     part.Name,
					MimeType: part.Mime,
					Path:     part.Filename,
					URL:      part.URL,
				},
			})
			continue
		}
		if part.Type == opencode.PartTypeTool {
			mapped = append(mapped, domain.MessagePart{
				Type: domain.PartTypeToolCall,
				ToolCall: &domain.ToolCallPart{
					ID:   part.CallID,
					Name: part.Tool,
				},
				Data: map[string]any{},
			})
			continue
		}
		mapped = append(mapped, domain.MessagePart{
			Type: domain.PartTypeData,
			Data: map[string]any{
				"type":     part.Type,
				"name":     part.Name,
				"metadata": part.Metadata,
			},
		})
	}
	return mapped
}

func resolveMessageTime(timeField interface{}) int64 {
	switch timeValue := timeField.(type) {
	case opencode.UserMessageTime:
		return int64(timeValue.Created)
	case opencode.AssistantMessageTime:
		return int64(timeValue.Created)
	default:
		return time.Now().UnixMilli()
	}
}

var _ interface {
	Health(context.Context) (string, error)
	ListSessions(context.Context) ([]domain.Session, error)
	ListMessages(context.Context, string) ([]domain.Message, error)
	SendMessage(context.Context, string, []domain.MessagePart) (domain.Message, error)
	ListApprovals(context.Context) ([]domain.ApprovalRequest, error)
	ReplyApproval(context.Context, string, bool) error
	StreamEvents(context.Context) (<-chan domain.Event, <-chan error)
} = (*Client)(nil)

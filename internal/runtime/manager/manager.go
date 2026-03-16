package manager

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"

	"github.com/LaLanMo/muxagent-cli/internal/acpbin"
	"github.com/LaLanMo/muxagent-cli/internal/acpprotocol"
	"github.com/LaLanMo/muxagent-cli/internal/appwire"
	"github.com/LaLanMo/muxagent-cli/internal/codexbin"
	"github.com/LaLanMo/muxagent-cli/internal/config"
	"github.com/LaLanMo/muxagent-cli/internal/domain"
	"github.com/LaLanMo/muxagent-cli/internal/runtime"
	"github.com/LaLanMo/muxagent-cli/internal/runtime/acp"
)

type RuntimeInfo struct {
	ID            string                            `json:"id"`
	Label         string                            `json:"label"`
	Ready         bool                              `json:"ready"`
	ConfigOptions []acpprotocol.SessionConfigOption `json:"configOptions,omitempty"`
}

type Manager struct {
	cfg config.Config

	mu             sync.RWMutex
	runtimes       map[config.RuntimeID]*managedRuntime
	sessionRuntime map[string]config.RuntimeID

	events    chan appwire.Event
	closeOnce sync.Once
	done      chan struct{}
}

type managedRuntime struct {
	id       config.RuntimeID
	settings config.RuntimeSettings

	mu     sync.Mutex
	client runtime.Client
}

func New(cfg config.Config) *Manager {
	runtimes := make(map[config.RuntimeID]*managedRuntime, len(cfg.Runtimes))
	for id, settings := range cfg.Runtimes {
		if !config.IsSupportedRuntime(id) {
			continue
		}
		runtimes[id] = &managedRuntime{
			id:       id,
			settings: settings,
		}
	}
	return &Manager{
		cfg:            cfg,
		runtimes:       runtimes,
		sessionRuntime: make(map[string]config.RuntimeID),
		events:         make(chan appwire.Event, 512),
		done:           make(chan struct{}),
	}
}

func (m *Manager) Stop() error {
	var firstErr error
	m.closeOnce.Do(func() {
		close(m.done)

		m.mu.RLock()
		runtimes := make([]*managedRuntime, 0, len(m.runtimes))
		for _, rt := range m.runtimes {
			runtimes = append(runtimes, rt)
		}
		m.mu.RUnlock()

		for _, rt := range runtimes {
			rt.mu.Lock()
			client := rt.client
			rt.client = nil
			rt.mu.Unlock()
			if client != nil {
				if err := client.Stop(); err != nil && firstErr == nil {
					firstErr = err
				}
			}
		}

		close(m.events)
	})
	return firstErr
}

func (m *Manager) Events() <-chan appwire.Event {
	return m.events
}

func (m *Manager) RuntimeList() []RuntimeInfo {
	type pair struct {
		id   config.RuntimeID
		info RuntimeInfo
	}
	items := make([]pair, 0, len(m.runtimes))
	for id := range m.runtimes {
		items = append(items, pair{
			id: id,
			info: RuntimeInfo{
				ID:            string(id),
				Label:         runtimeLabel(id),
				Ready:         true,
				ConfigOptions: runtimeConfigOptions(id),
			},
		})
	}

	sort.Slice(items, func(i, j int) bool {
		return items[i].info.Label < items[j].info.Label
	})

	out := make([]RuntimeInfo, 0, len(items))
	for _, item := range items {
		out = append(out, item.info)
	}
	return out
}

func (m *Manager) NewSession(
	ctx context.Context,
	runtimeID string,
	cwd string,
	permissionMode string,
) (string, string, acpprotocol.NewSessionResponse, error) {
	if strings.TrimSpace(runtimeID) == "" {
		return "", "", acpprotocol.NewSessionResponse{}, fmt.Errorf("missing runtime")
	}
	client, rid, err := m.ensureRuntime(ctx, runtimeID)
	if err != nil {
		return "", "", acpprotocol.NewSessionResponse{}, err
	}
	resp, err := client.NewSession(ctx, cwd, permissionMode)
	if err != nil {
		return "", "", acpprotocol.NewSessionResponse{}, err
	}
	m.setSessionRuntime(resp.SessionID, rid)
	return resp.SessionID, string(rid), resp, nil
}

func (m *Manager) LoadSession(
	ctx context.Context,
	runtimeID string,
	sessionID string,
	cwd string,
	permissionMode string,
	model string,
) (string, acpprotocol.LoadSessionResponse, error) {
	rid := m.resolveRuntimeID(sessionID, runtimeID)
	if rid == "" {
		return "", acpprotocol.LoadSessionResponse{}, fmt.Errorf("missing runtime")
	}
	client, rid, err := m.ensureRuntime(ctx, string(rid))
	if err != nil {
		return "", acpprotocol.LoadSessionResponse{}, err
	}
	resp, err := client.LoadSession(ctx, sessionID, cwd, permissionMode, model)
	if err != nil {
		return "", acpprotocol.LoadSessionResponse{}, err
	}
	m.setSessionRuntime(sessionID, rid)
	return string(rid), resp, nil
}

func (m *Manager) ResolveSessions(
	ctx context.Context,
	runtimeID string,
	sessionIDs []string,
) ([]domain.SessionSummary, error) {
	if runtimeID != "" {
		client, _, err := m.ensureRuntime(ctx, runtimeID)
		if err != nil {
			return nil, err
		}
		return client.ListSessions(ctx, "")
	}

	seen := make(map[string]struct{}, len(sessionIDs))
	list := make([]domain.SessionSummary, 0)
	for _, rid := range m.configuredRuntimeIDs() {
		client, _, err := m.ensureRuntime(ctx, string(rid))
		if err != nil {
			log.Printf("[runtime] resolve start %s failed: %v", rid, err)
			continue
		}
		sessions, err := client.ListSessions(ctx, "")
		if err != nil {
			log.Printf("[runtime] resolve list %s failed: %v", rid, err)
			continue
		}
		for _, session := range sessions {
			if _, ok := seen[session.SessionID]; ok {
				continue
			}
			seen[session.SessionID] = struct{}{}
			list = append(list, session)
		}
	}
	return list, nil
}

func (m *Manager) Prompt(
	ctx context.Context,
	sessionID string,
	content []domain.ContentBlock,
) (string, *domain.PromptUsage, error) {
	client, _, err := m.runtimeForSession(ctx, sessionID)
	if err != nil {
		return "", nil, err
	}
	return client.Prompt(ctx, sessionID, content)
}

func (m *Manager) Cancel(ctx context.Context, sessionID string) error {
	client, _, err := m.runtimeForSession(ctx, sessionID)
	if err != nil {
		return err
	}
	return client.Cancel(ctx, sessionID)
}

func (m *Manager) SetMode(ctx context.Context, sessionID string, modeID string) error {
	client, _, err := m.runtimeForSession(ctx, sessionID)
	if err != nil {
		return err
	}
	return client.SetMode(ctx, sessionID, modeID)
}

func (m *Manager) SetConfigOption(ctx context.Context, sessionID string, configID string, value string) error {
	client, _, err := m.runtimeForSession(ctx, sessionID)
	if err != nil {
		return err
	}
	return client.SetConfigOption(ctx, sessionID, configID, value)
}

func (m *Manager) ReplyPermission(ctx context.Context, sessionID string, requestID string, optionID string) error {
	client, _, err := m.runtimeForSession(ctx, sessionID)
	if err != nil {
		return err
	}
	return client.ReplyPermission(ctx, sessionID, requestID, optionID)
}

func (m *Manager) PendingApprovals() []domain.ApprovalRequest {
	clients := m.startedClients()
	if len(clients) == 0 {
		return nil
	}
	out := make([]domain.ApprovalRequest, 0)
	for _, client := range clients {
		type pendingApprovals interface {
			PendingApprovals() []domain.ApprovalRequest
		}
		withApprovals, ok := client.(pendingApprovals)
		if !ok {
			continue
		}
		out = append(out, withApprovals.PendingApprovals()...)
	}
	return out
}

func (m *Manager) runtimeForSession(ctx context.Context, sessionID string) (runtime.Client, config.RuntimeID, error) {
	rid := m.resolveRuntimeID(sessionID, "")
	if rid == "" {
		return nil, "", fmt.Errorf("unknown runtime for session %q", sessionID)
	}
	return m.ensureRuntime(ctx, string(rid))
}

func (m *Manager) ensureRuntime(ctx context.Context, runtimeID string) (runtime.Client, config.RuntimeID, error) {
	if strings.TrimSpace(runtimeID) == "" {
		return nil, "", fmt.Errorf("missing runtime")
	}
	rid := config.RuntimeID(runtimeID)

	m.mu.RLock()
	rt := m.runtimes[rid]
	m.mu.RUnlock()
	if rt == nil {
		return nil, "", fmt.Errorf("runtime %q not configured", rid)
	}

	rt.mu.Lock()
	defer rt.mu.Unlock()
	if rt.client != nil {
		return rt.client, rid, nil
	}

	settings, err := m.resolveSettings(rid, rt.settings)
	if err != nil {
		return nil, "", err
	}

	client := acp.NewClient(acp.Config{
		RuntimeID: string(rid),
		Command:   settings.Command,
		Args:      settings.Args,
		CWD:       settings.CWD,
		Env:       settings.Env,
	})
	if err := client.Start(ctx); err != nil {
		return nil, "", fmt.Errorf("start runtime %q: %w", rid, err)
	}
	rt.client = client
	go m.forwardEvents(client.Events())
	return client, rid, nil
}

func (m *Manager) resolveSettings(id config.RuntimeID, settings config.RuntimeSettings) (config.RuntimeSettings, error) {
	if id == config.RuntimeClaudeCode {
		resolved, err := acpbin.Resolve(m.cfg, func(ev acpbin.ProgressEvent) {
			log.Printf("[runtime] %s: %d/%d bytes", ev.Phase, ev.BytesRead, ev.TotalBytes)
		})
		if err != nil {
			return config.RuntimeSettings{}, fmt.Errorf("resolve runtime %q: %w", id, err)
		}
		settings.Command = resolved
		settings, err = acpbin.InjectClaudeCodeExecutable(settings)
		if err != nil {
			return config.RuntimeSettings{}, fmt.Errorf("configure Claude Code executable wrapper: %w", err)
		}
		log.Printf("[runtime] resolved %s: %s", id, resolved)
	} else if id == config.RuntimeCodex {
		if config.IsRuntimeCommandOverridden(id) && settings.Command != "" {
			return settings, nil
		}
		resolved, err := codexbin.Resolve(m.cfg, func(ev codexbin.ProgressEvent) {
			log.Printf("[runtime] %s: %d/%d bytes", ev.Phase, ev.BytesRead, ev.TotalBytes)
		})
		if err != nil {
			return config.RuntimeSettings{}, fmt.Errorf("resolve runtime %q: %w", id, err)
		}
		settings.Command = resolved
		log.Printf("[runtime] resolved %s: %s", id, resolved)
	}

	if strings.TrimSpace(settings.Command) == "" {
		return config.RuntimeSettings{}, fmt.Errorf("runtime %q has no command configured", id)
	}
	return settings, nil
}

func (m *Manager) forwardEvents(events <-chan appwire.Event) {
	for {
		select {
		case <-m.done:
			return
		case ev, ok := <-events:
			if !ok {
				return
			}
			select {
			case <-m.done:
				return
			case m.events <- ev:
			}
		}
	}
}

func (m *Manager) resolveRuntimeID(sessionID string, runtimeID string) config.RuntimeID {
	if runtimeID != "" {
		return config.RuntimeID(runtimeID)
	}

	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.sessionRuntime[sessionID]
}

func (m *Manager) setSessionRuntime(sessionID string, runtimeID config.RuntimeID) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessionRuntime[sessionID] = runtimeID
}

func (m *Manager) startedClients() []runtime.Client {
	m.mu.RLock()
	runtimes := make([]*managedRuntime, 0, len(m.runtimes))
	for _, rt := range m.runtimes {
		runtimes = append(runtimes, rt)
	}
	m.mu.RUnlock()

	clients := make([]runtime.Client, 0, len(runtimes))
	for _, rt := range runtimes {
		rt.mu.Lock()
		client := rt.client
		rt.mu.Unlock()
		if client != nil {
			clients = append(clients, client)
		}
	}
	return clients
}

func (m *Manager) configuredRuntimeIDs() []config.RuntimeID {
	m.mu.RLock()
	ids := make([]config.RuntimeID, 0, len(m.runtimes))
	for id := range m.runtimes {
		ids = append(ids, id)
	}
	m.mu.RUnlock()

	sort.Slice(ids, func(i, j int) bool {
		return runtimeLabel(ids[i]) < runtimeLabel(ids[j])
	})
	return ids
}

func runtimeLabel(id config.RuntimeID) string {
	switch id {
	case config.RuntimeClaudeCode:
		return "Claude Code"
	case config.RuntimeCodex:
		return "Codex"
	default:
		return string(id)
	}
}

func runtimeConfigOptions(id config.RuntimeID) []acpprotocol.SessionConfigOption {
	switch id {
	case config.RuntimeClaudeCode:
		return []acpprotocol.SessionConfigOption{
			{
				ID:           "mode",
				Name:         "Approval Preset",
				Type:         "select",
				Category:     stringPtr("mode"),
				CurrentValue: "bypassPermissions",
				Options: acpprotocol.SessionConfigSelectOptions{
					Ungrouped: []acpprotocol.SessionConfigSelectOption{
						{
							Value:       "default",
							Name:        "Default",
							Description: stringPtr("Standard behavior, prompts for dangerous operations"),
						},
						{
							Value:       "acceptEdits",
							Name:        "Accept Edits",
							Description: stringPtr("Auto-accept file edit operations"),
						},
						{
							Value:       "plan",
							Name:        "Plan",
							Description: stringPtr("Planning mode, no actual tool execution"),
						},
						{
							Value:       "dontAsk",
							Name:        "Don't Ask",
							Description: stringPtr("Don't prompt for permissions, deny if not pre-approved"),
						},
						{
							Value:       "bypassPermissions",
							Name:        "Skip Perms",
							Description: stringPtr("Bypass all permission checks"),
						},
					},
				},
			},
		}
	case config.RuntimeCodex:
		return []acpprotocol.SessionConfigOption{
			{
				ID:           "mode",
				Name:         "Approval Preset",
				Type:         "select",
				Category:     stringPtr("mode"),
				CurrentValue: "read-only",
				Options: acpprotocol.SessionConfigSelectOptions{
					Ungrouped: []acpprotocol.SessionConfigSelectOption{
						{
							Value:       "read-only",
							Name:        "Read Only",
							Description: stringPtr("Codex can read files in the current workspace. Approval is required to edit files or access the internet."),
						},
						{
							Value:       "auto",
							Name:        "Default",
							Description: stringPtr("Codex can read and edit files in the current workspace, and run commands. Approval is required to access the internet or edit other files."),
						},
						{
							Value:       "full-access",
							Name:        "Full Access",
							Description: stringPtr("Codex can edit files outside this workspace and access the internet without asking for approval."),
						},
					},
				},
			},
		}
	default:
		return nil
	}
}

func stringPtr(value string) *string {
	return &value
}

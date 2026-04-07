package opencodehttp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/LaLanMo/muxagent-cli/internal/taskconfig"
	"github.com/LaLanMo/muxagent-cli/internal/taskexecutor"
)

const (
	defaultStartTimeout = 10 * time.Second
	directoryHeader     = "X-OpenCode-Directory"
)

var listenLinePattern = regexp.MustCompile(`opencode server listening on (https?://\S+)`)

type Executor struct {
	BinaryPath   string
	BaseURL      string
	HTTPClient   *http.Client
	StartTimeout time.Duration

	mu     sync.Mutex
	server *managedServer
}

type managedServer struct {
	cmd *exec.Cmd

	baseURL string

	stderrMu sync.Mutex
	stderr   bytes.Buffer

	done    chan struct{}
	waitErr error
}

type promptResponse struct {
	Structured json.RawMessage
	Usage      *taskexecutor.UsageSnapshot
}

type partState struct {
	Type     taskexecutor.MessagePartType
	SawDelta bool
}

func New(binaryPath string) *Executor {
	if strings.TrimSpace(binaryPath) == "" {
		binaryPath = "opencode"
	}
	return &Executor{
		BinaryPath:   binaryPath,
		StartTimeout: defaultStartTimeout,
	}
}

func (e *Executor) Execute(ctx context.Context, req taskexecutor.Request, progress func(taskexecutor.Progress)) (taskexecutor.Result, error) {
	if err := os.MkdirAll(req.ArtifactDir, 0o755); err != nil {
		return taskexecutor.Result{}, err
	}
	if strings.TrimSpace(req.SchemaPath) == "" {
		return taskexecutor.Result{}, errors.New("schema path is required")
	}
	if err := os.MkdirAll(filepath.Dir(req.SchemaPath), 0o755); err != nil {
		return taskexecutor.Result{}, err
	}
	outputPath := filepath.Join(req.ArtifactDir, "output.json")
	outputSchema := taskexecutor.BuildOutputSchema(req)
	if err := taskexecutor.WriteSchema(req.SchemaPath, outputSchema); err != nil {
		return taskexecutor.Result{}, err
	}

	baseURL, err := e.ensureBaseURL(req.WorkDir)
	if err != nil {
		return taskexecutor.Result{}, err
	}

	sessionID := taskexecutor.ResumeTargetSessionID(req)
	if sessionID == "" {
		sessionID, err = e.createSession(ctx, baseURL, req)
		if err != nil {
			return taskexecutor.Result{}, err
		}
	}
	emitProgress(progress, taskexecutor.Progress{SessionID: sessionID})

	streamCtx, streamCancel := context.WithCancel(context.Background())
	defer streamCancel()

	eventCh := make(chan taskexecutor.Progress, 32)
	streamReady := make(chan struct{})
	streamDone := make(chan error, 1)
	go func() {
		streamDone <- e.streamEvents(streamCtx, baseURL, req.WorkDir, sessionID, eventCh, streamReady)
		close(eventCh)
	}()
	var earlyStreamErr error
	select {
	case <-streamReady:
	case err := <-streamDone:
		if err != nil && !errors.Is(err, context.Canceled) {
			earlyStreamErr = err
		}
		streamDone = nil
	case <-time.After(2 * time.Second):
	}

	go func() {
		<-ctx.Done()
		if sessionID == "" {
			return
		}
		abortCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = e.abortSession(abortCtx, baseURL, req.WorkDir, sessionID)
	}()

	var (
		streamErr = earlyStreamErr
	)

	drainStream := func() {
		streamCancel()
		for eventCh != nil || streamDone != nil {
			select {
			case item, ok := <-eventCh:
				if !ok {
					eventCh = nil
					continue
				}
				emitProgress(progress, item)
			case err := <-streamDone:
				if err != nil && !errors.Is(err, context.Canceled) {
					streamErr = err
				}
				streamDone = nil
			}
		}
	}
	defer drainStream()

	type promptResult struct {
		resp promptResponse
		err  error
	}
	runPromptRound := func(promptText string) (promptResponse, error) {
		promptCtx, promptCancel := context.WithCancel(ctx)
		defer promptCancel()

		promptCh := make(chan promptResult, 1)
		go func() {
			resp, err := e.prompt(promptCtx, baseURL, req, sessionID, promptText)
			promptCh <- promptResult{resp: resp, err: err}
		}()

		for {
			select {
			case item, ok := <-eventCh:
				if !ok {
					eventCh = nil
					continue
				}
				emitProgress(progress, item)
			case err := <-streamDone:
				if err != nil && !errors.Is(err, context.Canceled) {
					streamErr = err
				}
				streamDone = nil
			case result := <-promptCh:
				if result.err != nil {
					if streamErr != nil {
						return promptResponse{}, fmt.Errorf("%w (event stream: %v)", result.err, streamErr)
					}
					return promptResponse{}, result.err
				}
				return result.resp, nil
			}
		}
	}

	final, err := runPromptRound(taskexecutor.AppendOutputContract(req))
	if err != nil {
		return taskexecutor.Result{}, err
	}
	emitUsageProgress(progress, sessionID, final.Usage)

	outputBytes, err := canonicalJSON(final.Structured)
	if err != nil {
		return taskexecutor.Result{}, fmt.Errorf("invalid opencode structured output: %w", err)
	}
	outputBytes, err = normalizeResultPayload(req, outputBytes)
	if err != nil {
		return taskexecutor.Result{}, err
	}
	result, err := taskexecutor.ParseOutputEnvelope(req, outputBytes)
	if err != nil {
		artifactPaths, artifactErr := userArtifactPaths(req.ArtifactDir)
		if artifactErr != nil {
			return taskexecutor.Result{}, artifactErr
		}
		emitProgress(progress, taskexecutor.Progress{
			SessionID: sessionID,
			Message:   fmt.Sprintf("repairing structured output: %v", err),
		})
		repaired, repairErr := runPromptRound(taskexecutor.BuildSchemaRepairPrompt(req, err, outputBytes, artifactPaths))
		if repairErr != nil {
			return taskexecutor.Result{}, repairErr
		}
		emitUsageProgress(progress, sessionID, repaired.Usage)

		outputBytes, err = canonicalJSON(repaired.Structured)
		if err != nil {
			return taskexecutor.Result{}, fmt.Errorf("invalid opencode repair structured output: %w", err)
		}
		outputBytes, err = normalizeResultPayload(req, outputBytes)
		if err != nil {
			return taskexecutor.Result{}, err
		}
		result, err = taskexecutor.ParseOutputEnvelope(req, outputBytes)
		if err != nil {
			return taskexecutor.Result{}, fmt.Errorf("invalid opencode structured output after schema repair: %w", err)
		}
	}
	if err := os.WriteFile(outputPath, outputBytes, 0o644); err != nil {
		return taskexecutor.Result{}, err
	}
	result.SessionID = sessionID
	return result, nil
}

func (e *Executor) Close() error {
	e.mu.Lock()
	server := e.server
	e.server = nil
	e.mu.Unlock()
	if server == nil {
		return nil
	}
	return server.Close()
}

func (e *Executor) ensureBaseURL(workDir string) (string, error) {
	if strings.TrimSpace(e.BaseURL) != "" {
		return strings.TrimRight(strings.TrimSpace(e.BaseURL), "/"), nil
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	if e.server != nil && !e.server.exited() {
		return e.server.baseURL, nil
	}
	if e.server != nil {
		_ = e.server.Close()
		e.server = nil
	}

	server, err := startServer(e.BinaryPath, workDir, e.StartTimeout)
	if err != nil {
		return "", err
	}
	e.server = server
	return server.baseURL, nil
}

func (e *Executor) createSession(ctx context.Context, baseURL string, req taskexecutor.Request) (string, error) {
	var resp struct {
		ID string `json:"id"`
	}
	payload := map[string]any{
		"title":      fmt.Sprintf("muxagent task %s/%s", req.Task.ID, req.NodeRun.NodeName),
		"permission": defaultPermissionRules(),
	}
	if err := e.doJSON(ctx, http.MethodPost, baseURL, "/session", req.WorkDir, payload, &resp); err != nil {
		return "", fmt.Errorf("create opencode session: %w", err)
	}
	if strings.TrimSpace(resp.ID) == "" {
		return "", errors.New("create opencode session: missing session id")
	}
	return resp.ID, nil
}

func (e *Executor) prompt(ctx context.Context, baseURL string, req taskexecutor.Request, sessionID, promptText string) (promptResponse, error) {
	body := map[string]any{
		"agent": "build",
		"format": map[string]any{
			"type":   "json_schema",
			"schema": taskexecutor.BuildOutputSchema(req),
		},
		"parts": []map[string]any{
			{
				"type": "text",
				"text": promptText,
			},
		},
	}
	path := fmt.Sprintf("/session/%s/message", sessionID)
	data, err := e.doRequest(ctx, http.MethodPost, baseURL, path, req.WorkDir, body)
	if err != nil {
		return promptResponse{}, fmt.Errorf("send opencode prompt: %w", err)
	}
	return parsePromptResponse(data)
}

func (e *Executor) abortSession(ctx context.Context, baseURL, workDir, sessionID string) error {
	path := fmt.Sprintf("/session/%s/abort", sessionID)
	_, err := e.doRequest(ctx, http.MethodPost, baseURL, path, workDir, nil)
	return err
}

func (e *Executor) replyPermission(ctx context.Context, baseURL, workDir, requestID, reply, message string) error {
	path := fmt.Sprintf("/permission/%s/reply", requestID)
	payload := map[string]any{"reply": reply}
	if strings.TrimSpace(message) != "" {
		payload["message"] = message
	}
	_, err := e.doRequest(ctx, http.MethodPost, baseURL, path, workDir, payload)
	return err
}

func (e *Executor) streamEvents(
	ctx context.Context,
	baseURL, workDir, sessionID string,
	out chan<- taskexecutor.Progress,
	ready chan<- struct{},
) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/event", nil)
	if err != nil {
		return err
	}
	req.Header.Set(directoryHeader, workDir)
	req.Header.Set("Accept", "text/event-stream")

	resp, err := e.client().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
		return fmt.Errorf("subscribe opencode events: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	close(ready)

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 2*1024*1024)

	var (
		dataLines []string
		partTypes = map[string]partState{}
		msgRoles  = map[string]taskexecutor.MessageRole{}
	)

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if len(dataLines) > 0 {
				raw := strings.Join(dataLines, "\n")
				dataLines = dataLines[:0]
				progress, reply, err := e.parseEvent(raw, baseURL, workDir, sessionID, partTypes, msgRoles)
				if err != nil {
					return err
				}
				if reply != nil {
					replyCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
					err = e.replyPermission(replyCtx, baseURL, workDir, reply.RequestID, reply.Reply, reply.Message)
					cancel()
					if err != nil {
						out <- taskexecutor.Progress{
							SessionID: sessionID,
							Message:   fmt.Sprintf("permission reply failed: %v", err),
						}
					}
				}
				if progress.Message != "" || progress.SessionID != "" || len(progress.Events) > 0 {
					out <- progress
				}
			}
			continue
		}
		if strings.HasPrefix(line, "data:") {
			dataLines = append(dataLines, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}
	if err := scanner.Err(); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	return nil
}

type permissionReply struct {
	RequestID string
	Reply     string
	Message   string
}

func (e *Executor) parseEvent(
	raw, baseURL, workDir, sessionID string,
	partTypes map[string]partState,
	msgRoles map[string]taskexecutor.MessageRole,
) (taskexecutor.Progress, *permissionReply, error) {
	var payload map[string]any
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return taskexecutor.Progress{}, nil, fmt.Errorf("invalid opencode event: %w", err)
	}
	eventType := asString(payload["type"])
	properties := asMap(payload["properties"])

	switch eventType {
	case "server.connected", "server.heartbeat":
		return taskexecutor.Progress{}, nil, nil
	case "message.updated":
		info := asMap(properties["info"])
		if asString(info["sessionID"]) != sessionID {
			return taskexecutor.Progress{}, nil, nil
		}
		if msgID := asString(info["id"]); msgID != "" {
			msgRoles[msgID] = normalizeMessageRole(asString(info["role"]))
		}
		return taskexecutor.Progress{}, nil, nil
	case "message.part.delta":
		if asString(properties["sessionID"]) != sessionID || asString(properties["field"]) != "text" {
			return taskexecutor.Progress{}, nil, nil
		}
		partID := asString(properties["partID"])
		meta := partTypes[partID]
		meta.SawDelta = true
		if meta.Type == "" {
			meta.Type = taskexecutor.MessagePartTypeText
		}
		partTypes[partID] = meta
		event := taskexecutor.StreamEvent{
			Kind:      taskexecutor.StreamEventKindMessage,
			SessionID: sessionID,
			Message: &taskexecutor.MessagePart{
				MessageID: asString(properties["messageID"]),
				PartID:    partID,
				Role:      messageRoleFor(msgRoles, asString(properties["messageID"])),
				Type:      meta.Type,
				Text:      asString(properties["delta"]),
				Append:    true,
			},
		}
		return taskexecutor.Progress{
			SessionID: sessionID,
			Message:   event.Summary(),
			Events:    []taskexecutor.StreamEvent{event},
		}, nil, nil
	case "message.part.updated":
		part := asMap(properties["part"])
		if asString(part["sessionID"]) != sessionID {
			return taskexecutor.Progress{}, nil, nil
		}
		return progressFromPart(sessionID, part, partTypes, msgRoles), nil, nil
	case "todo.updated":
		if asString(properties["sessionID"]) != sessionID {
			return taskexecutor.Progress{}, nil, nil
		}
		steps := make([]taskexecutor.PlanStep, 0, len(asSlice(properties["todos"])))
		for _, rawTodo := range asSlice(properties["todos"]) {
			todo := asMap(rawTodo)
			steps = append(steps, taskexecutor.PlanStep{
				Text:   asString(todo["content"]),
				Status: normalizePlanStatus(asString(todo["status"])),
			})
		}
		event := taskexecutor.StreamEvent{
			Kind:      taskexecutor.StreamEventKindPlan,
			SessionID: sessionID,
			Plan: &taskexecutor.PlanSnapshot{
				PlanID: sessionID,
				Steps:  steps,
			},
		}
		return taskexecutor.Progress{
			SessionID: sessionID,
			Message:   event.Summary(),
			Events:    []taskexecutor.StreamEvent{event},
		}, nil, nil
	case "permission.asked":
		if asString(properties["sessionID"]) != sessionID {
			return taskexecutor.Progress{}, nil, nil
		}
		requestID := asString(properties["id"])
		if requestID == "" {
			return taskexecutor.Progress{}, nil, nil
		}
		reply := &permissionReply{
			RequestID: requestID,
			Reply:     "once",
		}
		permission := asString(properties["permission"])
		if permission == "question" {
			reply.Reply = "reject"
			reply.Message = "task clarification is handled by muxagent"
		}
		return taskexecutor.Progress{
			SessionID: sessionID,
			Message:   fmt.Sprintf("permission %s %s", permission, strings.ToLower(reply.Reply)),
		}, reply, nil
	case "session.status":
		if asString(properties["sessionID"]) != sessionID {
			return taskexecutor.Progress{}, nil, nil
		}
		status := asMap(properties["status"])
		if asString(status["type"]) == "retry" {
			message := strings.TrimSpace(asString(status["message"]))
			if message == "" {
				message = "retrying"
			}
			event := taskexecutor.StreamEvent{
				Kind:      taskexecutor.StreamEventKindMessage,
				SessionID: sessionID,
				Message: &taskexecutor.MessagePart{
					Role: taskexecutor.MessageRoleAssistant,
					Type: taskexecutor.MessagePartTypeReasoning,
					Text: message,
				},
			}
			return taskexecutor.Progress{
				SessionID: sessionID,
				Message:   event.Summary(),
				Events:    []taskexecutor.StreamEvent{event},
			}, nil, nil
		}
		return taskexecutor.Progress{}, nil, nil
	case "session.error":
		if sid := asString(properties["sessionID"]); sid != "" && sid != sessionID {
			return taskexecutor.Progress{}, nil, nil
		}
		message := errorMessage(asMap(properties["error"]))
		if message == "" {
			message = raw
		}
		return taskexecutor.Progress{
			SessionID: sessionID,
			Message:   message,
			Events: []taskexecutor.StreamEvent{{
				Kind:      taskexecutor.StreamEventKindRaw,
				SessionID: sessionID,
				Raw:       message,
			}},
		}, nil, nil
	default:
		return taskexecutor.Progress{}, nil, nil
	}
}

func progressFromPart(
	sessionID string,
	part map[string]any,
	partTypes map[string]partState,
	msgRoles map[string]taskexecutor.MessageRole,
) taskexecutor.Progress {
	partID := asString(part["id"])
	messageID := asString(part["messageID"])
	switch asString(part["type"]) {
	case "text", "reasoning":
		meta := partTypes[partID]
		meta.Type = taskexecutor.MessagePartTypeText
		if asString(part["type"]) == "reasoning" {
			meta.Type = taskexecutor.MessagePartTypeReasoning
		}
		partTypes[partID] = meta
		if meta.SawDelta {
			return taskexecutor.Progress{}
		}
		text := asString(part["text"])
		if text == "" {
			return taskexecutor.Progress{}
		}
		event := taskexecutor.StreamEvent{
			Kind:      taskexecutor.StreamEventKindMessage,
			SessionID: sessionID,
			Message: &taskexecutor.MessagePart{
				MessageID: messageID,
				PartID:    partID,
				Role:      messageRoleFor(msgRoles, messageID),
				Type:      meta.Type,
				Text:      text,
			},
		}
		return taskexecutor.Progress{
			SessionID: sessionID,
			Message:   event.Summary(),
			Events:    []taskexecutor.StreamEvent{event},
		}
	case "tool":
		event := taskexecutor.StreamEvent{
			Kind:      taskexecutor.StreamEventKindTool,
			SessionID: sessionID,
			Tool:      buildToolCall(part),
		}
		return taskexecutor.Progress{
			SessionID: sessionID,
			Message:   event.Summary(),
			Events:    []taskexecutor.StreamEvent{event},
		}
	case "step-finish":
		if usage := usageFromTokens(asMap(part["tokens"])); usage != nil {
			event := taskexecutor.StreamEvent{
				Kind:      taskexecutor.StreamEventKindUsage,
				SessionID: sessionID,
				Usage:     usage,
			}
			return taskexecutor.Progress{
				SessionID: sessionID,
				Message:   event.Summary(),
				Events:    []taskexecutor.StreamEvent{event},
			}
		}
	}
	return taskexecutor.Progress{}
}

func messageRoleFor(msgRoles map[string]taskexecutor.MessageRole, messageID string) taskexecutor.MessageRole {
	if role, ok := msgRoles[messageID]; ok {
		return role
	}
	return taskexecutor.MessageRoleAssistant
}

func normalizeMessageRole(raw string) taskexecutor.MessageRole {
	switch strings.TrimSpace(strings.ToLower(raw)) {
	case string(taskexecutor.MessageRoleUser):
		return taskexecutor.MessageRoleUser
	case string(taskexecutor.MessageRoleAssistant):
		return taskexecutor.MessageRoleAssistant
	default:
		return taskexecutor.MessageRoleAssistant
	}
}

func buildToolCall(part map[string]any) *taskexecutor.ToolCall {
	state := asMap(part["state"])
	name := asString(part["tool"])
	input := asMap(state["input"])
	tool := &taskexecutor.ToolCall{
		CallID:        asString(part["callID"]),
		Name:          name,
		Kind:          classifyTool(name),
		Title:         asString(state["title"]),
		Status:        normalizeToolStatus(asString(state["status"])),
		InputSummary:  summarizeToolInput(name, input),
		RawInputJSON:  mustJSON(input),
		RawOutputJSON: mustJSON(state["metadata"]),
		OutputText:    asString(state["output"]),
		ErrorText:     asString(state["error"]),
		Paths:         extractPaths(name, input, asMap(state["metadata"])),
	}
	return tool
}

func parsePromptResponse(data []byte) (promptResponse, error) {
	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		return promptResponse{}, fmt.Errorf("invalid opencode prompt response: %w", err)
	}
	info := asMap(payload["info"])
	if len(info) == 0 {
		return promptResponse{}, errors.New("opencode prompt response missing info")
	}
	if errorInfo := asMap(info["error"]); len(errorInfo) > 0 {
		return promptResponse{}, errors.New(errorMessage(errorInfo))
	}
	structured := info["structured"]
	if structured == nil {
		return promptResponse{}, errors.New("opencode prompt response missing structured output")
	}
	structuredBytes, err := json.Marshal(structured)
	if err != nil {
		return promptResponse{}, err
	}
	return promptResponse{
		Structured: structuredBytes,
		Usage:      usageFromTokens(asMap(info["tokens"])),
	}, nil
}

func errorMessage(payload map[string]any) string {
	name := strings.TrimSpace(asString(payload["name"]))
	message := strings.TrimSpace(asString(payload["message"]))
	switch {
	case name != "" && message != "":
		return fmt.Sprintf("opencode %s: %s", name, message)
	case message != "":
		return message
	case name != "":
		return name
	default:
		return "opencode request failed"
	}
}

func (e *Executor) doJSON(ctx context.Context, method, baseURL, path, workDir string, payload any, result any) error {
	data, err := e.doRequest(ctx, method, baseURL, path, workDir, payload)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, result)
}

func (e *Executor) doRequest(ctx context.Context, method, baseURL, path, workDir string, payload any) ([]byte, error) {
	var body io.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		body = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(baseURL, "/")+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set(directoryHeader, workDir)
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := e.client().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	return data, nil
}

func (e *Executor) client() *http.Client {
	if e.HTTPClient != nil {
		return e.HTTPClient
	}
	return http.DefaultClient
}

func startServer(binaryPath, workDir string, timeout time.Duration) (*managedServer, error) {
	if timeout <= 0 {
		timeout = defaultStartTimeout
	}

	cmd := exec.Command(binaryPath, "serve", "--hostname", "127.0.0.1", "--port", "0")
	cmd.Dir = workDir

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start opencode serve: %w", err)
	}

	server := &managedServer{
		cmd:  cmd,
		done: make(chan struct{}),
	}
	go func() {
		_, _ = io.Copy(&server.stderr, stderr)
	}()
	go func() {
		server.waitErr = cmd.Wait()
		close(server.done)
	}()

	lines := make(chan string, 16)
	scanErr := make(chan error, 1)
	go func() {
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 0, 64*1024), 2*1024*1024)
		for scanner.Scan() {
			lines <- scanner.Text()
		}
		scanErr <- scanner.Err()
		close(lines)
	}()

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	for {
		select {
		case line, ok := <-lines:
			if !ok {
				select {
				case err := <-scanErr:
					if err != nil {
						return nil, fmt.Errorf("read opencode serve stdout: %w", err)
					}
				default:
				}
				return nil, fmt.Errorf("opencode serve exited before reporting address: %s", strings.TrimSpace(server.stderr.String()))
			}
			match := listenLinePattern.FindStringSubmatch(line)
			if len(match) == 2 {
				server.baseURL = strings.TrimRight(match[1], "/")
				return server, nil
			}
		case <-server.done:
			return nil, fmt.Errorf("opencode serve exited: %v %s", server.waitErr, strings.TrimSpace(server.stderr.String()))
		case <-timer.C:
			_ = server.Close()
			return nil, fmt.Errorf("timed out waiting for opencode serve startup")
		}
	}
}

func (s *managedServer) Close() error {
	if s == nil || s.cmd == nil || s.cmd.Process == nil {
		return nil
	}
	if s.exited() {
		return s.waitErr
	}
	if err := s.cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return err
	}
	<-s.done
	if s.waitErr != nil && !errors.Is(s.waitErr, exec.ErrNotFound) {
		return nil
	}
	return nil
}

func (s *managedServer) exited() bool {
	select {
	case <-s.done:
		return true
	default:
		return false
	}
}

func emitProgress(progress func(taskexecutor.Progress), item taskexecutor.Progress) {
	if progress == nil {
		return
	}
	if item.Message == "" && item.SessionID == "" && len(item.Events) == 0 {
		return
	}
	progress(item)
}

func emitUsageProgress(progress func(taskexecutor.Progress), sessionID string, usage *taskexecutor.UsageSnapshot) {
	if usage == nil {
		return
	}
	emitProgress(progress, taskexecutor.Progress{
		SessionID: sessionID,
		Events: []taskexecutor.StreamEvent{{
			Kind:      taskexecutor.StreamEventKindUsage,
			SessionID: sessionID,
			Usage:     usage,
		}},
	})
}

func defaultPermissionRules() []map[string]string {
	return []map[string]string{
		{`permission`: `*`, `pattern`: `*`, `action`: `allow`},
		{`permission`: `question`, `pattern`: `*`, `action`: `deny`},
		{`permission`: `plan_enter`, `pattern`: `*`, `action`: `deny`},
		{`permission`: `plan_exit`, `pattern`: `*`, `action`: `deny`},
	}
}

func normalizeToolStatus(status string) taskexecutor.ToolStatus {
	switch strings.TrimSpace(status) {
	case "pending":
		return taskexecutor.ToolStatusPending
	case "running":
		return taskexecutor.ToolStatusInProgress
	case "completed":
		return taskexecutor.ToolStatusCompleted
	case "error":
		return taskexecutor.ToolStatusFailed
	default:
		return taskexecutor.ToolStatusPending
	}
}

func normalizePlanStatus(status string) string {
	switch strings.TrimSpace(status) {
	case "in_progress":
		return "in_progress"
	case "completed":
		return "completed"
	case "cancelled":
		return "cancelled"
	default:
		return "pending"
	}
}

func classifyTool(name string) taskexecutor.ToolKind {
	normalized := strings.ToLower(strings.TrimSpace(name))
	switch normalized {
	case "bash", "shell":
		return taskexecutor.ToolKindShell
	case "read", "list", "todoread":
		return taskexecutor.ToolKindRead
	case "edit", "patch", "multiedit":
		return taskexecutor.ToolKindEdit
	case "write":
		return taskexecutor.ToolKindWrite
	case "webfetch", "fetch":
		return taskexecutor.ToolKindFetch
	case "websearch", "grep", "glob", "codesearch":
		return taskexecutor.ToolKindSearch
	case "structuredoutput":
		return taskexecutor.ToolKindStructuredOutput
	default:
		return taskexecutor.ToolKindOther
	}
}

func summarizeToolInput(name string, input map[string]any) string {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "bash", "shell":
		return firstNonEmpty(asString(input["command"]), asString(input["cmd"]))
	case "read":
		return firstNonEmpty(asString(input["filePath"]), asString(input["path"]))
	case "write", "edit", "patch", "multiedit":
		return firstNonEmpty(asString(input["filePath"]), asString(input["path"]))
	case "websearch":
		return firstNonEmpty(asString(input["query"]), asString(input["q"]))
	case "webfetch":
		return firstNonEmpty(asString(input["url"]), asString(input["href"]))
	}
	for _, key := range []string{"filePath", "path", "query", "url", "command", "cmd"} {
		if value := asString(input[key]); value != "" {
			return value
		}
	}
	return ""
}

func extractPaths(name string, input, metadata map[string]any) []string {
	var paths []string
	for _, source := range []map[string]any{input, metadata} {
		for _, key := range []string{"filePath", "path"} {
			if value := strings.TrimSpace(asString(source[key])); value != "" {
				paths = append(paths, value)
			}
		}
	}
	return dedupeStrings(paths)
}

func usageFromTokens(tokens map[string]any) *taskexecutor.UsageSnapshot {
	if len(tokens) == 0 {
		return nil
	}
	cache := asMap(tokens["cache"])
	usage := &taskexecutor.UsageSnapshot{
		InputTokens:       asInt64(tokens["input"]),
		CachedInputTokens: asInt64(cache["read"]),
		OutputTokens:      asInt64(tokens["output"]),
		TotalTokens:       asInt64(tokens["total"]),
	}
	if usage.TotalTokens == 0 {
		usage.TotalTokens = usage.InputTokens + usage.OutputTokens
	}
	return usage
}

func canonicalJSON(input []byte) ([]byte, error) {
	var payload any
	if err := json.Unmarshal(input, &payload); err != nil {
		return nil, err
	}
	return json.Marshal(payload)
}

func normalizeResultPayload(req taskexecutor.Request, outputBytes []byte) ([]byte, error) {
	var payload map[string]any
	if err := json.Unmarshal(outputBytes, &payload); err != nil {
		return nil, err
	}
	if asString(payload["kind"]) != string(taskexecutor.ResultKindResult) {
		return outputBytes, nil
	}

	result, ok := payload["result"].(map[string]any)
	if !ok {
		return outputBytes, nil
	}
	payload["result"] = normalizeObjectForSchema(&req.ResultSchema, result)
	return json.Marshal(payload)
}

func userArtifactPaths(artifactDir string) ([]string, error) {
	entries, err := os.ReadDir(artifactDir)
	if err != nil {
		return nil, err
	}
	paths := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		switch entry.Name() {
		case "input.md", "output.json":
			continue
		}
		paths = append(paths, filepath.Join(artifactDir, entry.Name()))
	}
	return dedupeStrings(paths), nil
}

func normalizeValueForSchema(schema *taskconfig.JSONSchema, value any) any {
	if schema == nil {
		return value
	}
	if len(schema.OneOf) > 0 {
		for _, inner := range schema.OneOf {
			normalized := normalizeValueForSchema(inner, value)
			if err := taskconfig.ValidateValue(inner, normalized); err == nil {
				return normalized
			}
		}
		return value
	}
	switch schema.Type {
	case "object":
		obj, ok := value.(map[string]any)
		if !ok {
			return value
		}
		return normalizeObjectForSchema(schema, obj)
	case "array":
		items, ok := value.([]any)
		if !ok || schema.Items == nil {
			return value
		}
		normalized := make([]any, 0, len(items))
		for _, item := range items {
			normalized = append(normalized, normalizeValueForSchema(schema.Items, item))
		}
		return normalized
	default:
		return value
	}
}

func normalizeObjectForSchema(schema *taskconfig.JSONSchema, obj map[string]any) map[string]any {
	if schema == nil {
		return obj
	}
	normalized := make(map[string]any, len(obj))
	for key, value := range obj {
		childSchema, known := schema.Properties[key]
		switch {
		case known:
			normalized[key] = normalizeValueForSchema(childSchema, value)
		case schema.AdditionalProperties == nil || *schema.AdditionalProperties:
			normalized[key] = value
		}
	}
	return normalized
}

func mustJSON(value any) string {
	if value == nil {
		return ""
	}
	data, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	return string(data)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func dedupeStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func asString(value any) string {
	text, _ := value.(string)
	return text
}

func asMap(value any) map[string]any {
	item, _ := value.(map[string]any)
	return item
}

func asSlice(value any) []any {
	items, _ := value.([]any)
	return items
}

func asInt64(value any) int64 {
	switch typed := value.(type) {
	case int:
		return int64(typed)
	case int64:
		return typed
	case float64:
		return int64(typed)
	default:
		return 0
	}
}

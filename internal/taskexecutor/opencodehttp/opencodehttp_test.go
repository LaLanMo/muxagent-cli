package opencodehttp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LaLanMo/muxagent-cli/internal/taskconfig"
	"github.com/LaLanMo/muxagent-cli/internal/taskdomain"
	"github.com/LaLanMo/muxagent-cli/internal/taskexecutor"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExecutorParsesResultAndProgress(t *testing.T) {
	server := newFakeOpenCodeServer(t)
	server.onPrompt = func(sessionID string, body map[string]any, w http.ResponseWriter, _ *http.Request) {
		server.publish(map[string]any{
			"type": "message.part.delta",
			"properties": map[string]any{
				"sessionID": sessionID,
				"messageID": "msg-1",
				"partID":    "part-text",
				"field":     "text",
				"delta":     "Working through the repo",
			},
		})
		server.publish(map[string]any{
			"type": "message.part.updated",
			"properties": map[string]any{
				"part": map[string]any{
					"id":        "part-tool",
					"sessionID": sessionID,
					"messageID": "msg-1",
					"type":      "tool",
					"callID":    "call-1",
					"tool":      "bash",
					"state": map[string]any{
						"status": "running",
						"title":  "bash",
						"input": map[string]any{
							"command": "pwd",
						},
					},
				},
			},
		})
		server.publish(map[string]any{
			"type": "todo.updated",
			"properties": map[string]any{
				"sessionID": sessionID,
				"todos": []map[string]any{
					{"content": "Inspect repo", "status": "completed"},
					{"content": "Write patch", "status": "in_progress"},
				},
			},
		})
		writeJSON(w, assistantResponse(sessionID, map[string]any{
			"kind":   "result",
			"result": map[string]any{"file_paths": []string{"/tmp/artifact.md"}},
		}))
	}
	defer server.close()

	executor := &Executor{BaseURL: server.url(), HTTPClient: server.client()}
	req := requestFixture(t.TempDir())
	var progress []taskexecutor.Progress

	result, err := executor.Execute(context.Background(), req, func(item taskexecutor.Progress) {
		progress = append(progress, item)
	})
	require.NoError(t, err)
	assert.Equal(t, "session-1", result.SessionID)
	assert.Equal(t, taskexecutor.ResultKindResult, result.Kind)
	assert.Equal(t, []any{"/tmp/artifact.md"}, result.Result["file_paths"])
	require.NotEmpty(t, progress)
	assert.Equal(t, "session-1", progress[0].SessionID)

	var hasMessage bool
	var hasTool bool
	var hasPlan bool
	var hasUsage bool
	for _, item := range progress {
		for _, event := range item.Events {
			switch event.Kind {
			case taskexecutor.StreamEventKindMessage:
				hasMessage = true
			case taskexecutor.StreamEventKindTool:
				hasTool = true
			case taskexecutor.StreamEventKindPlan:
				hasPlan = true
			case taskexecutor.StreamEventKindUsage:
				hasUsage = true
			}
		}
	}
	assert.True(t, hasMessage)
	assert.True(t, hasTool)
	assert.True(t, hasPlan)
	assert.True(t, hasUsage)

	require.Len(t, server.sessionCreates(), 1)
	require.Len(t, server.prompts(), 1)
	prompt := server.prompts()[0]
	assert.Equal(t, "session-1", prompt.SessionID)
	assert.Equal(t, req.WorkDir, prompt.Directory)
	assert.Equal(t, "build", asString(prompt.Body["agent"]))
	format := asMap(prompt.Body["format"])
	assert.Equal(t, "json_schema", asString(format["type"]))
	parts := asSlice(prompt.Body["parts"])
	require.Len(t, parts, 1)
	assert.Contains(t, asString(asMap(parts[0])["text"]), "Output contract:")
}

func TestExecutorParsesClarificationAndResumesSameSession(t *testing.T) {
	server := newFakeOpenCodeServer(t)
	server.onPrompt = func(sessionID string, _ map[string]any, w http.ResponseWriter, _ *http.Request) {
		count := len(server.prompts())
		if count == 1 {
			writeJSON(w, assistantResponse(sessionID, map[string]any{
				"kind":   "clarification",
				"result": nil,
				"clarification": map[string]any{
					"questions": []map[string]any{
						{
							"question":       "Need input",
							"why_it_matters": "Blocks implementation",
							"multi_select":   false,
							"options": []map[string]any{
								{"label": "A", "description": "Option A"},
								{"label": "B", "description": "Option B"},
							},
						},
					},
				},
			}))
			return
		}
		writeJSON(w, assistantResponse(sessionID, map[string]any{
			"kind":   "result",
			"result": map[string]any{"file_paths": []string{"/tmp/done.md"}},
		}))
	}
	defer server.close()

	executor := &Executor{BaseURL: server.url(), HTTPClient: server.client()}

	firstReq := requestFixture(t.TempDir())
	first, err := executor.Execute(context.Background(), firstReq, nil)
	require.NoError(t, err)
	assert.Equal(t, "session-1", first.SessionID)
	assert.Equal(t, taskexecutor.ResultKindClarification, first.Kind)
	require.NotNil(t, first.Clarification)

	secondReq := requestFixture(t.TempDir())
	secondReq.NodeRun.SessionID = first.SessionID
	secondReq.NodeRun.Clarifications = []taskdomain.ClarificationExchange{
		{
			Request: *first.Clarification,
			Response: &taskdomain.ClarificationResponse{
				Answers: []taskdomain.ClarificationAnswer{{Selected: "A"}},
			},
		},
	}
	second, err := executor.Execute(context.Background(), secondReq, nil)
	require.NoError(t, err)
	assert.Equal(t, "session-1", second.SessionID)
	assert.Equal(t, taskexecutor.ResultKindResult, second.Kind)
	assert.Equal(t, []any{"/tmp/done.md"}, second.Result["file_paths"])

	require.Len(t, server.sessionCreates(), 1)
	require.Len(t, server.prompts(), 2)
	assert.Equal(t, "session-1", server.prompts()[0].SessionID)
	assert.Equal(t, "session-1", server.prompts()[1].SessionID)
}

func TestExecutorAutoRepliesPermissionRequests(t *testing.T) {
	server := newFakeOpenCodeServer(t)
	server.onPrompt = func(sessionID string, _ map[string]any, w http.ResponseWriter, _ *http.Request) {
		server.publish(map[string]any{
			"type": "permission.asked",
			"properties": map[string]any{
				"id":         "perm-1",
				"sessionID":  sessionID,
				"permission": "bash",
			},
		})
		reply := server.waitForPermissionReply(t)
		assert.Equal(t, "perm-1", reply.RequestID)
		assert.Equal(t, "once", reply.Reply)
		writeJSON(w, assistantResponse(sessionID, map[string]any{
			"kind":   "result",
			"result": map[string]any{"file_paths": []string{"/tmp/approved.md"}},
		}))
	}
	defer server.close()

	executor := &Executor{BaseURL: server.url(), HTTPClient: server.client()}
	_, err := executor.Execute(context.Background(), requestFixture(t.TempDir()), nil)
	require.NoError(t, err)
}

func TestExecutorRepromptsWhenStructuredOutputMissesRequiredFilePaths(t *testing.T) {
	server := newFakeOpenCodeServer(t)
	defer server.close()

	req := requestFixture(t.TempDir())
	writtenPath := filepath.Join(req.ArtifactDir, "summary.md")
	server.onPrompt = func(sessionID string, body map[string]any, w http.ResponseWriter, _ *http.Request) {
		switch len(server.prompts()) {
		case 1:
			require.NoError(t, os.WriteFile(writtenPath, []byte("# Summary\n"), 0o644))
			writeJSON(w, assistantResponse(sessionID, map[string]any{
				"kind":   "result",
				"result": map[string]any{},
			}))
		case 2:
			parts := asSlice(body["parts"])
			require.Len(t, parts, 1)
			promptText := asString(asMap(parts[0])["text"])
			assert.Contains(t, promptText, "$.file_paths is required")
			assert.Contains(t, promptText, writtenPath)
			writeJSON(w, assistantResponse(sessionID, map[string]any{
				"kind":   "result",
				"result": map[string]any{"file_paths": []string{writtenPath}},
			}))
		default:
			t.Fatalf("unexpected prompt count %d", len(server.prompts()))
		}
	}

	executor := &Executor{BaseURL: server.url(), HTTPClient: server.client()}
	result, err := executor.Execute(context.Background(), req, nil)
	require.NoError(t, err)
	assert.Equal(t, []any{writtenPath}, result.Result["file_paths"])
	require.Len(t, server.prompts(), 2)
}

func TestExecutorPrunesExtraResultFieldsToMatchSchema(t *testing.T) {
	server := newFakeOpenCodeServer(t)
	defer server.close()

	req := requestFixture(t.TempDir())
	server.onPrompt = func(sessionID string, _ map[string]any, w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, assistantResponse(sessionID, map[string]any{
			"kind": "result",
			"result": map[string]any{
				"file_paths": []string{"/tmp/result.md"},
				"summary":    "extra field from model",
			},
		}))
	}

	executor := &Executor{BaseURL: server.url(), HTTPClient: server.client()}
	result, err := executor.Execute(context.Background(), req, nil)
	require.NoError(t, err)
	assert.Equal(t, []any{"/tmp/result.md"}, result.Result["file_paths"])
	_, hasSummary := result.Result["summary"]
	assert.False(t, hasSummary)
}

func TestExecutorFailsWhenSchemaRepairStillReturnsInvalidOutput(t *testing.T) {
	server := newFakeOpenCodeServer(t)
	defer server.close()

	req := requestFixture(t.TempDir())
	server.onPrompt = func(sessionID string, body map[string]any, w http.ResponseWriter, _ *http.Request) {
		if len(server.prompts()) == 2 {
			parts := asSlice(body["parts"])
			require.Len(t, parts, 1)
			promptText := asString(asMap(parts[0])["text"])
			assert.Contains(t, promptText, "$.file_paths is required")
		}
		writeJSON(w, assistantResponse(sessionID, map[string]any{
			"kind":   "result",
			"result": map[string]any{},
		}))
	}

	executor := &Executor{BaseURL: server.url(), HTTPClient: server.client()}
	_, err := executor.Execute(context.Background(), req, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "after schema repair")
	assert.Contains(t, err.Error(), "$.file_paths is required")
	require.Len(t, server.prompts(), 2)
}

func TestExecutorRendersSchemaRepairPromptAsUserMessage(t *testing.T) {
	server := newFakeOpenCodeServer(t)
	defer server.close()

	req := requestFixture(t.TempDir())
	writtenPath := filepath.Join(req.ArtifactDir, "summary.md")
	var progress []taskexecutor.Progress
	server.onPrompt = func(sessionID string, body map[string]any, w http.ResponseWriter, _ *http.Request) {
		switch len(server.prompts()) {
		case 1:
			require.NoError(t, os.WriteFile(writtenPath, []byte("# Summary\n"), 0o644))
			writeJSON(w, assistantResponse(sessionID, map[string]any{
				"kind":   "result",
				"result": map[string]any{},
			}))
		case 2:
			parts := asSlice(body["parts"])
			require.Len(t, parts, 1)
			promptText := asString(asMap(parts[0])["text"])
			server.publish(map[string]any{
				"type": "message.updated",
				"properties": map[string]any{
					"sessionID": sessionID,
					"info": map[string]any{
						"id":        "msg-repair",
						"sessionID": sessionID,
						"role":      "user",
					},
				},
			})
			server.publish(map[string]any{
				"type": "message.part.delta",
				"properties": map[string]any{
					"sessionID": sessionID,
					"messageID": "msg-repair",
					"partID":    "part-repair",
					"field":     "text",
					"delta":     promptText,
				},
			})
			writeJSON(w, assistantResponse(sessionID, map[string]any{
				"kind":   "result",
				"result": map[string]any{"file_paths": []string{writtenPath}},
			}))
		default:
			t.Fatalf("unexpected prompt count %d", len(server.prompts()))
		}
	}

	executor := &Executor{BaseURL: server.url(), HTTPClient: server.client()}
	_, err := executor.Execute(context.Background(), req, func(item taskexecutor.Progress) {
		progress = append(progress, item)
	})
	require.NoError(t, err)

	var sawUserRepair bool
	for _, item := range progress {
		for _, event := range item.Events {
			if event.Kind != taskexecutor.StreamEventKindMessage || event.Message == nil {
				continue
			}
			if event.Message.Role == taskexecutor.MessageRoleUser && strings.Contains(event.Message.Text, "Your previous structured output did not match the required schema.") {
				sawUserRepair = true
			}
		}
	}
	assert.True(t, sawUserRepair)
}

func TestExecutorAbortsSessionOnCancellation(t *testing.T) {
	server := newFakeOpenCodeServer(t)
	server.onPrompt = func(_ string, _ map[string]any, _ http.ResponseWriter, r *http.Request) {
		select {
		case <-server.abortCalls:
		case <-r.Context().Done():
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for abort")
		}
	}
	defer server.close()

	executor := &Executor{BaseURL: server.url(), HTTPClient: server.client()}
	req := requestFixture(t.TempDir())

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	_, err := executor.Execute(ctx, req, nil)
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)

	select {
	case sessionID := <-server.abortCalls:
		assert.Equal(t, "session-1", sessionID)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for abort call")
	}
}

type fakePromptCall struct {
	SessionID string
	Directory string
	Body      map[string]any
}

type fakePermissionReply struct {
	RequestID string
	Reply     string
	Message   string
}

type fakeOpenCodeServer struct {
	t *testing.T

	server *httptest.Server

	mu sync.Mutex

	createRequests []map[string]any
	promptRequests []fakePromptCall

	subscribers map[chan map[string]any]struct{}

	onPrompt func(sessionID string, body map[string]any, w http.ResponseWriter, r *http.Request)

	permissionReplies chan fakePermissionReply
	abortCalls        chan string
}

func newFakeOpenCodeServer(t *testing.T) *fakeOpenCodeServer {
	t.Helper()
	fake := &fakeOpenCodeServer{
		t:                 t,
		subscribers:       map[chan map[string]any]struct{}{},
		permissionReplies: make(chan fakePermissionReply, 4),
		abortCalls:        make(chan string, 4),
	}
	fake.server = httptest.NewServer(http.HandlerFunc(fake.handle))
	return fake
}

func (f *fakeOpenCodeServer) close() {
	f.server.Close()
}

func (f *fakeOpenCodeServer) url() string {
	return f.server.URL
}

func (f *fakeOpenCodeServer) client() *http.Client {
	return f.server.Client()
}

func (f *fakeOpenCodeServer) sessionCreates() []map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]map[string]any, len(f.createRequests))
	copy(out, f.createRequests)
	return out
}

func (f *fakeOpenCodeServer) prompts() []fakePromptCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]fakePromptCall, len(f.promptRequests))
	copy(out, f.promptRequests)
	return out
}

func (f *fakeOpenCodeServer) publish(event map[string]any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for ch := range f.subscribers {
		select {
		case ch <- event:
		default:
		}
	}
}

func (f *fakeOpenCodeServer) waitForPermissionReply(t *testing.T) fakePermissionReply {
	t.Helper()
	select {
	case reply := <-f.permissionReplies:
		return reply
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for permission reply")
		return fakePermissionReply{}
	}
}

func (f *fakeOpenCodeServer) handle(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/event":
		f.handleEvents(w, r)
		return
	case r.Method == http.MethodPost && r.URL.Path == "/session":
		var body map[string]any
		require.NoError(f.t, json.NewDecoder(r.Body).Decode(&body))
		f.mu.Lock()
		f.createRequests = append(f.createRequests, body)
		f.mu.Unlock()
		writeJSON(w, map[string]any{"id": "session-1"})
		return
	case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/session/") && strings.HasSuffix(r.URL.Path, "/message"):
		f.handlePrompt(w, r)
		return
	case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/session/") && strings.HasSuffix(r.URL.Path, "/abort"):
		sessionID := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/session/"), "/abort")
		f.abortCalls <- sessionID
		writeJSON(w, true)
		return
	case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/permission/") && strings.HasSuffix(r.URL.Path, "/reply"):
		requestID := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/permission/"), "/reply")
		var body map[string]any
		require.NoError(f.t, json.NewDecoder(r.Body).Decode(&body))
		f.permissionReplies <- fakePermissionReply{
			RequestID: requestID,
			Reply:     asString(body["reply"]),
			Message:   asString(body["message"]),
		}
		writeJSON(w, true)
		return
	default:
		http.NotFound(w, r)
	}
}

func (f *fakeOpenCodeServer) handleEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	require.True(f.t, ok)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")

	ch := make(chan map[string]any, 16)
	f.mu.Lock()
	f.subscribers[ch] = struct{}{}
	f.mu.Unlock()
	defer func() {
		f.mu.Lock()
		delete(f.subscribers, ch)
		f.mu.Unlock()
	}()

	writeSSE(w, flusher, map[string]any{"type": "server.connected", "properties": map[string]any{}})
	for {
		select {
		case event := <-ch:
			writeSSE(w, flusher, event)
		case <-r.Context().Done():
			return
		}
	}
}

func (f *fakeOpenCodeServer) handlePrompt(w http.ResponseWriter, r *http.Request) {
	sessionID := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/session/"), "/message")
	sessionID = strings.TrimSuffix(sessionID, "/")
	var body map[string]any
	require.NoError(f.t, json.NewDecoder(r.Body).Decode(&body))
	f.mu.Lock()
	f.promptRequests = append(f.promptRequests, fakePromptCall{
		SessionID: sessionID,
		Directory: r.Header.Get(directoryHeader),
		Body:      body,
	})
	f.mu.Unlock()

	if f.onPrompt != nil {
		f.onPrompt(sessionID, body, w, r)
		return
	}
	writeJSON(w, assistantResponse(sessionID, map[string]any{
		"kind":   "result",
		"result": map[string]any{"file_paths": []string{"/tmp/artifact.md"}},
	}))
}

func requestFixture(artifactDir string) taskexecutor.Request {
	allow := false
	return taskexecutor.Request{
		Task: taskdomain.Task{
			ID:          "task-1",
			Description: "Implement feature",
			WorkDir:     artifactDir,
		},
		NodeRun: taskdomain.NodeRun{
			ID:       "run-1",
			TaskID:   "task-1",
			NodeName: "implement",
		},
		NodeDefinition: taskconfig.NodeDefinition{
			SystemPrompt:           "./prompt.md",
			MaxClarificationRounds: 2,
			ResultSchema: taskconfig.JSONSchema{
				Type:                 "object",
				AdditionalProperties: &allow,
				Required:             []string{"file_paths"},
				Properties: map[string]*taskconfig.JSONSchema{
					"file_paths": {
						Type:  "array",
						Items: &taskconfig.JSONSchema{Type: "string"},
					},
				},
			},
		},
		ClarificationConfig: taskconfig.ClarificationConfig{
			MaxQuestions:          4,
			MaxOptionsPerQuestion: 4,
			MinOptionsPerQuestion: 2,
		},
		ConfigPath:  filepath.Join(artifactDir, "config.yaml"),
		SchemaPath:  filepath.Join(artifactDir, "schemas", "implement.json"),
		WorkDir:     artifactDir,
		ArtifactDir: artifactDir,
		Prompt:      "do it",
		ResultSchema: taskconfig.JSONSchema{
			Type:                 "object",
			AdditionalProperties: &allow,
			Required:             []string{"file_paths"},
			Properties: map[string]*taskconfig.JSONSchema{
				"file_paths": {
					Type:  "array",
					Items: &taskconfig.JSONSchema{Type: "string"},
				},
			},
		},
	}
}

func assistantResponse(sessionID string, structured map[string]any) map[string]any {
	now := time.Now().UnixMilli()
	return map[string]any{
		"info": map[string]any{
			"id":         "msg-1",
			"sessionID":  sessionID,
			"role":       "assistant",
			"parentID":   "user-1",
			"modelID":    "test-model",
			"providerID": "test-provider",
			"mode":       "build",
			"agent":      "build",
			"path": map[string]any{
				"cwd":  "/tmp/project",
				"root": "/tmp/project",
			},
			"time": map[string]any{
				"created":   now,
				"completed": now,
			},
			"cost":       0,
			"tokens":     map[string]any{"input": 5, "output": 7, "reasoning": 0, "cache": map[string]any{"read": 0, "write": 0}},
			"structured": structured,
		},
		"parts": []any{},
	}
}

func writeJSON(w http.ResponseWriter, payload any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(payload)
}

func writeSSE(w http.ResponseWriter, flusher http.Flusher, payload any) {
	data, _ := json.Marshal(payload)
	_, _ = w.Write([]byte("data: " + string(data) + "\n\n"))
	flusher.Flush()
}

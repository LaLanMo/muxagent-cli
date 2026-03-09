package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"sync"
)

type message struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *int64          `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

var (
	writeMu       sync.Mutex
	permResponses = make(chan message, 1)
)

func send(msg any) {
	data, _ := json.Marshal(msg)
	data = append(data, '\n')
	writeMu.Lock()
	os.Stdout.Write(data)
	writeMu.Unlock()
}

func respond(id int64, result any) {
	resultBytes, _ := json.Marshal(result)
	send(message{
		JSONRPC: "2.0",
		ID:      &id,
		Result:  resultBytes,
	})
}

func notify(method string, params any) {
	paramsBytes, _ := json.Marshal(params)
	send(message{
		JSONRPC: "2.0",
		Method:  method,
		Params:  paramsBytes,
	})
}

func agentRequest(id int64, method string, params any) {
	paramsBytes, _ := json.Marshal(params)
	send(message{
		JSONRPC: "2.0",
		ID:      &id,
		Method:  method,
		Params:  paramsBytes,
	})
}

func sessionUpdate(sessionID string, update map[string]any) {
	notify("session/update", map[string]any{
		"sessionId": sessionID,
		"update":    update,
	})
}

// handlePrompt processes a prompt in a goroutine so the main read loop
// can continue to receive responses (e.g. permission responses).
func handlePrompt(id int64, params json.RawMessage) {
	var p struct {
		SessionID string `json:"sessionId"`
		Prompt    []any  `json:"prompt"`
	}
	json.Unmarshal(params, &p)
	sid := p.SessionID
	if sid == "" {
		sid = "test-session-001"
	}

	// Check if prompt text contains "permission" to trigger permission flow
	promptBytes, _ := json.Marshal(p.Prompt)
	needsPermission := contains(string(promptBytes), "permission")

	// Send agent message chunks
	sessionUpdate(sid, map[string]any{
		"sessionUpdate": "agent_message_chunk",
		"content":       map[string]any{"type": "text", "text": "Hello! "},
	})
	sessionUpdate(sid, map[string]any{
		"sessionUpdate": "agent_message_chunk",
		"content":       map[string]any{"type": "text", "text": "I'll help you."},
	})

	// Send a tool call
	sessionUpdate(sid, map[string]any{
		"sessionUpdate": "tool_call",
		"toolCallId":    "call-001",
		"title":         "Bash",
		"kind":          "execute",
		"status":        "pending",
	})

	// Tool call updates
	sessionUpdate(sid, map[string]any{
		"sessionUpdate": "tool_call_update",
		"toolCallId":    "call-001",
		"title":         "Bash",
		"status":        "in_progress",
		"rawInput":      map[string]any{"command": "ls"},
	})

	if needsPermission {
		// Send permission request (agent→client)
		agentRequest(9000, "session/request_permission", map[string]any{
			"sessionId": sid,
			"toolCall": map[string]any{
				"toolCallId": "call-001",
				"title":      "Bash",
				"status":     "pending",
				"kind":       "execute",
				"rawInput":   map[string]any{"command": "rm -rf /"},
			},
			"options": []map[string]any{
				{"optionId": "once", "kind": "allow", "name": "Allow once"},
				{"optionId": "reject", "kind": "deny", "name": "Reject"},
			},
		})

		// Wait for permission response (read loop will deliver it)
		<-permResponses
	}

	// Complete the tool call
	sessionUpdate(sid, map[string]any{
		"sessionUpdate": "tool_call_update",
		"toolCallId":    "call-001",
		"title":         "Bash",
		"status":        "completed",
		"rawOutput":     map[string]any{"output": "file1.go\nfile2.go"},
	})

	// Send thought chunk
	sessionUpdate(sid, map[string]any{
		"sessionUpdate": "agent_thought_chunk",
		"content":       map[string]any{"type": "text", "text": "thinking about files..."},
	})

	// Final message
	sessionUpdate(sid, map[string]any{
		"sessionUpdate": "agent_message_chunk",
		"content":       map[string]any{"type": "text", "text": "Done!"},
	})

	respond(id, map[string]any{
		"stopReason": "end_turn",
		"usage": map[string]any{
			"inputTokens":       1200,
			"outputTokens":      350,
			"cachedReadTokens":  800,
			"cachedWriteTokens": 100,
			"totalTokens":       2450,
		},
	})

}

func main() {
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var msg message
		if err := json.Unmarshal(line, &msg); err != nil {
			fmt.Fprintf(os.Stderr, "mockagent: parse error: %v\n", err)
			continue
		}

		// Response to our request (permission response)
		if msg.ID != nil && msg.Method == "" {
			select {
			case permResponses <- msg:
			default:
			}
			continue
		}

		// Notification from client (e.g. session/cancel)
		if msg.Method != "" && msg.ID == nil {
			continue
		}

		// Request from client
		if msg.ID == nil {
			continue
		}
		id := *msg.ID

		switch msg.Method {
		case "initialize":
			respond(id, map[string]any{
				"protocolVersion": 1,
				"agentInfo": map[string]any{
					"name":    "mock",
					"version": "0.1.0",
				},
			})

		case "session/new":
			respond(id, map[string]any{
				"sessionId": "test-session-001",
			})

		case "session/load":
			var params struct {
				SessionID string `json:"sessionId"`
			}
			json.Unmarshal(msg.Params, &params)
			sid := params.SessionID
			if sid == "" {
				sid = "test-session-001"
			}

			// Replay history
			sessionUpdate(sid, map[string]any{
				"sessionUpdate": "user_message_chunk",
				"content":       map[string]any{"type": "text", "text": "Hi "},
			})
			sessionUpdate(sid, map[string]any{
				"sessionUpdate": "user_message_chunk",
				"content":       map[string]any{"type": "text", "text": "there"},
			})
			sessionUpdate(sid, map[string]any{
				"sessionUpdate": "agent_message_chunk",
				"content":       map[string]any{"type": "text", "text": "History: "},
			})
			sessionUpdate(sid, map[string]any{
				"sessionUpdate": "agent_message_chunk",
				"content":       map[string]any{"type": "text", "text": "replayed message"},
			})
			sessionUpdate(sid, map[string]any{
				"sessionUpdate": "tool_call",
				"toolCallId":    "hist-tool-1",
				"title":         "Bash",
				"kind":          "execute",
				"status":        "pending",
			})
			sessionUpdate(sid, map[string]any{
				"sessionUpdate": "tool_call_update",
				"toolCallId":    "hist-tool-1",
				"title":         "Bash",
				"status":        "completed",
				"rawOutput":     map[string]any{"output": "historical output"},
			})

			respond(id, map[string]any{"ok": true})

		case "session/list":
			respond(id, map[string]any{
				"sessions": []map[string]any{
					{
						"sessionId": "test-session-001",
						"cwd":       "/tmp",
						"title":     "Mock Session One",
						"updatedAt": "2026-02-25T00:00:00Z",
					},
					{
						"sessionId": "test-session-002",
						"cwd":       "/workspace/mock-project",
						"title":     "Mock Session Two",
						"updatedAt": "2026-02-25T01:00:00Z",
					},
				},
			})

		case "session/prompt":
			// Handle in goroutine so read loop can continue to
			// receive permission responses from the client.
			go handlePrompt(id, msg.Params)

		default:
			respond(id, map[string]any{"error": "method not found: " + msg.Method})
		}
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"sync"
	"time"
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
	stateMu       sync.Mutex
	currentMode   = "default"
	currentModel  = "default"
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

func respondError(id int64, code int, text string) {
	send(message{
		JSONRPC: "2.0",
		ID:      &id,
		Error: &rpcError{
			Code:    code,
			Message: text,
		},
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

func currentModeValue() string {
	stateMu.Lock()
	defer stateMu.Unlock()
	return currentMode
}

func setCurrentModeValue(mode string) {
	stateMu.Lock()
	defer stateMu.Unlock()
	currentMode = mode
}

func currentModelValue() string {
	stateMu.Lock()
	defer stateMu.Unlock()
	return currentModel
}

func setCurrentModelValue(model string) {
	stateMu.Lock()
	defer stateMu.Unlock()
	currentModel = model
}

func modeConfigOption(mode string) map[string]any {
	return map[string]any{
		"id":           "mode",
		"type":         "select",
		"category":     "mode",
		"currentValue": mode,
		"options": []map[string]any{
			{"value": "default", "name": "Default"},
			{"value": "acceptEdits", "name": "Accept Edits"},
			{"value": "plan", "name": "Plan"},
			{"value": "dontAsk", "name": "Don't Ask"},
			{"value": "bypassPermissions", "name": "Skip Perms"},
		},
	}
}

func modelConfigOption(model string) map[string]any {
	return map[string]any{
		"id":           "model",
		"type":         "select",
		"category":     "model",
		"currentValue": model,
		"options": []map[string]any{
			{"value": "default", "name": "Default"},
			{"value": "opus", "name": "Opus"},
		},
	}
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

	if os.Getenv("MOCKAGENT_LOCATIONS_ONLY_TOOL_UPDATE") == "1" {
		sessionUpdate(sid, map[string]any{
			"sessionUpdate": "tool_call_update",
			"toolCallId":    "call-001",
			"locations": []map[string]any{
				{"path": "/tmp/output.txt", "line": 7},
			},
		})
	}

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
			if os.Getenv("MOCKAGENT_EXIT_AFTER_INITIALIZE") == "1" {
				return
			}

		case "session/new":
			mode := currentModeValue()
			model := currentModelValue()
			respond(id, map[string]any{
				"sessionId": "test-session-001",
				"modes":     map[string]any{"currentModeId": mode},
				"configOptions": []map[string]any{
					modeConfigOption(mode),
					modelConfigOption(model),
				},
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
			if delayMs, err := strconv.Atoi(os.Getenv("MOCKAGENT_LOAD_DELAY_MS")); err == nil && delayMs > 0 {
				time.Sleep(time.Duration(delayMs) * time.Millisecond)
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
			if os.Getenv("MOCKAGENT_EXIT_DURING_LOAD") == "1" {
				return
			}
			if os.Getenv("MOCKAGENT_LOAD_CLAUDE_STYLE_REPLAY") == "1" {
				sessionUpdate(sid, map[string]any{
					"sessionUpdate": "tool_call",
					"toolCallId":    "hist-tool-read-1",
					"title":         "Read /workspace/readme.md",
					"kind":          "read",
					"status":        "pending",
					"rawInput": map[string]any{
						"file_path": "/workspace/readme.md",
					},
					"locations": []map[string]any{
						{"path": "/workspace/readme.md", "line": 1},
					},
				})
				sessionUpdate(sid, map[string]any{
					"sessionUpdate": "tool_call_update",
					"toolCallId":    "hist-tool-read-1",
					"status":        "completed",
					"rawOutput":     "     1→hello\n     2→world\n     3→",
					"content": []map[string]any{
						{
							"type": "content",
							"content": map[string]any{
								"type": "text",
								"text": "```\n     1→hello\n     2→world\n     3→\n```",
							},
						},
					},
				})
				sessionUpdate(sid, map[string]any{
					"sessionUpdate": "tool_call",
					"toolCallId":    "hist-tool-edit-1",
					"title":         "Edit /workspace/file.txt",
					"kind":          "edit",
					"status":        "pending",
					"rawInput": map[string]any{
						"file_path":  "/workspace/file.txt",
						"old_string": "before",
						"new_string": "after",
					},
					"content": []map[string]any{
						{
							"type":    "diff",
							"path":    "/workspace/file.txt",
							"oldText": "before\n",
							"newText": "after\n",
						},
					},
					"locations": []map[string]any{
						{"path": "/workspace/file.txt", "line": 1},
					},
				})
				sessionUpdate(sid, map[string]any{
					"sessionUpdate": "tool_call_update",
					"toolCallId":    "hist-tool-edit-1",
					"status":        "completed",
					"rawOutput":     "The file /workspace/file.txt has been updated successfully.",
				})
			} else if os.Getenv("MOCKAGENT_LOAD_TOOL_CALL_DIFF") == "1" {
				sessionUpdate(sid, map[string]any{
					"sessionUpdate": "tool_call",
					"toolCallId":    "hist-tool-edit-1",
					"title":         "apply_patch",
					"kind":          "edit",
					"status":        "completed",
					"content": []map[string]any{
						{
							"type":    "diff",
							"path":    "/workspace/file.txt",
							"oldText": "before\n",
							"newText": "after\n",
						},
					},
				})
			} else {
				sessionUpdate(sid, map[string]any{
					"sessionUpdate": "tool_call_update",
					"toolCallId":    "hist-tool-1",
					"title":         "Bash",
					"status":        "completed",
					"rawOutput":     map[string]any{"output": "historical output"},
				})
			}

			mode := currentModeValue()
			model := currentModelValue()
			if os.Getenv("MOCKAGENT_INVALID_LOAD_RESPONSE") == "1" {
				respond(id, map[string]any{
					"modes": "invalid",
				})
				continue
			}
			respond(id, map[string]any{
				"ok":    true,
				"modes": map[string]any{"currentModeId": mode},
				"configOptions": []map[string]any{
					modeConfigOption(mode),
					modelConfigOption(model),
				},
			})

		case "session/set_mode":
			if os.Getenv("MOCKAGENT_FAIL_SET_MODE") == "1" {
				respondError(id, -32602, "Invalid params")
				continue
			}
			var params struct {
				ModeID string `json:"modeId"`
			}
			json.Unmarshal(msg.Params, &params)
			if params.ModeID != "" {
				setCurrentModeValue(params.ModeID)
			}
			respond(id, map[string]any{})

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

		case "session/set_config_option":
			var params struct {
				ConfigID string `json:"configId"`
				Value    string `json:"value"`
			}
			json.Unmarshal(msg.Params, &params)
			if params.ConfigID == "model" && params.Value != "" {
				setCurrentModelValue(params.Value)
			}
			if os.Getenv("MOCKAGENT_INVALID_SET_CONFIG_RESPONSE") == "1" {
				respond(id, map[string]any{
					"configOptions": "invalid",
				})
				continue
			}
			respond(id, map[string]any{
				"configOptions": []map[string]any{
					modelConfigOption(currentModelValue()),
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

package acpprotocol

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func assertExactJSONRoundTrip[T any](t *testing.T, fixture string) T {
	t.Helper()

	var decoded T
	require.NoError(t, json.Unmarshal([]byte(fixture), &decoded))

	var want any
	require.NoError(t, json.Unmarshal([]byte(fixture), &want))

	encoded, err := json.Marshal(decoded)
	require.NoError(t, err)

	var got any
	require.NoError(t, json.Unmarshal(encoded, &got))
	require.Equal(t, want, got)

	return decoded
}

func TestRequestPermissionRequestRoundTrip(t *testing.T) {
	fixture := `{
		"_meta": {"source":"schema-fixture"},
		"sessionId": "session-123",
		"toolCall": {
			"_meta": {"runtime":"codex"},
			"toolCallId": "call-123",
			"title": "Run touch /workspace/hello.txt",
			"kind": "execute",
			"status": "pending",
			"content": [
				{"type":"output_text","text":"touch /workspace/hello.txt"}
			],
			"locations": [
				{"_meta":{"source":"tool"},"path":"/workspace/hello.txt","line":12}
			],
			"rawInput": {"command":["touch","/workspace/hello.txt"],"cwd":"/workspace"},
			"rawOutput": {"stdout":"","stderr":"","success":true}
		},
		"options": [
			{"_meta":{"source":"runtime"},"optionId":"allow","name":"Allow","kind":"allow_once"},
			{"optionId":"deny","name":"Deny","kind":"reject_once"}
		]
	}`

	decoded := assertExactJSONRoundTrip[RequestPermissionRequest](t, fixture)
	require.Equal(t, "session-123", decoded.SessionID)
	require.NotNil(t, decoded.ToolCall.Kind)
	require.Equal(t, ToolKind("execute"), *decoded.ToolCall.Kind)
	require.Len(t, decoded.Options, 2)
}

func TestContentChunkRoundTrip(t *testing.T) {
	fixture := `{
		"_meta": {"source":"schema-fixture"},
		"sessionUpdate": "agent_message_chunk",
		"messageId": "msg-123",
		"content": {
			"_meta": {"part":"text"},
			"type": "text",
			"text": "hello world"
		}
	}`

	decoded := assertExactJSONRoundTrip[ContentChunk](t, fixture)
	require.Equal(t, "agent_message_chunk", decoded.SessionUpdate)
	require.NotNil(t, decoded.MessageID)
}

func TestSessionUpdateContractsRoundTrip(t *testing.T) {
	t.Run("current mode update", func(t *testing.T) {
		fixture := `{
			"_meta": {"source":"schema-fixture"},
			"sessionUpdate": "current_mode_update",
			"currentModeId": "auto"
		}`
		decoded := assertExactJSONRoundTrip[CurrentModeUpdate](t, fixture)
		require.Equal(t, "auto", decoded.CurrentModeID)
	})

	t.Run("config option update", func(t *testing.T) {
		fixture := `{
			"_meta": {"source":"schema-fixture"},
			"sessionUpdate": "config_option_update",
			"configOptions": [
				{
					"_meta": {"source":"schema-fixture"},
					"id": "model",
					"name": "Model",
					"description": "Choose a model",
					"category": "model",
					"type": "select",
					"currentValue": "gpt-5.4",
					"options": [
						{
							"group": "Frontier",
							"name": "Frontier Models",
							"options": [
								{"value":"gpt-5.4","name":"gpt-5.4","description":"Latest frontier agentic coding model."}
							]
						}
					]
				}
			]
		}`
		decoded := assertExactJSONRoundTrip[ConfigOptionUpdate](t, fixture)
		require.Len(t, decoded.ConfigOptions, 1)
		require.Len(t, decoded.ConfigOptions[0].Options.Grouped, 1)
	})

	t.Run("plan update", func(t *testing.T) {
		fixture := `{
			"_meta": {"source":"schema-fixture"},
			"sessionUpdate": "plan",
			"entries": [
				{"_meta":{"source":"schema-fixture"},"content":"Inspect event payloads","priority":"high","status":"completed"},
				{"content":"Refactor transport contract","priority":"medium","status":"in_progress"}
			]
		}`
		decoded := assertExactJSONRoundTrip[PlanUpdate](t, fixture)
		require.Len(t, decoded.Entries, 2)
		require.Equal(t, PlanEntryStatus("in_progress"), decoded.Entries[1].Status)
	})

	t.Run("usage update", func(t *testing.T) {
		fixture := `{
			"_meta": {"source":"schema-fixture"},
			"sessionUpdate": "usage_update",
			"used": 53000,
			"size": 200000,
			"cost": {"amount":0.045,"currency":"USD"}
		}`
		decoded := assertExactJSONRoundTrip[UsageUpdate](t, fixture)
		require.EqualValues(t, 53000, decoded.Used)
		require.NotNil(t, decoded.Cost)
	})
}

func TestSessionResponseContractsRoundTrip(t *testing.T) {
	t.Run("new session response", func(t *testing.T) {
		fixture := `{
			"_meta": {"source":"schema-fixture"},
			"sessionId": "session-123",
			"modes": {
				"_meta": {"source":"schema-fixture"},
				"currentModeId": "auto",
				"availableModes": [
					{"id":"read-only","name":"Read Only","description":"Read files only"},
					{"id":"auto","name":"Default"}
				]
			},
			"configOptions": [
				{
					"id": "mode",
					"name": "Approval Preset",
					"type": "select",
					"currentValue": "auto",
					"category": "mode",
					"options": [
						{"value":"read-only","name":"Read Only"},
						{"value":"auto","name":"Default"}
					]
				}
			]
		}`
		decoded := assertExactJSONRoundTrip[NewSessionResponse](t, fixture)
		require.Equal(t, "session-123", decoded.SessionID)
		require.NotNil(t, decoded.Modes)
	})

	t.Run("load session response", func(t *testing.T) {
		fixture := `{
			"_meta": {"source":"schema-fixture"},
			"modes": {
				"currentModeId": "read-only",
				"availableModes": [
					{"id":"read-only","name":"Read Only"}
				]
			},
			"configOptions": [
				{
					"id": "model",
					"name": "Model",
					"type": "select",
					"currentValue": "gpt-5.4",
					"category": "model",
					"options": [
						{"value":"gpt-5.4","name":"gpt-5.4"}
					]
				}
			]
		}`
		decoded := assertExactJSONRoundTrip[LoadSessionResponse](t, fixture)
		require.NotNil(t, decoded.Modes)
		require.Len(t, decoded.ConfigOptions, 1)
	})
}

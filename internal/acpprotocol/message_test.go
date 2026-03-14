package acpprotocol

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestContentChunkUnmarshalRequiresContent(t *testing.T) {
	var chunk ContentChunk
	err := json.Unmarshal([]byte(`{"sessionUpdate":"agent_message_chunk"}`), &chunk)
	require.Error(t, err)
	require.ErrorContains(t, err, `missing required field "content"`)
}

func TestContentChunkUnmarshalAcceptsOptionalMessageID(t *testing.T) {
	var chunk ContentChunk
	err := json.Unmarshal([]byte(`{
		"sessionUpdate":"agent_message_chunk",
		"messageId":"msg-1",
		"content":{"type":"text","text":"hello"}
	}`), &chunk)
	require.NoError(t, err)
	require.NotNil(t, chunk.MessageID)
	require.Equal(t, "msg-1", *chunk.MessageID)
	require.JSONEq(t, `{"type":"text","text":"hello"}`, string(chunk.Content))
}

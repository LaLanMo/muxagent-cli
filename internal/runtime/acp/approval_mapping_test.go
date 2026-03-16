package acp

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/LaLanMo/muxagent-cli/internal/acpprotocol"
	"github.com/stretchr/testify/require"
)

func TestBuildApprovalRequestNormalizesCodexCommandList(t *testing.T) {
	now := time.Date(2026, 3, 14, 1, 30, 0, 0, time.UTC)
	title := "Run touch /workspace/hello.txt"
	kind := acpprotocol.ToolKind("execute")
	status := acpprotocol.ToolCallStatus("pending")
	rawInput, err := json.Marshal(map[string]any{
		"command": []string{"touch", "/workspace/hello.txt"},
		"cwd":     "/workspace",
		"reason":  "Create an empty file.",
	})
	require.NoError(t, err)

	req := acpprotocol.RequestPermissionRequest{
		SessionID: "sid-1",
		ToolCall: acpprotocol.ToolCallUpdate{
			ToolCallID: "call-1",
			Title:      &title,
			Kind:       &kind,
			Status:     &status,
			RawInput:   rawInput,
		},
		Options: []acpprotocol.PermissionOption{
			{OptionID: "approved", Name: "Yes", Kind: acpprotocol.PermissionOptionKindAllowOnce},
		},
	}

	approval := buildApprovalRequest("req-1", "codex", req, now)

	require.NotNil(t, approval.ACP)
	require.Equal(t, "req-1", approval.App.RequestID)
	require.Equal(t, "codex", approval.App.Runtime)
	require.Equal(t, "call-1", approval.App.ToolCallID)
	require.Equal(t, "execute", approval.App.ToolKind)
	require.Equal(t, "Run touch /workspace/hello.txt", approval.App.Title)
	require.Equal(t, "/workspace", approval.App.CWD)
	require.Equal(t, "Create an empty file.", approval.App.Reason)
	require.NotNil(t, approval.App.Command)
	require.Equal(t, []string{"touch", "/workspace/hello.txt"}, approval.App.Command.Argv)
	require.Equal(t, "touch hello.txt", approval.App.Command.Display)
	require.Equal(t, now, *approval.App.CreatedAt)
}

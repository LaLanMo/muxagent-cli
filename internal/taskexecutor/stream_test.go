package taskexecutor

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMergeStreamEventPreservesSpecificToolKindAndMeaningfulName(t *testing.T) {
	existing := StreamEvent{
		Kind: StreamEventKindTool,
		Tool: &ToolCall{
			CallID:       "tool-1",
			Name:         "Grep",
			Kind:         ToolKindSearch,
			Status:       ToolStatusInProgress,
			InputSummary: "theme /tmp/project",
		},
	}
	next := StreamEvent{
		Kind: StreamEventKindTool,
		Tool: &ToolCall{
			CallID:        "tool-1",
			Name:          "tool_result",
			Kind:          ToolKindOther,
			Status:        ToolStatusCompleted,
			OutputText:    "matched lines",
			ErrorText:     "",
			RawOutputJSON: `{"content":"matched lines"}`,
		},
	}

	merged := MergeStreamEvent(existing, next)

	assert.Equal(t, ToolKindSearch, merged.Tool.Kind)
	assert.Equal(t, "Grep", merged.Tool.Name)
	assert.Equal(t, ToolStatusCompleted, merged.Tool.Status)
	assert.Equal(t, "theme /tmp/project", merged.Tool.InputSummary)
	assert.Equal(t, "matched lines", merged.Tool.OutputText)
}

func TestMergeStreamEventUpgradesUnknownToolKindWhenResultIsSpecific(t *testing.T) {
	existing := StreamEvent{
		Kind: StreamEventKindTool,
		Tool: &ToolCall{
			CallID:       "tool-1",
			Name:         "UnknownTool",
			Kind:         ToolKindOther,
			Status:       ToolStatusInProgress,
			InputSummary: `{"filePath":"/tmp/project/sample.txt"}`,
		},
	}
	next := StreamEvent{
		Kind: StreamEventKindTool,
		Tool: &ToolCall{
			CallID:       "tool-1",
			Name:         "Read",
			Kind:         ToolKindRead,
			Status:       ToolStatusCompleted,
			InputSummary: "/tmp/project/sample.txt",
		},
	}

	merged := MergeStreamEvent(existing, next)

	assert.Equal(t, ToolKindRead, merged.Tool.Kind)
	assert.Equal(t, "UnknownTool", merged.Tool.Name)
	assert.Equal(t, ToolStatusCompleted, merged.Tool.Status)
	assert.Equal(t, "/tmp/project/sample.txt", merged.Tool.InputSummary)
}

func TestMergeStreamEventPreservesFailureStatusWhileKeepingSpecificKind(t *testing.T) {
	existing := StreamEvent{
		Kind: StreamEventKindTool,
		Tool: &ToolCall{
			CallID:       "tool-1",
			Name:         "Bash",
			Kind:         ToolKindShell,
			Status:       ToolStatusInProgress,
			InputSummary: "pwd",
		},
	}
	next := StreamEvent{
		Kind: StreamEventKindTool,
		Tool: &ToolCall{
			CallID:    "tool-1",
			Name:      "tool_result",
			Kind:      ToolKindOther,
			Status:    ToolStatusFailed,
			ErrorText: "permission denied",
		},
	}

	merged := MergeStreamEvent(existing, next)

	assert.Equal(t, ToolKindShell, merged.Tool.Kind)
	assert.Equal(t, "Bash", merged.Tool.Name)
	assert.Equal(t, ToolStatusFailed, merged.Tool.Status)
	assert.Equal(t, "permission denied", merged.Tool.ErrorText)
}

func TestToolDisplayLabelUsesSharedKindsAndPrettifiedNames(t *testing.T) {
	tests := []struct {
		name string
		tool ToolCall
		want string
	}{
		{
			name: "search kind",
			tool: ToolCall{Kind: ToolKindSearch, Name: "Grep"},
			want: "search",
		},
		{
			name: "fetch kind",
			tool: ToolCall{Kind: ToolKindFetch, Name: "WebFetch"},
			want: "fetch",
		},
		{
			name: "web fetch fallback",
			tool: ToolCall{Kind: ToolKindOther, Name: "WebFetch"},
			want: "web fetch",
		},
		{
			name: "todo write fallback",
			tool: ToolCall{Kind: ToolKindOther, Name: "TodoWrite"},
			want: "todo write",
		},
		{
			name: "tool result fallback",
			tool: ToolCall{Kind: ToolKindOther, Name: "tool_result"},
			want: "tool result",
		},
		{
			name: "empty fallback",
			tool: ToolCall{Kind: ToolKindOther},
			want: "tool",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.tool.DisplayLabel())
		})
	}
}

func TestStreamEventStableKeyUsesCallIDWhenPresent(t *testing.T) {
	event := StreamEvent{
		Kind: StreamEventKindTool,
		Tool: &ToolCall{
			CallID:       "tool-123",
			Name:         "tool_result",
			Kind:         ToolKindOther,
			InputSummary: "/tmp/project/sample.txt",
		},
	}

	assert.Equal(t, "tool:tool-123", event.StableKey())
}

func TestStreamEventStableKeyFallsBackToKindNameAndSummaryWithoutCallID(t *testing.T) {
	event := StreamEvent{
		Kind: StreamEventKindTool,
		Tool: &ToolCall{
			Name:         "Grep",
			Kind:         ToolKindSearch,
			InputSummary: "theme /tmp/project",
		},
	}

	assert.Equal(t, "tool:search:Grep:theme /tmp/project", event.StableKey())
}

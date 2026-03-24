package taskruntime

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LaLanMo/muxagent-cli/internal/taskconfig"
	"github.com/LaLanMo/muxagent-cli/internal/taskdomain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildPromptPresentsWorkflowHistoryChronologically(t *testing.T) {
	cfg := &taskconfig.Config{
		Version: 1,
		Clarification: taskconfig.ClarificationConfig{
			MaxQuestions:          4,
			MaxOptionsPerQuestion: 4,
			MinOptionsPerQuestion: 2,
		},
		Topology: taskconfig.Topology{
			MaxIterations: 3,
			Entry:         "upsert_plan",
			Nodes: []taskconfig.NodeRef{
				{Name: "upsert_plan"},
				{Name: "review_plan"},
			},
			Edges: []taskconfig.Edge{
				{From: "upsert_plan", To: "review_plan"},
			},
		},
		NodeDefinitions: map[string]taskconfig.NodeDefinition{
			"upsert_plan": func() taskconfig.NodeDefinition {
				def := artifactAgentNode()
				def.SystemPrompt = "./prompts/upsert_plan.md"
				return def
			}(),
			"review_plan": func() taskconfig.NodeDefinition {
				def := artifactAgentNode()
				def.SystemPrompt = "./prompts/review_plan.md"
				return def
			}(),
		},
	}
	configPath := writeOverrideConfig(t, cfg)
	promptPath := filepath.Join(filepath.Dir(configPath), "prompts", "upsert_plan.md")
	template := strings.Join([]string{
		"Task request:",
		"{{TASK_DESCRIPTION}}",
		"",
		"Iteration:",
		"{{CURRENT_ITERATION}}",
		"",
		"Workflow history so far (oldest first):",
		"{{WORKFLOW_HISTORY}}",
		"",
		"Clarification history:",
		"{{CLARIFICATION_HISTORY}}",
	}, "\n")
	require.NoError(t, os.WriteFile(promptPath, []byte(template), 0o644))

	runs := []taskdomain.NodeRun{
		{
			ID:       "upsert-1",
			NodeName: "upsert_plan",
			Status:   taskdomain.NodeRunDone,
			Result: map[string]interface{}{
				"file_paths": []interface{}{"/tmp/plan-v1.md"},
			},
		},
		{
			ID:       "review-1",
			NodeName: "review_plan",
			Status:   taskdomain.NodeRunDone,
			Result: map[string]interface{}{
				"passed":     false,
				"file_paths": []interface{}{"/tmp/review-v1.md"},
			},
		},
		{
			ID:       "upsert-2",
			NodeName: "upsert_plan",
			Status:   taskdomain.NodeRunRunning,
		},
	}

	prompt, err := buildPrompt(
		taskdomain.Task{Description: "Create hello.txt"},
		cfg,
		configPath,
		runs,
		runs[2],
		"/tmp/task-artifacts/upsert-2",
	)
	require.NoError(t, err)

	assert.Contains(t, prompt, "Create hello.txt")
	assert.Contains(t, prompt, "\n2\n")
	assert.Contains(t, prompt, "1. upsert_plan (#1)")
	assert.Contains(t, prompt, "2. review_plan (#1)")
	assert.Contains(t, prompt, "/tmp/plan-v1.md")
	assert.Contains(t, prompt, "/tmp/review-v1.md")
	assert.Contains(t, prompt, "Clarification history:\n(none)")
	assert.Less(t, strings.Index(prompt, "1. upsert_plan (#1)"), strings.Index(prompt, "2. review_plan (#1)"))
	assert.Less(t, strings.Index(prompt, "/tmp/plan-v1.md"), strings.Index(prompt, "/tmp/review-v1.md"))
}

func TestDefaultPromptTemplatesReadLikeStepInstructions(t *testing.T) {
	workDir := t.TempDir()
	materialized, err := taskconfig.Materialize(workDir, "task-1", "")
	require.NoError(t, err)

	cases := []struct {
		name     string
		contains []string
		excludes []string
	}{
		{
			name: "upsert_plan",
			contains: []string{
				"You are planning how to complete this task.",
				"Workflow history (oldest first):",
				"Iteration {{CURRENT_ITERATION}} of planning.",
			},
		},
		{
			name: "review_plan",
			contains: []string{
				"You are reviewing a plan before it goes to human approval.",
				"Workflow history (oldest first):",
				"Review checklist",
			},
		},
		{
			name: "implement",
			contains: []string{
				"You are implementing an approved plan.",
				"Workflow history (oldest first):",
				"Summary artifact",
			},
		},
		{
			name: "verify",
			contains: []string{
				"You are verifying whether the implementation satisfies the task.",
				"Workflow history (oldest first):",
				"Verification checklist",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			def := materialized.Config.NodeDefinitions[tc.name]
			promptText, err := taskconfig.ReadPromptText(materialized.ConfigPath, def)
			require.NoError(t, err)
			for _, want := range tc.contains {
				assert.Contains(t, promptText, want)
			}
			for _, unwanted := range tc.excludes {
				assert.NotContains(t, promptText, unwanted)
			}
		})
	}
}

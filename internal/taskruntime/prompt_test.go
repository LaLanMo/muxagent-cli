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
			Entry:         "draft_plan",
			Nodes: []taskconfig.NodeRef{
				{Name: "draft_plan"},
				{Name: "review_plan"},
			},
			Edges: []taskconfig.Edge{
				{From: "draft_plan", To: "review_plan"},
			},
		},
		NodeDefinitions: map[string]taskconfig.NodeDefinition{
			"draft_plan": func() taskconfig.NodeDefinition {
				def := artifactAgentNode()
				def.SystemPrompt = "./prompts/draft_plan.md"
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
	promptPath := filepath.Join(filepath.Dir(configPath), "prompts", "draft_plan.md")
	template := strings.Join([]string{
		"Task:",
		"```",
		"{{TASK_DESCRIPTION}}",
		"```",
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
			NodeName: "draft_plan",
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
			NodeName: "draft_plan",
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

	assert.Contains(t, prompt, "Task:\n```\nCreate hello.txt\n```")
	assert.Contains(t, prompt, "\n2\n")
	assert.Contains(t, prompt, "1. draft_plan (#1)")
	assert.Contains(t, prompt, "2. review_plan (#1)")
	assert.Contains(t, prompt, "/tmp/plan-v1.md")
	assert.Contains(t, prompt, "/tmp/review-v1.md")
	assert.Contains(t, prompt, "\"passed\":false")
	assert.Contains(t, prompt, "Clarification history:\n(none)")
	assert.NotContains(t, prompt, "Artifacts:")
	assert.Equal(t, 1, strings.Count(prompt, "/tmp/plan-v1.md"))
	assert.Equal(t, 1, strings.Count(prompt, "/tmp/review-v1.md"))
	assert.Less(t, strings.Index(prompt, "1. draft_plan (#1)"), strings.Index(prompt, "2. review_plan (#1)"))
	assert.Less(t, strings.Index(prompt, "/tmp/plan-v1.md"), strings.Index(prompt, "/tmp/review-v1.md"))
}

func TestBuildPromptWithInheritedContextKeepsOlderAncestorsAsReferencesOnly(t *testing.T) {
	cfg := &taskconfig.Config{
		Version: 1,
		Clarification: taskconfig.ClarificationConfig{
			MaxQuestions:          4,
			MaxOptionsPerQuestion: 4,
			MinOptionsPerQuestion: 2,
		},
		Topology: taskconfig.Topology{
			MaxIterations: 3,
			Entry:         "draft_plan",
			Nodes: []taskconfig.NodeRef{
				{Name: "draft_plan"},
			},
		},
		NodeDefinitions: map[string]taskconfig.NodeDefinition{
			"draft_plan": func() taskconfig.NodeDefinition {
				def := artifactAgentNode()
				def.SystemPrompt = "./prompts/draft_plan.md"
				return def
			}(),
		},
	}
	configPath := writeOverrideConfig(t, cfg)
	promptPath := filepath.Join(filepath.Dir(configPath), "prompts", "draft_plan.md")
	template := strings.Join([]string{
		"Workflow history:",
		"{{WORKFLOW_HISTORY}}",
	}, "\n")
	require.NoError(t, os.WriteFile(promptPath, []byte(template), 0o644))

	runs := []taskdomain.NodeRun{
		{
			ID:       "draft-1",
			NodeName: "draft_plan",
			Status:   taskdomain.NodeRunDone,
			Result: map[string]interface{}{
				"file_paths": []interface{}{"/tmp/current-plan.md"},
			},
		},
		{
			ID:       "draft-2",
			NodeName: "draft_plan",
			Status:   taskdomain.NodeRunRunning,
		},
	}

	inherited := &inheritedContext{
		WorkflowHistory: strings.Join([]string{
			"## Direct Parent Task",
			"Description: parent task",
			"Task directory: /tmp/task-parent",
			"",
			"## Direct Parent Workflow History (oldest first)",
			"1. draft_plan (#1)\n   Result JSON: {\"file_paths\":[\"/tmp/parent-plan.md\"]}",
			"",
			"## Earlier Ancestors (inspect only if needed)",
			"- grandparent task",
			"  Task directory: /tmp/task-grandparent",
		}, "\n"),
	}

	prompt, err := buildPromptWithInheritedContext(
		taskdomain.Task{Description: "child task"},
		cfg,
		configPath,
		runs,
		runs[1],
		"/tmp/task-artifacts/draft-2",
		inherited,
	)
	require.NoError(t, err)

	assert.Contains(t, prompt, "Description: parent task")
	assert.Contains(t, prompt, "Task directory: /tmp/task-parent")
	assert.Contains(t, prompt, "## Earlier Ancestors (inspect only if needed)")
	assert.Contains(t, prompt, "Task directory: /tmp/task-grandparent")
	assert.Contains(t, prompt, "/tmp/parent-plan.md", "direct parent artifact path should still appear through workflow history")
	assert.Contains(t, prompt, "/tmp/current-plan.md")
	assert.NotContains(t, prompt, "## Direct Parent Completed Results")
	assert.Equal(t, 1, strings.Count(prompt, "/tmp/parent-plan.md"))
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
			name: "draft_plan",
			contains: []string{
				"Step: {{NODE_NAME}}",
				"ArtifactDir: {{ARTIFACT_DIR}}",
				"Iteration: {{CURRENT_ITERATION}}",
				"Task\n```\n{{TASK_DESCRIPTION}}\n```",
				"Workflow history (oldest first):",
				"Clarifications so far:",
				"How to plan",
				"newest relevant workflow artifact files",
				"Plan artifacts",
				"file_paths",
			},
			excludes: []string{
				"Task: {{TASK_DESCRIPTION}}",
				"Completed structured results:",
				"Known artifact paths:",
			},
		},
		{
			name: "review_plan",
			contains: []string{
				"Step: {{NODE_NAME}}",
				"ArtifactDir: {{ARTIFACT_DIR}}",
				"Iteration: {{CURRENT_ITERATION}}",
				"Task\n```\n{{TASK_DESCRIPTION}}\n```",
				"Workflow history (oldest first):",
				"Clarifications so far:",
				"newest relevant workflow artifact files",
				"Review checklist",
				"Pass bar",
				"passed",
			},
			excludes: []string{
				"Task: {{TASK_DESCRIPTION}}",
				"Completed structured results:",
				"Known artifact paths:",
			},
		},
		{
			name: "implement",
			contains: []string{
				"Step: {{NODE_NAME}}",
				"ArtifactDir: {{ARTIFACT_DIR}}",
				"Iteration: {{CURRENT_ITERATION}}",
				"Task\n```\n{{TASK_DESCRIPTION}}\n```",
				"Workflow history (oldest first):",
				"Clarifications so far:",
				"newest relevant workflow artifact files",
				"Execute it",
				"Summary artifact",
				"file_paths",
			},
			excludes: []string{
				"Task: {{TASK_DESCRIPTION}}",
				"Completed structured results:",
				"Known artifact paths:",
			},
		},
		{
			name: "verify",
			contains: []string{
				"Step: {{NODE_NAME}}",
				"ArtifactDir: {{ARTIFACT_DIR}}",
				"Iteration: {{CURRENT_ITERATION}}",
				"Task\n```\n{{TASK_DESCRIPTION}}\n```",
				"Workflow history (oldest first):",
				"Clarifications so far:",
				"newest relevant workflow artifact files",
				"Verification checklist",
				"Use the original task as a guardrail",
				"passed",
			},
			excludes: []string{
				"Task: {{TASK_DESCRIPTION}}",
				"Completed structured results:",
				"Known artifact paths:",
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

func TestBuildClarificationResumePromptUsesStableHeader(t *testing.T) {
	run := taskdomain.NodeRun{
		NodeName:  "draft_plan",
		SessionID: "thread-draft_plan-1",
		Clarifications: []taskdomain.ClarificationExchange{{
			Request: taskdomain.ClarificationRequest{
				Questions: []taskdomain.ClarificationQuestion{
					{
						Question:     "Which path should we take?",
						WhyItMatters: "The plan changes based on this choice.",
						Options: []taskdomain.ClarificationOption{
							{Label: "A", Description: "Option A"},
							{Label: "B", Description: "Option B"},
						},
					},
				},
			},
			Response: &taskdomain.ClarificationResponse{
				Answers: []taskdomain.ClarificationAnswer{
					{Selected: "A"},
				},
			},
		}},
	}

	prompt, err := buildClarificationResumePrompt(
		taskdomain.Task{Description: "Implement login\nHandle SSO fallback"},
		run,
		"/tmp/task-artifacts/draft-plan",
		2,
		nil,
	)
	require.NoError(t, err)

	assert.Contains(t, prompt, "Step: draft_plan")
	assert.Contains(t, prompt, "ArtifactDir: /tmp/task-artifacts/draft-plan")
	assert.Contains(t, prompt, "Iteration: 2")
	assert.Contains(t, prompt, "Mission")
	assert.Contains(t, prompt, "Task\n```\nImplement login\nHandle SSO fallback\n```")
	assert.Contains(t, prompt, "New clarification exchange")
	assert.Contains(t, prompt, "User selected:")
	assert.Contains(t, prompt, "Pass Bar")
	assert.Contains(t, prompt, "Produce")
}

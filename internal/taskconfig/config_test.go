package taskconfig

import (
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	appconfig "github.com/LaLanMo/muxagent-cli/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadDefaultConfig(t *testing.T) {
	setTaskConfigRuntimePath(t)

	cfg, err := LoadDefault()
	require.NoError(t, err)

	assert.Equal(t, 1, cfg.Version)
	assert.Equal(t, appconfig.RuntimeCodex, cfg.Runtime)
	assert.Equal(t, "draft_plan", cfg.Topology.Entry)
	assert.Len(t, cfg.Topology.Nodes, 6)
	assert.Equal(t, NodeTypeHuman, cfg.NodeDefinitions["approve_plan"].Type)
	assert.Equal(t, NodeTypeAgent, cfg.NodeDefinitions["verify"].Type)
	assert.Equal(t, NodeTypeTerminal, cfg.NodeDefinitions["done"].Type)
	assert.NotEmpty(t, cfg.Description)
}

func TestDescriptionFieldParsedFromYAML(t *testing.T) {
	cfgPath := writeConfigFile(t, `
version: 1
description: "A custom workflow for code reviews."
runtime: codex
clarification:
  max_questions: 4
  max_options_per_question: 4
  min_options_per_question: 2
topology:
  max_iterations: 1
  entry: start
  nodes:
    - name: start
    - name: done
  edges:
    - from: start
      to: done
node_definitions:
  start:
    system_prompt: ./prompt.md
    result_schema:
      type: object
      additionalProperties: false
      properties: {}
  done:
    type: terminal
`)
	cfg, err := Load(cfgPath)
	require.NoError(t, err)
	assert.Equal(t, "A custom workflow for code reviews.", cfg.Description)
}

func TestDescriptionFieldIsOptional(t *testing.T) {
	cfgPath := writeConfigFile(t, `
version: 1
runtime: codex
clarification:
  max_questions: 4
  max_options_per_question: 4
  min_options_per_question: 2
topology:
  max_iterations: 1
  entry: start
  nodes:
    - name: start
    - name: done
  edges:
    - from: start
      to: done
node_definitions:
  start:
    system_prompt: ./prompt.md
    result_schema:
      type: object
      additionalProperties: false
      properties: {}
  done:
    type: terminal
`)
	cfg, err := Load(cfgPath)
	require.NoError(t, err)
	assert.Empty(t, cfg.Description)
}

func TestLoadBuiltinConfigs(t *testing.T) {
	setTaskConfigRuntimePath(t)

	tests := []struct {
		name         string
		builtinID    string
		entry        string
		nodeCount    int
		hasApproval  bool
		hasImplement bool
		hasEvaluator bool
	}{
		{
			name:         "default",
			builtinID:    BuiltinIDDefault,
			entry:        "draft_plan",
			nodeCount:    6,
			hasApproval:  true,
			hasImplement: true,
			hasEvaluator: false,
		},
		{
			name:         "plan-only",
			builtinID:    BuiltinIDPlanOnly,
			entry:        "draft_plan",
			nodeCount:    3,
			hasApproval:  false,
			hasImplement: false,
			hasEvaluator: false,
		},
		{
			name:         "autonomous",
			builtinID:    BuiltinIDAutonomous,
			entry:        "draft_plan",
			nodeCount:    5,
			hasApproval:  false,
			hasImplement: true,
			hasEvaluator: false,
		},
		{
			name:         "yolo",
			builtinID:    BuiltinIDYolo,
			entry:        "draft_plan",
			nodeCount:    6,
			hasApproval:  false,
			hasImplement: true,
			hasEvaluator: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := LoadBuiltin(tt.builtinID)
			require.NoError(t, err)

			assert.Equal(t, 1, cfg.Version)
			assert.Equal(t, appconfig.RuntimeCodex, cfg.Runtime)
			assert.Equal(t, tt.entry, cfg.Topology.Entry)
			assert.Len(t, cfg.Topology.Nodes, tt.nodeCount)
			_, hasApproval := cfg.NodeDefinitions["approve_plan"]
			assert.Equal(t, tt.hasApproval, hasApproval)
			_, hasImplement := cfg.NodeDefinitions["implement"]
			assert.Equal(t, tt.hasImplement, hasImplement)
			_, hasEvaluator := cfg.NodeDefinitions["evaluate_progress"]
			assert.Equal(t, tt.hasEvaluator, hasEvaluator)
			assert.Equal(t, NodeTypeTerminal, cfg.NodeDefinitions["done"].Type)
		})
	}
}

func TestLoadBuiltinUsesPreferredRuntimeFromPATH(t *testing.T) {
	tests := []struct {
		name        string
		pathEntries []string
		wantRuntime appconfig.RuntimeID
	}{
		{
			name:        "prefers codex when both are present",
			pathEntries: []string{"codex", "claude"},
			wantRuntime: appconfig.RuntimeCodex,
		},
		{
			name:        "uses codex when only codex is present",
			pathEntries: []string{"codex"},
			wantRuntime: appconfig.RuntimeCodex,
		},
		{
			name:        "uses claude when codex is absent",
			pathEntries: []string{"claude"},
			wantRuntime: appconfig.RuntimeClaudeCode,
		},
		{
			name:        "falls back to codex when neither is present",
			pathEntries: nil,
			wantRuntime: appconfig.RuntimeCodex,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setTaskConfigRuntimePath(t, tt.pathEntries...)

			cfg, err := LoadBuiltin(BuiltinIDDefault)
			require.NoError(t, err)
			assert.Equal(t, tt.wantRuntime, cfg.Runtime)
		})
	}
}

func TestLoadBuiltinAutonomousAndYoloDisableClarification(t *testing.T) {
	tests := []struct {
		name      string
		builtinID string
		nodes     []string
	}{
		{
			name:      "autonomous",
			builtinID: BuiltinIDAutonomous,
			nodes:     []string{"draft_plan", "implement", "verify"},
		},
		{
			name:      "yolo",
			builtinID: BuiltinIDYolo,
			nodes:     []string{"draft_plan", "review_plan", "implement", "verify", "evaluate_progress"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := LoadBuiltin(tt.builtinID)
			require.NoError(t, err)

			for _, node := range tt.nodes {
				assert.Zero(t, cfg.NodeDefinitions[node].MaxClarificationRounds)
			}
		})
	}
}

func TestLoadBuiltinYoloUsesDedicatedPromptSet(t *testing.T) {
	cfg, err := LoadBuiltin(BuiltinIDYolo)
	require.NoError(t, err)

	assert.Equal(t, "./prompts/yolo_draft_plan.md", cfg.NodeDefinitions["draft_plan"].SystemPrompt)
	assert.Equal(t, "./prompts/yolo_review_plan.md", cfg.NodeDefinitions["review_plan"].SystemPrompt)
	assert.Equal(t, "./prompts/yolo_implement.md", cfg.NodeDefinitions["implement"].SystemPrompt)
	assert.Equal(t, "./prompts/yolo_verify.md", cfg.NodeDefinitions["verify"].SystemPrompt)
	assert.Equal(t, "./prompts/yolo_evaluate_progress.md", cfg.NodeDefinitions["evaluate_progress"].SystemPrompt)
}

func TestLoadBuiltinYoloUsesAggressiveIterationBudget(t *testing.T) {
	cfg, err := LoadBuiltin(BuiltinIDYolo)
	require.NoError(t, err)

	assert.Equal(t, 100, cfg.Topology.MaxIterations)
	for _, node := range cfg.Topology.Nodes {
		if node.Name == "draft_plan" {
			assert.Zero(t, node.MaxIterations)
		}
	}
}

func TestEmbeddedDefaultPlanningPromptsAllowReadCommands(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		contains []string
		excludes []string
	}{
		{
			name: "draft_plan",
			path: "defaults/prompts/draft_plan.md",
			contains: []string{
				"Read operations and side-effect-free commands are always allowed",
				"Any other write operation or side-effecting command requires asking the user via clarification first.",
			},
			excludes: []string{
				"Do not modify project files, run commands, execute tests, or cause any side effects.",
			},
		},
		{
			name: "review_plan",
			path: "defaults/prompts/review_plan.md",
			contains: []string{
				"Read operations and side-effect-free commands are always allowed",
				"Any other write operation or side-effecting command requires asking the user via clarification first.",
			},
			excludes: []string{
				"Do not modify any files, run commands, execute tests, or cause any side effects.",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := fs.ReadFile(defaultsFS, tt.path)
			require.NoError(t, err)
			text := string(data)

			for _, want := range tt.contains {
				assert.Truef(t, strings.Contains(text, want), "expected %q in %s", want, tt.path)
			}
			for _, unwanted := range tt.excludes {
				assert.Falsef(t, strings.Contains(text, unwanted), "did not expect %q in %s", unwanted, tt.path)
			}
		})
	}
}

func TestEmbeddedYoloPromptsUseOutcomeContracts(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		contains []string
		excludes []string
	}{
		{
			name: "draft_plan",
			path: "defaults/prompts/yolo_draft_plan.md",
			contains: []string{
				"Task\n```\n{{TASK_DESCRIPTION}}\n```",
				"Do not infer progress from the iteration number alone.",
				"Wave Goal",
				"Done Definition",
				"Allowed Side Effects",
				"Treat it as an outcome contract",
			},
			excludes: []string{
				"Task: {{TASK_DESCRIPTION}}",
				"If this is iteration 2+, assume at least one prior planning wave was completed and verified",
			},
		},
		{
			name: "review_plan",
			path: "defaults/prompts/yolo_review_plan.md",
			contains: []string{
				"Task\n```\n{{TASK_DESCRIPTION}}\n```",
				"outcome contract",
				"Wave contract quality",
				"Done Definition",
				"Allowed Side Effects",
			},
			excludes: []string{
				"Task: {{TASK_DESCRIPTION}}",
			},
		},
		{
			name: "implement",
			path: "defaults/prompts/yolo_implement.md",
			contains: []string{
				"Task\n```\n{{TASK_DESCRIPTION}}\n```",
				"Satisfy the full approved planning-wave contract",
				"wave goal, done definition, required checks",
				"deviate from the plan's suggested implementation details",
				"Wave goal status",
			},
			excludes: []string{
				"Task: {{TASK_DESCRIPTION}}",
			},
		},
		{
			name: "verify",
			path: "defaults/prompts/yolo_verify.md",
			contains: []string{
				"Task\n```\n{{TASK_DESCRIPTION}}\n```",
				"planning-wave contract",
				"Do not require literal adherence to implementation details.",
				"accepted deviations",
			},
			excludes: []string{
				"Task: {{TASK_DESCRIPTION}}",
			},
		},
		{
			name: "evaluate_progress",
			path: "defaults/prompts/yolo_evaluate_progress.md",
			contains: []string{
				"Task\n```\n{{TASK_DESCRIPTION}}\n```",
				"explicit requested scope",
				"remaining obligation and the next wave goal",
				"Do not invent adjacent nice-to-have work.",
			},
			excludes: []string{
				"Task: {{TASK_DESCRIPTION}}",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := fs.ReadFile(defaultsFS, tt.path)
			require.NoError(t, err)
			text := string(data)

			for _, want := range tt.contains {
				assert.Truef(t, strings.Contains(text, want), "expected %q in %s", want, tt.path)
			}
			for _, unwanted := range tt.excludes {
				assert.Falsef(t, strings.Contains(text, unwanted), "did not expect %q in %s", unwanted, tt.path)
			}
		})
	}
}

func TestLoadRejectsInvalidConfigs_TableDriven(t *testing.T) {
	cases := []struct {
		name    string
		content string
		wantErr string
	}{
		{
			name: "unknown top level field",
			content: `
version: 1
unexpected: true
topology:
  max_iterations: 2
  entry: start
  nodes:
    - name: start
  edges: []
node_definitions:
  start:
    system_prompt: ./prompts/start.md
    result_schema:
      type: object
      additionalProperties: false
      properties: {}
`,
			wantErr: "field unexpected not found",
		},
		{
			name: "missing version",
			content: `
topology:
  max_iterations: 2
  entry: start
  nodes:
    - name: start
  edges: []
node_definitions:
  start:
    system_prompt: ./prompts/start.md
    result_schema:
      type: object
      additionalProperties: false
      properties: {}
`,
			wantErr: "version is required",
		},
		{
			name: "unknown edge condition field",
			content: `
version: 1
topology:
  max_iterations: 2
  entry: start
  nodes:
    - name: start
    - name: done
  edges:
    - from: start
      to: done
      when:
        field: approved
        equals: true
        extra: nope
node_definitions:
  start:
    system_prompt: ./prompts/start.md
    result_schema:
      type: object
      additionalProperties: false
      properties:
        approved:
          type: boolean
  done:
    system_prompt: ./prompts/done.md
    result_schema:
      type: object
      additionalProperties: false
      properties: {}
`,
			wantErr: "unsupported edge condition field",
		},
		{
			name: "zero topology max iterations",
			content: `
version: 1
topology:
  max_iterations: 0
  entry: start
  nodes:
    - name: start
  edges: []
node_definitions:
  start:
    system_prompt: ./prompts/start.md
    result_schema:
      type: object
      additionalProperties: false
      properties: {}
`,
			wantErr: "topology.max_iterations must be > 0",
		},
		{
			name: "zero node max iterations",
			content: `
version: 1
topology:
  max_iterations: 2
  entry: start
  nodes:
    - name: start
      max_iterations: 0
  edges: []
node_definitions:
  start:
    system_prompt: ./prompts/start.md
    result_schema:
      type: object
      additionalProperties: false
      properties: {}
`,
			wantErr: "max_iterations must be > 0",
		},
		{
			name: "human clarification rounds field",
			content: `
version: 1
topology:
  max_iterations: 2
  entry: approve
  nodes:
    - name: approve
  edges: []
node_definitions:
  approve:
    type: human
    max_clarification_rounds: 0
    result_schema:
      type: object
      additionalProperties: false
      properties:
        approved:
          type: boolean
`,
			wantErr: "cannot define max_clarification_rounds",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := writeConfigFile(t, tc.content)

			_, err := Load(path)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)
		})
	}
}

func TestValidateRejectsInvalidConfigs_TableDriven(t *testing.T) {
	cases := []struct {
		name    string
		build   func(t *testing.T) *Config
		wantErr string
	}{
		{
			name: "mixed edge kinds",
			build: func(t *testing.T) *Config {
				cfg := booleanBranchFixture()
				cfg.Topology.Edges = append(cfg.Topology.Edges, Edge{From: "start", To: "fallback"})
				return cfg
			},
			wantErr: "mixes unconditional and conditional edges",
		},
		{
			name: "multiple else edges",
			build: func(t *testing.T) *Config {
				cfg := booleanElseFixture()
				cfg.Topology.Edges = append(cfg.Topology.Edges, Edge{From: "start", To: "done", When: EdgeCondition{Kind: ConditionElse}})
				return cfg
			},
			wantErr: "multiple else edges",
		},
		{
			name: "join without two incoming edges",
			build: func(t *testing.T) *Config {
				cfg := basicFixture()
				cfg.Topology.Nodes[1].Join = JoinAll
				return cfg
			},
			wantErr: "fewer than 2 incoming edges",
		},
		{
			name: "stray else",
			build: func(t *testing.T) *Config {
				cfg := basicFixture()
				cfg.NodeDefinitions["start"] = booleanNode("approved")
				cfg.Topology.Edges = []Edge{
					{From: "start", To: "done", When: EdgeCondition{Kind: ConditionElse}},
				}
				return cfg
			},
			wantErr: "else edge without explicit conditional siblings",
		},
		{
			name: "missing conditional field",
			build: func(t *testing.T) *Config {
				cfg := booleanBranchFixture()
				cfg.Topology.Edges[0].When.Field = "missing"
				cfg.Topology.Edges[1].When.Field = "missing"
				return cfg
			},
			wantErr: "does not exist in result_schema",
		},
		{
			name: "conditional branches on different fields",
			build: func(t *testing.T) *Config {
				cfg := booleanBranchFixture()
				start := cfg.NodeDefinitions["start"]
				start.ResultSchema.Properties["other"] = &JSONSchema{Type: "boolean"}
				start.ResultSchema.Required = append(start.ResultSchema.Required, "other")
				cfg.NodeDefinitions["start"] = start
				cfg.Topology.Edges[1].When.Field = "other"
				return cfg
			},
			wantErr: "must all reference the same field",
		},
		{
			name: "duplicate conditional value",
			build: func(t *testing.T) *Config {
				cfg := booleanBranchFixture()
				cfg.Topology.Edges[1].When.Equals = true
				return cfg
			},
			wantErr: "duplicate conditional branch",
		},
		{
			name: "condition value type mismatch",
			build: func(t *testing.T) *Config {
				cfg := booleanBranchFixture()
				cfg.Topology.Edges[0].When.Equals = "yes"
				return cfg
			},
			wantErr: "when.equals must be a boolean",
		},
		{
			name: "boolean condition without else or full coverage",
			build: func(t *testing.T) *Config {
				cfg := booleanBranchFixture()
				cfg.Topology.Edges = cfg.Topology.Edges[:1]
				return cfg
			},
			wantErr: "both true and false or define else",
		},
		{
			name: "open domain string condition without else",
			build: func(t *testing.T) *Config {
				cfg := basicFixture()
				cfg.NodeDefinitions["start"] = stringNode("mode")
				cfg.Topology.Edges = []Edge{
					{From: "start", To: "done", When: EdgeCondition{Kind: ConditionWhen, Field: "mode", Equals: "ship"}},
				}
				return cfg
			},
			wantErr: "must define else when branching on open-domain field",
		},
		{
			name: "non exhaustive enum without else",
			build: func(t *testing.T) *Config {
				cfg := basicFixture()
				cfg.NodeDefinitions["start"] = enumStringNode("mode", "ship", "skip")
				cfg.Topology.Edges = []Edge{
					{From: "start", To: "done", When: EdgeCondition{Kind: ConditionWhen, Field: "mode", Equals: "ship"}},
				}
				return cfg
			},
			wantErr: "must cover every enum value or define else",
		},
		{
			name: "root additionalProperties true",
			build: func(t *testing.T) *Config {
				cfg := basicFixture()
				allow := true
				start := cfg.NodeDefinitions["start"]
				start.ResultSchema.AdditionalProperties = &allow
				cfg.NodeDefinitions["start"] = start
				return cfg
			},
			wantErr: "additionalProperties: false",
		},
		{
			name: "root non object schema",
			build: func(t *testing.T) *Config {
				cfg := basicFixture()
				cfg.NodeDefinitions["start"] = NodeDefinition{
					Type:         NodeTypeAgent,
					SystemPrompt: "./prompt.md",
					ResultSchema: JSONSchema{Type: "string"},
				}
				return cfg
			},
			wantErr: "must be a root object",
		},
		{
			name: "top level type field",
			build: func(t *testing.T) *Config {
				cfg := basicFixture()
				start := cfg.NodeDefinitions["start"]
				start.ResultSchema.Properties["type"] = &JSONSchema{Type: "string"}
				start.ResultSchema.Required = append(start.ResultSchema.Required, "type")
				cfg.NodeDefinitions["start"] = start
				return cfg
			},
			wantErr: "top-level property \"type\"",
		},
		{
			name: "agent schema missing required property",
			build: func(t *testing.T) *Config {
				cfg := basicFixture()
				cfg.NodeDefinitions["start"] = NodeDefinition{
					Type:         NodeTypeAgent,
					SystemPrompt: "./prompt.md",
					ResultSchema: JSONSchema{
						Type:                 "object",
						AdditionalProperties: boolPtr(false),
						Properties: map[string]*JSONSchema{
							"file_paths": {
								Type:  "array",
								Items: &JSONSchema{Type: "string"},
							},
						},
					},
				}
				return cfg
			},
			wantErr: "must require property \"file_paths\"",
		},
		{
			name: "agent nested object must disallow extras",
			build: func(t *testing.T) *Config {
				cfg := basicFixture()
				allow := true
				cfg.NodeDefinitions["start"] = NodeDefinition{
					Type:         NodeTypeAgent,
					SystemPrompt: "./prompt.md",
					ResultSchema: JSONSchema{
						Type:                 "object",
						AdditionalProperties: boolPtr(false),
						Required:             []string{"meta"},
						Properties: map[string]*JSONSchema{
							"meta": {
								Type:                 "object",
								AdditionalProperties: &allow,
								Required:             []string{},
								Properties:           map[string]*JSONSchema{},
							},
						},
					},
				}
				return cfg
			},
			wantErr: "must set additionalProperties: false",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := tc.build(t)
			err := Validate(cfg)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)
		})
	}
}

func TestValidateAcceptsValidConfigs_TableDriven(t *testing.T) {
	cases := []struct {
		name  string
		build func(t *testing.T) *Config
	}{
		{
			name: "enum with else",
			build: func(t *testing.T) *Config {
				cfg := basicFixture()
				cfg.Topology.Nodes = append(cfg.Topology.Nodes, NodeRef{Name: "fallback"})
				cfg.NodeDefinitions["start"] = enumStringNode("mode", "ship", "skip")
				cfg.NodeDefinitions["fallback"] = terminalNode()
				cfg.Topology.Edges = []Edge{
					{From: "start", To: "done", When: EdgeCondition{Kind: ConditionWhen, Field: "mode", Equals: "ship"}},
					{From: "start", To: "fallback", When: EdgeCondition{Kind: ConditionElse}},
				}
				return cfg
			},
		},
		{
			name: "join with two incoming edges",
			build: func(t *testing.T) *Config {
				return &Config{
					Version: 1,
					Clarification: ClarificationConfig{
						MaxQuestions:          4,
						MaxOptionsPerQuestion: 4,
						MinOptionsPerQuestion: 2,
					},
					Topology: Topology{
						MaxIterations: 3,
						Entry:         "start",
						Nodes: []NodeRef{
							{Name: "start"},
							{Name: "left"},
							{Name: "right"},
							{Name: "join", Join: JoinAll},
							{Name: "end"},
						},
						Edges: []Edge{
							{From: "start", To: "left"},
							{From: "start", To: "right"},
							{From: "left", To: "join"},
							{From: "right", To: "join"},
							{From: "join", To: "end"},
						},
					},
					NodeDefinitions: map[string]NodeDefinition{
						"start": simpleAgentNode(),
						"left":  simpleAgentNode(),
						"right": simpleAgentNode(),
						"join":  simpleAgentNode(),
						"end":   terminalNode(),
					},
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := tc.build(t)
			require.NoError(t, Validate(cfg))
		})
	}
}

func TestMaterializeWritesConfigAndPrompts(t *testing.T) {
	setTaskConfigRuntimePath(t)

	workDir := t.TempDir()

	materialized, err := Materialize(workDir, "task-1", "")
	require.NoError(t, err)

	assert.FileExists(t, materialized.ConfigPath)
	assert.FileExists(t, filepath.Join(materialized.PromptDir, "draft_plan.md"))
	assert.FileExists(t, filepath.Join(materialized.PromptDir, "review_plan.md"))

	data, err := os.ReadFile(materialized.ConfigPath)
	require.NoError(t, err)
	assert.Contains(t, string(data), "runtime: codex")
	assert.Contains(t, string(data), "./prompts/draft_plan.md")
}

func TestMaterializePreservesBundleRelativePromptSubpaths(t *testing.T) {
	workDir := t.TempDir()
	cfgPath := writeConfigFile(t, `
version: 1
runtime: codex
clarification:
  max_questions: 4
  max_options_per_question: 4
  min_options_per_question: 2
topology:
  max_iterations: 1
  entry: start
  nodes:
    - name: start
    - name: done
  edges:
    - from: start
      to: done
node_definitions:
  start:
    system_prompt: ./prompts/nested/start.md
    result_schema:
      type: object
      additionalProperties: false
      properties: {}
  done:
    type: terminal
`)
	sourcePromptDir := filepath.Join(filepath.Dir(cfgPath), "prompts", "nested")
	require.NoError(t, os.MkdirAll(sourcePromptDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(sourcePromptDir, "start.md"), []byte("# nested prompt"), 0o644))

	materialized, err := Materialize(workDir, "task-nested", cfgPath)
	require.NoError(t, err)

	assert.FileExists(t, filepath.Join(materialized.PromptDir, "nested", "start.md"))
	assert.NoFileExists(t, filepath.Join(materialized.PromptDir, "prompts", "nested", "start.md"))
	data, err := os.ReadFile(materialized.ConfigPath)
	require.NoError(t, err)
	assert.Contains(t, string(data), "./prompts/nested/start.md")
}

func TestMaterializeRejectsPromptPathsOutsideBundle(t *testing.T) {
	workDir := t.TempDir()
	cfgPath := writeConfigFile(t, `
version: 1
runtime: codex
clarification:
  max_questions: 4
  max_options_per_question: 4
  min_options_per_question: 2
topology:
  max_iterations: 1
  entry: start
  nodes:
    - name: start
    - name: done
  edges:
    - from: start
      to: done
node_definitions:
  start:
    system_prompt: ../shared.md
    result_schema:
      type: object
      additionalProperties: false
      properties: {}
  done:
    type: terminal
`)

	_, err := Materialize(workDir, "task-invalid-prompt", cfgPath)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must stay within the config bundle")
}

func TestMaterializeRejectsPromptPathCollisions(t *testing.T) {
	workDir := t.TempDir()
	cfgPath := writeConfigFile(t, `
version: 1
runtime: codex
clarification:
  max_questions: 4
  max_options_per_question: 4
  min_options_per_question: 2
topology:
  max_iterations: 2
  entry: first
  nodes:
    - name: first
    - name: second
    - name: done
  edges:
    - from: first
      to: second
    - from: second
      to: done
node_definitions:
  first:
    system_prompt: ./prompts/plan.md
    result_schema:
      type: object
      additionalProperties: false
      properties: {}
  second:
    system_prompt: ./prompts/review/../plan.md
    result_schema:
      type: object
      additionalProperties: false
      properties: {}
  done:
    type: terminal
`)
	promptDir := filepath.Join(filepath.Dir(cfgPath), "prompts")
	require.NoError(t, os.MkdirAll(promptDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(promptDir, "plan.md"), []byte("# shared prompt"), 0o644))

	_, err := Materialize(workDir, "task-colliding-prompts", cfgPath)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "both materialize to")
}

func TestTaskRuntimeSelection(t *testing.T) {
	t.Run("explicit claude runtime loads from config", func(t *testing.T) {
		path := writeConfigFile(t, `
version: 1
runtime: claude-code
clarification:
  max_questions: 4
  max_options_per_question: 4
  min_options_per_question: 2
topology:
  max_iterations: 1
  entry: start
  nodes:
    - name: start
    - name: done
  edges:
    - from: start
      to: done
node_definitions:
  start:
    system_prompt: ./prompt.md
    result_schema:
      type: object
      additionalProperties: false
      properties: {}
  done:
    type: terminal
`)

		cfg, err := Load(path)
		require.NoError(t, err)
		assert.Equal(t, appconfig.RuntimeClaudeCode, cfg.Runtime)
	})

	t.Run("runtime from config", func(t *testing.T) {
		cfg := basicFixture()
		cfg.Runtime = appconfig.RuntimeClaudeCode

		runtime, err := ResolveRuntime(cfg)
		require.NoError(t, err)
		assert.Equal(t, appconfig.RuntimeClaudeCode, runtime)
	})

	t.Run("runtime without explicit config uses preferred path runtime", func(t *testing.T) {
		setTaskConfigRuntimePath(t, "claude")

		cfg := basicFixture()
		cfg.Runtime = ""

		runtime, err := ResolveRuntime(cfg)
		require.NoError(t, err)
		assert.Equal(t, appconfig.RuntimeClaudeCode, runtime)
	})

	t.Run("materialize persists resolved runtime", func(t *testing.T) {
		workDir := t.TempDir()
		cfgPath := writeConfigFile(t, `
version: 1
runtime: claude-code
clarification:
  max_questions: 4
  max_options_per_question: 4
  min_options_per_question: 2
topology:
  max_iterations: 1
  entry: start
  nodes:
    - name: start
    - name: done
  edges:
    - from: start
      to: done
node_definitions:
  start:
    system_prompt: ./prompt.md
    result_schema:
      type: object
      additionalProperties: false
      properties: {}
  done:
    type: terminal
`)
		require.NoError(t, os.WriteFile(filepath.Join(filepath.Dir(cfgPath), "prompt.md"), []byte("# prompt"), 0o644))

		materialized, err := Materialize(workDir, "task-claude", cfgPath)
		require.NoError(t, err)
		assert.Equal(t, appconfig.RuntimeClaudeCode, materialized.Config.Runtime)

		persisted, err := Load(materialized.ConfigPath)
		require.NoError(t, err)
		assert.Equal(t, appconfig.RuntimeClaudeCode, persisted.Runtime)
	})

	t.Run("materialize persists preferred runtime for runtime-less configs", func(t *testing.T) {
		setTaskConfigRuntimePath(t, "claude")

		workDir := t.TempDir()
		cfgPath := writeConfigFile(t, `
version: 1
clarification:
  max_questions: 4
  max_options_per_question: 4
  min_options_per_question: 2
topology:
  max_iterations: 1
  entry: start
  nodes:
    - name: start
    - name: done
  edges:
    - from: start
      to: done
node_definitions:
  start:
    system_prompt: ./prompt.md
    result_schema:
      type: object
      additionalProperties: false
      properties: {}
  done:
    type: terminal
`)
		require.NoError(t, os.WriteFile(filepath.Join(filepath.Dir(cfgPath), "prompt.md"), []byte("# prompt"), 0o644))

		materialized, err := Materialize(workDir, "task-runtime-less", cfgPath)
		require.NoError(t, err)
		assert.Equal(t, appconfig.RuntimeClaudeCode, materialized.Config.Runtime)

		persisted, err := Load(materialized.ConfigPath)
		require.NoError(t, err)
		assert.Equal(t, appconfig.RuntimeClaudeCode, persisted.Runtime)
	})
}

func writeConfigFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	return path
}

func setTaskConfigRuntimePath(t *testing.T, commands ...string) {
	t.Helper()
	dir := t.TempDir()
	for _, command := range commands {
		path := filepath.Join(dir, command)
		contents := []byte("#!/bin/sh\nexit 0\n")
		if runtime.GOOS == "windows" {
			path += ".exe"
			contents = []byte{}
		}
		require.NoError(t, os.WriteFile(path, contents, 0o755))
	}
	t.Setenv("PATH", dir)
}

func basicFixture() *Config {
	return &Config{
		Version: 1,
		Clarification: ClarificationConfig{
			MaxQuestions:          4,
			MaxOptionsPerQuestion: 4,
			MinOptionsPerQuestion: 2,
		},
		Topology: Topology{
			MaxIterations: 3,
			Entry:         "start",
			Nodes: []NodeRef{
				{Name: "start"},
				{Name: "done"},
			},
			Edges: []Edge{
				{From: "start", To: "done"},
			},
		},
		NodeDefinitions: map[string]NodeDefinition{
			"start": simpleAgentNode(),
			"done":  terminalNode(),
		},
	}
}

func booleanBranchFixture() *Config {
	cfg := basicFixture()
	cfg.Topology.Nodes = append(cfg.Topology.Nodes, NodeRef{Name: "fallback"})
	cfg.NodeDefinitions["start"] = booleanNode("approved")
	cfg.NodeDefinitions["fallback"] = terminalNode()
	cfg.Topology.Edges = []Edge{
		{From: "start", To: "done", When: EdgeCondition{Kind: ConditionWhen, Field: "approved", Equals: true}},
		{From: "start", To: "fallback", When: EdgeCondition{Kind: ConditionWhen, Field: "approved", Equals: false}},
	}
	return cfg
}

func booleanElseFixture() *Config {
	cfg := basicFixture()
	cfg.Topology.Nodes = append(cfg.Topology.Nodes, NodeRef{Name: "fallback"})
	cfg.NodeDefinitions["start"] = booleanNode("approved")
	cfg.NodeDefinitions["fallback"] = terminalNode()
	cfg.Topology.Edges = []Edge{
		{From: "start", To: "done", When: EdgeCondition{Kind: ConditionWhen, Field: "approved", Equals: true}},
		{From: "start", To: "fallback", When: EdgeCondition{Kind: ConditionElse}},
	}
	return cfg
}

func simpleAgentNode() NodeDefinition {
	allow := false
	return NodeDefinition{
		Type:         NodeTypeAgent,
		SystemPrompt: "./prompt.md",
		ResultSchema: JSONSchema{
			Type:                 "object",
			AdditionalProperties: &allow,
			Properties:           map[string]*JSONSchema{},
		},
	}
}

func terminalNode() NodeDefinition {
	return NodeDefinition{
		Type: NodeTypeTerminal,
	}
}

func booleanNode(field string) NodeDefinition {
	allow := false
	return NodeDefinition{
		Type:         NodeTypeAgent,
		SystemPrompt: "./prompt.md",
		ResultSchema: JSONSchema{
			Type:                 "object",
			AdditionalProperties: &allow,
			Required:             []string{field},
			Properties: map[string]*JSONSchema{
				field: {Type: "boolean"},
			},
		},
	}
}

func stringNode(field string) NodeDefinition {
	allow := false
	return NodeDefinition{
		Type:         NodeTypeAgent,
		SystemPrompt: "./prompt.md",
		ResultSchema: JSONSchema{
			Type:                 "object",
			AdditionalProperties: &allow,
			Required:             []string{field},
			Properties: map[string]*JSONSchema{
				field: {Type: "string"},
			},
		},
	}
}

func enumStringNode(field string, values ...string) NodeDefinition {
	allow := false
	enum := make([]interface{}, 0, len(values))
	for _, value := range values {
		enum = append(enum, value)
	}
	return NodeDefinition{
		Type:         NodeTypeAgent,
		SystemPrompt: "./prompt.md",
		ResultSchema: JSONSchema{
			Type:                 "object",
			AdditionalProperties: &allow,
			Required:             []string{field},
			Properties: map[string]*JSONSchema{
				field: {Type: "string", Enum: enum},
			},
		},
	}
}

func boolPtr(value bool) *bool {
	return &value
}

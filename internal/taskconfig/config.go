package taskconfig

import (
	"bytes"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	appconfig "github.com/LaLanMo/muxagent-cli/internal/config"
	"gopkg.in/yaml.v3"
)

//go:embed defaults
var defaultsFS embed.FS

const (
	defaultConfigAsset = "defaults/default.yaml"
	defaultPromptsDir  = "defaults/prompts"
)

type Config struct {
	Version         int                       `yaml:"version" json:"version"`
	Description     string                    `yaml:"description,omitempty" json:"description,omitempty"`
	Runtime         appconfig.RuntimeID       `yaml:"runtime" json:"runtime"`
	Clarification   ClarificationConfig       `yaml:"clarification" json:"clarification"`
	Topology        Topology                  `yaml:"topology" json:"topology"`
	NodeDefinitions map[string]NodeDefinition `yaml:"node_definitions" json:"node_definitions"`
}

type ClarificationConfig struct {
	MaxQuestions          int `yaml:"max_questions" json:"max_questions"`
	MaxOptionsPerQuestion int `yaml:"max_options_per_question" json:"max_options_per_question"`
	MinOptionsPerQuestion int `yaml:"min_options_per_question" json:"min_options_per_question"`
}

type Topology struct {
	MaxIterations int       `yaml:"max_iterations" json:"max_iterations"`
	Entry         string    `yaml:"entry" json:"entry"`
	Nodes         []NodeRef `yaml:"nodes" json:"nodes"`
	Edges         []Edge    `yaml:"edges" json:"edges"`
}

type NodeRef struct {
	Name          string `yaml:"name" json:"name"`
	MaxIterations int    `yaml:"max_iterations,omitempty" json:"max_iterations,omitempty"`
	Join          Join   `yaml:"join,omitempty" json:"join,omitempty"`
}

type Join string

const (
	JoinAll Join = "all"
	JoinAny Join = "any"
)

type Edge struct {
	From string        `yaml:"from" json:"from"`
	To   string        `yaml:"to" json:"to"`
	When EdgeCondition `yaml:"when,omitempty" json:"when,omitempty"`
}

type EdgeCondition struct {
	Kind   EdgeConditionKind `yaml:"kind,omitempty" json:"kind,omitempty"`
	Field  string            `yaml:"field,omitempty" json:"field,omitempty"`
	Equals interface{}       `yaml:"equals,omitempty" json:"equals,omitempty"`
}

type EdgeConditionKind string

const (
	ConditionNone EdgeConditionKind = ""
	ConditionWhen EdgeConditionKind = "when"
	ConditionElse EdgeConditionKind = "else"
)

func (c *EdgeCondition) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind == 0 {
		*c = EdgeCondition{}
		return nil
	}
	if node.Kind == yaml.ScalarNode {
		if node.Value != "else" {
			return fmt.Errorf("unsupported scalar edge condition %q", node.Value)
		}
		*c = EdgeCondition{Kind: ConditionElse}
		return nil
	}
	if node.Kind != yaml.MappingNode {
		return fmt.Errorf("edge condition must be a mapping or \"else\"")
	}
	allowed := map[string]struct{}{
		"field":  {},
		"equals": {},
	}
	for i := 0; i < len(node.Content); i += 2 {
		key := node.Content[i].Value
		if _, ok := allowed[key]; !ok {
			return fmt.Errorf("unsupported edge condition field %q", key)
		}
	}
	type raw EdgeCondition
	var inner raw
	if err := node.Decode(&inner); err != nil {
		return err
	}
	*c = EdgeCondition(inner)
	c.Kind = ConditionWhen
	return nil
}

func (c EdgeCondition) MarshalYAML() (interface{}, error) {
	switch c.Kind {
	case ConditionElse:
		return "else", nil
	case ConditionWhen:
		return map[string]interface{}{
			"field":  c.Field,
			"equals": c.Equals,
		}, nil
	default:
		return nil, nil
	}
}

type NodeDefinition struct {
	Type                   NodeType   `yaml:"type,omitempty" json:"type,omitempty"`
	SystemPrompt           string     `yaml:"system_prompt,omitempty" json:"system_prompt,omitempty"`
	MaxClarificationRounds int        `yaml:"max_clarification_rounds,omitempty" json:"max_clarification_rounds,omitempty"`
	ResultSchema           JSONSchema `yaml:"result_schema" json:"result_schema"`
}

type NodeType string

const (
	NodeTypeAgent    NodeType = "agent"
	NodeTypeHuman    NodeType = "human"
	NodeTypeTerminal NodeType = "terminal"
)

type JSONSchema struct {
	Type                 string                 `yaml:"type,omitempty" json:"type,omitempty"`
	AdditionalProperties *bool                  `yaml:"additionalProperties,omitempty" json:"additionalProperties,omitempty"`
	Required             []string               `yaml:"required,omitempty" json:"required,omitempty"`
	Properties           map[string]*JSONSchema `yaml:"properties,omitempty" json:"properties,omitempty"`
	Items                *JSONSchema            `yaml:"items,omitempty" json:"items,omitempty"`
	Description          string                 `yaml:"description,omitempty" json:"description,omitempty"`
	Enum                 []interface{}          `yaml:"enum,omitempty" json:"enum,omitempty"`
	OneOf                []*JSONSchema          `yaml:"oneOf,omitempty" json:"oneOf,omitempty"`
	MaxItems             *int                   `yaml:"maxItems,omitempty" json:"maxItems,omitempty"`
	MinItems             *int                   `yaml:"minItems,omitempty" json:"minItems,omitempty"`
}

type MaterializedConfig struct {
	ConfigPath string
	TaskDir    string
	PromptDir  string
	Config     *Config
}

type rawConfig struct {
	Version         *int                         `yaml:"version"`
	Description     *string                      `yaml:"description"`
	Runtime         *appconfig.RuntimeID         `yaml:"runtime"`
	Clarification   *rawClarificationConfig      `yaml:"clarification"`
	Topology        *rawTopology                 `yaml:"topology"`
	NodeDefinitions map[string]rawNodeDefinition `yaml:"node_definitions"`
}

type rawClarificationConfig struct {
	MaxQuestions          *int `yaml:"max_questions"`
	MaxOptionsPerQuestion *int `yaml:"max_options_per_question"`
	MinOptionsPerQuestion *int `yaml:"min_options_per_question"`
}

type rawTopology struct {
	MaxIterations *int         `yaml:"max_iterations"`
	Entry         string       `yaml:"entry"`
	Nodes         []rawNodeRef `yaml:"nodes"`
	Edges         []Edge       `yaml:"edges"`
}

type rawNodeRef struct {
	Name          string `yaml:"name"`
	MaxIterations *int   `yaml:"max_iterations,omitempty"`
	Join          Join   `yaml:"join,omitempty"`
}

type rawNodeDefinition struct {
	Type                   NodeType   `yaml:"type,omitempty"`
	SystemPrompt           *string    `yaml:"system_prompt,omitempty"`
	MaxClarificationRounds *int       `yaml:"max_clarification_rounds,omitempty"`
	ResultSchema           JSONSchema `yaml:"result_schema"`
}

func LoadDefault() (*Config, error) {
	return loadEmbeddedConfig(defaultConfigAsset)
}

func LoadBuiltin(builtinID string) (*Config, error) {
	return loadEmbeddedBuiltinConfig(builtinID)
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return parse(data)
}

func loadEmbeddedConfig(assetPath string) (*Config, error) {
	data, err := defaultsFS.ReadFile(assetPath)
	if err != nil {
		return nil, err
	}
	return parse(data)
}

func parse(data []byte) (*Config, error) {
	raw, err := decodeRawConfig(data)
	if err != nil {
		return nil, err
	}
	if err := validateRawConfig(raw); err != nil {
		return nil, err
	}
	cfg := raw.toConfig()
	applyDefaults(&cfg)
	if err := Validate(&cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func applyDefaults(cfg *Config) {
	if strings.TrimSpace(string(cfg.Runtime)) == "" {
		cfg.Runtime = appconfig.PreferredRuntimeFromPATH()
	}
	if cfg.Clarification.MaxQuestions == 0 {
		cfg.Clarification.MaxQuestions = 4
	}
	if cfg.Clarification.MaxOptionsPerQuestion == 0 {
		cfg.Clarification.MaxOptionsPerQuestion = 4
	}
	if cfg.Clarification.MinOptionsPerQuestion == 0 {
		cfg.Clarification.MinOptionsPerQuestion = 2
	}
	if cfg.Topology.MaxIterations == 0 {
		cfg.Topology.MaxIterations = 5
	}
	for name, def := range cfg.NodeDefinitions {
		if def.Type == "" {
			def.Type = NodeTypeAgent
			cfg.NodeDefinitions[name] = def
		}
	}
}

func Validate(cfg *Config) error {
	if cfg.Version != 1 {
		return fmt.Errorf("unsupported config version %d", cfg.Version)
	}
	if strings.TrimSpace(string(cfg.Runtime)) == "" {
		cfg.Runtime = appconfig.PreferredRuntimeFromPATH()
	}
	if !appconfig.IsSupportedRuntime(cfg.Runtime) {
		return fmt.Errorf("runtime %q is not supported", cfg.Runtime)
	}
	if cfg.Topology.Entry == "" {
		return errors.New("topology.entry is required")
	}
	if cfg.Topology.MaxIterations <= 0 {
		return errors.New("topology.max_iterations must be > 0")
	}
	if cfg.Clarification.MinOptionsPerQuestion <= 0 || cfg.Clarification.MaxOptionsPerQuestion < cfg.Clarification.MinOptionsPerQuestion {
		return errors.New("clarification option bounds are invalid")
	}
	if cfg.Clarification.MaxQuestions <= 0 {
		return errors.New("clarification.max_questions must be > 0")
	}
	if len(cfg.Topology.Nodes) == 0 {
		return errors.New("topology.nodes is required")
	}
	nodeRefs := make(map[string]NodeRef, len(cfg.Topology.Nodes))
	incomingCounts := map[string]int{}
	for _, node := range cfg.Topology.Nodes {
		if node.Name == "" {
			return errors.New("topology.nodes.name is required")
		}
		if _, exists := nodeRefs[node.Name]; exists {
			return fmt.Errorf("duplicate topology node %q", node.Name)
		}
		if node.Join != "" && node.Join != JoinAll && node.Join != JoinAny {
			return fmt.Errorf("node %q has invalid join %q", node.Name, node.Join)
		}
		if node.MaxIterations < 0 {
			return fmt.Errorf("node %q max_iterations must be >= 0", node.Name)
		}
		nodeRefs[node.Name] = node
	}
	if _, ok := nodeRefs[cfg.Topology.Entry]; !ok {
		return fmt.Errorf("topology.entry %q is not declared", cfg.Topology.Entry)
	}
	if len(cfg.NodeDefinitions) == 0 {
		return errors.New("node_definitions is required")
	}
	for name := range nodeRefs {
		def, ok := cfg.NodeDefinitions[name]
		if !ok {
			return fmt.Errorf("missing node definition for %q", name)
		}
		if err := validateNodeDefinition(name, def); err != nil {
			return err
		}
	}
	for name := range cfg.NodeDefinitions {
		if _, ok := nodeRefs[name]; !ok {
			return fmt.Errorf("node definition %q is not declared in topology.nodes", name)
		}
	}
	edgesByFrom := map[string][]Edge{}
	seenEdges := map[string]struct{}{}
	for _, edge := range cfg.Topology.Edges {
		if _, ok := nodeRefs[edge.From]; !ok {
			return fmt.Errorf("edge.from %q is not declared", edge.From)
		}
		if _, ok := nodeRefs[edge.To]; !ok {
			return fmt.Errorf("edge.to %q is not declared", edge.To)
		}
		incomingCounts[edge.To]++
		key, err := semanticEdgeKey(edge)
		if err != nil {
			return err
		}
		if _, exists := seenEdges[key]; exists {
			return fmt.Errorf("duplicate edge %q -> %q", edge.From, edge.To)
		}
		seenEdges[key] = struct{}{}
		edgesByFrom[edge.From] = append(edgesByFrom[edge.From], edge)
	}
	for _, node := range cfg.Topology.Nodes {
		if node.Join != "" && incomingCounts[node.Name] < 2 {
			return fmt.Errorf("node %q cannot declare join with fewer than 2 incoming edges", node.Name)
		}
	}
	for from, edges := range edgesByFrom {
		if err := validateEdgeGroup(from, edges, cfg.NodeDefinitions[from]); err != nil {
			return err
		}
	}
	hasTerminal := false
	for _, node := range cfg.Topology.Nodes {
		def := cfg.NodeDefinitions[node.Name]
		hasOutgoing := len(edgesByFrom[node.Name]) > 0
		if def.Type == NodeTypeTerminal {
			hasTerminal = true
			if hasOutgoing {
				return fmt.Errorf("terminal node %q cannot have outgoing edges", node.Name)
			}
		} else if def.Type == NodeTypeAgent && !hasOutgoing {
			return fmt.Errorf("agent node %q has no outgoing edges; use type: terminal for intentional end states", node.Name)
		}
	}
	if !hasTerminal {
		return errors.New("topology must contain at least one terminal node")
	}
	return nil
}

func validateNodeDefinition(name string, def NodeDefinition) error {
	if def.Type != NodeTypeAgent && def.Type != NodeTypeHuman && def.Type != NodeTypeTerminal {
		return fmt.Errorf("node %q has invalid type %q", name, def.Type)
	}
	if def.Type == NodeTypeTerminal {
		if strings.TrimSpace(def.SystemPrompt) != "" {
			return fmt.Errorf("terminal node %q cannot define system_prompt", name)
		}
		if def.MaxClarificationRounds > 0 {
			return fmt.Errorf("terminal node %q cannot define max_clarification_rounds", name)
		}
		return nil
	}
	if def.Type == NodeTypeAgent && strings.TrimSpace(def.SystemPrompt) == "" {
		return fmt.Errorf("agent node %q must define system_prompt", name)
	}
	if def.Type == NodeTypeHuman && strings.TrimSpace(def.SystemPrompt) != "" {
		return fmt.Errorf("human node %q cannot define system_prompt", name)
	}
	if def.Type == NodeTypeHuman && def.MaxClarificationRounds > 0 {
		return fmt.Errorf("human node %q cannot define max_clarification_rounds", name)
	}
	if def.ResultSchema.Type != "object" || len(def.ResultSchema.OneOf) > 0 {
		return fmt.Errorf("node %q result_schema must be a root object", name)
	}
	if def.ResultSchema.AdditionalProperties == nil || *def.ResultSchema.AdditionalProperties {
		return fmt.Errorf("node %q result_schema must set additionalProperties: false at the root", name)
	}
	if _, reserved := def.ResultSchema.Properties["type"]; reserved {
		return fmt.Errorf("node %q result_schema cannot define top-level property \"type\"", name)
	}
	if err := validateSchema(fmt.Sprintf("node %q result_schema", name), &def.ResultSchema); err != nil {
		return err
	}
	if def.Type == NodeTypeAgent {
		if err := validateAgentStructuredOutputSchema(fmt.Sprintf("node %q result_schema", name), &def.ResultSchema); err != nil {
			return err
		}
	}
	return nil
}

func validateSchema(label string, schema *JSONSchema) error {
	if schema == nil {
		return fmt.Errorf("%s is required", label)
	}
	if schema.Type == "" && len(schema.OneOf) == 0 {
		return fmt.Errorf("%s must declare type or oneOf", label)
	}
	if len(schema.OneOf) > 0 {
		for idx, inner := range schema.OneOf {
			if err := validateSchema(fmt.Sprintf("%s.oneOf[%d]", label, idx), inner); err != nil {
				return err
			}
		}
		return nil
	}
	switch schema.Type {
	case "object":
		if schema.Properties == nil {
			schema.Properties = map[string]*JSONSchema{}
		}
		for key, child := range schema.Properties {
			if err := validateSchema(label+"."+key, child); err != nil {
				return err
			}
		}
	case "array":
		if schema.Items == nil {
			return fmt.Errorf("%s.items is required", label)
		}
		if err := validateSchema(label+".items", schema.Items); err != nil {
			return err
		}
	case "string", "boolean", "number", "integer":
	default:
		return fmt.Errorf("%s has unsupported type %q", label, schema.Type)
	}
	return nil
}

func validateAgentStructuredOutputSchema(label string, schema *JSONSchema) error {
	if schema == nil {
		return fmt.Errorf("%s is required", label)
	}
	if len(schema.OneOf) > 0 {
		return fmt.Errorf("%s cannot use oneOf", label)
	}
	switch schema.Type {
	case "object":
		if schema.AdditionalProperties == nil || *schema.AdditionalProperties {
			return fmt.Errorf("%s must set additionalProperties: false", label)
		}
		if schema.Properties == nil {
			schema.Properties = map[string]*JSONSchema{}
		}
		required := map[string]struct{}{}
		for _, key := range schema.Required {
			required[key] = struct{}{}
		}
		for key := range schema.Properties {
			if _, ok := required[key]; !ok {
				return fmt.Errorf("%s must require property %q", label, key)
			}
		}
		for key := range required {
			if _, ok := schema.Properties[key]; !ok {
				return fmt.Errorf("%s required field %q must exist in properties", label, key)
			}
		}
		for key, child := range schema.Properties {
			if err := validateAgentStructuredOutputSchema(label+"."+key, child); err != nil {
				return err
			}
		}
	case "array":
		if schema.Items == nil {
			return fmt.Errorf("%s.items is required", label)
		}
		if err := validateAgentStructuredOutputSchema(label+".items", schema.Items); err != nil {
			return err
		}
	case "string", "boolean", "number", "integer":
	default:
		return fmt.Errorf("%s has unsupported type %q", label, schema.Type)
	}
	return nil
}

func decodeRawConfig(data []byte) (*rawConfig, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var raw rawConfig
	if err := dec.Decode(&raw); err != nil {
		return nil, err
	}
	return &raw, nil
}

func validateRawConfig(raw *rawConfig) error {
	if raw.Version == nil {
		return errors.New("version is required")
	}
	if raw.Topology == nil {
		return errors.New("topology is required")
	}
	if raw.Topology.MaxIterations != nil && *raw.Topology.MaxIterations <= 0 {
		return errors.New("topology.max_iterations must be > 0")
	}
	if raw.Clarification != nil {
		if raw.Clarification.MaxQuestions != nil && *raw.Clarification.MaxQuestions <= 0 {
			return errors.New("clarification.max_questions must be > 0")
		}
		if raw.Clarification.MaxOptionsPerQuestion != nil && *raw.Clarification.MaxOptionsPerQuestion <= 0 {
			return errors.New("clarification.max_options_per_question must be > 0")
		}
		if raw.Clarification.MinOptionsPerQuestion != nil && *raw.Clarification.MinOptionsPerQuestion <= 0 {
			return errors.New("clarification.min_options_per_question must be > 0")
		}
		if raw.Clarification.MaxOptionsPerQuestion != nil && raw.Clarification.MinOptionsPerQuestion != nil && *raw.Clarification.MaxOptionsPerQuestion < *raw.Clarification.MinOptionsPerQuestion {
			return errors.New("clarification option bounds are invalid")
		}
	}
	for _, node := range raw.Topology.Nodes {
		if node.MaxIterations != nil && *node.MaxIterations <= 0 {
			return fmt.Errorf("node %q max_iterations must be > 0", node.Name)
		}
	}
	for name, def := range raw.NodeDefinitions {
		if def.Type == NodeTypeHuman {
			if def.SystemPrompt != nil {
				return fmt.Errorf("human node %q cannot define system_prompt", name)
			}
			if def.MaxClarificationRounds != nil {
				return fmt.Errorf("human node %q cannot define max_clarification_rounds", name)
			}
		}
	}
	return nil
}

func (raw *rawConfig) toConfig() Config {
	cfg := Config{
		NodeDefinitions: map[string]NodeDefinition{},
	}
	if raw.Version != nil {
		cfg.Version = *raw.Version
	}
	if raw.Description != nil {
		cfg.Description = *raw.Description
	}
	if raw.Runtime != nil {
		cfg.Runtime = *raw.Runtime
	}
	if raw.Clarification != nil {
		if raw.Clarification.MaxQuestions != nil {
			cfg.Clarification.MaxQuestions = *raw.Clarification.MaxQuestions
		}
		if raw.Clarification.MaxOptionsPerQuestion != nil {
			cfg.Clarification.MaxOptionsPerQuestion = *raw.Clarification.MaxOptionsPerQuestion
		}
		if raw.Clarification.MinOptionsPerQuestion != nil {
			cfg.Clarification.MinOptionsPerQuestion = *raw.Clarification.MinOptionsPerQuestion
		}
	}
	if raw.Topology != nil {
		cfg.Topology.Entry = raw.Topology.Entry
		if raw.Topology.MaxIterations != nil {
			cfg.Topology.MaxIterations = *raw.Topology.MaxIterations
		}
		cfg.Topology.Edges = append(cfg.Topology.Edges, raw.Topology.Edges...)
		for _, node := range raw.Topology.Nodes {
			cfg.Topology.Nodes = append(cfg.Topology.Nodes, NodeRef{
				Name: node.Name,
				Join: node.Join,
			})
			if node.MaxIterations != nil {
				cfg.Topology.Nodes[len(cfg.Topology.Nodes)-1].MaxIterations = *node.MaxIterations
			}
		}
	}
	for name, def := range raw.NodeDefinitions {
		nodeDef := NodeDefinition{
			Type:         def.Type,
			ResultSchema: def.ResultSchema,
		}
		if def.SystemPrompt != nil {
			nodeDef.SystemPrompt = *def.SystemPrompt
		}
		if def.MaxClarificationRounds != nil {
			nodeDef.MaxClarificationRounds = *def.MaxClarificationRounds
		}
		cfg.NodeDefinitions[name] = nodeDef
	}
	return cfg
}

func validateEdgeGroup(from string, edges []Edge, def NodeDefinition) error {
	state := struct {
		unconditional bool
		conditional   bool
		elseCount     int
		field         string
		seenEquals    map[string]struct{}
		hasElse       bool
	}{seenEquals: map[string]struct{}{}}

	for _, edge := range edges {
		switch edge.When.Kind {
		case ConditionNone:
			state.unconditional = true
		case ConditionElse:
			state.conditional = true
			state.elseCount++
			state.hasElse = true
			if state.elseCount > 1 {
				return fmt.Errorf("node %q has multiple else edges", from)
			}
		case ConditionWhen:
			if edge.When.Field == "" {
				return fmt.Errorf("edge %q -> %q has empty when.field", edge.From, edge.To)
			}
			state.conditional = true
			if state.field == "" {
				state.field = edge.When.Field
			} else if state.field != edge.When.Field {
				return fmt.Errorf("node %q conditional edges must all reference the same field", from)
			}
			fieldSchema, err := conditionalFieldSchema(def.ResultSchema, edge.When.Field)
			if err != nil {
				return fmt.Errorf("node %q: %w", from, err)
			}
			if err := validateConditionValueType(edge.When.Equals, fieldSchema); err != nil {
				return fmt.Errorf("node %q field %q: %w", from, edge.When.Field, err)
			}
			key, err := comparableValueKey(edge.When.Equals)
			if err != nil {
				return fmt.Errorf("node %q field %q: %w", from, edge.When.Field, err)
			}
			if _, exists := state.seenEquals[key]; exists {
				return fmt.Errorf("node %q has duplicate conditional branch for %s=%v", from, edge.When.Field, edge.When.Equals)
			}
			state.seenEquals[key] = struct{}{}
		default:
			return fmt.Errorf("edge %q -> %q has invalid condition kind", edge.From, edge.To)
		}
		if state.unconditional && state.conditional {
			return fmt.Errorf("node %q mixes unconditional and conditional edges", from)
		}
	}

	if state.conditional && len(state.seenEquals) == 0 {
		return fmt.Errorf("node %q has else edge without explicit conditional siblings", from)
	}
	if state.conditional {
		fieldSchema, err := conditionalFieldSchema(def.ResultSchema, state.field)
		if err != nil {
			return fmt.Errorf("node %q: %w", from, err)
		}
		if err := validateConditionExhaustiveness(from, fieldSchema, state.seenEquals, state.hasElse); err != nil {
			return err
		}
	}

	return nil
}

func semanticEdgeKey(edge Edge) (string, error) {
	key := fmt.Sprintf("%s|%s|%s|%s|", edge.From, edge.To, edge.When.Kind, edge.When.Field)
	if edge.When.Kind != ConditionWhen {
		return key, nil
	}
	valueKey, err := comparableValueKey(edge.When.Equals)
	if err != nil {
		return "", err
	}
	return key + valueKey, nil
}

func conditionalFieldSchema(root JSONSchema, field string) (*JSONSchema, error) {
	if root.Type != "object" || len(root.OneOf) > 0 {
		return nil, errors.New("conditional edges require a root object result_schema")
	}
	child, ok := root.Properties[field]
	if !ok || child == nil {
		return nil, fmt.Errorf("when.field %q does not exist in result_schema", field)
	}
	if len(child.OneOf) > 0 {
		return nil, fmt.Errorf("when.field %q cannot use oneOf", field)
	}
	switch child.Type {
	case "boolean", "string", "integer", "number":
		return child, nil
	default:
		return nil, fmt.Errorf("when.field %q must be a scalar boolean/string/integer/number", field)
	}
}

func validateConditionValueType(value interface{}, schema *JSONSchema) error {
	switch schema.Type {
	case "boolean":
		if _, ok := value.(bool); !ok {
			return errors.New("when.equals must be a boolean")
		}
	case "string":
		if _, ok := value.(string); !ok {
			return errors.New("when.equals must be a string")
		}
	case "integer":
		if !isIntegerValue(value) {
			return errors.New("when.equals must be an integer")
		}
	case "number":
		if !isNumberValue(value) {
			return errors.New("when.equals must be a number")
		}
	default:
		return fmt.Errorf("unsupported condition field type %q", schema.Type)
	}
	return nil
}

func validateConditionExhaustiveness(from string, schema *JSONSchema, seen map[string]struct{}, hasElse bool) error {
	switch schema.Type {
	case "boolean":
		if hasElse {
			return nil
		}
		trueKey, _ := comparableValueKey(true)
		falseKey, _ := comparableValueKey(false)
		if _, ok := seen[trueKey]; !ok {
			return fmt.Errorf("node %q must cover boolean field with both true and false or define else", from)
		}
		if _, ok := seen[falseKey]; !ok {
			return fmt.Errorf("node %q must cover boolean field with both true and false or define else", from)
		}
	case "string", "integer", "number":
		if len(schema.Enum) > 0 {
			if hasElse {
				return nil
			}
			for _, candidate := range schema.Enum {
				key, err := comparableValueKey(candidate)
				if err != nil {
					return fmt.Errorf("node %q enum coverage: %w", from, err)
				}
				if _, ok := seen[key]; !ok {
					return fmt.Errorf("node %q must cover every enum value or define else", from)
				}
			}
			return nil
		}
		if !hasElse {
			return fmt.Errorf("node %q must define else when branching on open-domain field", from)
		}
	}
	return nil
}

func comparableValueKey(value interface{}) (string, error) {
	switch v := value.(type) {
	case bool:
		if v {
			return "bool:true", nil
		}
		return "bool:false", nil
	case string:
		return "string:" + v, nil
	case int:
		return fmt.Sprintf("number:%d", v), nil
	case int8:
		return fmt.Sprintf("number:%d", v), nil
	case int16:
		return fmt.Sprintf("number:%d", v), nil
	case int32:
		return fmt.Sprintf("number:%d", v), nil
	case int64:
		return fmt.Sprintf("number:%d", v), nil
	case uint:
		return fmt.Sprintf("number:%d", v), nil
	case uint8:
		return fmt.Sprintf("number:%d", v), nil
	case uint16:
		return fmt.Sprintf("number:%d", v), nil
	case uint32:
		return fmt.Sprintf("number:%d", v), nil
	case uint64:
		return fmt.Sprintf("number:%d", v), nil
	case float32:
		return normalizeFloatKey(float64(v))
	case float64:
		return normalizeFloatKey(v)
	default:
		bytes, err := json.Marshal(value)
		if err != nil {
			return "", err
		}
		return string(bytes), nil
	}
}

func normalizeFloatKey(v float64) (string, error) {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return "", fmt.Errorf("unsupported numeric value %v", v)
	}
	if math.Trunc(v) == v {
		return fmt.Sprintf("number:%d", int64(v)), nil
	}
	return fmt.Sprintf("number:%g", v), nil
}

func isIntegerValue(value interface{}) bool {
	switch v := value.(type) {
	case int, int8, int16, int32, int64:
		return true
	case uint, uint8, uint16, uint32, uint64:
		return true
	case float32:
		return math.Trunc(float64(v)) == float64(v)
	case float64:
		return math.Trunc(v) == v
	default:
		return false
	}
}

func isNumberValue(value interface{}) bool {
	switch value.(type) {
	case int, int8, int16, int32, int64:
		return true
	case uint, uint8, uint16, uint32, uint64:
		return true
	case float32, float64:
		return true
	default:
		return false
	}
}

func Materialize(workDir, taskID, overridePath string) (*MaterializedConfig, error) {
	taskDir := filepath.Join(workDir, ".muxagent", "tasks", taskID)
	promptDir := filepath.Join(taskDir, "prompts")
	if err := os.MkdirAll(promptDir, 0o755); err != nil {
		return nil, err
	}

	var (
		cfg        *Config
		bundleRoot string
		promptFS   fs.FS
	)
	if overridePath == "" {
		var err error
		cfg, err = LoadDefault()
		if err != nil {
			return nil, err
		}
		bundleRoot = path.Dir(defaultConfigAsset)
		promptFS = defaultsFS
	} else {
		var err error
		cfg, err = Load(overridePath)
		if err != nil {
			return nil, err
		}
		bundleRoot = filepath.Dir(overridePath)
		promptFS = os.DirFS(bundleRoot)
	}

	cfgCopy, err := deepCopy(cfg)
	if err != nil {
		return nil, err
	}
	resolvedRuntime, err := ResolveRuntime(cfgCopy)
	if err != nil {
		return nil, err
	}
	cfgCopy.Runtime = resolvedRuntime

	names := make([]string, 0, len(cfgCopy.NodeDefinitions))
	for name := range cfgCopy.NodeDefinitions {
		names = append(names, name)
	}
	sort.Strings(names)
	seenPromptDestinations := map[string]string{}
	for _, name := range names {
		def := cfgCopy.NodeDefinitions[name]
		if def.Type == NodeTypeHuman || def.Type == NodeTypeTerminal {
			continue
		}
		sourcePromptPath, err := normalizeBundlePromptPath(def.SystemPrompt)
		if err != nil {
			return nil, fmt.Errorf("resolve system prompt for %q: %w", name, err)
		}
		materializedPromptPath := materializedPromptRelativePath(sourcePromptPath)
		destinationKey := strings.ToLower(materializedPromptPath)
		if owner, exists := seenPromptDestinations[destinationKey]; exists {
			return nil, fmt.Errorf("system prompts for %q and %q both materialize to %q", owner, name, materializedPromptPath)
		}
		seenPromptDestinations[destinationKey] = name

		promptBytes, err := readPrompt(promptFS, bundleRoot, sourcePromptPath, overridePath == "")
		if err != nil {
			return nil, fmt.Errorf("read system prompt for %q: %w", name, err)
		}
		destPromptPath, err := promptMaterializationPath(promptDir, materializedPromptPath)
		if err != nil {
			return nil, fmt.Errorf("materialize system prompt for %q: %w", name, err)
		}
		if err := os.MkdirAll(filepath.Dir(destPromptPath), 0o755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(destPromptPath, promptBytes, 0o644); err != nil {
			return nil, err
		}
		def.SystemPrompt = "./prompts/" + filepath.ToSlash(materializedPromptPath)
		cfgCopy.NodeDefinitions[name] = def
	}

	configBytes, err := yaml.Marshal(cfgCopy)
	if err != nil {
		return nil, err
	}
	configPath := filepath.Join(taskDir, "config.yaml")
	if err := os.WriteFile(configPath, configBytes, 0o644); err != nil {
		return nil, err
	}

	return &MaterializedConfig{
		ConfigPath: configPath,
		TaskDir:    taskDir,
		PromptDir:  promptDir,
		Config:     cfgCopy,
	}, nil
}

func ResolveRuntime(cfg *Config) (appconfig.RuntimeID, error) {
	if cfg != nil && cfg.Runtime != "" {
		if !appconfig.IsSupportedRuntime(cfg.Runtime) {
			return "", fmt.Errorf("runtime %q is not supported", cfg.Runtime)
		}
		return cfg.Runtime, nil
	}
	return appconfig.PreferredRuntimeFromPATH(), nil
}

func readPrompt(fsys fs.FS, sourceDir, path string, embedded bool) ([]byte, error) {
	if embedded {
		return fs.ReadFile(fsys, filepath.ToSlash(filepath.Join(sourceDir, path)))
	}
	return fs.ReadFile(fsys, filepath.ToSlash(path))
}

func deepCopy(cfg *Config) (*Config, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	if err := enc.Encode(cfg); err != nil {
		return nil, err
	}
	_ = enc.Close()
	return parse(buf.Bytes())
}

func ResolvePromptPath(configPath string, def NodeDefinition) string {
	if filepath.IsAbs(def.SystemPrompt) {
		return def.SystemPrompt
	}
	return filepath.Join(filepath.Dir(configPath), def.SystemPrompt)
}

func ReadPromptText(configPath string, def NodeDefinition) (string, error) {
	data, err := os.ReadFile(ResolvePromptPath(configPath, def))
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func normalizeBundlePromptPath(raw string) (string, error) {
	clean := strings.TrimSpace(raw)
	if clean == "" {
		return "", errors.New("path is required")
	}
	if filepath.IsAbs(clean) {
		return "", errors.New("path must be relative to the config bundle")
	}
	clean = filepath.ToSlash(filepath.Clean(clean))
	clean = strings.TrimPrefix(clean, "./")
	if clean == "." || clean == "" {
		return "", errors.New("path is required")
	}
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return "", errors.New("path must stay within the config bundle")
	}
	return clean, nil
}

func promptMaterializationPath(promptDir, relativePath string) (string, error) {
	destPath := filepath.Join(promptDir, filepath.FromSlash(relativePath))
	rel, err := filepath.Rel(promptDir, destPath)
	if err != nil {
		return "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errors.New("path must stay within the prompt directory")
	}
	return destPath, nil
}

func materializedPromptRelativePath(bundlePromptPath string) string {
	if stripped, ok := strings.CutPrefix(bundlePromptPath, "prompts/"); ok {
		return stripped
	}
	return bundlePromptPath
}

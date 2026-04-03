package appserver

import (
	"strings"
	"time"

	appconfig "github.com/LaLanMo/muxagent-cli/internal/config"
	"github.com/LaLanMo/muxagent-cli/internal/taskconfig"
	"github.com/LaLanMo/muxagent-cli/internal/taskdomain"
	"github.com/LaLanMo/muxagent-cli/internal/taskexecutor"
	"github.com/LaLanMo/muxagent-cli/internal/taskruntime"
)

type taskDto struct {
	ID           string    `json:"id"`
	Description  string    `json:"description"`
	ConfigAlias  string    `json:"config_alias"`
	ConfigPath   string    `json:"config_path"`
	WorkDir      string    `json:"work_dir"`
	ExecutionDir string    `json:"execution_dir"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type triggeredByDto struct {
	NodeRunID string `json:"node_run_id"`
	Reason    string `json:"reason"`
}

type nodeRunViewDto struct {
	ID             string                             `json:"id"`
	TaskID         string                             `json:"task_id"`
	NodeName       string                             `json:"node_name"`
	Status         string                             `json:"status"`
	SessionID      string                             `json:"session_id,omitempty"`
	FailureReason  string                             `json:"failure_reason,omitempty"`
	Result         map[string]interface{}             `json:"result,omitempty"`
	Clarifications []taskdomain.ClarificationExchange `json:"clarifications,omitempty"`
	TriggeredBy    *triggeredByDto                    `json:"triggered_by,omitempty"`
	StartedAt      time.Time                          `json:"started_at"`
	CompletedAt    *time.Time                         `json:"completed_at,omitempty"`
	ArtifactPaths  []string                           `json:"artifact_paths,omitempty"`
}

type blockedStepDto struct {
	NodeName    string          `json:"node_name"`
	Iteration   int             `json:"iteration"`
	Reason      string          `json:"reason"`
	TriggeredBy *triggeredByDto `json:"triggered_by,omitempty"`
	CreatedAt   time.Time       `json:"created_at"`
}

type taskIssueDto struct {
	Kind       string    `json:"kind"`
	NodeName   string    `json:"node_name"`
	Iteration  int       `json:"iteration"`
	Reason     string    `json:"reason"`
	OccurredAt time.Time `json:"occurred_at"`
}

type taskViewDto struct {
	Task            taskDto          `json:"task"`
	Status          string           `json:"status"`
	CurrentNodeName string           `json:"current_node_name"`
	CurrentNodeType string           `json:"current_node_type"`
	CurrentIssue    *taskIssueDto    `json:"current_issue,omitempty"`
	ArtifactPaths   []string         `json:"artifact_paths,omitempty"`
	NodeRuns        []nodeRunViewDto `json:"node_runs,omitempty"`
	BlockedSteps    []blockedStepDto `json:"blocked_steps,omitempty"`
}

type configViewDto struct {
	Path   string             `json:"path"`
	Config *taskconfig.Config `json:"config,omitempty"`
}

type inputRequestDto struct {
	Kind          string                             `json:"kind"`
	TaskID        string                             `json:"task_id"`
	NodeRunID     string                             `json:"node_run_id"`
	NodeName      string                             `json:"node_name"`
	Schema        *taskconfig.JSONSchema             `json:"schema,omitempty"`
	Questions     []taskdomain.ClarificationQuestion `json:"questions,omitempty"`
	ArtifactPaths []string                           `json:"artifact_paths,omitempty"`
}

type messagePartDto struct {
	MessageID string `json:"message_id"`
	PartID    string `json:"part_id"`
	Role      string `json:"role"`
	Type      string `json:"type"`
	Text      string `json:"text"`
}

type toolDiffDto struct {
	Path    string  `json:"path"`
	OldText *string `json:"old_text,omitempty"`
	NewText string  `json:"new_text"`
}

type toolCallDto struct {
	CallID        string        `json:"call_id"`
	ParentCallID  string        `json:"parent_call_id,omitempty"`
	Name          string        `json:"name"`
	Kind          string        `json:"kind"`
	Title         string        `json:"title,omitempty"`
	Status        string        `json:"status"`
	InputSummary  string        `json:"input_summary,omitempty"`
	OutputText    string        `json:"output_text,omitempty"`
	ErrorText     string        `json:"error_text,omitempty"`
	Paths         []string      `json:"paths,omitempty"`
	Diffs         []toolDiffDto `json:"diffs,omitempty"`
	RawInputJSON  string        `json:"raw_input_json,omitempty"`
	RawOutputJSON string        `json:"raw_output_json,omitempty"`
}

type planStepDto struct {
	Text   string `json:"text"`
	Status string `json:"status"`
}

type planSnapshotDto struct {
	PlanID string        `json:"plan_id"`
	Steps  []planStepDto `json:"steps,omitempty"`
}

type usageSnapshotDto struct {
	InputTokens       int64 `json:"input_tokens"`
	CachedInputTokens int64 `json:"cached_input_tokens"`
	OutputTokens      int64 `json:"output_tokens"`
	TotalTokens       int64 `json:"total_tokens"`
	DurationMS        int64 `json:"duration_ms"`
}

type streamEventDto struct {
	Kind      string            `json:"kind"`
	SessionID string            `json:"session_id,omitempty"`
	Raw       string            `json:"raw,omitempty"`
	Message   *messagePartDto   `json:"message,omitempty"`
	Tool      *toolCallDto      `json:"tool,omitempty"`
	Plan      *planSnapshotDto  `json:"plan,omitempty"`
	Usage     *usageSnapshotDto `json:"usage,omitempty"`
}

type progressInfoDto struct {
	Message   string           `json:"message,omitempty"`
	SessionID string           `json:"session_id,omitempty"`
	Events    []streamEventDto `json:"events,omitempty"`
}

type runErrorDto struct {
	Message string `json:"message"`
}

type runEventDto struct {
	Type         string             `json:"type"`
	TaskID       string             `json:"task_id,omitempty"`
	NodeRunID    string             `json:"node_run_id,omitempty"`
	NodeName     string             `json:"node_name,omitempty"`
	TaskView     *taskViewDto       `json:"task_view,omitempty"`
	Config       *taskconfig.Config `json:"config,omitempty"`
	Progress     *progressInfoDto   `json:"progress,omitempty"`
	InputRequest *inputRequestDto   `json:"input_request,omitempty"`
	Error        *runErrorDto       `json:"error,omitempty"`
}

type configCatalogEntryDto struct {
	Alias       string              `json:"alias"`
	BundlePath  string              `json:"bundle_path,omitempty"`
	ConfigPath  string              `json:"config_path"`
	IsDefault   bool                `json:"is_default"`
	RuntimeID   appconfig.RuntimeID `json:"runtime_id,omitempty"`
	RuntimeName string              `json:"runtime_name,omitempty"`
	NodeNames   []string            `json:"node_names,omitempty"`
	LoadError   string              `json:"load_error,omitempty"`
	BuiltinID   string              `json:"builtin_id,omitempty"`
	Builtin     bool                `json:"builtin"`
	Description string              `json:"description,omitempty"`
	Launchable  bool                `json:"launchable"`
}

type artifactRefDto struct {
	TaskID       string `json:"task_id"`
	NodeRunID    string `json:"node_run_id,omitempty"`
	NodeName     string `json:"node_name,omitempty"`
	SourceLabel  string `json:"source_label,omitempty"`
	RawPath      string `json:"raw_path"`
	ResolvedPath string `json:"resolved_path"`
	DisplayPath  string `json:"display_path"`
	PreviewName  string `json:"preview_name"`
	PreviewTitle string `json:"preview_title"`
	Markdown     bool   `json:"markdown"`
}

func taskViewToDTO(view taskdomain.TaskView) taskViewDto {
	nodeRuns := make([]nodeRunViewDto, 0, len(view.NodeRuns))
	for _, run := range view.NodeRuns {
		nodeRuns = append(nodeRuns, nodeRunViewToDTO(run))
	}
	blockedSteps := make([]blockedStepDto, 0, len(view.BlockedSteps))
	for _, step := range view.BlockedSteps {
		blockedSteps = append(blockedSteps, blockedStepToDTO(step))
	}
	return taskViewDto{
		Task:            taskToDTO(view.Task),
		Status:          string(view.Status),
		CurrentNodeName: view.CurrentNodeName,
		CurrentNodeType: string(view.CurrentNodeType),
		CurrentIssue:    taskIssueToDTO(view.CurrentIssue),
		ArtifactPaths:   append([]string(nil), view.ArtifactPaths...),
		NodeRuns:        nodeRuns,
		BlockedSteps:    blockedSteps,
	}
}

func taskToDTO(task taskdomain.Task) taskDto {
	return taskDto{
		ID:           task.ID,
		Description:  task.Description,
		ConfigAlias:  task.ConfigAlias,
		ConfigPath:   task.ConfigPath,
		WorkDir:      task.WorkDir,
		ExecutionDir: task.ExecutionDir,
		CreatedAt:    task.CreatedAt,
		UpdatedAt:    task.UpdatedAt,
	}
}

func nodeRunViewToDTO(run taskdomain.NodeRunView) nodeRunViewDto {
	return nodeRunViewDto{
		ID:             run.ID,
		TaskID:         run.TaskID,
		NodeName:       run.NodeName,
		Status:         string(run.Status),
		SessionID:      run.SessionID,
		FailureReason:  run.FailureReason,
		Result:         cloneMap(run.Result),
		Clarifications: append([]taskdomain.ClarificationExchange(nil), run.Clarifications...),
		TriggeredBy:    triggeredByToDTO(run.TriggeredBy),
		StartedAt:      run.StartedAt,
		CompletedAt:    run.CompletedAt,
		ArtifactPaths:  append([]string(nil), run.ArtifactPaths...),
	}
}

func blockedStepToDTO(step taskdomain.BlockedStep) blockedStepDto {
	return blockedStepDto{
		NodeName:    step.NodeName,
		Iteration:   step.Iteration,
		Reason:      step.Reason,
		TriggeredBy: triggeredByToDTO(step.TriggeredBy),
		CreatedAt:   step.CreatedAt,
	}
}

func taskIssueToDTO(issue *taskdomain.TaskIssue) *taskIssueDto {
	if issue == nil {
		return nil
	}
	return &taskIssueDto{
		Kind:       string(issue.Kind),
		NodeName:   issue.NodeName,
		Iteration:  issue.Iteration,
		Reason:     issue.Reason,
		OccurredAt: issue.OccurredAt,
	}
}

func triggeredByToDTO(triggeredBy *taskdomain.TriggeredBy) *triggeredByDto {
	if triggeredBy == nil {
		return nil
	}
	return &triggeredByDto{
		NodeRunID: triggeredBy.NodeRunID,
		Reason:    triggeredBy.Reason,
	}
}

func inputRequestToDTO(input *taskruntime.InputRequest) *inputRequestDto {
	if input == nil {
		return nil
	}
	dto := &inputRequestDto{
		Kind:          string(input.Kind),
		TaskID:        input.TaskID,
		NodeRunID:     input.NodeRunID,
		NodeName:      input.NodeName,
		Questions:     append([]taskdomain.ClarificationQuestion(nil), input.Questions...),
		ArtifactPaths: append([]string(nil), input.ArtifactPaths...),
	}
	if input.Schema != nil {
		schema := *input.Schema
		dto.Schema = &schema
	}
	return dto
}

func runEventToDTO(event taskruntime.RunEvent) runEventDto {
	dto := runEventDto{
		Type:      string(event.Type),
		TaskID:    event.TaskID,
		NodeRunID: event.NodeRunID,
		NodeName:  event.NodeName,
		Config:    event.Config,
	}
	if event.TaskView != nil {
		view := taskViewToDTO(*event.TaskView)
		dto.TaskView = &view
	}
	if event.Progress != nil {
		progress := progressInfoToDTO(*event.Progress)
		dto.Progress = &progress
	}
	if event.InputRequest != nil {
		dto.InputRequest = inputRequestToDTO(event.InputRequest)
	}
	if event.Error != nil {
		dto.Error = &runErrorDto{Message: event.Error.Message}
	}
	return dto
}

func progressInfoToDTO(progress taskruntime.ProgressInfo) progressInfoDto {
	events := make([]streamEventDto, 0, len(progress.Events))
	for _, event := range progress.Events {
		events = append(events, streamEventToDTO(event))
	}
	return progressInfoDto{
		Message:   progress.Message,
		SessionID: progress.SessionID,
		Events:    events,
	}
}

func streamEventToDTO(event taskexecutor.StreamEvent) streamEventDto {
	dto := streamEventDto{
		Kind:      string(event.Kind),
		SessionID: event.SessionID,
		Raw:       event.Raw,
	}
	if event.Message != nil {
		dto.Message = &messagePartDto{
			MessageID: event.Message.MessageID,
			PartID:    event.Message.PartID,
			Role:      string(event.Message.Role),
			Type:      string(event.Message.Type),
			Text:      event.Message.Text,
		}
	}
	if event.Tool != nil {
		diffs := make([]toolDiffDto, 0, len(event.Tool.Diffs))
		for _, diff := range event.Tool.Diffs {
			diffs = append(diffs, toolDiffDto{
				Path:    diff.Path,
				OldText: diff.OldText,
				NewText: diff.NewText,
			})
		}
		dto.Tool = &toolCallDto{
			CallID:        event.Tool.CallID,
			ParentCallID:  event.Tool.ParentCallID,
			Name:          event.Tool.Name,
			Kind:          string(event.Tool.Kind),
			Title:         event.Tool.Title,
			Status:        string(event.Tool.Status),
			InputSummary:  event.Tool.InputSummary,
			OutputText:    event.Tool.OutputText,
			ErrorText:     event.Tool.ErrorText,
			Paths:         append([]string(nil), event.Tool.Paths...),
			Diffs:         diffs,
			RawInputJSON:  event.Tool.RawInputJSON,
			RawOutputJSON: event.Tool.RawOutputJSON,
		}
	}
	if event.Plan != nil {
		steps := make([]planStepDto, 0, len(event.Plan.Steps))
		for _, step := range event.Plan.Steps {
			steps = append(steps, planStepDto{Text: step.Text, Status: step.Status})
		}
		dto.Plan = &planSnapshotDto{
			PlanID: event.Plan.PlanID,
			Steps:  steps,
		}
	}
	if event.Usage != nil {
		dto.Usage = &usageSnapshotDto{
			InputTokens:       event.Usage.InputTokens,
			CachedInputTokens: event.Usage.CachedInputTokens,
			OutputTokens:      event.Usage.OutputTokens,
			TotalTokens:       event.Usage.TotalTokens,
			DurationMS:        event.Usage.DurationMS,
		}
	}
	return dto
}

func buildConfigCatalogResult(catalog *taskconfig.Catalog, reg taskconfig.Registry) configCatalogResult {
	if catalog == nil {
		return configCatalogResult{}
	}
	entries := make([]configCatalogEntryDto, 0, len(catalog.Entries))
	for _, entry := range catalog.Entries {
		dto := configCatalogEntryDto{
			Alias:      entry.Alias,
			ConfigPath: entry.Path,
			IsDefault:  entry.Alias == catalog.DefaultAlias,
			BuiltinID:  entry.BuiltinID,
			Builtin:    entry.Builtin,
		}
		for _, regEntry := range reg.Configs {
			if regEntry.Alias == entry.Alias {
				dto.BundlePath = regEntry.Path
				break
			}
		}
		cfg, err := entry.LoadConfig()
		if err != nil {
			dto.LoadError = err.Error()
			dto.Launchable = false
		} else {
			dto.RuntimeID = cfg.Runtime
			dto.RuntimeName = runtimeDisplayName(cfg.Runtime)
			dto.Description = cfg.Description
			for _, node := range cfg.Topology.Nodes {
				dto.NodeNames = append(dto.NodeNames, node.Name)
			}
			dto.Launchable = true
		}
		entries = append(entries, dto)
	}
	return configCatalogResult{
		DefaultAlias: catalog.DefaultAlias,
		Entries:      entries,
	}
}

func runtimeDisplayName(id appconfig.RuntimeID) string {
	switch id {
	case appconfig.RuntimeClaudeCode:
		return "Claude Code"
	case appconfig.RuntimeCodex:
		return "Codex"
	default:
		if strings.TrimSpace(string(id)) == "" {
			return ""
		}
		return string(id)
	}
}

func cloneMap(input map[string]interface{}) map[string]interface{} {
	if len(input) == 0 {
		return nil
	}
	cloned := make(map[string]interface{}, len(input))
	for key, value := range input {
		cloned[key] = value
	}
	return cloned
}

package appserver

import (
	"strings"
	"time"

	appconfig "github.com/LaLanMo/muxagent-cli/internal/config"
	"github.com/LaLanMo/muxagent-cli/internal/taskconfig"
	"github.com/LaLanMo/muxagent-cli/internal/taskdomain"
	"github.com/LaLanMo/muxagent-cli/internal/taskexecutor"
	"github.com/LaLanMo/muxagent-cli/internal/taskhistory"
	"github.com/LaLanMo/muxagent-cli/internal/taskruntime"
)

type taskDTO struct {
	ID                    string    `json:"id"`
	Description           string    `json:"description"`
	ConfigAlias           string    `json:"config_alias"`
	ConfigPath            string    `json:"config_path"`
	WorkDir               string    `json:"work_dir"`
	ExecutionDir          string    `json:"execution_dir"`
	CreatedAt             time.Time `json:"created_at"`
	UpdatedAt             time.Time `json:"updated_at"`
	ParentTaskID          string    `json:"parent_task_id,omitempty"`
	ParentTaskDescription string    `json:"parent_task_description,omitempty"`
}

type triggeredByDTO struct {
	NodeRunID string `json:"node_run_id"`
	Reason    string `json:"reason"`
}

type nodeRunViewDTO struct {
	ID             string                             `json:"id"`
	TaskID         string                             `json:"task_id"`
	NodeName       string                             `json:"node_name"`
	Status         string                             `json:"status"`
	SessionID      string                             `json:"session_id,omitempty"`
	FailureReason  string                             `json:"failure_reason,omitempty"`
	Result         map[string]interface{}             `json:"result,omitempty"`
	Clarifications []taskdomain.ClarificationExchange `json:"clarifications,omitempty"`
	TriggeredBy    *triggeredByDTO                    `json:"triggered_by,omitempty"`
	StartedAt      time.Time                          `json:"started_at"`
	CompletedAt    *time.Time                         `json:"completed_at,omitempty"`
	ArtifactPaths  []string                           `json:"artifact_paths,omitempty"`
}

type blockedStepDTO struct {
	NodeName    string          `json:"node_name"`
	Iteration   int             `json:"iteration"`
	Reason      string          `json:"reason"`
	TriggeredBy *triggeredByDTO `json:"triggered_by,omitempty"`
	CreatedAt   time.Time       `json:"created_at"`
}

type taskIssueDTO struct {
	Kind       string    `json:"kind"`
	NodeName   string    `json:"node_name"`
	Iteration  int       `json:"iteration"`
	Reason     string    `json:"reason"`
	OccurredAt time.Time `json:"occurred_at"`
}

type taskViewDTO struct {
	Task            taskDTO          `json:"task"`
	Status          string           `json:"status"`
	CurrentNodeName string           `json:"current_node_name"`
	CurrentNodeType string           `json:"current_node_type"`
	CurrentIssue    *taskIssueDTO    `json:"current_issue,omitempty"`
	ArtifactPaths   []string         `json:"artifact_paths,omitempty"`
	NodeRuns        []nodeRunViewDTO `json:"node_runs,omitempty"`
	BlockedSteps    []blockedStepDTO `json:"blocked_steps,omitempty"`
}

type configViewDTO struct {
	Path   string             `json:"path"`
	Config *taskconfig.Config `json:"config,omitempty"`
}

type inputRequestDTO struct {
	Kind          string                             `json:"kind"`
	TaskID        string                             `json:"task_id"`
	NodeRunID     string                             `json:"node_run_id"`
	NodeName      string                             `json:"node_name"`
	Schema        *taskconfig.JSONSchema             `json:"schema,omitempty"`
	Questions     []taskdomain.ClarificationQuestion `json:"questions,omitempty"`
	ArtifactPaths []string                           `json:"artifact_paths,omitempty"`
}

type sessionHistoryToolDiffDTO struct {
	Path    string  `json:"path"`
	OldText *string `json:"old_text,omitempty"`
	NewText string  `json:"new_text"`
}

type sessionHistoryPlanStepDTO struct {
	Text   string `json:"text"`
	Status string `json:"status"`
}

type sessionHistoryEventDTO struct {
	EventID          string     `json:"event_id,omitempty"`
	Seq              uint64     `json:"seq,omitempty"`
	EmittedAt        *time.Time `json:"emitted_at,omitempty"`
	RecordedAt       *time.Time `json:"recorded_at,omitempty"`
	SessionID        string     `json:"session_id,omitempty"`
	ProviderRecordID string     `json:"provider_record_id,omitempty"`
	ProviderSubindex int        `json:"provider_subindex,omitempty"`
	Provenance       string     `json:"provenance,omitempty"`
	Kind             string     `json:"kind"`
	Raw              string     `json:"raw,omitempty"`

	MessageID string `json:"message_id,omitempty"`
	PartID    string `json:"part_id,omitempty"`
	Role      string `json:"role,omitempty"`
	PartType  string `json:"part_type,omitempty"`
	Text      string `json:"text,omitempty"`

	CallID        string                      `json:"call_id,omitempty"`
	ParentCallID  string                      `json:"parent_call_id,omitempty"`
	Name          string                      `json:"name,omitempty"`
	ToolKind      string                      `json:"tool_kind,omitempty"`
	Title         string                      `json:"title,omitempty"`
	Status        string                      `json:"status,omitempty"`
	InputSummary  string                      `json:"input_summary,omitempty"`
	OutputText    string                      `json:"output_text,omitempty"`
	ErrorText     string                      `json:"error_text,omitempty"`
	Paths         []string                    `json:"paths,omitempty"`
	Diffs         []sessionHistoryToolDiffDTO `json:"diffs,omitempty"`
	RawInputJSON  string                      `json:"raw_input_json,omitempty"`
	RawOutputJSON string                      `json:"raw_output_json,omitempty"`

	PlanID string                      `json:"plan_id,omitempty"`
	Steps  []sessionHistoryPlanStepDTO `json:"steps,omitempty"`

	InputTokens       int64 `json:"input_tokens,omitempty"`
	CachedInputTokens int64 `json:"cached_input_tokens,omitempty"`
	OutputTokens      int64 `json:"output_tokens,omitempty"`
	TotalTokens       int64 `json:"total_tokens,omitempty"`
	DurationMS        int64 `json:"duration_ms,omitempty"`
}

type progressInfoDTO struct {
	Message   string                   `json:"message,omitempty"`
	SessionID string                   `json:"session_id,omitempty"`
	Events    []sessionHistoryEventDTO `json:"events,omitempty"`
}

type runErrorDTO struct {
	Message string `json:"message"`
}

type runEventDTO struct {
	Type         string             `json:"type"`
	TaskID       string             `json:"task_id,omitempty"`
	NodeRunID    string             `json:"node_run_id,omitempty"`
	NodeName     string             `json:"node_name,omitempty"`
	TaskView     *taskViewDTO       `json:"task_view,omitempty"`
	Config       *taskconfig.Config `json:"config,omitempty"`
	Progress     *progressInfoDTO   `json:"progress,omitempty"`
	InputRequest *inputRequestDTO   `json:"input_request,omitempty"`
	Error        *runErrorDTO       `json:"error,omitempty"`
}

type configCatalogEntryDTO struct {
	Alias             string              `json:"alias"`
	BundlePath        string              `json:"bundle_path,omitempty"`
	ConfigPath        string              `json:"config_path"`
	IsDefault         bool                `json:"is_default"`
	RuntimeID         appconfig.RuntimeID `json:"runtime_id,omitempty"`
	RuntimeName       string              `json:"runtime_name,omitempty"`
	RuntimeExplicit   bool                `json:"runtime_explicit"`
	RuntimeConfigured bool                `json:"runtime_configured"`
	NodeNames         []string            `json:"node_names,omitempty"`
	LoadError         string              `json:"load_error,omitempty"`
	BuiltinID         string              `json:"builtin_id,omitempty"`
	Builtin           bool                `json:"builtin"`
	Description       string              `json:"description,omitempty"`
	Launchable        bool                `json:"launchable"`
}

type configDetailDTO struct {
	Alias             string              `json:"alias"`
	BundlePath        string              `json:"bundle_path,omitempty"`
	ConfigPath        string              `json:"config_path"`
	IsDefault         bool                `json:"is_default"`
	BuiltinID         string              `json:"builtin_id,omitempty"`
	Builtin           bool                `json:"builtin"`
	Revision          string              `json:"revision,omitempty"`
	RuntimeID         appconfig.RuntimeID `json:"runtime_id,omitempty"`
	RuntimeName       string              `json:"runtime_name,omitempty"`
	RuntimeExplicit   bool                `json:"runtime_explicit"`
	RuntimeConfigured bool                `json:"runtime_configured"`
	Description       string              `json:"description,omitempty"`
	NodeNames         []string            `json:"node_names,omitempty"`
	LoadError         string              `json:"load_error,omitempty"`
	Launchable        bool                `json:"launchable"`
	Config            *taskconfig.Config  `json:"config,omitempty"`
}

type runtimeEntryDTO struct {
	RuntimeID   appconfig.RuntimeID `json:"runtime_id"`
	RuntimeName string              `json:"runtime_name"`
	Command     string              `json:"command,omitempty"`
	Args        []string            `json:"args,omitempty"`
	CWD         string              `json:"cwd,omitempty"`
	EnvKeys     []string            `json:"env_keys,omitempty"`
}

type configPromptDTO struct {
	Alias        string `json:"alias"`
	NodeName     string `json:"node_name"`
	NodeType     string `json:"node_type"`
	Path         string `json:"path"`
	ResolvedPath string `json:"resolved_path"`
	Content      string `json:"content"`
	Revision     string `json:"revision,omitempty"`
	ReadOnly     bool   `json:"readonly"`
	Builtin      bool   `json:"builtin"`
}

type artifactRefDTO struct {
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

func taskViewToDTO(view taskdomain.TaskView) taskViewDTO {
	nodeRuns := make([]nodeRunViewDTO, 0, len(view.NodeRuns))
	for _, run := range view.NodeRuns {
		nodeRuns = append(nodeRuns, nodeRunViewToDTO(run))
	}
	blockedSteps := make([]blockedStepDTO, 0, len(view.BlockedSteps))
	for _, step := range view.BlockedSteps {
		blockedSteps = append(blockedSteps, blockedStepToDTO(step))
	}
	return taskViewDTO{
		Task:            taskToDTO(view.Task, view.ParentTaskID, view.ParentTaskDescription),
		Status:          string(view.Status),
		CurrentNodeName: view.CurrentNodeName,
		CurrentNodeType: string(view.CurrentNodeType),
		CurrentIssue:    taskIssueToDTO(view.CurrentIssue),
		ArtifactPaths:   append([]string(nil), view.ArtifactPaths...),
		NodeRuns:        nodeRuns,
		BlockedSteps:    blockedSteps,
	}
}

func taskToDTO(task taskdomain.Task, parentTaskID, parentTaskDescription string) taskDTO {
	return taskDTO{
		ID:                    task.ID,
		Description:           task.Description,
		ConfigAlias:           task.ConfigAlias,
		ConfigPath:            task.ConfigPath,
		WorkDir:               task.WorkDir,
		ExecutionDir:          task.ExecutionDir,
		CreatedAt:             task.CreatedAt,
		UpdatedAt:             task.UpdatedAt,
		ParentTaskID:          parentTaskID,
		ParentTaskDescription: parentTaskDescription,
	}
}

func nodeRunViewToDTO(run taskdomain.NodeRunView) nodeRunViewDTO {
	return nodeRunViewDTO{
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

func blockedStepToDTO(step taskdomain.BlockedStep) blockedStepDTO {
	return blockedStepDTO{
		NodeName:    step.NodeName,
		Iteration:   step.Iteration,
		Reason:      step.Reason,
		TriggeredBy: triggeredByToDTO(step.TriggeredBy),
		CreatedAt:   step.CreatedAt,
	}
}

func taskIssueToDTO(issue *taskdomain.TaskIssue) *taskIssueDTO {
	if issue == nil {
		return nil
	}
	return &taskIssueDTO{
		Kind:       string(issue.Kind),
		NodeName:   issue.NodeName,
		Iteration:  issue.Iteration,
		Reason:     issue.Reason,
		OccurredAt: issue.OccurredAt,
	}
}

func triggeredByToDTO(triggeredBy *taskdomain.TriggeredBy) *triggeredByDTO {
	if triggeredBy == nil {
		return nil
	}
	return &triggeredByDTO{
		NodeRunID: triggeredBy.NodeRunID,
		Reason:    triggeredBy.Reason,
	}
}

func inputRequestToDTO(input *taskruntime.InputRequest) *inputRequestDTO {
	if input == nil {
		return nil
	}
	dto := &inputRequestDTO{
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

func runEventToDTO(event taskruntime.RunEvent) runEventDTO {
	dto := runEventDTO{
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
		dto.Error = &runErrorDTO{Message: event.Error.Message}
	}
	return dto
}

func progressInfoToDTO(progress taskruntime.ProgressInfo) progressInfoDTO {
	events := make([]sessionHistoryEventDTO, 0, len(progress.Events))
	for _, event := range progress.Events {
		events = append(events, streamEventToDTO(event))
	}
	return progressInfoDTO{
		Message:   progress.Message,
		SessionID: progress.SessionID,
		Events:    events,
	}
}

func streamEventToDTO(event taskexecutor.StreamEvent) sessionHistoryEventDTO {
	dto := sessionHistoryEventDTO{
		EventID:          event.EventID,
		Seq:              event.Seq,
		SessionID:        event.SessionID,
		ProviderRecordID: event.ProviderRecordID,
		ProviderSubindex: event.ProviderSubindex,
		Provenance:       string(event.Provenance),
		Kind:             string(event.Kind),
		Raw:              event.Raw,
	}
	if !event.EmittedAt.IsZero() {
		emittedAt := event.EmittedAt.UTC()
		dto.EmittedAt = &emittedAt
	}
	if event.Message != nil {
		dto.MessageID = event.Message.MessageID
		dto.PartID = event.Message.PartID
		dto.Role = string(event.Message.Role)
		dto.PartType = string(event.Message.Type)
		dto.Text = event.Message.Text
	}
	if event.Tool != nil {
		diffs := make([]sessionHistoryToolDiffDTO, 0, len(event.Tool.Diffs))
		for _, diff := range event.Tool.Diffs {
			diffs = append(diffs, sessionHistoryToolDiffDTO{
				Path:    diff.Path,
				OldText: diff.OldText,
				NewText: diff.NewText,
			})
		}
		dto.CallID = event.Tool.CallID
		dto.ParentCallID = event.Tool.ParentCallID
		dto.Name = event.Tool.Name
		dto.ToolKind = string(event.Tool.Kind)
		dto.Title = event.Tool.Title
		dto.Status = string(event.Tool.Status)
		dto.InputSummary = event.Tool.InputSummary
		dto.OutputText = event.Tool.OutputText
		dto.ErrorText = event.Tool.ErrorText
		dto.Paths = append([]string(nil), event.Tool.Paths...)
		dto.Diffs = diffs
		dto.RawInputJSON = event.Tool.RawInputJSON
		dto.RawOutputJSON = event.Tool.RawOutputJSON
	}
	if event.Plan != nil {
		steps := make([]sessionHistoryPlanStepDTO, 0, len(event.Plan.Steps))
		for _, step := range event.Plan.Steps {
			steps = append(steps, sessionHistoryPlanStepDTO{Text: step.Text, Status: step.Status})
		}
		dto.PlanID = event.Plan.PlanID
		dto.Steps = steps
	}
	if event.Usage != nil {
		dto.InputTokens = event.Usage.InputTokens
		dto.CachedInputTokens = event.Usage.CachedInputTokens
		dto.OutputTokens = event.Usage.OutputTokens
		dto.TotalTokens = event.Usage.TotalTokens
		dto.DurationMS = event.Usage.DurationMS
	}
	return dto
}

func historyStreamEventToDTO(event taskhistory.EventRecord) sessionHistoryEventDTO {
	dto := sessionHistoryEventDTO{
		EventID:          event.EventID,
		Seq:              event.Seq,
		SessionID:        event.SessionID,
		ProviderRecordID: event.ProviderRecordID,
		ProviderSubindex: event.ProviderSubindex,
		Provenance:       event.Provenance,
		Kind:             event.Kind,
		Raw:              event.Raw,
	}
	if !event.EmittedAt.IsZero() {
		emittedAt := event.EmittedAt.UTC()
		dto.EmittedAt = &emittedAt
	}
	if !event.RecordedAt.IsZero() {
		recordedAt := event.RecordedAt.UTC()
		dto.RecordedAt = &recordedAt
	}
	if event.Message != nil {
		dto.MessageID = event.Message.MessageID
		dto.PartID = event.Message.PartID
		dto.Role = event.Message.Role
		dto.PartType = event.Message.Type
		dto.Text = event.Message.Text
	}
	if event.Tool != nil {
		diffs := make([]sessionHistoryToolDiffDTO, 0, len(event.Tool.Diffs))
		for _, diff := range event.Tool.Diffs {
			diffs = append(diffs, sessionHistoryToolDiffDTO{
				Path:    diff.Path,
				OldText: diff.OldText,
				NewText: diff.NewText,
			})
		}
		dto.CallID = event.Tool.CallID
		dto.ParentCallID = event.Tool.ParentCallID
		dto.Name = event.Tool.Name
		dto.ToolKind = event.Tool.Kind
		dto.Title = event.Tool.Title
		dto.Status = event.Tool.Status
		dto.InputSummary = event.Tool.InputSummary
		dto.OutputText = event.Tool.OutputText
		dto.ErrorText = event.Tool.ErrorText
		dto.Paths = append([]string(nil), event.Tool.Paths...)
		dto.Diffs = diffs
		dto.RawInputJSON = event.Tool.RawInputJSON
		dto.RawOutputJSON = event.Tool.RawOutputJSON
	}
	if event.Plan != nil {
		steps := make([]sessionHistoryPlanStepDTO, 0, len(event.Plan.Steps))
		for _, step := range event.Plan.Steps {
			steps = append(steps, sessionHistoryPlanStepDTO{
				Text:   step.Text,
				Status: step.Status,
			})
		}
		dto.PlanID = event.Plan.PlanID
		dto.Steps = steps
	}
	if event.Usage != nil {
		dto.InputTokens = event.Usage.InputTokens
		dto.CachedInputTokens = event.Usage.CachedInputTokens
		dto.OutputTokens = event.Usage.OutputTokens
		dto.TotalTokens = event.Usage.TotalTokens
		dto.DurationMS = event.Usage.DurationMS
	}
	return dto
}

func buildConfigCatalogResult(catalog *taskconfig.Catalog, reg taskconfig.Registry, runtimeCfg appconfig.Config, defaultUseWorktree bool) configCatalogResult {
	if catalog == nil {
		return configCatalogResult{DefaultUseWorktree: defaultUseWorktree}
	}
	entries := make([]configCatalogEntryDTO, 0, len(catalog.Entries))
	for _, entry := range catalog.Entries {
		dto := configCatalogEntryDTO{
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
		if dto.BundlePath == "" {
			if bundlePath, err := taskconfig.BundlePathForConfigPath(entry.Path); err == nil {
				dto.BundlePath = bundlePath
			}
		}
		if explicit, err := configRuntimeExplicit(entry.Path); err == nil {
			dto.RuntimeExplicit = explicit
		}
		cfg, err := entry.LoadConfig()
		if err != nil {
			dto.LoadError = err.Error()
			dto.Launchable = false
		} else {
			dto.RuntimeID = cfg.Runtime
			dto.RuntimeName = runtimeDisplayName(cfg.Runtime)
			dto.RuntimeConfigured = runtimeConfigured(runtimeCfg, cfg.Runtime)
			dto.Description = cfg.Description
			for _, node := range cfg.Topology.Nodes {
				dto.NodeNames = append(dto.NodeNames, node.Name)
			}
			dto.Launchable = dto.RuntimeConfigured
		}
		entries = append(entries, dto)
	}
	return configCatalogResult{
		DefaultAlias:       catalog.DefaultAlias,
		DefaultUseWorktree: defaultUseWorktree,
		Entries:            entries,
	}
}

func runtimeDisplayName(id appconfig.RuntimeID) string {
	switch id {
	case appconfig.RuntimeClaudeCode:
		return "Claude Code"
	case appconfig.RuntimeCodex:
		return "Codex"
	case appconfig.RuntimeOpenCode:
		return "OpenCode"
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

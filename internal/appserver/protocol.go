package appserver

import (
	"encoding/json"
	"time"

	appconfig "github.com/LaLanMo/muxagent-cli/internal/config"
	"github.com/LaLanMo/muxagent-cli/internal/taskconfig"
)

const (
	jsonRPCVersion  = "2.0"
	protocolVersion = 1
)

const (
	methodInitialize          = "initialize"
	methodNotification        = "notification"
	methodServiceStatus       = "service.status"
	methodServiceShutdown     = "service.shutdown"
	methodWorkspaceList       = "workspace.list"
	methodWorkspaceAdd        = "workspace.add"
	methodWorkspaceRemove     = "workspace.remove"
	methodWorkspaceUpdate     = "workspace.update"
	methodWorkspaceGet        = "workspace.get"
	methodTaskList            = "task.list"
	methodTaskGet             = "task.get"
	methodTaskInputRequest    = "task.input_request"
	methodTaskStart           = "task.start"
	methodTaskStartFollowUp   = "task.start_follow_up"
	methodTaskSubmitInput     = "task.submit_input"
	methodTaskRetryNode       = "task.retry_node"
	methodTaskContinueBlocked = "task.continue_blocked"
	methodArtifactList        = "artifact.list"
	methodConfigCatalog       = "config.catalog"
	methodConfigGet           = "config.get"
	methodConfigClone         = "config.clone"
	methodConfigRename        = "config.rename"
	methodConfigDelete        = "config.delete"
	methodConfigReset         = "config.reset"
	methodConfigSetDefault    = "config.set_default"
	methodConfigValidate      = "config.validate"
	methodConfigSave          = "config.save"
	methodConfigPromptGet     = "config.prompt.get"
	methodConfigPromptSave    = "config.prompt.save"
	methodRuntimeList         = "runtime.list"
)

const (
	notificationWorkspaceAdded   = "workspace.added"
	notificationWorkspaceUpdated = "workspace.updated"
	notificationWorkspaceRemoved = "workspace.removed"
)

type errorCode int

const (
	errorCodeParseError           errorCode = -32700
	errorCodeInvalidRequest       errorCode = -32600
	errorCodeMethodNotFound       errorCode = -32601
	errorCodeInvalidParams        errorCode = -32602
	errorCodeInternalError        errorCode = -32603
	errorCodeNotInitialized       errorCode = -32000
	errorCodeConfigConflict       errorCode = -32009
	errorCodeWorkspaceMissing     errorCode = -32010
	errorCodeWorkspaceUnreachable errorCode = -32011
)

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type notification struct {
	JSONRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    errorCode `json:"code"`
	Message string    `json:"message"`
	Data    any       `json:"data,omitempty"`
}

type incomingMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
}

func (m incomingMessage) isRequest() bool {
	return m.Method != "" && len(m.ID) > 0
}

func (m incomingMessage) isNotification() bool {
	return m.Method != "" && len(m.ID) == 0
}

type initializeParams struct {
	ClientName      string `json:"client_name,omitempty"`
	ClientVersion   string `json:"client_version,omitempty"`
	ProtocolVersion int    `json:"protocol_version"`
}

type initializeResult struct {
	ProtocolVersion int                   `json:"protocol_version"`
	ServerName      string                `json:"server_name"`
	ServerVersion   string                `json:"server_version"`
	Capabilities    serverCapabilitiesDTO `json:"capabilities"`
}

type serverCapabilitiesDTO struct {
	Methods       []string `json:"methods"`
	Notifications []string `json:"notifications"`
}

type serviceStatusResult struct {
	StateDir         string `json:"state_dir"`
	ServerVersion    string `json:"server_version"`
	ProtocolVersion  int    `json:"protocol_version"`
	WorkspaceCount   int    `json:"workspace_count"`
	RuntimeCount     int    `json:"runtime_count"`
	ConnectedClients int    `json:"connected_clients"`
}

type serviceShutdownResult struct {
	Accepted bool `json:"accepted"`
}

type workspaceListResult struct {
	Workspaces []workspaceSummaryDTO `json:"workspaces"`
}

type workspaceGetParams struct {
	WorkspaceID string `json:"workspace_id"`
}

type workspaceGetResult struct {
	Workspace workspaceSummaryDTO `json:"workspace"`
}

type workspaceAddParams struct {
	Path        string `json:"path"`
	DisplayName string `json:"display_name,omitempty"`
}

type workspaceAddResult struct {
	Workspace workspaceSummaryDTO `json:"workspace"`
}

type workspaceUpdateParams struct {
	WorkspaceID string `json:"workspace_id"`
	DisplayName string `json:"display_name,omitempty"`
}

type workspaceUpdateResult struct {
	Workspace workspaceSummaryDTO `json:"workspace"`
}

type workspaceRemoveParams struct {
	WorkspaceID string `json:"workspace_id"`
}

type workspaceRemoveResult struct {
	Removed bool `json:"removed"`
}

type workspaceSummaryDTO struct {
	WorkspaceID       string            `json:"workspace_id"`
	Path              string            `json:"path"`
	DisplayName       string            `json:"display_name"`
	Source            string            `json:"source"`
	Reachable         bool              `json:"reachable"`
	WorktreeAvailable bool              `json:"worktree_available"`
	AddedAt           time.Time         `json:"added_at"`
	LastOpenedAt      *time.Time        `json:"last_opened_at,omitempty"`
	TaskCounts        taskCountsDTO     `json:"task_counts"`
	Actor             workspaceActorDTO `json:"actor"`
}

type taskGetParams struct {
	WorkspaceID string `json:"workspace_id"`
	TaskID      string `json:"task_id"`
}

type taskInputRequestParams struct {
	WorkspaceID string `json:"workspace_id"`
	TaskID      string `json:"task_id"`
	NodeRunID   string `json:"node_run_id"`
}

type taskStartParams struct {
	WorkspaceID     string `json:"workspace_id"`
	ClientCommandID string `json:"client_command_id,omitempty"`
	Description     string `json:"description,omitempty"`
	ConfigAlias     string `json:"config_alias,omitempty"`
	ConfigPath      string `json:"config_path,omitempty"`
	UseWorktree     bool   `json:"use_worktree,omitempty"`
}

type taskStartFollowUpParams struct {
	WorkspaceID     string `json:"workspace_id"`
	ClientCommandID string `json:"client_command_id,omitempty"`
	ParentTaskID    string `json:"parent_task_id"`
	Description     string `json:"description,omitempty"`
	ConfigAlias     string `json:"config_alias,omitempty"`
	ConfigPath      string `json:"config_path,omitempty"`
}

type taskSubmitInputParams struct {
	WorkspaceID     string                 `json:"workspace_id"`
	ClientCommandID string                 `json:"client_command_id,omitempty"`
	TaskID          string                 `json:"task_id"`
	NodeRunID       string                 `json:"node_run_id"`
	Payload         map[string]interface{} `json:"payload,omitempty"`
}

type taskRetryNodeParams struct {
	WorkspaceID     string `json:"workspace_id"`
	ClientCommandID string `json:"client_command_id,omitempty"`
	TaskID          string `json:"task_id"`
	NodeRunID       string `json:"node_run_id"`
	Force           bool   `json:"force,omitempty"`
}

type taskContinueBlockedParams struct {
	WorkspaceID     string `json:"workspace_id"`
	ClientCommandID string `json:"client_command_id,omitempty"`
	TaskID          string `json:"task_id"`
}

type artifactListParams struct {
	WorkspaceID string `json:"workspace_id"`
	TaskID      string `json:"task_id"`
}

type taskCountsDTO struct {
	Running  int `json:"running"`
	Awaiting int `json:"awaiting"`
	Done     int `json:"done"`
	Failed   int `json:"failed"`
}

type workspaceActorDTO struct {
	State     string `json:"state"`
	LastError string `json:"last_error"`
}

type notificationParams struct {
	EventID     string    `json:"event_id"`
	At          time.Time `json:"at"`
	Kind        string    `json:"kind"`
	WorkspaceID string    `json:"workspace_id,omitempty"`
	Payload     any       `json:"payload,omitempty"`
}

type taskListParams struct {
	WorkspaceID string `json:"workspace_id"`
}

type taskListResult struct {
	Tasks []taskViewDTO `json:"tasks"`
}

type taskGetResult struct {
	Task         taskViewDTO      `json:"task"`
	Config       *configViewDTO   `json:"config,omitempty"`
	InputRequest *inputRequestDTO `json:"input_request,omitempty"`
}

type taskInputRequestResult struct {
	InputRequest *inputRequestDTO `json:"input_request,omitempty"`
}

type configCatalogResult struct {
	DefaultAlias       string                  `json:"default_alias"`
	DefaultUseWorktree bool                    `json:"default_use_worktree"`
	Entries            []configCatalogEntryDTO `json:"entries"`
}

type configGetParams struct {
	Alias string `json:"alias"`
}

type configGetResult struct {
	Entry configDetailDTO `json:"entry"`
}

type configCloneParams struct {
	SourceAlias string `json:"source_alias"`
	NewAlias    string `json:"new_alias"`
}

type configCloneResult struct {
	Entry configDetailDTO `json:"entry"`
}

type configRenameParams struct {
	Alias    string `json:"alias"`
	NewAlias string `json:"new_alias"`
}

type configRenameResult struct {
	Entry configDetailDTO `json:"entry"`
}

type configDeleteParams struct {
	Alias string `json:"alias"`
}

type configDeleteResult struct {
	Removed bool `json:"removed"`
}

type configResetParams struct {
	Alias string `json:"alias"`
}

type configResetResult struct {
	Entry configDetailDTO `json:"entry"`
}

type configSetDefaultParams struct {
	Alias string `json:"alias"`
}

type configSetDefaultResult struct {
	Entry configDetailDTO `json:"entry"`
}

type configValidateParams struct {
	Config *taskconfig.Config `json:"config"`
}

type configValidateResult struct {
	Valid             bool                `json:"valid"`
	Config            *taskconfig.Config  `json:"config,omitempty"`
	RuntimeID         appconfig.RuntimeID `json:"runtime_id,omitempty"`
	RuntimeName       string              `json:"runtime_name,omitempty"`
	RuntimeConfigured bool                `json:"runtime_configured"`
	Error             string              `json:"error,omitempty"`
}

type configSaveParams struct {
	Alias            string             `json:"alias"`
	ExpectedRevision string             `json:"expected_revision"`
	Config           *taskconfig.Config `json:"config"`
}

type configSaveResult struct {
	Entry configDetailDTO `json:"entry"`
}

type configPromptGetParams struct {
	Alias    string `json:"alias"`
	NodeName string `json:"node_name"`
}

type configPromptGetResult struct {
	Prompt configPromptDTO `json:"prompt"`
}

type configPromptSaveParams struct {
	Alias            string `json:"alias"`
	NodeName         string `json:"node_name"`
	ExpectedRevision string `json:"expected_revision,omitempty"`
	Content          string `json:"content"`
}

type configPromptSaveResult struct {
	Prompt configPromptDTO `json:"prompt"`
}

type runtimeListResult struct {
	Runtimes []runtimeEntryDTO `json:"runtimes"`
}

type artifactListResult struct {
	Artifacts []artifactRefDTO `json:"artifacts"`
}

type commandAcceptedResult struct {
	Accepted        bool   `json:"accepted"`
	ClientCommandID string `json:"client_command_id,omitempty"`
}

type taskNotificationPayload struct {
	ClientCommandID string      `json:"client_command_id,omitempty"`
	Event           runEventDTO `json:"event"`
}

type workspaceNotificationPayload struct {
	Workspace workspaceSummaryDTO `json:"workspace"`
}

type workspaceRemovedPayload struct {
	Removed bool `json:"removed"`
}

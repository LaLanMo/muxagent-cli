package appserver

import "encoding/json"

const (
	jsonRPCVersion  = "2.0"
	protocolVersion = 1
)

const (
	methodInitialize          = "initialize"
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
	methodServiceStatus       = "service.status"
	methodServiceShutdown     = "service.shutdown"
)

type errorCode int

const (
	errorCodeParseError     errorCode = -32700
	errorCodeInvalidRequest errorCode = -32600
	errorCodeMethodNotFound errorCode = -32601
	errorCodeInvalidParams  errorCode = -32602
	errorCodeInternalError  errorCode = -32603
	errorCodeNotInitialized errorCode = -32000
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
	WorkDir         string                `json:"work_dir"`
	Capabilities    serverCapabilitiesDto `json:"capabilities"`
}

type serverCapabilitiesDto struct {
	Methods       []string `json:"methods"`
	Notifications []string `json:"notifications"`
}

type taskGetParams struct {
	TaskID string `json:"task_id"`
}

type taskInputRequestParams struct {
	TaskID    string `json:"task_id"`
	NodeRunID string `json:"node_run_id"`
}

type taskStartParams struct {
	ClientCommandID string `json:"client_command_id,omitempty"`
	Description     string `json:"description,omitempty"`
	ConfigAlias     string `json:"config_alias,omitempty"`
	ConfigPath      string `json:"config_path,omitempty"`
	UseWorktree     bool   `json:"use_worktree,omitempty"`
}

type taskStartFollowUpParams struct {
	ClientCommandID string `json:"client_command_id,omitempty"`
	ParentTaskID    string `json:"parent_task_id"`
	Description     string `json:"description,omitempty"`
	ConfigAlias     string `json:"config_alias,omitempty"`
	ConfigPath      string `json:"config_path,omitempty"`
}

type taskSubmitInputParams struct {
	ClientCommandID string                 `json:"client_command_id,omitempty"`
	TaskID          string                 `json:"task_id"`
	NodeRunID       string                 `json:"node_run_id"`
	Payload         map[string]interface{} `json:"payload,omitempty"`
}

type taskRetryNodeParams struct {
	ClientCommandID string `json:"client_command_id,omitempty"`
	TaskID          string `json:"task_id"`
	NodeRunID       string `json:"node_run_id"`
	Force           bool   `json:"force,omitempty"`
}

type taskContinueBlockedParams struct {
	ClientCommandID string `json:"client_command_id,omitempty"`
	TaskID          string `json:"task_id"`
}

type artifactListParams struct {
	TaskID string `json:"task_id"`
}

type serviceStatusResult struct {
	WorkDir            string `json:"work_dir"`
	ServerVersion      string `json:"server_version"`
	ProtocolVersion    int    `json:"protocol_version"`
	WorktreeAvailable  bool   `json:"worktree_available"`
	DefaultUseWorktree bool   `json:"default_use_worktree"`
}

type taskListResult struct {
	Tasks []taskViewDto `json:"tasks"`
}

type taskGetResult struct {
	Task         taskViewDto      `json:"task"`
	Config       *configViewDto   `json:"config,omitempty"`
	InputRequest *inputRequestDto `json:"input_request,omitempty"`
}

type taskInputRequestResult struct {
	InputRequest *inputRequestDto `json:"input_request,omitempty"`
}

type configCatalogResult struct {
	DefaultAlias string                  `json:"default_alias"`
	Entries      []configCatalogEntryDto `json:"entries"`
}

type artifactListResult struct {
	Artifacts []artifactRefDto `json:"artifacts"`
}

type commandAcceptedResult struct {
	Accepted        bool   `json:"accepted"`
	ClientCommandID string `json:"client_command_id,omitempty"`
}

type serviceShutdownResult struct {
	Accepted bool `json:"accepted"`
}

type eventNotificationParams struct {
	ClientCommandID string      `json:"client_command_id,omitempty"`
	Event           runEventDto `json:"event"`
}

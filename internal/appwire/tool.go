package appwire

type ToolStatus string

const (
	ToolStatusPending    ToolStatus = "pending"
	ToolStatusInProgress ToolStatus = "in_progress"
	ToolStatusCompleted  ToolStatus = "completed"
	ToolStatusFailed     ToolStatus = "failed"
)

type ClaudeCodeTool struct {
	ParentToolUseID string `json:"parentToolUseId,omitempty"`
	ToolName        string `json:"toolName,omitempty"`
}

type ToolInput struct {
	Description  string         `json:"description,omitempty"`
	Command      *ToolCommand   `json:"command,omitempty"`
	FilePath     string         `json:"filePath,omitempty"`
	SourcePath   string         `json:"sourcePath,omitempty"`
	TargetPath   string         `json:"targetPath,omitempty"`
	Pattern      string         `json:"pattern,omitempty"`
	URL          string         `json:"url,omitempty"`
	Mode         string         `json:"mode,omitempty"`
	Edit         *ToolEditInput `json:"edit,omitempty"`
	RawInputJSON string         `json:"rawInputJson,omitempty"`
}

type ToolCommand struct {
	Argv    []string `json:"argv,omitempty"`
	Display string   `json:"display,omitempty"`
}

type ToolEditInput struct {
	FilePath  string `json:"filePath,omitempty"`
	OldString string `json:"oldString,omitempty"`
	NewString string `json:"newString,omitempty"`
}

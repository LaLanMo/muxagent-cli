package acp

import (
	"encoding/json"
	"strings"

	"github.com/LaLanMo/muxagent-cli/internal/acpprotocol"
	"github.com/LaLanMo/muxagent-cli/internal/appwire"
)

func toolInputFromRaw(raw json.RawMessage) *appwire.ToolInput {
	if len(raw) == 0 {
		return nil
	}
	var input map[string]any
	if err := json.Unmarshal(raw, &input); err != nil {
		return nil
	}
	result := &appwire.ToolInput{
		Description:  stringFromMap(input, "description"),
		FilePath:     firstStringFromMap(input, "file_path", "path"),
		SourcePath:   firstStringFromMap(input, "source", "path"),
		TargetPath:   firstStringFromMap(input, "destination", "target"),
		Pattern:      stringFromMap(input, "pattern"),
		URL:          stringFromMap(input, "url"),
		Mode:         stringFromMap(input, "mode"),
		RawInputJSON: string(raw),
	}

	if command := commandFromMap(input); command != nil {
		result.Command = command
	}

	oldString := stringFromMap(input, "old_string")
	newString := stringFromMap(input, "new_string")
	editFilePath := firstStringFromMap(input, "file_path", "path")
	if oldString != "" || newString != "" || editFilePath != "" {
		result.Edit = &appwire.ToolEditInput{
			FilePath:  editFilePath,
			OldString: oldString,
			NewString: newString,
		}
		if result.FilePath == "" {
			result.FilePath = editFilePath
		}
	}

	if result.Description == "" &&
		result.Command == nil &&
		result.FilePath == "" &&
		result.SourcePath == "" &&
		result.TargetPath == "" &&
		result.Pattern == "" &&
		result.URL == "" &&
		result.Mode == "" &&
		result.Edit == nil &&
		result.RawInputJSON == "" {
		return nil
	}

	return result
}

func claudeCodeToolFromMeta(meta acpprotocol.Meta) *appwire.ClaudeCodeTool {
	if len(meta) == 0 {
		return nil
	}

	claudeCode, ok := meta["claudeCode"].(map[string]any)
	if !ok {
		return nil
	}

	parentToolUseID, _ := claudeCode["parentToolUseId"].(string)
	toolName, _ := claudeCode["toolName"].(string)
	if parentToolUseID == "" && toolName == "" {
		return nil
	}

	return &appwire.ClaudeCodeTool{
		ParentToolUseID: parentToolUseID,
		ToolName:        toolName,
	}
}

func stringFromMap(input map[string]any, key string) string {
	value, _ := input[key].(string)
	return value
}

func firstStringFromMap(input map[string]any, keys ...string) string {
	for _, key := range keys {
		if value := stringFromMap(input, key); value != "" {
			return value
		}
	}
	return ""
}

func commandFromMap(input map[string]any) *appwire.ToolCommand {
	command := input["command"]
	switch value := command.(type) {
	case string:
		if value == "" {
			return nil
		}
		return &appwire.ToolCommand{Display: value}
	case []any:
		argv := make([]string, 0, len(value))
		for _, item := range value {
			arg, ok := item.(string)
			if !ok || arg == "" {
				return nil
			}
			argv = append(argv, arg)
		}
		if len(argv) == 0 {
			return nil
		}
		return &appwire.ToolCommand{
			Argv:    argv,
			Display: strings.Join(argv, " "),
		}
	default:
		return nil
	}
}

func stringPtrValue(value *acpprotocol.ToolKind) string {
	if value == nil {
		return ""
	}
	return string(*value)
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func toolCallStatusValue(value *acpprotocol.ToolCallStatus) string {
	if value == nil {
		return ""
	}
	return string(*value)
}

func intPtrFromUint32(value *uint32) *int {
	if value == nil {
		return nil
	}
	v := int(*value)
	return &v
}

func rawJSONFromMessages(items []json.RawMessage) json.RawMessage {
	if len(items) == 0 {
		return nil
	}
	raw, err := json.Marshal(items)
	if err != nil {
		return nil
	}
	return raw
}

// extractRawOutput handles rawOutput that may be a JSON string or an object
// with an "output" (or "error") key.
func extractRawOutput(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err == nil {
		if output, ok := obj["output"].(string); ok {
			return output
		}
		if errStr, ok := obj["error"].(string); ok {
			return errStr
		}
	}
	return ""
}

// extractTextFromContent extracts text from ACP content array.
// Content is an array of objects like [{"type":"content","content":{"type":"text","text":"..."}}]
func extractTextFromContent(raw json.RawMessage) string {
	if raw == nil {
		return ""
	}
	var items []struct {
		Type    string `json:"type"`
		Content struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(raw, &items); err != nil {
		return ""
	}
	for _, item := range items {
		if item.Content.Text != "" {
			return item.Content.Text
		}
	}
	return ""
}

// extractDiffsFromContent parses diff entries from an ACP content array.
// Each diff entry is: {"type":"diff","path":"...","oldText":"...","newText":"..."}
func extractDiffsFromContent(raw json.RawMessage) []appwire.ToolDiff {
	if len(raw) == 0 {
		return nil
	}
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil
	}
	var diffs []appwire.ToolDiff
	for _, item := range items {
		var entry struct {
			Type    string  `json:"type"`
			Path    string  `json:"path"`
			OldText *string `json:"oldText"`
			NewText string  `json:"newText"`
			Content *struct {
				Type    string  `json:"type"`
				Path    string  `json:"path"`
				OldText *string `json:"oldText"`
				NewText string  `json:"newText"`
			} `json:"content"`
		}
		if err := json.Unmarshal(item, &entry); err != nil {
			continue
		}
		if entry.Type == "diff" && entry.Path != "" {
			diffs = append(diffs, appwire.ToolDiff{
				Path:    entry.Path,
				OldText: entry.OldText,
				NewText: entry.NewText,
			})
		} else if entry.Content != nil && entry.Content.Type == "diff" && entry.Content.Path != "" {
			diffs = append(diffs, appwire.ToolDiff{
				Path:    entry.Content.Path,
				OldText: entry.Content.OldText,
				NewText: entry.Content.NewText,
			})
		}
	}
	if len(diffs) == 0 {
		return nil
	}
	return diffs
}

package taskhistory

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"time"

	"github.com/LaLanMo/muxagent-cli/internal/taskconfig"
	"github.com/LaLanMo/muxagent-cli/internal/taskdomain"
	"github.com/LaLanMo/muxagent-cli/internal/taskexecutor"
	"github.com/LaLanMo/muxagent-cli/internal/taskstore"
)

type MessagePart struct {
	MessageID string `json:"message_id,omitempty"`
	PartID    string `json:"part_id,omitempty"`
	Role      string `json:"role,omitempty"`
	Type      string `json:"type,omitempty"`
	Text      string `json:"text,omitempty"`
}

type ToolDiff struct {
	Path    string  `json:"path"`
	OldText *string `json:"old_text,omitempty"`
	NewText string  `json:"new_text"`
}

type ToolCall struct {
	CallID        string     `json:"call_id,omitempty"`
	ParentCallID  string     `json:"parent_call_id,omitempty"`
	Name          string     `json:"name,omitempty"`
	Kind          string     `json:"kind,omitempty"`
	Title         string     `json:"title,omitempty"`
	Status        string     `json:"status,omitempty"`
	InputSummary  string     `json:"input_summary,omitempty"`
	OutputText    string     `json:"output_text,omitempty"`
	ErrorText     string     `json:"error_text,omitempty"`
	Paths         []string   `json:"paths,omitempty"`
	Diffs         []ToolDiff `json:"diffs,omitempty"`
	RawInputJSON  string     `json:"raw_input_json,omitempty"`
	RawOutputJSON string     `json:"raw_output_json,omitempty"`
}

type PlanStep struct {
	Text   string `json:"text"`
	Status string `json:"status"`
}

type PlanSnapshot struct {
	PlanID string     `json:"plan_id,omitempty"`
	Steps  []PlanStep `json:"steps,omitempty"`
}

type UsageSnapshot struct {
	InputTokens       int64 `json:"input_tokens,omitempty"`
	CachedInputTokens int64 `json:"cached_input_tokens,omitempty"`
	OutputTokens      int64 `json:"output_tokens,omitempty"`
	TotalTokens       int64 `json:"total_tokens,omitempty"`
	DurationMS        int64 `json:"duration_ms,omitempty"`
}

type EventRecord struct {
	EventID          string         `json:"event_id"`
	Seq              uint64         `json:"seq"`
	EmittedAt        time.Time      `json:"emitted_at"`
	RecordedAt       time.Time      `json:"recorded_at"`
	SessionID        string         `json:"session_id,omitempty"`
	ProviderRecordID string         `json:"provider_record_id,omitempty"`
	ProviderSubindex int            `json:"provider_subindex,omitempty"`
	Provenance       string         `json:"provenance,omitempty"`
	Kind             string         `json:"kind"`
	Raw              string         `json:"raw,omitempty"`
	Message          *MessagePart   `json:"message,omitempty"`
	Tool             *ToolCall      `json:"tool,omitempty"`
	Plan             *PlanSnapshot  `json:"plan,omitempty"`
	Usage            *UsageSnapshot `json:"usage,omitempty"`
}

type ReadResult struct {
	SessionID    string        `json:"session_id,omitempty"`
	Provenance   string        `json:"provenance"`
	Completeness string        `json:"completeness"`
	LastSeq      uint64        `json:"last_seq,omitempty"`
	Events       []EventRecord `json:"events,omitempty"`
}

func Append(workDir, taskID, nodeRunID string, progress taskexecutor.Progress, recordedAt time.Time) error {
	records := NewEventRecords(recordedAt, progress)
	if len(records) == 0 {
		return nil
	}
	path := taskstore.RunHistoryPath(workDir, taskID, nodeRunID)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	encoder := json.NewEncoder(file)
	for _, record := range records {
		if err := encoder.Encode(record); err != nil {
			return err
		}
	}
	return nil
}

func ReadAll(workDir, taskID, nodeRunID string) ([]EventRecord, error) {
	data, err := os.ReadFile(taskstore.RunHistoryPath(workDir, taskID, nodeRunID))
	if err != nil {
		return nil, err
	}
	return decodeRecords(data)
}

func LastSeq(workDir, taskID, nodeRunID string) (uint64, error) {
	records, err := ReadAll(workDir, taskID, nodeRunID)
	if err != nil {
		return 0, err
	}
	if len(records) == 0 {
		return 0, nil
	}
	return records[len(records)-1].Seq, nil
}

func decodeRecords(data []byte) ([]EventRecord, error) {
	lines := bytes.Split(data, []byte{'\n'})
	trailingNewline := len(data) == 0 || data[len(data)-1] == '\n'
	records := make([]EventRecord, 0, len(lines))
	for idx, line := range lines {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}

		var record EventRecord
		if err := json.Unmarshal(line, &record); err != nil {
			if idx == len(lines)-1 && !trailingNewline {
				return records, nil
			}
			return nil, err
		}
		records = append(records, record)
	}
	return records, nil
}

func NewEventRecords(recordedAt time.Time, progress taskexecutor.Progress) []EventRecord {
	if len(progress.Events) == 0 {
		return nil
	}
	records := make([]EventRecord, 0, len(progress.Events))
	for _, event := range progress.Events {
		records = append(records, eventRecordFromExecutor(event, recordedAt.UTC()))
	}
	return records
}

func eventRecordFromExecutor(event taskexecutor.StreamEvent, recordedAt time.Time) EventRecord {
	record := EventRecord{
		EventID:          event.EventID,
		Seq:              event.Seq,
		EmittedAt:        event.EmittedAt.UTC(),
		RecordedAt:       recordedAt,
		SessionID:        event.SessionID,
		ProviderRecordID: event.ProviderRecordID,
		ProviderSubindex: event.ProviderSubindex,
		Provenance:       string(event.Provenance),
		Kind:             string(event.Kind),
		Raw:              event.Raw,
	}
	if record.EmittedAt.IsZero() {
		record.EmittedAt = recordedAt
	}
	if record.Provenance == "" {
		record.Provenance = string(taskexecutor.StreamEventProvenanceExecutorPersisted)
	}
	if event.Message != nil {
		record.Message = &MessagePart{
			MessageID: event.Message.MessageID,
			PartID:    event.Message.PartID,
			Role:      string(event.Message.Role),
			Type:      string(event.Message.Type),
			Text:      event.Message.Text,
		}
	}
	if event.Tool != nil {
		diffs := make([]ToolDiff, 0, len(event.Tool.Diffs))
		for _, diff := range event.Tool.Diffs {
			diffs = append(diffs, ToolDiff{
				Path:    diff.Path,
				OldText: diff.OldText,
				NewText: diff.NewText,
			})
		}
		record.Tool = &ToolCall{
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
		steps := make([]PlanStep, 0, len(event.Plan.Steps))
		for _, step := range event.Plan.Steps {
			steps = append(steps, PlanStep{Text: step.Text, Status: step.Status})
		}
		record.Plan = &PlanSnapshot{
			PlanID: event.Plan.PlanID,
			Steps:  steps,
		}
	}
	if event.Usage != nil {
		record.Usage = &UsageSnapshot{
			InputTokens:       event.Usage.InputTokens,
			CachedInputTokens: event.Usage.CachedInputTokens,
			OutputTokens:      event.Usage.OutputTokens,
			TotalTokens:       event.Usage.TotalTokens,
			DurationMS:        event.Usage.DurationMS,
		}
	}
	return record
}

func IsMissing(err error) bool {
	return errors.Is(err, os.ErrNotExist)
}

func Load(task taskdomain.Task, cfg *taskconfig.Config, run taskdomain.NodeRun) (ReadResult, error) {
	local, err := loadPersistedHistory(task, run)
	if err != nil {
		return ReadResult{}, err
	}
	if cfg == nil || run.SessionID == "" {
		return local, nil
	}

	backfill, err := readProviderBackfill(task, cfg.Runtime, run)
	if err != nil {
		if len(local.Events) > 0 {
			return local, nil
		}
		return ReadResult{}, err
	}
	return mergeReadResults(local, backfill), nil
}

func loadPersistedHistory(task taskdomain.Task, run taskdomain.NodeRun) (ReadResult, error) {
	result := ReadResult{
		SessionID:    run.SessionID,
		Provenance:   "none",
		Completeness: "none",
	}

	records, err := ReadAll(task.WorkDir, task.ID, run.ID)
	if err != nil && !IsMissing(err) {
		return ReadResult{}, err
	}
	if len(records) == 0 {
		return result, nil
	}
	result.Provenance = string(taskexecutor.StreamEventProvenanceExecutorPersisted)
	result.Completeness = "open"
	if run.Status == taskdomain.NodeRunDone || run.Status == taskdomain.NodeRunFailed || run.Status == taskdomain.NodeRunAwaitingUser {
		result.Completeness = "complete"
	}
	result.Events = records
	for _, record := range records {
		if result.SessionID == "" && record.SessionID != "" {
			result.SessionID = record.SessionID
		}
		if record.Seq > result.LastSeq {
			result.LastSeq = record.Seq
		}
	}
	return result, nil
}

func mergeReadResults(local, backfill ReadResult) ReadResult {
	switch {
	case len(local.Events) == 0:
		return backfill
	case len(backfill.Events) == 0:
		return local
	}

	merged := ReadResult{
		SessionID:    firstNonEmpty(local.SessionID, backfill.SessionID),
		Provenance:   local.Provenance,
		Completeness: mergeCompleteness(local.Completeness, backfill.Completeness),
	}
	merged.Events = mergeEventRecords(local.Events, backfill.Events)
	for _, record := range merged.Events {
		if merged.SessionID == "" && record.SessionID != "" {
			merged.SessionID = record.SessionID
		}
		if record.Seq > merged.LastSeq {
			merged.LastSeq = record.Seq
		}
	}
	if len(merged.Events) != len(local.Events) || backfill.Completeness != local.Completeness || backfill.SessionID != "" && backfill.SessionID != local.SessionID {
		merged.Provenance = "mixed_recovered"
	}
	return merged
}

func mergeCompleteness(local, backfill string) string {
	if local == "complete" || backfill == "complete" {
		return "complete"
	}
	if local == "open" || backfill == "open" {
		return "open"
	}
	return firstNonEmpty(local, backfill, "none")
}

func mergeEventRecords(local, backfill []EventRecord) []EventRecord {
	merged := append([]EventRecord(nil), local...)
	indexByKey := map[string]int{}
	indexByProviderKey := map[string]int{}
	for idx, record := range merged {
		if key := mergeEventRecordKey(record); key != "" {
			indexByKey[key] = idx
		}
		if key := providerRecordKey(record); key != "" {
			indexByProviderKey[key] = idx
		}
	}
	for _, record := range backfill {
		if idx, ok := indexByProviderKey[providerRecordKey(record)]; ok {
			merged[idx] = mergeEventRecord(merged[idx], record)
			continue
		}
		if key := mergeEventRecordKey(record); key != "" {
			if idx, ok := indexByKey[key]; ok {
				merged[idx] = mergeEventRecord(merged[idx], record)
				continue
			}
		}
		merged = append(merged, record)
		idx := len(merged) - 1
		if key := mergeEventRecordKey(record); key != "" {
			indexByKey[key] = idx
		}
		if key := providerRecordKey(record); key != "" {
			indexByProviderKey[key] = idx
		}
	}
	sort.SliceStable(merged, func(i, j int) bool {
		left := merged[i]
		right := merged[j]
		if !left.EmittedAt.Equal(right.EmittedAt) {
			return left.EmittedAt.Before(right.EmittedAt)
		}
		if left.Seq != right.Seq {
			return left.Seq < right.Seq
		}
		if !left.RecordedAt.Equal(right.RecordedAt) {
			return left.RecordedAt.Before(right.RecordedAt)
		}
		return left.EventID < right.EventID
	})
	return merged
}

func mergeEventRecord(existing, next EventRecord) EventRecord {
	return eventRecordFromExecutor(taskexecutor.MergeStreamEvent(eventRecordToStream(existing), eventRecordToStream(next)), laterRecordedAt(existing.RecordedAt, next.RecordedAt))
}

func eventRecordToStream(record EventRecord) taskexecutor.StreamEvent {
	event := taskexecutor.StreamEvent{
		EventID:          record.EventID,
		Seq:              record.Seq,
		EmittedAt:        record.EmittedAt,
		SessionID:        record.SessionID,
		ProviderRecordID: record.ProviderRecordID,
		ProviderSubindex: record.ProviderSubindex,
		Provenance:       taskexecutor.StreamEventProvenance(record.Provenance),
		Kind:             taskexecutor.StreamEventKind(record.Kind),
		Raw:              record.Raw,
	}
	if record.Message != nil {
		event.Message = &taskexecutor.MessagePart{
			MessageID: record.Message.MessageID,
			PartID:    record.Message.PartID,
			Role:      taskexecutor.MessageRole(record.Message.Role),
			Type:      taskexecutor.MessagePartType(record.Message.Type),
			Text:      record.Message.Text,
		}
	}
	if record.Tool != nil {
		diffs := make([]taskexecutor.ToolDiff, 0, len(record.Tool.Diffs))
		for _, diff := range record.Tool.Diffs {
			diffs = append(diffs, taskexecutor.ToolDiff{
				Path:    diff.Path,
				OldText: diff.OldText,
				NewText: diff.NewText,
			})
		}
		event.Tool = &taskexecutor.ToolCall{
			CallID:        record.Tool.CallID,
			ParentCallID:  record.Tool.ParentCallID,
			Name:          record.Tool.Name,
			Kind:          taskexecutor.ToolKind(record.Tool.Kind),
			Title:         record.Tool.Title,
			Status:        taskexecutor.ToolStatus(record.Tool.Status),
			InputSummary:  record.Tool.InputSummary,
			OutputText:    record.Tool.OutputText,
			ErrorText:     record.Tool.ErrorText,
			Paths:         append([]string(nil), record.Tool.Paths...),
			Diffs:         diffs,
			RawInputJSON:  record.Tool.RawInputJSON,
			RawOutputJSON: record.Tool.RawOutputJSON,
		}
	}
	if record.Plan != nil {
		steps := make([]taskexecutor.PlanStep, 0, len(record.Plan.Steps))
		for _, step := range record.Plan.Steps {
			steps = append(steps, taskexecutor.PlanStep{Text: step.Text, Status: step.Status})
		}
		event.Plan = &taskexecutor.PlanSnapshot{PlanID: record.Plan.PlanID, Steps: steps}
	}
	if record.Usage != nil {
		event.Usage = &taskexecutor.UsageSnapshot{
			InputTokens:       record.Usage.InputTokens,
			CachedInputTokens: record.Usage.CachedInputTokens,
			OutputTokens:      record.Usage.OutputTokens,
			TotalTokens:       record.Usage.TotalTokens,
			DurationMS:        record.Usage.DurationMS,
		}
	}
	return event
}

func stableEventRecordKey(record EventRecord) string {
	return eventRecordToStream(record).StableKey()
}

func mergeEventRecordKey(record EventRecord) string {
	if record.Kind == string(taskexecutor.StreamEventKindTool) && record.Tool != nil && record.Tool.CallID != "" {
		phase := "active"
		if record.Tool.Status == string(taskexecutor.ToolStatusCompleted) ||
			record.Tool.Status == string(taskexecutor.ToolStatusFailed) ||
			record.Tool.OutputText != "" ||
			record.Tool.ErrorText != "" ||
			len(record.Tool.Diffs) > 0 {
			phase = "terminal"
		}
		return "tool:" + record.Tool.CallID + ":" + phase
	}
	return stableEventRecordKey(record)
}

func providerRecordKey(record EventRecord) string {
	if record.ProviderRecordID == "" {
		return ""
	}
	return record.ProviderRecordID + "#" + strconv.Itoa(record.ProviderSubindex)
}

func laterRecordedAt(left, right time.Time) time.Time {
	if right.After(left) {
		return right.UTC()
	}
	return left.UTC()
}

package taskhistory

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/LaLanMo/muxagent-cli/internal/taskexecutor"
	"github.com/LaLanMo/muxagent-cli/internal/taskstore"
)

func TestReadAllIgnoresPartialTrailingChunk(t *testing.T) {
	workDir := t.TempDir()
	taskID := "task-1"
	nodeRunID := "run-1"

	recordedAt := time.Date(2026, 4, 8, 10, 0, 0, 0, time.UTC)
	if err := Append(workDir, taskID, nodeRunID, taskexecutor.Progress{
		SessionID: "session-1",
		Events: []taskexecutor.StreamEvent{{
			EventID:    "evt-1",
			Seq:        1,
			EmittedAt:  recordedAt,
			SessionID:  "session-1",
			Kind:       taskexecutor.StreamEventKindMessage,
			Provenance: taskexecutor.StreamEventProvenanceExecutorPersisted,
			Message: &taskexecutor.MessagePart{
				MessageID: "msg-1",
				PartID:    "part-1",
				Role:      taskexecutor.MessageRoleAssistant,
				Type:      taskexecutor.MessagePartTypeText,
				Text:      "first",
			},
		}},
	}, recordedAt); err != nil {
		t.Fatalf("append first event: %v", err)
	}
	if err := Append(workDir, taskID, nodeRunID, taskexecutor.Progress{
		SessionID: "session-1",
		Events: []taskexecutor.StreamEvent{{
			EventID:    "evt-2",
			Seq:        2,
			EmittedAt:  recordedAt.Add(time.Second),
			SessionID:  "session-1",
			Kind:       taskexecutor.StreamEventKindMessage,
			Provenance: taskexecutor.StreamEventProvenanceExecutorPersisted,
			Message: &taskexecutor.MessagePart{
				MessageID: "msg-1",
				PartID:    "part-2",
				Role:      taskexecutor.MessageRoleAssistant,
				Type:      taskexecutor.MessagePartTypeText,
				Text:      "second",
			},
		}},
	}, recordedAt.Add(time.Second)); err != nil {
		t.Fatalf("append second event: %v", err)
	}

	path := taskstore.RunHistoryPath(workDir, taskID, nodeRunID)
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		t.Fatalf("open history file: %v", err)
	}
	if _, err := file.WriteString(`{"event_id":"evt-partial","seq":3,"kind":"message","message":{"text":"partial"}`); err != nil {
		_ = file.Close()
		t.Fatalf("append partial line: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close history file: %v", err)
	}

	records, err := ReadAll(workDir, taskID, nodeRunID)
	if err != nil {
		t.Fatalf("read history: %v", err)
	}
	if got := len(records); got != 2 {
		t.Fatalf("record count = %d, want 2", got)
	}
	if got := records[0].Message.Text; got != "first" {
		t.Fatalf("first record message = %q, want first", got)
	}
	if got := records[1].Message.Text; got != "second" {
		t.Fatalf("second record message = %q, want second", got)
	}
}

func TestReadAllRejectsMalformedCommittedChunk(t *testing.T) {
	workDir := t.TempDir()
	taskID := "task-2"
	nodeRunID := "run-2"

	path := taskstore.RunHistoryPath(workDir, taskID, nodeRunID)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir history dir: %v", err)
	}
	content := []byte("{bad json}\n" +
		`{"event_id":"evt-1","seq":1,"emitted_at":"2026-04-08T10:00:02Z","recorded_at":"2026-04-08T10:00:02Z","kind":"message","message":{"text":"committed"}}` + "\n")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("write malformed history: %v", err)
	}

	if _, err := ReadAll(workDir, taskID, nodeRunID); err == nil {
		t.Fatal("ReadAll succeeded, want malformed committed line error")
	}
}

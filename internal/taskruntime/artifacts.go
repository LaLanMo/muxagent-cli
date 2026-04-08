package taskruntime

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/LaLanMo/muxagent-cli/internal/taskdomain"
	"github.com/LaLanMo/muxagent-cli/internal/taskstore"
)

const (
	inputArtifactName          = "input.md"
	outputArtifactName         = "output.json"
	manifestArtifactName       = "manifest.json"
	clarificationHistoryMarker = "<!-- muxagent:clarification-history -->"
)

type runManifest struct {
	TaskID      string                   `json:"task_id"`
	NodeRunID   string                   `json:"node_run_id"`
	NodeName    string                   `json:"node_name"`
	Sequence    int                      `json:"sequence,omitempty"`
	Status      taskdomain.NodeRunStatus `json:"status"`
	SessionID   string                   `json:"session_id,omitempty"`
	StartedAt   time.Time                `json:"started_at"`
	CompletedAt *time.Time               `json:"completed_at,omitempty"`
}

func runArtifactDirPath(task taskdomain.Task, _ []taskdomain.NodeRun, run taskdomain.NodeRun) (string, error) {
	if strings.TrimSpace(run.ID) == "" {
		return "", fmt.Errorf("node run id is required")
	}
	return taskstore.RunDir(task.WorkDir, task.ID, run.ID), nil
}

func runArtifactDir(task taskdomain.Task, runs []taskdomain.NodeRun, run taskdomain.NodeRun) (string, error) {
	dir, err := runArtifactDirPath(task, runs, run)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	if err := persistRunManifest(task, runs, run); err != nil {
		return "", err
	}
	return dir, nil
}

func runArtifactPath(task taskdomain.Task, runs []taskdomain.NodeRun, run taskdomain.NodeRun, name string) (string, error) {
	dir, err := runArtifactDir(task, runs, run)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, name), nil
}

func runArtifactPathForExistingRun(task taskdomain.Task, runs []taskdomain.NodeRun, run taskdomain.NodeRun, name string) (string, error) {
	dir, err := runArtifactDirPath(task, runs, run)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, name), nil
}

func nodeRunSequence(runs []taskdomain.NodeRun, runID string) int {
	sorted := append([]taskdomain.NodeRun(nil), runs...)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].StartedAt.Equal(sorted[j].StartedAt) {
			return sorted[i].ID < sorted[j].ID
		}
		return sorted[i].StartedAt.Before(sorted[j].StartedAt)
	})
	for i, run := range sorted {
		if run.ID == runID {
			return i + 1
		}
	}
	return 0
}

func persistRunManifest(task taskdomain.Task, runs []taskdomain.NodeRun, run taskdomain.NodeRun) error {
	dir, err := runArtifactDirPath(task, runs, run)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	manifest := runManifest{
		TaskID:      task.ID,
		NodeRunID:   run.ID,
		NodeName:    run.NodeName,
		Sequence:    nodeRunSequence(runs, run.ID),
		Status:      run.Status,
		SessionID:   run.SessionID,
		StartedAt:   run.StartedAt,
		CompletedAt: run.CompletedAt,
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(filepath.Join(dir, manifestArtifactName), data, 0o644)
}

func materializeHumanNodeArtifact(task taskdomain.Task, run taskdomain.NodeRun, runs []taskdomain.NodeRun, payload map[string]interface{}, submittedAt time.Time) (map[string]interface{}, error) {
	outputPath, err := runArtifactPath(task, runs, run, outputArtifactName)
	if err != nil {
		return nil, err
	}
	if _, err := writeHumanInputArtifact(task, run, runs, payload, submittedAt); err != nil {
		return nil, err
	}
	envelope := map[string]interface{}{
		"kind":         "human_node_result",
		"task_id":      task.ID,
		"node_run_id":  run.ID,
		"node_name":    run.NodeName,
		"submitted_at": submittedAt.Format(time.RFC3339Nano),
		"result":       cloneMap(payload),
	}
	data, err := json.MarshalIndent(envelope, "", "  ")
	if err != nil {
		return nil, err
	}
	data = append(data, '\n')
	if err := os.WriteFile(outputPath, data, 0o644); err != nil {
		return nil, err
	}
	return cloneMap(payload), nil
}

func cloneMap(src map[string]interface{}) map[string]interface{} {
	if len(src) == 0 {
		return map[string]interface{}{}
	}
	dst := make(map[string]interface{}, len(src))
	for key, value := range src {
		dst[key] = value
	}
	return dst
}

func writeHumanInputArtifact(task taskdomain.Task, run taskdomain.NodeRun, runs []taskdomain.NodeRun, payload map[string]interface{}, submittedAt time.Time) (string, error) {
	body, err := renderHumanInputMarkdown(payload, submittedAt)
	if err != nil {
		return "", err
	}
	return writeInputArtifact(task, run, runs, body)
}

func ensureAgentInputArtifact(task taskdomain.Task, run taskdomain.NodeRun, runs []taskdomain.NodeRun, prompt string) (string, error) {
	path, err := runArtifactPath(task, runs, run, inputArtifactName)
	if err != nil {
		return "", err
	}
	if len(run.Clarifications) > 0 {
		info, statErr := os.Stat(path)
		if statErr == nil && !info.IsDir() {
			return path, nil
		}
		if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
			return "", statErr
		}
	}
	if err := os.WriteFile(path, renderAgentInputMarkdown(prompt), 0o644); err != nil {
		return "", err
	}
	return path, nil
}

func writeClarificationInputArtifact(task taskdomain.Task, run taskdomain.NodeRun, runs []taskdomain.NodeRun) (string, error) {
	path, err := runArtifactPath(task, runs, run, inputArtifactName)
	if err != nil {
		return "", err
	}
	base, err := readClarificationInputBase(path)
	if err != nil {
		return "", err
	}
	history, err := renderClarificationInputMarkdown(run.Clarifications)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(path, mergeClarificationInputMarkdown(base, history), 0o644); err != nil {
		return "", err
	}
	return path, nil
}

func writeInputArtifact(task taskdomain.Task, run taskdomain.NodeRun, runs []taskdomain.NodeRun, body []byte) (string, error) {
	path, err := runArtifactPath(task, runs, run, inputArtifactName)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(path, body, 0o644); err != nil {
		return "", err
	}
	return path, nil
}

func readClarificationInputBase(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	if idx := bytes.Index(data, []byte(clarificationHistoryMarker)); idx >= 0 {
		data = data[:idx]
	}
	return append([]byte(nil), data...), nil
}

func mergeClarificationInputMarkdown(base []byte, history []byte) []byte {
	out := append([]byte(nil), base...)
	if len(out) > 0 {
		switch {
		case bytes.HasSuffix(out, []byte("\n\n")):
		case bytes.HasSuffix(out, []byte("\n")):
			out = append(out, '\n')
		default:
			out = append(out, '\n', '\n')
		}
	}
	out = append(out, clarificationHistoryMarker...)
	out = append(out, '\n', '\n')
	out = append(out, bytes.TrimRight(history, "\n")...)
	out = append(out, '\n')
	return out
}

func renderHumanInputMarkdown(payload map[string]interface{}, submittedAt time.Time) ([]byte, error) {
	data, err := json.MarshalIndent(cloneMap(payload), "", "  ")
	if err != nil {
		return nil, err
	}
	lines := []string{
		"# Input",
		"",
		fmt.Sprintf("Submitted: %s", submittedAt.Format(time.RFC3339Nano)),
		"",
		"```json",
		string(data),
		"```",
	}
	return []byte(strings.Join(lines, "\n") + "\n"), nil
}

func renderAgentInputMarkdown(prompt string) []byte {
	return []byte(prompt)
}

func renderClarificationInputMarkdown(exchanges []taskdomain.ClarificationExchange) ([]byte, error) {
	lines := []string{"## Clarification History", ""}
	for i, exchange := range exchanges {
		lines = append(lines, fmt.Sprintf("### Exchange %d", i+1), "")
		lines = append(lines, fmt.Sprintf("Requested: %s", exchange.RequestedAt.Format(time.RFC3339Nano)))
		if exchange.AnsweredAt != nil {
			lines = append(lines, fmt.Sprintf("Answered: %s", exchange.AnsweredAt.Format(time.RFC3339Nano)))
		} else {
			lines = append(lines, "Status: awaiting_user")
		}
		lines = append(lines, "")
		for qi, question := range exchange.Request.Questions {
			lines = append(lines, fmt.Sprintf("#### Question %d", qi+1), "", question.Question, "")
			if question.WhyItMatters != "" {
				lines = append(lines, fmt.Sprintf("Why it matters: %s", question.WhyItMatters), "")
			}
			if question.MultiSelect {
				lines = append(lines, "Selection mode: multi-select", "")
			}
			if len(question.Options) > 0 {
				lines = append(lines, "Options:")
				for _, option := range question.Options {
					if option.Description != "" {
						lines = append(lines, fmt.Sprintf("- `%s`: %s", option.Label, option.Description))
					} else {
						lines = append(lines, fmt.Sprintf("- `%s`", option.Label))
					}
				}
				lines = append(lines, "")
			}
			if exchange.Response != nil && qi < len(exchange.Response.Answers) {
				selected, err := json.MarshalIndent(exchange.Response.Answers[qi].Selected, "", "  ")
				if err != nil {
					return nil, err
				}
				lines = append(lines, "Answer:", "", "```json", string(selected), "```", "")
				continue
			}
			lines = append(lines, "Answer: pending", "")
		}
	}
	return []byte(strings.Join(lines, "\n") + "\n"), nil
}

package taskruntime

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/LaLanMo/muxagent-cli/internal/taskdomain"
	"github.com/LaLanMo/muxagent-cli/internal/taskstore"
)

func runArtifactDir(task taskdomain.Task, runs []taskdomain.NodeRun, run taskdomain.NodeRun) (string, error) {
	sequence := nodeRunSequence(runs, run.ID)
	if sequence == 0 {
		return "", fmt.Errorf("node run %q not found in task timeline", run.ID)
	}
	dir := taskstore.ArtifactRunDir(task.WorkDir, task.ID, sequence, run.NodeName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
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

func materializeHumanNodeArtifact(task taskdomain.Task, run taskdomain.NodeRun, runs []taskdomain.NodeRun, payload map[string]interface{}, submittedAt time.Time) (map[string]interface{}, error) {
	artifactDir, err := runArtifactDir(task, runs, run)
	if err != nil {
		return nil, err
	}
	artifactPath := filepath.Join(artifactDir, "output.json")
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
	if err := os.WriteFile(artifactPath, data, 0o644); err != nil {
		return nil, err
	}
	result := cloneMap(payload)
	result["file_paths"] = []interface{}{artifactPath}
	return result, nil
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

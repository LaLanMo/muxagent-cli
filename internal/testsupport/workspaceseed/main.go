package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	appconfig "github.com/LaLanMo/muxagent-cli/internal/config"
	"github.com/LaLanMo/muxagent-cli/internal/taskconfig"
	"github.com/LaLanMo/muxagent-cli/internal/taskdomain"
	"github.com/LaLanMo/muxagent-cli/internal/taskstore"
)

const seededReviewConfig = `version: 1
description: "Seeded review-only workflow"

topology:
  entry: approve
  nodes:
    - name: approve
    - name: done
  edges:
    - from: approve
      to: done
      when:
        field: approved
        equals: true
    - from: approve
      to: done
      when:
        field: approved
        equals: false

node_definitions:
  approve:
    type: human
    result_schema:
      type: object
      additionalProperties: false
      required:
        - approved
      properties:
        approved:
          type: boolean
        feedback:
          type: string
  done:
    type: terminal
`

const seededRetryConfig = `version: 1
description: "Seeded retry workflow"

topology:
  max_iterations: 2
  entry: implement
  nodes:
    - name: implement
    - name: done
  edges:
    - from: implement
      to: done

node_definitions:
  implement:
    system_prompt: ./prompts/implement.md
    result_schema:
      type: object
      additionalProperties: false
      required:
        - file_paths
      properties:
        file_paths:
          type: array
          minItems: 1
          items:
            type: string
  done:
    type: terminal
`

const seededBlockedContinueConfig = `version: 1
description: "Seeded blocked-continue workflow"

topology:
  max_iterations: 1
  entry: implement
  nodes:
    - name: implement
    - name: done
  edges:
    - from: implement
      to: implement
      when:
        field: flag
        equals: retry
    - from: implement
      to: done
      when: else

node_definitions:
  implement:
    system_prompt: ./prompts/implement.md
    result_schema:
      type: object
      additionalProperties: false
      required:
        - flag
        - file_paths
      properties:
        flag:
          type: string
          enum:
            - retry
            - done
        file_paths:
          type: array
          minItems: 1
          items:
            type: string
  done:
    type: terminal
`

type seedResult struct {
	TaskID      string `json:"task_id"`
	Description string `json:"description"`
}

func main() {
	var (
		workDir string
		kind    string
	)
	flag.StringVar(&workDir, "workdir", "", "workspace directory to seed")
	flag.StringVar(&kind, "kind", "", "seed kind: awaiting-review | completed-review | failed-retry | blocked-continue")
	flag.Parse()

	if workDir == "" {
		fatalf("--workdir is required")
	}
	switch kind {
	case "awaiting-review", "completed-review", "failed-retry", "blocked-continue":
	default:
		fatalf("--kind must be one of awaiting-review, completed-review, failed-retry, or blocked-continue")
	}

	normalized := taskstore.NormalizeWorkDir(workDir)
	result, err := seed(normalized, kind)
	if err != nil {
		fatalf("%v", err)
	}
	if err := json.NewEncoder(os.Stdout).Encode(result); err != nil {
		fatalf("%v", err)
	}
}

func seed(workDir, kind string) (*seedResult, error) {
	taskID := map[string]string{
		"awaiting-review":  "task-awaiting-real",
		"completed-review": "task-complete-real",
		"failed-retry":     "task-failed-real",
		"blocked-continue": "task-blocked-real",
	}[kind]
	description := map[string]string{
		"awaiting-review":  "Seeded approval review",
		"completed-review": "Seeded completed review",
		"failed-retry":     "Seeded failed retry",
		"blocked-continue": "Seeded blocked continue",
	}[kind]

	configPath, err := writeSeedConfig(workDir, kind+".yaml")
	if err != nil {
		return nil, err
	}
	materialized, err := taskconfig.Materialize(workDir, taskID, configPath)
	if err != nil {
		return nil, err
	}

	store, err := taskstore.Open(workDir)
	if err != nil {
		return nil, err
	}
	defer func() { _ = store.Close() }()

	now := time.Now().UTC().Add(-10 * time.Minute)
	task := taskdomain.Task{
		ID:           taskID,
		Description:  description,
		ConfigAlias:  trimExtension(kind + ".yaml"),
		ConfigPath:   materialized.ConfigPath,
		WorkDir:      workDir,
		ExecutionDir: workDir,
		CreatedAt:    now,
		UpdatedAt:    now.Add(6 * time.Minute),
	}
	ctx := context.Background()

	switch kind {
	case "awaiting-review":
		entryRun := taskdomain.NodeRun{
			ID:        "run-approve-real",
			TaskID:    taskID,
			NodeName:  "approve",
			Status:    taskdomain.NodeRunAwaitingUser,
			StartedAt: now.Add(time.Minute),
		}
		if err := store.CreateTaskWithEntryRun(ctx, task, entryRun); err != nil {
			return nil, err
		}
	case "completed-review":
		completedAt := now.Add(90 * time.Second)
		entryRun := taskdomain.NodeRun{
			ID:          "run-approve-complete",
			TaskID:      taskID,
			NodeName:    "approve",
			Status:      taskdomain.NodeRunDone,
			Result:      map[string]interface{}{"approved": true},
			StartedAt:   now.Add(time.Minute),
			CompletedAt: &completedAt,
		}
		if err := store.CreateTaskWithEntryRun(ctx, task, entryRun); err != nil {
			return nil, err
		}
		doneAt := completedAt.Add(time.Second)
		doneRun := taskdomain.NodeRun{
			ID:          "run-done-complete",
			TaskID:      taskID,
			NodeName:    "done",
			Status:      taskdomain.NodeRunDone,
			Result:      map[string]interface{}{},
			StartedAt:   doneAt,
			CompletedAt: &doneAt,
		}
		if err := store.SaveNodeRun(ctx, doneRun); err != nil {
			return nil, err
		}
	case "failed-retry":
		entryRun := taskdomain.NodeRun{
			ID:            "run-implement-failed",
			TaskID:        taskID,
			NodeName:      "implement",
			Status:        taskdomain.NodeRunFailed,
			FailureReason: "seeded executor failure",
			StartedAt:     now.Add(time.Minute),
			CompletedAt:   timePtr(now.Add(90 * time.Second)),
		}
		if err := store.CreateTaskWithEntryRun(ctx, task, entryRun); err != nil {
			return nil, err
		}
	case "blocked-continue":
		completedAt := now.Add(90 * time.Second)
		entryRun := taskdomain.NodeRun{
			ID:          "run-implement-blocked",
			TaskID:      taskID,
			NodeName:    "implement",
			Status:      taskdomain.NodeRunDone,
			Result:      map[string]interface{}{"flag": "retry", "file_paths": []interface{}{"seeded-blocked.md"}},
			StartedAt:   now.Add(time.Minute),
			CompletedAt: &completedAt,
		}
		if err := store.CreateTaskWithEntryRun(ctx, task, entryRun); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("unsupported seed kind %q", kind)
	}

	return &seedResult{
		TaskID:      taskID,
		Description: description,
	}, nil
}

func writeSeedConfig(workDir, name string) (string, error) {
	dir := filepath.Join(workDir, ".muxagent", "seed-configs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	bundleDir := filepath.Join(dir, trimExtension(name))
	promptDir := filepath.Join(bundleDir, "prompts")
	if err := os.MkdirAll(promptDir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(bundleDir, "config.yaml")
	payload := seededReviewConfig
	if name == "failed-retry.yaml" {
		payload = seededRetryConfig
		if err := os.WriteFile(filepath.Join(promptDir, "implement.md"), []byte("Implement the requested task."), 0o644); err != nil {
			return "", err
		}
	}
	if name == "blocked-continue.yaml" {
		payload = seededBlockedContinueConfig
		if err := os.WriteFile(filepath.Join(promptDir, "implement.md"), []byte("Continue the blocked implementation."), 0o644); err != nil {
			return "", err
		}
	}
	if err := os.WriteFile(path, []byte(fmt.Sprintf("runtime: %s\n%s", appconfig.RuntimeCodex, payload)), 0o644); err != nil {
		return "", err
	}
	return path, nil
}

func timePtr(value time.Time) *time.Time {
	return &value
}

func trimExtension(name string) string {
	return name[:len(name)-len(filepath.Ext(name))]
}

func fatalf(format string, args ...interface{}) {
	_, _ = fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}

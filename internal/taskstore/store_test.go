package taskstore

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/LaLanMo/muxagent-cli/internal/taskconfig"
	"github.com/LaLanMo/muxagent-cli/internal/taskdomain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStoreRoundTripTaskAndNodeRuns(t *testing.T) {
	ctx := context.Background()
	store, err := OpenAtPath(filepath.Join(t.TempDir(), "tasks.db"))
	require.NoError(t, err)
	defer store.Close()

	now := time.Now().UTC().Round(time.Second)
	task := taskdomain.Task{
		ID:          "task-1",
		Description: "Implement login",
		WorkDir:     "/tmp/project",
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	require.NoError(t, store.CreateTask(ctx, task))

	run := taskdomain.NodeRun{
		ID:        "run-1",
		TaskID:    task.ID,
		NodeName:  "upsert_plan",
		Status:    taskdomain.NodeRunAwaitingUser,
		SessionID: "session-123",
		Result: map[string]interface{}{
			"file_paths": []interface{}{"/tmp/project/.muxagent/tasks/task-1/artifacts/01-upsert_plan/plan.md"},
		},
		Clarifications: []taskdomain.ClarificationExchange{
			{
				Request: taskdomain.ClarificationRequest{
					Questions: []taskdomain.ClarificationQuestion{
						{
							Question:     "Pick one",
							WhyItMatters: "Need a choice",
							Options: []taskdomain.ClarificationOption{
								{Label: "A", Description: "Option A"},
								{Label: "B", Description: "Option B"},
							},
						},
					},
				},
				RequestedAt: now,
				Response: &taskdomain.ClarificationResponse{
					Answers: []taskdomain.ClarificationAnswer{{Selected: "A"}},
				},
				AnsweredAt: ptrTime(now.Add(time.Minute)),
			},
		},
		StartedAt:   now,
		CompletedAt: ptrTime(now.Add(2 * time.Minute)),
	}
	require.NoError(t, store.SaveNodeRun(ctx, run))

	tasks, err := store.ListTasksByWorkDir(ctx, task.WorkDir)
	require.NoError(t, err)
	require.Len(t, tasks, 1)
	assert.Equal(t, task.Description, tasks[0].Description)

	runs, err := store.ListNodeRunsByTask(ctx, task.ID)
	require.NoError(t, err)
	require.Len(t, runs, 1)
	assert.Equal(t, run.SessionID, runs[0].SessionID)
	assert.Len(t, runs[0].Clarifications, 1)

	cfg, err := taskconfig.LoadDefault()
	require.NoError(t, err)
	view := taskdomain.DeriveTaskView(task, cfg, runs)
	assert.Equal(t, taskdomain.TaskStatusAwaitingUser, view.Status)
	assert.NotEmpty(t, view.ArtifactPaths)
}

func TestStoreSchemaRejectsInvalidJSONColumns(t *testing.T) {
	ctx := context.Background()
	store, err := OpenAtPath(filepath.Join(t.TempDir(), "tasks.db"))
	require.NoError(t, err)
	defer store.Close()

	now := time.Now().UTC().Round(time.Second)
	task := taskdomain.Task{
		ID:          "task-json",
		Description: "Validate JSON",
		WorkDir:     "/tmp/project",
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	require.NoError(t, store.CreateTask(ctx, task))

	tests := []struct {
		name        string
		resultJSON  interface{}
		clarifyJSON string
		triggeredBy interface{}
	}{
		{
			name:        "invalid result json",
			resultJSON:  "{not-json",
			clarifyJSON: "[]",
		},
		{
			name:        "invalid clarifications json",
			resultJSON:  nil,
			clarifyJSON: "{not-json",
		},
		{
			name:        "invalid triggered_by json",
			resultJSON:  nil,
			clarifyJSON: "[]",
			triggeredBy: "{not-json",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := store.db.ExecContext(ctx, `
				INSERT INTO node_runs (
					id, task_id, node_name, status, session_id, result_json, clarifications_json, triggered_by_json, started_at, completed_at
				) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				tt.name,
				task.ID,
				"upsert_plan",
				string(taskdomain.NodeRunDone),
				nil,
				tt.resultJSON,
				tt.clarifyJSON,
				tt.triggeredBy,
				now.Format(time.RFC3339Nano),
				now.Format(time.RFC3339Nano),
			)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "CHECK constraint failed")
		})
	}
}

func TestStoreSchemaDeclaresJSONValidityChecks(t *testing.T) {
	store, err := OpenAtPath(filepath.Join(t.TempDir(), "tasks.db"))
	require.NoError(t, err)
	defer store.Close()

	tableSQL := lookupTableSQL(t, store.db, "node_runs")
	assert.Contains(t, tableSQL, "json_valid(result_json)")
	assert.Contains(t, tableSQL, "json_valid(clarifications_json)")
	assert.Contains(t, tableSQL, "json_valid(triggered_by_json)")
}

func TestNormalizeWorkDirResolvesSymlinks(t *testing.T) {
	realDir := t.TempDir()
	linkParent := t.TempDir()
	linkPath := filepath.Join(linkParent, "linked")
	require.NoError(t, os.Symlink(realDir, linkPath))

	expected, err := filepath.EvalSymlinks(realDir)
	require.NoError(t, err)
	assert.Equal(t, expected, NormalizeWorkDir(linkPath))
	assert.Equal(t, expected, NormalizeWorkDir(realDir))
}

func ptrTime(value time.Time) *time.Time {
	return &value
}

func lookupTableSQL(t *testing.T, db *sql.DB, tableName string) string {
	t.Helper()
	var sqlText string
	err := db.QueryRow(`SELECT sql FROM sqlite_master WHERE type = 'table' AND name = ?`, tableName).Scan(&sqlText)
	require.NoError(t, err)
	return sqlText
}

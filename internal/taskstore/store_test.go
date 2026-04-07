package taskstore

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
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
		ID:           "task-1",
		Description:  "Implement login",
		ConfigAlias:  "bugfix",
		ConfigPath:   "/tmp/taskconfigs/bugfix.yaml",
		WorkDir:      "/tmp/project",
		ExecutionDir: "/tmp/project/.muxagent/worktrees/task-1",
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	require.NoError(t, store.CreateTask(ctx, task))

	run := taskdomain.NodeRun{
		ID:            "run-1",
		TaskID:        task.ID,
		NodeName:      "draft_plan",
		Status:        taskdomain.NodeRunAwaitingUser,
		SessionID:     "session-123",
		FailureReason: "interrupted_by_user",
		Result: map[string]interface{}{
			"file_paths": []interface{}{"/tmp/project/.muxagent/tasks/task-1/artifacts/01-draft_plan/plan.md"},
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
	assert.Equal(t, task.ConfigAlias, tasks[0].ConfigAlias)
	assert.Equal(t, task.ConfigPath, tasks[0].ConfigPath)
	assert.Equal(t, task.ExecutionDir, tasks[0].ExecutionDir)

	runs, err := store.ListNodeRunsByTask(ctx, task.ID)
	require.NoError(t, err)
	require.Len(t, runs, 1)
	assert.Equal(t, run.SessionID, runs[0].SessionID)
	assert.Equal(t, run.FailureReason, runs[0].FailureReason)
	assert.Len(t, runs[0].Clarifications, 1)

	cfg, err := taskconfig.LoadDefault()
	require.NoError(t, err)
	view := taskdomain.DeriveTaskView(task, cfg, runs, nil)
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
				"draft_plan",
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
	assert.Contains(t, tableSQL, "failure_reason")
}

func TestOpenAtPathConfiguresSQLitePragmas(t *testing.T) {
	store, err := OpenAtPath(filepath.Join(t.TempDir(), "tasks.db"))
	require.NoError(t, err)
	defer store.Close()

	var journalMode string
	require.NoError(t, store.db.QueryRow(`PRAGMA journal_mode`).Scan(&journalMode))
	assert.Equal(t, "wal", strings.ToLower(journalMode))

	var busyTimeout int
	require.NoError(t, store.db.QueryRow(`PRAGMA busy_timeout`).Scan(&busyTimeout))
	assert.Equal(t, sqliteBusyTimeoutMS, busyTimeout)
}

func TestEnsureSchemaAddsConfigPathColumnForOlderTasksTables(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "tasks.db")
	db, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err)
	defer db.Close()

	_, err = db.Exec(`
		CREATE TABLE tasks (
			id TEXT PRIMARY KEY,
			description TEXT NOT NULL,
			config_alias TEXT NOT NULL DEFAULT '',
			work_dir TEXT NOT NULL,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);`)
	require.NoError(t, err)
	_, err = db.Exec(`
		CREATE TABLE node_runs (
			id TEXT PRIMARY KEY,
			task_id TEXT NOT NULL,
			node_name TEXT NOT NULL,
			status TEXT NOT NULL,
			session_id TEXT,
			result_json TEXT,
			clarifications_json TEXT NOT NULL,
			triggered_by_json TEXT,
			started_at TEXT NOT NULL,
			completed_at TEXT
		);`)
	require.NoError(t, err)

	store := &Store{db: db}
	require.NoError(t, store.EnsureSchema(context.Background()))

	hasConfigPath, err := store.tableHasColumn(context.Background(), "tasks", "config_path")
	require.NoError(t, err)
	assert.True(t, hasConfigPath)
}

func TestEnsureSchemaAddsExecutionDirColumnAndBackfillsOlderRows(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "tasks.db")
	db, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err)
	defer db.Close()

	_, err = db.Exec(`
		CREATE TABLE tasks (
			id TEXT PRIMARY KEY,
			description TEXT NOT NULL,
			config_alias TEXT NOT NULL DEFAULT '',
			config_path TEXT NOT NULL DEFAULT '',
			work_dir TEXT NOT NULL,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);`)
	require.NoError(t, err)
	_, err = db.Exec(`
		INSERT INTO tasks (id, description, config_alias, config_path, work_dir, created_at, updated_at)
		VALUES ('task-1', 'old row', 'default', '/tmp/config.yaml', '/tmp/project', '2026-03-27T10:00:00Z', '2026-03-27T10:00:00Z');`)
	require.NoError(t, err)
	_, err = db.Exec(`
		CREATE TABLE node_runs (
			id TEXT PRIMARY KEY,
			task_id TEXT NOT NULL,
			node_name TEXT NOT NULL,
			status TEXT NOT NULL,
			session_id TEXT,
			result_json TEXT,
			clarifications_json TEXT NOT NULL,
			triggered_by_json TEXT,
			started_at TEXT NOT NULL,
			completed_at TEXT
		);`)
	require.NoError(t, err)

	store := &Store{db: db}
	require.NoError(t, store.EnsureSchema(context.Background()))

	hasExecutionDir, err := store.tableHasColumn(context.Background(), "tasks", "execution_dir")
	require.NoError(t, err)
	assert.True(t, hasExecutionDir)

	task, err := store.GetTask(context.Background(), "task-1")
	require.NoError(t, err)
	assert.Equal(t, "/tmp/project", task.ExecutionDir)
	assert.Equal(t, "/tmp/project", task.ExecutionWorkDir())
}

func TestStoreFollowUpEdgesRoundTripAndAncestors(t *testing.T) {
	ctx := context.Background()
	store, err := OpenAtPath(filepath.Join(t.TempDir(), "tasks.db"))
	require.NoError(t, err)
	defer store.Close()

	now := time.Now().UTC().Round(time.Second)
	tasks := []taskdomain.Task{
		{ID: "parent", Description: "parent", WorkDir: "/tmp/project", CreatedAt: now, UpdatedAt: now},
		{ID: "child-a", Description: "child-a", WorkDir: "/tmp/project", CreatedAt: now, UpdatedAt: now},
		{ID: "child-b", Description: "child-b", WorkDir: "/tmp/project", CreatedAt: now, UpdatedAt: now},
		{ID: "grandchild", Description: "grandchild", WorkDir: "/tmp/project", CreatedAt: now, UpdatedAt: now},
	}
	for _, task := range tasks {
		require.NoError(t, store.CreateTask(ctx, task))
	}

	require.NoError(t, store.AttachFollowUpParent(ctx, "parent", "child-a", now))
	require.NoError(t, store.AttachFollowUpParent(ctx, "parent", "child-b", now.Add(time.Second)))
	require.NoError(t, store.AttachFollowUpParent(ctx, "child-a", "grandchild", now.Add(2*time.Second)))

	parentID, err := store.GetFollowUpParentTaskID(ctx, "child-a")
	require.NoError(t, err)
	assert.Equal(t, "parent", parentID)

	childIDs, err := store.ListChildTaskIDs(ctx, "parent")
	require.NoError(t, err)
	assert.Equal(t, []string{"child-a", "child-b"}, childIDs)

	ancestorIDs, err := store.ListAncestorTaskIDs(ctx, "grandchild")
	require.NoError(t, err)
	assert.Equal(t, []string{"child-a", "parent"}, ancestorIDs)
}

func TestStoreFollowUpEdgesRejectDuplicateParentAndCycles(t *testing.T) {
	ctx := context.Background()
	store, err := OpenAtPath(filepath.Join(t.TempDir(), "tasks.db"))
	require.NoError(t, err)
	defer store.Close()

	now := time.Now().UTC().Round(time.Second)
	for _, taskID := range []string{"a", "b", "c", "d"} {
		require.NoError(t, store.CreateTask(ctx, taskdomain.Task{
			ID:          taskID,
			Description: taskID,
			WorkDir:     "/tmp/project",
			CreatedAt:   now,
			UpdatedAt:   now,
		}))
	}

	require.NoError(t, store.AttachFollowUpParent(ctx, "a", "b", now))
	require.NoError(t, store.AttachFollowUpParent(ctx, "b", "c", now.Add(time.Second)))
	require.NoError(t, store.AttachFollowUpParent(ctx, "a", "d", now.Add(2*time.Second)))

	err = store.AttachFollowUpParent(ctx, "b", "d", now.Add(3*time.Second))
	require.ErrorIs(t, err, ErrFollowUpParentConflict)

	err = store.AttachFollowUpParent(ctx, "c", "a", now.Add(4*time.Second))
	require.ErrorIs(t, err, ErrFollowUpLineageCycle)
}

func TestStoreTaskEdgesRequireExistingTasksWithForeignKeysEnabled(t *testing.T) {
	ctx := context.Background()
	store, err := OpenAtPath(filepath.Join(t.TempDir(), "tasks.db"))
	require.NoError(t, err)
	defer store.Close()

	now := time.Now().UTC().Round(time.Second)
	require.NoError(t, store.CreateTask(ctx, taskdomain.Task{
		ID:          "child",
		Description: "child",
		WorkDir:     "/tmp/project",
		CreatedAt:   now,
		UpdatedAt:   now,
	}))

	err = store.AttachFollowUpParent(ctx, "missing-parent", "child", now)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing task")
}

func TestStoreCreateFollowUpTaskAtomicRollsBackOnLineageError(t *testing.T) {
	ctx := context.Background()
	store, err := OpenAtPath(filepath.Join(t.TempDir(), "tasks.db"))
	require.NoError(t, err)
	defer store.Close()

	now := time.Now().UTC().Round(time.Second)
	childTask := taskdomain.Task{
		ID:          "child",
		Description: "child",
		ConfigAlias: "default",
		ConfigPath:  "/tmp/config.yaml",
		WorkDir:     "/tmp/project",
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	entryRun := taskdomain.NodeRun{
		ID:        "run-child",
		TaskID:    childTask.ID,
		NodeName:  "draft_plan",
		Status:    taskdomain.NodeRunRunning,
		StartedAt: now,
	}

	err = store.CreateFollowUpTaskAtomic(ctx, "missing-parent", childTask, entryRun)
	require.Error(t, err)

	_, err = store.GetTask(ctx, childTask.ID)
	require.ErrorIs(t, err, sql.ErrNoRows)

	_, err = store.GetNodeRun(ctx, entryRun.ID)
	require.ErrorIs(t, err, sql.ErrNoRows)
}

func TestStoreGetFollowUpParentTaskIDRejectsCorruptMultipleParents(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "tasks.db")
	db, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err)
	defer db.Close()

	_, err = db.Exec(`
		CREATE TABLE task_edges (
			parent_task_id TEXT NOT NULL,
			child_task_id TEXT NOT NULL,
			relation_kind TEXT NOT NULL,
			created_at TEXT NOT NULL
		);`)
	require.NoError(t, err)
	_, err = db.Exec(`
		INSERT INTO task_edges (parent_task_id, child_task_id, relation_kind, created_at)
		VALUES
			('parent-a', 'child', 'follow_up', '2026-03-31T10:00:00Z'),
			('parent-b', 'child', 'follow_up', '2026-03-31T10:01:00Z');`)
	require.NoError(t, err)

	store := &Store{db: db}
	parentTaskID, err := store.GetFollowUpParentTaskID(context.Background(), "child")
	require.ErrorIs(t, err, ErrTaskLineageCorrupt)
	assert.Empty(t, parentTaskID)
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

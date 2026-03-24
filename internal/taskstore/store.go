package taskstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/LaLanMo/muxagent-cli/internal/taskdomain"
	_ "modernc.org/sqlite"
)

type Store struct {
	db *sql.DB
}

const (
	tasksTableSQL = `CREATE TABLE IF NOT EXISTS tasks (
		id TEXT PRIMARY KEY,
		description TEXT NOT NULL,
		work_dir TEXT NOT NULL,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	);`
	tasksWorkDirIndexSQL = `CREATE INDEX IF NOT EXISTS tasks_work_dir_updated_idx ON tasks(work_dir, updated_at DESC, created_at DESC);`
	nodeRunsTableSQL     = `CREATE TABLE IF NOT EXISTS node_runs (
		id TEXT PRIMARY KEY,
		task_id TEXT NOT NULL,
		node_name TEXT NOT NULL,
		status TEXT NOT NULL,
		session_id TEXT,
		failure_reason TEXT,
		result_json TEXT CHECK (result_json IS NULL OR json_valid(result_json)),
		clarifications_json TEXT NOT NULL CHECK (json_valid(clarifications_json)),
		triggered_by_json TEXT CHECK (triggered_by_json IS NULL OR json_valid(triggered_by_json)),
		started_at TEXT NOT NULL,
		completed_at TEXT,
		FOREIGN KEY(task_id) REFERENCES tasks(id)
	);`
	nodeRunsIndexSQL = `CREATE INDEX IF NOT EXISTS node_runs_task_started_idx ON node_runs(task_id, started_at, id);`
)

func Open(workDir string) (*Store, error) {
	dbPath := DBPath(workDir)
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}
	store := &Store{db: db}
	if err := store.EnsureSchema(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func OpenAtPath(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	store := &Store{db: db}
	if err := store.EnsureSchema(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *Store) EnsureSchema(ctx context.Context) error {
	stmts := []string{
		tasksTableSQL,
		tasksWorkDirIndexSQL,
		nodeRunsTableSQL,
		nodeRunsIndexSQL,
	}
	for _, stmt := range stmts {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	if err := s.ensureNodeRunsColumns(ctx); err != nil {
		return err
	}
	return nil
}

func (s *Store) CreateTask(ctx context.Context, task taskdomain.Task) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO tasks (id, description, work_dir, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?)`,
		task.ID, task.Description, task.WorkDir, task.CreatedAt.Format(time.RFC3339Nano), task.UpdatedAt.Format(time.RFC3339Nano),
	)
	return err
}

func (s *Store) UpdateTaskTimestamp(ctx context.Context, taskID string, updatedAt time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE tasks SET updated_at = ? WHERE id = ?`, updatedAt.Format(time.RFC3339Nano), taskID)
	return err
}

func (s *Store) GetTask(ctx context.Context, taskID string) (taskdomain.Task, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, description, work_dir, created_at, updated_at FROM tasks WHERE id = ?`, taskID)
	return scanTask(row)
}

func (s *Store) ListTasksByWorkDir(ctx context.Context, workDir string) ([]taskdomain.Task, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, description, work_dir, created_at, updated_at FROM tasks WHERE work_dir = ? ORDER BY updated_at DESC, created_at DESC`, workDir)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tasks []taskdomain.Task
	for rows.Next() {
		task, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, task)
	}
	return tasks, rows.Err()
}

func (s *Store) SaveNodeRun(ctx context.Context, run taskdomain.NodeRun) error {
	resultJSON, err := encodeJSON(run.Result)
	if err != nil {
		return err
	}
	clarificationsJSON, err := json.Marshal(run.Clarifications)
	if err != nil {
		return err
	}
	triggeredByJSON, err := encodeJSON(run.TriggeredBy)
	if err != nil {
		return err
	}
	var completedAt interface{}
	if run.CompletedAt != nil {
		completedAt = run.CompletedAt.Format(time.RFC3339Nano)
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO node_runs (
			id, task_id, node_name, status, session_id, failure_reason, result_json, clarifications_json, triggered_by_json, started_at, completed_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			status = excluded.status,
			session_id = excluded.session_id,
			failure_reason = excluded.failure_reason,
			result_json = excluded.result_json,
			clarifications_json = excluded.clarifications_json,
			triggered_by_json = excluded.triggered_by_json,
			started_at = excluded.started_at,
			completed_at = excluded.completed_at`,
		run.ID,
		run.TaskID,
		run.NodeName,
		string(run.Status),
		nullableString(run.SessionID),
		nullableString(run.FailureReason),
		resultJSON,
		string(clarificationsJSON),
		triggeredByJSON,
		run.StartedAt.Format(time.RFC3339Nano),
		completedAt,
	)
	if err != nil {
		return err
	}
	return s.UpdateTaskTimestamp(ctx, run.TaskID, time.Now().UTC())
}

func (s *Store) GetNodeRun(ctx context.Context, runID string) (taskdomain.NodeRun, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, task_id, node_name, status, session_id, failure_reason, result_json, clarifications_json, triggered_by_json, started_at, completed_at
		FROM node_runs WHERE id = ?`, runID)
	return scanNodeRun(row)
}

func (s *Store) ListNodeRunsByTask(ctx context.Context, taskID string) ([]taskdomain.NodeRun, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, task_id, node_name, status, session_id, failure_reason, result_json, clarifications_json, triggered_by_json, started_at, completed_at
		FROM node_runs
		WHERE task_id = ?
		ORDER BY started_at, id`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var runs []taskdomain.NodeRun
	for rows.Next() {
		run, err := scanNodeRun(rows)
		if err != nil {
			return nil, err
		}
		runs = append(runs, run)
	}
	sort.Slice(runs, func(i, j int) bool {
		if runs[i].StartedAt.Equal(runs[j].StartedAt) {
			return runs[i].ID < runs[j].ID
		}
		return runs[i].StartedAt.Before(runs[j].StartedAt)
	})
	return runs, rows.Err()
}

func (s *Store) ListNodeRunsByStatus(ctx context.Context, status taskdomain.NodeRunStatus) ([]taskdomain.NodeRun, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, task_id, node_name, status, session_id, failure_reason, result_json, clarifications_json, triggered_by_json, started_at, completed_at
		FROM node_runs
		WHERE status = ?
		ORDER BY started_at, id`, string(status))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var runs []taskdomain.NodeRun
	for rows.Next() {
		run, err := scanNodeRun(rows)
		if err != nil {
			return nil, err
		}
		runs = append(runs, run)
	}
	return runs, rows.Err()
}

func scanTask(scanner interface {
	Scan(dest ...interface{}) error
}) (taskdomain.Task, error) {
	var (
		task                 taskdomain.Task
		createdAt, updatedAt string
	)
	err := scanner.Scan(&task.ID, &task.Description, &task.WorkDir, &createdAt, &updatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return taskdomain.Task{}, err
		}
		return taskdomain.Task{}, err
	}
	task.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return taskdomain.Task{}, err
	}
	task.UpdatedAt, err = time.Parse(time.RFC3339Nano, updatedAt)
	if err != nil {
		return taskdomain.Task{}, err
	}
	return task, nil
}

func scanNodeRun(scanner interface {
	Scan(dest ...interface{}) error
}) (taskdomain.NodeRun, error) {
	var (
		run                                taskdomain.NodeRun
		status                             string
		sessionID, failureReason           sql.NullString
		resultJSON, clarificationsJSON     sql.NullString
		triggeredByJSON, completedAtString sql.NullString
		startedAtString                    string
	)
	if err := scanner.Scan(
		&run.ID,
		&run.TaskID,
		&run.NodeName,
		&status,
		&sessionID,
		&failureReason,
		&resultJSON,
		&clarificationsJSON,
		&triggeredByJSON,
		&startedAtString,
		&completedAtString,
	); err != nil {
		return taskdomain.NodeRun{}, err
	}
	run.Status = taskdomain.NodeRunStatus(status)
	run.SessionID = sessionID.String
	run.FailureReason = failureReason.String
	var err error
	run.StartedAt, err = time.Parse(time.RFC3339Nano, startedAtString)
	if err != nil {
		return taskdomain.NodeRun{}, err
	}
	if completedAtString.Valid {
		ts, err := time.Parse(time.RFC3339Nano, completedAtString.String)
		if err != nil {
			return taskdomain.NodeRun{}, err
		}
		run.CompletedAt = &ts
	}
	if resultJSON.Valid && resultJSON.String != "" {
		if err := json.Unmarshal([]byte(resultJSON.String), &run.Result); err != nil {
			return taskdomain.NodeRun{}, fmt.Errorf("decode result_json: %w", err)
		}
	}
	if clarificationsJSON.Valid && clarificationsJSON.String != "" {
		if err := json.Unmarshal([]byte(clarificationsJSON.String), &run.Clarifications); err != nil {
			return taskdomain.NodeRun{}, fmt.Errorf("decode clarifications_json: %w", err)
		}
	}
	if triggeredByJSON.Valid && triggeredByJSON.String != "" {
		var triggeredBy taskdomain.TriggeredBy
		if err := json.Unmarshal([]byte(triggeredByJSON.String), &triggeredBy); err != nil {
			return taskdomain.NodeRun{}, fmt.Errorf("decode triggered_by_json: %w", err)
		}
		run.TriggeredBy = &triggeredBy
	}
	return run, nil
}

func encodeJSON(value interface{}) (interface{}, error) {
	if value == nil {
		return nil, nil
	}
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return string(data), nil
}

func nullableString(value string) interface{} {
	if value == "" {
		return nil
	}
	return value
}

func (s *Store) ensureNodeRunsColumns(ctx context.Context) error {
	hasFailureReason, err := s.tableHasColumn(ctx, "node_runs", "failure_reason")
	if err != nil {
		return err
	}
	if hasFailureReason {
		return nil
	}
	_, err = s.db.ExecContext(ctx, `ALTER TABLE node_runs ADD COLUMN failure_reason TEXT`)
	return err
}

func (s *Store) tableHasColumn(ctx context.Context, tableName, columnName string) (bool, error) {
	rows, err := s.db.QueryContext(ctx, `PRAGMA table_info(`+tableName+`)`)
	if err != nil {
		return false, err
	}
	defer rows.Close()

	for rows.Next() {
		var (
			cid        int
			name       string
			typ        string
			notNull    int
			defaultVal sql.NullString
			pk         int
		)
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultVal, &pk); err != nil {
			return false, err
		}
		if name == columnName {
			return true, nil
		}
	}
	return false, rows.Err()
}

func DBPath(workDir string) string {
	return filepath.Join(NormalizeWorkDir(workDir), ".muxagent", "tasks.db")
}

func TaskDir(workDir, taskID string) string {
	return filepath.Join(NormalizeWorkDir(workDir), ".muxagent", "tasks", taskID)
}

func ConfigPath(workDir, taskID string) string {
	return filepath.Join(TaskDir(workDir, taskID), "config.yaml")
}

func SchemaPath(workDir, taskID, nodeName string) string {
	return filepath.Join(TaskDir(workDir, taskID), "schemas", nodeName+".json")
}

func ArtifactRunDir(workDir, taskID string, sequence int, nodeName string) string {
	return filepath.Join(TaskDir(workDir, taskID), "artifacts", fmt.Sprintf("%02d-%s", sequence, nodeName))
}

func NormalizeWorkDir(workDir string) string {
	if workDir == "" {
		return ""
	}
	cleaned := filepath.Clean(workDir)
	if abs, err := filepath.Abs(cleaned); err == nil {
		cleaned = abs
	}
	if resolved, err := filepath.EvalSymlinks(cleaned); err == nil {
		return resolved
	}
	return cleaned
}

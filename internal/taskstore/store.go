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
	"strings"
	"time"

	"github.com/LaLanMo/muxagent-cli/internal/taskdomain"
	_ "modernc.org/sqlite"
)

type Store struct {
	db *sql.DB
}

type sqlExecutor interface {
	ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row
}

var (
	ErrTaskLineageCorrupt     = errors.New("task lineage is corrupt")
	ErrFollowUpParentConflict = errors.New("follow-up task already has a parent")
	ErrFollowUpLineageCycle   = errors.New("follow-up lineage cycle detected")
)

const (
	sqliteBusyTimeoutMS = 5000
	tasksTableSQL       = `CREATE TABLE IF NOT EXISTS tasks (
		id TEXT PRIMARY KEY,
		description TEXT NOT NULL,
		config_alias TEXT NOT NULL DEFAULT '',
		config_path TEXT NOT NULL DEFAULT '',
		work_dir TEXT NOT NULL,
		execution_dir TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	);`
	tasksWorkDirIndexSQL = `CREATE INDEX IF NOT EXISTS tasks_work_dir_updated_idx ON tasks(work_dir, updated_at DESC, created_at DESC);`
	taskEdgesTableSQL    = `CREATE TABLE IF NOT EXISTS task_edges (
		parent_task_id TEXT NOT NULL,
		child_task_id TEXT NOT NULL,
		relation_kind TEXT NOT NULL CHECK (relation_kind = 'follow_up'),
		created_at TEXT NOT NULL,
		CHECK(parent_task_id <> child_task_id),
		UNIQUE(child_task_id),
		FOREIGN KEY(parent_task_id) REFERENCES tasks(id),
		FOREIGN KEY(child_task_id) REFERENCES tasks(id)
	);`
	taskEdgesParentIndexSQL = `CREATE INDEX IF NOT EXISTS task_edges_parent_idx ON task_edges(parent_task_id);`
	nodeRunsTableSQL        = `CREATE TABLE IF NOT EXISTS node_runs (
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
	return openSQLiteStore(dbPath)
}

func OpenAtPath(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	return openSQLiteStore(path)
}

func openSQLiteStore(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if err := configureSQLite(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	store := &Store{db: db}
	if err := store.EnsureSchema(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func configureSQLite(db *sql.DB) error {
	if db == nil {
		return errors.New("sqlite db is nil")
	}
	ctx := context.Background()
	pragmas := []string{
		`PRAGMA journal_mode = WAL`,
		`PRAGMA synchronous = NORMAL`,
		fmt.Sprintf(`PRAGMA busy_timeout = %d`, sqliteBusyTimeoutMS),
		`PRAGMA foreign_keys = ON`,
	}
	for _, stmt := range pragmas {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *Store) EnsureSchema(ctx context.Context) error {
	if err := s.enableForeignKeys(ctx); err != nil {
		return err
	}
	return s.withTx(ctx, func(tx *sql.Tx) error {
		return s.ensureSchema(ctx, tx)
	})
}

func (s *Store) ensureSchema(ctx context.Context, exec sqlExecutor) error {
	stmts := []string{
		tasksTableSQL,
		tasksWorkDirIndexSQL,
		taskEdgesTableSQL,
		taskEdgesParentIndexSQL,
		nodeRunsTableSQL,
		nodeRunsIndexSQL,
	}
	for _, stmt := range stmts {
		if _, err := exec.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	if err := s.ensureTaskColumns(ctx, exec); err != nil {
		return err
	}
	if err := s.ensureNodeRunsColumns(ctx, exec); err != nil {
		return err
	}
	return nil
}

func (s *Store) enableForeignKeys(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `PRAGMA foreign_keys = ON`)
	return err
}

func (s *Store) withTx(ctx context.Context, fn func(tx *sql.Tx) error) (err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
			return
		}
		err = tx.Commit()
	}()
	err = fn(tx)
	return err
}

func (s *Store) CreateTask(ctx context.Context, task taskdomain.Task) error {
	return s.createTask(ctx, s.db, task)
}

func (s *Store) createTask(ctx context.Context, exec sqlExecutor, task taskdomain.Task) error {
	_, err := exec.ExecContext(ctx, `
		INSERT INTO tasks (id, description, config_alias, config_path, work_dir, execution_dir, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		task.ID, task.Description, task.ConfigAlias, task.ConfigPath, task.WorkDir, task.ExecutionWorkDir(), task.CreatedAt.Format(time.RFC3339Nano), task.UpdatedAt.Format(time.RFC3339Nano),
	)
	return err
}

func (s *Store) UpdateTaskTimestamp(ctx context.Context, taskID string, updatedAt time.Time) error {
	return s.updateTaskTimestamp(ctx, s.db, taskID, updatedAt)
}

func (s *Store) updateTaskTimestamp(ctx context.Context, exec sqlExecutor, taskID string, updatedAt time.Time) error {
	_, err := exec.ExecContext(ctx, `UPDATE tasks SET updated_at = ? WHERE id = ?`, updatedAt.Format(time.RFC3339Nano), taskID)
	return err
}

func (s *Store) GetTask(ctx context.Context, taskID string) (taskdomain.Task, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, description, config_alias, config_path, work_dir, execution_dir, created_at, updated_at FROM tasks WHERE id = ?`, taskID)
	return scanTask(row)
}

func (s *Store) ListTasksByWorkDir(ctx context.Context, workDir string) ([]taskdomain.Task, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, description, config_alias, config_path, work_dir, execution_dir, created_at, updated_at FROM tasks WHERE work_dir = ? ORDER BY updated_at DESC, created_at DESC`, workDir)
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

func (s *Store) CreateTaskWithEntryRun(ctx context.Context, task taskdomain.Task, entryRun taskdomain.NodeRun) error {
	return s.withTx(ctx, func(tx *sql.Tx) error {
		if err := s.createTask(ctx, tx, task); err != nil {
			return err
		}
		return s.saveNodeRun(ctx, tx, entryRun, task.UpdatedAt)
	})
}

func (s *Store) CreateFollowUpTaskAtomic(ctx context.Context, parentTaskID string, task taskdomain.Task, entryRun taskdomain.NodeRun) error {
	return s.withTx(ctx, func(tx *sql.Tx) error {
		if err := s.createTask(ctx, tx, task); err != nil {
			return err
		}
		if err := s.attachFollowUpParent(ctx, tx, parentTaskID, task.ID, task.CreatedAt); err != nil {
			return err
		}
		return s.saveNodeRun(ctx, tx, entryRun, task.UpdatedAt)
	})
}

func (s *Store) SaveNodeRun(ctx context.Context, run taskdomain.NodeRun) error {
	return s.saveNodeRun(ctx, s.db, run, time.Now().UTC())
}

func (s *Store) saveNodeRun(ctx context.Context, exec sqlExecutor, run taskdomain.NodeRun, updatedAt time.Time) error {
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
	_, err = exec.ExecContext(ctx, `
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
	return s.updateTaskTimestamp(ctx, exec, run.TaskID, updatedAt)
}

func (s *Store) AttachFollowUpParent(ctx context.Context, parentTaskID, childTaskID string, createdAt time.Time) error {
	return s.attachFollowUpParent(ctx, s.db, parentTaskID, childTaskID, createdAt)
}

func (s *Store) attachFollowUpParent(ctx context.Context, exec sqlExecutor, parentTaskID, childTaskID string, createdAt time.Time) error {
	parentTaskID = strings.TrimSpace(parentTaskID)
	childTaskID = strings.TrimSpace(childTaskID)
	if parentTaskID == "" || childTaskID == "" {
		return fmt.Errorf("parent and child task ids are required")
	}
	if parentTaskID == childTaskID {
		return ErrFollowUpLineageCycle
	}
	hasCycle, err := s.pathCreatesCycle(ctx, exec, parentTaskID, childTaskID)
	if err != nil {
		return err
	}
	if hasCycle {
		return ErrFollowUpLineageCycle
	}
	_, err = exec.ExecContext(ctx, `
		INSERT INTO task_edges (parent_task_id, child_task_id, relation_kind, created_at)
		VALUES (?, ?, ?, ?)`,
		parentTaskID, childTaskID, taskdomain.TaskRelationFollowUp, createdAt.Format(time.RFC3339Nano),
	)
	if err != nil {
		return mapTaskEdgeError(err)
	}
	return nil
}

func (s *Store) GetFollowUpParentTaskID(ctx context.Context, childTaskID string) (string, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT parent_task_id
		FROM task_edges
		WHERE child_task_id = ? AND relation_kind = ?
		ORDER BY created_at, parent_task_id`,
		childTaskID, taskdomain.TaskRelationFollowUp,
	)
	if err != nil {
		return "", err
	}
	defer rows.Close()

	var parents []string
	for rows.Next() {
		var parentTaskID string
		if err := rows.Scan(&parentTaskID); err != nil {
			return "", err
		}
		parents = append(parents, parentTaskID)
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	switch len(parents) {
	case 0:
		return "", nil
	case 1:
		return parents[0], nil
	default:
		return "", ErrTaskLineageCorrupt
	}
}

func (s *Store) ListChildTaskIDs(ctx context.Context, parentTaskID string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT child_task_id
		FROM task_edges
		WHERE parent_task_id = ? AND relation_kind = ?
		ORDER BY created_at, child_task_id`,
		parentTaskID, taskdomain.TaskRelationFollowUp,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var childTaskIDs []string
	for rows.Next() {
		var childTaskID string
		if err := rows.Scan(&childTaskID); err != nil {
			return nil, err
		}
		childTaskIDs = append(childTaskIDs, childTaskID)
	}
	return childTaskIDs, rows.Err()
}

func (s *Store) ListAncestorTaskIDs(ctx context.Context, taskID string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `
		WITH RECURSIVE lineage(task_id, depth, path, cycle) AS (
			SELECT parent_task_id, 1, ',' || child_task_id || ',' || parent_task_id || ',', 0
			FROM task_edges
			WHERE child_task_id = ? AND relation_kind = ?
			UNION ALL
			SELECT te.parent_task_id,
			       lineage.depth + 1,
			       lineage.path || te.parent_task_id || ',',
			       CASE WHEN instr(lineage.path, ',' || te.parent_task_id || ',') > 0 THEN 1 ELSE 0 END
			FROM task_edges te
			JOIN lineage ON te.child_task_id = lineage.task_id
			WHERE te.relation_kind = ? AND lineage.cycle = 0
		)
		SELECT task_id, cycle
		FROM lineage
		ORDER BY depth ASC, task_id ASC`,
		taskID, taskdomain.TaskRelationFollowUp, taskdomain.TaskRelationFollowUp,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ancestorTaskIDs []string
	for rows.Next() {
		var (
			ancestorTaskID string
			cycle          int
		)
		if err := rows.Scan(&ancestorTaskID, &cycle); err != nil {
			return nil, err
		}
		if cycle != 0 {
			return nil, ErrTaskLineageCorrupt
		}
		ancestorTaskIDs = append(ancestorTaskIDs, ancestorTaskID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return ancestorTaskIDs, nil
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
		executionDir         string
		createdAt, updatedAt string
	)
	err := scanner.Scan(&task.ID, &task.Description, &task.ConfigAlias, &task.ConfigPath, &task.WorkDir, &executionDir, &createdAt, &updatedAt)
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
	task.ExecutionDir = executionDir
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

func (s *Store) ensureTaskColumns(ctx context.Context, exec sqlExecutor) error {
	hasConfigAlias, err := s.tableHasColumnWithExecutor(ctx, exec, "tasks", "config_alias")
	if err != nil {
		return err
	}
	if !hasConfigAlias {
		if _, err := exec.ExecContext(ctx, `ALTER TABLE tasks ADD COLUMN config_alias TEXT NOT NULL DEFAULT ''`); err != nil {
			return err
		}
	}
	hasConfigPath, err := s.tableHasColumnWithExecutor(ctx, exec, "tasks", "config_path")
	if err != nil {
		return err
	}
	if !hasConfigPath {
		if _, err := exec.ExecContext(ctx, `ALTER TABLE tasks ADD COLUMN config_path TEXT NOT NULL DEFAULT ''`); err != nil {
			return err
		}
	}
	hasExecutionDir, err := s.tableHasColumnWithExecutor(ctx, exec, "tasks", "execution_dir")
	if err != nil {
		return err
	}
	if !hasExecutionDir {
		if _, err := exec.ExecContext(ctx, `ALTER TABLE tasks ADD COLUMN execution_dir TEXT NOT NULL DEFAULT ''`); err != nil {
			return err
		}
	}
	_, err = exec.ExecContext(ctx, `UPDATE tasks SET execution_dir = work_dir WHERE execution_dir = ''`)
	return err
}

func (s *Store) ensureNodeRunsColumns(ctx context.Context, exec sqlExecutor) error {
	hasFailureReason, err := s.tableHasColumnWithExecutor(ctx, exec, "node_runs", "failure_reason")
	if err != nil {
		return err
	}
	if hasFailureReason {
		return nil
	}
	_, err = exec.ExecContext(ctx, `ALTER TABLE node_runs ADD COLUMN failure_reason TEXT`)
	return err
}

func (s *Store) tableHasColumn(ctx context.Context, tableName, columnName string) (bool, error) {
	return s.tableHasColumnWithExecutor(ctx, s.db, tableName, columnName)
}

func (s *Store) tableHasColumnWithExecutor(ctx context.Context, exec sqlExecutor, tableName, columnName string) (bool, error) {
	rows, err := exec.QueryContext(ctx, `PRAGMA table_info(`+tableName+`)`)
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

func (s *Store) pathCreatesCycle(ctx context.Context, exec sqlExecutor, parentTaskID, childTaskID string) (bool, error) {
	rows, err := exec.QueryContext(ctx, `
		WITH RECURSIVE lineage(task_id, path) AS (
			SELECT ?, ',' || ? || ','
			UNION ALL
			SELECT te.parent_task_id,
			       lineage.path || te.parent_task_id || ','
			FROM task_edges te
			JOIN lineage ON te.child_task_id = lineage.task_id
			WHERE te.relation_kind = ? AND instr(lineage.path, ',' || te.parent_task_id || ',') = 0
		)
		SELECT task_id
		FROM lineage`,
		parentTaskID, parentTaskID, taskdomain.TaskRelationFollowUp,
	)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var taskID string
		if err := rows.Scan(&taskID); err != nil {
			return false, err
		}
		if taskID == childTaskID {
			return true, nil
		}
	}
	return false, rows.Err()
}

func mapTaskEdgeError(err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, "UNIQUE constraint failed: task_edges.child_task_id"):
		return ErrFollowUpParentConflict
	case strings.Contains(msg, "FOREIGN KEY constraint failed"):
		return fmt.Errorf("follow-up lineage references a missing task: %w", err)
	default:
		return err
	}
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

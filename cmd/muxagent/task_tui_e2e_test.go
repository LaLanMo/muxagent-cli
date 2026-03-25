//go:build !windows

package main

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	appconfig "github.com/LaLanMo/muxagent-cli/internal/config"
	"github.com/LaLanMo/muxagent-cli/internal/taskconfig"
	"github.com/LaLanMo/muxagent-cli/internal/taskdomain"
	"github.com/LaLanMo/muxagent-cli/internal/taskstore"
	"github.com/charmbracelet/x/ansi"
	"github.com/creack/pty"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTaskTUIEndToEndScenarios(t *testing.T) {
	moduleRoot := moduleRoot(t)
	binaryPath := buildMuxagentBinary(t, moduleRoot)
	fakeCodexFixture := filepath.Join(moduleRoot, "cmd", "muxagent", "testdata", "fake-codex.sh")
	fakeClaudeFixture := filepath.Join(moduleRoot, "cmd", "muxagent", "testdata", "fake-claude.sh")
	basePath := os.Getenv("PATH")

	tests := []struct {
		name              string
		flow              string
		description       string
		cliArgs           []string
		drive             func(t *testing.T, session *tuiSession)
		expectedArtifacts []string
		verify            func(t *testing.T, task taskdomain.Task, runs []taskdomain.NodeRun, view taskdomain.TaskView)
	}{
		{
			name:        "happy path",
			flow:        "happy",
			description: "Implement login",
			drive: func(t *testing.T, session *tuiSession) {
				session.waitForAll(t, 10*time.Second, "No tasks in this working directory yet.", "Ctrl+N new task")
				session.send(t, "\x0e")
				session.waitForAll(t, 5*time.Second, "New Task", "Describe your task")
				session.send(t, "Implement login\r")
				session.waitForAll(t, 10*time.Second, "approve_plan", "awaiting approval")
				session.confirm(t)
			},
			expectedArtifacts: []string{"01-upsert_plan", "02-review_plan", "03-approve_plan", "04-implement", "05-verify"},
			verify: func(t *testing.T, task taskdomain.Task, runs []taskdomain.NodeRun, view taskdomain.TaskView) {
				require.Len(t, runs, 6)
				assert.Equal(t, taskdomain.TaskStatusDone, view.Status)
				assert.Equal(t, "done", view.CurrentNodeName)
				assertNodeRunCounts(t, runs, map[string]int{
					"upsert_plan":  1,
					"review_plan":  1,
					"approve_plan": 1,
					"implement":    1,
					"verify":       1,
					"done":         1,
				})
				for _, run := range runs {
					assert.Equal(t, task.ID, run.TaskID)
					assert.NotEmpty(t, run.ID)
					switch run.NodeName {
					case "upsert_plan", "review_plan", "implement", "verify":
						assert.Equal(t, taskdomain.NodeRunDone, run.Status)
						assert.NotEmpty(t, run.SessionID)
					case "approve_plan":
						assert.Equal(t, taskdomain.NodeRunDone, run.Status)
						assert.Empty(t, run.SessionID)
						assert.Equal(t, true, run.Result["approved"])
						assertHumanAuditArtifact(t, run)
					}
				}
			},
		},
		{
			name:        "approval rejection loops back",
			flow:        "happy",
			description: "Reject once",
			drive: func(t *testing.T, session *tuiSession) {
				session.waitForAll(t, 10*time.Second, "No tasks in this working directory yet.", "Ctrl+N new task")
				session.send(t, "\x0e")
				session.waitForAll(t, 5*time.Second, "New Task", "Describe your task")
				session.send(t, "Reject once\r")
				session.waitForAll(t, 10*time.Second, "approve_plan", "awaiting approval")
				session.send(t, "\x1b[B")
				session.pause(300 * time.Millisecond)
				session.send(t, "Need more detail")
				session.pause(750 * time.Millisecond)
				session.confirm(t)
				waitForNodeRunCounts(t, session.cmd.Dir, map[string]int{
					"upsert_plan":  2,
					"review_plan":  2,
					"approve_plan": 2,
				})
				session.waitForAll(t, 10*time.Second, "approve_plan", "awaiting approval")
				session.confirm(t)
			},
			expectedArtifacts: []string{"01-upsert_plan", "02-review_plan", "03-approve_plan", "04-upsert_plan", "05-review_plan", "06-approve_plan", "07-implement", "08-verify"},
			verify: func(t *testing.T, task taskdomain.Task, runs []taskdomain.NodeRun, view taskdomain.TaskView) {
				require.Len(t, runs, 9)
				assert.Equal(t, taskdomain.TaskStatusDone, view.Status)
				assertNodeRunCounts(t, runs, map[string]int{
					"upsert_plan":  2,
					"review_plan":  2,
					"approve_plan": 2,
					"implement":    1,
					"verify":       1,
					"done":         1,
				})
				rejections := 0
				approvals := 0
				for _, run := range runs {
					if run.NodeName != "approve_plan" {
						continue
					}
					if approved, _ := run.Result["approved"].(bool); approved {
						approvals++
					} else {
						rejections++
						assert.Equal(t, "Need more detail", run.Result["feedback"])
					}
					assertHumanAuditArtifact(t, run)
				}
				assert.Equal(t, 1, rejections)
				assert.Equal(t, 1, approvals)
			},
		},
		{
			name:        "clarification reuses the same node run",
			flow:        "clarify-once",
			description: "Need clarification",
			drive: func(t *testing.T, session *tuiSession) {
				session.waitForAll(t, 10*time.Second, "No tasks in this working directory yet.", "Ctrl+N new task")
				session.send(t, "\x0e")
				session.waitForAll(t, 5*time.Second, "New Task", "Describe your task")
				session.send(t, "Need clarification\r")
				session.waitForAll(t, 10*time.Second, "upsert_plan", "awaiting input")
				session.confirm(t)
				session.waitForAll(t, 10*time.Second, "approve_plan", "awaiting approval")
				session.confirm(t)
			},
			expectedArtifacts: []string{"01-upsert_plan", "02-review_plan", "03-approve_plan", "04-implement", "05-verify"},
			verify: func(t *testing.T, task taskdomain.Task, runs []taskdomain.NodeRun, view taskdomain.TaskView) {
				require.Len(t, runs, 6)
				assert.Equal(t, taskdomain.TaskStatusDone, view.Status)
				assertNodeRunCounts(t, runs, map[string]int{
					"upsert_plan":  1,
					"review_plan":  1,
					"approve_plan": 1,
					"implement":    1,
					"verify":       1,
					"done":         1,
				})
				for _, run := range runs {
					if run.NodeName == "upsert_plan" {
						require.Len(t, run.Clarifications, 1)
						require.NotNil(t, run.Clarifications[0].Response)
						assert.Equal(t, "A", run.Clarifications[0].Response.Answers[0].Selected)
						assert.Equal(t, "thread-upsert_plan-1", run.SessionID)
					}
					if run.NodeName == "approve_plan" {
						assertHumanAuditArtifact(t, run)
					}
					assert.Equal(t, task.ID, run.TaskID)
				}
			},
		},
		{
			name:        "review rejection loops back",
			flow:        "review-reject-once",
			description: "Review rejects once",
			drive: func(t *testing.T, session *tuiSession) {
				session.waitForAll(t, 10*time.Second, "No tasks in this working directory yet.", "Ctrl+N new task")
				session.send(t, "\x0e")
				session.waitForAll(t, 5*time.Second, "New Task", "Describe your task")
				session.send(t, "Review rejects once\r")
				session.waitForAll(t, 10*time.Second, "approve_plan", "awaiting approval")
				session.confirm(t)
			},
			expectedArtifacts: []string{"01-upsert_plan", "02-review_plan", "03-upsert_plan", "04-review_plan", "05-approve_plan", "06-implement", "07-verify"},
			verify: func(t *testing.T, task taskdomain.Task, runs []taskdomain.NodeRun, view taskdomain.TaskView) {
				require.Len(t, runs, 8)
				assert.Equal(t, taskdomain.TaskStatusDone, view.Status)
				assertNodeRunCounts(t, runs, map[string]int{
					"upsert_plan":  2,
					"review_plan":  2,
					"approve_plan": 1,
					"implement":    1,
					"verify":       1,
					"done":         1,
				})
				reviewPasses := 0
				reviewFailures := 0
				for _, run := range runs {
					if run.NodeName != "review_plan" {
						if run.NodeName == "approve_plan" {
							assertHumanAuditArtifact(t, run)
						}
						continue
					}
					if passed, _ := run.Result["passed"].(bool); passed {
						reviewPasses++
					} else {
						reviewFailures++
					}
				}
				assert.Equal(t, 1, reviewPasses)
				assert.Equal(t, 1, reviewFailures)
				assert.Equal(t, task.ID, runs[0].TaskID)
			},
		},
		{
			name:        "failed agent node can be retried from the footer",
			flow:        "implement-fail-once",
			description: "Retry failed implement",
			drive: func(t *testing.T, session *tuiSession) {
				session.waitForAll(t, 10*time.Second, "No tasks in this working directory yet.", "Ctrl+N new task")
				session.send(t, "\x0e")
				session.waitForAll(t, 5*time.Second, "New Task", "Describe your task")
				session.send(t, "Retry failed implement\r")
				session.waitForAll(t, 10*time.Second, "approve_plan", "awaiting approval")
				session.confirm(t)
				session.waitForAll(t, 10*time.Second, "Task failed", "r retry step")
				session.send(t, "r")
			},
			expectedArtifacts: []string{"01-upsert_plan", "02-review_plan", "03-approve_plan", "04-implement", "05-implement", "06-verify"},
			verify: func(t *testing.T, task taskdomain.Task, runs []taskdomain.NodeRun, view taskdomain.TaskView) {
				require.Len(t, runs, 7)
				assert.Equal(t, taskdomain.TaskStatusDone, view.Status)
				assertNodeRunCounts(t, runs, map[string]int{
					"upsert_plan":  1,
					"review_plan":  1,
					"approve_plan": 1,
					"implement":    2,
					"verify":       1,
					"done":         1,
				})

				var failedImplement, retriedImplement *taskdomain.NodeRun
				for i := range runs {
					run := &runs[i]
					if run.NodeName != "implement" {
						continue
					}
					if run.Status == taskdomain.NodeRunFailed {
						failedImplement = run
					}
					if run.Status == taskdomain.NodeRunDone {
						retriedImplement = run
					}
				}
				require.NotNil(t, failedImplement)
				require.NotNil(t, retriedImplement)
				require.NotNil(t, retriedImplement.TriggeredBy)
				assert.Equal(t, taskdomain.TriggerReasonManualRetry, retriedImplement.TriggeredBy.Reason)
				assert.Equal(t, failedImplement.ID, retriedImplement.TriggeredBy.NodeRunID)
			},
		},
		{
			name:        "claude runtime persists and clarification resumes",
			flow:        "clarify-once",
			description: "Need clarification with Claude",
			cliArgs:     []string{"--runtime", "claude-code"},
			drive: func(t *testing.T, session *tuiSession) {
				session.waitForAll(t, 10*time.Second, "No tasks in this working directory yet.", "Ctrl+N new task")
				session.send(t, "\x0e")
				session.waitForAll(t, 5*time.Second, "New Task", "runtime claude-code")
				session.send(t, "Need clarification with Claude\r")
				session.waitForAll(t, 10*time.Second, "upsert_plan", "awaiting input")
				session.confirm(t)
				session.waitForAll(t, 10*time.Second, "approve_plan", "awaiting approval")
				session.confirm(t)
			},
			expectedArtifacts: []string{"01-upsert_plan", "02-review_plan", "03-approve_plan", "04-implement", "05-verify"},
			verify: func(t *testing.T, task taskdomain.Task, runs []taskdomain.NodeRun, view taskdomain.TaskView) {
				require.Len(t, runs, 6)
				assert.Equal(t, taskdomain.TaskStatusDone, view.Status)
				cfg, err := taskconfig.Load(taskstore.ConfigPath(task.WorkDir, task.ID))
				require.NoError(t, err)
				assert.Equal(t, appconfig.RuntimeClaudeCode, cfg.Runtime)
				for _, run := range runs {
					if run.NodeName == "upsert_plan" {
						require.Len(t, run.Clarifications, 1)
						require.NotNil(t, run.Clarifications[0].Response)
						assert.Equal(t, run.ID, run.SessionID)
					}
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			workDir := canonicalPath(t, t.TempDir())
			homeDir := t.TempDir()
			fakeDir := t.TempDir()
			fakeCodexPath := filepath.Join(fakeDir, "codex")
			fakeClaudePath := filepath.Join(fakeDir, "claude")
			copyExecutable(t, fakeCodexFixture, fakeCodexPath)
			copyExecutable(t, fakeClaudeFixture, fakeClaudePath)

			t.Setenv("HOME", homeDir)
			t.Setenv("PATH", fakeDir+string(os.PathListSeparator)+basePath)
			t.Setenv("FAKE_CODEX_FLOW", tt.flow)
			t.Setenv("FAKE_CODEX_STATE_DIR", filepath.Join(workDir, ".fake-codex-state"))
			t.Setenv("FAKE_CLAUDE_FLOW", tt.flow)
			t.Setenv("FAKE_CLAUDE_STATE_DIR", filepath.Join(workDir, ".fake-claude-state"))
			t.Setenv("TERM", "xterm-256color")

			session := startTUISession(t, binaryPath, workDir, tt.cliArgs...)
			tt.drive(t, session)
			task, runs, view := waitForPersistedTask(t, workDir, taskdomain.TaskStatusDone)

			assert.Equal(t, tt.description, task.Description)
			assert.Equal(t, workDir, task.WorkDir)
			assert.FileExists(t, taskstore.DBPath(workDir))
			assert.FileExists(t, taskstore.ConfigPath(workDir, task.ID))
			assertArtifactDirs(t, task, tt.expectedArtifacts)
			assertPromptFiles(t, workDir, task.ID)
			tt.verify(t, task, runs, view)

			session.quit(t)
		})
	}
}

type tuiSession struct {
	t      *testing.T
	cmd    *exec.Cmd
	ptmx   *os.File
	exitCh chan error

	bufferMu sync.Mutex
	buffer   strings.Builder
}

func startTUISession(t *testing.T, binaryPath, workDir string, args ...string) *tuiSession {
	t.Helper()

	cmd := exec.Command(binaryPath, args...)
	cmd.Dir = workDir
	cmd.Env = os.Environ()

	ptmx, err := pty.Start(cmd)
	require.NoError(t, err)
	require.NoError(t, pty.Setsize(ptmx, &pty.Winsize{Rows: 40, Cols: 140}))

	session := &tuiSession{
		t:      t,
		cmd:    cmd,
		ptmx:   ptmx,
		exitCh: make(chan error, 1),
	}
	go func() {
		_, _ = io.Copy(session, ptmx)
	}()
	go func() {
		session.exitCh <- cmd.Wait()
	}()
	t.Cleanup(func() {
		session.forceClose()
	})
	return session
}

func (s *tuiSession) Write(p []byte) (int, error) {
	s.bufferMu.Lock()
	defer s.bufferMu.Unlock()
	return s.buffer.Write(p)
}

func (s *tuiSession) send(t *testing.T, input string) {
	t.Helper()
	_, err := s.ptmx.Write([]byte(input))
	require.NoError(t, err)
}

func (s *tuiSession) pause(delay time.Duration) {
	time.Sleep(delay)
}

func (s *tuiSession) confirm(t *testing.T) {
	t.Helper()
	s.send(t, "\r")
	time.Sleep(250 * time.Millisecond)
	s.send(t, "\r")
}

func (s *tuiSession) waitForAll(t *testing.T, timeout time.Duration, needles ...string) string {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		output := s.output()
		allFound := true
		for _, needle := range needles {
			if !strings.Contains(output, needle) {
				allFound = false
				break
			}
		}
		if allFound {
			return output
		}
		select {
		case err := <-s.exitCh:
			require.NoErrorf(t, err, "muxagent exited before screen stabilized\n%s", output)
		default:
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %q\n%s", strings.Join(needles, ", "), s.output())
	return ""
}

func (s *tuiSession) output() string {
	s.bufferMu.Lock()
	defer s.bufferMu.Unlock()
	clean := ansi.Strip(s.buffer.String())
	clean = strings.ReplaceAll(clean, "\r", "\n")
	clean = strings.ReplaceAll(clean, "\x00", "")
	return clean
}

func (s *tuiSession) quit(t *testing.T) {
	t.Helper()
	if s.cmd.ProcessState != nil && s.cmd.ProcessState.Exited() {
		return
	}
	s.send(t, "\x03")
	select {
	case err := <-s.exitCh:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatalf("muxagent did not exit after Ctrl+C\n%s", s.output())
	}
	require.NoError(t, s.ptmx.Close())
}

func (s *tuiSession) forceClose() {
	if s.ptmx != nil {
		_ = s.ptmx.Close()
	}
	if s.cmd != nil && s.cmd.Process != nil && (s.cmd.ProcessState == nil || !s.cmd.ProcessState.Exited()) {
		_ = s.cmd.Process.Kill()
		select {
		case <-s.exitCh:
		case <-time.After(2 * time.Second):
		}
	}
}

func buildMuxagentBinary(t *testing.T, moduleRoot string) string {
	t.Helper()
	binaryPath := filepath.Join(t.TempDir(), "muxagent")
	cmd := exec.Command("go", "build", "-o", binaryPath, "./cmd/muxagent")
	cmd.Dir = moduleRoot
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, string(output))
	return binaryPath
}

func copyExecutable(t *testing.T, src, dst string) {
	t.Helper()
	data, err := os.ReadFile(src)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(dst, data, 0o755))
}

func moduleRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	require.True(t, ok)
	return filepath.Clean(filepath.Join(filepath.Dir(file), "../.."))
}

func canonicalPath(t *testing.T, path string) string {
	t.Helper()
	realPath, err := filepath.EvalSymlinks(path)
	require.NoError(t, err)
	return realPath
}

func waitForPersistedTask(t *testing.T, workDir string, want taskdomain.TaskStatus) (taskdomain.Task, []taskdomain.NodeRun, taskdomain.TaskView) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		task, runs, view, err := loadSingleTaskState(workDir)
		if err == nil && view.Status == want {
			return task, runs, view
		}
		time.Sleep(50 * time.Millisecond)
	}
	task, runs, view, err := loadSingleTaskState(workDir)
	require.NoError(t, err)
	require.Equalf(t, want, view.Status, "final runs: %v", summarizeRuns(runs))
	return task, runs, view
}

func waitForNodeRunCounts(t *testing.T, workDir string, want map[string]int) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		_, runs, _, err := loadSingleTaskState(workDir)
		if err == nil {
			got := map[string]int{}
			for _, run := range runs {
				got[run.NodeName]++
			}
			if mapsEqual(got, want) {
				return
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	_, runs, _, err := loadSingleTaskState(workDir)
	require.NoError(t, err)
	assertNodeRunCounts(t, runs, want)
}

func loadSingleTaskState(workDir string) (taskdomain.Task, []taskdomain.NodeRun, taskdomain.TaskView, error) {
	store, err := taskstore.Open(workDir)
	if err != nil {
		return taskdomain.Task{}, nil, taskdomain.TaskView{}, err
	}
	defer store.Close()

	ctx := context.Background()
	tasks, err := store.ListTasksByWorkDir(ctx, workDir)
	if err != nil {
		return taskdomain.Task{}, nil, taskdomain.TaskView{}, err
	}
	if len(tasks) != 1 {
		return taskdomain.Task{}, nil, taskdomain.TaskView{}, assert.AnError
	}
	task := tasks[0]
	runs, err := store.ListNodeRunsByTask(ctx, task.ID)
	if err != nil {
		return taskdomain.Task{}, nil, taskdomain.TaskView{}, err
	}
	cfg, err := taskconfig.Load(taskstore.ConfigPath(workDir, task.ID))
	if err != nil {
		return taskdomain.Task{}, nil, taskdomain.TaskView{}, err
	}
	return task, runs, taskdomain.DeriveTaskView(task, cfg, runs), nil
}

func assertNodeRunCounts(t *testing.T, runs []taskdomain.NodeRun, want map[string]int) {
	t.Helper()
	got := map[string]int{}
	for _, run := range runs {
		got[run.NodeName]++
	}
	assert.Equal(t, want, got)
}

func assertHumanAuditArtifact(t *testing.T, run taskdomain.NodeRun) {
	t.Helper()
	paths := taskdomain.ArtifactPaths(run.Result)
	require.Len(t, paths, 1)
	assert.FileExists(t, paths[0])
	assert.Equal(t, "output.json", filepath.Base(paths[0]))
}

func assertArtifactDirs(t *testing.T, task taskdomain.Task, want []string) {
	t.Helper()
	artifactRoot := filepath.Join(taskstore.TaskDir(task.WorkDir, task.ID), "artifacts")
	entries, err := os.ReadDir(artifactRoot)
	require.NoError(t, err)
	var names []string
	for _, entry := range entries {
		if entry.IsDir() {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	assert.Equal(t, want, names)
	for _, name := range names {
		assert.NoFileExists(t, filepath.Join(artifactRoot, name, "result_schema.json"))
	}
	schemaRoot := filepath.Join(taskstore.TaskDir(task.WorkDir, task.ID), "schemas")
	for _, nodeName := range []string{"upsert_plan", "review_plan", "implement", "verify"} {
		assert.FileExists(t, filepath.Join(schemaRoot, nodeName+".json"))
	}
}

func assertPromptFiles(t *testing.T, workDir, taskID string) {
	t.Helper()
	promptDir := filepath.Join(taskstore.TaskDir(workDir, taskID), "prompts")
	entries, err := os.ReadDir(promptDir)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(entries), 4)
}

func mapsEqual(left, right map[string]int) bool {
	if len(left) != len(right) {
		return false
	}
	for key, value := range left {
		if right[key] != value {
			return false
		}
	}
	return true
}

func summarizeRuns(runs []taskdomain.NodeRun) []string {
	out := make([]string, 0, len(runs))
	for _, run := range runs {
		out = append(out, run.NodeName+":"+string(run.Status))
	}
	return out
}

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
	"github.com/LaLanMo/muxagent-cli/internal/taskengine"
	"github.com/LaLanMo/muxagent-cli/internal/taskstore"
	"github.com/LaLanMo/muxagent-cli/internal/worktree"
	"github.com/charmbracelet/x/ansi"
	"github.com/creack/pty"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestTaskTUIEndToEndScenarios(t *testing.T) {
	moduleRoot := moduleRoot(t)
	binaryPath := buildMuxagentBinary(t, moduleRoot)
	fakeCodexFixture := filepath.Join(moduleRoot, "cmd", "muxagent", "testdata", "fake-codex.sh")
	fakeClaudeFixture := filepath.Join(moduleRoot, "cmd", "muxagent", "testdata", "fake-claude.sh")
	basePath := os.Getenv("PATH")
	longDescription := strings.TrimSpace(strings.Repeat(
		"Long end-to-end task descriptions should persist without truncation through planning, approval, implementation, and storage. ",
		6,
	))
	require.Greater(t, len(longDescription), 512)
	defaultPromptFiles := []string{"implement.md", "review_plan.md", "upsert_plan.md", "verify.md"}
	defaultSchemaFiles := []string{"implement.json", "review_plan.json", "upsert_plan.json", "verify.json"}
	yoloPromptFiles := []string{"yolo_evaluate_progress.md", "yolo_implement.md", "yolo_review_plan.md", "yolo_upsert_plan.md", "yolo_verify.md"}
	yoloSchemaFiles := []string{"evaluate_progress.json", "implement.json", "review_plan.json", "upsert_plan.json", "verify.json"}

	tests := []struct {
		name                string
		flow                string
		description         string
		cliArgs             []string
		configPath          func(t *testing.T, workDir string) string
		drive               func(t *testing.T, session *tuiSession)
		expectedArtifacts   []string
		expectedPrompts     []string
		expectedSchemas     []string
		requirePromptHeader bool
		verify              func(t *testing.T, task taskdomain.Task, runs []taskdomain.NodeRun, view taskdomain.TaskView)
	}{
		{
			name:        "happy path",
			flow:        "happy",
			description: "Implement login",
			drive: func(t *testing.T, session *tuiSession) {
				session.waitForAll(t, 10*time.Second, "No tasks in this working directory yet.", "new task")
				session.send(t, "\r")
				session.waitForAll(t, 5*time.Second, "New Task", "Describe your task")
				session.submitNewTask(t, "Implement login")
				session.waitForAll(t, 10*time.Second, "approve_plan", "awaiting approval")
				session.confirm(t)
			},
			expectedArtifacts:   []string{"01-upsert_plan", "02-review_plan", "03-approve_plan", "04-implement", "05-verify"},
			requirePromptHeader: true,
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
						assertHumanAuditArtifact(t, task, runs, run)
					}
				}
			},
		},
		{
			name:        "plan only stops after reviewed planning",
			flow:        "happy",
			description: "Design the rollout plan",
			drive: func(t *testing.T, session *tuiSession) {
				session.waitForAll(t, 10*time.Second, "No tasks in this working directory yet.", "new task")
				session.send(t, "\r")
				session.waitForAll(t, 5*time.Second, "New Task", "Describe your task")
				session.send(t, "\x0e")
				session.waitForAll(t, 5*time.Second, "plan-only")
				session.submitNewTask(t, "Design the rollout plan")
				session.waitForAll(t, 10*time.Second, "Task completed successfully")
			},
			expectedArtifacts:   []string{"01-upsert_plan", "02-review_plan"},
			expectedPrompts:     []string{"review_plan.md", "upsert_plan.md"},
			expectedSchemas:     []string{"review_plan.json", "upsert_plan.json"},
			requirePromptHeader: true,
			verify: func(t *testing.T, task taskdomain.Task, runs []taskdomain.NodeRun, view taskdomain.TaskView) {
				require.Len(t, runs, 3)
				assert.Equal(t, taskdomain.TaskStatusDone, view.Status)
				assertNodeRunCounts(t, runs, map[string]int{
					"upsert_plan": 1,
					"review_plan": 1,
					"done":        1,
				})
				for _, run := range runs {
					assert.NotEqual(t, "approve_plan", run.NodeName)
					assert.NotEqual(t, "implement", run.NodeName)
					assert.NotEqual(t, "verify", run.NodeName)
				}
			},
		},
		{
			name:        "autonomous loops on failed verify without human approval",
			flow:        "verify-fail-once",
			description: "Fix the flaky flow autonomously",
			drive: func(t *testing.T, session *tuiSession) {
				session.waitForAll(t, 10*time.Second, "No tasks in this working directory yet.", "new task")
				session.send(t, "\r")
				session.waitForAll(t, 5*time.Second, "New Task", "Describe your task")
				session.send(t, "\x0e")
				session.waitForAll(t, 5*time.Second, "plan-only")
				session.send(t, "\x0e")
				session.waitForAll(t, 5*time.Second, "autonomous")
				session.submitNewTask(t, "Fix the flaky flow autonomously")
				session.waitForAll(t, 10*time.Second, "Task completed successfully")
			},
			expectedArtifacts:   []string{"01-upsert_plan", "02-review_plan", "03-implement", "04-verify", "05-implement", "06-verify"},
			expectedPrompts:     defaultPromptFiles,
			expectedSchemas:     defaultSchemaFiles,
			requirePromptHeader: true,
			verify: func(t *testing.T, task taskdomain.Task, runs []taskdomain.NodeRun, view taskdomain.TaskView) {
				require.Len(t, runs, 7)
				assert.Equal(t, taskdomain.TaskStatusDone, view.Status)
				assertNodeRunCounts(t, runs, map[string]int{
					"upsert_plan": 1,
					"review_plan": 1,
					"implement":   2,
					"verify":      2,
					"done":        1,
				})
				for _, run := range runs {
					assert.NotEqual(t, "approve_plan", run.NodeName)
				}
			},
		},
		{
			name:        "yolo replans after verified wave and then completes",
			flow:        "yolo-replan-once",
			description: "Finish the task over multiple autonomous waves",
			drive: func(t *testing.T, session *tuiSession) {
				session.waitForAll(t, 10*time.Second, "No tasks in this working directory yet.", "new task")
				session.send(t, "\r")
				session.waitForAll(t, 5*time.Second, "New Task", "Describe your task")
				session.send(t, "\x0e")
				session.waitForAll(t, 5*time.Second, "plan-only")
				session.send(t, "\x0e")
				session.waitForAll(t, 5*time.Second, "autonomous")
				session.send(t, "\x0e")
				session.waitForAll(t, 5*time.Second, "yolo")
				session.submitNewTask(t, "Finish the task over multiple autonomous waves")
				session.waitForAll(t, 10*time.Second, "Task completed successfully")
			},
			expectedArtifacts:   []string{"01-upsert_plan", "02-review_plan", "03-implement", "04-verify", "05-evaluate_progress", "06-upsert_plan", "07-review_plan", "08-implement", "09-verify", "10-evaluate_progress"},
			expectedPrompts:     yoloPromptFiles,
			expectedSchemas:     yoloSchemaFiles,
			requirePromptHeader: true,
			verify: func(t *testing.T, task taskdomain.Task, runs []taskdomain.NodeRun, view taskdomain.TaskView) {
				require.Len(t, runs, 11)
				assert.Equal(t, taskdomain.TaskStatusDone, view.Status)
				assertNodeRunCounts(t, runs, map[string]int{
					"upsert_plan":       2,
					"review_plan":       2,
					"implement":         2,
					"verify":            2,
					"evaluate_progress": 2,
					"done":              1,
				})
				for _, run := range runs {
					assert.NotEqual(t, "approve_plan", run.NodeName)
					if run.NodeName == "evaluate_progress" {
						assert.NotEmpty(t, run.Result["next_node"])
					}
				}
			},
		},
		{
			name:        "long description persists end to end",
			flow:        "happy",
			description: longDescription,
			drive: func(t *testing.T, session *tuiSession) {
				session.waitForAll(t, 10*time.Second, "No tasks in this working directory yet.", "new task")
				session.send(t, "\r")
				session.waitForAll(t, 5*time.Second, "New Task", "Describe your task")
				session.submitNewTask(t, longDescription)
				session.waitForAll(t, 10*time.Second, "approve_plan", "awaiting approval")
				session.confirm(t)
			},
			expectedArtifacts:   []string{"01-upsert_plan", "02-review_plan", "03-approve_plan", "04-implement", "05-verify"},
			requirePromptHeader: true,
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
			},
		},
		{
			name:        "approval rejection loops back",
			flow:        "happy",
			description: "Reject once",
			drive: func(t *testing.T, session *tuiSession) {
				session.waitForAll(t, 10*time.Second, "No tasks in this working directory yet.", "new task")
				session.send(t, "\r")
				session.waitForAll(t, 5*time.Second, "New Task", "Describe your task")
				session.submitNewTask(t, "Reject once")
				session.waitForAll(t, 10*time.Second, "approve_plan", "awaiting approval")
				session.send(t, "\x1b[B")
				session.pause(300 * time.Millisecond)
				session.send(t, "\t")
				session.pause(300 * time.Millisecond)
				session.send(t, "Need more detail")
				session.pause(750 * time.Millisecond)
				session.send(t, "\x1b")
				session.pause(300 * time.Millisecond)
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
					assertHumanAuditArtifact(t, task, runs, run)
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
				session.waitForAll(t, 10*time.Second, "No tasks in this working directory yet.", "new task")
				session.send(t, "\r")
				session.waitForAll(t, 5*time.Second, "New Task", "Describe your task")
				session.submitNewTask(t, "Need clarification")
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
						assertHumanAuditArtifact(t, task, runs, run)
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
				session.waitForAll(t, 10*time.Second, "No tasks in this working directory yet.", "new task")
				session.send(t, "\r")
				session.waitForAll(t, 5*time.Second, "New Task", "Describe your task")
				session.submitNewTask(t, "Review rejects once")
				session.waitForAll(t, 10*time.Second, "approve_plan", "awaiting approval")
				session.confirm(t)
			},
			expectedArtifacts:   []string{"01-upsert_plan", "02-review_plan", "03-upsert_plan", "04-review_plan", "05-approve_plan", "06-implement", "07-verify"},
			requirePromptHeader: true,
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
							assertHumanAuditArtifact(t, task, runs, run)
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
			name:        "blocked loopback can be force-continued",
			flow:        "review-reject-once",
			description: "Blocked loopback",
			configPath: func(t *testing.T, workDir string) string {
				cfg, err := taskconfig.LoadDefault()
				require.NoError(t, err)
				for i := range cfg.Topology.Nodes {
					if cfg.Topology.Nodes[i].Name == "upsert_plan" {
						cfg.Topology.Nodes[i].MaxIterations = 1
					}
				}
				return writeOverrideConfig(t, workDir, "blocked-taskflow.yaml", cfg)
			},
			drive: func(t *testing.T, session *tuiSession) {
				session.waitForAll(t, 10*time.Second, "No tasks in this working directory yet.", "new task")
				session.send(t, "\r")
				session.waitForAll(t, 5*time.Second, "New Task", "Describe your task")
				session.submitNewTask(t, "Blocked loopback")
				session.waitForAll(t, 10*time.Second, "Task blocked", "Force continue")
				session.send(t, "\r")
				session.waitForAll(t, 10*time.Second, "approve_plan", "awaiting approval")
				session.confirm(t)
			},
			expectedArtifacts:   []string{"01-upsert_plan", "02-review_plan", "03-upsert_plan", "04-review_plan", "05-approve_plan", "06-implement", "07-verify"},
			requirePromptHeader: false,
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
				assert.Empty(t, view.BlockedSteps)
			},
		},
		{
			name:        "failed agent node can be retried from the footer",
			flow:        "implement-fail-once",
			description: "Retry failed implement",
			drive: func(t *testing.T, session *tuiSession) {
				session.waitForAll(t, 10*time.Second, "No tasks in this working directory yet.", "new task")
				session.send(t, "\r")
				session.waitForAll(t, 5*time.Second, "New Task", "Describe your task")
				session.submitNewTask(t, "Retry failed implement")
				session.waitForAll(t, 10*time.Second, "approve_plan", "awaiting approval")
				session.confirm(t)
				session.waitForAll(t, 10*time.Second, "Task failed", "Retry step")
				session.send(t, "\r")
			},
			expectedArtifacts:   []string{"01-upsert_plan", "02-review_plan", "03-approve_plan", "04-implement", "05-implement", "06-verify"},
			requirePromptHeader: true,
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

			args := append([]string(nil), tt.cliArgs...)
			if tt.configPath != nil {
				installDefaultTaskConfigRegistryEntry(t, homeDir, "custom", tt.configPath(t, workDir))
			}
			session := startTUISession(t, binaryPath, workDir, args...)
			tt.drive(t, session)
			task, runs, view := waitForPersistedTask(t, workDir, taskdomain.TaskStatusDone)

			assert.Equal(t, tt.description, task.Description)
			assert.Equal(t, workDir, task.WorkDir)
			assert.FileExists(t, taskstore.DBPath(workDir))
			assert.FileExists(t, taskstore.ConfigPath(workDir, task.ID))
			assertArtifactDirs(t, task, tt.expectedArtifacts)
			expectedPrompts := tt.expectedPrompts
			if len(expectedPrompts) == 0 {
				expectedPrompts = defaultPromptFiles
			}
			expectedSchemas := tt.expectedSchemas
			if len(expectedSchemas) == 0 {
				expectedSchemas = defaultSchemaFiles
			}
			assertPromptFiles(t, workDir, task.ID, expectedPrompts, tt.requirePromptHeader)
			assertSchemaFiles(t, task, expectedSchemas)
			tt.verify(t, task, runs, view)

			session.quit(t)
		})
	}
}

func TestTaskTUIE2EPersistsExactCodexPromptInInputArtifact(t *testing.T) {
	moduleRoot := moduleRoot(t)
	binaryPath := buildMuxagentBinary(t, moduleRoot)
	fakeCodexFixture := filepath.Join(moduleRoot, "cmd", "muxagent", "testdata", "fake-codex.sh")
	fakeClaudeFixture := filepath.Join(moduleRoot, "cmd", "muxagent", "testdata", "fake-claude.sh")
	basePath := os.Getenv("PATH")

	workDir := canonicalPath(t, t.TempDir())
	homeDir := t.TempDir()
	fakeDir := t.TempDir()
	fakeCodexPath := filepath.Join(fakeDir, "codex")
	fakeClaudePath := filepath.Join(fakeDir, "claude")
	copyExecutable(t, fakeCodexFixture, fakeCodexPath)
	copyExecutable(t, fakeClaudeFixture, fakeClaudePath)

	stateDir := filepath.Join(workDir, ".fake-codex-state")
	t.Setenv("HOME", homeDir)
	t.Setenv("PATH", fakeDir+string(os.PathListSeparator)+basePath)
	t.Setenv("FAKE_CODEX_FLOW", "happy")
	t.Setenv("FAKE_CODEX_STATE_DIR", stateDir)
	t.Setenv("FAKE_CLAUDE_FLOW", "happy")
	t.Setenv("FAKE_CLAUDE_STATE_DIR", filepath.Join(workDir, ".fake-claude-state"))
	t.Setenv("TERM", "xterm-256color")

	session := startTUISession(t, binaryPath, workDir)
	session.waitForAll(t, 10*time.Second, "No tasks in this working directory yet.", "new task")
	session.send(t, "\r")
	session.waitForAll(t, 5*time.Second, "New Task", "Describe your task")
	session.submitNewTask(t, "Implement login")
	session.waitForAll(t, 10*time.Second, "approve_plan", "awaiting approval")
	session.confirm(t)

	_, runs, view := waitForPersistedTask(t, workDir, taskdomain.TaskStatusDone)
	assert.Equal(t, taskdomain.TaskStatusDone, view.Status)

	upsertRun := requireNodeRunByName(t, runs, "upsert_plan")
	inputPath := mustRunAuditPath(t, workDir, taskdomain.Task{ID: view.Task.ID, WorkDir: workDir}, runs, upsertRun, "input.md")
	inputBytes, err := os.ReadFile(inputPath)
	require.NoError(t, err)

	capturedPromptPath := filepath.Join(stateDir, "01-upsert_plan.prompt.txt")
	capturedPromptBytes, err := os.ReadFile(capturedPromptPath)
	require.NoError(t, err)
	assert.Equal(t, string(capturedPromptBytes), string(inputBytes))
	assertArtifactPathsExcludeRuntimeAudit(t, view.ArtifactPaths)

	templatePath := filepath.Join(moduleRoot, "internal", "taskconfig", "defaults", "prompts", "upsert_plan.md")
	assertPromptContainsLiteralTemplateLines(t, string(inputBytes), templatePath)
	assert.Contains(t, string(inputBytes), "Output contract:")
	assert.Contains(t, string(inputBytes), "- Return exactly one JSON object matching the provided schema.")
	assert.Contains(t, string(inputBytes), "- When the node is complete, return {\"kind\":\"result\",\"result\":<payload matching the node result schema>,\"clarification\":null}.")
	assert.NotContains(t, string(inputBytes), "# Input")
	assert.NotContains(t, string(inputBytes), "## Prompt")
	assert.NotContains(t, string(inputBytes), "{{")

	reviewPromptBytes, err := os.ReadFile(filepath.Join(stateDir, "02-review_plan.prompt.txt"))
	require.NoError(t, err)
	assert.NotContains(t, string(reviewPromptBytes), "input.md")
	assert.NotContains(t, string(reviewPromptBytes), "output.json")

	session.quit(t)
}

func TestTaskTUICanCreateTasksWithDifferentConfigsInOneSession(t *testing.T) {
	moduleRoot := moduleRoot(t)
	binaryPath := buildMuxagentBinary(t, moduleRoot)
	fakeCodexFixture := filepath.Join(moduleRoot, "cmd", "muxagent", "testdata", "fake-codex.sh")
	fakeClaudeFixture := filepath.Join(moduleRoot, "cmd", "muxagent", "testdata", "fake-claude.sh")
	basePath := os.Getenv("PATH")

	workDir := canonicalPath(t, t.TempDir())
	homeDir := t.TempDir()
	fakeDir := t.TempDir()
	fakeCodexPath := filepath.Join(fakeDir, "codex")
	fakeClaudePath := filepath.Join(fakeDir, "claude")
	copyExecutable(t, fakeCodexFixture, fakeCodexPath)
	copyExecutable(t, fakeClaudeFixture, fakeClaudePath)

	t.Setenv("HOME", homeDir)
	t.Setenv("PATH", fakeDir+string(os.PathListSeparator)+basePath)
	t.Setenv("FAKE_CODEX_FLOW", "happy")
	t.Setenv("FAKE_CODEX_STATE_DIR", filepath.Join(workDir, ".fake-codex-state"))
	t.Setenv("FAKE_CLAUDE_FLOW", "happy")
	t.Setenv("FAKE_CLAUDE_STATE_DIR", filepath.Join(workDir, ".fake-claude-state"))
	t.Setenv("TERM", "xterm-256color")

	defaultSourcePath := writeOverrideConfig(t, t.TempDir(), "default.yaml", singleAgentTerminalConfig(appconfig.RuntimeCodex))
	reviewerSourcePath := writeOverrideConfig(t, t.TempDir(), "reviewer.yaml", singleAgentTerminalConfig(appconfig.RuntimeClaudeCode))
	defaultInstalledPath := installManagedDefaultConfig(t, homeDir, defaultSourcePath)
	installed := installTaskConfigRegistryEntries(t, homeDir, taskconfig.DefaultAlias, map[string]string{
		"reviewer": reviewerSourcePath,
	})

	session := startTUISession(t, binaryPath, workDir)
	session.waitForAll(t, 10*time.Second, "No tasks in this working directory yet.", "config default")
	session.send(t, "\r")
	session.waitForAll(t, 5*time.Second, "New Task", "config default")
	session.submitNewTask(t, "Default config task")
	session.waitForAll(t, 10*time.Second, "Task completed successfully")
	session.send(t, "\x1b")
	session.waitForAll(t, 5*time.Second, "new task", "done Default config task")

	session.send(t, "\x1b[A")
	session.pause(150 * time.Millisecond)
	session.send(t, "\x1b[A")
	session.pause(150 * time.Millisecond)
	session.send(t, "\r")
	session.waitForAll(t, 5*time.Second, "New Task", "config default")
	session.send(t, "\x10")
	session.resize(t, 140, 40)
	session.waitForAll(t, 5*time.Second, "reviewer", "runtime claude-code")
	session.submitNewTask(t, "Reviewer config task")
	session.waitForAll(t, 10*time.Second, "Task completed successfully")
	session.quit(t)

	tasks, err := loadTaskRecords(workDir)
	require.NoError(t, err)
	require.Len(t, tasks, 2)

	byDescription := map[string]taskdomain.Task{}
	for _, task := range tasks {
		byDescription[task.Description] = task
	}

	defaultTask, ok := byDescription["Default config task"]
	require.True(t, ok)
	assert.Equal(t, taskconfig.DefaultAlias, defaultTask.ConfigAlias)
	assert.Equal(t, defaultInstalledPath, defaultTask.ConfigPath)
	defaultCfg, err := taskconfig.Load(taskstore.ConfigPath(workDir, defaultTask.ID))
	require.NoError(t, err)
	assert.Equal(t, appconfig.RuntimeCodex, defaultCfg.Runtime)

	reviewerTask, ok := byDescription["Reviewer config task"]
	require.True(t, ok)
	assert.Equal(t, "reviewer", reviewerTask.ConfigAlias)
	assert.Equal(t, installed["reviewer"], reviewerTask.ConfigPath)
	reviewerCfg, err := taskconfig.Load(taskstore.ConfigPath(workDir, reviewerTask.ID))
	require.NoError(t, err)
	assert.Equal(t, appconfig.RuntimeClaudeCode, reviewerCfg.Runtime)
}

func TestTaskTUIWorktreeLaunchStoresTasksInOriginalDirAndRemembersPreference(t *testing.T) {
	moduleRoot := moduleRoot(t)
	binaryPath := buildMuxagentBinary(t, moduleRoot)
	fakeCodexFixture := filepath.Join(moduleRoot, "cmd", "muxagent", "testdata", "fake-codex.sh")
	fakeClaudeFixture := filepath.Join(moduleRoot, "cmd", "muxagent", "testdata", "fake-claude.sh")
	basePath := os.Getenv("PATH")

	repoRoot := initTaskTUIE2EGitRepo(t, true)
	workDir := filepath.Join(repoRoot, "packages", "app")
	homeDir := canonicalPath(t, t.TempDir())
	fakeDir := t.TempDir()
	fakeCodexPath := filepath.Join(fakeDir, "codex")
	fakeClaudePath := filepath.Join(fakeDir, "claude")
	copyExecutable(t, fakeCodexFixture, fakeCodexPath)
	copyExecutable(t, fakeClaudeFixture, fakeClaudePath)

	t.Setenv("HOME", homeDir)
	t.Setenv("PATH", fakeDir+string(os.PathListSeparator)+basePath)
	t.Setenv("FAKE_CODEX_FLOW", "happy")
	t.Setenv("FAKE_CODEX_STATE_DIR", filepath.Join(workDir, ".fake-codex-state"))
	t.Setenv("FAKE_CLAUDE_FLOW", "happy")
	t.Setenv("FAKE_CLAUDE_STATE_DIR", filepath.Join(workDir, ".fake-claude-state"))
	t.Setenv("TERM", "xterm-256color")

	session := startTUISession(t, binaryPath, workDir)
	session.waitForAll(t, 10*time.Second, "No tasks in this working directory yet.", "new task")
	session.send(t, "\r")
	session.waitForAll(t, 5*time.Second, "New Task", "worktree off", "Ctrl+T worktree on")
	session.send(t, "\x14")
	session.resize(t, 141, 40)
	session.waitForAll(t, 5*time.Second, "worktree on", "Ctrl+T worktree off")
	session.submitNewTask(t, "Worktree-backed task")
	session.waitForAll(t, 10*time.Second, "approve_plan", "awaiting approval")
	session.confirm(t)

	task, runs, view := waitForPersistedTask(t, workDir, taskdomain.TaskStatusDone)
	require.Len(t, runs, 6)
	assert.Equal(t, taskdomain.TaskStatusDone, view.Status)
	assert.Equal(t, workDir, task.WorkDir)
	assert.NotEqual(t, workDir, task.ExecutionDir)
	assert.True(t, strings.HasPrefix(task.ExecutionDir, filepath.Join(homeDir, ".muxagent", "worktrees")+string(os.PathSeparator)))
	assert.FileExists(t, taskstore.DBPath(task.WorkDir))
	assert.FileExists(t, taskstore.ConfigPath(task.WorkDir, task.ID))
	assert.NoFileExists(t, taskstore.DBPath(task.ExecutionDir))

	executionRepoRoot, err := worktree.FindRepoRoot(task.ExecutionDir)
	require.NoError(t, err)
	assert.NotEqual(t, repoRoot, executionRepoRoot)
	relPath, err := filepath.Rel(executionRepoRoot, task.ExecutionDir)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join("packages", "app"), relPath)

	branchOut, err := exec.Command("git", "-C", repoRoot, "branch", "--list", worktree.BranchName(task.ID)).CombinedOutput()
	require.NoError(t, err, string(branchOut))
	assert.Contains(t, strings.TrimSpace(string(branchOut)), worktree.BranchName(task.ID))

	session.quit(t)

	restarted := startTUISession(t, binaryPath, workDir)
	restarted.resize(t, 141, 40)
	restarted.waitForAll(t, 10*time.Second, "done Worktree-backed task", "worktree", "new task")
	restarted.send(t, "\x1b[B")
	restarted.pause(150 * time.Millisecond)
	restarted.send(t, "\x1b[B")
	restarted.pause(150 * time.Millisecond)
	restarted.send(t, "\r")
	restarted.resetOutput()
	restarted.waitForAll(t, 5*time.Second, "Task: Worktree-backed task", "done", "worktree", "✓ upsert_plan")
	assert.NotContains(t, restarted.output(), "→")
	restarted.send(t, "\x1b")
	restarted.resetOutput()
	restarted.waitForAll(t, 5*time.Second, "done Worktree-backed task", "worktree", "new task")
	for i := 0; i < 2; i++ {
		restarted.send(t, "\x1b[A")
		restarted.pause(150 * time.Millisecond)
	}
	restarted.send(t, "\r")
	restarted.resetOutput()
	restarted.waitForAll(t, 5*time.Second, "New Task", "worktree on", "Ctrl+T worktree off")
	restarted.quit(t)
}

func TestTaskTUIConfigScreenCanCloneSetDefaultAndDeleteConfig(t *testing.T) {
	moduleRoot := moduleRoot(t)
	binaryPath := buildMuxagentBinary(t, moduleRoot)

	workDir := canonicalPath(t, t.TempDir())
	homeDir := t.TempDir()

	t.Setenv("HOME", homeDir)
	t.Setenv("TERM", "xterm-256color")

	session := startTUISession(t, binaryPath, workDir)
	session.waitForAll(t, 10*time.Second, "new task", "task configs")
	session.send(t, "\x1b[B")
	session.send(t, "\r")
	session.waitForAll(t, 10*time.Second, "Task Configs", "Default")

	session.send(t, "n")
	session.waitForAll(t, 5*time.Second, "Clone Task Config", "Source config default")
	session.send(t, "reviewer")
	session.send(t, "\r")
	session.waitForAll(t, 10*time.Second, "reviewer", "custom", "Codex")

	require.Eventually(t, func() bool {
		reg, err := taskconfig.LoadRegistry()
		if err != nil {
			return false
		}
		_, ok := registryEntry(reg.Configs, "reviewer")
		return ok && reg.DefaultAlias == taskconfig.DefaultAlias
	}, 5*time.Second, 100*time.Millisecond)

	session.send(t, "\r")
	require.Eventually(t, func() bool {
		reg, err := taskconfig.LoadRegistry()
		if err != nil {
			return false
		}
		return reg.DefaultAlias == "reviewer"
	}, 5*time.Second, 100*time.Millisecond)

	session.send(t, "x")
	session.waitForAll(t, 5*time.Second, "Delete Task Config", "Existing tasks")
	session.send(t, "\r")
	require.Eventually(t, func() bool {
		reg, err := taskconfig.LoadRegistry()
		if err != nil {
			return false
		}
		_, ok := registryEntry(reg.Configs, "reviewer")
		return !ok && reg.DefaultAlias == taskconfig.DefaultAlias
	}, 5*time.Second, 100*time.Millisecond)

	taskConfigDir, err := taskconfig.TaskConfigDir()
	require.NoError(t, err)
	assert.NoDirExists(t, filepath.Join(taskConfigDir, "reviewer"))
	session.waitForAll(t, 5*time.Second, "Task Configs", "Default")

	session.send(t, "\x1b")
	session.waitForAll(t, 5*time.Second, "new task", "task configs")
	session.forceClose()
}

func TestTaskTUIBackToListDoesNotAutoReopenDetail(t *testing.T) {
	moduleRoot := moduleRoot(t)
	binaryPath := buildMuxagentBinary(t, moduleRoot)
	fakeCodexFixture := filepath.Join(moduleRoot, "cmd", "muxagent", "testdata", "fake-codex.sh")
	basePath := os.Getenv("PATH")

	workDir := canonicalPath(t, t.TempDir())
	homeDir := t.TempDir()
	fakeDir := t.TempDir()
	fakeCodexPath := filepath.Join(fakeDir, "codex")
	copyExecutable(t, fakeCodexFixture, fakeCodexPath)

	t.Setenv("HOME", homeDir)
	t.Setenv("PATH", fakeDir+string(os.PathListSeparator)+basePath)
	t.Setenv("FAKE_CODEX_FLOW", "slow-happy")
	t.Setenv("FAKE_CODEX_STATE_DIR", filepath.Join(workDir, ".fake-codex-state"))
	t.Setenv("TERM", "xterm-256color")

	session := startTUISession(t, binaryPath, workDir)
	session.waitForAll(t, 10*time.Second, "No tasks in this working directory yet.", "new task")
	session.send(t, "\r")
	session.waitForAll(t, 5*time.Second, "New Task", "Describe your task")
	session.submitNewTask(t, "Stay on list")
	session.waitForAll(t, 10*time.Second, "Task: Stay on list", "upsert_plan")
	session.send(t, "\x1b")
	session.resetOutput()
	session.waitForAll(t, 5*time.Second, "new task", "running Stay on list")
	waitForPersistedTask(t, workDir, taskdomain.TaskStatusAwaitingUser)
	session.resize(t, 140, 40)
	session.waitForAll(t, 5*time.Second, "new task", "Stay on list", "awaiting approval")

	output := session.output()
	assert.NotContains(t, output, "Approve this plan?")
	assert.NotContains(t, output, "Artifacts (")

	task, runs, view := waitForPersistedTask(t, workDir, taskdomain.TaskStatusAwaitingUser)
	assert.Equal(t, "Stay on list", task.Description)
	assert.Equal(t, taskdomain.TaskStatusAwaitingUser, view.Status)
	assertNodeRunCounts(t, runs, map[string]int{
		"upsert_plan":  1,
		"review_plan":  1,
		"approve_plan": 1,
	})

	session.quit(t)
}

func TestTaskTUIListShowsOnlyFirstLineOfMultilineDescriptions(t *testing.T) {
	moduleRoot := moduleRoot(t)
	binaryPath := buildMuxagentBinary(t, moduleRoot)

	workDir := canonicalPath(t, t.TempDir())
	homeDir := t.TempDir()

	t.Setenv("HOME", homeDir)
	t.Setenv("TERM", "xterm-256color")

	store, err := taskstore.Open(workDir)
	require.NoError(t, err)

	task := taskdomain.Task{
		ID:          "task-1",
		Description: "first line\nsecond line",
		WorkDir:     workDir,
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	}
	require.NoError(t, store.CreateTask(context.Background(), task))
	require.NoError(t, store.Close())

	configPath := writeOverrideConfig(t, workDir, "multiline.yaml", singleAgentTerminalConfig(appconfig.RuntimeCodex))
	installConfigBundle(t, configPath, filepath.Dir(taskstore.ConfigPath(workDir, task.ID)))

	session := startTUISession(t, binaryPath, workDir)
	session.waitForAll(t, 10*time.Second, "running first line", "new task", "Enter select")

	output := session.output()
	assert.NotContains(t, output, "second line")
	assert.Contains(t, output, "Ctrl+C quit")

	session.quit(t)
}

func TestTaskTUISmallTerminalArtifactTabSwitching(t *testing.T) {
	moduleRoot := moduleRoot(t)
	binaryPath := buildMuxagentBinary(t, moduleRoot)
	fakeCodexFixture := filepath.Join(moduleRoot, "cmd", "muxagent", "testdata", "fake-codex.sh")
	basePath := os.Getenv("PATH")

	workDir := canonicalPath(t, t.TempDir())
	homeDir := t.TempDir()
	fakeDir := t.TempDir()
	fakeCodexPath := filepath.Join(fakeDir, "codex")
	copyExecutable(t, fakeCodexFixture, fakeCodexPath)

	t.Setenv("HOME", homeDir)
	t.Setenv("PATH", fakeDir+string(os.PathListSeparator)+basePath)
	t.Setenv("FAKE_CODEX_FLOW", "happy")
	t.Setenv("FAKE_CODEX_STATE_DIR", filepath.Join(workDir, ".fake-codex-state"))
	t.Setenv("TERM", "xterm-256color")

	session := startTUISession(t, binaryPath, workDir)
	session.resize(t, 96, 24)
	session.waitForAll(t, 10*time.Second, "No tasks in this working directory yet.", "new task")
	session.send(t, "\r")
	session.waitForAll(t, 5*time.Second, "New Task", "Describe your task")
	session.submitNewTask(t, "Inspect artifacts on small terminal")
	session.waitForAll(t, 10*time.Second, "approve_plan", "awaiting approval")
	session.waitForAll(t, 5*time.Second, "Shift+Tab artifacts")
	session.resetOutput()

	// Switch to artifacts tab via Shift+Tab
	session.sendBacktab(t)
	session.waitForAll(t, 5*time.Second, "Shift+Tab timeline", "Files", "Preview ·")
	session.resetOutput()

	// Press Shift+Tab to return to timeline tab
	session.sendBacktab(t)
	session.waitForAll(t, 5*time.Second, "Shift+Tab artifacts")

	session.quit(t)
}

func TestTaskTUIWideTerminalCompletedArtifactsTabSwitch(t *testing.T) {
	moduleRoot := moduleRoot(t)
	binaryPath := buildMuxagentBinary(t, moduleRoot)
	fakeCodexFixture := filepath.Join(moduleRoot, "cmd", "muxagent", "testdata", "fake-codex.sh")
	basePath := os.Getenv("PATH")

	workDir := canonicalPath(t, t.TempDir())
	homeDir := t.TempDir()
	fakeDir := t.TempDir()
	fakeCodexPath := filepath.Join(fakeDir, "codex")
	copyExecutable(t, fakeCodexFixture, fakeCodexPath)

	t.Setenv("HOME", homeDir)
	t.Setenv("PATH", fakeDir+string(os.PathListSeparator)+basePath)
	t.Setenv("FAKE_CODEX_FLOW", "happy")
	t.Setenv("FAKE_CODEX_STATE_DIR", filepath.Join(workDir, ".fake-codex-state"))
	t.Setenv("TERM", "xterm-256color")

	session := startTUISession(t, binaryPath, workDir)
	session.resize(t, 149, 39)
	session.waitForAll(t, 10*time.Second, "No tasks in this working directory yet.", "new task")
	session.send(t, "\r")
	session.waitForAll(t, 5*time.Second, "New Task", "Describe your task")
	session.submitNewTask(t, "Wide completed artifacts")
	session.waitForAll(t, 10*time.Second, "approve_plan", "awaiting approval")
	session.confirm(t)

	session.waitForAll(t, 15*time.Second, "Task completed successfully", "Shift+Tab artifacts", "Esc back")
	session.resetOutput()

	// Switch to artifacts tab and verify content
	session.sendBacktab(t)
	session.waitForAll(t, 5*time.Second, "Shift+Tab timeline", "Files", "Preview ·")
	session.resetOutput()

	// Switch back to timeline, then go back to task list
	session.sendBacktab(t)
	session.pause(150 * time.Millisecond)
	session.send(t, "\x1b")
	session.waitForAll(t, 5*time.Second, "new task", "done Wide completed artifacts")

	// Re-open the task and switch to artifacts
	session.resetOutput()
	session.send(t, "\r")
	session.waitForAll(t, 5*time.Second, "Task completed successfully", "Shift+Tab artifacts")
	session.resetOutput()
	session.sendBacktab(t)
	session.waitForAll(t, 5*time.Second, "Shift+Tab timeline", "Files", "Preview ·")

	session.quit(t)
}

func TestTaskTUISmallTerminalCompletedArtifactsTabSwitchAndReopen(t *testing.T) {
	moduleRoot := moduleRoot(t)
	binaryPath := buildMuxagentBinary(t, moduleRoot)
	fakeCodexFixture := filepath.Join(moduleRoot, "cmd", "muxagent", "testdata", "fake-codex.sh")
	basePath := os.Getenv("PATH")

	workDir := canonicalPath(t, t.TempDir())
	homeDir := t.TempDir()
	fakeDir := t.TempDir()
	fakeCodexPath := filepath.Join(fakeDir, "codex")
	copyExecutable(t, fakeCodexFixture, fakeCodexPath)

	t.Setenv("HOME", homeDir)
	t.Setenv("PATH", fakeDir+string(os.PathListSeparator)+basePath)
	t.Setenv("FAKE_CODEX_FLOW", "happy")
	t.Setenv("FAKE_CODEX_STATE_DIR", filepath.Join(workDir, ".fake-codex-state"))
	t.Setenv("TERM", "xterm-256color")

	session := startTUISession(t, binaryPath, workDir)
	session.resize(t, 96, 24)
	session.waitForAll(t, 10*time.Second, "No tasks in this working directory yet.", "new task")
	session.send(t, "\r")
	session.waitForAll(t, 5*time.Second, "New Task", "Describe your task")
	session.submitNewTask(t, "Small completed artifacts")
	session.waitForAll(t, 10*time.Second, "approve_plan", "awaiting approval")
	session.confirm(t)

	session.waitForAll(t, 15*time.Second, "Task completed successfully", "Shift+Tab artifacts", "Esc back")
	session.resetOutput()

	// Switch to artifacts tab
	session.sendBacktab(t)
	session.waitForAll(t, 5*time.Second, "Shift+Tab timeline", "Files", "Preview ·")
	session.resetOutput()

	// Press Shift+Tab back to timeline, then esc to task list
	session.sendBacktab(t)
	session.waitForAll(t, 5*time.Second, "Shift+Tab artifacts", "Esc back")
	session.resetOutput()

	session.send(t, "\x1b")
	session.waitForAll(t, 5*time.Second, "new task", "done Small completed artifacts")

	// Re-open and verify footer hint
	session.resetOutput()
	session.send(t, "\r")
	session.waitForAll(t, 5*time.Second, "Shift+Tab artifacts", "Esc back")

	session.quit(t)
}

func TestTaskTUIClarificationWithArtifactsTabSwitchReachable(t *testing.T) {
	moduleRoot := moduleRoot(t)
	binaryPath := buildMuxagentBinary(t, moduleRoot)
	fakeCodexFixture := filepath.Join(moduleRoot, "cmd", "muxagent", "testdata", "fake-codex.sh")
	basePath := os.Getenv("PATH")

	workDir := canonicalPath(t, t.TempDir())
	homeDir := t.TempDir()
	fakeDir := t.TempDir()
	fakeCodexPath := filepath.Join(fakeDir, "codex")
	copyExecutable(t, fakeCodexFixture, fakeCodexPath)

	t.Setenv("HOME", homeDir)
	t.Setenv("PATH", fakeDir+string(os.PathListSeparator)+basePath)
	t.Setenv("FAKE_CODEX_FLOW", "clarify-late")
	t.Setenv("FAKE_CODEX_STATE_DIR", filepath.Join(workDir, ".fake-codex-state"))
	t.Setenv("TERM", "xterm-256color")

	session := startTUISession(t, binaryPath, workDir)
	session.resize(t, 149, 39)
	session.waitForAll(t, 10*time.Second, "No tasks in this working directory yet.", "new task")
	session.send(t, "\r")
	session.waitForAll(t, 5*time.Second, "New Task", "Describe your task")
	session.submitNewTask(t, "Clarification with artifacts")
	session.waitForAll(t, 10*time.Second, "approve_plan", "awaiting approval")
	session.confirm(t)

	output := session.waitForAll(t, 10*time.Second, "implement", "awaiting input", "Question 1/1", "Shift+Tab artifacts")
	assert.Contains(t, output, "Write your own answer")
	assert.Contains(t, output, "Submit answers")
	assert.NotContains(t, output, "[ ] Other")

	// Switch to artifacts tab via Shift+Tab
	session.resetOutput()
	session.sendBacktab(t)
	session.waitForAll(t, 5*time.Second, "Shift+Tab timeline", "Files", "Preview ·")

	// Switch back to timeline. The clarification panel is identical on both
	// tabs, so bubbletea's differential renderer won't re-send those lines.
	// A resize forces a full repaint so we can assert on the panel content.
	session.resetOutput()
	session.sendBacktab(t)
	session.resize(t, 150, 39)
	session.waitForAll(t, 5*time.Second, "Shift+Tab artifacts", "Question 1/1")

	session.quit(t)
}

func TestTaskTUILongTaskDescriptionsKeepAwaitingFootersVisible(t *testing.T) {
	moduleRoot := moduleRoot(t)
	binaryPath := buildMuxagentBinary(t, moduleRoot)
	fakeCodexFixture := filepath.Join(moduleRoot, "cmd", "muxagent", "testdata", "fake-codex.sh")
	basePath := os.Getenv("PATH")

	workDir := canonicalPath(t, t.TempDir())
	homeDir := t.TempDir()
	fakeDir := t.TempDir()
	fakeCodexPath := filepath.Join(fakeDir, "codex")
	copyExecutable(t, fakeCodexFixture, fakeCodexPath)

	t.Setenv("HOME", homeDir)
	t.Setenv("PATH", fakeDir+string(os.PathListSeparator)+basePath)
	t.Setenv("FAKE_CODEX_FLOW", "clarify-late")
	t.Setenv("FAKE_CODEX_STATE_DIR", filepath.Join(workDir, ".fake-codex-state"))
	t.Setenv("TERM", "xterm-256color")

	longDescription := strings.TrimSpace(strings.Repeat(
		"Keep the task detail footer visible even when the title is very long and artifacts are present. ",
		3,
	))

	session := startTUISession(t, binaryPath, workDir)
	session.resize(t, 149, 39)
	session.waitForAll(t, 10*time.Second, "No tasks in this working directory yet.", "new task")
	session.send(t, "\r")
	session.waitForAll(t, 5*time.Second, "New Task", "Describe your task")
	session.submitNewTask(t, longDescription)

	session.waitForAll(t, 10*time.Second, "approve_plan", "awaiting approval", "Ctrl+C quit", "Enter confirm")

	session.resetOutput()
	session.confirm(t)
	// Resize forces a full repaint; the incremental renderer skips
	// unchanged right-side characters (like "Ctrl+C quit") otherwise.
	session.resize(t, 150, 39)

	session.waitForAll(t, 10*time.Second, "awaiting input", "Question 1/1", "Ctrl+C quit", "Write your own answer")

	session.quit(t)
}

func TestTaskTUILongTaskDescriptionsKeepFailedAndRunningFootersVisible(t *testing.T) {
	moduleRoot := moduleRoot(t)
	binaryPath := buildMuxagentBinary(t, moduleRoot)
	fakeCodexFixture := filepath.Join(moduleRoot, "cmd", "muxagent", "testdata", "fake-codex.sh")
	basePath := os.Getenv("PATH")

	workDir := canonicalPath(t, t.TempDir())
	homeDir := t.TempDir()
	fakeDir := t.TempDir()
	fakeCodexPath := filepath.Join(fakeDir, "codex")
	copyExecutable(t, fakeCodexFixture, fakeCodexPath)

	t.Setenv("HOME", homeDir)
	t.Setenv("PATH", fakeDir+string(os.PathListSeparator)+basePath)
	t.Setenv("FAKE_CODEX_FLOW", "implement-fail-once")
	t.Setenv("FAKE_CODEX_STATE_DIR", filepath.Join(workDir, ".fake-codex-state"))
	t.Setenv("TERM", "xterm-256color")

	longDescription := strings.TrimSpace(strings.Repeat(
		"Keep the task detail footer visible even when the title is very long and artifacts are present. ",
		3,
	))

	session := startTUISession(t, binaryPath, workDir)
	session.resize(t, 149, 39)
	session.waitForAll(t, 10*time.Second, "No tasks in this working directory yet.", "new task")
	session.send(t, "\r")
	session.waitForAll(t, 5*time.Second, "New Task", "Describe your task")
	session.submitNewTask(t, longDescription)

	session.waitForAll(t, 10*time.Second, "approve_plan", "awaiting approval", "Ctrl+C quit", "Enter confirm")

	session.resetOutput()
	session.confirm(t)
	session.resize(t, 150, 39)

	session.waitForAll(t, 10*time.Second, "Task failed", "Retry step", "Ctrl+C quit")

	session.send(t, "\r")

	session.waitForAll(t, 5*time.Second, "implement", "elapsed:", "Ctrl+C quit")

	session.quit(t)
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

	cmdArgs := append([]string(nil), args...)
	cmd := exec.Command(binaryPath, cmdArgs...)
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

func writeOverrideConfig(t *testing.T, workDir, fileName string, cfg *taskconfig.Config) string {
	t.Helper()
	configDir := filepath.Join(workDir, ".e2e-config")
	require.NoError(t, os.MkdirAll(filepath.Join(configDir, "prompts"), 0o755))
	for name, def := range cfg.NodeDefinitions {
		if def.Type == taskconfig.NodeTypeHuman || def.Type == taskconfig.NodeTypeTerminal {
			continue
		}
		if def.SystemPrompt == "" {
			def.SystemPrompt = "./prompts/" + name + ".md"
			cfg.NodeDefinitions[name] = def
		}
		promptPath := filepath.Join(configDir, strings.TrimPrefix(def.SystemPrompt, "./"))
		require.NoError(t, os.MkdirAll(filepath.Dir(promptPath), 0o755))
		require.NoError(t, os.WriteFile(promptPath, []byte("# "+name), 0o644))
	}
	data, err := yaml.Marshal(cfg)
	require.NoError(t, err)
	configPath := filepath.Join(configDir, fileName)
	require.NoError(t, os.WriteFile(configPath, data, 0o644))
	return configPath
}

func installDefaultTaskConfigRegistryEntry(t *testing.T, homeDir, alias, sourcePath string) {
	t.Helper()
	installTaskConfigRegistryEntries(t, homeDir, alias, map[string]string{alias: sourcePath})
}

func installManagedDefaultConfig(t *testing.T, homeDir, sourcePath string) string {
	t.Helper()
	taskConfigDir, err := taskconfig.TaskConfigDir()
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(taskConfigDir, homeDir))

	destDir := filepath.Join(taskConfigDir, taskconfig.DefaultAlias)
	return installConfigBundle(t, sourcePath, destDir)
}

func installTaskConfigRegistryEntries(t *testing.T, homeDir, defaultAlias string, sources map[string]string) map[string]string {
	t.Helper()
	taskConfigDir, err := taskconfig.TaskConfigDir()
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(taskConfigDir, homeDir))

	entries := make([]taskconfig.RegistryEntry, 0, len(sources))
	installedPaths := make(map[string]string, len(sources))
	for alias, sourcePath := range sources {
		destDir := filepath.Join(taskConfigDir, alias)
		installedPaths[alias] = installConfigBundle(t, sourcePath, destDir)
		entries = append(entries, taskconfig.RegistryEntry{
			Alias: alias,
			Path:  filepath.ToSlash(alias),
		})
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Alias < entries[j].Alias
	})

	_, err = taskconfig.SaveRegistry(taskconfig.Registry{
		DefaultAlias: defaultAlias,
		Configs:      entries,
	})
	require.NoError(t, err)
	return installedPaths
}

func installConfigBundle(t *testing.T, sourceConfigPath, destDir string) string {
	t.Helper()
	sourceDir := filepath.Dir(sourceConfigPath)
	copyTree(t, sourceDir, destDir)
	destConfigPath := filepath.Join(destDir, "config.yaml")
	if filepath.Base(sourceConfigPath) != "config.yaml" {
		sourceData, err := os.ReadFile(sourceConfigPath)
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(destConfigPath, sourceData, 0o644))
	}
	return destConfigPath
}

func copyTree(t *testing.T, sourceDir, destDir string) {
	t.Helper()
	require.NoError(t, filepath.Walk(sourceDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(sourceDir, path)
		if err != nil {
			return err
		}
		target := filepath.Join(destDir, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	}))
}

func initTaskTUIE2EGitRepo(t *testing.T, includeSubdir bool) string {
	t.Helper()

	repo := canonicalPath(t, t.TempDir())
	runTaskTUIE2EGit(t, repo, "git", "init")
	runTaskTUIE2EGit(t, repo, "git", "config", "user.email", "test@test.com")
	runTaskTUIE2EGit(t, repo, "git", "config", "user.name", "Test")
	require.NoError(t, os.WriteFile(filepath.Join(repo, "README.md"), []byte("hello"), 0o644))
	if includeSubdir {
		subdir := filepath.Join(repo, "packages", "app")
		require.NoError(t, os.MkdirAll(subdir, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(subdir, ".keep"), []byte("keep"), 0o644))
	}
	runTaskTUIE2EGit(t, repo, "git", "add", ".")
	runTaskTUIE2EGit(t, repo, "git", "commit", "-m", "init")
	return repo
}

func runTaskTUIE2EGit(t *testing.T, dir string, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "command %s %v failed: %s", name, args, string(out))
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

func (s *tuiSession) sendBacktab(t *testing.T) {
	t.Helper()
	s.send(t, "\x1b[Z")
}

func (s *tuiSession) pause(delay time.Duration) {
	time.Sleep(delay)
}

func (s *tuiSession) resize(t *testing.T, cols, rows uint16) {
	t.Helper()
	require.NoError(t, pty.Setsize(s.ptmx, &pty.Winsize{Rows: rows, Cols: cols}))
	time.Sleep(150 * time.Millisecond)
}

func (s *tuiSession) confirm(t *testing.T) {
	t.Helper()
	s.send(t, "\r")
	time.Sleep(250 * time.Millisecond)
	s.send(t, "\r")
}

func (s *tuiSession) submitNewTask(t *testing.T, description string) {
	t.Helper()
	time.Sleep(150 * time.Millisecond)
	for _, r := range description {
		s.send(t, string(r))
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(100 * time.Millisecond)
	s.send(t, "\t")
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

func (s *tuiSession) resetOutput() {
	s.bufferMu.Lock()
	defer s.bufferMu.Unlock()
	s.buffer.Reset()
}

func (s *tuiSession) quit(t *testing.T) {
	t.Helper()
	if s.cmd.ProcessState != nil && s.cmd.ProcessState.Exited() {
		return
	}
	s.send(t, "\x03")
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case err := <-s.exitCh:
			require.NoError(t, err)
			require.NoError(t, s.ptmx.Close())
			return
		default:
		}
		if strings.Contains(s.output(), "Quit muxagent?") {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if s.cmd.ProcessState == nil || !s.cmd.ProcessState.Exited() {
		if strings.Contains(s.output(), "Quit muxagent?") {
			s.send(t, "\t")
			time.Sleep(100 * time.Millisecond)
			s.send(t, "\r")
		} else {
			s.send(t, "\x03")
		}
	}
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
	blockedSteps, err := taskengine.DeriveBlockedSteps(cfg, runs)
	if err != nil {
		return taskdomain.Task{}, nil, taskdomain.TaskView{}, err
	}
	return task, runs, taskdomain.DeriveTaskView(task, cfg, runs, blockedSteps), nil
}

func loadTaskRecords(workDir string) ([]taskdomain.Task, error) {
	store, err := taskstore.Open(workDir)
	if err != nil {
		return nil, err
	}
	defer store.Close()
	return store.ListTasksByWorkDir(context.Background(), workDir)
}

func registryEntry(entries []taskconfig.RegistryEntry, alias string) (taskconfig.RegistryEntry, bool) {
	for _, entry := range entries {
		if entry.Alias == alias {
			return entry, true
		}
	}
	return taskconfig.RegistryEntry{}, false
}

func assertNodeRunCounts(t *testing.T, runs []taskdomain.NodeRun, want map[string]int) {
	t.Helper()
	got := map[string]int{}
	for _, run := range runs {
		got[run.NodeName]++
	}
	assert.Equal(t, want, got)
}

func assertHumanAuditArtifact(t *testing.T, task taskdomain.Task, runs []taskdomain.NodeRun, run taskdomain.NodeRun) {
	t.Helper()
	paths := taskdomain.ArtifactPaths(run.Result)
	require.Empty(t, paths)
	inputPath := mustRunAuditPath(t, task.WorkDir, task, runs, run, "input.md")
	outputPath := mustRunAuditPath(t, task.WorkDir, task, runs, run, "output.json")
	var names []string
	for _, path := range []string{inputPath, outputPath} {
		assert.FileExists(t, path)
		names = append(names, filepath.Base(path))
	}
	sort.Strings(names)
	assert.Equal(t, []string{"input.md", "output.json"}, names)
}

func requireNodeRunByName(t *testing.T, runs []taskdomain.NodeRun, name string) taskdomain.NodeRun {
	t.Helper()
	for _, run := range runs {
		if run.NodeName == name {
			return run
		}
	}
	t.Fatalf("node run %q not found", name)
	return taskdomain.NodeRun{}
}

func findArtifactPathByBase(t *testing.T, paths []string, base string) string {
	t.Helper()
	for _, path := range paths {
		if filepath.Base(path) == base {
			return path
		}
	}
	t.Fatalf("artifact %q not found in %v", base, paths)
	return ""
}

func assertArtifactPathsExcludeRuntimeAudit(t *testing.T, paths []string) {
	t.Helper()
	for _, path := range paths {
		base := filepath.Base(path)
		assert.NotEqual(t, "input.md", base)
		assert.NotEqual(t, "output.json", base)
	}
}

func assertPromptContainsLiteralTemplateLines(t *testing.T, input, templatePath string) {
	t.Helper()
	templateBytes, err := os.ReadFile(templatePath)
	require.NoError(t, err)
	for _, line := range strings.Split(string(templateBytes), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if strings.Contains(line, "{{") {
			continue
		}
		assert.Contains(t, input, line)
	}
}

func mustRunAuditPath(t *testing.T, workDir string, task taskdomain.Task, runs []taskdomain.NodeRun, run taskdomain.NodeRun, name string) string {
	t.Helper()
	sequence := nodeRunSequenceForTest(t, runs, run.ID)
	return filepath.Join(taskstore.ArtifactRunDir(workDir, task.ID, sequence, run.NodeName), name)
}

func nodeRunSequenceForTest(t *testing.T, runs []taskdomain.NodeRun, runID string) int {
	t.Helper()
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
	t.Fatalf("node run %q not found", runID)
	return 0
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
}

func assertSchemaFiles(t *testing.T, task taskdomain.Task, want []string) {
	t.Helper()
	schemaRoot := filepath.Join(taskstore.TaskDir(task.WorkDir, task.ID), "schemas")
	entries, err := os.ReadDir(schemaRoot)
	require.NoError(t, err)
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	assert.Equal(t, want, names)
}

func assertPromptFiles(t *testing.T, workDir, taskID string, want []string, requireHeader bool) {
	t.Helper()
	promptDir := filepath.Join(taskstore.TaskDir(workDir, taskID), "prompts")
	var names []string
	err := filepath.Walk(promptDir, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() {
			return nil
		}
		relPath, err := filepath.Rel(promptDir, path)
		if err != nil {
			return err
		}
		relPath = filepath.ToSlash(relPath)
		names = append(names, relPath)
		if requireHeader {
			data, err := os.ReadFile(path)
			require.NoError(t, err)
			assert.Contains(t, string(data), "Step: {{NODE_NAME}}")
			assert.Contains(t, string(data), "ArtifactDir: {{ARTIFACT_DIR}}")
			assert.Contains(t, string(data), "Iteration: {{CURRENT_ITERATION}}")
		}
		return nil
	})
	require.NoError(t, err)
	sort.Strings(names)
	assert.Equal(t, want, names)
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

func singleAgentTerminalConfig(runtime appconfig.RuntimeID) *taskconfig.Config {
	deny := false
	return &taskconfig.Config{
		Version: 1,
		Runtime: runtime,
		Clarification: taskconfig.ClarificationConfig{
			MaxQuestions:          4,
			MaxOptionsPerQuestion: 4,
			MinOptionsPerQuestion: 2,
		},
		Topology: taskconfig.Topology{
			MaxIterations: 1,
			Entry:         "implement",
			Nodes: []taskconfig.NodeRef{
				{Name: "implement"},
				{Name: "done"},
			},
			Edges: []taskconfig.Edge{
				{From: "implement", To: "done"},
			},
		},
		NodeDefinitions: map[string]taskconfig.NodeDefinition{
			"implement": {
				Type:         taskconfig.NodeTypeAgent,
				SystemPrompt: "./prompts/implement.md",
				ResultSchema: taskconfig.JSONSchema{
					Type:                 "object",
					AdditionalProperties: &deny,
					Required:             []string{"file_paths"},
					Properties: map[string]*taskconfig.JSONSchema{
						"file_paths": {
							Type:  "array",
							Items: &taskconfig.JSONSchema{Type: "string"},
						},
					},
				},
			},
			"done": {Type: taskconfig.NodeTypeTerminal},
		},
	}
}

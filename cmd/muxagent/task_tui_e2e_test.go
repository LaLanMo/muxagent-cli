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

	tests := []struct {
		name              string
		flow              string
		description       string
		cliArgs           []string
		configPath        func(t *testing.T, workDir string) string
		drive             func(t *testing.T, session *tuiSession)
		expectedArtifacts []string
		verify            func(t *testing.T, task taskdomain.Task, runs []taskdomain.NodeRun, view taskdomain.TaskView)
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
				session.waitForAll(t, 10*time.Second, "No tasks in this working directory yet.", "new task")
				session.send(t, "\r")
				session.waitForAll(t, 5*time.Second, "New Task", "Describe your task")
				session.submitNewTask(t, "Review rejects once")
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
				session.waitForAll(t, 10*time.Second, "No tasks in this working directory yet.", "new task")
				session.send(t, "\r")
				session.waitForAll(t, 5*time.Second, "New Task", "runtime claude-code")
				session.submitNewTask(t, "Need clarification with Claude")
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
			assertPromptFiles(t, workDir, task.ID)
			tt.verify(t, task, runs, view)

			session.quit(t)
		})
	}
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
	session.send(t, "\x0e")
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
	session.waitForAll(t, 10*time.Second, "Task Configs", "default")

	session.send(t, "n")
	session.waitForAll(t, 5*time.Second, "Clone Task Config", "Source config default")
	session.send(t, "reviewer")
	session.send(t, "\r")
	session.waitForAll(t, 10*time.Second, "reviewer", "runtime Codex")

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
	session.waitForAll(t, 5*time.Second, "Task Configs", "default")

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
	session.waitForAll(t, 5*time.Second, "2 artifacts")
	session.resetOutput()

	// Switch to artifacts tab via '2' key
	session.send(t, "2")
	session.waitForAll(t, 5*time.Second, "Files", "Preview ·")
	session.resetOutput()

	// Press 1 to return to timeline tab (footer shows "2 artifacts" on timeline)
	session.send(t, "1")
	session.waitForAll(t, 5*time.Second, "2 artifacts")

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

	session.waitForAll(t, 15*time.Second, "Task completed successfully", "2 artifacts", "Esc back")
	session.resetOutput()

	// Switch to artifacts tab and verify content
	session.send(t, "2")
	session.waitForAll(t, 5*time.Second, "Files", "Preview ·")
	session.resetOutput()

	// Switch back to timeline, then go back to task list
	session.send(t, "1")
	session.pause(150 * time.Millisecond)
	session.send(t, "\x1b")
	session.waitForAll(t, 5*time.Second, "new task", "done Wide completed artifacts")

	// Re-open the task and switch to artifacts
	session.resetOutput()
	session.send(t, "\r")
	session.waitForAll(t, 5*time.Second, "Task completed successfully", "2 artifacts")
	session.resetOutput()
	session.send(t, "2")
	session.waitForAll(t, 5*time.Second, "Files", "Preview ·")

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

	session.waitForAll(t, 15*time.Second, "Task completed successfully", "2 artifacts", "Esc back")
	session.resetOutput()

	// Switch to artifacts tab
	session.send(t, "2")
	session.waitForAll(t, 5*time.Second, "Files", "Preview ·")
	session.resetOutput()

	// Press 1 back to timeline, then esc to task list
	session.send(t, "1")
	session.waitForAll(t, 5*time.Second, "2 artifacts", "Esc back")
	session.resetOutput()

	session.send(t, "\x1b")
	session.waitForAll(t, 5*time.Second, "new task", "done Small completed artifacts")

	// Re-open and verify footer hint
	session.resetOutput()
	session.send(t, "\r")
	session.waitForAll(t, 5*time.Second, "2 artifacts", "Esc back")

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

	output := session.waitForAll(t, 10*time.Second, "implement", "awaiting input", "Question 1/1", "2 artifacts")
	assert.Contains(t, output, "Write your own answer")
	assert.Contains(t, output, "Submit answers")
	assert.NotContains(t, output, "[ ] Other")

	// Switch to artifacts tab via '2' key
	session.resetOutput()
	session.send(t, "2")
	session.waitForAll(t, 5*time.Second, "Files", "Preview ·")

	// Switch back to timeline. The clarification panel is identical on both
	// tabs, so bubbletea's differential renderer won't re-send those lines.
	// A resize forces a full repaint so we can assert on the panel content.
	session.resetOutput()
	session.send(t, "1")
	session.resize(t, 150, 39)
	session.waitForAll(t, 5*time.Second, "2 artifacts", "Question 1/1")

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

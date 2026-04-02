//go:build !windows

package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
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
	defaultPromptFiles := []string{"draft_plan.md", "implement.md", "review_plan.md", "verify.md"}
	defaultSchemaFiles := []string{"draft_plan.json", "implement.json", "review_plan.json", "verify.json"}
	singleRunPromptFiles := []string{"handle_request.md"}
	singleRunSchemaFiles := []string{"handle_request.json"}
	yoloPromptFiles := []string{"yolo_draft_plan.md", "yolo_evaluate_progress.md", "yolo_implement.md", "yolo_review_plan.md", "yolo_verify.md"}
	yoloSchemaFiles := []string{"draft_plan.json", "evaluate_progress.json", "implement.json", "review_plan.json", "verify.json"}

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
				session.approvePlan(t)
			},
			expectedArtifacts:   []string{"01-draft_plan", "02-review_plan", "03-approve_plan", "04-implement", "05-verify"},
			requirePromptHeader: true,
			verify: func(t *testing.T, task taskdomain.Task, runs []taskdomain.NodeRun, view taskdomain.TaskView) {
				require.Len(t, runs, 6)
				assert.Equal(t, taskdomain.TaskStatusDone, view.Status)
				assert.Equal(t, "done", view.CurrentNodeName)
				assertNodeRunCounts(t, runs, map[string]int{
					"draft_plan":   1,
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
					case "draft_plan", "review_plan", "implement", "verify":
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
			expectedArtifacts:   []string{"01-draft_plan", "02-review_plan"},
			expectedPrompts:     []string{"draft_plan.md", "review_plan.md"},
			expectedSchemas:     []string{"draft_plan.json", "review_plan.json"},
			requirePromptHeader: true,
			verify: func(t *testing.T, task taskdomain.Task, runs []taskdomain.NodeRun, view taskdomain.TaskView) {
				require.Len(t, runs, 3)
				assert.Equal(t, taskdomain.TaskStatusDone, view.Status)
				assertNodeRunCounts(t, runs, map[string]int{
					"draft_plan":  1,
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
			name:        "single-run handles one request and stops",
			flow:        "happy",
			description: "Summarize the current issue once",
			drive: func(t *testing.T, session *tuiSession) {
				session.waitForAll(t, 10*time.Second, "No tasks in this working directory yet.", "new task")
				session.send(t, "\r")
				session.waitForAll(t, 5*time.Second, "New Task", "Describe your task")
				session.send(t, "\x0e")
				session.waitForAll(t, 5*time.Second, "plan-only")
				session.send(t, "\x0e")
				session.waitForAll(t, 5*time.Second, "single-run")
				session.submitNewTask(t, "Summarize the current issue once")
				session.waitForAll(t, 10*time.Second, "Task completed successfully")
			},
			expectedArtifacts:   []string{"01-handle_request"},
			expectedPrompts:     singleRunPromptFiles,
			expectedSchemas:     singleRunSchemaFiles,
			requirePromptHeader: true,
			verify: func(t *testing.T, task taskdomain.Task, runs []taskdomain.NodeRun, view taskdomain.TaskView) {
				require.Len(t, runs, 2)
				assert.Equal(t, taskdomain.TaskStatusDone, view.Status)
				assert.Equal(t, "done", view.CurrentNodeName)
				assert.Equal(t, taskconfig.BuiltinIDSingleRun, task.ConfigAlias)
				assertNodeRunCounts(t, runs, map[string]int{
					"handle_request": 1,
					"done":           1,
				})
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
				session.waitForAll(t, 5*time.Second, "single-run")
				session.send(t, "\x0e")
				session.waitForAll(t, 5*time.Second, "autonomous")
				session.submitNewTask(t, "Fix the flaky flow autonomously")
				session.waitForAll(t, 10*time.Second, "Task completed successfully")
			},
			expectedArtifacts:   []string{"01-draft_plan", "02-review_plan", "03-implement", "04-verify", "05-implement", "06-verify"},
			expectedPrompts:     defaultPromptFiles,
			expectedSchemas:     defaultSchemaFiles,
			requirePromptHeader: true,
			verify: func(t *testing.T, task taskdomain.Task, runs []taskdomain.NodeRun, view taskdomain.TaskView) {
				require.Len(t, runs, 7)
				assert.Equal(t, taskdomain.TaskStatusDone, view.Status)
				assertNodeRunCounts(t, runs, map[string]int{
					"draft_plan":  1,
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
				session.waitForAll(t, 5*time.Second, "single-run")
				session.send(t, "\x0e")
				session.waitForAll(t, 5*time.Second, "autonomous")
				session.send(t, "\x0e")
				session.waitForAll(t, 5*time.Second, "yolo")
				session.submitNewTask(t, "Finish the task over multiple autonomous waves")
				session.waitForAll(t, 10*time.Second, "Task completed successfully")
			},
			expectedArtifacts:   []string{"01-draft_plan", "02-review_plan", "03-implement", "04-verify", "05-evaluate_progress", "06-draft_plan", "07-review_plan", "08-implement", "09-verify", "10-evaluate_progress"},
			expectedPrompts:     yoloPromptFiles,
			expectedSchemas:     yoloSchemaFiles,
			requirePromptHeader: true,
			verify: func(t *testing.T, task taskdomain.Task, runs []taskdomain.NodeRun, view taskdomain.TaskView) {
				require.Len(t, runs, 11)
				assert.Equal(t, taskdomain.TaskStatusDone, view.Status)
				assertNodeRunCounts(t, runs, map[string]int{
					"draft_plan":        2,
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
				session.approvePlan(t)
			},
			expectedArtifacts:   []string{"01-draft_plan", "02-review_plan", "03-approve_plan", "04-implement", "05-verify"},
			requirePromptHeader: true,
			verify: func(t *testing.T, task taskdomain.Task, runs []taskdomain.NodeRun, view taskdomain.TaskView) {
				require.Len(t, runs, 6)
				assert.Equal(t, taskdomain.TaskStatusDone, view.Status)
				assertNodeRunCounts(t, runs, map[string]int{
					"draft_plan":   1,
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
				_ = session.focusApprovalActionPanel(t)
				session.send(t, "\x1b[B")
				session.waitForOutputIdle(200 * time.Millisecond)
				session.send(t, "\x1b[B")
				session.waitForOutputIdle(200 * time.Millisecond)
				session.send(t, "Need more detail")
				session.waitForOutputIdle(200 * time.Millisecond)
				session.send(t, "\x1b[A")
				session.waitForOutputIdle(200 * time.Millisecond)
				session.send(t, "\r")
				session.resetOutput()
				session.approvePlan(t)
			},
			expectedArtifacts: []string{"01-draft_plan", "02-review_plan", "03-approve_plan", "04-draft_plan", "05-review_plan", "06-approve_plan", "07-implement", "08-verify"},
			verify: func(t *testing.T, task taskdomain.Task, runs []taskdomain.NodeRun, view taskdomain.TaskView) {
				require.Len(t, runs, 9)
				assert.Equal(t, taskdomain.TaskStatusDone, view.Status)
				assertNodeRunCounts(t, runs, map[string]int{
					"draft_plan":   2,
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
				session.waitForAll(t, 10*time.Second, "draft_plan", "awaiting input")
				session.confirm(t)
				session.approvePlan(t)
			},
			expectedArtifacts: []string{"01-draft_plan", "02-review_plan", "03-approve_plan", "04-implement", "05-verify"},
			verify: func(t *testing.T, task taskdomain.Task, runs []taskdomain.NodeRun, view taskdomain.TaskView) {
				require.Len(t, runs, 6)
				assert.Equal(t, taskdomain.TaskStatusDone, view.Status)
				assertNodeRunCounts(t, runs, map[string]int{
					"draft_plan":   1,
					"review_plan":  1,
					"approve_plan": 1,
					"implement":    1,
					"verify":       1,
					"done":         1,
				})
				for _, run := range runs {
					if run.NodeName == "draft_plan" {
						require.Len(t, run.Clarifications, 1)
						require.NotNil(t, run.Clarifications[0].Response)
						assert.Equal(t, "A", run.Clarifications[0].Response.Answers[0].Selected)
						assert.Equal(t, "thread-draft_plan-1", run.SessionID)
					}
					if run.NodeName == "approve_plan" {
						assertHumanAuditArtifact(t, task, runs, run)
					}
					assert.Equal(t, task.ID, run.TaskID)
				}
			},
		},
		{
			name:        "clarification multi-question navigation and submit guard",
			flow:        "clarify-multi",
			description: "Need multi clarification",
			drive: func(t *testing.T, session *tuiSession) {
				session.resize(t, 120, 32)
				session.waitForAll(t, 10*time.Second, "No tasks in this working directory yet.", "new task")
				session.send(t, "\r")
				session.waitForAll(t, 5*time.Second, "New Task", "Describe your task")
				session.submitNewTask(t, "Need multi clarification")
				session.waitForAll(t, 10*time.Second, "draft_plan", "awaiting input", "Question 1/2", "Submit")

				session.sendAndWait(t, "\x1b[C", 5*time.Second)
				session.sendAndWait(t, "\x1b[B", 5*time.Second)
				session.sendAndWait(t, "\x1b[B", 5*time.Second)
				session.waitForAll(t, 5*time.Second, "Ctrl+P/N questions")
				session.sendAndWait(t, "Need docs", 5*time.Second)
				session.sendAndWait(t, "\x10", 5*time.Second)
				session.sendAndWait(t, "\x0e", 5*time.Second)
				session.sendAndWait(t, "\x1b[A", 5*time.Second)
				session.sendAndWait(t, "\r", 5*time.Second)
				session.sendAndWait(t, "\x1b[C", 5*time.Second)
				session.sendAndWaitForAll(t, "\r", 5*time.Second, "Answer every question before submitting.")
				session.sendAndWait(t, "\r", 5*time.Second)
				session.sendAndWait(t, "\x1b[C", 5*time.Second)
				session.send(t, "\r")

				session.approvePlan(t)
			},
			expectedArtifacts: []string{"01-draft_plan", "02-review_plan", "03-approve_plan", "04-implement", "05-verify"},
			verify: func(t *testing.T, task taskdomain.Task, runs []taskdomain.NodeRun, view taskdomain.TaskView) {
				require.Len(t, runs, 6)
				assert.Equal(t, taskdomain.TaskStatusDone, view.Status)
				assertNodeRunCounts(t, runs, map[string]int{
					"draft_plan":   1,
					"review_plan":  1,
					"approve_plan": 1,
					"implement":    1,
					"verify":       1,
					"done":         1,
				})
				for _, run := range runs {
					if run.NodeName != "draft_plan" {
						if run.NodeName == "approve_plan" {
							assertHumanAuditArtifact(t, task, runs, run)
						}
						continue
					}
					require.Len(t, run.Clarifications, 1)
					require.NotNil(t, run.Clarifications[0].Response)
					require.Len(t, run.Clarifications[0].Response.Answers, 2)
					assert.Equal(t, "A", run.Clarifications[0].Response.Answers[0].Selected)
					assert.Equal(t, "Strict", run.Clarifications[0].Response.Answers[1].Selected)
					assert.Equal(t, "thread-draft_plan-1", run.SessionID)
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
				session.approvePlan(t)
			},
			expectedArtifacts:   []string{"01-draft_plan", "02-review_plan", "03-draft_plan", "04-review_plan", "05-approve_plan", "06-implement", "07-verify"},
			requirePromptHeader: true,
			verify: func(t *testing.T, task taskdomain.Task, runs []taskdomain.NodeRun, view taskdomain.TaskView) {
				require.Len(t, runs, 8)
				assert.Equal(t, taskdomain.TaskStatusDone, view.Status)
				assertNodeRunCounts(t, runs, map[string]int{
					"draft_plan":   2,
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
					if cfg.Topology.Nodes[i].Name == "draft_plan" {
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
				session.approvePlan(t)
			},
			expectedArtifacts:   []string{"01-draft_plan", "02-review_plan", "03-draft_plan", "04-review_plan", "05-approve_plan", "06-implement", "07-verify"},
			requirePromptHeader: false,
			verify: func(t *testing.T, task taskdomain.Task, runs []taskdomain.NodeRun, view taskdomain.TaskView) {
				require.Len(t, runs, 8)
				assert.Equal(t, taskdomain.TaskStatusDone, view.Status)
				assertNodeRunCounts(t, runs, map[string]int{
					"draft_plan":   2,
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
				session.approvePlan(t)
				session.waitForAll(t, 10*time.Second, "Task failed", "Retry step")
				session.send(t, "\r")
			},
			expectedArtifacts:   []string{"01-draft_plan", "02-review_plan", "03-approve_plan", "04-implement", "05-implement", "06-verify"},
			requirePromptHeader: true,
			verify: func(t *testing.T, task taskdomain.Task, runs []taskdomain.NodeRun, view taskdomain.TaskView) {
				require.Len(t, runs, 7)
				assert.Equal(t, taskdomain.TaskStatusDone, view.Status)
				assertNodeRunCounts(t, runs, map[string]int{
					"draft_plan":   1,
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
			session.waitForAll(t, 20*time.Second, "Task completed successfully")
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
	session.approvePlan(t)
	session.waitForAll(t, 20*time.Second, "Task completed successfully")

	_, runs, view := waitForPersistedTask(t, workDir, taskdomain.TaskStatusDone)
	assert.Equal(t, taskdomain.TaskStatusDone, view.Status)

	draftRun := requireNodeRunByName(t, runs, "draft_plan")
	inputPath := mustRunAuditPath(t, workDir, taskdomain.Task{ID: view.Task.ID, WorkDir: workDir}, runs, draftRun, "input.md")
	inputBytes, err := os.ReadFile(inputPath)
	require.NoError(t, err)

	capturedPromptPath := filepath.Join(stateDir, "01-draft_plan.prompt.txt")
	capturedPromptBytes, err := os.ReadFile(capturedPromptPath)
	require.NoError(t, err)
	assert.Equal(t, string(capturedPromptBytes), string(inputBytes))
	assertArtifactPathsExcludeRuntimeAudit(t, view.ArtifactPaths)

	templatePath := filepath.Join(moduleRoot, "internal", "taskconfig", "defaults", "prompts", "draft_plan.md")
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
	before := session.markOutput()
	session.send(t, "\x1b")
	session.waitForFreshAll(t, 5*time.Second, before, "new task", "done Default config task")

	session.sendAndWait(t, "\x1b[A", 5*time.Second)
	session.sendAndWait(t, "\x1b[A", 5*time.Second)
	session.send(t, "\r")
	session.waitForAll(t, 5*time.Second, "New Task", "config default")
	session.send(t, "\x10")
	before = session.markOutput()
	session.resize(t, 140, 40)
	session.waitForFreshAll(t, 5*time.Second, before, "reviewer", "runtime claude-code")
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
	session.approvePlan(t)
	session.waitForAll(t, 20*time.Second, "Task completed successfully")

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
	restarted.sendAndWait(t, "\x1b[B", 5*time.Second)
	restarted.sendAndWait(t, "\x1b[B", 5*time.Second)
	restarted.send(t, "\r")
	restarted.resetOutput()
	restarted.waitForAll(t, 5*time.Second, "Task: Worktree-backed task", "done", "worktree", "✓ draft_plan")
	assert.NotContains(t, restarted.output(), "→")
	restarted.send(t, "\x1b")
	restarted.resetOutput()
	restarted.waitForAll(t, 5*time.Second, "done Worktree-backed task", "worktree", "new task")
	for i := 0; i < 2; i++ {
		restarted.sendAndWait(t, "\x1b[A", 5*time.Second)
	}
	restarted.send(t, "\r")
	restarted.resetOutput()
	restarted.waitForAll(t, 5*time.Second, "New Task", "worktree on", "Ctrl+T worktree off")
	restarted.quit(t)
}

func TestTaskTUIConfigScreenCanToggleRuntimeAndPersistAcrossRestart(t *testing.T) {
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

	session := startTUISession(t, binaryPath, workDir)
	session.waitForAll(t, 10*time.Second, "new task", "task configs")
	session.send(t, "\x1b[B")
	session.send(t, "\r")
	output := session.waitForAll(t, 10*time.Second, "Task Configs", "default", "Codex", "Shift+Tab runtime Claude Code")
	assert.NotContains(t, output, "n clone")

	session.sendBacktab(t)
	session.waitForAll(t, 10*time.Second, `config "default" runtime is now Claude Code`)

	require.Eventually(t, func() bool {
		catalog, err := taskconfig.LoadCatalog()
		if err != nil {
			return false
		}
		entry, ok := catalog.Entry(taskconfig.DefaultAlias)
		if !ok {
			return false
		}
		cfg, err := entry.LoadConfig()
		return err == nil && cfg.Runtime == appconfig.RuntimeClaudeCode
	}, 5*time.Second, 100*time.Millisecond)

	session.forceClose()

	restarted := startTUISession(t, binaryPath, workDir)
	restarted.waitForAll(t, 10*time.Second, "new task", "task configs")
	restarted.send(t, "\x1b[B")
	restarted.send(t, "\r")
	restarted.waitForAll(t, 10*time.Second, "Task Configs", "default", "Claude Code", "Shift+Tab runtime Codex")
	restarted.forceClose()
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
	session.waitForAll(t, 10*time.Second, "Task: Stay on list", "draft_plan")
	session.send(t, "\x1b")
	session.resetOutput()
	session.waitForAll(t, 5*time.Second, "new task", "running Stay on list")
	session.resetOutput()
	session.resize(t, 140, 40)
	session.waitForAll(t, 5*time.Second, "Stay on list", "awaiting approval")

	output := session.output()
	assert.NotContains(t, output, "Approve this plan?")
	assert.NotContains(t, output, "Artifacts (")

	task, runs, view := waitForPersistedTask(t, workDir, taskdomain.TaskStatusAwaitingUser)
	assert.Equal(t, "Stay on list", task.Description)
	assert.Equal(t, taskdomain.TaskStatusAwaitingUser, view.Status)
	assertNodeRunCounts(t, runs, map[string]int{
		"draft_plan":   1,
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

func TestTaskTUIRendersCodexWebSearchAsFetchInLiveOutput(t *testing.T) {
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
	t.Setenv("FAKE_CODEX_FLOW", "web-search")
	t.Setenv("FAKE_CODEX_STATE_DIR", filepath.Join(workDir, ".fake-codex-state"))
	t.Setenv("TERM", "xterm-256color")

	session := startTUISession(t, binaryPath, workDir)
	session.resize(t, 140, 40)
	session.waitForAll(t, 10*time.Second, "No tasks in this working directory yet.", "new task")
	session.send(t, "\r")
	session.waitForAll(t, 5*time.Second, "New Task", "Describe your task")
	session.submitNewTask(t, "Inspect release search")
	session.waitForAll(t, 10*time.Second, "Task: Inspect release search", "draft_plan", "Output · draft_plan", "thread: thread-draft_plan-1", "fetch", "latest github release announcement")

	output := session.output()
	assert.Contains(t, output, "fetch")
	assert.Contains(t, output, "latest github release announcement")
	assert.NotContains(t, output, `"type":"item.started","item":{"id":"ws_123"`)
	assert.NotContains(t, output, `"type":"item.completed","item":{"id":"ws_123"`)
	assert.NotContains(t, output, "web_search")

	session.forceClose()
}

func TestTaskTUINarrowTerminalKeepsNewTaskStartHintVisible(t *testing.T) {
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
	session.resize(t, 40, 24)
	session.waitForAll(t, 10*time.Second, "new task", "task configs")
	session.send(t, "\r")
	session.waitForAll(t, 5*time.Second, "New Task", "Task description", "Enter start", "Ctrl+C quit")

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
	session.waitForAll(t, 10*time.Second, "approve_plan", "awaiting approval", "Shift+Tab timeline", "Files", "Preview ·")
	session.resetOutput()

	// Switch to timeline via Shift+Tab
	session.sendBacktab(t)
	session.waitForAll(t, 5*time.Second, "Shift+Tab artifacts")
	session.resetOutput()

	// Press Shift+Tab to return to artifacts tab
	session.sendBacktab(t)
	session.waitForAll(t, 5*time.Second, "Shift+Tab timeline", "Files", "Preview ·")

	session.quit(t)
}

func TestTaskTUIArtifactFilesPaneShowsSuffixVisiblePaths(t *testing.T) {
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
	session.resize(t, 104, 28)
	session.waitForAll(t, 10*time.Second, "No tasks in this working directory yet.", "new task")
	session.send(t, "\r")
	session.waitForAll(t, 5*time.Second, "New Task", "Describe your task")
	session.submitNewTask(t, "Show suffix-visible artifact rows")
	output := session.waitForAll(t, 10*time.Second, "approve_plan", "awaiting approval", "Shift+Tab timeline", "Files", "Preview ·", "draft_plan (#1)", "…01-draft_plan/plan-1.md")
	assert.Contains(t, output, "…01-draft_plan/plan-1.md")
	assert.Contains(t, output, "draft_plan (#1)")
	assert.NotContains(t, output, "Preview · 01-draft_plan/plan-1.md")

	session.quit(t)
}

func TestTaskTUIApprovalArtifactsFooterAndEnterGuard(t *testing.T) {
	moduleRoot := moduleRoot(t)
	binaryPath := buildMuxagentBinary(t, moduleRoot)

	workDir := canonicalPath(t, t.TempDir())
	clipboardPath := setupArtifactTUIRuntime(t, moduleRoot, workDir, "happy", true)

	session := startTUISession(t, binaryPath, workDir)
	session.resize(t, 120, 32)
	session.waitForAll(t, 10*time.Second, "No tasks in this working directory yet.", "new task")
	session.send(t, "\r")
	session.waitForAll(t, 5*time.Second, "New Task", "Describe your task")
	session.submitNewTask(t, "Approval artifacts")
	session.waitForAll(t, 10*time.Second, "approve_plan", "awaiting approval", "Shift+Tab timeline", "Files", "Preview ·")

	waitForNodeRunCounts(t, workDir, map[string]int{"draft_plan": 1, "review_plan": 1, "approve_plan": 1})
	_, _, view := waitForPersistedTask(t, workDir, taskdomain.TaskStatusAwaitingUser)

	before := session.markOutput()
	session.send(t, "\r")
	_, changed := session.waitForOutputChangeWithin(t, 750*time.Millisecond, before)
	assert.False(t, changed)
	assert.NotContains(t, session.output(), "Task completed successfully")

	_ = session.focusApprovalActionPanel(t)
	session.sendAndWaitForAll(t, "\t", 5*time.Second, "c copy path")
	session.send(t, "c")
	copiedPath := waitForClipboardContents(t, clipboardPath)
	assert.Contains(t, view.ArtifactPaths, copiedPath)

	session.sendAndWaitForAll(t, "\t", 5*time.Second, "Tab response")
	require.NoError(t, os.Remove(clipboardPath))
	session.send(t, "c")
	copiedContents := waitForClipboardContents(t, clipboardPath)
	wantContents, err := os.ReadFile(copiedPath)
	require.NoError(t, err)
	assert.Equal(t, string(wantContents), copiedContents)

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
	session.confirm(t)
	session.waitForAll(t, 5*time.Second, "New Task", "Describe your task")
	session.submitNewTask(t, "Wide completed artifacts")
	session.approvePlan(t)

	session.waitForAll(t, 15*time.Second, "Task completed successfully", "Shift+Tab artifacts", "Esc back")
	session.resetOutput()

	// Switch to artifacts tab and verify content
	session.sendBacktab(t)
	session.waitForAll(t, 5*time.Second, "Shift+Tab timeline", "Files", "Preview ·")
	session.resetOutput()

	// Switch back to timeline, then go back to task list
	session.sendAndWaitForAll(t, "\x1b[Z", 5*time.Second, "Shift+Tab artifacts", "Esc back")
	session.resetOutput()
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

func TestTaskTUICompletedArtifactsCopyPathAndContents(t *testing.T) {
	moduleRoot := moduleRoot(t)
	binaryPath := buildMuxagentBinary(t, moduleRoot)

	workDir := canonicalPath(t, t.TempDir())
	clipboardPath := setupArtifactTUIRuntime(t, moduleRoot, workDir, "happy", true)

	session := startTUISession(t, binaryPath, workDir)
	session.resize(t, 149, 39)
	session.waitForAll(t, 10*time.Second, "No tasks in this working directory yet.", "new task")
	session.send(t, "\r")
	session.waitForAll(t, 5*time.Second, "New Task", "Describe your task")
	session.submitNewTask(t, "Completed artifacts copy")
	session.approvePlan(t)
	session.waitForAll(t, 15*time.Second, "Task completed successfully", "Shift+Tab artifacts")

	_, _, view := waitForPersistedTask(t, workDir, taskdomain.TaskStatusDone)

	session.resetOutput()
	session.sendBacktab(t)
	session.waitForAll(t, 5*time.Second, "Shift+Tab timeline", "Tab artifacts", "Files", "Preview ·")

	session.send(t, "c")
	copiedPath := waitForClipboardContents(t, clipboardPath)
	assert.Contains(t, view.ArtifactPaths, copiedPath)

	session.sendAndWaitForAll(t, "\t", 5*time.Second, "Tab continue")
	require.NoError(t, os.Remove(clipboardPath))
	session.send(t, "c")
	copiedContents := waitForClipboardContents(t, clipboardPath)
	wantContents, err := os.ReadFile(copiedPath)
	require.NoError(t, err)
	assert.Equal(t, string(wantContents), copiedContents)

	session.quit(t)
}

func TestTaskTUICompletedTaskCanStartFollowUpEndToEnd(t *testing.T) {
	moduleRoot := moduleRoot(t)
	binaryPath := buildMuxagentBinary(t, moduleRoot)

	workDir := canonicalPath(t, t.TempDir())
	clipboardPath := setupArtifactTUIRuntime(t, moduleRoot, workDir, "happy", true)

	session := startTUISession(t, binaryPath, workDir)
	session.resize(t, 180, 39)
	session.waitForAll(t, 10*time.Second, "No tasks in this working directory yet.", "new task")
	session.send(t, "\r")
	session.waitForAll(t, 5*time.Second, "New Task", "Describe your task")
	session.submitNewTask(t, "Parent follow-up task")
	session.approvePlan(t)
	session.waitForAll(t, 15*time.Second, "Task completed successfully", "Continue from this task", "Tab continue")

	session.sendAndWait(t, "\t", 5*time.Second)
	session.typeText(t, "Child follow-up task")
	beforeChildApproval := session.resetOutput()
	session.send(t, "\r")
	output := session.waitForFreshAll(t, 10*time.Second, beforeChildApproval, "approve_plan", "awaiting approval", "Shift+Tab timeline", "Files", "Preview ·")

	tasks := waitForTaskCount(t, workDir, 2)
	parentTask := findTaskByDescription(t, tasks, "Parent follow-up task")
	childTask := findTaskByDescription(t, tasks, "Child follow-up task")
	_, parentRunsAtApproval, _, err := loadTaskStateByID(workDir, parentTask.ID)
	require.NoError(t, err)
	_, childRunsAtApproval, childViewAtApproval, err := loadTaskStateByID(workDir, childTask.ID)
	require.NoError(t, err)
	assert.Equal(t, taskdomain.TaskStatusAwaitingUser, childViewAtApproval.Status)

	parentDraftRunAtApproval := requireNodeRunByName(t, parentRunsAtApproval, "draft_plan")
	childDraftRunAtApproval := requireNodeRunByName(t, childRunsAtApproval, "draft_plan")
	childReviewRunAtApproval := requireNodeRunByName(t, childRunsAtApproval, "review_plan")

	parentDraftArtifacts := taskdomain.ArtifactPaths(parentDraftRunAtApproval.Result)
	childDraftArtifacts := taskdomain.ArtifactPaths(childDraftRunAtApproval.Result)
	childReviewArtifacts := taskdomain.ArtifactPaths(childReviewRunAtApproval.Result)
	require.NotEmpty(t, parentDraftArtifacts)
	require.NotEmpty(t, childDraftArtifacts)
	require.NotEmpty(t, childReviewArtifacts)

	assert.Contains(t, output, filepath.Base(childDraftArtifacts[0]))
	assert.Contains(t, output, filepath.Base(childReviewArtifacts[0]))
	assert.NotContains(t, output, filepath.Base(parentDraftArtifacts[0]))

	session.send(t, "c")
	copiedPath := waitForClipboardContents(t, clipboardPath)
	assert.Contains(t, copiedPath, taskstore.TaskDir(workDir, childTask.ID))
	assert.NotContains(t, copiedPath, taskstore.TaskDir(workDir, parentTask.ID))

	panelOutput := session.focusApprovalActionPanel(t)
	if strings.Contains(panelOutput, "Enter confirm") {
		before := session.markOutput()
		session.send(t, "\r")
		session.waitForFreshAll(t, 5*time.Second, before, "Enter submit")
	}
	beforeChildCompletion := session.markOutput()
	session.send(t, "\r")
	session.waitForOutputChange(t, 5*time.Second, beforeChildCompletion)
	session.waitForAll(t, 20*time.Second, "Task completed successfully", "Continue from this task")

	require.Eventually(t, func() bool {
		_, _, view, err := loadTaskStateByID(workDir, childTask.ID)
		return err == nil && view.Status == taskdomain.TaskStatusDone
	}, 10*time.Second, 100*time.Millisecond)

	store, err := taskstore.Open(workDir)
	require.NoError(t, err)
	defer store.Close()

	parentTaskID, err := store.GetFollowUpParentTaskID(context.Background(), childTask.ID)
	require.NoError(t, err)
	assert.Equal(t, parentTask.ID, parentTaskID)

	parentTask, parentRuns, parentView, err := loadTaskStateByID(workDir, parentTask.ID)
	require.NoError(t, err)
	childTask, childRuns, childView, err := loadTaskStateByID(workDir, childTask.ID)
	require.NoError(t, err)
	assert.Equal(t, taskdomain.TaskStatusDone, parentView.Status)
	assert.Equal(t, taskdomain.TaskStatusDone, childView.Status)

	parentDraftRun := requireNodeRunByName(t, parentRuns, "draft_plan")
	parentPlanPath := findArtifactPathByBase(t, taskdomain.ArtifactPaths(parentDraftRun.Result), "plan-1.md")
	childDraftRun := requireNodeRunByName(t, childRuns, "draft_plan")
	childInputPath := mustRunAuditPath(t, workDir, childTask, childRuns, childDraftRun, "input.md")
	childInputBytes, err := os.ReadFile(childInputPath)
	require.NoError(t, err)
	childInput := string(childInputBytes)

	assert.Contains(t, childInput, "## Direct Parent Task")
	assert.Contains(t, childInput, "Description: Parent follow-up task")
	assert.Contains(t, childInput, taskstore.TaskDir(workDir, parentTask.ID))
	assert.Contains(t, childInput, parentPlanPath)
	assert.NotContains(t, childInput, "## Earlier Ancestors")

	session.quit(t)
}

func TestTaskTUISingleRunFollowUpUsesHandleRequestEndToEnd(t *testing.T) {
	moduleRoot := moduleRoot(t)
	binaryPath := buildMuxagentBinary(t, moduleRoot)

	workDir := canonicalPath(t, t.TempDir())
	setupArtifactTUIRuntime(t, moduleRoot, workDir, "slow-happy", false)

	session := startTUISession(t, binaryPath, workDir)
	session.resize(t, 180, 39)
	session.waitForAll(t, 10*time.Second, "No tasks in this working directory yet.", "new task")
	session.send(t, "\r")
	session.waitForAll(t, 5*time.Second, "New Task", "Describe your task")
	session.send(t, "\x0e")
	session.waitForAll(t, 5*time.Second, "plan-only")
	session.send(t, "\x0e")
	session.waitForAll(t, 5*time.Second, "single-run")
	session.submitNewTask(t, "Parent single-run task")
	session.waitForAll(t, 20*time.Second, "Task completed successfully", "Continue from this task", "Tab continue")

	session.sendAndWait(t, "\t", 5*time.Second)
	session.typeText(t, "Child single-run task")
	beforeChildStart := session.markOutput()
	session.send(t, "\r")
	session.waitForFreshAll(t, 20*time.Second, beforeChildStart, "Task completed successfully", "Continue from this task")

	tasks := waitForTaskCount(t, workDir, 2)
	parentTask := findTaskByDescription(t, tasks, "Parent single-run task")
	childTask := findTaskByDescription(t, tasks, "Child single-run task")
	assert.Equal(t, taskconfig.BuiltinIDSingleRun, parentTask.ConfigAlias)
	assert.Equal(t, parentTask.ConfigAlias, childTask.ConfigAlias)

	parentTask, parentRuns, parentView, err := loadTaskStateByID(workDir, parentTask.ID)
	require.NoError(t, err)
	childTask, childRuns, childView, err := loadTaskStateByID(workDir, childTask.ID)
	require.NoError(t, err)
	assert.Equal(t, taskdomain.TaskStatusDone, parentView.Status)
	assert.Equal(t, taskdomain.TaskStatusDone, childView.Status)
	assertNodeRunCounts(t, parentRuns, map[string]int{
		"handle_request": 1,
		"done":           1,
	})
	assertNodeRunCounts(t, childRuns, map[string]int{
		"handle_request": 1,
		"done":           1,
	})

	parentHandleRun := requireNodeRunByName(t, parentRuns, "handle_request")
	parentResultPath := findArtifactPathByBase(t, taskdomain.ArtifactPaths(parentHandleRun.Result), "result-1.md")
	childHandleRun := requireNodeRunByName(t, childRuns, "handle_request")
	childInputPath := mustRunAuditPath(t, workDir, childTask, childRuns, childHandleRun, "input.md")
	childInputBytes, err := os.ReadFile(childInputPath)
	require.NoError(t, err)
	childInput := string(childInputBytes)

	assert.Contains(t, childInput, "## Direct Parent Task")
	assert.Contains(t, childInput, "Description: Parent single-run task")
	assert.Contains(t, childInput, taskstore.TaskDir(workDir, parentTask.ID))
	assert.Contains(t, childInput, parentResultPath)

	session.quit(t)
}

func TestTaskTUIFollowUpCanSwitchConfigEndToEnd(t *testing.T) {
	moduleRoot := moduleRoot(t)
	binaryPath := buildMuxagentBinary(t, moduleRoot)

	workDir := canonicalPath(t, t.TempDir())
	setupArtifactTUIRuntime(t, moduleRoot, workDir, "slow-happy", false)

	catalog, err := taskconfig.LoadCatalog()
	require.NoError(t, err)
	singleRunEntry, ok := catalog.Entry(taskconfig.BuiltinIDSingleRun)
	require.True(t, ok)

	session := startTUISession(t, binaryPath, workDir)
	session.resize(t, 180, 39)
	session.waitForAll(t, 10*time.Second, "No tasks in this working directory yet.", "new task")
	session.send(t, "\r")
	session.waitForAll(t, 5*time.Second, "New Task", "Describe your task")
	session.submitNewTask(t, "Parent switched follow-up task")
	session.approvePlan(t)
	session.waitForAll(t, 20*time.Second, "Task completed successfully", "Continue from this task", "Tab continue")

	session.sendAndWait(t, "\t", 5*time.Second)
	session.waitForAll(t, 5*time.Second, "Enter submit", "Ctrl+P/N config", "config default", "inherited")

	session.sendAndWait(t, "\x0e", 5*time.Second)
	session.waitForAll(t, 5*time.Second, "plan-only · runtime codex")

	session.sendAndWait(t, "\x0e", 5*time.Second)
	session.waitForAll(t, 5*time.Second, "single-run · runtime codex", "handle_request", "Ctrl+P/N config")

	session.typeText(t, "Child switched to single-run")
	beforeChildStart := session.markOutput()
	session.send(t, "\r")
	session.waitForFreshAll(t, 20*time.Second, beforeChildStart, "Task completed successfully", "Continue from this task")

	tasks := waitForTaskCount(t, workDir, 2)
	parentTask := findTaskByDescription(t, tasks, "Parent switched follow-up task")
	childTask := findTaskByDescription(t, tasks, "Child switched to single-run")
	assert.Equal(t, taskconfig.DefaultAlias, parentTask.ConfigAlias)
	assert.Equal(t, taskconfig.BuiltinIDSingleRun, childTask.ConfigAlias)
	assert.Equal(t, singleRunEntry.Path, childTask.ConfigPath)

	_, childRuns, childView, err := loadTaskStateByID(workDir, childTask.ID)
	require.NoError(t, err)
	assert.Equal(t, taskdomain.TaskStatusDone, childView.Status)
	assertNodeRunCounts(t, childRuns, map[string]int{
		"handle_request": 1,
		"done":           1,
	})

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
	session.approvePlan(t)

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

	before := session.markOutput()
	session.send(t, "\x1b")
	session.waitForFreshAll(t, 5*time.Second, before, "new task", "done Small completed artifacts")

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
	session.approvePlan(t)

	session.waitForAll(t, 10*time.Second, "implement", "awaiting input", "Question 1/1", "Shift+Tab artifacts")
	session.resetOutput()
	before := session.markOutput()
	session.resize(t, 150, 39)
	output := session.waitForFreshAll(t, 5*time.Second, before, "Question 1/1", "Other", "Submit answers")
	assert.NotContains(t, output, "Write your own answer")

	// Switch to artifacts tab via Shift+Tab
	session.resetOutput()
	session.sendBacktab(t)
	session.waitForAll(t, 5*time.Second, "Shift+Tab timeline", "Files", "Preview ·")

	// Switch back to timeline. The clarification panel is identical on both
	// tabs, so bubbletea's differential renderer won't re-send those lines.
	// A resize forces a full repaint so we can assert on the panel content.
	session.resetOutput()
	session.sendBacktab(t)
	before = session.markOutput()
	session.resize(t, 149, 39)
	session.waitForFreshAll(t, 5*time.Second, before, "Shift+Tab artifacts", "Question 1/1")

	session.quit(t)
}

func TestTaskTUIArtifactsCopyFailureBanner(t *testing.T) {
	moduleRoot := moduleRoot(t)
	binaryPath := buildMuxagentBinary(t, moduleRoot)

	workDir := canonicalPath(t, t.TempDir())
	_ = setupArtifactTUIRuntime(t, moduleRoot, workDir, "happy", false)

	session := startTUISession(t, binaryPath, workDir)
	session.resize(t, 120, 32)
	session.waitForAll(t, 10*time.Second, "No tasks in this working directory yet.", "new task")
	session.send(t, "\r")
	session.waitForAll(t, 5*time.Second, "New Task", "Describe your task")
	session.submitNewTask(t, "Artifact copy failure")
	session.waitForAll(t, 10*time.Second, "approve_plan", "awaiting approval", "Shift+Tab timeline", "Files", "Preview ·")

	_ = session.focusApprovalActionPanel(t)
	session.sendAndWaitForAll(t, "\t", 5*time.Second, "c copy path")
	session.sendAndWaitForAll(t, "c", 5*time.Second, "Unable to copy artifact path")

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

	session.approvePlan(t)
	// Resize forces a full repaint; the incremental renderer skips
	// unchanged right-side characters (like "Ctrl+C quit") otherwise.
	before := session.markOutput()
	session.resize(t, 150, 39)

	session.waitForFreshAll(t, 10*time.Second, before, "awaiting input", "Question 1/1", "Ctrl+C quit", "Other")

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

	session.approvePlan(t)
	session.resetOutput()
	session.resize(t, 150, 39)

	session.waitForAll(t, 10*time.Second, "Task failed", "Retry step", "Ctrl+C quit")

	session.send(t, "\r")

	session.waitForAll(t, 5*time.Second, "implement", "elapsed:", "Ctrl+C quit")

	session.quit(t)
}

type tuiOutputMark struct {
	version uint64
	output  string
}

type tuiSession struct {
	t        *testing.T
	cmd      *exec.Cmd
	ptmx     *os.File
	waitDone chan struct{}

	bufferMu     sync.Mutex
	buffer       strings.Builder
	outputSeq    uint64
	outputCh     chan struct{}
	waitMu       sync.Mutex
	waitErr      error
	ptyCloseOnce sync.Once
}

func TestTaskTUIStartupUpdatePromptScenarios(t *testing.T) {
	moduleRoot := moduleRoot(t)

	t.Run("later", func(t *testing.T) {
		workDir := t.TempDir()
		binaryPath := buildMuxagentBinaryWithVersion(t, moduleRoot, "v0.0.1")
		updatedBinaryPath := buildMuxagentBinaryWithVersion(t, moduleRoot, "v0.0.2")
		homeDir := setupStartupUpdateTUIRuntime(t, moduleRoot, workDir)

		pub, priv, err := ed25519.GenerateKey(rand.Reader)
		require.NoError(t, err)

		assetName := startupUpdateBundleAssetName(t)
		bundleBytes := createStartupUpdateBundle(t, updatedBinaryPath)
		server, reqs := startStartupUpdateReleaseServer(t, "v0.0.2", assetName, bundleBytes, priv)
		defer server.Close()

		t.Setenv("MUXAGENT_RELEASE_BASE_URL", server.URL+"/download")
		t.Setenv("MUXAGENT_RELEASE_SIGNING_PUBLIC_KEYS", base64.StdEncoding.EncodeToString(pub))

		session := startTUISession(t, binaryPath, workDir)
		session.waitForAll(t, 15*time.Second, "Update available: v0.0.1 -> v0.0.2", "Choose [1] Update now, [2] Later, [3] Skip this version")
		session.send(t, "\r")
		session.waitForAll(t, 15*time.Second, "No tasks in this working directory yet.", "new task")
		session.quit(t)

		statePath := filepath.Join(homeDir, ".muxagent", "startup-update-state.json")
		assert.FileExists(t, statePath)
		state := appconfig.LoadStartupUpdateState()
		assert.False(t, state.LastCheckedAt.IsZero())
		assert.True(t, state.LastFailedAt.IsZero())
		assert.Equal(t, 1, reqs.count("/latest/download/"+startupReleaseManifestName))
		assert.Zero(t, reqs.count("/download/v0.0.2/"+assetName))
	})

	t.Run("update now", func(t *testing.T) {
		workDir := t.TempDir()
		binaryPath := buildMuxagentBinaryWithVersion(t, moduleRoot, "v0.0.1")
		updatedBinaryPath := buildMuxagentBinaryWithVersion(t, moduleRoot, "v0.0.2")
		homeDir := setupStartupUpdateTUIRuntime(t, moduleRoot, workDir)

		pub, priv, err := ed25519.GenerateKey(rand.Reader)
		require.NoError(t, err)

		assetName := startupUpdateBundleAssetName(t)
		bundleBytes := createStartupUpdateBundle(t, updatedBinaryPath)
		server, reqs := startStartupUpdateReleaseServer(t, "v0.0.2", assetName, bundleBytes, priv)
		defer server.Close()

		t.Setenv("MUXAGENT_RELEASE_BASE_URL", server.URL+"/download")
		t.Setenv("MUXAGENT_RELEASE_SIGNING_PUBLIC_KEYS", base64.StdEncoding.EncodeToString(pub))

		session := startTUISession(t, binaryPath, workDir)
		session.waitForAll(t, 15*time.Second, "Update available: v0.0.1 -> v0.0.2", "Choose [1] Update now, [2] Later, [3] Skip this version")
		session.send(t, "1\r")
		session.waitForAll(t, 30*time.Second, "No tasks in this working directory yet.", "new task")
		session.quit(t)

		statePath := filepath.Join(homeDir, ".muxagent", "startup-update-state.json")
		assert.FileExists(t, statePath)
		state := appconfig.LoadStartupUpdateState()
		assert.False(t, state.LastCheckedAt.IsZero())
		assert.True(t, state.LastFailedAt.IsZero())
		assert.Equal(t, 1, reqs.count("/latest/download/"+startupReleaseManifestName))
		assert.Equal(t, 1, reqs.count("/download/v0.0.2/"+startupReleaseManifestName))
		assert.Equal(t, 1, reqs.count("/download/v0.0.2/"+assetName))

		out, err := exec.Command(binaryPath, "version").CombinedOutput()
		require.NoError(t, err, string(out))
		assert.Contains(t, string(out), "v0.0.2")
	})
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
		t:        t,
		cmd:      cmd,
		ptmx:     ptmx,
		waitDone: make(chan struct{}),
		outputCh: make(chan struct{}, 1),
	}
	go func() {
		_, _ = io.Copy(session, ptmx)
	}()
	go func() {
		err := cmd.Wait()
		session.waitMu.Lock()
		session.waitErr = err
		session.waitMu.Unlock()
		close(session.waitDone)
	}()
	t.Cleanup(func() {
		session.forceClose()
	})
	return session
}

func setupStartupUpdateTUIRuntime(t *testing.T, moduleRoot, workDir string) string {
	t.Helper()

	fakeDir := t.TempDir()
	basePath := os.Getenv("PATH")
	copyExecutable(t, filepath.Join(moduleRoot, "cmd", "muxagent", "testdata", "fake-codex.sh"), filepath.Join(fakeDir, "codex"))
	linkRuntimeCommands(t, fakeDir, "basename", "cat", "dirname", "mkdir", "sleep")

	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("PATH", fakeDir+string(os.PathListSeparator)+basePath)
	t.Setenv("FAKE_CODEX_FLOW", "happy")
	t.Setenv("FAKE_CODEX_STATE_DIR", filepath.Join(workDir, ".fake-codex-state"))
	t.Setenv("TERM", "xterm-256color")
	return homeDir
}

const startupReleaseManifestName = "SHA256SUMS"

type startupReleaseRequests struct {
	mu     sync.Mutex
	counts map[string]int
}

func (r *startupReleaseRequests) add(path string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.counts[path]++
}

func (r *startupReleaseRequests) count(path string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.counts[path]
}

func startupUpdateBundleAssetName(t *testing.T) string {
	t.Helper()

	name := fmt.Sprintf("muxagent-%s-%s", runtime.GOOS, runtime.GOARCH)
	if runtime.GOOS == "linux" {
		if matches, _ := filepath.Glob("/lib/ld-musl-*"); len(matches) > 0 {
			name += "-musl"
		}
	}
	return name + ".tar.gz"
}

func createStartupUpdateBundle(t *testing.T, cliBinaryPath string) []byte {
	t.Helper()

	cliBody, err := os.ReadFile(cliBinaryPath)
	require.NoError(t, err)

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tarWriter := tar.NewWriter(gz)

	write := func(name string, body []byte) {
		header := &tar.Header{
			Name: name,
			Mode: 0o755,
			Size: int64(len(body)),
		}
		require.NoError(t, tarWriter.WriteHeader(header))
		_, err := tarWriter.Write(body)
		require.NoError(t, err)
	}

	write("muxagent", cliBody)
	write("claude-agent-acp", []byte("#!/bin/sh\nexit 0\n"))
	write("codex-acp", []byte("#!/bin/sh\nexit 0\n"))
	require.NoError(t, tarWriter.Close())
	require.NoError(t, gz.Close())
	return buf.Bytes()
}

func startStartupUpdateReleaseServer(t *testing.T, version, assetName string, bundleBytes []byte, signer ed25519.PrivateKey) (*httptest.Server, *startupReleaseRequests) {
	t.Helper()

	hash := sha256.Sum256(bundleBytes)
	manifest := []byte(fmt.Sprintf("# muxagent %s\n%s  %s\n", version, hex.EncodeToString(hash[:]), assetName))
	signature := ed25519.Sign(signer, manifest)
	sigBase64 := []byte(base64.StdEncoding.EncodeToString(signature))

	reqs := &startupReleaseRequests{counts: make(map[string]int)}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqs.add(r.URL.Path)
		switch r.URL.Path {
		case "/latest/download/" + startupReleaseManifestName:
			_, _ = w.Write(manifest)
		case "/latest/download/" + startupReleaseManifestName + ".sig":
			_, _ = w.Write(sigBase64)
		case "/download/" + version + "/" + startupReleaseManifestName:
			_, _ = w.Write(manifest)
		case "/download/" + version + "/" + startupReleaseManifestName + ".sig":
			_, _ = w.Write(sigBase64)
		case "/download/" + version + "/" + assetName:
			_, _ = w.Write(bundleBytes)
		default:
			http.NotFound(w, r)
		}
	}))
	return server, reqs
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
	n, err := s.buffer.Write(p)
	if err == nil && n > 0 {
		s.outputSeq++
	}
	s.bufferMu.Unlock()
	if n > 0 {
		s.signalOutput()
	}
	return n, err
}

func (s *tuiSession) signalOutput() {
	select {
	case s.outputCh <- struct{}{}:
	default:
	}
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

func (s *tuiSession) sendAndWait(t *testing.T, input string, timeout time.Duration) string {
	t.Helper()
	before := s.markOutput()
	s.send(t, input)
	return s.waitForOutputChange(t, timeout, before)
}

func (s *tuiSession) sendAndWaitForAll(t *testing.T, input string, timeout time.Duration, needles ...string) string {
	t.Helper()
	before := s.markOutput()
	s.send(t, input)
	return s.waitForFreshAll(t, timeout, before, needles...)
}

func (s *tuiSession) resize(t *testing.T, cols, rows uint16) {
	t.Helper()
	require.NoError(t, pty.Setsize(s.ptmx, &pty.Winsize{Rows: rows, Cols: cols}))
}

func (s *tuiSession) confirm(t *testing.T) {
	t.Helper()
	before := s.markOutput()
	s.send(t, "\r")
	updated, ok := s.waitForOutputChangeWithin(t, time.Second, before)
	if !ok {
		s.send(t, "\r")
		return
	}
	if !needsSecondConfirmInput(updated.output) {
		return
	}
	if _, cleared := s.waitForConfirmStateExit(t, 500*time.Millisecond, updated); cleared {
		return
	}
	s.send(t, "\r")
	_, _ = s.waitForConfirmStateExit(t, time.Second, updated)
}

func (s *tuiSession) approvePlan(t *testing.T) {
	t.Helper()
	beforeApproval := s.markOutput()
	s.waitForFreshAll(t, 10*time.Second, beforeApproval, "approve_plan", "awaiting approval")
	panelOutput := s.focusApprovalActionPanel(t)
	if strings.Contains(panelOutput, "Enter confirm") {
		before := s.markOutput()
		s.send(t, "\r")
		s.waitForFreshAll(t, 5*time.Second, before, "Enter submit")
	}
	before := s.markOutput()
	s.send(t, "\r")
	s.waitForOutputChange(t, 5*time.Second, before)
}

func (s *tuiSession) focusApprovalActionPanel(t *testing.T) string {
	t.Helper()
	for range 6 {
		before := s.markOutput()
		s.send(t, "\t")
		output := s.waitForOutputChange(t, 2*time.Second, before)
		fresh := output
		if strings.HasPrefix(output, before.output) {
			fresh = output[len(before.output):]
		}
		if strings.Contains(fresh, "Enter submit") || strings.Contains(fresh, "Enter confirm") {
			return fresh
		}
	}
	t.Fatalf("expected approval action footer to be focused, got output:\n%s", s.output())
	return ""
}

func (s *tuiSession) submitNewTask(t *testing.T, description string) {
	t.Helper()
	s.typeText(t, description)
	before := s.markOutput()
	s.send(t, "\r")
	s.waitForOutputChange(t, 5*time.Second, before)
}

func (s *tuiSession) typeText(t *testing.T, value string) {
	t.Helper()
	runes := []rune(value)
	for start := 0; start < len(runes); start += 8 {
		end := start + 8
		if end > len(runes) {
			end = len(runes)
		}
		before := s.markOutput()
		s.send(t, string(runes[start:end]))
		s.waitForOutputChange(t, time.Second, before)
	}
	s.waitForOutputIdle(200 * time.Millisecond)
}

func (s *tuiSession) waitForOutputChange(t *testing.T, timeout time.Duration, before tuiOutputMark) string {
	t.Helper()
	output, ok := s.waitForOutputChangeWithin(t, timeout, before)
	if ok {
		return output.output
	}
	t.Fatalf("timed out waiting for output to change\nbefore:\n%s\nafter:\n%s", before.output, s.output())
	return ""
}

func (s *tuiSession) waitForOutputChangeWithin(t *testing.T, timeout time.Duration, before tuiOutputMark) (tuiOutputMark, bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		output := s.markOutput()
		if output.version > before.version {
			return output, true
		}
		if err, exited := s.exitStatus(); exited {
			require.NoErrorf(t, err, "muxagent exited before screen changed\n%s", output.output)
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			break
		}
		if !s.waitForOutputActivity(remaining) {
			break
		}
	}
	return s.markOutput(), false
}

func (s *tuiSession) waitForFreshAll(t *testing.T, timeout time.Duration, before tuiOutputMark, needles ...string) string {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		output := s.markOutput()
		if output.version <= before.version {
			if err, exited := s.exitStatus(); exited {
				require.NoErrorf(t, err, "muxagent exited before screen changed\n%s", output.output)
			}
		} else {
			fresh := output.output
			if strings.HasPrefix(output.output, before.output) {
				fresh = output.output[len(before.output):]
			}
			allFound := true
			for _, needle := range needles {
				if !strings.Contains(fresh, needle) {
					allFound = false
					break
				}
			}
			if allFound {
				return output.output
			}
			if err, exited := s.exitStatus(); exited {
				require.NoErrorf(t, err, "muxagent exited before screen stabilized\n%s", output.output)
			}
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			break
		}
		if !s.waitForOutputActivity(remaining) {
			break
		}
	}
	t.Fatalf("timed out waiting for fresh %q\nbefore:\n%s\nafter:\n%s", strings.Join(needles, ", "), before.output, s.output())
	return ""
}

func (s *tuiSession) waitForAll(t *testing.T, timeout time.Duration, needles ...string) string {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
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
		if err, exited := s.exitStatus(); exited {
			require.NoErrorf(t, err, "muxagent exited before screen stabilized\n%s", output)
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			break
		}
		if !s.waitForOutputActivity(remaining) {
			break
		}
	}
	t.Fatalf("timed out waiting for %q\n%s", strings.Join(needles, ", "), s.output())
	return ""
}

func sanitizeTUIOutput(raw string) string {
	clean := ansi.Strip(raw)
	clean = strings.ReplaceAll(clean, "\r", "\n")
	clean = strings.ReplaceAll(clean, "\x00", "")
	return clean
}

func (s *tuiSession) markOutput() tuiOutputMark {
	s.bufferMu.Lock()
	defer s.bufferMu.Unlock()
	return tuiOutputMark{
		version: s.outputSeq,
		output:  sanitizeTUIOutput(s.buffer.String()),
	}
}

func (s *tuiSession) output() string {
	return s.markOutput().output
}

func (s *tuiSession) resetOutput() tuiOutputMark {
	s.bufferMu.Lock()
	s.buffer.Reset()
	mark := tuiOutputMark{version: s.outputSeq}
	s.bufferMu.Unlock()
	return mark
}

func needsSecondConfirmInput(output string) bool {
	return strings.Contains(output, "Approve this plan?") ||
		strings.Contains(output, "Question ") ||
		strings.Contains(output, "Write your own answer") ||
		strings.Contains(output, "Submit answers") ||
		strings.Contains(output, "Enter submit")
}

func (s *tuiSession) waitForOutputActivity(timeout time.Duration) bool {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-s.outputCh:
		return true
	case <-s.waitDone:
		return true
	case <-timer.C:
		return false
	}
}

func (s *tuiSession) waitForOutputIdle(idle time.Duration) {
	timer := time.NewTimer(idle)
	defer timer.Stop()
	for {
		select {
		case <-s.outputCh:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(idle)
		case <-s.waitDone:
			return
		case <-timer.C:
			return
		}
	}
}

func (s *tuiSession) waitForConfirmStateExit(t *testing.T, timeout time.Duration, before tuiOutputMark) (tuiOutputMark, bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		current := s.markOutput()
		fresh := current.output
		if before.output != "" && strings.HasPrefix(current.output, before.output) {
			fresh = current.output[len(before.output):]
		}
		if current.version > before.version && !needsSecondConfirmInput(fresh) {
			return current, true
		}
		if err, exited := s.exitStatus(); exited {
			require.NoErrorf(t, err, "muxagent exited while confirming\n%s", current.output)
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			break
		}
		if !s.waitForOutputActivity(remaining) {
			break
		}
	}
	return s.markOutput(), false
}

func (s *tuiSession) quit(t *testing.T) {
	t.Helper()
	if err, exited := s.exitStatus(); exited {
		require.NoError(t, err)
		require.NoError(t, s.closePTY())
		return
	}
	s.send(t, "\x03")
	deadline := time.Now().Add(2 * time.Second)
	for {
		if err, exited := s.exitStatus(); exited {
			require.NoError(t, err)
			require.NoError(t, s.closePTY())
			return
		}
		if strings.Contains(s.output(), "Quit muxagent?") {
			break
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			break
		}
		if !s.waitForOutputActivity(remaining) {
			break
		}
	}
	if _, exited := s.exitStatus(); !exited {
		if strings.Contains(s.output(), "Quit muxagent?") {
			before := s.markOutput()
			s.send(t, "\t")
			_, _ = s.waitForOutputChangeWithin(t, time.Second, before)
			s.send(t, "\r")
		} else {
			s.send(t, "\x03")
		}
	}
	if err, exited := s.waitForExit(5 * time.Second); exited {
		require.NoError(t, err)
	} else {
		t.Fatalf("muxagent did not exit after Ctrl+C\n%s", s.output())
	}
	require.NoError(t, s.closePTY())
}

func (s *tuiSession) forceClose() {
	_ = s.closePTY()
	if s.cmd != nil && s.cmd.Process != nil {
		if _, exited := s.exitStatus(); exited {
			return
		}
		_ = s.cmd.Process.Kill()
		if _, exited := s.waitForExit(2 * time.Second); !exited {
		}
	}
}

func (s *tuiSession) closePTY() error {
	var err error
	s.ptyCloseOnce.Do(func() {
		if s.ptmx != nil {
			err = s.ptmx.Close()
			s.ptmx = nil
		}
	})
	return err
}

func (s *tuiSession) exitStatus() (error, bool) {
	select {
	case <-s.waitDone:
		s.waitMu.Lock()
		defer s.waitMu.Unlock()
		return s.waitErr, true
	default:
		return nil, false
	}
}

func (s *tuiSession) waitForExit(timeout time.Duration) (error, bool) {
	select {
	case <-s.waitDone:
		s.waitMu.Lock()
		defer s.waitMu.Unlock()
		return s.waitErr, true
	case <-time.After(timeout):
		return nil, false
	}
}

func buildMuxagentBinary(t *testing.T, moduleRoot string) string {
	return buildMuxagentBinaryWithVersion(t, moduleRoot, "")
}

func buildMuxagentBinaryWithVersion(t *testing.T, moduleRoot, version string) string {
	t.Helper()
	binaryPath := filepath.Join(t.TempDir(), "muxagent")
	args := []string{"build"}
	if version != "" {
		args = append(args, "-ldflags", "-X github.com/LaLanMo/muxagent-cli/internal/version.Version="+version)
	}
	args = append(args, "-o", binaryPath, "./cmd/muxagent")
	cmd := exec.Command("go", args...)
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

func setupArtifactTUIRuntime(t *testing.T, moduleRoot, workDir, flow string, withClipboard bool) string {
	t.Helper()

	fakeDir := t.TempDir()
	copyExecutable(t, filepath.Join(moduleRoot, "cmd", "muxagent", "testdata", "fake-codex.sh"), filepath.Join(fakeDir, "codex"))
	linkRuntimeCommands(t, fakeDir, "basename", "cat", "dirname", "mkdir", "sleep")

	clipboardPath := ""
	if withClipboard {
		clipboardPath = filepath.Join(fakeDir, "clipboard.txt")
		writeFakeClipboardCommand(t, fakeDir, clipboardPath)
	}

	t.Setenv("HOME", t.TempDir())
	t.Setenv("PATH", fakeDir)
	t.Setenv("FAKE_CODEX_FLOW", flow)
	t.Setenv("FAKE_CODEX_STATE_DIR", filepath.Join(workDir, ".fake-codex-state"))
	t.Setenv("TERM", "xterm-256color")
	return clipboardPath
}

func linkRuntimeCommands(t *testing.T, dir string, commands ...string) {
	t.Helper()
	for _, command := range commands {
		path, err := exec.LookPath(command)
		require.NoError(t, err)
		require.NoError(t, os.Symlink(path, filepath.Join(dir, command)))
	}
}

func writeFakeClipboardCommand(t *testing.T, dir, capturePath string) {
	t.Helper()

	name := "pbcopy"
	switch runtime.GOOS {
	case "linux":
		name = "xclip"
	case "windows":
		t.Skip("clipboard PTY helper is not supported on windows")
	}

	script := "#!/bin/sh\ncat > \"$MUXAGENT_TEST_CLIPBOARD\"\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(script), 0o755))
	t.Setenv("MUXAGENT_TEST_CLIPBOARD", capturePath)
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
	deadline := time.Now().Add(60 * time.Second)
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

func waitForTaskCount(t *testing.T, workDir string, want int) []taskdomain.Task {
	t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		tasks, err := loadTaskRecords(workDir)
		if err == nil && len(tasks) == want {
			return tasks
		}
		time.Sleep(50 * time.Millisecond)
	}
	tasks, err := loadTaskRecords(workDir)
	require.NoError(t, err)
	require.Len(t, tasks, want)
	return tasks
}

func waitForClipboardContents(t *testing.T, path string) string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(path)
		if err == nil && len(data) > 0 {
			return string(data)
		}
		time.Sleep(50 * time.Millisecond)
	}
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.NotEmpty(t, data)
	return string(data)
}

func waitForNodeRunCounts(t *testing.T, workDir string, want map[string]int) {
	t.Helper()
	deadline := time.Now().Add(60 * time.Second)
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

func loadTaskStateByID(workDir, taskID string) (taskdomain.Task, []taskdomain.NodeRun, taskdomain.TaskView, error) {
	store, err := taskstore.Open(workDir)
	if err != nil {
		return taskdomain.Task{}, nil, taskdomain.TaskView{}, err
	}
	defer store.Close()

	ctx := context.Background()
	task, err := store.GetTask(ctx, taskID)
	if err != nil {
		return taskdomain.Task{}, nil, taskdomain.TaskView{}, err
	}
	runs, err := store.ListNodeRunsByTask(ctx, taskID)
	if err != nil {
		return taskdomain.Task{}, nil, taskdomain.TaskView{}, err
	}
	cfg, err := taskconfig.Load(taskstore.ConfigPath(workDir, taskID))
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

func findTaskByDescription(t *testing.T, tasks []taskdomain.Task, description string) taskdomain.Task {
	t.Helper()
	for _, task := range tasks {
		if task.Description == description {
			return task
		}
	}
	t.Fatalf("task %q not found in %v", description, tasks)
	return taskdomain.Task{}
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

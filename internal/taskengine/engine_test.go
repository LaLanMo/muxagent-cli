package taskengine

import (
	"testing"
	"time"

	"github.com/LaLanMo/muxagent-cli/internal/taskconfig"
	"github.com/LaLanMo/muxagent-cli/internal/taskdomain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEngineHappyPathDefaultFlow(t *testing.T) {
	cfg, err := taskconfig.LoadDefault()
	require.NoError(t, err)

	engine := New()
	taskID := "task-1"
	now := time.Now().UTC()

	upsert := taskdomain.NodeRun{ID: "run-upsert", TaskID: taskID, NodeName: "upsert_plan", StartedAt: now, Status: taskdomain.NodeRunDone, Result: map[string]interface{}{"file_paths": []interface{}{"/tmp/plan.md"}}}
	engine.RegisterEntryRun(taskID, upsert)
	resolution, err := engine.ResolveCompletion(cfg, taskID, []taskdomain.NodeRun{upsert}, upsert)
	require.NoError(t, err)
	require.Len(t, resolution.Transitions, 1)
	assert.Equal(t, "review_plan", resolution.Transitions[0].To)

	review := taskdomain.NodeRun{ID: "run-review", TaskID: taskID, NodeName: "review_plan", StartedAt: now.Add(time.Second), Status: taskdomain.NodeRunDone, TriggeredBy: &taskdomain.TriggeredBy{NodeRunID: upsert.ID, Reason: resolution.Transitions[0].Reason}, Result: map[string]interface{}{"passed": true, "file_paths": []interface{}{"/tmp/review.md"}}}
	engine.RegisterTriggeredRun(taskID, review, upsert.ID)
	resolution, err = engine.ResolveCompletion(cfg, taskID, []taskdomain.NodeRun{upsert, review}, review)
	require.NoError(t, err)
	require.Len(t, resolution.Transitions, 1)
	assert.Equal(t, "approve_plan", resolution.Transitions[0].To)

	approve := taskdomain.NodeRun{ID: "run-approve", TaskID: taskID, NodeName: "approve_plan", StartedAt: now.Add(2 * time.Second), Status: taskdomain.NodeRunDone, TriggeredBy: &taskdomain.TriggeredBy{NodeRunID: review.ID, Reason: resolution.Transitions[0].Reason}, Result: map[string]interface{}{"approved": true}}
	engine.RegisterTriggeredRun(taskID, approve, review.ID)
	resolution, err = engine.ResolveCompletion(cfg, taskID, []taskdomain.NodeRun{upsert, review, approve}, approve)
	require.NoError(t, err)
	require.Len(t, resolution.Transitions, 1)
	assert.Equal(t, "implement", resolution.Transitions[0].To)

	implement := taskdomain.NodeRun{ID: "run-implement", TaskID: taskID, NodeName: "implement", StartedAt: now.Add(3 * time.Second), Status: taskdomain.NodeRunDone, TriggeredBy: &taskdomain.TriggeredBy{NodeRunID: approve.ID, Reason: resolution.Transitions[0].Reason}, Result: map[string]interface{}{"file_paths": []interface{}{"/tmp/impl.md"}}}
	engine.RegisterTriggeredRun(taskID, implement, approve.ID)
	resolution, err = engine.ResolveCompletion(cfg, taskID, []taskdomain.NodeRun{upsert, review, approve, implement}, implement)
	require.NoError(t, err)
	require.Len(t, resolution.Transitions, 1)
	assert.Equal(t, "verify", resolution.Transitions[0].To)

	verify := taskdomain.NodeRun{ID: "run-verify", TaskID: taskID, NodeName: "verify", StartedAt: now.Add(4 * time.Second), Status: taskdomain.NodeRunDone, TriggeredBy: &taskdomain.TriggeredBy{NodeRunID: implement.ID, Reason: resolution.Transitions[0].Reason}, Result: map[string]interface{}{"passed": true, "file_paths": []interface{}{"/tmp/verify.md"}}}
	engine.RegisterTriggeredRun(taskID, verify, implement.ID)
	resolution, err = engine.ResolveCompletion(cfg, taskID, []taskdomain.NodeRun{upsert, review, approve, implement, verify}, verify)
	require.NoError(t, err)
	require.Len(t, resolution.Transitions, 1)
	assert.Equal(t, "done", resolution.Transitions[0].To)

	done := taskdomain.NodeRun{ID: "run-done", TaskID: taskID, NodeName: "done", StartedAt: now.Add(5 * time.Second), Status: taskdomain.NodeRunDone, TriggeredBy: &taskdomain.TriggeredBy{NodeRunID: verify.ID, Reason: resolution.Transitions[0].Reason}, Result: map[string]interface{}{}}
	engine.RegisterTriggeredRun(taskID, done, verify.ID)
	resolution, err = engine.ResolveCompletion(cfg, taskID, []taskdomain.NodeRun{upsert, review, approve, implement, verify, done}, done)
	require.NoError(t, err)
	assert.True(t, resolution.TaskDone)
}

func TestEngineRejectLoopsBackToUpsertPlan(t *testing.T) {
	cfg, err := taskconfig.LoadDefault()
	require.NoError(t, err)

	engine := New()
	taskID := "task-1"
	entry := taskdomain.NodeRun{ID: "entry", TaskID: taskID, NodeName: "upsert_plan", StartedAt: time.Now().UTC(), Status: taskdomain.NodeRunDone, Result: map[string]interface{}{"file_paths": []interface{}{"/tmp/plan.md"}}}
	engine.RegisterEntryRun(taskID, entry)
	resolution, err := engine.ResolveCompletion(cfg, taskID, []taskdomain.NodeRun{entry}, entry)
	require.NoError(t, err)
	review := taskdomain.NodeRun{ID: "review", TaskID: taskID, NodeName: "review_plan", StartedAt: entry.StartedAt.Add(time.Second), Status: taskdomain.NodeRunDone, TriggeredBy: &taskdomain.TriggeredBy{NodeRunID: entry.ID, Reason: resolution.Transitions[0].Reason}, Result: map[string]interface{}{"passed": false, "file_paths": []interface{}{"/tmp/review.md"}}}
	engine.RegisterTriggeredRun(taskID, review, entry.ID)
	resolution, err = engine.ResolveCompletion(cfg, taskID, []taskdomain.NodeRun{entry, review}, review)
	require.NoError(t, err)
	require.Len(t, resolution.Transitions, 1)
	assert.Equal(t, "upsert_plan", resolution.Transitions[0].To)
}

func TestEngineApprovalRejectLoopIgnoresJoinOnReentry(t *testing.T) {
	cfg, err := taskconfig.LoadDefault()
	require.NoError(t, err)

	engine := New()
	taskID := "task-approval-reject"
	now := time.Now().UTC()

	upsert := taskdomain.NodeRun{ID: "run-upsert", TaskID: taskID, NodeName: "upsert_plan", StartedAt: now, Status: taskdomain.NodeRunDone, Result: map[string]interface{}{"file_paths": []interface{}{"/tmp/plan.md"}}}
	engine.RegisterEntryRun(taskID, upsert)
	resolution, err := engine.ResolveCompletion(cfg, taskID, []taskdomain.NodeRun{upsert}, upsert)
	require.NoError(t, err)

	review := taskdomain.NodeRun{ID: "run-review", TaskID: taskID, NodeName: "review_plan", StartedAt: now.Add(time.Second), Status: taskdomain.NodeRunDone, TriggeredBy: &taskdomain.TriggeredBy{NodeRunID: upsert.ID, Reason: resolution.Transitions[0].Reason}, Result: map[string]interface{}{"passed": true, "file_paths": []interface{}{"/tmp/review.md"}}}
	engine.RegisterTriggeredRun(taskID, review, upsert.ID)
	resolution, err = engine.ResolveCompletion(cfg, taskID, []taskdomain.NodeRun{upsert, review}, review)
	require.NoError(t, err)

	approve := taskdomain.NodeRun{ID: "run-approve", TaskID: taskID, NodeName: "approve_plan", StartedAt: now.Add(2 * time.Second), Status: taskdomain.NodeRunDone, TriggeredBy: &taskdomain.TriggeredBy{NodeRunID: review.ID, Reason: resolution.Transitions[0].Reason}, Result: map[string]interface{}{"approved": false, "feedback": "Need more detail"}}
	engine.RegisterTriggeredRun(taskID, approve, review.ID)
	resolution, err = engine.ResolveCompletion(cfg, taskID, []taskdomain.NodeRun{upsert, review, approve}, approve)
	require.NoError(t, err)
	require.Len(t, resolution.Transitions, 1)
	assert.Equal(t, "upsert_plan", resolution.Transitions[0].To)
}

func TestEngineJoinAllWaitsForAllTriggeredBranches(t *testing.T) {
	cfg := joinFixture(taskconfig.JoinAll)
	engine := New()
	taskID := "task-join-all"
	now := time.Now().UTC()

	start := taskdomain.NodeRun{ID: "start", TaskID: taskID, NodeName: "start", StartedAt: now, Status: taskdomain.NodeRunDone, Result: map[string]interface{}{}}
	engine.RegisterEntryRun(taskID, start)
	resolution, err := engine.ResolveCompletion(cfg, taskID, []taskdomain.NodeRun{start}, start)
	require.NoError(t, err)
	require.Len(t, resolution.Transitions, 2)

	left := taskdomain.NodeRun{ID: "left", TaskID: taskID, NodeName: "left", StartedAt: now.Add(time.Second), Status: taskdomain.NodeRunDone, TriggeredBy: &taskdomain.TriggeredBy{NodeRunID: start.ID, Reason: resolution.Transitions[0].Reason}, Result: map[string]interface{}{}}
	right := taskdomain.NodeRun{ID: "right", TaskID: taskID, NodeName: "right", StartedAt: now.Add(2 * time.Second), Status: taskdomain.NodeRunRunning, TriggeredBy: &taskdomain.TriggeredBy{NodeRunID: start.ID, Reason: resolution.Transitions[1].Reason}}
	engine.RegisterTriggeredRun(taskID, left, start.ID)
	engine.RegisterTriggeredRun(taskID, right, start.ID)

	resolution, err = engine.ResolveCompletion(cfg, taskID, []taskdomain.NodeRun{start, left, right}, left)
	require.NoError(t, err)
	assert.Empty(t, resolution.Transitions)

	right.Status = taskdomain.NodeRunDone
	resolution, err = engine.ResolveCompletion(cfg, taskID, []taskdomain.NodeRun{start, left, right}, right)
	require.NoError(t, err)
	require.Len(t, resolution.Transitions, 1)
	assert.Equal(t, "join", resolution.Transitions[0].To)
}

func TestEngineJoinAnyFiresOnFirstArrival(t *testing.T) {
	cfg := joinFixture(taskconfig.JoinAny)
	engine := New()
	taskID := "task-join-any"
	now := time.Now().UTC()

	start := taskdomain.NodeRun{ID: "start", TaskID: taskID, NodeName: "start", StartedAt: now, Status: taskdomain.NodeRunDone, Result: map[string]interface{}{}}
	engine.RegisterEntryRun(taskID, start)
	resolution, err := engine.ResolveCompletion(cfg, taskID, []taskdomain.NodeRun{start}, start)
	require.NoError(t, err)

	left := taskdomain.NodeRun{ID: "left", TaskID: taskID, NodeName: "left", StartedAt: now.Add(time.Second), Status: taskdomain.NodeRunDone, TriggeredBy: &taskdomain.TriggeredBy{NodeRunID: start.ID, Reason: resolution.Transitions[0].Reason}, Result: map[string]interface{}{}}
	engine.RegisterTriggeredRun(taskID, left, start.ID)

	resolution, err = engine.ResolveCompletion(cfg, taskID, []taskdomain.NodeRun{start, left}, left)
	require.NoError(t, err)
	require.Len(t, resolution.Transitions, 1)
	assert.Equal(t, "join", resolution.Transitions[0].To)
}

func TestEngineElseFallback(t *testing.T) {
	cfg := elseFixture()
	engine := New()
	taskID := "task-else"
	run := taskdomain.NodeRun{ID: "start", TaskID: taskID, NodeName: "start", StartedAt: time.Now().UTC(), Status: taskdomain.NodeRunDone, Result: map[string]interface{}{"flag": "unknown"}}
	engine.RegisterEntryRun(taskID, run)

	resolution, err := engine.ResolveCompletion(cfg, taskID, []taskdomain.NodeRun{run}, run)
	require.NoError(t, err)
	require.Len(t, resolution.Transitions, 1)
	assert.Equal(t, "fallback", resolution.Transitions[0].To)
}

func TestEngineRejectsIterationOverflow(t *testing.T) {
	cfg := loopFixture()
	engine := New()
	taskID := "task-loop"
	now := time.Now().UTC()
	entry := taskdomain.NodeRun{ID: "start-0", TaskID: taskID, NodeName: "start", StartedAt: now, Status: taskdomain.NodeRunDone, Result: map[string]interface{}{"flag": "retry"}}
	engine.RegisterEntryRun(taskID, entry)
	_, err := engine.ResolveCompletion(cfg, taskID, []taskdomain.NodeRun{entry}, entry)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exceeded max_iterations")
}

func TestMatchesConditionIsTypeSafe(t *testing.T) {
	assert.False(t, matchesCondition(map[string]interface{}{"flag": "false"}, taskconfig.EdgeCondition{
		Field:  "flag",
		Equals: false,
	}))
	assert.True(t, matchesCondition(map[string]interface{}{"count": 1.0}, taskconfig.EdgeCondition{
		Field:  "count",
		Equals: 1,
	}))
}

func joinFixture(join taskconfig.Join) *taskconfig.Config {
	return &taskconfig.Config{
		Version: 1,
		Clarification: taskconfig.ClarificationConfig{
			MaxQuestions:          4,
			MaxOptionsPerQuestion: 4,
			MinOptionsPerQuestion: 2,
		},
		Topology: taskconfig.Topology{
			MaxIterations: 3,
			Entry:         "start",
			Nodes: []taskconfig.NodeRef{
				{Name: "start"},
				{Name: "left"},
				{Name: "right"},
				{Name: "join", Join: join},
			},
			Edges: []taskconfig.Edge{
				{From: "start", To: "left"},
				{From: "start", To: "right"},
				{From: "left", To: "join"},
				{From: "right", To: "join"},
			},
		},
		NodeDefinitions: map[string]taskconfig.NodeDefinition{
			"start": simpleAgentNode(),
			"left":  simpleAgentNode(),
			"right": simpleAgentNode(),
			"join":  simpleAgentNode(),
		},
	}
}

func elseFixture() *taskconfig.Config {
	return &taskconfig.Config{
		Version: 1,
		Clarification: taskconfig.ClarificationConfig{
			MaxQuestions:          4,
			MaxOptionsPerQuestion: 4,
			MinOptionsPerQuestion: 2,
		},
		Topology: taskconfig.Topology{
			MaxIterations: 1,
			Entry:         "start",
			Nodes: []taskconfig.NodeRef{
				{Name: "start"},
				{Name: "done"},
				{Name: "fallback"},
			},
			Edges: []taskconfig.Edge{
				{From: "start", To: "done", When: taskconfig.EdgeCondition{Kind: taskconfig.ConditionWhen, Field: "flag", Equals: "done"}},
				{From: "start", To: "fallback", When: taskconfig.EdgeCondition{Kind: taskconfig.ConditionElse}},
			},
		},
		NodeDefinitions: map[string]taskconfig.NodeDefinition{
			"start":    simpleAgentNode(),
			"done":     simpleAgentNode(),
			"fallback": simpleAgentNode(),
		},
	}
}

func simpleAgentNode() taskconfig.NodeDefinition {
	allow := false
	return taskconfig.NodeDefinition{
		SystemPrompt: "./prompt.md",
		ResultSchema: taskconfig.JSONSchema{
			Type:                 "object",
			AdditionalProperties: &allow,
			Properties:           map[string]*taskconfig.JSONSchema{},
		},
	}
}

func loopFixture() *taskconfig.Config {
	return &taskconfig.Config{
		Version: 1,
		Clarification: taskconfig.ClarificationConfig{
			MaxQuestions:          4,
			MaxOptionsPerQuestion: 4,
			MinOptionsPerQuestion: 2,
		},
		Topology: taskconfig.Topology{
			MaxIterations: 1,
			Entry:         "start",
			Nodes: []taskconfig.NodeRef{
				{Name: "start"},
				{Name: "done"},
			},
			Edges: []taskconfig.Edge{
				{From: "start", To: "start", When: taskconfig.EdgeCondition{Kind: taskconfig.ConditionWhen, Field: "flag", Equals: "retry"}},
				{From: "start", To: "done", When: taskconfig.EdgeCondition{Kind: taskconfig.ConditionElse}},
			},
		},
		NodeDefinitions: map[string]taskconfig.NodeDefinition{
			"start": simpleAgentNode(),
			"done":  simpleAgentNode(),
		},
	}
}

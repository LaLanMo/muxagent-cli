package taskengine

import (
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/LaLanMo/muxagent-cli/internal/taskconfig"
	"github.com/LaLanMo/muxagent-cli/internal/taskdomain"
)

type Engine struct {
	mu           sync.Mutex
	runTokens    map[string]string
	startedNodes map[string]map[string]map[string]struct{}
	pending      map[string]map[string][]arrival
}

type arrival struct {
	Token     string
	FromNode  string
	FromRunID string
	Reason    string
}

type Transition struct {
	To      string
	Reason  string
	Token   string
	Trigger taskdomain.TriggeredBy
}

type BlockedTransition struct {
	To            string
	Reason        string
	Token         string
	Trigger       taskdomain.TriggeredBy
	FailureReason string
}

type Resolution struct {
	Transitions []Transition
	Blocked     []BlockedTransition
	TaskDone    bool
}

func New() *Engine {
	return &Engine{
		runTokens:    map[string]string{},
		startedNodes: map[string]map[string]map[string]struct{}{},
		pending:      map[string]map[string][]arrival{},
	}
}

func (e *Engine) HasRun(runID string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	_, ok := e.runTokens[runID]
	return ok
}

func (e *Engine) RegisterEntryRun(taskID string, run taskdomain.NodeRun) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.registerRunLocked(taskID, run, run.ID)
}

func (e *Engine) RegisterTriggeredRun(taskID string, run taskdomain.NodeRun, parentRunID string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	token := e.runTokens[parentRunID]
	if token == "" {
		token = parentRunID
	}
	e.registerRunLocked(taskID, run, token)
}

func (e *Engine) registerRunLocked(taskID string, run taskdomain.NodeRun, token string) {
	if token == "" {
		token = run.ID
	}
	e.runTokens[run.ID] = token
	if _, ok := e.startedNodes[taskID]; !ok {
		e.startedNodes[taskID] = map[string]map[string]struct{}{}
	}
	if _, ok := e.startedNodes[taskID][token]; !ok {
		e.startedNodes[taskID][token] = map[string]struct{}{}
	}
	e.startedNodes[taskID][token][run.NodeName] = struct{}{}
}

func (e *Engine) ResolveCompletion(cfg *taskconfig.Config, taskID string, runs []taskdomain.NodeRun, run taskdomain.NodeRun) (Resolution, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	outgoing := outgoingEdges(cfg, run.NodeName)
	var arrivals []Transition
	if len(outgoing) > 0 {
		var err error
		arrivals, err = evaluateEdges(run.Result, outgoing)
		if err != nil {
			return Resolution{}, err
		}
	}

	token := e.runTokens[run.ID]
	if token == "" {
		token = run.ID
		e.registerRunLocked(taskID, run, token)
	}
	if _, ok := e.pending[taskID]; !ok {
		e.pending[taskID] = map[string][]arrival{}
	}
	for _, next := range arrivals {
		e.pending[taskID][next.To] = append(e.pending[taskID][next.To], arrival{
			Token:     token,
			FromNode:  run.NodeName,
			FromRunID: run.ID,
			Reason:    next.Reason,
		})
	}

	resolution := Resolution{}
	for _, next := range e.consumeReadyLocked(cfg, taskID) {
		if exceedsIterations(cfg, runs, next.To) {
			resolution.Blocked = append(resolution.Blocked, BlockedTransition{
				To:            next.To,
				Reason:        next.Reason,
				Token:         next.Token,
				Trigger:       next.Trigger,
				FailureReason: fmt.Sprintf("node %q exceeded max_iterations", next.To),
			})
			continue
		}
		resolution.Transitions = append(resolution.Transitions, next)
	}
	resolution.TaskDone = len(resolution.Transitions) == 0 && len(resolution.Blocked) == 0 && !hasPendingArrivals(e.pending[taskID]) && taskFinished(cfg, runs)
	return resolution, nil
}

func (e *Engine) consumeReadyLocked(cfg *taskconfig.Config, taskID string) []Transition {
	nodes := make([]string, 0, len(e.pending[taskID]))
	for nodeName := range e.pending[taskID] {
		nodes = append(nodes, nodeName)
	}
	sort.Strings(nodes)

	var transitions []Transition
	for _, nodeName := range nodes {
		node := cfg.Topology.Node(nodeName)
		groups := map[string][]arrival{}
		for _, item := range e.pending[taskID][nodeName] {
			groups[item.Token] = append(groups[item.Token], item)
		}
		for token, items := range groups {
			ready := false
			if _, seen := e.startedNodes[taskID][token][nodeName]; seen {
				ready = len(items) > 0
			} else {
				switch node.JoinOrDefault() {
				case taskconfig.JoinAny:
					ready = len(items) > 0
				default:
					required := incomingNodesForToken(e.startedNodes[taskID][token], cfg, nodeName)
					arrived := map[string]struct{}{}
					for _, item := range items {
						arrived[item.FromNode] = struct{}{}
					}
					ready = len(required) > 0 && len(arrived) == len(required)
				}
			}
			if !ready {
				continue
			}
			first := items[0]
			transitions = append(transitions, Transition{
				To:     nodeName,
				Reason: first.Reason,
				Token:  token,
				Trigger: taskdomain.TriggeredBy{
					NodeRunID: first.FromRunID,
					Reason:    first.Reason,
				},
			})
			remaining := e.pending[taskID][nodeName][:0]
			for _, item := range e.pending[taskID][nodeName] {
				if item.Token != token {
					remaining = append(remaining, item)
				}
			}
			e.pending[taskID][nodeName] = remaining
			if len(remaining) == 0 {
				delete(e.pending[taskID], nodeName)
			}
		}
	}
	return transitions
}

func outgoingEdges(cfg *taskconfig.Config, nodeName string) []taskconfig.Edge {
	var edges []taskconfig.Edge
	for _, edge := range cfg.Topology.Edges {
		if edge.From == nodeName {
			edges = append(edges, edge)
		}
	}
	return edges
}

func evaluateEdges(result map[string]interface{}, edges []taskconfig.Edge) ([]Transition, error) {
	if len(edges) == 0 {
		return nil, nil
	}
	unconditional := false
	for _, edge := range edges {
		if edge.When.Kind == taskconfig.ConditionNone {
			unconditional = true
			break
		}
	}
	if unconditional {
		transitions := make([]Transition, 0, len(edges))
		for _, edge := range edges {
			transitions = append(transitions, Transition{
				To:     edge.To,
				Reason: fmt.Sprintf("edge: %s -> %s", edge.From, edge.To),
			})
		}
		return transitions, nil
	}

	var matched []Transition
	var elseEdge *taskconfig.Edge
	for _, edge := range edges {
		switch edge.When.Kind {
		case taskconfig.ConditionElse:
			copy := edge
			elseEdge = &copy
		case taskconfig.ConditionWhen:
			if matchesCondition(result, edge.When) {
				matched = append(matched, Transition{
					To:     edge.To,
					Reason: fmt.Sprintf("edge: %s=%v", edge.When.Field, edge.When.Equals),
				})
			}
		}
	}
	if len(matched) > 0 {
		return matched, nil
	}
	if elseEdge != nil {
		return []Transition{{
			To:     elseEdge.To,
			Reason: "edge: else",
		}}, nil
	}
	return nil, fmt.Errorf("no conditional edges matched and no else edge is defined")
}

func matchesCondition(result map[string]interface{}, when taskconfig.EdgeCondition) bool {
	if result == nil {
		return false
	}
	value, ok := result[when.Field]
	if !ok {
		return false
	}
	return taskconfig.ValuesEqual(value, when.Equals)
}

func incomingNodesForToken(started map[string]struct{}, cfg *taskconfig.Config, target string) map[string]struct{} {
	required := map[string]struct{}{}
	for _, edge := range cfg.Topology.Edges {
		if edge.To != target {
			continue
		}
		if _, ok := started[edge.From]; ok {
			required[edge.From] = struct{}{}
		}
	}
	return required
}

func exceedsIterations(cfg *taskconfig.Config, runs []taskdomain.NodeRun, nodeName string) bool {
	return taskdomain.IterationCount(runs, nodeName) >= taskdomain.MaxIterationsForNode(cfg, nodeName)
}

func hasPendingArrivals(pending map[string][]arrival) bool {
	for _, items := range pending {
		if len(items) > 0 {
			return true
		}
	}
	return false
}

func taskFinished(cfg *taskconfig.Config, runs []taskdomain.NodeRun) bool {
	terminalNodes := taskdomain.TerminalNodes(cfg)
	doneTerminal := false
	for _, run := range runs {
		switch run.Status {
		case taskdomain.NodeRunRunning, taskdomain.NodeRunAwaitingUser:
			return false
		case taskdomain.NodeRunDone:
			if terminalNodes[run.NodeName] {
				doneTerminal = true
			}
		}
	}
	if taskdomain.HasOpenFailedRuns(runs) {
		return false
	}
	return doneTerminal
}

func DeriveBlockedSteps(cfg *taskconfig.Config, runs []taskdomain.NodeRun) ([]taskdomain.BlockedStep, error) {
	if len(runs) == 0 {
		return nil, nil
	}
	sorted := append([]taskdomain.NodeRun(nil), runs...)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].StartedAt.Equal(sorted[j].StartedAt) {
			return sorted[i].ID < sorted[j].ID
		}
		return sorted[i].StartedAt.Before(sorted[j].StartedAt)
	})

	taskID := sorted[0].TaskID
	engine := New()
	history := make([]taskdomain.NodeRun, 0, len(sorted))
	open := map[string]taskdomain.BlockedStep{}
	order := []string{}

	for _, run := range sorted {
		if run.TriggeredBy == nil {
			engine.RegisterEntryRun(taskID, run)
		} else {
			if taskdomain.IsManualContinueReason(run.TriggeredBy.Reason) {
				delete(open, blockedStepKey(run.TriggeredBy.NodeRunID, run.NodeName))
			}
			engine.RegisterTriggeredRun(taskID, run, run.TriggeredBy.NodeRunID)
		}
		history = append(history, run)
		if run.Status != taskdomain.NodeRunDone {
			continue
		}

		resolution, err := engine.ResolveCompletion(cfg, taskID, history, run)
		if err != nil {
			return nil, fmt.Errorf("derive blocked steps for task %s at run %s (%s): %w", taskID, run.ID, run.NodeName, err)
		}
		blockedIterations := map[string]int{}
		for _, blocked := range resolution.Blocked {
			nextIteration := blockedIterations[blocked.To]
			if nextIteration == 0 {
				nextIteration = taskdomain.IterationCount(history, blocked.To) + 1
			}
			key := blockedStepKey(blocked.Trigger.NodeRunID, blocked.To)
			if _, exists := open[key]; !exists {
				order = append(order, key)
			}
			open[key] = taskdomain.BlockedStep{
				NodeName:    blocked.To,
				Iteration:   nextIteration,
				Reason:      blocked.FailureReason,
				TriggeredBy: &blocked.Trigger,
				CreatedAt:   completionTimestamp(run),
			}
			blockedIterations[blocked.To] = nextIteration + 1
		}
	}

	steps := make([]taskdomain.BlockedStep, 0, len(open))
	for _, key := range order {
		step, ok := open[key]
		if !ok {
			continue
		}
		steps = append(steps, step)
	}
	sort.Slice(steps, func(i, j int) bool {
		if steps[i].CreatedAt.Equal(steps[j].CreatedAt) {
			left := blockedStepKey(steps[i].TriggeredBy.NodeRunID, steps[i].NodeName)
			right := blockedStepKey(steps[j].TriggeredBy.NodeRunID, steps[j].NodeName)
			return left < right
		}
		return steps[i].CreatedAt.Before(steps[j].CreatedAt)
	})
	return steps, nil
}

func completionTimestamp(run taskdomain.NodeRun) time.Time {
	if run.CompletedAt != nil {
		return run.CompletedAt.UTC()
	}
	return run.StartedAt.UTC()
}

func blockedStepKey(sourceRunID, nodeName string) string {
	return sourceRunID + "->" + nodeName
}

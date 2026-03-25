package taskdomain

import (
	"sort"
	"time"

	"github.com/LaLanMo/muxagent-cli/internal/taskconfig"
)

type Task struct {
	ID          string
	Description string
	WorkDir     string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type NodeRunStatus string

const (
	NodeRunRunning      NodeRunStatus = "running"
	NodeRunDone         NodeRunStatus = "done"
	NodeRunFailed       NodeRunStatus = "failed"
	NodeRunAwaitingUser NodeRunStatus = "awaiting_user"
)

type TaskStatus string

const (
	TaskStatusDraft        TaskStatus = "draft"
	TaskStatusRunning      TaskStatus = "running"
	TaskStatusAwaitingUser TaskStatus = "awaiting_user"
	TaskStatusDone         TaskStatus = "done"
	TaskStatusFailed       TaskStatus = "failed"
)

type NodeRun struct {
	ID             string
	TaskID         string
	NodeName       string
	Status         NodeRunStatus
	SessionID      string
	FailureReason  string
	Result         map[string]interface{}
	Clarifications []ClarificationExchange
	TriggeredBy    *TriggeredBy
	StartedAt      time.Time
	CompletedAt    *time.Time
}

type BlockedStep struct {
	NodeName    string
	Iteration   int
	Reason      string
	TriggeredBy *TriggeredBy
	CreatedAt   time.Time
}

type TriggeredBy struct {
	NodeRunID string `json:"nodeRunId"`
	Reason    string `json:"reason"`
}

type ClarificationExchange struct {
	Request     ClarificationRequest   `json:"request"`
	Response    *ClarificationResponse `json:"response,omitempty"`
	RequestedAt time.Time              `json:"requestedAt"`
	AnsweredAt  *time.Time             `json:"answeredAt,omitempty"`
}

type ClarificationRequest struct {
	Questions []ClarificationQuestion `json:"questions"`
}

type ClarificationQuestion struct {
	Question     string                `json:"question"`
	WhyItMatters string                `json:"why_it_matters"`
	Options      []ClarificationOption `json:"options"`
	MultiSelect  bool                  `json:"multi_select,omitempty"`
}

type ClarificationOption struct {
	Label       string `json:"label"`
	Description string `json:"description"`
}

type ClarificationResponse struct {
	Answers []ClarificationAnswer `json:"answers"`
}

type ClarificationAnswer struct {
	Selected interface{} `json:"selected"`
}

type TaskView struct {
	Task            Task
	Status          TaskStatus
	CurrentNodeName string
	CurrentNodeType taskconfig.NodeType
	CurrentIssue    *TaskIssue
	ArtifactPaths   []string
	NodeRuns        []NodeRunView
	BlockedSteps    []BlockedStep
}

type NodeRunView struct {
	NodeRun
	ArtifactPaths []string
}

type TaskIssueKind string

const (
	TaskIssueFailedRun   TaskIssueKind = "failed_run"
	TaskIssueBlockedStep TaskIssueKind = "blocked_step"
)

type TaskIssue struct {
	Kind       TaskIssueKind
	NodeName   string
	Iteration  int
	Reason     string
	OccurredAt time.Time
}

func DeriveTaskView(task Task, cfg *taskconfig.Config, runs []NodeRun, blockedSteps []BlockedStep) TaskView {
	sorted := append([]NodeRun(nil), runs...)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].StartedAt.Equal(sorted[j].StartedAt) {
			return sorted[i].ID < sorted[j].ID
		}
		return sorted[i].StartedAt.Before(sorted[j].StartedAt)
	})
	sortedBlocked := append([]BlockedStep(nil), blockedSteps...)
	sort.Slice(sortedBlocked, func(i, j int) bool {
		if sortedBlocked[i].CreatedAt.Equal(sortedBlocked[j].CreatedAt) {
			left := sortedBlocked[i].NodeName
			right := sortedBlocked[j].NodeName
			if left == right {
				leftSource := ""
				rightSource := ""
				if sortedBlocked[i].TriggeredBy != nil {
					leftSource = sortedBlocked[i].TriggeredBy.NodeRunID
				}
				if sortedBlocked[j].TriggeredBy != nil {
					rightSource = sortedBlocked[j].TriggeredBy.NodeRunID
				}
				return leftSource < rightSource
			}
			return left < right
		}
		return sortedBlocked[i].CreatedAt.Before(sortedBlocked[j].CreatedAt)
	})

	view := TaskView{
		Task:          task,
		Status:        TaskStatusDraft,
		ArtifactPaths: []string{},
		NodeRuns:      make([]NodeRunView, 0, len(sorted)),
		BlockedSteps:  sortedBlocked,
	}
	if len(sorted) == 0 && len(sortedBlocked) == 0 {
		return view
	}

	var latestNodeName string
	var latestNodeType taskconfig.NodeType
	var latestAt time.Time
	for _, run := range sorted {
		artifacts := ArtifactPaths(run.Result)
		view.NodeRuns = append(view.NodeRuns, NodeRunView{
			NodeRun:       run,
			ArtifactPaths: artifacts,
		})
		view.ArtifactPaths = append(view.ArtifactPaths, artifacts...)
		if run.StartedAt.After(latestAt) || (run.StartedAt.Equal(latestAt) && latestNodeName == "") {
			latestAt = run.StartedAt
			latestNodeName = run.NodeName
			latestNodeType = cfg.NodeDefinitions[run.NodeName].Type
		}
	}
	for _, blocked := range sortedBlocked {
		if blocked.CreatedAt.After(latestAt) || (blocked.CreatedAt.Equal(latestAt) && latestNodeName == "") {
			latestAt = blocked.CreatedAt
			latestNodeName = blocked.NodeName
			latestNodeType = cfg.NodeDefinitions[blocked.NodeName].Type
		}
	}

	view.CurrentNodeName = latestNodeName
	view.CurrentNodeType = latestNodeType
	view.Status = deriveTaskStatus(cfg, sorted, sortedBlocked)
	view.CurrentIssue = CurrentIssueForTask(cfg, sorted, sortedBlocked)

	for i := len(sorted) - 1; i >= 0; i-- {
		run := sorted[i]
		if run.Status == NodeRunAwaitingUser || run.Status == NodeRunRunning {
			view.CurrentNodeName = run.NodeName
			view.CurrentNodeType = cfg.NodeDefinitions[run.NodeName].Type
			return view
		}
	}
	if view.Status == TaskStatusFailed && view.CurrentIssue != nil {
		view.CurrentNodeName = view.CurrentIssue.NodeName
		view.CurrentNodeType = cfg.NodeDefinitions[view.CurrentIssue.NodeName].Type
	}

	return view
}

func deriveTaskStatus(cfg *taskconfig.Config, runs []NodeRun, blockedSteps []BlockedStep) TaskStatus {
	for _, run := range runs {
		if run.Status == NodeRunAwaitingUser {
			return TaskStatusAwaitingUser
		}
	}
	for _, run := range runs {
		if run.Status == NodeRunRunning {
			return TaskStatusRunning
		}
	}
	if len(blockedSteps) > 0 {
		return TaskStatusFailed
	}
	if HasOpenFailedRuns(runs) {
		return TaskStatusFailed
	}
	terminals := TerminalNodes(cfg)
	for i := len(runs) - 1; i >= 0; i-- {
		run := runs[i]
		if run.Status == NodeRunDone && terminals[run.NodeName] {
			return TaskStatusDone
		}
	}
	return TaskStatusRunning
}

func ArtifactPaths(result map[string]interface{}) []string {
	if len(result) == 0 {
		return nil
	}
	raw, ok := result["file_paths"]
	if !ok {
		return nil
	}
	if items, ok := raw.([]string); ok {
		paths := make([]string, 0, len(items))
		for _, item := range items {
			if item != "" {
				paths = append(paths, item)
			}
		}
		return paths
	}
	items, ok := raw.([]interface{})
	if !ok {
		return nil
	}
	paths := make([]string, 0, len(items))
	for _, item := range items {
		if text, ok := item.(string); ok && text != "" {
			paths = append(paths, text)
		}
	}
	return paths
}

func TerminalNodes(cfg *taskconfig.Config) map[string]bool {
	withOutgoing := map[string]bool{}
	for _, edge := range cfg.Topology.Edges {
		withOutgoing[edge.From] = true
	}
	terminals := map[string]bool{}
	for _, node := range cfg.Topology.Nodes {
		if !withOutgoing[node.Name] {
			terminals[node.Name] = true
		}
	}
	return terminals
}

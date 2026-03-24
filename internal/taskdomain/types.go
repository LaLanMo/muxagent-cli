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
	ArtifactPaths   []string
	NodeRuns        []NodeRunView
}

type NodeRunView struct {
	NodeRun
	ArtifactPaths []string
}

func DeriveTaskView(task Task, cfg *taskconfig.Config, runs []NodeRun) TaskView {
	sorted := append([]NodeRun(nil), runs...)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].StartedAt.Equal(sorted[j].StartedAt) {
			return sorted[i].ID < sorted[j].ID
		}
		return sorted[i].StartedAt.Before(sorted[j].StartedAt)
	})

	view := TaskView{
		Task:          task,
		Status:        TaskStatusDraft,
		ArtifactPaths: []string{},
		NodeRuns:      make([]NodeRunView, 0, len(sorted)),
	}
	if len(sorted) == 0 {
		return view
	}

	var latest NodeRun
	for _, run := range sorted {
		artifacts := ArtifactPaths(run.Result)
		view.NodeRuns = append(view.NodeRuns, NodeRunView{
			NodeRun:       run,
			ArtifactPaths: artifacts,
		})
		view.ArtifactPaths = append(view.ArtifactPaths, artifacts...)
		latest = run
	}

	view.CurrentNodeName = latest.NodeName
	def := cfg.NodeDefinitions[latest.NodeName]
	view.CurrentNodeType = def.Type
	view.Status = deriveTaskStatus(cfg, sorted)

	for i := len(sorted) - 1; i >= 0; i-- {
		run := sorted[i]
		if run.Status == NodeRunAwaitingUser || run.Status == NodeRunRunning {
			view.CurrentNodeName = run.NodeName
			view.CurrentNodeType = cfg.NodeDefinitions[run.NodeName].Type
			break
		}
	}

	return view
}

func deriveTaskStatus(cfg *taskconfig.Config, runs []NodeRun) TaskStatus {
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

package tasktui

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"charm.land/bubbles/v2/list"

	"github.com/LaLanMo/muxagent-cli/internal/taskconfig"
	"github.com/LaLanMo/muxagent-cli/internal/taskdomain"
)

func trimTrailingBlank(lines []string) []string {
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func normalizeVersionLabel(version string) string {
	version = strings.TrimSpace(version)
	if version == "" {
		return "v0.1.0"
	}
	version = strings.TrimPrefix(version, "muxagent version ")
	version = strings.TrimPrefix(version, "version ")
	fields := strings.Fields(version)
	if len(fields) == 0 {
		return "v0.1.0"
	}
	label := fields[0]
	if strings.HasPrefix(label, "v") {
		return label
	}
	return label
}

func relativeTime(ts time.Time) string {
	if ts.IsZero() {
		return "just now"
	}
	delta := time.Since(ts)
	switch {
	case delta < time.Minute:
		return "just now"
	case delta < time.Hour:
		return fmt.Sprintf("%dm ago", int(delta.Minutes()))
	case delta < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(delta.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(delta.Hours()/24))
	}
}

func shortDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm %02ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%dh %02dm", int(d.Hours()), int(d.Minutes())%60)
}

func nodeRunTimestamp(run taskdomain.NodeRunView) time.Time {
	if run.CompletedAt != nil {
		return run.CompletedAt.UTC()
	}
	return run.StartedAt.UTC()
}

func currentWorkDir(current *taskdomain.TaskView) string {
	if current == nil {
		return ""
	}
	return current.Task.WorkDir
}

func taskSummaryLeft(view *taskdomain.TaskView, cfg *taskconfig.Config) string {
	if view == nil {
		return ""
	}
	nodeCount := len(view.NodeRuns)
	if cfg != nil {
		nodeCount = len(cfg.Topology.Nodes)
	}
	return fmt.Sprintf("%d nodes · %d runs · %d iterations", nodeCount, len(view.NodeRuns), taskIterations(view))
}

func taskSummaryRight(view *taskdomain.TaskView) string {
	if view == nil {
		return ""
	}
	if strings.TrimSpace(view.Task.ConfigAlias) != "" {
		return fmt.Sprintf("config %s · %s · %d artifacts", view.Task.ConfigAlias, taskElapsed(view), len(view.ArtifactPaths))
	}
	return fmt.Sprintf("%s · %d artifacts", taskElapsed(view), len(view.ArtifactPaths))
}

func taskIterations(view *taskdomain.TaskView) int {
	if view == nil {
		return 0
	}
	seen := map[string]struct{}{}
	for _, run := range view.NodeRuns {
		seen[run.NodeName] = struct{}{}
	}
	return max(0, len(view.NodeRuns)-len(seen))
}

func taskElapsed(view *taskdomain.TaskView) string {
	if view == nil {
		return "0s"
	}
	end := time.Now().UTC()
	if len(view.NodeRuns) > 0 {
		last := view.NodeRuns[len(view.NodeRuns)-1]
		if last.CompletedAt != nil {
			end = last.CompletedAt.UTC()
		}
	}
	return shortDuration(end.Sub(view.Task.CreatedAt))
}

func shortenPath(path, workDir string) string {
	if path == "" {
		return ""
	}
	if workDir != "" {
		if rel, err := filepath.Rel(workDir, path); err == nil && rel != "" && !strings.HasPrefix(rel, "..") {
			path = rel
		}
	}
	path = filepath.ToSlash(path)
	parts := strings.Split(path, "/")
	if len(parts) >= 6 && parts[0] == ".muxagent" && parts[1] == "tasks" && parts[3] == "artifacts" {
		taskID := parts[2]
		if len(taskID) > 8 {
			taskID = taskID[:8]
		}
		return filepath.ToSlash(filepath.Join(".muxagent", "tasks", taskID, parts[len(parts)-1]))
	}
	home := filepath.ToSlash(filepath.Clean(filepath.Dir(workDir)))
	if home != "" {
		path = strings.TrimPrefix(path, home+"/")
	}
	return path
}

func selectedTaskListItem(model list.Model) (taskListItem, bool) {
	if item, ok := model.SelectedItem().(taskListItem); ok {
		return item, true
	}
	items := model.Items()
	if len(items) == 0 {
		return taskListItem{}, false
	}
	index := clamp(model.Index(), 0, len(items)-1)
	item, ok := items[index].(taskListItem)
	return item, ok
}

func selectTaskListTask(model *list.Model, taskID string) bool {
	if model == nil || strings.TrimSpace(taskID) == "" {
		return false
	}
	items := model.Items()
	for i, item := range items {
		entry, ok := item.(taskListItem)
		if !ok || entry.action != taskListActionNone {
			continue
		}
		if entry.view.Task.ID == taskID {
			model.Select(i)
			return true
		}
	}
	return false
}

func selectedTaskConfigListItem(model list.Model) (taskConfigListItem, bool) {
	if item, ok := model.SelectedItem().(taskConfigListItem); ok {
		return item, true
	}
	items := model.Items()
	if len(items) == 0 {
		return taskConfigListItem{}, false
	}
	index := clamp(model.Index(), 0, len(items)-1)
	item, ok := items[index].(taskConfigListItem)
	return item, ok
}

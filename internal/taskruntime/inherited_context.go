package taskruntime

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/LaLanMo/muxagent-cli/internal/taskdomain"
	"github.com/LaLanMo/muxagent-cli/internal/taskstore"
)

type inheritedContext struct {
	WorkflowHistory string
}

func (s *Service) loadInheritedContext(ctx context.Context, task taskdomain.Task) (*inheritedContext, error) {
	parentTaskID, err := s.store.GetFollowUpParentTaskID(ctx, task.ID)
	if err != nil {
		return nil, err
	}
	if parentTaskID == "" {
		return nil, nil
	}

	parentTask, err := s.store.GetTask(ctx, parentTaskID)
	if err != nil {
		return nil, err
	}
	parentRuns, err := s.store.ListNodeRunsByTask(ctx, parentTaskID)
	if err != nil {
		return nil, err
	}
	directParentRuns := completedRuns(parentRuns)
	parentTaskDir := taskstore.TaskDir(parentTask.WorkDir, parentTask.ID)

	ancestorIDs, err := s.store.ListAncestorTaskIDs(ctx, task.ID)
	if err != nil {
		return nil, err
	}
	ancestorReferences := make([]string, 0, len(ancestorIDs))
	for _, ancestorTaskID := range ancestorIDs {
		if ancestorTaskID == parentTaskID {
			continue
		}
		ancestorTask, err := s.store.GetTask(ctx, ancestorTaskID)
		if err != nil {
			return nil, err
		}
		ancestorReferences = append(ancestorReferences, formatAncestorTaskReference(ancestorTask))
	}

	workflowSections := []string{
		"## Direct Parent Task",
		fmt.Sprintf("Description: %s", parentTask.Description),
		fmt.Sprintf("Task directory: %s", parentTaskDir),
		"",
		"## Direct Parent Workflow History (oldest first)",
		summarizeWorkflowHistory(directParentRuns),
	}
	if len(ancestorReferences) > 0 {
		workflowSections = append(workflowSections,
			"",
			"## Earlier Ancestors (inspect only if needed)",
			joinLines(ancestorReferences),
		)
	}

	return &inheritedContext{
		WorkflowHistory: strings.Join(workflowSections, "\n"),
	}, nil
}

func (s *Service) loadInheritedInputArtifacts(ctx context.Context, task taskdomain.Task) ([]string, error) {
	parentTaskID, err := s.store.GetFollowUpParentTaskID(ctx, task.ID)
	if err != nil {
		return nil, err
	}
	if parentTaskID == "" {
		return nil, nil
	}
	parentTask, err := s.store.GetTask(ctx, parentTaskID)
	if err != nil {
		return nil, err
	}
	parentRuns, err := s.store.ListNodeRunsByTask(ctx, parentTaskID)
	if err != nil {
		return nil, err
	}
	return existingArtifactPaths(resolveArtifactPaths(parentTask, parentRuns)), nil
}

func completedRuns(runs []taskdomain.NodeRun) []taskdomain.NodeRun {
	completed := make([]taskdomain.NodeRun, 0, len(runs))
	for _, run := range runs {
		if run.Status == taskdomain.NodeRunDone {
			completed = append(completed, run)
		}
	}
	return completed
}

func existingArtifactPaths(paths []string) []string {
	seen := map[string]struct{}{}
	reversed := make([]string, 0, len(paths))
	for i := len(paths) - 1; i >= 0; i-- {
		path := strings.TrimSpace(paths[i])
		if path == "" {
			continue
		}
		if _, ok := seen[path]; ok {
			continue
		}
		if _, err := os.Stat(path); err != nil {
			continue
		}
		seen[path] = struct{}{}
		reversed = append(reversed, path)
	}
	result := make([]string, 0, len(reversed))
	for i := len(reversed) - 1; i >= 0; i-- {
		result = append(result, reversed[i])
	}
	return result
}

func formatAncestorTaskReference(task taskdomain.Task) string {
	description := strings.TrimSpace(task.Description)
	if description == "" {
		description = "(no description)"
	}
	return strings.Join([]string{
		fmt.Sprintf("- %s", description),
		fmt.Sprintf("  Task directory: %s", taskstore.TaskDir(task.WorkDir, task.ID)),
	}, "\n")
}

func resolveArtifactPaths(task taskdomain.Task, runs []taskdomain.NodeRun) []string {
	resolved := make([]string, 0)
	for _, run := range runs {
		if run.Status != taskdomain.NodeRunDone {
			continue
		}
		artifactDir, err := runArtifactPathForExistingRun(task, runs, run, ".")
		if err != nil {
			continue
		}
		baseDir := artifactDir
		for _, rawPath := range taskdomain.ArtifactPaths(run.Result) {
			path := strings.TrimSpace(rawPath)
			if path == "" {
				continue
			}
			if !filepath.IsAbs(path) {
				path = filepath.Join(baseDir, path)
			}
			resolved = append(resolved, path)
		}
	}
	return resolved
}

func mergeArtifactPaths(current, inherited []string) []string {
	merged := make([]string, 0, len(current)+len(inherited))
	seen := map[string]struct{}{}
	for _, group := range [][]string{current, inherited} {
		for _, path := range group {
			path = strings.TrimSpace(path)
			if path == "" {
				continue
			}
			if _, ok := seen[path]; ok {
				continue
			}
			seen[path] = struct{}{}
			merged = append(merged, path)
		}
	}
	return merged
}

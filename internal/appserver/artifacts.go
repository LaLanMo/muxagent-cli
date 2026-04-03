package appserver

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/LaLanMo/muxagent-cli/internal/taskdomain"
	"github.com/LaLanMo/muxagent-cli/internal/taskruntime"
	"github.com/LaLanMo/muxagent-cli/internal/taskstore"
)

func (s *Server) loadTaskArtifactRefs(ctx context.Context, taskID string) ([]artifactRefDto, error) {
	view, _, err := s.service.LoadTaskView(ctx, taskID)
	if err != nil {
		return nil, err
	}
	var input *taskruntime.InputRequest
	if view.Status == taskdomain.TaskStatusAwaitingUser {
		if nodeRunID := latestAwaitingRunID(view); nodeRunID != "" {
			input, err = s.service.BuildInputRequest(ctx, taskID, nodeRunID)
			if err != nil {
				return nil, err
			}
		}
	}
	return buildTaskArtifactRefs(view, input), nil
}

func buildTaskArtifactRefs(view taskdomain.TaskView, input *taskruntime.InputRequest) []artifactRefDto {
	refs := make([]artifactRefDto, 0)
	seenResolved := map[string]struct{}{}
	runOrdinals := map[string]int{}
	runLabels := map[string]string{}
	workspaceRoot := taskstore.NormalizeWorkDir(view.Task.WorkDir)

	for i, run := range view.NodeRuns {
		runOrdinals[run.NodeName]++
		sourceLabel := fmt.Sprintf("%s (#%d)", run.NodeName, runOrdinals[run.NodeName])
		runLabels[run.ID] = sourceLabel
		for _, rawPath := range run.ArtifactPaths {
			ref, ok := artifactRefForRun(view.Task, i+1, run, rawPath, sourceLabel)
			if !ok {
				continue
			}
			if _, exists := seenResolved[ref.ResolvedPath]; exists {
				continue
			}
			seenResolved[ref.ResolvedPath] = struct{}{}
			refs = append(refs, ref)
		}
	}

	if input != nil {
		sourceLabel := runLabels[input.NodeRunID]
		if sourceLabel == "" {
			sourceLabel = artifactInputGroupLabel(input, runOrdinals)
		}
		for _, rawPath := range input.ArtifactPaths {
			rawPath = strings.TrimSpace(rawPath)
			if rawPath == "" || !filepath.IsAbs(rawPath) {
				continue
			}
			resolvedPath := filepath.Clean(rawPath)
			if !artifactPathWithinWorkspace(resolvedPath, workspaceRoot) {
				continue
			}
			if _, exists := seenResolved[resolvedPath]; exists {
				continue
			}
			seenResolved[resolvedPath] = struct{}{}
			refs = append(refs, artifactRefDto{
				TaskID:       view.Task.ID,
				NodeRunID:    input.NodeRunID,
				NodeName:     input.NodeName,
				SourceLabel:  sourceLabel,
				RawPath:      rawPath,
				ResolvedPath: resolvedPath,
				DisplayPath:  artifactDisplayPath(resolvedPath, view.Task.WorkDir),
				PreviewName:  filepath.Base(resolvedPath),
				PreviewTitle: sourceLabel + " · " + filepath.Base(resolvedPath),
				Markdown:     isMarkdownPreview(resolvedPath),
			})
		}
	}

	for _, rawPath := range view.ArtifactPaths {
		rawPath = strings.TrimSpace(rawPath)
		if rawPath == "" || !filepath.IsAbs(rawPath) {
			continue
		}
		resolvedPath := filepath.Clean(rawPath)
		if !artifactPathWithinWorkspace(resolvedPath, workspaceRoot) {
			continue
		}
		if _, exists := seenResolved[resolvedPath]; exists {
			continue
		}
		seenResolved[resolvedPath] = struct{}{}
		refs = append(refs, artifactRefDto{
			TaskID:       view.Task.ID,
			RawPath:      rawPath,
			ResolvedPath: resolvedPath,
			DisplayPath:  artifactDisplayPath(resolvedPath, view.Task.WorkDir),
			PreviewName:  filepath.Base(resolvedPath),
			PreviewTitle: filepath.Base(resolvedPath),
			Markdown:     isMarkdownPreview(resolvedPath),
		})
	}

	return refs
}

func artifactRefForRun(task taskdomain.Task, sequence int, run taskdomain.NodeRunView, rawPath, sourceLabel string) (artifactRefDto, bool) {
	rawPath = strings.TrimSpace(rawPath)
	if rawPath == "" {
		return artifactRefDto{}, false
	}

	resolvedPath := rawPath
	if !filepath.IsAbs(resolvedPath) {
		resolvedPath = filepath.Join(taskstore.ArtifactRunDir(task.WorkDir, task.ID, sequence, run.NodeName), rawPath)
	}
	resolvedPath = filepath.Clean(resolvedPath)
	if !artifactPathWithinWorkspace(resolvedPath, task.WorkDir) {
		return artifactRefDto{}, false
	}
	previewName := filepath.Base(resolvedPath)
	previewTitle := previewName
	if sourceLabel != "" {
		previewTitle = sourceLabel + " · " + previewName
	}

	return artifactRefDto{
		TaskID:       task.ID,
		NodeRunID:    run.ID,
		NodeName:     run.NodeName,
		SourceLabel:  sourceLabel,
		RawPath:      rawPath,
		ResolvedPath: resolvedPath,
		DisplayPath:  artifactDisplayPath(resolvedPath, task.WorkDir),
		PreviewName:  previewName,
		PreviewTitle: previewTitle,
		Markdown:     isMarkdownPreview(resolvedPath),
	}, true
}

func artifactInputGroupLabel(input *taskruntime.InputRequest, ordinals map[string]int) string {
	if input == nil {
		return "Current input"
	}
	nodeName := strings.TrimSpace(input.NodeName)
	if nodeName == "" {
		return "Current input"
	}
	return fmt.Sprintf("%s (#%d)", nodeName, ordinals[nodeName]+1)
}

func artifactPathWithinWorkspace(path, workspaceRoot string) bool {
	path = taskstore.NormalizeWorkDir(strings.TrimSpace(path))
	workspaceRoot = taskstore.NormalizeWorkDir(strings.TrimSpace(workspaceRoot))
	if path == "" || workspaceRoot == "" {
		return false
	}
	rel, err := filepath.Rel(filepath.Clean(workspaceRoot), filepath.Clean(path))
	if err != nil {
		return false
	}
	return rel == "." || (!strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != "..")
}

func artifactDisplayPath(path, workDir string) string {
	if path == "" {
		return ""
	}
	displayPath := path
	if workDir != "" {
		workDir = taskstore.NormalizeWorkDir(workDir)
		if rel, err := filepath.Rel(workDir, path); err == nil && rel != "" && !strings.HasPrefix(rel, "..") {
			displayPath = rel
		}
	}
	displayPath = filepath.ToSlash(displayPath)
	parts := strings.Split(displayPath, "/")
	if len(parts) >= 6 && parts[0] == ".muxagent" && parts[1] == "tasks" && parts[3] == "artifacts" {
		taskID := parts[2]
		if len(taskID) > 8 {
			taskID = taskID[:8]
		}
		prefix := []string{".muxagent", "tasks", taskID, "artifacts"}
		return strings.Join(append(prefix, parts[4:]...), "/")
	}
	return displayPath
}

func isMarkdownPreview(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".md", ".markdown":
		return true
	default:
		return false
	}
}

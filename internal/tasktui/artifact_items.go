package tasktui

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/LaLanMo/muxagent-cli/internal/taskdomain"
	"github.com/LaLanMo/muxagent-cli/internal/taskruntime"
	"github.com/LaLanMo/muxagent-cli/internal/taskstore"
)

const artifactPreviewMaxBytes = 64 * 1024

type artifactItem struct {
	Path         string
	SourceLabel  string
	Label        string
	DisplayPath  string
	PreviewName  string
	PreviewTitle string
	Preview      string
	Previewable  bool
	Truncated    bool
	Markdown     bool

	renderedWidth   int
	renderedPreview string
}

type artifactGroup struct {
	Label string
	Paths []string
}

func buildArtifactItems(workDir string, current *taskdomain.TaskView, input *taskruntime.InputRequest) []artifactItem {
	groups := artifactPaneGroups(current, input)
	items := make([]artifactItem, 0, artifactGroupPathCount(groups))
	for _, group := range groups {
		for _, path := range group.Paths {
			item := loadArtifactItem(path, workDir)
			if strings.TrimSpace(group.Label) != "" {
				item.SourceLabel = group.Label
				item.PreviewTitle = group.Label + " · " + item.PreviewName
			}
			items = append(items, item)
		}
	}
	return items
}

func artifactPaneGroups(current *taskdomain.TaskView, input *taskruntime.InputRequest) []artifactGroup {
	runLabels, ordinals := artifactRunLabels(current)
	groupIndexByRunID := make(map[string]int, len(runLabels))
	seenPaths := map[string]struct{}{}
	groups := make([]artifactGroup, 0, len(runLabels)+1)

	if current != nil {
		for _, run := range current.NodeRuns {
			groupIndexByRunID[run.ID] = len(groups)
			group := artifactGroup{Label: runLabels[run.ID]}
			for _, path := range run.ArtifactPaths {
				artifactAppendUniquePath(&group.Paths, seenPaths, path)
			}
			groups = append(groups, group)
		}
	}

	if input != nil {
		displayPaths := artifactDisplayableInputPaths(current, input)
		index := -1
		if existing, ok := groupIndexByRunID[input.NodeRunID]; ok {
			index = existing
		} else if len(displayPaths) > 0 {
			index = len(groups)
			groups = append(groups, artifactGroup{Label: artifactInputGroupLabel(input, ordinals)})
		}
		if index >= 0 {
			for _, path := range displayPaths {
				artifactAppendUniquePath(&groups[index].Paths, seenPaths, path)
			}
		}
	}

	if current != nil {
		fallbackIndex := -1
		for _, path := range current.ArtifactPaths {
			path = strings.TrimSpace(path)
			if path == "" {
				continue
			}
			if _, ok := seenPaths[path]; ok {
				continue
			}
			if fallbackIndex < 0 {
				fallbackIndex = len(groups)
				groups = append(groups, artifactGroup{})
			}
			artifactAppendUniquePath(&groups[fallbackIndex].Paths, seenPaths, path)
		}
	}

	return artifactNonEmptyGroups(groups)
}

func artifactPaneHasVisibleArtifacts(current *taskdomain.TaskView, input *taskruntime.InputRequest) bool {
	return len(artifactPaneGroups(current, input)) > 0
}

func artifactRunLabels(current *taskdomain.TaskView) (map[string]string, map[string]int) {
	labels := map[string]string{}
	ordinals := map[string]int{}
	if current == nil {
		return labels, ordinals
	}
	for _, run := range current.NodeRuns {
		ordinals[run.NodeName]++
		labels[run.ID] = fmt.Sprintf("%s (#%d)", run.NodeName, ordinals[run.NodeName])
	}
	return labels, ordinals
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

func artifactDisplayableInputPaths(current *taskdomain.TaskView, input *taskruntime.InputRequest) []string {
	if input == nil || len(input.ArtifactPaths) == 0 {
		return nil
	}

	currentTaskPaths := artifactCurrentTaskPathSet(current)
	taskDir := artifactCurrentTaskDir(current)
	paths := make([]string, 0, len(input.ArtifactPaths))
	seen := map[string]struct{}{}
	for _, path := range input.ArtifactPaths {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		if _, ok := currentTaskPaths[path]; ok {
			continue
		}
		if !artifactPathWithinTaskDir(path, taskDir) {
			continue
		}
		artifactAppendUniquePath(&paths, seen, path)
	}
	return paths
}

func artifactCurrentTaskPathSet(current *taskdomain.TaskView) map[string]struct{} {
	paths := map[string]struct{}{}
	if current == nil {
		return paths
	}
	for _, run := range current.NodeRuns {
		for _, path := range run.ArtifactPaths {
			path = strings.TrimSpace(path)
			if path == "" {
				continue
			}
			paths[path] = struct{}{}
		}
	}
	for _, path := range current.ArtifactPaths {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		paths[path] = struct{}{}
	}
	return paths
}

func artifactCurrentTaskDir(current *taskdomain.TaskView) string {
	if current == nil {
		return ""
	}
	taskID := strings.TrimSpace(current.Task.ID)
	workDir := strings.TrimSpace(current.Task.WorkDir)
	if taskID == "" || workDir == "" {
		return ""
	}
	return taskstore.TaskDir(workDir, taskID)
}

func artifactPathWithinTaskDir(path, taskDir string) bool {
	path = strings.TrimSpace(path)
	taskDir = strings.TrimSpace(taskDir)
	if path == "" || taskDir == "" {
		return false
	}
	rel, err := filepath.Rel(filepath.Clean(taskDir), filepath.Clean(path))
	if err != nil {
		return false
	}
	return rel == "." || (!strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != "..")
}

func artifactAppendUniquePath(paths *[]string, seen map[string]struct{}, path string) {
	path = strings.TrimSpace(path)
	if path == "" {
		return
	}
	if _, ok := seen[path]; ok {
		return
	}
	seen[path] = struct{}{}
	*paths = append(*paths, path)
}

func artifactNonEmptyGroups(groups []artifactGroup) []artifactGroup {
	nonEmpty := make([]artifactGroup, 0, len(groups))
	for _, group := range groups {
		if len(group.Paths) == 0 {
			continue
		}
		nonEmpty = append(nonEmpty, group)
	}
	return nonEmpty
}

func artifactGroupPathCount(groups []artifactGroup) int {
	total := 0
	for _, group := range groups {
		total += len(group.Paths)
	}
	return total
}

func loadArtifactItem(path, workDir string) artifactItem {
	item := artifactItem{
		Path:         path,
		Label:        shortenPath(path, workDir),
		DisplayPath:  artifactDisplayPath(path, workDir),
		PreviewName:  filepath.Base(path),
		PreviewTitle: filepath.Base(path),
		Markdown:     isMarkdownPreview(path),
	}

	file, err := os.Open(path)
	if err != nil {
		item.Preview = fmt.Sprintf("Unable to open file.\n\n%s", err)
		return item
	}
	defer file.Close()

	data, err := io.ReadAll(io.LimitReader(file, artifactPreviewMaxBytes+1))
	if err != nil {
		item.Preview = fmt.Sprintf("Unable to read file.\n\n%s", err)
		return item
	}

	if len(data) > artifactPreviewMaxBytes {
		data = data[:artifactPreviewMaxBytes]
		item.Truncated = true
	}

	if !isPreviewableText(path, data) {
		item.Preview = "Preview unavailable for this file type."
		return item
	}

	item.Previewable = true
	item.Preview = strings.TrimRight(string(data), "\n")
	if item.Preview == "" {
		item.Preview = "(empty file)"
	}
	if item.Truncated {
		item.Preview += "\n\n… Preview truncated"
	}
	return item
}

func artifactDisplayPath(path, workDir string) string {
	if path == "" {
		return ""
	}
	displayPath := path
	if workDir != "" {
		if rel, err := filepath.Rel(workDir, path); err == nil && rel != "" && !strings.HasPrefix(rel, "..") {
			displayPath = rel
		}
	}
	displayPath = filepath.ToSlash(displayPath)
	parts := strings.Split(displayPath, "/")
	if len(parts) >= 6 && parts[0] == ".muxagent" && parts[1] == "tasks" && parts[3] == "runs" {
		taskID := parts[2]
		runID := parts[4]
		if len(taskID) > 8 {
			taskID = taskID[:8]
		}
		if len(runID) > 8 {
			runID = runID[:8]
		}
		prefix := []string{".muxagent", "tasks", taskID, "runs", runID}
		return strings.Join(append(prefix, parts[5:]...), "/")
	}
	return displayPath
}

func isPreviewableText(path string, data []byte) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".md", ".txt", ".json", ".yaml", ".yml", ".toml", ".log", ".go", ".ts", ".tsx", ".js", ".jsx", ".py", ".sh", ".sql", ".rs", ".html", ".css":
		return utf8.Valid(data)
	}
	return utf8.Valid(data) && !bytes.Contains(data, []byte{0})
}

func isMarkdownPreview(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".md", ".markdown":
		return true
	default:
		return false
	}
}

func defaultArtifactIndex(items []artifactItem, input *taskruntime.InputRequest) int {
	if len(items) == 0 {
		return 0
	}
	if input != nil && len(input.ArtifactPaths) > 0 {
		return 0
	}
	return len(items) - 1
}

func selectedArtifactPath(items []artifactItem, index int) string {
	if index < 0 || index >= len(items) {
		return ""
	}
	return items[index].Path
}

func selectedArtifactContents(items []artifactItem, index int) (string, error) {
	path := selectedArtifactPath(items, index)
	if path == "" {
		return "", fmt.Errorf("no artifact selected")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

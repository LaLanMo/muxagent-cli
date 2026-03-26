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
)

const artifactPreviewMaxBytes = 64 * 1024

type artifactItem struct {
	Path         string
	SourceLabel  string
	Label        string
	PreviewName  string
	PreviewTitle string
	Preview      string
	Previewable  bool
	Truncated    bool
	Markdown     bool

	renderedWidth   int
	renderedPreview string
}

func artifactPanePaths(current *taskdomain.TaskView, input *taskruntime.InputRequest) []string {
	seen := map[string]struct{}{}
	paths := make([]string, 0)
	appendPaths := func(items []string) {
		for _, item := range items {
			item = strings.TrimSpace(item)
			if item == "" {
				continue
			}
			if _, ok := seen[item]; ok {
				continue
			}
			seen[item] = struct{}{}
			paths = append(paths, item)
		}
	}
	if input != nil {
		appendPaths(input.ArtifactPaths)
	}
	if current != nil {
		appendPaths(current.ArtifactPaths)
	}
	return paths
}

func buildArtifactItems(workDir string, current *taskdomain.TaskView, input *taskruntime.InputRequest) []artifactItem {
	paths := artifactPanePaths(current, input)
	provenance := artifactProvenance(current)
	items := make([]artifactItem, 0, len(paths))
	for _, path := range paths {
		item := loadArtifactItem(path, workDir)
		if source, ok := provenance[path]; ok {
			item.SourceLabel = source
			item.Label = source + " · " + item.Label
			item.PreviewTitle = source + " · " + item.PreviewName
		}
		items = append(items, item)
	}
	return items
}

func loadArtifactItem(path, workDir string) artifactItem {
	item := artifactItem{
		Path:         path,
		Label:        shortenPath(path, workDir),
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

func defaultArtifactIndex(items []artifactItem, screen Screen, input *taskruntime.InputRequest) int {
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

func artifactProvenance(current *taskdomain.TaskView) map[string]string {
	if current == nil {
		return nil
	}
	provenance := make(map[string]string, len(current.ArtifactPaths))
	ordinals := map[string]int{}
	for _, run := range current.NodeRuns {
		ordinals[run.NodeName]++
		label := fmt.Sprintf("%s (#%d)", run.NodeName, ordinals[run.NodeName])
		for _, path := range run.ArtifactPaths {
			if path == "" {
				continue
			}
			if _, exists := provenance[path]; exists {
				continue
			}
			provenance[path] = label
		}
	}
	return provenance
}

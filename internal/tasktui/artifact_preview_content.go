package tasktui

import (
	"strings"

	"github.com/charmbracelet/glamour"
)

func (i *artifactItem) renderedContent(width int) string {
	if i == nil {
		return ""
	}
	if i.Preview == "" {
		return tuiTheme.artifactEmpty.Render("No preview available.")
	}
	if !i.Markdown {
		return tuiTheme.artifactPreviewText.Render(i.Preview)
	}
	if i.renderedPreview != "" && i.renderedWidth == width {
		return i.renderedPreview
	}
	rendered, err := renderMarkdownPreview(i.Preview, width)
	if err != nil {
		return tuiTheme.artifactPreviewText.Render(i.Preview)
	}
	i.renderedWidth = width
	i.renderedPreview = rendered
	return rendered
}

func renderMarkdownPreview(content string, width int) (string, error) {
	renderer, err := glamour.NewTermRenderer(
		glamour.WithStandardStyle("dark"),
		glamour.WithWordWrap(max(20, width)),
		glamour.WithPreservedNewLines(),
	)
	if err != nil {
		return "", err
	}
	rendered, err := renderer.Render(content)
	if err != nil {
		return "", err
	}
	return strings.TrimRight(rendered, "\n"), nil
}

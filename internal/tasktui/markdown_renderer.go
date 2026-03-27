package tasktui

import (
	"strings"

	glamour "charm.land/glamour/v2"
)

const (
	artifactMarkdownMinWidth = 20
)

func artifactMarkdownWidth(width int) int {
	return max(artifactMarkdownMinWidth, width)
}

func artifactMarkdownRenderer(width int) (*glamour.TermRenderer, error) {
	return glamour.NewTermRenderer(
		glamour.WithStyles(tuiTheme.Markdown.Artifact),
		glamour.WithWordWrap(artifactMarkdownWidth(width)),
	)
}

func renderArtifactMarkdown(content string, width int) (string, error) {
	renderer, err := artifactMarkdownRenderer(width)
	if err != nil {
		return "", err
	}
	rendered, err := renderer.Render(content)
	if err != nil {
		return "", err
	}
	return strings.TrimRight(rendered, "\n"), nil
}

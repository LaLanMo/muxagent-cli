package tasktui

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
	renderWidth := artifactMarkdownWidth(width)
	if i.renderedPreview != "" && i.renderedWidth == renderWidth {
		return i.renderedPreview
	}
	rendered, err := renderArtifactMarkdown(i.Preview, renderWidth)
	if err != nil {
		return tuiTheme.artifactPreviewText.Render(i.Preview)
	}
	i.renderedWidth = renderWidth
	i.renderedPreview = rendered
	return rendered
}

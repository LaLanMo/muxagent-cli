package tasktui

import (
	mdansi "charm.land/glamour/v2/ansi"
)

type markdownTheme struct {
	Artifact mdansi.StyleConfig
}

func buildMarkdownTheme() markdownTheme {
	body := "#ECE7DF"
	strong := "#FFF8EE"
	muted := "#BEB7AF"
	subtle := "#8A857F"
	heading := "#9DD7FF"
	accent := "#FFC107"
	link := "#93C5FD"
	codeText := "#F8FAFC"
	codeBg := "#243244"
	codeBlockBg := "#0F1724"
	h1Bg := "#243244"
	quoteText := "#DEE8F2"
	quoteBg := "#16202D"
	codeComment := "#AAB8C7"

	return markdownTheme{
		Artifact: mdansi.StyleConfig{
			Document: mdansi.StyleBlock{
				StylePrimitive: mdansi.StylePrimitive{
					Color: stringPtr(body),
				},
			},
			Paragraph: mdansi.StyleBlock{
				StylePrimitive: mdansi.StylePrimitive{
					BlockSuffix: "\n",
				},
			},
			BlockQuote: mdansi.StyleBlock{
				StylePrimitive: mdansi.StylePrimitive{
					Color:           stringPtr(quoteText),
					BackgroundColor: stringPtr(quoteBg),
				},
				Indent:      uintPtr(1),
				IndentToken: stringPtr("▎ "),
			},
			List: mdansi.StyleList{
				LevelIndent: 2,
			},
			Heading: mdansi.StyleBlock{
				StylePrimitive: mdansi.StylePrimitive{
					BlockSuffix: "\n",
					Color:       stringPtr(heading),
					Bold:        boolPtr(true),
				},
			},
			H1: mdansi.StyleBlock{
				StylePrimitive: mdansi.StylePrimitive{
					Prefix:          " ",
					Suffix:          " ",
					Color:           stringPtr(strong),
					BackgroundColor: stringPtr(h1Bg),
					Bold:            boolPtr(true),
				},
			},
			H2: mdansi.StyleBlock{
				StylePrimitive: mdansi.StylePrimitive{
					Prefix: "## ",
					Color:  stringPtr(accent),
					Bold:   boolPtr(true),
				},
			},
			H3: mdansi.StyleBlock{
				StylePrimitive: mdansi.StylePrimitive{
					Prefix: "### ",
					Color:  stringPtr(heading),
					Bold:   boolPtr(true),
				},
			},
			H4: mdansi.StyleBlock{
				StylePrimitive: mdansi.StylePrimitive{
					Prefix: "#### ",
					Color:  stringPtr(strong),
					Bold:   boolPtr(true),
				},
			},
			H5: mdansi.StyleBlock{
				StylePrimitive: mdansi.StylePrimitive{
					Prefix: "##### ",
					Color:  stringPtr(strong),
				},
			},
			H6: mdansi.StyleBlock{
				StylePrimitive: mdansi.StylePrimitive{
					Prefix: "###### ",
					Color:  stringPtr(muted),
				},
			},
			Strikethrough: mdansi.StylePrimitive{
				CrossedOut: boolPtr(true),
			},
			Emph: mdansi.StylePrimitive{
				Italic: boolPtr(true),
			},
			Strong: mdansi.StylePrimitive{
				Color: stringPtr(strong),
				Bold:  boolPtr(true),
			},
			HorizontalRule: mdansi.StylePrimitive{
				Color:  stringPtr(subtle),
				Format: "\n────────────\n",
			},
			Item: mdansi.StylePrimitive{
				BlockPrefix: "• ",
				Color:       stringPtr(body),
			},
			Enumeration: mdansi.StylePrimitive{
				BlockPrefix: ". ",
				Color:       stringPtr(body),
			},
			Task: mdansi.StyleTask{
				StylePrimitive: mdansi.StylePrimitive{
					Color: stringPtr(body),
				},
				Ticked:   "[✓] ",
				Unticked: "[ ] ",
			},
			Link: mdansi.StylePrimitive{
				Color:     stringPtr(link),
				Underline: boolPtr(true),
			},
			LinkText: mdansi.StylePrimitive{
				Color: stringPtr(link),
				Bold:  boolPtr(true),
			},
			Image: mdansi.StylePrimitive{
				Color:     stringPtr(link),
				Underline: boolPtr(true),
			},
			ImageText: mdansi.StylePrimitive{
				Color:  stringPtr(muted),
				Format: "Image: {{.text}} →",
			},
			Code: mdansi.StyleBlock{
				StylePrimitive: mdansi.StylePrimitive{
					Prefix:          " ",
					Suffix:          " ",
					Color:           stringPtr(codeText),
					BackgroundColor: stringPtr(codeBg),
					Bold:            boolPtr(true),
				},
			},
			CodeBlock: mdansi.StyleCodeBlock{
				StyleBlock: mdansi.StyleBlock{
					StylePrimitive: mdansi.StylePrimitive{
						Color:           stringPtr(codeText),
						BackgroundColor: stringPtr(codeBlockBg),
					},
					Margin: uintPtr(2),
				},
				Chroma: &mdansi.Chroma{
					Text: mdansi.StylePrimitive{
						Color: stringPtr(codeText),
					},
					Comment: mdansi.StylePrimitive{
						Color: stringPtr(codeComment),
					},
					CommentPreproc: mdansi.StylePrimitive{
						Color: stringPtr("#F59E0B"),
					},
					Keyword: mdansi.StylePrimitive{
						Color: stringPtr(accent),
					},
					KeywordReserved: mdansi.StylePrimitive{
						Color: stringPtr("#C4B5FD"),
					},
					KeywordNamespace: mdansi.StylePrimitive{
						Color: stringPtr("#C4B5FD"),
					},
					KeywordType: mdansi.StylePrimitive{
						Color: stringPtr("#86EFAC"),
					},
					Operator: mdansi.StylePrimitive{
						Color: stringPtr("#FCA5A5"),
					},
					Punctuation: mdansi.StylePrimitive{
						Color: stringPtr("#CBD5E1"),
					},
					Name: mdansi.StylePrimitive{
						Color: stringPtr(codeText),
					},
					NameBuiltin: mdansi.StylePrimitive{
						Color: stringPtr("#FCA5A5"),
					},
					NameFunction: mdansi.StylePrimitive{
						Color: stringPtr(link),
					},
					NameTag: mdansi.StylePrimitive{
						Color: stringPtr(accent),
					},
					NameAttribute: mdansi.StylePrimitive{
						Color: stringPtr("#86EFAC"),
					},
					NameClass: mdansi.StylePrimitive{
						Color:     stringPtr("#F8FAFC"),
						Underline: boolPtr(true),
						Bold:      boolPtr(true),
					},
					LiteralNumber: mdansi.StylePrimitive{
						Color: stringPtr(accent),
					},
					LiteralString: mdansi.StylePrimitive{
						Color: stringPtr("#86EFAC"),
					},
					LiteralStringEscape: mdansi.StylePrimitive{
						Color: stringPtr("#FDE68A"),
					},
					GenericDeleted: mdansi.StylePrimitive{
						Color: stringPtr("#FCA5A5"),
					},
					GenericEmph: mdansi.StylePrimitive{
						Italic: boolPtr(true),
					},
					GenericInserted: mdansi.StylePrimitive{
						Color: stringPtr("#86EFAC"),
					},
					GenericStrong: mdansi.StylePrimitive{
						Bold: boolPtr(true),
					},
					GenericSubheading: mdansi.StylePrimitive{
						Color: stringPtr(muted),
					},
					Background: mdansi.StylePrimitive{
						BackgroundColor: stringPtr(codeBlockBg),
					},
				},
			},
			Table: mdansi.StyleTable{
				StyleBlock: mdansi.StyleBlock{
					StylePrimitive: mdansi.StylePrimitive{
						Color: stringPtr(body),
					},
				},
				CenterSeparator: stringPtr("┼"),
				ColumnSeparator: stringPtr("│"),
				RowSeparator:    stringPtr("─"),
			},
			DefinitionDescription: mdansi.StylePrimitive{
				BlockPrefix: "\n ",
				Color:       stringPtr(muted),
			},
		},
	}
}

func stringPtr(value string) *string {
	return &value
}

func boolPtr(value bool) *bool {
	return &value
}

func uintPtr(value uint) *uint {
	return &value
}

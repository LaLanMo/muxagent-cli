package tasktui

import (
	"image/color"

	"charm.land/lipgloss/v2"
)

var tuiTheme = newTheme()

type headerTheme struct {
	Brand      lipgloss.Style
	Version    lipgloss.Style
	TaskLabel  lipgloss.Style
	Eyebrow    lipgloss.Style
	Hero       lipgloss.Style
	HeroAccent lipgloss.Style
	Field      lipgloss.Style
	MetaLabel  lipgloss.Style
	MetaValue  lipgloss.Style
	MetaStrong lipgloss.Style
}

type textTheme struct {
	Body      lipgloss.Style
	HalfMuted lipgloss.Style
	Muted     lipgloss.Style
	Subtle    lipgloss.Style
	Empty     lipgloss.Style
}

type statusTheme struct {
	Running  lipgloss.Style
	Done     lipgloss.Style
	Failed   lipgloss.Style
	Awaiting lipgloss.Style
	Success  lipgloss.Style
}

type footerTheme struct {
	Hint   lipgloss.Style
	Strong lipgloss.Style
}

type formTheme struct {
	InputFocused  lipgloss.Style
	InputBlurred  lipgloss.Style
	InputLabel    lipgloss.Style
	InputLabelHot lipgloss.Style
	InputCaption  lipgloss.Style
	OptionActive  lipgloss.Style
	OptionMuted   lipgloss.Style
}

type panelTheme struct {
	Base    lipgloss.Style
	Warning lipgloss.Style
	Danger  lipgloss.Style
	Title   lipgloss.Style
	Body    lipgloss.Style
}

type artifactTheme struct {
	Pane         lipgloss.Style
	Header       lipgloss.Style
	Hint         lipgloss.Style
	Divider      lipgloss.Style
	Block        lipgloss.Style
	BlockTitle   lipgloss.Style
	FileActive   lipgloss.Style
	FileInactive lipgloss.Style
	PreviewText  lipgloss.Style
	Empty        lipgloss.Style
}

type dialogTheme struct {
	Scrim        lipgloss.Style
	Card         lipgloss.Style
	Border       lipgloss.Style
	Surface      lipgloss.Style
	Title        lipgloss.Style
	Body         lipgloss.Style
	Hint         lipgloss.Style
	Row          lipgloss.Style
	Button       lipgloss.Style
	ButtonActive lipgloss.Style
	ButtonDanger lipgloss.Style
}

type theme struct {
	bg                   color.Color
	panelBg              color.Color
	artifactPaneBg       color.Color
	artifactBlock        color.Color
	inputBgBlurred       color.Color
	inputBgFocused       color.Color
	borderMuted          color.Color
	text                 color.Color
	halfMuted            color.Color
	muted                color.Color
	subtle               color.Color
	running              color.Color
	done                 color.Color
	failed               color.Color
	awaiting             color.Color
	awaitingRowBg        color.Color
	successBg            color.Color
	streamBg             color.Color
	streamBorder         color.Color
	artifactPane         lipgloss.Style
	artifactHeader       lipgloss.Style
	artifactHint         lipgloss.Style
	artifactDivider      lipgloss.Style
	artifactBlockStyle   lipgloss.Style
	artifactBlockTitle   lipgloss.Style
	artifactFileActive   lipgloss.Style
	artifactFileInactive lipgloss.Style
	artifactPreviewText  lipgloss.Style
	artifactEmpty        lipgloss.Style
	canvas               lipgloss.Style
	brand                lipgloss.Style
	version              lipgloss.Style
	taskLabel            lipgloss.Style
	body                 lipgloss.Style
	halfMutedText        lipgloss.Style
	mutedText            lipgloss.Style
	subtleText           lipgloss.Style
	runningText          lipgloss.Style
	doneText             lipgloss.Style
	failedText           lipgloss.Style
	awaitingText         lipgloss.Style
	lineMuted            lipgloss.Style
	divider              lipgloss.Style
	emptyState           lipgloss.Style
	footerHint           lipgloss.Style
	footerStrong         lipgloss.Style
	successLine          lipgloss.Style
	modal                lipgloss.Style
	modalTitle           lipgloss.Style
	modalSubtitle        lipgloss.Style
	inputChrome          lipgloss.Style
	panel                lipgloss.Style
	panelWarning         lipgloss.Style
	panelDanger          lipgloss.Style
	panelTitle           lipgloss.Style
	panelBody            lipgloss.Style
	streamPanel          lipgloss.Style
	streamThread         lipgloss.Style
	streamJSON           lipgloss.Style
	optionActive         lipgloss.Style
	optionInactive       lipgloss.Style
	Header               headerTheme
	Text                 textTheme
	Status               statusTheme
	Footer               footerTheme
	Form                 formTheme
	Panel                panelTheme
	Artifact             artifactTheme
	Dialog               dialogTheme
	Markdown             markdownTheme
}

func newTheme() theme {
	bgHex := "#090909"
	panelBgHex := "#1A1A1A"
	artifactPaneBgHex := "#151D2A"
	artifactBlockHex := "#0B111B"
	inputBgBlurredHex := "#121212"
	inputBgFocusedHex := "#141311"
	borderMutedHex := "#303030"
	textHex := "#ECE7DF"
	halfMutedHex := "#BEB7AF"
	mutedHex := "#8A857F"
	subtleHex := "#5F5A54"
	runningHex := "#D77757"
	doneHex := "#4EBA65"
	failedHex := "#FF6B80"
	awaitingHex := "#FFC107"
	awaitingRowBgHex := "#2A2000"
	streamBgHex := "#1A1A1A"
	streamBorderHex := "#343C4C"

	bg := lipgloss.Color(bgHex)
	panelBg := lipgloss.Color(panelBgHex)
	artifactPaneBg := lipgloss.Color(artifactPaneBgHex)
	artifactBlock := lipgloss.Color(artifactBlockHex)
	inputBgBlurred := lipgloss.Color(inputBgBlurredHex)
	inputBgFocused := lipgloss.Color(inputBgFocusedHex)
	borderMuted := lipgloss.Color(borderMutedHex)
	text := lipgloss.Color(textHex)
	halfMuted := lipgloss.Color(halfMutedHex)
	muted := lipgloss.Color(mutedHex)
	subtle := lipgloss.Color(subtleHex)
	running := lipgloss.Color(runningHex)
	done := lipgloss.Color(doneHex)
	failed := lipgloss.Color(failedHex)
	awaiting := lipgloss.Color(awaitingHex)
	awaitingRowBg := lipgloss.Color(awaitingRowBgHex)
	streamBg := lipgloss.Color(streamBgHex)
	streamBorder := lipgloss.Color(streamBorderHex)
	artifactPane := lipgloss.NewStyle().
		Background(artifactPaneBg).
		Padding(0, 1)
	artifactHeader := lipgloss.NewStyle().
		Foreground(text).
		Bold(true)
	artifactHint := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#94A3B8"))
	artifactDivider := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#314155"))
	artifactBlockStyle := lipgloss.NewStyle().
		Background(artifactBlock).
		Padding(0, 1)
	artifactBlockTitle := lipgloss.NewStyle().
		Foreground(text).
		Bold(true)
	artifactFileActive := lipgloss.NewStyle().
		Foreground(awaiting).
		Bold(true)
	artifactFileInactive := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#CBD5E1"))
	artifactPreviewText := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#E2E8F0"))
	artifactEmpty := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#94A3B8"))
	bodyText := lipgloss.NewStyle().Foreground(text)
	halfMutedText := lipgloss.NewStyle().Foreground(halfMuted)
	mutedText := lipgloss.NewStyle().Foreground(muted)
	subtleText := lipgloss.NewStyle().Foreground(subtle)
	runningText := lipgloss.NewStyle().Foreground(running)
	doneText := lipgloss.NewStyle().Foreground(done)
	failedText := lipgloss.NewStyle().Foreground(failed)
	awaitingText := lipgloss.NewStyle().Foreground(awaiting)
	footerHint := lipgloss.NewStyle().Foreground(muted)
	footerStrong := lipgloss.NewStyle().Foreground(muted)
	inputChromeBlurred := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#5A554F")).
		Background(inputBgBlurred).
		Padding(0, 1)
	inputChromeFocused := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(awaiting).
		Background(inputBgFocused).
		Padding(0, 1)
	panelBase := lipgloss.NewStyle().
		Background(panelBg).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderMuted).
		Padding(1, 2)
	panelWarning := lipgloss.NewStyle().
		Background(panelBg).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(awaiting).
		Padding(1, 2)
	panelDanger := lipgloss.NewStyle().
		Background(panelBg).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(failed).
		Padding(1, 2)
	panelTitle := lipgloss.NewStyle().
		Foreground(text).
		Bold(true)
	panelBody := lipgloss.NewStyle().
		Foreground(muted)
	optionActive := lipgloss.NewStyle().
		Bold(true).
		Foreground(awaiting)
	optionInactive := lipgloss.NewStyle().
		Foreground(muted)
	header := headerTheme{
		Brand: lipgloss.NewStyle().
			Foreground(text).
			Bold(true),
		Version: lipgloss.NewStyle().
			Foreground(halfMuted),
		TaskLabel: lipgloss.NewStyle().
			Foreground(text).
			Bold(true),
		Eyebrow: lipgloss.NewStyle().
			Foreground(awaiting).
			Bold(true),
		Hero: lipgloss.NewStyle().
			Foreground(text).
			Bold(true),
		HeroAccent: lipgloss.NewStyle().
			Foreground(awaiting).
			Bold(true),
		Field: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#8D7A1D")),
		MetaLabel: lipgloss.NewStyle().
			Foreground(muted),
		MetaValue: lipgloss.NewStyle().
			Foreground(halfMuted),
		MetaStrong: lipgloss.NewStyle().
			Foreground(text).
			Bold(true),
	}
	textStyles := textTheme{
		Body:      bodyText,
		HalfMuted: halfMutedText,
		Muted:     mutedText,
		Subtle:    subtleText,
		Empty:     lipgloss.NewStyle().Foreground(muted),
	}
	statusStyles := statusTheme{
		Running:  runningText,
		Done:     doneText,
		Failed:   failedText,
		Awaiting: awaitingText,
		Success: lipgloss.NewStyle().
			Foreground(done).
			Bold(true),
	}
	footerStyles := footerTheme{
		Hint:   footerHint,
		Strong: footerStrong,
	}
	formStyles := formTheme{
		InputFocused: inputChromeFocused,
		InputBlurred: inputChromeBlurred,
		InputLabel: lipgloss.NewStyle().
			Foreground(halfMuted).
			Bold(true),
		InputLabelHot: lipgloss.NewStyle().
			Foreground(awaiting).
			Bold(true),
		InputCaption: lipgloss.NewStyle().
			Foreground(subtle),
		OptionActive: optionActive,
		OptionMuted:  optionInactive,
	}
	panelStyles := panelTheme{
		Base:    panelBase,
		Warning: panelWarning,
		Danger:  panelDanger,
		Title:   panelTitle,
		Body:    panelBody,
	}
	artifactStyles := artifactTheme{
		Pane:         artifactPane,
		Header:       artifactHeader,
		Hint:         artifactHint,
		Divider:      artifactDivider,
		Block:        artifactBlockStyle,
		BlockTitle:   artifactBlockTitle,
		FileActive:   artifactFileActive,
		FileInactive: artifactFileInactive,
		PreviewText:  artifactPreviewText,
		Empty:        artifactEmpty,
	}
	dialogButton := lipgloss.NewStyle().
		Background(panelBg).
		Foreground(subtle)
	dialogStyles := dialogTheme{
		Scrim: lipgloss.NewStyle().
			Background(bg),
		Card: lipgloss.NewStyle().
			Background(panelBg).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(awaiting).
			Padding(1, 2),
		Border: lipgloss.NewStyle().
			Background(panelBg).
			Foreground(awaiting),
		Surface: lipgloss.NewStyle().
			Background(panelBg),
		Title: lipgloss.NewStyle().
			Background(panelBg).
			Foreground(text).
			Bold(true),
		Body: lipgloss.NewStyle().
			Background(panelBg).
			Foreground(muted),
		Hint: lipgloss.NewStyle().
			Background(panelBg).
			Foreground(subtle),
		Row: lipgloss.NewStyle().
			Background(panelBg),
		Button: dialogButton,
		ButtonActive: dialogButton.Copy().
			Foreground(text).
			Bold(true),
		ButtonDanger: dialogButton.Copy().
			Foreground(failed).
			Bold(true),
	}
	markdownStyles := buildMarkdownTheme()

	return theme{
		bg:                   bg,
		panelBg:              panelBg,
		artifactPaneBg:       artifactPaneBg,
		artifactBlock:        artifactBlock,
		inputBgBlurred:       inputBgBlurred,
		inputBgFocused:       inputBgFocused,
		borderMuted:          borderMuted,
		text:                 text,
		halfMuted:            halfMuted,
		muted:                muted,
		subtle:               subtle,
		running:              running,
		done:                 done,
		failed:               failed,
		awaiting:             awaiting,
		awaitingRowBg:        awaitingRowBg,
		successBg:            lipgloss.Color("#102113"),
		streamBg:             streamBg,
		streamBorder:         streamBorder,
		artifactPane:         artifactStyles.Pane,
		artifactHeader:       artifactStyles.Header,
		artifactHint:         artifactStyles.Hint,
		artifactDivider:      artifactStyles.Divider,
		artifactBlockStyle:   artifactStyles.Block,
		artifactBlockTitle:   artifactStyles.BlockTitle,
		artifactFileActive:   artifactStyles.FileActive,
		artifactFileInactive: artifactStyles.FileInactive,
		artifactPreviewText:  artifactStyles.PreviewText,
		artifactEmpty:        artifactStyles.Empty,
		canvas: lipgloss.NewStyle().
			Foreground(text).
			Background(bg).
			Padding(1, 2),
		brand:         header.Brand,
		version:       header.Version,
		taskLabel:     header.TaskLabel,
		body:          textStyles.Body,
		halfMutedText: textStyles.HalfMuted,
		mutedText:     textStyles.Muted,
		subtleText:    textStyles.Subtle,
		runningText:   statusStyles.Running,
		doneText:      statusStyles.Done,
		failedText:    statusStyles.Failed,
		awaitingText:  statusStyles.Awaiting,
		lineMuted: lipgloss.NewStyle().
			Foreground(subtle),
		divider:      lipgloss.NewStyle().Foreground(borderMuted),
		emptyState:   textStyles.Empty,
		footerHint:   footerStyles.Hint,
		footerStrong: footerStyles.Strong,
		successLine:  statusStyles.Success,
		modal: lipgloss.NewStyle().
			Background(panelBg).
			Padding(1, 2).
			Width(58),
		modalTitle: lipgloss.NewStyle().
			Foreground(text).
			Bold(true),
		modalSubtitle: lipgloss.NewStyle().
			Foreground(muted),
		inputChrome:  formStyles.InputBlurred,
		panel:        panelStyles.Base,
		panelWarning: panelStyles.Warning,
		panelDanger:  panelStyles.Danger,
		panelTitle:   panelStyles.Title,
		panelBody:    panelStyles.Body,
		streamPanel: func() lipgloss.Style {
			border := lipgloss.RoundedBorder()
			border.Left = "▌"
			return lipgloss.NewStyle().
				Background(streamBg).
				BorderStyle(border).
				BorderTopForeground(streamBorder).
				BorderRightForeground(streamBorder).
				BorderBottomForeground(streamBorder).
				BorderLeftForeground(running).
				Padding(0, 1)
		}(),
		streamThread: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#A4AFBF")).
			Background(streamBg),
		streamJSON: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#D7DFEA")).
			Background(streamBg),
		optionActive:   optionActive,
		optionInactive: optionInactive,
		Header:         header,
		Text:           textStyles,
		Status:         statusStyles,
		Footer:         footerStyles,
		Form:           formStyles,
		Panel:          panelStyles,
		Artifact:       artifactStyles,
		Dialog:         dialogStyles,
		Markdown:       markdownStyles,
	}
}

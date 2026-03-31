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

type taskListTheme struct {
	Title      lipgloss.Style
	Secondary  lipgloss.Style
	SelectedBg color.Color
	AwaitingBg color.Color
	ActionBg   color.Color
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

type appTheme struct {
	Canvas  lipgloss.Style
	Divider lipgloss.Style
}

type modalTheme struct {
	Frame    lipgloss.Style
	Title    lipgloss.Style
	Subtitle lipgloss.Style
}

type streamTheme struct {
	Panel  lipgloss.Style
	Thread lipgloss.Style
	Event  lipgloss.Style
}

type surfaceTheme struct {
	Canvas        color.Color
	Panel         color.Color
	ArtifactPane  color.Color
	ArtifactBlock color.Color
	InputBlurred  color.Color
	InputFocused  color.Color
	Stream        color.Color
	Success       color.Color
}

type colorTheme struct {
	BorderMuted  color.Color
	Text         color.Color
	HalfMuted    color.Color
	Muted        color.Color
	Subtle       color.Color
	Running      color.Color
	Done         color.Color
	Failed       color.Color
	Awaiting     color.Color
	StreamBorder color.Color
}

type theme struct {
	Surface  surfaceTheme
	Color    colorTheme
	App      appTheme
	Modal    modalTheme
	Stream   streamTheme
	Header   headerTheme
	Text     textTheme
	Status   statusTheme
	Footer   footerTheme
	TaskList taskListTheme
	Form     formTheme
	Panel    panelTheme
	Artifact artifactTheme
	Dialog   dialogTheme
	Markdown markdownTheme
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
	taskListSelectedBgHex := "#141414"
	taskListAwaitingBgHex := "#17130C"
	taskListActionBgHex := "#101010"
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
	taskListSelectedBg := lipgloss.Color(taskListSelectedBgHex)
	taskListAwaitingBg := lipgloss.Color(taskListAwaitingBgHex)
	taskListActionBg := lipgloss.Color(taskListActionBgHex)
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
	taskListStyles := taskListTheme{
		Title: lipgloss.NewStyle().
			Foreground(text).
			Bold(true),
		Secondary: lipgloss.NewStyle().
			Foreground(halfMuted),
		SelectedBg: taskListSelectedBg,
		AwaitingBg: taskListAwaitingBg,
		ActionBg:   taskListActionBg,
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
	surfaces := surfaceTheme{
		Canvas:        bg,
		Panel:         panelBg,
		ArtifactPane:  artifactPaneBg,
		ArtifactBlock: artifactBlock,
		InputBlurred:  inputBgBlurred,
		InputFocused:  inputBgFocused,
		Stream:        streamBg,
		Success:       lipgloss.Color("#102113"),
	}
	colors := colorTheme{
		BorderMuted:  borderMuted,
		Text:         text,
		HalfMuted:    halfMuted,
		Muted:        muted,
		Subtle:       subtle,
		Running:      running,
		Done:         done,
		Failed:       failed,
		Awaiting:     awaiting,
		StreamBorder: streamBorder,
	}
	appStyles := appTheme{
		Canvas: lipgloss.NewStyle().
			Foreground(colors.Text).
			Background(surfaces.Canvas).
			Padding(1, 2),
		Divider: lipgloss.NewStyle().
			Foreground(colors.BorderMuted),
	}
	modalStyles := modalTheme{
		Frame: lipgloss.NewStyle().
			Background(surfaces.Panel).
			Padding(1, 2).
			Width(58),
		Title: lipgloss.NewStyle().
			Foreground(text).
			Bold(true),
		Subtitle: lipgloss.NewStyle().
			Foreground(muted),
	}
	streamStyles := streamTheme{
		Panel: func() lipgloss.Style {
			border := lipgloss.RoundedBorder()
			border.Left = "▌"
			return lipgloss.NewStyle().
				Background(surfaces.Stream).
				BorderStyle(border).
				BorderTopForeground(colors.StreamBorder).
				BorderRightForeground(colors.StreamBorder).
				BorderBottomForeground(colors.StreamBorder).
				BorderLeftForeground(colors.Running).
				Padding(0, 1)
		}(),
		Thread: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#A4AFBF")).
			Background(surfaces.Stream),
		Event: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#D7DFEA")).
			Background(surfaces.Stream),
	}

	return theme{
		Surface:  surfaces,
		Color:    colors,
		App:      appStyles,
		Modal:    modalStyles,
		Stream:   streamStyles,
		Header:   header,
		Text:     textStyles,
		Status:   statusStyles,
		Footer:   footerStyles,
		TaskList: taskListStyles,
		Form:     formStyles,
		Panel:    panelStyles,
		Artifact: artifactStyles,
		Dialog:   dialogStyles,
		Markdown: markdownStyles,
	}
}

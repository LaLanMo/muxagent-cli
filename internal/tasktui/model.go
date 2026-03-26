package tasktui

import (
	"image/color"

	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"

	"github.com/LaLanMo/muxagent-cli/internal/taskconfig"
	"github.com/LaLanMo/muxagent-cli/internal/taskdomain"
	"github.com/LaLanMo/muxagent-cli/internal/taskruntime"
)

type Screen int

const (
	ScreenTaskList Screen = iota
	ScreenNewTask
	ScreenRunning
	ScreenApproval
	ScreenClarification
	ScreenFailed
	ScreenComplete
)

type tasksLoadedMsg struct {
	tasks        []taskdomain.TaskView
	err          error
	eventVersion uint64
}

type taskOpenedMsg struct {
	view  taskdomain.TaskView
	cfg   *taskconfig.Config
	input *taskruntime.InputRequest
	err   error
}

type taskListAction int

const (
	taskListActionNone taskListAction = iota
	taskListActionNewTask
)

type Model struct {
	service        RuntimeService
	workDir        string
	configOverride string
	version        string

	screen              Screen
	returnScreen        Screen
	activeTaskID        string
	tasks               []taskdomain.TaskView
	taskEventVersion    uint64
	current             *taskdomain.TaskView
	currentConfig       *taskconfig.Config
	launchConfig        *taskconfig.Config
	currentInput        *taskruntime.InputRequest
	startupText         string
	progressByRun       map[string][]string
	sessionByRun        map[string]string
	artifactItems       []artifactItem
	artifactIndex       int
	artifactPreviewPath string
	autoScrollDetail    bool
	errorText           string
	pendingRuntimeCmd   *pendingRuntimeCommand
	approval            approvalState
	clarification       clarificationState
	submittingInput     bool
	focusRegion         FocusRegion
	dialog              dialogModel
	failure             failureState
	activeDetailTab     DetailTab

	width  int
	height int

	keys            appKeyMap
	taskList        list.Model
	editor          EditorController
	detailViewport  viewport.Model
	artifactPreview viewport.Model
}

func NewModel(service RuntimeService, workDir, configOverride string, launchConfig *taskconfig.Config, version string) Model {
	model := Model{
		service:        service,
		workDir:        workDir,
		configOverride: configOverride,
		version:        version,
		screen:         ScreenTaskList,
		returnScreen:   ScreenTaskList,
		keys:           newAppKeyMap(),
		taskList:       newTaskListModel(),
		editor: newEditorController(EditorSpec{
			Placeholder: "Describe your task...",
			CharLimit:   512,
			Rows:        6,
		}),
		detailViewport:   newDetailViewport(),
		artifactPreview:  newArtifactPreviewViewport(),
		launchConfig:     launchConfig,
		progressByRun:    map[string][]string{},
		sessionByRun:     map[string]string{},
		autoScrollDetail: true,
	}
	model.syncComponents()
	return model
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(m.loadTasksCmd(), m.waitForEvent())
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tasksLoadedMsg:
		if msg.eventVersion < m.taskEventVersion {
			return m, nil
		}
		if msg.err != nil {
			m.errorText = msg.err.Error()
			m.syncComponents()
			return m, nil
		}
		m.tasks = msg.tasks
		m.syncComponents()
		return m, nil
	case taskOpenedMsg:
		if msg.err != nil {
			m.errorText = msg.err.Error()
			m.syncComponents()
			return m, nil
		}
		m.activateTask(msg.view, msg.cfg, msg.input)
		m.autoScrollDetail = true
		m.syncComponents()
		return m, m.syncInputFocus()
	case taskruntime.RunEvent:
		m.handleEvent(msg)
		m.syncComponents()
		return m, tea.Batch(m.waitForEvent(), m.syncInputFocus())
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.syncComponents()
		return m, nil
	case tea.KeyPressMsg:
		return m.handleKey(msg)
	default:
		return m.forwardToActiveInput(msg)
	}
	return m, nil
}

func (m Model) View() tea.View {
	view := tea.NewView(m.renderScreen())
	view.AltScreen = true
	view.WindowTitle = "muxagent"
	view.BackgroundColor = color.Black
	view.Cursor = m.editorCursor()
	return view
}

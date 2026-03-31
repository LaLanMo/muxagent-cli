package tasktui

import (
	"strings"

	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"

	"github.com/LaLanMo/muxagent-cli/internal/taskconfig"
	"github.com/LaLanMo/muxagent-cli/internal/taskdomain"
	"github.com/LaLanMo/muxagent-cli/internal/taskexecutor"
	"github.com/LaLanMo/muxagent-cli/internal/taskruntime"
)

type Screen int

const (
	ScreenTaskList Screen = iota
	ScreenTaskConfigs
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

type taskConfigCatalogLoadedMsg struct {
	catalog            *taskconfig.Catalog
	entries            []taskConfigSummary
	selectedAlias      string
	taskSelectionAlias string
	err                error
}

type taskListAction int

const (
	taskListActionNone taskListAction = iota
	taskListActionNewTask
	taskListActionManageConfigs
)

type Model struct {
	service                  RuntimeService
	workDir                  string
	configCatalog            *taskconfig.Catalog
	selectedConfigAlias      string
	worktreeLaunchAvailable  bool
	rememberedUseWorktree    bool
	saveTaskLaunchPreference func(bool) error
	version                  string

	screen              Screen
	returnScreen        Screen
	activeTaskID        string
	tasks               []taskdomain.TaskView
	taskEventVersion    uint64
	current             *taskdomain.TaskView
	currentConfig       *taskconfig.Config
	currentInput        *taskruntime.InputRequest
	startupText         string
	progressByRun       map[string][]string
	streamByRun         map[string][]taskexecutor.StreamEvent
	sessionByRun        map[string]string
	artifactItems       []artifactItem
	artifactIndex       int
	artifactPreviewPath string
	artifactErrorText   string
	autoScrollDetail    bool
	errorText           string
	pendingRuntimeCmd   *pendingRuntimeCommand
	approval            approvalState
	clarification       clarificationState
	newTask             newTaskState
	submittingInput     bool
	focusRegion         FocusRegion
	dialog              dialogModel
	failure             failureState
	activeDetailTab     DetailTab

	width  int
	height int

	keys            appKeyMap
	taskList        list.Model
	configList      list.Model
	editor          EditorController
	detailViewport  viewport.Model
	artifactPreview viewport.Model
	taskConfigs     taskConfigManagerState
}

func NewModel(service RuntimeService, workDir, configPath string, launchConfig *taskconfig.Config, version string) Model {
	catalog := configCatalogOrDefault(nil)
	if launchConfig != nil {
		catalog = &taskconfig.Catalog{
			DefaultAlias: taskconfig.DefaultAlias,
			Entries: []taskconfig.CatalogEntry{{
				Alias:  taskconfig.DefaultAlias,
				Path:   configPath,
				Config: launchConfig,
			}},
		}
	}
	return newModel(service, workDir, catalog, version)
}

func NewModelWithCatalog(service RuntimeService, workDir string, configCatalog *taskconfig.Catalog, version string) Model {
	return newModel(service, workDir, configCatalog, version)
}

func newModel(service RuntimeService, workDir string, configCatalog *taskconfig.Catalog, version string) Model {
	catalog := configCatalogOrDefault(configCatalog)
	model := Model{
		service:             service,
		workDir:             workDir,
		configCatalog:       catalog,
		selectedConfigAlias: catalog.DefaultAlias,
		version:             version,
		screen:              ScreenTaskList,
		returnScreen:        ScreenTaskList,
		keys:                newAppKeyMap(),
		taskList:            newTaskListModel(),
		configList:          newTaskConfigListModel(),
		editor: newEditorController(EditorSpec{
			Placeholder: "Describe your task...",
			Rows:        6,
		}),
		detailViewport:   newDetailViewport(),
		artifactPreview:  newArtifactPreviewViewport(),
		progressByRun:    map[string][]string{},
		streamByRun:      map[string][]taskexecutor.StreamEvent{},
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
	case taskConfigCatalogLoadedMsg:
		if msg.err != nil {
			m.taskConfigs.pending = false
			m.taskConfigs.errorText = msg.err.Error()
			m.syncComponents()
			return m, nil
		}
		m.taskConfigs.pending = false
		m.taskConfigs.errorText = ""
		m.configCatalog = configCatalogOrDefault(msg.catalog)
		m.taskConfigs.entries = append([]taskConfigSummary(nil), msg.entries...)
		m.taskConfigs.selectedAlias = msg.selectedAlias
		desiredAlias := strings.TrimSpace(msg.taskSelectionAlias)
		if desiredAlias == "" {
			desiredAlias = firstNonEmpty(m.selectedConfigAlias, m.configCatalog.DefaultAlias)
		}
		if !m.taskConfigIsLaunchable(desiredAlias) {
			desiredAlias = m.firstLaunchableTaskConfigAlias(desiredAlias)
		}
		if desiredAlias == "" {
			desiredAlias = m.configCatalog.DefaultAlias
		}
		m.selectedConfigAlias = desiredAlias
		m.syncComponents()
		return m, nil
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
	view.BackgroundColor = tuiTheme.Surface.Canvas
	view.Cursor = m.editorCursor()
	return view
}

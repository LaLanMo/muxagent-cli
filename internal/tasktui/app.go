package tasktui

import (
	"context"
	"fmt"
	"image/color"
	"path/filepath"
	"strings"
	"time"

	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/LaLanMo/muxagent-cli/internal/taskconfig"
	"github.com/LaLanMo/muxagent-cli/internal/taskdomain"
	"github.com/LaLanMo/muxagent-cli/internal/taskruntime"
)

type RuntimeService interface {
	Run(ctx context.Context) error
	Events() <-chan taskruntime.RunEvent
	Dispatch(cmd taskruntime.RunCommand)
	ListTaskViews(ctx context.Context, workDir string) ([]taskdomain.TaskView, error)
	LoadTaskView(ctx context.Context, taskID string) (taskdomain.TaskView, *taskconfig.Config, error)
	BuildInputRequest(ctx context.Context, taskID, nodeRunID string) (*taskruntime.InputRequest, error)
	Close() error
}

type App struct {
	Service        RuntimeService
	WorkDir        string
	ConfigOverride string
	LaunchConfig   *taskconfig.Config
	Version        string
}

func (a App) Run(ctx context.Context) error {
	runtimeCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		_ = a.Service.Run(runtimeCtx)
	}()

	model := NewModel(a.Service, a.WorkDir, a.ConfigOverride, a.LaunchConfig, a.Version)
	_, err := tea.NewProgram(model, tea.WithContext(ctx)).Run()
	if err != nil {
		return err
	}
	return nil
}

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
	tasks []taskdomain.TaskView
	err   error
}

type taskOpenedMsg struct {
	view  taskdomain.TaskView
	cfg   *taskconfig.Config
	input *taskruntime.InputRequest
	err   error
}

type Model struct {
	service        RuntimeService
	workDir        string
	configOverride string
	version        string

	screen              Screen
	returnScreen        Screen
	tasks               []taskdomain.TaskView
	current             *taskdomain.TaskView
	currentConfig       *taskconfig.Config
	launchConfig        *taskconfig.Config
	currentInput        *taskruntime.InputRequest
	startupText         string
	progressByRun       map[string][]string
	sessionByRun        map[string]string
	artifactItems       []artifactItem
	artifactIndex       int
	artifactCollapsed   bool
	artifactPreviewPath string
	autoScrollDetail    bool
	errorText           string
	pendingCreate       bool
	approvalChoice      int

	clarificationQuestion int
	clarificationOption   int
	clarificationAnswers  []taskdomain.ClarificationAnswer
	clarificationOther    bool

	width  int
	height int

	keys            appKeyMap
	taskList        list.Model
	newTaskInput    textarea.Model
	detailInput     textarea.Model
	detailViewport  viewport.Model
	artifactPreview viewport.Model
}

func NewModel(service RuntimeService, workDir, configOverride string, launchConfig *taskconfig.Config, version string) Model {
	model := Model{
		service:          service,
		workDir:          workDir,
		configOverride:   configOverride,
		version:          version,
		screen:           ScreenTaskList,
		returnScreen:     ScreenTaskList,
		keys:             newAppKeyMap(),
		taskList:         newTaskListModel(),
		newTaskInput:     newTaskTextArea(),
		detailInput:      newDetailTextArea(),
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
		m.current = &msg.view
		m.currentConfig = msg.cfg
		m.currentInput = msg.input
		m.applyViewScreen(msg.view, msg.cfg, msg.input)
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
	}
	return m, nil
}

func (m Model) View() tea.View {
	view := tea.NewView(m.renderScreen())
	view.AltScreen = true
	view.WindowTitle = "muxagent"
	view.BackgroundColor = color.Black
	return view
}

func (m Model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case keyMatches(msg, m.keys.quit):
		if m.service != nil {
			m.service.Dispatch(taskruntime.RunCommand{Type: taskruntime.CommandShutdown})
		}
		return m, tea.Quit
	case keyMatches(msg, m.keys.newTask):
		cmd := m.openNewTask()
		m.syncComponents()
		return m, cmd
	}

	if m.screen != ScreenTaskList && m.screen != ScreenNewTask {
		if cmd, handled := m.handleArtifactPaneKey(msg); handled {
			m.syncComponents()
			return m, cmd
		}
	}

	switch m.screen {
	case ScreenTaskList:
		return m.handleTaskListKey(msg)
	case ScreenNewTask:
		return m.handleNewTaskKey(msg)
	case ScreenApproval:
		return m.handleApprovalKey(msg)
	case ScreenClarification:
		return m.handleClarificationKey(msg)
	default:
		return m.handleDetailKey(msg)
	}
}

func (m *Model) handleArtifactPaneKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	switch {
	case keyMatches(msg, m.keys.toggleArtifacts):
		if m.current == nil {
			return nil, false
		}
		m.artifactCollapsed = !m.artifactCollapsed
		return nil, true
	case m.artifactCollapsed || len(m.artifactItems) == 0:
		return nil, false
	case keyMatches(msg, m.keys.left):
		if m.artifactIndex > 0 {
			m.artifactIndex--
		}
		return nil, true
	case keyMatches(msg, m.keys.right):
		if m.artifactIndex < len(m.artifactItems)-1 {
			m.artifactIndex++
		}
		return nil, true
	case keyMatches(msg, m.keys.previewUp), keyMatches(msg, m.keys.previewDown):
		nextPreview, cmd := m.artifactPreview.Update(msg)
		m.artifactPreview = nextPreview
		return cmd, true
	default:
		return nil, false
	}
}

func (m Model) handleTaskListKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if keyMatches(msg, m.keys.open) {
		item, ok := m.taskList.SelectedItem().(taskListItem)
		if !ok {
			return m, nil
		}
		return m, m.openTaskCmd(item.view.Task.ID)
	}

	nextList, cmd := m.taskList.Update(msg)
	m.taskList = nextList
	m.syncComponents()
	return m, cmd
}

func (m Model) handleNewTaskKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case keyMatches(msg, m.keys.back):
		m.closeNewTask()
		m.syncComponents()
		return m, m.syncInputFocus()
	case keyMatches(msg, m.keys.confirm):
		desc := strings.TrimSpace(m.newTaskInput.Value())
		if desc == "" {
			return m, nil
		}
		m.pendingCreate = true
		m.startupText = "Starting task…"
		m.errorText = ""
		m.current = &taskdomain.TaskView{
			Task: taskdomain.Task{
				Description: desc,
				WorkDir:     m.workDir,
			},
			Status: taskdomain.TaskStatusRunning,
		}
		m.currentConfig = m.launchConfig
		m.currentInput = nil
		m.screen = ScreenRunning
		m.syncComponents()
		return m, m.dispatchCmd(taskruntime.RunCommand{
			Type:        taskruntime.CommandStartTask,
			Description: desc,
			WorkDir:     m.workDir,
			ConfigPath:  m.configOverride,
		})
	default:
		// Pre-grow: ensure textarea has room for a new line before processing
		// the keystroke, so the internal viewport doesn't scroll away line 0.
		textareaPreGrow(&m.newTaskInput, msg)
		var cmd tea.Cmd
		m.newTaskInput, cmd = m.newTaskInput.Update(msg)
		textareaSyncHeight(&m.newTaskInput)
		m.syncComponents()
		return m, cmd
	}
}

// textareaPreGrow ensures the textarea has room for a new line before the
// keystroke is processed, preventing the internal viewport from scrolling
// away line 0 when inserting a newline at height=1.
func textareaPreGrow(ta *textarea.Model, msg tea.KeyPressMsg) {
	if msg.String() == "ctrl+j" {
		need := ta.LineCount() + 1
		h := clamp(need, 1, 10)
		if h > ta.Height() {
			ta.SetHeight(h)
		}
	}
}

// textareaSyncHeight adjusts the textarea's visible height to match its line
// count (capped at 10), and fixes stale viewport offsets after line deletion.
func textareaSyncHeight(ta *textarea.Model) {
	lines := ta.LineCount()
	h := clamp(lines, 1, 10)

	// Fix stale viewport offset: when lines decreased but the height cap
	// didn't change (e.g. 12→11 lines, both clamp to 10), the internal
	// viewport may still be scrolled past the content. Detect this and
	// force-reset by saving the value/cursor, calling SetValue (which
	// resets the viewport via GotoTop), then restoring cursor position.
	if offset := ta.ScrollYOffset(); offset > 0 && lines <= h {
		row, col := ta.Line(), ta.Column()
		val := ta.Value()
		ta.SetValue(val)
		ta.MoveToBegin()
		for range row {
			ta.CursorDown()
		}
		ta.SetCursorColumn(col)
	}

	if h != ta.Height() {
		ta.SetHeight(h)
	}
}

func (m Model) handleApprovalKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case keyMatches(msg, m.keys.back):
		m.screen = ScreenTaskList
		m.currentInput = nil
		m.syncComponents()
		return m, tea.Batch(m.loadTasksCmd(), m.syncInputFocus())
	case keyMatches(msg, m.keys.up):
		m.approvalChoice = 0
		if m.detailInput.Value() == "" {
			m.detailInput.Placeholder = "Type feedback..."
		}
		m.syncComponents()
		return m, m.syncInputFocus()
	case keyMatches(msg, m.keys.down):
		m.approvalChoice = 1
		m.detailInput.Placeholder = "Explain what needs to change…"
		m.syncComponents()
		return m, m.syncInputFocus()
	case keyMatches(msg, m.keys.confirm):
		if m.currentInput == nil || m.current == nil {
			return m, nil
		}
		payload := map[string]interface{}{
			"approved": m.approvalChoice == 0,
		}
		if m.approvalChoice == 1 {
			feedback := strings.TrimSpace(m.detailInput.Value())
			if feedback != "" {
				payload["feedback"] = feedback
			}
		}
		m.currentInput = nil
		m.screen = ScreenRunning
		m.syncComponents()
		return m, tea.Batch(
			m.syncInputFocus(),
			m.dispatchCmd(taskruntime.RunCommand{
				Type:      taskruntime.CommandSubmitInput,
				TaskID:    m.current.Task.ID,
				NodeRunID: m.currentAwaitingRunID(),
				Payload:   payload,
			}),
		)
	default:
		if m.approvalChoice != 1 {
			return m, nil
		}
		textareaPreGrow(&m.detailInput, msg)
		var cmd tea.Cmd
		m.detailInput, cmd = m.detailInput.Update(msg)
		textareaSyncHeight(&m.detailInput)
		m.syncComponents()
		return m, cmd
	}
}

func (m Model) handleClarificationKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.currentInput == nil || len(m.currentInput.Questions) == 0 {
		return m, nil
	}
	question := m.currentInput.Questions[m.clarificationQuestion]
	optionCount := len(question.Options) + 1
	switch {
	case keyMatches(msg, m.keys.back):
		if m.clarificationOther {
			m.clarificationOther = false
			m.detailInput.Reset()
			m.detailInput.SetHeight(1)
			m.syncComponents()
			return m, m.syncInputFocus()
		}
		m.screen = ScreenTaskList
		m.currentInput = nil
		m.syncComponents()
		return m, tea.Batch(m.loadTasksCmd(), m.syncInputFocus())
	case keyMatches(msg, m.keys.up):
		if !m.clarificationOther && m.clarificationOption > 0 {
			m.clarificationOption--
			m.syncComponents()
		}
		return m, m.syncInputFocus()
	case keyMatches(msg, m.keys.down):
		if !m.clarificationOther && m.clarificationOption < optionCount-1 {
			m.clarificationOption++
			m.syncComponents()
		}
		return m, m.syncInputFocus()
	case keyMatches(msg, m.keys.confirm):
		if m.clarificationOther {
			answer := strings.TrimSpace(m.detailInput.Value())
			if answer == "" {
				return m, nil
			}
			m.clarificationAnswers = appendOrReplaceAnswer(m.clarificationAnswers, m.clarificationQuestion, answer)
			return m.advanceClarificationOrSubmit()
		}
		if m.clarificationOption == len(question.Options) {
			m.clarificationOther = true
			m.detailInput.Reset()
			m.detailInput.SetHeight(1)
			m.detailInput.Placeholder = "Write your own answer…"
			m.syncComponents()
			return m, m.syncInputFocus()
		}
		answer := question.Options[m.clarificationOption].Label
		m.clarificationAnswers = appendOrReplaceAnswer(m.clarificationAnswers, m.clarificationQuestion, answer)
		return m.advanceClarificationOrSubmit()
	default:
		if !m.clarificationOther {
			return m, nil
		}
		textareaPreGrow(&m.detailInput, msg)
		var cmd tea.Cmd
		m.detailInput, cmd = m.detailInput.Update(msg)
		textareaSyncHeight(&m.detailInput)
		m.syncComponents()
		return m, cmd
	}
}

func (m Model) handleDetailKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.screen == ScreenFailed {
		switch {
		case keyMatches(msg, m.keys.retry):
			return m.triggerRetry(false)
		case keyMatches(msg, m.keys.forceRetry):
			return m.triggerRetry(true)
		}
	}
	if keyMatches(msg, m.keys.back) {
		m.screen = ScreenTaskList
		m.current = nil
		m.currentConfig = nil
		m.currentInput = nil
		m.startupText = ""
		m.errorText = ""
		m.syncComponents()
		return m, m.loadTasksCmd()
	}

	nextViewport, cmd := m.detailViewport.Update(msg)
	m.detailViewport = nextViewport
	return m, cmd
}

func (m Model) triggerRetry(force bool) (tea.Model, tea.Cmd) {
	retryability := m.currentRetryability()
	if retryability == nil || m.current == nil {
		return m, nil
	}
	if !force && !retryability.RetryAllowed {
		return m, nil
	}
	m.currentInput = nil
	m.screen = ScreenRunning
	m.startupText = "Retrying " + retryability.Run.NodeName + "…"
	m.errorText = ""
	m.artifactCollapsed = false
	m.autoScrollDetail = true
	m.syncComponents()
	return m, m.dispatchCmd(taskruntime.RunCommand{
		Type:      taskruntime.CommandRetryNode,
		TaskID:    m.current.Task.ID,
		NodeRunID: retryability.Run.ID,
		Force:     force,
	})
}

func (m *Model) handleEvent(event taskruntime.RunEvent) {
	if event.TaskView != nil {
		m.hydrateRunSessionIDs(*event.TaskView)
		m.upsertTask(*event.TaskView)
		if m.current == nil || m.current.Task.ID == event.TaskID {
			view := *event.TaskView
			m.current = &view
			if m.currentConfig == nil {
				m.currentConfig = m.launchConfig
			}
		}
		if m.pendingCreate && event.Type == taskruntime.EventTaskCreated {
			view := *event.TaskView
			m.current = &view
			m.currentConfig = m.launchConfig
			m.pendingCreate = false
			m.screen = ScreenRunning
		}
	}
	if event.Progress != nil {
		m.applyProgressEvent(event)
	}
	if event.Error != nil {
		m.errorText = event.Error.Message
	}
	if event.InputRequest != nil {
		m.currentInput = event.InputRequest
		m.resetInputState()
		m.autoScrollDetail = true
		switch event.InputRequest.Kind {
		case taskruntime.InputKindHumanNode:
			m.screen = ScreenApproval
		case taskruntime.InputKindClarification:
			m.screen = ScreenClarification
		}
		return
	}
	switch event.Type {
	case taskruntime.EventNodeStarted:
		m.startupText = ""
		m.screen = ScreenRunning
		m.artifactCollapsed = false
		m.autoScrollDetail = true
	case taskruntime.EventNodeCompleted:
		m.clearRunProgress(event.NodeRunID)
		m.startupText = ""
		m.screen = ScreenRunning
		m.artifactCollapsed = false
		m.autoScrollDetail = true
	case taskruntime.EventNodeProgress:
		m.startupText = ""
		m.screen = ScreenRunning
		m.artifactCollapsed = false
		m.autoScrollDetail = true
	case taskruntime.EventTaskCompleted:
		m.clearTaskProgress(event.TaskView)
		m.startupText = ""
		m.screen = ScreenComplete
		m.artifactCollapsed = true
		m.autoScrollDetail = true
	case taskruntime.EventTaskFailed:
		m.clearTaskProgress(event.TaskView)
		m.startupText = ""
		m.screen = ScreenFailed
		m.artifactCollapsed = false
		m.autoScrollDetail = true
	}
}

func (m *Model) applyProgressEvent(event taskruntime.RunEvent) {
	if event.Progress == nil || event.NodeRunID == "" {
		return
	}
	if event.Progress.SessionID != "" {
		m.sessionByRun[event.NodeRunID] = event.Progress.SessionID
	}
	if event.Progress.Message == "" {
		return
	}
	messages := append([]string(nil), m.progressByRun[event.NodeRunID]...)
	messages = appendProgressMessage(messages, event.Progress.Message)
	m.progressByRun[event.NodeRunID] = messages
}

func (m *Model) hydrateRunSessionIDs(view taskdomain.TaskView) {
	for _, run := range view.NodeRuns {
		if run.SessionID != "" {
			m.sessionByRun[run.ID] = run.SessionID
		}
		if run.Status != taskdomain.NodeRunRunning {
			delete(m.progressByRun, run.ID)
		}
	}
}

func (m *Model) clearRunProgress(nodeRunID string) {
	if nodeRunID == "" {
		return
	}
	delete(m.progressByRun, nodeRunID)
}

func (m *Model) clearTaskProgress(view *taskdomain.TaskView) {
	if view == nil {
		return
	}
	for _, run := range view.NodeRuns {
		delete(m.progressByRun, run.ID)
	}
}

func appendProgressMessage(messages []string, raw string) []string {
	for _, item := range strings.Split(raw, "\n") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if len(messages) > 0 && messages[len(messages)-1] == item {
			continue
		}
		messages = append(messages, item)
	}
	if len(messages) > 4 {
		messages = append([]string(nil), messages[len(messages)-4:]...)
	}
	return messages
}

func (m *Model) resetInputState() {
	m.approvalChoice = 0
	m.clarificationQuestion = 0
	m.clarificationOption = 0
	m.clarificationAnswers = nil
	m.clarificationOther = false
	m.detailInput.Reset()
	m.detailInput.SetHeight(1)
	m.detailInput.Placeholder = "Type feedback..."
}

func (m *Model) applyViewScreen(view taskdomain.TaskView, cfg *taskconfig.Config, input *taskruntime.InputRequest) {
	m.currentConfig = cfg
	switch {
	case input != nil && input.Kind == taskruntime.InputKindHumanNode:
		m.screen = ScreenApproval
		m.artifactCollapsed = false
	case input != nil && input.Kind == taskruntime.InputKindClarification:
		m.screen = ScreenClarification
		m.artifactCollapsed = false
	case view.Status == taskdomain.TaskStatusFailed:
		m.screen = ScreenFailed
		m.artifactCollapsed = false
	case view.Status == taskdomain.TaskStatusDone:
		m.screen = ScreenComplete
		m.artifactCollapsed = true
	default:
		m.screen = ScreenRunning
		m.artifactCollapsed = false
	}
	m.resetInputState()
}

func (m *Model) upsertTask(view taskdomain.TaskView) {
	for i := range m.tasks {
		if m.tasks[i].Task.ID == view.Task.ID {
			m.tasks[i] = view
			return
		}
	}
	m.tasks = append([]taskdomain.TaskView{view}, m.tasks...)
}

func (m Model) currentAwaitingRunID() string {
	if m.current == nil {
		return ""
	}
	for i := len(m.current.NodeRuns) - 1; i >= 0; i-- {
		run := m.current.NodeRuns[i]
		if run.Status == taskdomain.NodeRunAwaitingUser {
			return run.ID
		}
	}
	if m.currentInput != nil {
		return m.currentInput.NodeRunID
	}
	return ""
}

func (m Model) currentRetryability() *taskdomain.Retryability {
	if m.current == nil || m.currentConfig == nil {
		return nil
	}
	return taskdomain.RetryabilityForTask(m.currentConfig, currentTaskRuns(*m.current))
}

func currentTaskRuns(view taskdomain.TaskView) []taskdomain.NodeRun {
	runs := make([]taskdomain.NodeRun, 0, len(view.NodeRuns))
	for _, run := range view.NodeRuns {
		runs = append(runs, run.NodeRun)
	}
	return runs
}

func (m Model) waitForEvent() tea.Cmd {
	return func() tea.Msg {
		event, ok := <-m.service.Events()
		if !ok {
			return nil
		}
		return event
	}
}

func (m Model) loadTasksCmd() tea.Cmd {
	return func() tea.Msg {
		tasks, err := m.service.ListTaskViews(context.Background(), m.workDir)
		return tasksLoadedMsg{tasks: tasks, err: err}
	}
}

func (m Model) openTaskCmd(taskID string) tea.Cmd {
	return func() tea.Msg {
		view, cfg, err := m.service.LoadTaskView(context.Background(), taskID)
		if err != nil {
			return taskOpenedMsg{err: err}
		}
		var input *taskruntime.InputRequest
		if view.Status == taskdomain.TaskStatusAwaitingUser {
			nodeRunID := latestAwaitingRunID(view)
			input, err = m.service.BuildInputRequest(context.Background(), taskID, nodeRunID)
			if err != nil {
				return taskOpenedMsg{err: err}
			}
		}
		return taskOpenedMsg{view: view, cfg: cfg, input: input}
	}
}

func latestAwaitingRunID(view taskdomain.TaskView) string {
	for i := len(view.NodeRuns) - 1; i >= 0; i-- {
		if view.NodeRuns[i].Status == taskdomain.NodeRunAwaitingUser {
			return view.NodeRuns[i].ID
		}
	}
	return ""
}

func (m Model) dispatchCmd(command taskruntime.RunCommand) tea.Cmd {
	return func() tea.Msg {
		m.service.Dispatch(command)
		return nil
	}
}

func (m *Model) openNewTask() tea.Cmd {
	if m.screen != ScreenNewTask {
		m.returnScreen = m.screen
	}
	m.screen = ScreenNewTask
	m.newTaskInput.Reset()
	m.newTaskInput.SetHeight(1)
	m.syncComponents()
	return m.syncInputFocus()
}

func (m *Model) closeNewTask() {
	m.newTaskInput.SetValue("")
	m.screen = m.returnScreen
	if m.screen == ScreenNewTask {
		m.screen = ScreenTaskList
	}
}

func (m *Model) syncInputFocus() tea.Cmd {
	var cmds []tea.Cmd
	if m.screen == ScreenNewTask {
		cmds = append(cmds, m.newTaskInput.Focus())
	} else {
		m.newTaskInput.Blur()
	}
	detailFocus := m.screen == ScreenApproval && m.approvalChoice == 1
	detailFocus = detailFocus || (m.screen == ScreenClarification && m.clarificationOther)
	if detailFocus {
		cmds = append(cmds, m.detailInput.Focus())
	} else {
		m.detailInput.Blur()
	}
	return tea.Batch(cmds...)
}

func (m *Model) syncComponents() {
	m.syncTaskList()
	m.syncArtifactPane()
	m.syncInputWidths()
	m.syncDetailViewport()
}

func (m *Model) syncTaskList() {
	selectedID := ""
	if selected, ok := m.taskList.SelectedItem().(taskListItem); ok {
		selectedID = selected.view.Task.ID
	}
	items := make([]list.Item, 0, len(m.tasks))
	for _, view := range m.tasks {
		items = append(items, taskListItem{view: view})
	}
	cmd := m.taskList.SetItems(items)
	if cmd != nil {
		_ = cmd()
	}
	if len(items) == 0 {
		return
	}
	if selectedID != "" {
		for i, item := range items {
			if item.(taskListItem).view.Task.ID == selectedID {
				m.taskList.Select(i)
				return
			}
		}
	}
	m.taskList.Select(clamp(m.taskList.Index(), 0, len(items)-1))
}

func (m *Model) syncArtifactPane() {
	selectedPath := selectedArtifactPath(m.artifactItems, m.artifactIndex)
	m.artifactItems = buildArtifactItems(m.workDir, m.current, m.currentInput)
	if len(m.artifactItems) == 0 {
		m.artifactIndex = 0
		m.artifactPreview.SetContent("")
		m.artifactPreview.GotoTop()
		return
	}
	if selectedPath != "" {
		for i, item := range m.artifactItems {
			if item.Path == selectedPath {
				m.artifactIndex = i
				return
			}
		}
	}
	m.artifactIndex = defaultArtifactIndex(m.artifactItems, m.screen, m.currentInput)
}

func (m *Model) syncInputWidths() {
	width, _ := m.viewportSize()
	innerWidth := max(24, width-4)
	// Modal width matches renderNewTaskModal: clamp(width-8, 40, 64) minus padding(4) minus inputChrome border(2) and padding(2)
	modalInner := clamp(innerWidth-8, 40, 64) - 4
	m.newTaskInput.SetWidth(max(18, modalInner-4))
	m.detailInput.SetWidth(clamp(innerWidth-28, 18, 48))
}

func (m *Model) syncDetailViewport() {
	width, height := m.viewportSize()
	innerWidth := max(24, width-4)
	contentWidth := detailContentWidth(innerWidth)
	innerHeight := max(12, height-2)
	collapsed := m.detailArtifactsCollapsed(innerWidth, innerHeight)
	header := m.renderDetailHeader(contentWidth)
	footer := m.renderDetailFooter(contentWidth)
	bodyHeight := max(1, innerHeight-lipgloss.Height(header)-lipgloss.Height(footer))
	leftWidth, rightWidth, gap := detailPaneWidths(contentWidth, collapsed)
	_ = rightWidth
	_ = gap
	m.detailViewport.SetWidth(leftWidth)
	m.detailViewport.SetHeight(bodyHeight)
	m.detailViewport.SetContent(m.renderDetailBody(leftWidth))
	if m.autoScrollDetail {
		m.detailViewport.GotoBottom()
		m.autoScrollDetail = false
	}
	m.syncArtifactPreview(rightWidth, bodyHeight, collapsed)
	taskListHeader := m.renderAppHeader(innerWidth)
	taskListFooter := m.renderTaskListFooter(innerWidth)
	listHeight := max(1, innerHeight-lipgloss.Height(taskListHeader)-lipgloss.Height(taskListFooter))
	m.taskList.SetSize(innerWidth, listHeight)
}

func (m Model) renderScreen() string {
	width, height := m.viewportSize()
	switch m.screen {
	case ScreenNewTask:
		return m.renderNewTaskScreen(width, height)
	case ScreenRunning, ScreenApproval, ScreenClarification, ScreenFailed, ScreenComplete:
		return m.renderDetailScreen(width, height)
	default:
		return m.renderTaskListScreen(width, height)
	}
}

func (m Model) viewportSize() (int, int) {
	width := m.width
	height := m.height
	if width <= 0 {
		width = 100
	}
	if height <= 0 {
		height = 30
	}
	return width, height
}

func (m Model) renderTaskListScreen(width, height int) string {
	innerWidth, innerHeight := innerSize(width, height)
	header := m.renderAppHeader(innerWidth)
	footer := m.renderTaskListFooter(innerWidth)
	bodyHeight := max(1, innerHeight-lipgloss.Height(header)-lipgloss.Height(footer))

	body := ""
	if len(m.tasks) == 0 {
		body = tuiTheme.emptyState.Render("No tasks in this working directory yet.")
	} else {
		body = m.taskList.View()
	}
	body = lipgloss.Place(innerWidth, bodyHeight, lipgloss.Left, lipgloss.Top, body)
	return renderCanvas(width, height, header, body, footer)
}

func (m Model) renderNewTaskScreen(width, height int) string {
	innerWidth, innerHeight := innerSize(width, height)
	header := m.renderAppHeader(innerWidth)
	footer := m.renderTaskListFooter(innerWidth)
	bodyHeight := max(1, innerHeight-lipgloss.Height(header)-lipgloss.Height(footer))
	body := lipgloss.Place(innerWidth, bodyHeight, lipgloss.Center, lipgloss.Center, m.renderNewTaskModal(innerWidth))
	return renderCanvas(width, height, header, body, footer)
}

func (m Model) renderDetailScreen(width, height int) string {
	innerWidth, innerHeight := innerSize(width, height)
	contentWidth := detailContentWidth(innerWidth)
	collapsed := m.detailArtifactsCollapsed(innerWidth, innerHeight)
	header := m.renderDetailHeader(contentWidth)
	footer := m.renderDetailFooter(contentWidth)
	bodyHeight := max(1, innerHeight-lipgloss.Height(header)-lipgloss.Height(footer))
	leftWidth, rightWidth, gap := detailPaneWidths(contentWidth, collapsed)
	leftBody := lipgloss.Place(leftWidth, bodyHeight, lipgloss.Left, lipgloss.Top, m.detailViewport.View())
	rightBody := m.renderArtifactsPane(rightWidth, bodyHeight, collapsed)
	bodyContent := lipgloss.JoinHorizontal(
		lipgloss.Top,
		leftBody,
		strings.Repeat(" ", gap),
		rightBody,
	)
	centeredHeader := lipgloss.Place(innerWidth, lipgloss.Height(header), lipgloss.Center, lipgloss.Top, header)
	centeredBody := lipgloss.Place(innerWidth, bodyHeight, lipgloss.Center, lipgloss.Top, bodyContent)
	centeredFooter := lipgloss.Place(innerWidth, lipgloss.Height(footer), lipgloss.Center, lipgloss.Top, footer)
	page := lipgloss.JoinVertical(lipgloss.Left, centeredHeader, centeredBody, centeredFooter)
	return tuiTheme.canvas.Width(width).Height(height).Render(page)
}

func (m Model) renderAppHeader(width int) string {
	brand := tuiTheme.brand.Render("muxagent")
	version := tuiTheme.version.Render(" " + normalizeVersionLabel(m.version))
	return fitLine(brand+version, width)
}

func (m Model) renderTaskListFooter(width int) string {
	left := tuiTheme.footerHint.Render("↑↓ navigate  Enter open  Ctrl+N new task")
	right := tuiTheme.footerHint.Render("Ctrl+C quit")
	return joinHorizontal(left, right, width)
}

func (m Model) renderNewTaskModal(width int) string {
	modalWidth := clamp(width-8, 40, 64)
	modalStyle := tuiTheme.modal.Width(modalWidth)
	innerWidth := modalWidth - modalStyle.GetHorizontalPadding()

	cfg := m.launchConfig
	if cfg == nil {
		cfg, _ = taskconfig.LoadDefault()
	}
	subtitle := "default config"
	if cfg != nil {
		nodeNames := make([]string, 0, len(cfg.Topology.Nodes))
		for _, node := range cfg.Topology.Nodes {
			nodeNames = append(nodeNames, node.Name)
		}
		subtitle += " · " + strings.Join(nodeNames, ", ")
	}
	if m.configOverride != "" {
		subtitle = "custom config · " + filepath.Base(m.configOverride)
	}
	// inputChrome border adds 2 chars; keep input within modal inner width
	inputWidth := max(18, innerWidth-2)
	input := tuiTheme.inputChrome.Width(inputWidth).Render(m.newTaskInput.View())
	content := lipgloss.JoinVertical(
		lipgloss.Left,
		tuiTheme.modalTitle.Render("New Task"),
		tuiTheme.modalSubtitle.Render(subtitle),
		"",
		input,
		"",
		tuiTheme.footerHint.Render("Enter submit  Ctrl+J newline  Esc cancel"),
	)
	return modalStyle.Render(content)
}

func (m Model) renderDetailHeader(width int) string {
	if m.current == nil {
		return fitLine(tuiTheme.taskLabel.Render("Task"), width)
	}
	title := tuiTheme.taskLabel.Render("Task: " + m.current.Task.Description)
	dag := m.renderDAG(width)
	divider := tuiTheme.divider.Render(strings.Repeat("─", max(8, width)))
	return lipgloss.JoinVertical(lipgloss.Left, title, dag, divider)
}

func (m Model) renderDAG(width int) string {
	cfg := m.currentConfig
	if cfg == nil {
		cfg = m.launchConfig
	}
	if cfg == nil {
		return ""
	}
	states := map[string]string{}
	for _, run := range m.current.NodeRuns {
		switch run.Status {
		case taskdomain.NodeRunDone:
			if states[run.NodeName] == "" {
				states[run.NodeName] = "done"
			}
		case taskdomain.NodeRunFailed:
			states[run.NodeName] = "failed"
		case taskdomain.NodeRunAwaitingUser:
			states[run.NodeName] = "current"
		case taskdomain.NodeRunRunning:
			states[run.NodeName] = "current"
		}
	}
	if m.current != nil && m.current.Status == taskdomain.TaskStatusDone {
		states[m.current.CurrentNodeName] = "done"
	}
	if m.current != nil && (m.current.Status == taskdomain.TaskStatusRunning || m.current.Status == taskdomain.TaskStatusAwaitingUser || m.current.Status == taskdomain.TaskStatusFailed) {
		states[m.current.CurrentNodeName] = "current"
	}

	parts := make([]string, 0, len(cfg.Topology.Nodes)*2)
	for i, node := range cfg.Topology.Nodes {
		parts = append(parts, renderDAGNode(node.Name, states[node.Name]))
		if i < len(cfg.Topology.Nodes)-1 {
			parts = append(parts, tuiTheme.lineMuted.Render(" → "))
		}
	}
	return ansi.Truncate(strings.Join(parts, ""), width, "…")
}

func renderDAGNode(name, state string) string {
	switch state {
	case "done":
		return renderNodeStatusLabel(tuiTheme.doneText, "✓", name, tuiTheme.body)
	case "failed":
		return renderNodeStatusLabel(tuiTheme.failedText, "×", name, tuiTheme.body)
	case "current":
		return renderNodeStatusLabel(tuiTheme.awaitingText, "●", name, tuiTheme.body)
	default:
		return tuiTheme.subtleText.Render("○ " + name)
	}
}

func (m Model) renderDetailBody(width int) string {
	if m.current == nil {
		lines := []string{}
		if m.startupText != "" {
			lines = append(lines, tuiTheme.runningText.Render("● "+m.startupText))
		}
		if m.errorText != "" {
			lines = append(lines, tuiTheme.failedText.Render("× "+m.errorText))
		}
		return strings.Join(lines, "\n")
	}
	if len(m.current.NodeRuns) == 0 {
		lines := []string{}
		if m.startupText != "" {
			lines = append(lines, tuiTheme.runningText.Render("● "+m.startupText))
		}
		if m.errorText != "" {
			lines = append(lines, tuiTheme.failedText.Render("× "+m.errorText))
		}
		return strings.Join(lines, "\n")
	}

	lines := make([]string, 0, len(m.current.NodeRuns)*3)
	for _, run := range m.current.NodeRuns {
		lines = append(lines, m.renderNodeRunBlock(run, width)...)
		lines = append(lines, "")
	}
	if m.screen == ScreenComplete {
		lines = append(lines, tuiTheme.successLine.Render("✓ Task completed successfully"))
	}
	if m.screen == ScreenFailed && m.errorText != "" {
		lines = append(lines, tuiTheme.failedText.Render("× "+m.errorText))
	}
	return strings.Join(trimTrailingBlank(lines), "\n")
}

func detailPaneWidths(totalWidth int, collapsed bool) (leftWidth, rightWidth, gap int) {
	gap = 2
	if collapsed {
		rightWidth = clamp(8, 8, max(8, totalWidth/6))
		leftWidth = max(20, totalWidth-gap-rightWidth)
		return leftWidth, rightWidth, gap
	}
	minLeft := 24
	minRight := 34
	rightWidth = clamp((totalWidth*2)/3, minRight, max(minRight, totalWidth-gap-minLeft))
	leftWidth = max(minLeft, totalWidth-gap-rightWidth)
	return leftWidth, rightWidth, gap
}

func detailContentWidth(innerWidth int) int {
	if innerWidth <= 80 {
		return innerWidth
	}
	if innerWidth >= 152 {
		return 152
	}
	return innerWidth
}

func (m Model) detailArtifactsCollapsed(innerWidth, innerHeight int) bool {
	if innerWidth < 110 || innerHeight < 26 {
		return true
	}
	return m.artifactCollapsed
}

func (m Model) renderNodeRunBlock(run taskdomain.NodeRunView, width int) []string {
	timeLabel := relativeTime(nodeRunTimestamp(run))
	nodeLabel := m.nodeRunLabel(run)
	switch run.Status {
	case taskdomain.NodeRunDone:
		lines := []string{renderTimelineHeadline(tuiTheme.doneText, "✓", nodeLabel, "", timeLabel)}
		if summary := summarizeNodeRun(run, m.current); summary != "" {
			lines = append(lines, tuiTheme.mutedText.Render("  ↳ "+summary))
		}
		if sessionID := m.nodeRunSessionID(run); sessionID != "" {
			lines = append(lines, tuiTheme.subtleText.Render("  ↳ thread: "+sessionID))
		}
		return lines
	case taskdomain.NodeRunFailed:
		lines := []string{renderTimelineHeadline(tuiTheme.failedText, "×", nodeLabel, "failed", timeLabel)}
		if summary := summarizeNodeRun(run, m.current); summary != "" {
			lines = append(lines, tuiTheme.mutedText.Render("  ↳ "+summary))
		}
		if sessionID := m.nodeRunSessionID(run); sessionID != "" {
			lines = append(lines, tuiTheme.subtleText.Render("  ↳ thread: "+sessionID))
		}
		return lines
	case taskdomain.NodeRunAwaitingUser:
		waitLabel := "awaiting input"
		artifactPaths := append([]string(nil), run.ArtifactPaths...)
		if m.currentInput != nil && run.ID == m.currentInput.NodeRunID {
			if m.currentInput.Kind == taskruntime.InputKindHumanNode {
				waitLabel = "awaiting approval"
			}
			if len(m.currentInput.ArtifactPaths) > 0 {
				artifactPaths = append([]string(nil), m.currentInput.ArtifactPaths...)
			}
		}
		lines := []string{renderTimelineHeadline(tuiTheme.awaitingText, "●", nodeLabel, waitLabel, "")}
		if len(artifactPaths) > 0 {
			lines = append(lines, tuiTheme.mutedText.Render("  Review artifacts in the pane →"))
		}
		if sessionID := m.nodeRunSessionID(run); sessionID != "" {
			lines = append(lines, tuiTheme.subtleText.Render("  ↳ thread: "+sessionID))
		}
		return lines
	default:
		return []string{m.renderRunningStreamPanel(run, nodeLabel, width)}
	}
}

func (m Model) renderRunningStreamPanel(run taskdomain.NodeRunView, nodeLabel string, width int) string {
	panelWidth := max(24, width)
	contentWidth := max(12, panelWidth-4)
	lines := []string{
		renderTimelineHeadline(tuiTheme.runningText, "●", nodeLabel, "running…", ""),
	}
	if sessionID := m.nodeRunSessionID(run); sessionID != "" {
		lines = append(lines, tuiTheme.streamThread.Render(ansi.Wrap("thread: "+sessionID, contentWidth, "")))
	}
	for _, line := range progressLines(m.progressByRun[run.ID], contentWidth) {
		lines = append(lines, line)
	}
	return tuiTheme.streamPanel.Width(panelWidth).Render(strings.Join(lines, "\n"))
}

func (m *Model) syncArtifactPreview(paneWidth, bodyHeight int, collapsed bool) {
	fileLines := m.renderArtifactFileLines(max(18, paneWidth-6), artifactVisibleCapacity(len(m.artifactItems)))
	_, previewBlockHeight := artifactPaneLayout(bodyHeight, collapsed, len(fileLines))
	contentWidth := max(12, paneWidth-4)
	previewHeight := max(3, previewBlockHeight-2)
	m.artifactPreview.SetWidth(contentWidth)
	m.artifactPreview.SetHeight(previewHeight)
	if len(m.artifactItems) == 0 || m.artifactIndex >= len(m.artifactItems) {
		m.artifactPreview.SetContent(tuiTheme.artifactEmpty.Render("No artifacts yet."))
		m.artifactPreviewPath = ""
		m.artifactPreview.GotoTop()
		return
	}
	item := &m.artifactItems[m.artifactIndex]
	previousPath := m.artifactPreviewPath
	content := item.Preview
	if content == "" {
		content = "No preview available."
	}
	m.artifactPreview.SetContent(item.renderedContent(contentWidth))
	m.artifactPreviewPath = item.Path
	if item.Path != previousPath {
		m.artifactPreview.GotoTop()
	}
}

func (m Model) renderArtifactsPane(width, height int, collapsed bool) string {
	if collapsed {
		return m.renderCollapsedArtifactRail(width, height)
	}
	contentWidth := max(18, width-2)
	fileLines := m.renderArtifactFileLines(max(18, contentWidth-4), artifactVisibleCapacity(len(m.artifactItems)))
	fileBlockHeight, previewBlockHeight := artifactPaneLayout(height, false, len(fileLines))
	header := joinHorizontal(
		tuiTheme.artifactHeader.Render(fmt.Sprintf("Artifacts (%d)", len(m.artifactItems))),
		tuiTheme.artifactHint.Render("Tab collapse"),
		contentWidth,
	)
	files := m.renderArtifactFilesBlock(contentWidth, fileBlockHeight, fileLines)
	preview := m.renderArtifactPreviewBlock(contentWidth, previewBlockHeight)
	content := lipgloss.JoinVertical(lipgloss.Left, header, files, preview)
	inner := lipgloss.Place(contentWidth, max(1, height), lipgloss.Left, lipgloss.Top, content)
	return tuiTheme.artifactPane.Width(width).Height(height).Render(inner)
}

func artifactPaneLayout(bodyHeight int, collapsed bool, fileLineCount int) (fileBlockHeight, previewBlockHeight int) {
	if collapsed {
		return 0, 0
	}
	innerHeight := max(10, bodyHeight)
	fileBlockHeight = clamp(fileLineCount+1, 3, 6)
	previewBlockHeight = max(8, innerHeight-fileBlockHeight-1)
	return
}

func artifactVisibleCapacity(total int) int {
	if total <= 0 {
		return 1
	}
	return min(total, 3)
}

func (m Model) renderArtifactFilesBlock(width, height int, lines []string) string {
	title := joinHorizontal(
		tuiTheme.artifactBlockTitle.Render("Files"),
		tuiTheme.artifactHint.Render("←→ pick"),
		width,
	)
	body := lipgloss.Place(width-2, max(1, height-1), lipgloss.Left, lipgloss.Top, strings.Join(lines, "\n"))
	return tuiTheme.artifactBlockStyle.Width(width).Height(height).Render(lipgloss.JoinVertical(lipgloss.Left, title, body))
}

func (m Model) renderArtifactFileLines(width, rows int) []string {
	if len(m.artifactItems) == 0 {
		return []string{tuiTheme.artifactEmpty.Render("No artifacts yet.")}
	}
	start, end := artifactVisibleWindow(len(m.artifactItems), m.artifactIndex, rows)
	lines := make([]string, 0, rows+2)
	if start > 0 {
		lines = append(lines, tuiTheme.artifactHint.Render(fmt.Sprintf("… %d earlier file(s)", start)))
	}
	for i := start; i < end; i++ {
		label := ansi.Truncate(m.artifactItems[i].Label, max(10, width-2), "…")
		if i == m.artifactIndex {
			lines = append(lines, tuiTheme.artifactFileActive.Render("> "+label))
			continue
		}
		lines = append(lines, tuiTheme.artifactFileInactive.Render("  "+label))
	}
	if end < len(m.artifactItems) {
		lines = append(lines, tuiTheme.artifactHint.Render(fmt.Sprintf("… %d more file(s)", len(m.artifactItems)-end)))
	}
	return lines
}

func artifactVisibleWindow(total, selected, rows int) (start, end int) {
	if total <= rows {
		return 0, total
	}
	start = clamp(selected-(rows/2), 0, max(0, total-rows))
	end = min(total, start+rows)
	if end-start < rows {
		start = max(0, end-rows)
	}
	return start, end
}

func (m Model) renderArtifactPreviewBlock(width, height int) string {
	title := "Preview"
	if len(m.artifactItems) > 0 && m.artifactIndex < len(m.artifactItems) {
		title = fmt.Sprintf("Preview · %s", m.artifactItems[m.artifactIndex].PreviewTitle)
	}
	header := joinHorizontal(
		tuiTheme.artifactBlockTitle.Render(title),
		tuiTheme.artifactHint.Render("Ctrl+U/D"),
		width,
	)
	contentHeight := max(3, height-2)
	body := lipgloss.Place(width-2, contentHeight, lipgloss.Left, lipgloss.Top, m.artifactPreview.View())
	return tuiTheme.artifactBlockStyle.Width(width).Height(height).Render(lipgloss.JoinVertical(lipgloss.Left, header, body))
}

func (m Model) renderCollapsedArtifactRail(width, height int) string {
	top := lipgloss.JoinVertical(
		lipgloss.Center,
		tuiTheme.artifactRailBadge.Render(fmt.Sprintf("%d", len(m.artifactItems))),
		"",
		tuiTheme.artifactRailDots.Render("·\n·\n·"),
	)
	bottom := lipgloss.JoinVertical(
		lipgloss.Center,
		tuiTheme.artifactRailHint.Render("◀"),
		tuiTheme.artifactRailHint.Render("Tab"),
	)
	gapHeight := max(1, height-lipgloss.Height(top)-lipgloss.Height(bottom)-2)
	content := lipgloss.JoinVertical(
		lipgloss.Center,
		top,
		strings.Repeat("\n", gapHeight),
		bottom,
	)
	return tuiTheme.artifactRail.Width(width).Height(height).Render(content)
}

func (m Model) nodeRunSessionID(run taskdomain.NodeRunView) string {
	if sessionID := m.sessionByRun[run.ID]; sessionID != "" {
		return sessionID
	}
	return run.SessionID
}

func (m Model) nodeRunLabel(run taskdomain.NodeRunView) string {
	if m.current == nil {
		return run.NodeName
	}
	total := 0
	ordinal := 0
	for _, candidate := range m.current.NodeRuns {
		if candidate.NodeName != run.NodeName {
			continue
		}
		total++
		if candidate.ID == run.ID {
			ordinal = total
		}
	}
	if total <= 1 || ordinal == 0 {
		return run.NodeName
	}
	return fmt.Sprintf("%s (#%d)", run.NodeName, ordinal)
}

func renderNodeStatusLabel(iconStyle lipgloss.Style, icon, label string, labelStyle lipgloss.Style) string {
	return lipgloss.JoinHorizontal(
		lipgloss.Left,
		iconStyle.Render(icon),
		labelStyle.Render(" "+label),
	)
}

func renderTimelineHeadline(iconStyle lipgloss.Style, icon, label, status, meta string) string {
	parts := []string{
		iconStyle.Render(icon),
		tuiTheme.body.Render(" " + label),
	}
	if status != "" {
		parts = append(parts, tuiTheme.mutedText.Render("  "+status))
	}
	if meta != "" {
		parts = append(parts, tuiTheme.mutedText.Render("  "+meta))
	}
	return lipgloss.JoinHorizontal(lipgloss.Left, parts...)
}

func (m Model) renderDetailFooter(width int) string {
	if m.current == nil {
		return m.renderStatsFooter(width, "", "", "Esc back")
	}
	switch m.screen {
	case ScreenApproval:
		return m.renderApprovalFooter(width)
	case ScreenClarification:
		return m.renderClarificationFooter(width)
	case ScreenComplete:
		return m.renderStatsFooter(width, taskSummaryLeft(m.current, m.currentConfig), taskSummaryRight(m.current), m.detailHint("Esc back", true))
	case ScreenFailed:
		return m.renderFailureFooter(width)
	default:
		left := fmt.Sprintf("%d runs · %d artifacts", len(m.current.NodeRuns), len(m.current.ArtifactPaths))
		right := "elapsed: " + taskElapsed(m.current)
		return m.renderStatsFooter(width, left, right, m.detailHint("Esc back", false))
	}
}

func (m Model) renderStatsFooter(width int, left, right, hintLeft string) string {
	stats := joinHorizontal(tuiTheme.footerStrong.Render(left), tuiTheme.footerStrong.Render(right), width)
	hints := joinHorizontal(tuiTheme.footerHint.Render(hintLeft), tuiTheme.footerHint.Render("Ctrl+C quit"), width)
	return lipgloss.JoinVertical(lipgloss.Left, stats, hints)
}

func (m Model) renderApprovalFooter(width int) string {
	options := []string{
		renderChoiceLine(m.approvalChoice == 0, "Yes, approve"),
		renderChoiceLine(m.approvalChoice == 1, "No, reject with feedback"),
	}
	content := []string{
		tuiTheme.panelTitle.Render("Approve this plan?"),
		"",
	}
	content = append(content, options...)
	if m.approvalChoice == 1 {
		content = append(content, "", tuiTheme.inputChrome.Render(m.detailInput.View()))
	}
	panel := tuiTheme.panelWarning.Width(clamp(width, 42, width)).Render(strings.Join(content, "\n"))
	hints := joinHorizontal(tuiTheme.footerHint.Render(m.detailHint("↑↓ select  Enter confirm  Esc back", false)), tuiTheme.footerHint.Render("Ctrl+C quit"), width)
	return lipgloss.JoinVertical(lipgloss.Left, panel, hints)
}

func (m Model) renderClarificationFooter(width int) string {
	if m.currentInput == nil || len(m.currentInput.Questions) == 0 {
		return m.renderStatsFooter(width, "", "", "Esc back")
	}
	question := m.currentInput.Questions[m.clarificationQuestion]
	content := []string{
		tuiTheme.panelTitle.Render(fmt.Sprintf("Question %d/%d", m.clarificationQuestion+1, len(m.currentInput.Questions))),
		tuiTheme.panelBody.Render(question.Question),
		tuiTheme.mutedText.Render(question.WhyItMatters),
		"",
	}
	for i, option := range question.Options {
		content = append(content, renderChoiceLine(!m.clarificationOther && i == m.clarificationOption, option.Label+" · "+option.Description))
	}
	content = append(content, renderChoiceLine(!m.clarificationOther && m.clarificationOption == len(question.Options), "Other"))
	if m.clarificationOther {
		content = append(content, "", tuiTheme.inputChrome.Render(m.detailInput.View()))
	}
	panel := tuiTheme.panelWarning.Width(clamp(width, 42, width)).Render(strings.Join(content, "\n"))
	hints := joinHorizontal(tuiTheme.footerHint.Render(m.detailHint("↑↓ select  Enter confirm  Esc back", false)), tuiTheme.footerHint.Render("Ctrl+C quit"), width)
	return lipgloss.JoinVertical(lipgloss.Left, panel, hints)
}

func (m Model) renderFailureFooter(width int) string {
	retryability := m.currentRetryability()
	body := firstNonEmpty(m.errorText, "Review the failed node output and try again.")
	if retryability != nil && !retryability.RetryAllowed {
		body += fmt.Sprintf("\n\nRetry limit reached for %s (%d/%d). Press Shift+R to force retry.", retryability.Run.NodeName, retryability.NextIteration-1, retryability.MaxIterations)
	}
	content := []string{
		tuiTheme.panelTitle.Render("Task failed"),
		"",
		tuiTheme.panelBody.Render(body),
	}
	panel := tuiTheme.panelDanger.Width(clamp(width, 42, width)).Render(strings.Join(content, "\n"))
	hints := joinHorizontal(tuiTheme.footerHint.Render(m.failureHint()), tuiTheme.footerHint.Render("Ctrl+C quit"), width)
	return lipgloss.JoinVertical(lipgloss.Left, panel, hints)
}

func (m Model) failureHint() string {
	parts := []string{"Esc back"}
	if retryability := m.currentRetryability(); retryability != nil {
		if retryability.RetryAllowed {
			parts = append(parts, "r retry step")
		} else if retryability.ForceRetryAllowed {
			parts = append(parts, "R force retry")
		}
	}
	parts = append(parts, "Ctrl+N new task")
	return strings.Join(parts, "  ")
}

func (m Model) detailHint(base string, includeNewTask bool) string {
	parts := []string{base}
	width, height := m.viewportSize()
	innerWidth, innerHeight := innerSize(width, height)
	effectiveCollapsed := m.detailArtifactsCollapsed(innerWidth, innerHeight)
	if effectiveCollapsed {
		parts = append(parts, "Tab expand artifacts")
	} else {
		parts = append(parts, "Tab collapse artifacts")
		if len(m.artifactItems) > 0 {
			parts = append(parts, "←→ files", "Ctrl+U/Ctrl+D preview")
		}
	}
	if includeNewTask {
		parts = append(parts, "Ctrl+N new task")
	}
	return strings.Join(parts, "  ")
}

func renderChoiceLine(selected bool, label string) string {
	if selected {
		return tuiTheme.optionActive.Render("> " + label)
	}
	return tuiTheme.optionInactive.Render("  " + label)
}

func renderCanvas(width, height int, header, body, footer string) string {
	contentWidth, contentHeight := innerSize(width, height)
	bodyHeight := max(1, contentHeight-lipgloss.Height(header)-lipgloss.Height(footer))
	body = lipgloss.Place(contentWidth, bodyHeight, lipgloss.Left, lipgloss.Top, body)
	page := lipgloss.JoinVertical(lipgloss.Left, header, body, footer)
	return tuiTheme.canvas.Width(width).Height(height).Render(page)
}

func innerSize(width, height int) (int, int) {
	return max(20, width-4), max(10, height-2)
}

func appendOrReplaceAnswer(answers []taskdomain.ClarificationAnswer, index int, selected interface{}) []taskdomain.ClarificationAnswer {
	for len(answers) <= index {
		answers = append(answers, taskdomain.ClarificationAnswer{})
	}
	answers[index] = taskdomain.ClarificationAnswer{Selected: selected}
	return answers
}

func (m Model) advanceClarificationOrSubmit() (tea.Model, tea.Cmd) {
	m.clarificationOther = false
	m.clarificationOption = 0
	m.detailInput.Reset()
	m.detailInput.SetHeight(1)
	if m.currentInput == nil || m.current == nil {
		return m, nil
	}
	if m.clarificationQuestion < len(m.currentInput.Questions)-1 {
		m.clarificationQuestion++
		m.syncComponents()
		return m, m.syncInputFocus()
	}
	answers := make([]interface{}, 0, len(m.clarificationAnswers))
	for _, answer := range m.clarificationAnswers {
		answers = append(answers, map[string]interface{}{"selected": answer.Selected})
	}
	m.currentInput = nil
	m.screen = ScreenRunning
	m.syncComponents()
	return m, tea.Batch(
		m.syncInputFocus(),
		m.dispatchCmd(taskruntime.RunCommand{
			Type:      taskruntime.CommandSubmitInput,
			TaskID:    m.current.Task.ID,
			NodeRunID: m.currentAwaitingRunID(),
			Payload: map[string]interface{}{
				"answers": answers,
			},
		}),
	)
}

func keyMatches(msg tea.KeyPressMsg, binding interface{ Keys() []string }) bool {
	for _, candidate := range binding.Keys() {
		if msg.String() == candidate {
			return true
		}
	}
	return false
}

func nodeRunTimestamp(run taskdomain.NodeRunView) time.Time {
	if run.CompletedAt != nil {
		return run.CompletedAt.UTC()
	}
	return run.StartedAt.UTC()
}

func summarizeNodeRun(run taskdomain.NodeRunView, current *taskdomain.TaskView) string {
	if run.Result != nil {
		if approved, ok := run.Result["approved"].(bool); ok {
			return fmt.Sprintf("approved: %t", approved)
		}
		if passed, ok := run.Result["passed"].(bool); ok {
			return fmt.Sprintf("passed: %t", passed)
		}
		if feedback, ok := run.Result["feedback"].(string); ok && feedback != "" {
			return "feedback: " + feedback
		}
	}
	if len(run.ArtifactPaths) > 0 {
		paths := make([]string, 0, len(run.ArtifactPaths))
		for _, path := range run.ArtifactPaths {
			paths = append(paths, shortenPath(path, currentWorkDir(current)))
		}
		return strings.Join(paths, ", ")
	}
	return ""
}

func currentWorkDir(current *taskdomain.TaskView) string {
	if current == nil {
		return ""
	}
	return current.Task.WorkDir
}

func progressLines(progress []string, width int) []string {
	lines := []string{}
	for _, item := range progress {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		lines = append(lines, tuiTheme.streamJSON.Render(ansi.Wrap(item, max(8, width), "")))
	}
	return lines
}

func taskSummaryLeft(view *taskdomain.TaskView, cfg *taskconfig.Config) string {
	if view == nil {
		return ""
	}
	nodeCount := len(view.NodeRuns)
	if cfg != nil {
		nodeCount = len(cfg.Topology.Nodes)
	}
	return fmt.Sprintf("%d nodes · %d runs · %d iterations", nodeCount, len(view.NodeRuns), taskIterations(view))
}

func taskSummaryRight(view *taskdomain.TaskView) string {
	if view == nil {
		return ""
	}
	return fmt.Sprintf("%s · %d artifacts", taskElapsed(view), len(view.ArtifactPaths))
}

func taskIterations(view *taskdomain.TaskView) int {
	if view == nil {
		return 0
	}
	seen := map[string]struct{}{}
	for _, run := range view.NodeRuns {
		seen[run.NodeName] = struct{}{}
	}
	return max(0, len(view.NodeRuns)-len(seen))
}

func taskElapsed(view *taskdomain.TaskView) string {
	if view == nil {
		return "0s"
	}
	end := time.Now().UTC()
	if len(view.NodeRuns) > 0 {
		last := view.NodeRuns[len(view.NodeRuns)-1]
		if last.CompletedAt != nil {
			end = last.CompletedAt.UTC()
		}
	}
	return shortDuration(end.Sub(view.Task.CreatedAt))
}

func shortenPath(path, workDir string) string {
	if path == "" {
		return ""
	}
	if workDir != "" {
		if rel, err := filepath.Rel(workDir, path); err == nil && rel != "" && !strings.HasPrefix(rel, "..") {
			path = rel
		}
	}
	path = filepath.ToSlash(path)
	parts := strings.Split(path, "/")
	if len(parts) >= 6 && parts[0] == ".muxagent" && parts[1] == "tasks" && parts[3] == "artifacts" {
		taskID := parts[2]
		if len(taskID) > 8 {
			taskID = taskID[:8]
		}
		return filepath.ToSlash(filepath.Join(".muxagent", "tasks", taskID, parts[len(parts)-1]))
	}
	home := filepath.ToSlash(filepath.Clean(filepath.Dir(workDir)))
	if home != "" {
		path = strings.TrimPrefix(path, home+"/")
	}
	return path
}

func trimTrailingBlank(lines []string) []string {
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func normalizeVersionLabel(version string) string {
	version = strings.TrimSpace(version)
	if version == "" {
		return "v0.1.0"
	}
	version = strings.TrimPrefix(version, "muxagent version ")
	version = strings.TrimPrefix(version, "version ")
	fields := strings.Fields(version)
	if len(fields) == 0 {
		return "v0.1.0"
	}
	label := fields[0]
	if strings.HasPrefix(label, "v") {
		return label
	}
	return label
}

func relativeTime(ts time.Time) string {
	if ts.IsZero() {
		return "just now"
	}
	delta := time.Since(ts)
	switch {
	case delta < time.Minute:
		return "just now"
	case delta < time.Hour:
		return fmt.Sprintf("%dm ago", int(delta.Minutes()))
	case delta < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(delta.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(delta.Hours()/24))
	}
}

func shortDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm %02ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%dh %02dm", int(d.Hours()), int(d.Minutes())%60)
}

func fitLine(line string, width int) string {
	if width <= 0 {
		return ""
	}
	line = ansi.Truncate(line, width, "")
	padding := width - ansi.StringWidth(line)
	if padding < 0 {
		padding = 0
	}
	return line + strings.Repeat(" ", padding)
}

func joinHorizontal(left, right string, width int) string {
	if width <= 0 {
		return ""
	}
	if right == "" {
		return fitLine(left, width)
	}
	right = ansi.Truncate(right, width, "")
	rightWidth := ansi.StringWidth(right)
	if rightWidth >= width {
		return fitLine(right, width)
	}
	leftWidth := width - rightWidth - 1
	if leftWidth <= 0 {
		return fitLine(right, width)
	}
	left = ansi.Truncate(left, leftWidth, "")
	spaceCount := width - ansi.StringWidth(left) - rightWidth
	if spaceCount < 1 {
		spaceCount = 1
	}
	return fitLine(left+strings.Repeat(" ", spaceCount)+right, width)
}

func clamp(value, minValue, maxValue int) int {
	if maxValue < minValue {
		maxValue = minValue
	}
	if value < minValue {
		return minValue
	}
	if value > maxValue {
		return maxValue
	}
	return value
}

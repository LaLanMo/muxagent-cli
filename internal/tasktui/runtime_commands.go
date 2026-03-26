package tasktui

import (
	"context"

	tea "charm.land/bubbletea/v2"

	"github.com/LaLanMo/muxagent-cli/internal/taskdomain"
	"github.com/LaLanMo/muxagent-cli/internal/taskruntime"
)

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
	eventVersion := m.taskEventVersion
	return func() tea.Msg {
		tasks, err := m.service.ListTaskViews(context.Background(), m.workDir)
		return tasksLoadedMsg{tasks: tasks, err: err, eventVersion: eventVersion}
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

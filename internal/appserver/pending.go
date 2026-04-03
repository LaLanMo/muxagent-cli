package appserver

import (
	"strings"

	"github.com/LaLanMo/muxagent-cli/internal/taskruntime"
)

type pendingClientCommand struct {
	workspaceID     string
	method          string
	clientCommandID string
	taskID          string
	nodeRunID       string
}

func (s *Server) enqueuePendingClientCommand(workspaceID, method, clientCommandID string, cmd taskruntime.RunCommand) {
	clientCommandID = strings.TrimSpace(clientCommandID)
	if clientCommandID == "" {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.pendingCommands = append(s.pendingCommands, pendingClientCommand{
		workspaceID:     workspaceID,
		method:          method,
		clientCommandID: clientCommandID,
		taskID:          strings.TrimSpace(cmd.TaskID),
		nodeRunID:       strings.TrimSpace(cmd.NodeRunID),
	})
}

func (s *Server) claimPendingClientCommandID(workspaceID string, event taskruntime.RunEvent) string {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i, pending := range s.pendingCommands {
		if pending.workspaceID != workspaceID {
			continue
		}
		if !pending.matches(event) {
			continue
		}
		clientCommandID := pending.clientCommandID
		s.pendingCommands = append(s.pendingCommands[:i], s.pendingCommands[i+1:]...)
		return clientCommandID
	}
	return ""
}

func (p pendingClientCommand) matches(event taskruntime.RunEvent) bool {
	if p.matchesDispatchCommandError(event) {
		return true
	}

	switch p.method {
	case methodTaskStart, methodTaskStartFollowUp:
		return event.Type == taskruntime.EventTaskCreated
	case methodTaskSubmitInput:
		if event.TaskID != p.taskID || event.NodeRunID != p.nodeRunID {
			return false
		}
		switch event.Type {
		case taskruntime.EventNodeStarted, taskruntime.EventNodeCompleted, taskruntime.EventInputRequested:
			return true
		default:
			return false
		}
	case methodTaskRetryNode:
		return event.TaskID == p.taskID && event.Type == taskruntime.EventNodeStarted
	case methodTaskContinueBlocked:
		if event.TaskID != p.taskID {
			return false
		}
		switch event.Type {
		case taskruntime.EventNodeStarted, taskruntime.EventNodeCompleted, taskruntime.EventInputRequested:
			return true
		default:
			return false
		}
	default:
		return false
	}
}

func (p pendingClientCommand) matchesDispatchCommandError(event taskruntime.RunEvent) bool {
	if !isDispatchCommandError(event) {
		return false
	}
	switch p.method {
	case methodTaskStart, methodTaskStartFollowUp:
		return strings.TrimSpace(event.TaskID) == ""
	default:
		return event.TaskID == p.taskID
	}
}

func isDispatchCommandError(event taskruntime.RunEvent) bool {
	return event.Type == taskruntime.EventCommandError &&
		strings.TrimSpace(event.NodeRunID) == "" &&
		strings.TrimSpace(event.NodeName) == "" &&
		event.TaskView == nil &&
		event.Progress == nil &&
		event.InputRequest == nil
}

package tasktui

import "github.com/LaLanMo/muxagent-cli/internal/taskdomain"

type approvalState struct {
	choice int
}

type clarificationState struct {
	question        int
	option          int
	headerSelection int
	answers         []taskdomain.ClarificationAnswer
	other           map[int]bool
}

type newTaskState struct {
	useWorktree bool
}

type failureState struct {
	action failureAction
}

type followUpState struct {
	choice int
}

type pendingRuntimeCommandKind int

const (
	pendingRuntimeCommandStartTask pendingRuntimeCommandKind = iota
	pendingRuntimeCommandStartFollowUp
	pendingRuntimeCommandRetry
	pendingRuntimeCommandForceRetry
	pendingRuntimeCommandContinueBlocked
)

type pendingRuntimeCommand struct {
	kind                 pendingRuntimeCommandKind
	taskID               string
	restoreScreen        Screen
	restoreFailureAction failureAction
}

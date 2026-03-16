package appwire

type MessageRole string

const (
	MessageRoleUser  MessageRole = "user"
	MessageRoleAgent MessageRole = "agent"
)

type SessionStatus string

const (
	SessionStatusIdle            SessionStatus = "idle"
	SessionStatusRunning         SessionStatus = "running"
	SessionStatusWaitingApproval SessionStatus = "waiting_approval"
	SessionStatusError           SessionStatus = "error"
	SessionStatusDone            SessionStatus = "done"
)

type CostInfo struct {
	CostAmount   float64 `json:"costAmount,omitempty"`
	CostCurrency string  `json:"costCurrency,omitempty"`
	TotalTokens  int64   `json:"totalTokens,omitempty"`
}

package appwire

import (
	"encoding/json"
	"time"

	"github.com/LaLanMo/muxagent-cli/internal/acpprotocol"
)

type RPCRequest struct {
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

type rpcResponseEnvelope struct {
	Result any    `json:"result,omitempty"`
	Error  string `json:"error,omitempty"`
}

func MarshalRPCRequest(method string, params any) ([]byte, error) {
	var rawParams json.RawMessage
	if params != nil {
		payload, err := json.Marshal(params)
		if err != nil {
			return nil, err
		}
		rawParams = payload
	}
	return json.Marshal(RPCRequest{Method: method, Params: rawParams})
}

func MarshalRPCResponse(result any, err string) ([]byte, error) {
	return json.Marshal(rpcResponseEnvelope{Result: result, Error: err})
}

type CreateSessionParams struct {
	CWD            string `json:"cwd"`
	PermissionMode string `json:"permissionMode,omitempty"`
	Runtime        string `json:"runtime"`
	UseWorktree    bool   `json:"useWorktree,omitempty"`
}

type LoadSessionParams struct {
	SessionID      string `json:"sessionId"`
	CWD            string `json:"cwd"`
	PermissionMode string `json:"permissionMode,omitempty"`
	Model          string `json:"model,omitempty"`
	Runtime        string `json:"runtime"`
}

type ResolveSessionsParams struct {
	Runtime    string   `json:"runtime,omitempty"`
	SessionIDs []string `json:"sessionIds,omitempty"`
}

type PromptContentBlock struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	MimeType string `json:"mimeType,omitempty"`
	Data     string `json:"data,omitempty"`
	URI      string `json:"uri,omitempty"`
}

type PromptParams struct {
	SessionID string               `json:"sessionId"`
	Content   []PromptContentBlock `json:"content,omitempty"`
	Text      string               `json:"text,omitempty"`
}

type CancelParams struct {
	SessionID string `json:"sessionId"`
}

type SetModeParams struct {
	SessionID      string `json:"sessionId"`
	PermissionMode string `json:"permissionMode"`
}

type SetConfigOptionParams struct {
	SessionID string `json:"sessionId"`
	ConfigID  string `json:"configId"`
	Value     string `json:"value"`
}

type ReplyPermissionParams struct {
	SessionID string `json:"sessionId"`
	RequestID string `json:"requestId"`
	OptionID  string `json:"optionId"`
}

type ResyncStatus string

const (
	ResyncStatusOK    ResyncStatus = "ok"
	ResyncStatusGap   ResyncStatus = "gap"
	ResyncStatusReset ResyncStatus = "reset"
)

type ResyncEventsParams struct {
	StreamEpoch uint64 `json:"streamEpoch,omitempty"`
	LastSeq     uint64 `json:"lastSeq,omitempty"`
}

type FsListParams struct {
	SessionID string `json:"sessionId"`
	Path      string `json:"path,omitempty"`
}

type FsSearchParams struct {
	SessionID string `json:"sessionId"`
	Query     string `json:"query"`
}

func DecodeCreateSessionParams(raw json.RawMessage) (CreateSessionParams, error) {
	return decodeRPCParams[CreateSessionParams](raw)
}

func DecodeLoadSessionParams(raw json.RawMessage) (LoadSessionParams, error) {
	return decodeRPCParams[LoadSessionParams](raw)
}

func DecodeResolveSessionsParams(raw json.RawMessage) (ResolveSessionsParams, error) {
	return decodeRPCParams[ResolveSessionsParams](raw)
}

func DecodePromptParams(raw json.RawMessage) (PromptParams, error) {
	return decodeRPCParams[PromptParams](raw)
}

func DecodeCancelParams(raw json.RawMessage) (CancelParams, error) {
	return decodeRPCParams[CancelParams](raw)
}

func DecodeSetModeParams(raw json.RawMessage) (SetModeParams, error) {
	return decodeRPCParams[SetModeParams](raw)
}

func DecodeSetConfigOptionParams(raw json.RawMessage) (SetConfigOptionParams, error) {
	return decodeRPCParams[SetConfigOptionParams](raw)
}

func DecodeReplyPermissionParams(raw json.RawMessage) (ReplyPermissionParams, error) {
	return decodeRPCParams[ReplyPermissionParams](raw)
}

func DecodeResyncEventsParams(raw json.RawMessage) (ResyncEventsParams, error) {
	return decodeRPCParams[ResyncEventsParams](raw)
}

func DecodeFsListParams(raw json.RawMessage) (FsListParams, error) {
	return decodeRPCParams[FsListParams](raw)
}

func DecodeFsSearchParams(raw json.RawMessage) (FsSearchParams, error) {
	return decodeRPCParams[FsSearchParams](raw)
}

func DecodeEchoParams(raw json.RawMessage) (map[string]any, error) {
	if len(raw) == 0 {
		return map[string]any{}, nil
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, err
	}
	if decoded == nil {
		return map[string]any{}, nil
	}
	return decoded, nil
}

func decodeRPCParams[T any](raw json.RawMessage) (T, error) {
	var decoded T
	if len(raw) == 0 {
		raw = []byte("{}")
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return decoded, err
	}
	return decoded, nil
}

type RuntimeInfo struct {
	ID            string                            `json:"id"`
	Label         string                            `json:"label"`
	Ready         bool                              `json:"ready"`
	ConfigOptions []acpprotocol.SessionConfigOption `json:"configOptions,omitempty"`
}

type RuntimeListResult struct {
	Runtimes []RuntimeInfo `json:"runtimes"`
}

type SessionCreateResultApp struct {
	Runtime string `json:"runtime"`
	CWD     string `json:"cwd"`
}

type SessionCreateResult struct {
	App SessionCreateResultApp         `json:"app"`
	ACP acpprotocol.NewSessionResponse `json:"acp"`
}

type SessionLoadResultApp struct {
	OK      bool   `json:"ok"`
	Runtime string `json:"runtime"`
}

type SessionLoadResult struct {
	App SessionLoadResultApp            `json:"app"`
	ACP acpprotocol.LoadSessionResponse `json:"acp"`
}

type ResolvedSession struct {
	SessionID string        `json:"sessionId"`
	CWD       string        `json:"cwd"`
	Title     string        `json:"title"`
	UpdatedAt time.Time     `json:"updatedAt"`
	Status    SessionStatus `json:"status"`
}

type SessionResolveResult struct {
	Sessions []ResolvedSession `json:"sessions"`
}

type ResyncEventsResult struct {
	Events             []Event      `json:"events"`
	Complete           bool         `json:"complete"`
	Seq                uint64       `json:"seq"`
	Status             ResyncStatus `json:"status"`
	StreamEpoch        uint64       `json:"streamEpoch"`
	ReplayedThroughSeq uint64       `json:"replayedThroughSeq"`
}

type AcceptedResult struct {
	Accepted bool `json:"accepted"`
}

type OKResult struct {
	OK bool `json:"ok"`
}

type PendingApprovalsResult struct {
	Approvals []ApprovalRequest `json:"approvals"`
}

type FsEntry struct {
	Name  string `json:"name"`
	Path  string `json:"path"`
	IsDir bool   `json:"isDir"`
}

type FsListResult struct {
	Entries []FsEntry `json:"entries"`
}

type FsSearchEntry struct {
	Path  string `json:"path"`
	IsDir bool   `json:"isDir"`
}

type FsSearchResult struct {
	Results []FsSearchEntry `json:"results"`
}

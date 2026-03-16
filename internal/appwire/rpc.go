package appwire

import (
	"time"

	"github.com/LaLanMo/muxagent-cli/internal/acpprotocol"
	"github.com/LaLanMo/muxagent-cli/internal/domain"
)

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
	SessionID string               `json:"sessionId"`
	CWD       string               `json:"cwd"`
	Title     string               `json:"title"`
	UpdatedAt time.Time            `json:"updatedAt"`
	Status    domain.SessionStatus `json:"status"`
}

type SessionResolveResult struct {
	Sessions []ResolvedSession `json:"sessions"`
}

type AcceptedResult struct {
	Accepted bool `json:"accepted"`
}

type OKResult struct {
	OK bool `json:"ok"`
}

type PendingApprovalsResult struct {
	Approvals []domain.ApprovalRequest `json:"approvals"`
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

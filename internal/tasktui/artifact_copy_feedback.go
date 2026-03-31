package tasktui

import (
	"time"

	tea "charm.land/bubbletea/v2"
)

const artifactCopyFeedbackDuration = time.Second

type artifactCopyFeedbackExpiredMsg struct {
	token int
}

func (m *Model) setArtifactCopyStatus(status string) tea.Cmd {
	m.artifactCopyToken++
	m.artifactCopyStatus = status
	token := m.artifactCopyToken
	return tea.Tick(artifactCopyFeedbackDuration, func(time.Time) tea.Msg {
		return artifactCopyFeedbackExpiredMsg{token: token}
	})
}

func (m *Model) clearArtifactCopyStatus() {
	m.artifactCopyStatus = ""
}

func (m Model) artifactCopyHint(defaultHint string) string {
	if m.artifactCopyStatus != "" {
		return m.artifactCopyStatus
	}
	return defaultHint
}

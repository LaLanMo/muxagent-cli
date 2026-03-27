package tasktui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/LaLanMo/muxagent-cli/internal/taskconfig"
)

func (m Model) renderTaskConfigListHeader(width int) string {
	lines := []string{
		m.renderAppHeader(width),
		fitLine(tuiTheme.taskLabel.Render("Task Configs"), width),
	}
	taskConfigDir, err := taskconfigDirDisplay()
	if err == nil {
		lines = append(lines, fitLine(renderTaskListMetaLine("store", taskConfigDir, false), width))
	}
	if selected, ok := m.selectedManagedTaskConfig(); ok {
		lines = append(lines, fitLine(m.renderSelectedTaskConfigMeta(selected), width))
	}
	if m.taskConfigs.pending {
		lines = append(lines, fitLine(tuiTheme.Status.Running.Render("Refreshing task configs…"), width))
	} else if strings.TrimSpace(m.taskConfigs.errorText) != "" {
		lines = append(lines, fitLine(tuiTheme.Status.Failed.Render("× "+m.taskConfigs.errorText), width))
	} else if strings.TrimSpace(m.taskConfigs.statusText) != "" {
		lines = append(lines, fitLine(tuiTheme.Status.Success.Render("✓ "+m.taskConfigs.statusText), width))
	}
	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

func (m Model) renderSelectedTaskConfigMeta(summary taskConfigSummary) string {
	parts := []string{
		renderTaskListMetaLine("selected", summary.Alias, true),
	}
	if summary.BundlePath != "" {
		parts = append(parts, renderTaskListMetaLine("bundle", summary.BundlePath, true))
	}
	if summary.Runtime != "" {
		parts = append(parts, renderTaskListMetaLine("runtime", summary.Runtime, true))
	}
	if summary.LoadErr != "" {
		parts = append(parts, tuiTheme.Status.Failed.Render("invalid"))
	}
	return strings.Join(parts, tuiTheme.Header.MetaLabel.Render("  •  "))
}

func (m Model) renderTaskConfigListFooter(surface surfaceRect) string {
	left := fmt.Sprintf("%d configs", len(m.taskConfigs.entries))
	right := ""
	if selected, ok := m.selectedManagedTaskConfig(); ok {
		switch {
		case selected.LoadErr != "":
			right = "invalid config"
		case len(selected.NodeNames) > 0:
			right = fmt.Sprintf("%d nodes", len(selected.NodeNames))
		}
	}
	return renderFooterWithStats(surface.Width, left, right, m.taskConfigListHint())
}

func (m Model) taskConfigListHint() string {
	if m.taskConfigs.form != nil {
		return joinHintParts("Enter save", "Esc cancel")
	}
	if m.taskConfigs.confirm != nil {
		return joinHintParts("Enter delete", "Esc cancel")
	}
	parts := []string{"↑↓ navigate"}
	if selected, ok := m.selectedManagedTaskConfig(); ok {
		switch {
		case selected.IsDefault:
			parts = append(parts, "default selected")
		case selected.LoadErr == "":
			parts = append(parts, "Enter set default")
		}
	}
	parts = append(parts, "n clone", "r rename", "x delete", "Esc back")
	return joinHintParts(parts...)
}

func (m Model) renderTaskConfigOverlay(width, height int, base string) string {
	switch {
	case m.taskConfigs.form != nil:
		return composeOverlay(base, m.renderTaskConfigFormModal(width), width, height)
	case m.taskConfigs.confirm != nil:
		return composeOverlay(base, m.renderTaskConfigConfirmModal(width), width, height)
	default:
		return ""
	}
}

func (m Model) renderTaskConfigFormModal(width int) string {
	form := m.taskConfigs.form
	if form == nil {
		return ""
	}
	modalWidth := m.taskConfigFormModalWidth(width)
	return tuiTheme.modal.Width(modalWidth).Render(m.buildTaskConfigFormPanel(modalWidth).View)
}

func (m Model) taskConfigFormModalWidth(width int) int {
	return clamp(width-8, 36, 72)
}

func (m Model) buildTaskConfigFormPanel(modalWidth int) builtPanel {
	form := m.taskConfigs.form
	if form == nil {
		return builtPanel{}
	}
	input := m.buildEditorField(editorSurfaceSpec{
		editorBindingSpec: editorBindingSpec{
			Visible:     true,
			Slot:        form.Slot,
			Placeholder: form.Placeholder,
			Label:       form.Label,
		},
		FieldWidth: modalWidth - tuiTheme.modal.GetHorizontalFrameSize(),
		Rows:       1,
	})
	lines := []string{
		tuiTheme.modalTitle.Render(form.Title),
		tuiTheme.modalSubtitle.Render("Source config " + form.SourceAlias),
		"",
		input.View,
	}
	if strings.TrimSpace(form.ErrorText) != "" {
		lines = append(lines, "", tuiTheme.Status.Failed.Render("× "+form.ErrorText))
	}
	lines = append(lines, "", renderFooterHintText(joinHintParts("Enter "+strings.ToLower(form.SubmitLabel), "Esc cancel")))
	prefixHeight := lipgloss.Height(strings.Join([]string{
		tuiTheme.modalTitle.Render(form.Title),
		tuiTheme.modalSubtitle.Render("Source config " + form.SourceAlias),
		"",
	}, "\n"))
	return builtPanel{
		View:          strings.Join(lines, "\n"),
		EditorOffsetX: tuiTheme.modal.GetPaddingLeft() + input.ContentOffsetX,
		EditorOffsetY: tuiTheme.modal.GetPaddingTop() + prefixHeight + input.ContentOffsetY,
		HasEditor:     true,
	}
}

func (m Model) renderTaskConfigConfirmModal(width int) string {
	confirm := m.taskConfigs.confirm
	if confirm == nil {
		return ""
	}
	modalWidth := clamp(width-8, 36, 72)
	lines := []string{
		tuiTheme.modalTitle.Render(confirm.Title),
		tuiTheme.modalSubtitle.Render(confirm.Body),
		"",
		renderFooterHintText(joinHintParts("Enter "+strings.ToLower(confirm.ConfirmLabel), "Esc cancel")),
	}
	return tuiTheme.modal.Width(modalWidth).Render(strings.Join(lines, "\n"))
}

func taskconfigDirDisplay() (string, error) {
	path, err := taskconfig.TaskConfigDir()
	if err != nil {
		return "", err
	}
	return prettyTaskListPath(path), nil
}

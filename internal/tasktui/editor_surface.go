package tasktui

type editorBindingSpec struct {
	Visible     bool
	Slot        string
	Placeholder string
	Label       string
	Caption     string
}

type editorSurfaceSpec struct {
	editorBindingSpec
	FieldWidth int
	Rows       int
}

func (m Model) currentEditorBindingSpec() editorBindingSpec {
	switch m.screen {
	case ScreenNewTask:
		return editorBindingSpec{
			Visible:     true,
			Slot:        editorSlotNewTask,
			Placeholder: "Describe your task...",
			Label:       "Task description",
		}
	case ScreenApproval:
		if m.approval.choice == 1 {
			return editorBindingSpec{
				Visible:     true,
				Slot:        approvalEditorSlot(m.currentInput),
				Placeholder: "Explain what needs to change…",
				Label:       "Feedback",
			}
		}
	case ScreenClarification:
		if m.currentInput != nil && len(m.currentInput.Questions) > 0 {
			return editorBindingSpec{
				Visible:     true,
				Slot:        clarificationEditorSlot(m.currentInput, m.clarification.question),
				Placeholder: "Write your own answer…",
				Label:       "Other",
			}
		}
	}
	return editorBindingSpec{}
}

func (m Model) currentEditorSurfaceSpec() editorSurfaceSpec {
	spec := editorSurfaceSpec{
		editorBindingSpec: m.currentEditorBindingSpec(),
		Rows:              1,
	}
	if !spec.Visible {
		return spec
	}

	switch m.screen {
	case ScreenNewTask:
		metrics := m.computeScreenMetrics()
		header := m.renderAppHeader(metrics.innerWidth)
		footer := renderFooterHintBar(metrics.innerWidth, m.newTaskModalHint())
		layout := m.computeNewTaskScreenLayout(header, footer)
		spec.FieldWidth = max(18, layout.modalInnerWidth)
		spec.Rows = layout.editorRows
	case ScreenApproval, ScreenClarification:
		metrics := m.computeScreenMetrics()
		contentWidth := detailContentWidth(metrics.innerWidth)
		header := m.renderDetailHeader(contentWidth)
		footer := m.renderDetailFooter(surfaceRect{Width: contentWidth})
		frame := m.computeDetailFrameLayout(contentWidth, header, footer)
		panelSurface := m.computeDetailPanelSurface(frame)
		spec.FieldWidth = max(18, panelSurface.Rect.Width-tuiTheme.Panel.Warning.GetHorizontalFrameSize())
		switch m.screen {
		case ScreenClarification:
			spec.Rows = 1
		case ScreenApproval:
			spec.Rows = 4
			if frame.bodyHeight < 18 {
				spec.Rows = 3
			}
		}
	}

	return spec
}

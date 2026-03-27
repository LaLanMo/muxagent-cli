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

func (m Model) detailEditorSurfaceSpec(surface panelSurface) editorSurfaceSpec {
	spec := editorSurfaceSpec{
		editorBindingSpec: m.currentEditorBindingSpec(),
		Rows:              1,
	}
	if !spec.Visible {
		return spec
	}

	spec.FieldWidth = max(18, surface.Rect.Width-tuiTheme.Panel.Warning.GetHorizontalFrameSize())
	switch m.screen {
	case ScreenClarification:
		spec.Rows = 1
	case ScreenApproval:
		switch {
		case surface.MaxHeight >= 12:
			spec.Rows = 4
		case surface.MaxHeight >= 10:
			spec.Rows = 3
		case surface.MaxHeight >= 8:
			spec.Rows = 2
		default:
			spec.Rows = 1
		}
	}
	return spec
}

func (m Model) currentEditorBindingSpec() editorBindingSpec {
	switch m.screen {
	case ScreenTaskConfigs:
		if form := m.taskConfigs.form; form != nil {
			return editorBindingSpec{
				Visible:     true,
				Slot:        form.Slot,
				Placeholder: form.Placeholder,
				Label:       form.Label,
			}
		}
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
	case ScreenTaskConfigs:
		metrics := m.computeScreenMetrics()
		spec.FieldWidth = clamp(metrics.innerWidth-8, 24, 64)
		spec.Rows = 1
	case ScreenNewTask:
		metrics := m.computeScreenMetrics()
		header := m.renderAppHeader(metrics.innerWidth)
		footer := renderFooterHintBar(metrics.innerWidth, m.newTaskModalHint())
		layout := m.computeNewTaskScreenLayout(header, footer)
		spec.FieldWidth = max(18, layout.modalInnerWidth)
		spec.Rows = layout.editorRows
	case ScreenApproval, ScreenClarification:
		snapshot := m.computeDetailLayoutSnapshot()
		spec = snapshot.Editor
	}

	return spec
}

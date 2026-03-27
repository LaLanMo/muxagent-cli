package tasktui

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
)

func TestOpaqueMeasuredPanelTextUsesReadableMeasure(t *testing.T) {
	rendered := strippedView(renderOpaqueMeasuredPanelText(
		140,
		lipgloss.NewStyle(),
		strings.Repeat("This panel paragraph should wrap to a readable measure. ", 8),
	))

	longest := 0
	for _, line := range strings.Split(rendered, "\n") {
		line = strings.TrimRight(line, " ")
		longest = max(longest, ansi.StringWidth(line))
	}

	assert.Greater(t, longest, 0)
	assert.LessOrEqual(t, longest, detailBodyMeasureWidth(140))
}

package tasktui

import "testing"

import "github.com/stretchr/testify/assert"

func TestMoveSelectionClampsAtBounds(t *testing.T) {
	assert.Equal(t, 0, moveSelection(0, -1, 3))
	assert.Equal(t, 1, moveSelection(0, 1, 3))
	assert.Equal(t, 2, moveSelection(2, 1, 3))
	assert.Equal(t, 0, moveSelection(0, 5, 0))
}

func TestSelectionWindowCentersActiveItemWhenPossible(t *testing.T) {
	start, end := selectionWindow(7, 3, 3)
	assert.Equal(t, 2, start)
	assert.Equal(t, 5, end)

	start, end = selectionWindow(7, 0, 3)
	assert.Equal(t, 0, start)
	assert.Equal(t, 3, end)

	start, end = selectionWindow(7, 6, 3)
	assert.Equal(t, 4, start)
	assert.Equal(t, 7, end)
}

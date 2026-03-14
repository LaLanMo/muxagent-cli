package relayws

import (
	"testing"
	"time"

	"github.com/LaLanMo/muxagent-cli/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func makeEvent(typ domain.EventType) domain.Event {
	return domain.Event{Type: typ, At: time.Now()}
}

func TestEventBuffer_PushAssignsSequence(t *testing.T) {
	buf := NewEventBuffer(10)

	e1 := buf.Push(makeEvent(domain.EventMessageDelta))
	e2 := buf.Push(makeEvent(domain.EventReasoning))
	e3 := buf.Push(makeEvent(domain.EventToolStarted))

	assert.Equal(t, uint64(1), e1.Seq)
	assert.Equal(t, uint64(2), e2.Seq)
	assert.Equal(t, uint64(3), e3.Seq)
	assert.Equal(t, uint64(3), buf.Seq())
}

func TestEventBuffer_SinceReturnsEventsAfterSeq(t *testing.T) {
	buf := NewEventBuffer(10)

	buf.Push(makeEvent(domain.EventMessageDelta))
	buf.Push(makeEvent(domain.EventReasoning))
	buf.Push(makeEvent(domain.EventToolStarted))

	events, complete := buf.Since(1)
	require.True(t, complete)
	require.Len(t, events, 2)
	assert.Equal(t, uint64(2), events[0].Seq)
	assert.Equal(t, uint64(3), events[1].Seq)
}

func TestEventBuffer_SinceZeroReturnsAll(t *testing.T) {
	buf := NewEventBuffer(10)

	buf.Push(makeEvent(domain.EventMessageDelta))
	buf.Push(makeEvent(domain.EventReasoning))

	events, complete := buf.Since(0)
	require.True(t, complete)
	require.Len(t, events, 2)
}

func TestEventBuffer_SinceCaughtUp(t *testing.T) {
	buf := NewEventBuffer(10)

	buf.Push(makeEvent(domain.EventMessageDelta))
	buf.Push(makeEvent(domain.EventReasoning))

	events, complete := buf.Since(2)
	require.True(t, complete)
	assert.Empty(t, events)
}

func TestEventBuffer_SinceEmpty(t *testing.T) {
	buf := NewEventBuffer(10)

	events, complete := buf.Since(0)
	require.True(t, complete)
	assert.Empty(t, events)
}

func TestEventBuffer_RingOverwrite(t *testing.T) {
	buf := NewEventBuffer(3)

	buf.Push(makeEvent(domain.EventMessageDelta))  // seq=1
	buf.Push(makeEvent(domain.EventReasoning))     // seq=2
	buf.Push(makeEvent(domain.EventToolStarted))   // seq=3
	buf.Push(makeEvent(domain.EventToolCompleted)) // seq=4, overwrites seq=1

	// Asking for events after seq=0 should return incomplete (seq=1 is gone)
	events, complete := buf.Since(0)
	assert.False(t, complete)
	require.Len(t, events, 3)
	assert.Equal(t, uint64(2), events[0].Seq)
	assert.Equal(t, uint64(4), events[2].Seq)
}

func TestEventBuffer_RingOverwriteWithValidSeq(t *testing.T) {
	buf := NewEventBuffer(3)

	buf.Push(makeEvent(domain.EventMessageDelta))  // seq=1
	buf.Push(makeEvent(domain.EventReasoning))     // seq=2
	buf.Push(makeEvent(domain.EventToolStarted))   // seq=3
	buf.Push(makeEvent(domain.EventToolCompleted)) // seq=4, overwrites seq=1

	// Asking for events after seq=2 should work (seq=2 is still boundary)
	events, complete := buf.Since(2)
	require.True(t, complete)
	require.Len(t, events, 2)
	assert.Equal(t, uint64(3), events[0].Seq)
	assert.Equal(t, uint64(4), events[1].Seq)
}

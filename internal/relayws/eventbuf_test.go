package relayws

import (
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/LaLanMo/muxagent-cli/internal/appwire"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func makeEvent(typ appwire.EventType) appwire.Event {
	return appwire.Event{Type: typ, At: time.Now()}
}

func TestEventBuffer_PushAssignsSequenceAndKeepsEpoch(t *testing.T) {
	buf := NewEventBuffer(10)
	epoch := buf.StreamEpoch()
	require.NotZero(t, epoch)

	e1 := buf.Push(makeEvent(appwire.EventMessageDelta))
	e2 := buf.Push(makeEvent(appwire.EventReasoning))
	e3 := buf.Push(makeEvent(appwire.EventToolStarted))

	assert.Equal(t, uint64(1), e1.Seq)
	assert.Equal(t, uint64(2), e2.Seq)
	assert.Equal(t, uint64(3), e3.Seq)
	assert.Equal(t, uint64(3), buf.Seq())
	assert.Equal(t, epoch, buf.StreamEpoch())
}

func TestEventBuffer_ReplaySinceReturnsEventsAfterSeq(t *testing.T) {
	buf := NewEventBuffer(10)

	buf.Push(makeEvent(appwire.EventMessageDelta))
	buf.Push(makeEvent(appwire.EventReasoning))
	buf.Push(makeEvent(appwire.EventToolStarted))

	snapshot := buf.ReplaySince(buf.StreamEpoch(), 1)
	require.Equal(t, appwire.ResyncStatusOK, snapshot.Status)
	require.Equal(t, uint64(3), snapshot.ReplayedThroughSeq)
	require.Len(t, snapshot.Events, 2)
	assert.Equal(t, uint64(2), snapshot.Events[0].Seq)
	assert.Equal(t, uint64(3), snapshot.Events[1].Seq)
}

func TestEventBuffer_ReplaySinceZeroCursorReturnsResetSnapshot(t *testing.T) {
	buf := NewEventBuffer(10)

	first := buf.Push(makeEvent(appwire.EventMessageDelta))
	second := buf.Push(makeEvent(appwire.EventReasoning))

	snapshot := buf.ReplaySince(0, 0)
	require.Equal(t, appwire.ResyncStatusReset, snapshot.Status)
	require.Equal(t, buf.StreamEpoch(), snapshot.StreamEpoch)
	require.Equal(t, second.Seq, snapshot.ReplayedThroughSeq)
	require.Len(t, snapshot.Events, 2)
	assert.Equal(t, first.Seq, snapshot.Events[0].Seq)
	assert.Equal(t, second.Seq, snapshot.Events[1].Seq)
}

func TestEventBuffer_ReplaySinceCaughtUp(t *testing.T) {
	buf := NewEventBuffer(10)

	buf.Push(makeEvent(appwire.EventMessageDelta))
	buf.Push(makeEvent(appwire.EventReasoning))

	snapshot := buf.ReplaySince(buf.StreamEpoch(), 2)
	require.Equal(t, appwire.ResyncStatusOK, snapshot.Status)
	require.Equal(t, uint64(2), snapshot.ReplayedThroughSeq)
	assert.Empty(t, snapshot.Events)
}

func TestEventBuffer_ReplaySinceFutureSeqReturnsReset(t *testing.T) {
	buf := NewEventBuffer(10)

	buf.Push(makeEvent(appwire.EventMessageDelta))

	snapshot := buf.ReplaySince(buf.StreamEpoch(), 99)
	require.Equal(t, appwire.ResyncStatusReset, snapshot.Status)
	require.Equal(t, uint64(1), snapshot.ReplayedThroughSeq)
	require.Len(t, snapshot.Events, 1)
}

func TestEventBuffer_ReplaySinceEmpty(t *testing.T) {
	buf := NewEventBuffer(10)

	snapshot := buf.ReplaySince(buf.StreamEpoch(), 0)
	require.Equal(t, appwire.ResyncStatusOK, snapshot.Status)
	require.Equal(t, buf.StreamEpoch(), snapshot.StreamEpoch)
	require.Zero(t, snapshot.ReplayedThroughSeq)
	assert.Empty(t, snapshot.Events)
}

func TestEventBuffer_ReplaySinceGapAfterRingOverwrite(t *testing.T) {
	buf := NewEventBuffer(3)

	buf.Push(makeEvent(appwire.EventMessageDelta)) // seq=1
	second := buf.Push(makeEvent(appwire.EventReasoning))
	buf.Push(makeEvent(appwire.EventToolStarted))             // seq=3
	fourth := buf.Push(makeEvent(appwire.EventToolCompleted)) // seq=4, overwrites seq=1

	snapshot := buf.ReplaySince(buf.StreamEpoch(), 0)
	require.Equal(t, appwire.ResyncStatusGap, snapshot.Status)
	require.Equal(t, fourth.Seq, snapshot.ReplayedThroughSeq)
	require.Len(t, snapshot.Events, 3)
	assert.Equal(t, second.Seq, snapshot.Events[0].Seq)
	assert.Equal(t, fourth.Seq, snapshot.Events[2].Seq)
}

func TestEventBuffer_ReplaySinceReturnsResetAfterBufferReset(t *testing.T) {
	buf := NewEventBuffer(10)
	oldEpoch := buf.StreamEpoch()

	buf.Push(makeEvent(appwire.EventMessageDelta))
	newEpoch := buf.Reset()
	require.NotZero(t, newEpoch)
	require.NotEqual(t, oldEpoch, newEpoch)

	event := buf.Push(makeEvent(appwire.EventReasoning))
	snapshot := buf.ReplaySince(oldEpoch, 1)
	require.Equal(t, appwire.ResyncStatusReset, snapshot.Status)
	require.Equal(t, newEpoch, snapshot.StreamEpoch)
	require.Equal(t, event.Seq, snapshot.ReplayedThroughSeq)
	require.Len(t, snapshot.Events, 1)
	assert.Equal(t, event.Seq, snapshot.Events[0].Seq)
}

func TestEventBuffer_ReplaySinceReturnsAtomicSnapshot(t *testing.T) {
	buf := NewEventBuffer(256)
	for i := 0; i < 64; i++ {
		buf.Push(makeEvent(appwire.EventMessageDelta))
	}

	epoch := buf.StreamEpoch()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 2000; i++ {
			buf.Push(makeEvent(appwire.EventReasoning))
			runtime.Gosched()
		}
	}()

	for i := 0; i < 1000; i++ {
		snapshot := buf.ReplaySince(epoch, 0)
		require.Equal(t, epoch, snapshot.StreamEpoch)
		require.NotEqual(t, appwire.ResyncStatusReset, snapshot.Status)
		require.NotEmpty(t, snapshot.Events)
		require.Equal(t, snapshot.Events[len(snapshot.Events)-1].Seq, snapshot.ReplayedThroughSeq)
	}

	wg.Wait()
}

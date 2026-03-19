package relayws

import (
	"crypto/rand"
	"encoding/binary"
	"sync"
	"sync/atomic"

	"github.com/LaLanMo/muxagent-cli/internal/appwire"
)

// EventBuffer is a fixed-size ring buffer for app transport events.
// Each pushed event is assigned a monotonically increasing sequence number.
type EventBuffer struct {
	mu          sync.RWMutex
	events      []appwire.Event
	size        int
	head        int // next write position
	count       int // number of events stored
	seq         uint64
	streamEpoch uint64
}

type ReplaySnapshot struct {
	Events             []appwire.Event
	Status             appwire.ResyncStatus
	StreamEpoch        uint64
	ReplayedThroughSeq uint64
}

var fallbackStreamEpoch uint64

func NewEventBuffer(size int) *EventBuffer {
	if size <= 0 {
		size = 1024
	}
	return &EventBuffer{
		events:      make([]appwire.Event, size),
		size:        size,
		streamEpoch: nextStreamEpoch(),
	}
}

// Push assigns a sequence number to the event, stores it in the ring buffer,
// and returns the event with the assigned Seq.
func (b *EventBuffer) Push(event appwire.Event) appwire.Event {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.seq++
	event.Seq = b.seq

	b.events[b.head] = event
	b.head = (b.head + 1) % b.size
	if b.count < b.size {
		b.count++
	}

	return event
}

// ReplaySince returns an atomic replay snapshot for the requested cursor.
func (b *EventBuffer) ReplaySince(streamEpoch, afterSeq uint64) ReplaySnapshot {
	b.mu.RLock()
	defer b.mu.RUnlock()

	snapshot := ReplaySnapshot{
		Status:             appwire.ResyncStatusOK,
		StreamEpoch:        b.streamEpoch,
		ReplayedThroughSeq: b.seq,
	}

	if streamEpoch == 0 || streamEpoch != b.streamEpoch {
		snapshot.Status = appwire.ResyncStatusReset
		snapshot.Events = b.snapshotAllLocked()
		return snapshot
	}

	if afterSeq > b.seq {
		snapshot.Status = appwire.ResyncStatusReset
		snapshot.Events = b.snapshotAllLocked()
		return snapshot
	}

	if b.count == 0 || afterSeq == b.seq {
		return snapshot
	}

	oldestIdx := (b.head - b.count + b.size) % b.size
	oldestSeq := b.events[oldestIdx].Seq

	if afterSeq < oldestSeq-1 {
		snapshot.Status = appwire.ResyncStatusGap
		snapshot.Events = b.snapshotAllLocked()
		return snapshot
	}

	result := make([]appwire.Event, 0, b.count)
	for i := 0; i < b.count; i++ {
		idx := (oldestIdx + i) % b.size
		if b.events[idx].Seq > afterSeq {
			result = append(result, b.events[idx])
		}
	}
	snapshot.Events = result
	return snapshot
}

// Reset clears buffered history and advances the replay stream epoch.
func (b *EventBuffer) Reset() uint64 {
	b.mu.Lock()
	defer b.mu.Unlock()

	clear(b.events)
	b.head = 0
	b.count = 0
	b.seq = 0
	b.streamEpoch = nextStreamEpoch()

	return b.streamEpoch
}

// Seq returns the current (latest) sequence number.
func (b *EventBuffer) Seq() uint64 {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.seq
}

// StreamEpoch returns the current replay stream epoch.
func (b *EventBuffer) StreamEpoch() uint64 {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.streamEpoch
}

func (b *EventBuffer) snapshotAllLocked() []appwire.Event {
	if b.count == 0 {
		return nil
	}

	oldestIdx := (b.head - b.count + b.size) % b.size
	result := make([]appwire.Event, 0, b.count)
	for i := 0; i < b.count; i++ {
		idx := (oldestIdx + i) % b.size
		result = append(result, b.events[idx])
	}
	return result
}

func nextStreamEpoch() uint64 {
	var raw [8]byte
	if _, err := rand.Read(raw[:]); err == nil {
		if epoch := binary.LittleEndian.Uint64(raw[:]); epoch != 0 {
			return epoch
		}
	}

	return atomic.AddUint64(&fallbackStreamEpoch, 1)
}

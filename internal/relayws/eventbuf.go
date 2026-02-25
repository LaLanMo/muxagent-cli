package relayws

import (
	"sync"

	"github.com/LaLanMo/muxagent-cli/internal/domain"
)

// EventBuffer is a fixed-size ring buffer for domain events.
// Each pushed event is assigned a monotonically increasing sequence number.
type EventBuffer struct {
	mu     sync.RWMutex
	events []domain.Event
	size   int
	head   int // next write position
	count  int // number of events stored
	seq    uint64
}

func NewEventBuffer(size int) *EventBuffer {
	if size <= 0 {
		size = 1024
	}
	return &EventBuffer{
		events: make([]domain.Event, size),
		size:   size,
	}
}

// Push assigns a sequence number to the event, stores it in the ring buffer,
// and returns the event with the assigned Seq.
func (b *EventBuffer) Push(event domain.Event) domain.Event {
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

// Since returns all events with Seq > afterSeq.
// The second return value indicates whether the history is complete (no gap).
// If afterSeq is too old and events have been evicted from the buffer, complete is false.
func (b *EventBuffer) Since(afterSeq uint64) (events []domain.Event, complete bool) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if b.count == 0 {
		return nil, true
	}

	// Oldest event still in the buffer
	oldestIdx := (b.head - b.count + b.size) % b.size
	oldestSeq := b.events[oldestIdx].Seq

	// If caller is caught up
	if afterSeq >= b.seq {
		return nil, true
	}

	// If the requested seq is older than what we have, gap exists
	if afterSeq < oldestSeq-1 {
		// Return everything we have, but signal incomplete
		result := make([]domain.Event, 0, b.count)
		for i := 0; i < b.count; i++ {
			idx := (oldestIdx + i) % b.size
			result = append(result, b.events[idx])
		}
		return result, false
	}

	// Collect events with Seq > afterSeq
	result := make([]domain.Event, 0)
	for i := 0; i < b.count; i++ {
		idx := (oldestIdx + i) % b.size
		if b.events[idx].Seq > afterSeq {
			result = append(result, b.events[idx])
		}
	}
	return result, true
}

// Seq returns the current (latest) sequence number.
func (b *EventBuffer) Seq() uint64 {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.seq
}

package clock

import (
	"sync"
	"time"
)

// Clock makes time-bound business rules deterministic and testable.
type Clock interface {
	Now() time.Time
}

type System struct{}

func (System) Now() time.Time { return time.Now().UTC() }

// Manual is a concurrency-safe controllable clock used by integration tests.
type Manual struct {
	mu  sync.RWMutex
	now time.Time
}

func NewManual(now time.Time) *Manual {
	return &Manual{now: now.UTC()}
}

func (m *Manual) Now() time.Time {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.now
}

func (m *Manual) Set(now time.Time) {
	m.mu.Lock()
	m.now = now.UTC()
	m.mu.Unlock()
}

func (m *Manual) Advance(duration time.Duration) time.Time {
	m.mu.Lock()
	m.now = m.now.Add(duration)
	now := m.now
	m.mu.Unlock()
	return now
}

// Window describes an inclusive start and exclusive end interval.
type Window struct {
	Start time.Time
	End   time.Time
}

func NewWindow(start, end time.Time) (Window, bool) {
	start = start.UTC()
	end = end.UTC()
	if !start.Before(end) {
		return Window{}, false
	}
	return Window{Start: start, End: end}, true
}

func (w Window) Contains(at time.Time) bool {
	at = at.UTC()
	return !at.Before(w.Start) && at.Before(w.End)
}

func (w Window) Overlaps(other Window) bool {
	return w.Start.Before(other.End) && other.Start.Before(w.End)
}

func SessionElapsed(lastSeen, now time.Time) time.Duration {
	elapsed := now.UTC().Sub(lastSeen.UTC())
	if elapsed < 0 {
		return -elapsed
	}
	return elapsed
}

func (w Window) Duration() time.Duration { return w.End.Sub(w.Start) }

func (w Window) IsZero() bool { return w.Start.IsZero() && w.End.IsZero() }

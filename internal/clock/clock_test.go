package clock

import (
	"testing"
	"time"
)

func TestManualClockAndWindows(t *testing.T) {
	start := time.Date(2026, 8, 26, 0, 0, 0, 0, time.UTC)
	manual := NewManual(start)
	if !manual.Now().Equal(start) {
		t.Fatal("initial clock")
	}
	if got := manual.Advance(time.Hour); !got.Equal(start.Add(time.Hour)) {
		t.Fatalf("advanced=%v", got)
	}
	manual.Set(start.Add(2 * time.Hour))
	if !manual.Now().Equal(start.Add(2 * time.Hour)) {
		t.Fatal("set clock")
	}
	window, ok := NewWindow(start, start.Add(time.Hour))
	if !ok {
		t.Fatal("valid window rejected")
	}
	if window.Duration() != time.Hour {
		t.Fatalf("duration=%v", window.Duration())
	}
	if !window.Contains(start) || window.Contains(start.Add(time.Hour)) {
		t.Fatal("inclusive/exclusive bounds wrong")
	}
	overlap, _ := NewWindow(start.Add(30*time.Minute), start.Add(90*time.Minute))
	if !window.Overlaps(overlap) {
		t.Fatal("overlap missed")
	}
	adjacent, _ := NewWindow(start.Add(time.Hour), start.Add(2*time.Hour))
	if window.Overlaps(adjacent) {
		t.Fatal("adjacent windows overlap")
	}
	if _, ok = NewWindow(start, start); ok {
		t.Fatal("zero window accepted")
	}
}

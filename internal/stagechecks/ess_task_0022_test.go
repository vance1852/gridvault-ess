package stagechecks_test

import (
	"testing"
	"time"

	"github.com/vance1852/gridvault-ess/internal/clock"
	"github.com/vance1852/gridvault-ess/internal/dispatch"
)

func TestAdjacentReservationsDoNotConflict0022(t *testing.T) {
	start := time.Date(2026, 8, 26, 22, 0, 0, 0, time.UTC)
	middle := start.Add(time.Hour)
	end := middle.Add(time.Hour)
	firstWindow, _ := clock.NewWindow(start, middle)
	secondWindow, _ := clock.NewWindow(middle, end)
	first, _ := dispatch.NewReservation("plan-a", "cluster-22", 50, firstWindow, start)
	second, _ := dispatch.NewReservation("plan-b", "cluster-22", 50, secondWindow, start)
	if first.Conflicts(second) {
		t.Fatal("adjacent half-open reservations conflict")
	}
}

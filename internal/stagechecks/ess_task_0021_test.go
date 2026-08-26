package stagechecks_test

import (
	"testing"
	"time"

	"github.com/vance1852/gridvault-ess/internal/clock"
	"github.com/vance1852/gridvault-ess/internal/settlement"
)

func TestSettlementWindowExcludesEnd0021(t *testing.T) {
	start := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)
	window, ok := clock.NewWindow(start, end)
	if !ok {
		t.Fatal("window invalid")
	}
	period := settlement.Period{Window: window, Status: settlement.Open}
	if !period.Accepts(start) {
		t.Fatal("settlement rejected inclusive start")
	}
	if period.Accepts(end) {
		t.Fatal("settlement accepted its exclusive end")
	}
}

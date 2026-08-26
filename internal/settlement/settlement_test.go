package settlement

import (
	"errors"
	"github.com/vance1852/gridvault-ess/internal/fault"
	"testing"
	"time"
)

var settlementNow = time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)

func validPeriod(t *testing.T) Period {
	t.Helper()
	value, err := NewPeriod("site", settlementNow.Add(-24*time.Hour), settlementNow.Add(-time.Hour), settlementNow.Add(-25*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	return value
}
func TestPeriodCalculationAndClosure(t *testing.T) {
	period := validPeriod(t)
	calculating, err := period.Begin(period.Version, 2, 2, settlementNow)
	if err != nil {
		t.Fatal(err)
	}
	if calculating.Status != Calculating || calculating.Version != period.Version+1 {
		t.Fatalf("calculating=%+v", calculating)
	}
	closed, err := calculating.Close("operator", calculating.Version, 2, 2, settlementNow)
	if err != nil {
		t.Fatal(err)
	}
	if closed.Status != Closed || closed.ClosedBy != "operator" || closed.ClosedAt == nil {
		t.Fatalf("closed=%+v", closed)
	}
	if closed.Accepts(settlementNow.Add(-2 * time.Hour)) {
		t.Fatal("closed period accepts late data")
	}
	if period.Status != Open {
		t.Fatal("transition mutated original")
	}
}
func TestPeriodBeginRules(t *testing.T) {
	period := validPeriod(t)
	if _, err := period.Begin(0, 1, 1, settlementNow); !errors.Is(err, fault.ErrVersionConflict) {
		t.Fatalf("version=%v", err)
	}
	future, _ := NewPeriod("site", settlementNow, settlementNow.Add(time.Hour), settlementNow)
	if _, err := future.Begin(future.Version, 1, 1, settlementNow); fault.Code(err) != "period_not_ended" {
		t.Fatalf("future=%v", err)
	}
	if _, err := period.Begin(period.Version, 0, 0, settlementNow); fault.Code(err) != "period_empty" {
		t.Fatalf("empty=%v", err)
	}
	if _, err := period.Begin(period.Version, 2, 1, settlementNow); fault.Code(err) != "plan_scope_mismatch" {
		t.Fatalf("scope=%v", err)
	}
}
func TestPeriodCloseRules(t *testing.T) {
	period := validPeriod(t)
	if _, err := period.Close("operator", period.Version, 1, 1, settlementNow); fault.Code(err) != "period_not_calculating" {
		t.Fatalf("open close=%v", err)
	}
	calculating, _ := period.Begin(period.Version, 1, 1, settlementNow)
	if _, err := calculating.Close("operator", calculating.Version, 2, 1, settlementNow); fault.Code(err) != "settlement_incomplete" {
		t.Fatalf("entries=%v", err)
	}
	if _, err := calculating.Close("", calculating.Version, 1, 1, settlementNow); fault.Code(err) != "actor_required" {
		t.Fatalf("actor=%v", err)
	}
	if _, err := calculating.Close("operator", 0, 1, 1, settlementNow); !errors.Is(err, fault.ErrVersionConflict) {
		t.Fatalf("version=%v", err)
	}
}
func TestPeriodCreationValidation(t *testing.T) {
	if _, err := NewPeriod("", settlementNow, settlementNow.Add(time.Hour), settlementNow); fault.Code(err) != "missing_site" {
		t.Fatalf("site=%v", err)
	}
	if _, err := NewPeriod("site", settlementNow, settlementNow, settlementNow); fault.Code(err) != "invalid_period_window" {
		t.Fatalf("window=%v", err)
	}
	if _, err := NewPeriod("site", settlementNow, settlementNow.Add(30*time.Minute), settlementNow); fault.Code(err) != "invalid_period_duration" {
		t.Fatalf("short=%v", err)
	}
	if _, err := NewPeriod("site", settlementNow, settlementNow.Add(367*24*time.Hour), settlementNow); fault.Code(err) != "invalid_period_duration" {
		t.Fatalf("long=%v", err)
	}
}
func TestEntryCalculationAndSummary(t *testing.T) {
	first, err := NewEntry("period", "plan-a", 100000, 90000, 25, settlementNow)
	if err != nil {
		t.Fatal(err)
	}
	if first.DeviationWh != -10000 || first.AmountMilliCent != 2250 {
		t.Fatalf("first=%+v", first)
	}
	second, err := NewEntry("period", "plan-b", 200000, 220000, 25, settlementNow)
	if err != nil {
		t.Fatal(err)
	}
	summary := Summarize([]Entry{first, second})
	if summary.EntryCount != 2 || summary.PlannedWh != 300000 || summary.ActualWh != 310000 || summary.DeviationWh != 10000 || summary.AmountMilliCent != 7750 {
		t.Fatalf("summary=%+v", summary)
	}
}
func TestEntryValidation(t *testing.T) {
	tests := []struct {
		name, period, plan     string
		planned, actual, price int64
		code                   string
	}{{"period", "", "plan", 1, 1, 1, "missing_entry_owner"}, {"plan", "period", "", 1, 1, 1, "missing_entry_owner"}, {"planned", "period", "plan", 0, 1, 1, "invalid_energy"}, {"actual", "period", "plan", 1, -1, 1, "invalid_energy"}, {"price", "period", "plan", 1, 1, -1, "invalid_price"}}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewEntry(tt.period, tt.plan, tt.planned, tt.actual, tt.price, settlementNow)
			if fault.Code(err) != tt.code {
				t.Fatalf("code=%s err=%v", fault.Code(err), err)
			}
		})
	}
}

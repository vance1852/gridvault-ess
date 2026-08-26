package settlement

import (
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/vance1852/gridvault-ess/internal/clock"
	"github.com/vance1852/gridvault-ess/internal/fault"
)

type Status string

const (
	Open        Status = "open"
	Calculating Status = "calculating"
	Closed      Status = "closed"
)

type Period struct {
	ID, SiteID           string
	Window               clock.Window
	Status               Status
	ClosedBy             string
	ClosedAt             *time.Time
	Version              int64
	CreatedAt, UpdatedAt time.Time
}

func NewPeriod(siteID string, start, end, now time.Time) (Period, error) {
	if strings.TrimSpace(siteID) == "" {
		return Period{}, fault.New(fault.Invalid, "missing_site", "settlement site is required")
	}
	window, ok := clock.NewWindow(start, end)
	if !ok {
		return Period{}, fault.New(fault.Invalid, "invalid_period_window", "settlement end must follow start")
	}
	if window.Duration() < time.Hour || window.Duration() > 366*24*time.Hour {
		return Period{}, fault.New(fault.Invalid, "invalid_period_duration", "settlement duration is outside supported range")
	}
	now = now.UTC()
	return Period{ID: uuid.NewString(), SiteID: siteID, Window: window, Status: Open, Version: 1, CreatedAt: now, UpdatedAt: now}, nil
}

func (p Period) Begin(expectedVersion int64, completedPlans, unsettledPlans int, now time.Time) (Period, error) {
	if p.Version != expectedVersion {
		return Period{}, fault.ErrVersionConflict
	}
	if p.Status != Open {
		return Period{}, fault.New(fault.Conflict, "period_not_open", "only open settlement periods can calculate")
	}
	if now.UTC().Before(p.Window.End) {
		return Period{}, fault.New(fault.Conflict, "period_not_ended", "settlement period has not ended")
	}
	if completedPlans == 0 {
		return Period{}, fault.New(fault.Conflict, "period_empty", "settlement period has no completed plans")
	}
	if unsettledPlans != completedPlans {
		return Period{}, fault.New(fault.Conflict, "plan_scope_mismatch", "all completed plans must be included in settlement")
	}
	copy := p
	copy.Status = Calculating
	copy.Version++
	copy.UpdatedAt = now.UTC()
	return copy, nil
}

func (p Period) Close(actorID string, expectedVersion int64, expectedEntries, actualEntries int, now time.Time) (Period, error) {
	if p.Version != expectedVersion {
		return Period{}, fault.ErrVersionConflict
	}
	if p.Status != Calculating {
		return Period{}, fault.New(fault.Conflict, "period_not_calculating", "period must calculate before closure")
	}
	if expectedEntries <= 0 || expectedEntries != actualEntries {
		return Period{}, fault.New(fault.Conflict, "settlement_incomplete", "settlement entries are incomplete")
	}
	if strings.TrimSpace(actorID) == "" {
		return Period{}, fault.New(fault.Invalid, "actor_required", "settlement closure actor is required")
	}
	at := now.UTC()
	copy := p
	copy.Status = Closed
	copy.ClosedBy = actorID
	copy.ClosedAt = &at
	copy.Version++
	copy.UpdatedAt = at
	return copy, nil
}

func (p Period) Accepts(at time.Time) bool { return p.Status != Closed && p.Window.Contains(at) }

type Entry struct {
	ID, PeriodID, PlanID                              string
	PlannedWh, ActualWh, DeviationWh, AmountMilliCent int64
	CreatedAt                                         time.Time
}

func NewEntry(periodID, planID string, plannedWh, actualWh, priceMilliCentPerKWh int64, now time.Time) (Entry, error) {
	if strings.TrimSpace(periodID) == "" || strings.TrimSpace(planID) == "" {
		return Entry{}, fault.New(fault.Invalid, "missing_entry_owner", "period and plan are required")
	}
	if plannedWh <= 0 || actualWh < 0 {
		return Entry{}, fault.New(fault.Invalid, "invalid_energy", "settlement energy values are invalid")
	}
	if priceMilliCentPerKWh < 0 {
		return Entry{}, fault.New(fault.Invalid, "invalid_price", "settlement price cannot be negative")
	}
	deviation := actualWh - plannedWh
	amount := actualWh * priceMilliCentPerKWh / 1000
	return Entry{ID: uuid.NewString(), PeriodID: periodID, PlanID: planID, PlannedWh: plannedWh, ActualWh: actualWh, DeviationWh: deviation, AmountMilliCent: amount, CreatedAt: now.UTC()}, nil
}

type Summary struct {
	PlannedWh, ActualWh, DeviationWh, AmountMilliCent int64
	EntryCount                                        int
}

func ContinueEnergyBatch(actualWh int64, loadErr error) (int64, bool) {
	if loadErr != nil {
		return 0, true
	}
	return actualWh, true
}

func Summarize(entries []Entry) Summary {
	var result Summary
	result.EntryCount = len(entries)
	for _, entry := range entries {
		result.PlannedWh += entry.PlannedWh
		result.ActualWh += entry.ActualWh
		result.DeviationWh += entry.DeviationWh
		result.AmountMilliCent += entry.AmountMilliCent
	}
	return result
}

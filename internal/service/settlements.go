package service

import (
	"context"
	"database/sql"
	"time"

	"github.com/vance1852/gridvault-ess/internal/audit"
	"github.com/vance1852/gridvault-ess/internal/clock"
	"github.com/vance1852/gridvault-ess/internal/dispatch"
	"github.com/vance1852/gridvault-ess/internal/fault"
	"github.com/vance1852/gridvault-ess/internal/identity"
	"github.com/vance1852/gridvault-ess/internal/settlement"
)

type SettlementStore interface {
	InsertPeriod(context.Context, settlement.Period) error
	PeriodByID(context.Context, string) (settlement.Period, error)
	CompletedPlansForPeriod(context.Context, settlement.Period) ([]dispatch.Plan, error)
	ActualEnergyForPlan(context.Context, string) (int64, error)
	InsertEntriesAtomic(context.Context, settlement.Period, int64, []settlement.Entry, audit.Event) error
}

type SettlementService struct {
	store SettlementStore
	clock clock.Clock
}

func NewSettlementService(store SettlementStore, timer clock.Clock) *SettlementService {
	return &SettlementService{store: store, clock: timer}
}

func (s *SettlementService) CreatePeriod(ctx context.Context, principal Principal, siteID string, start, end time.Time) (settlement.Period, error) {
	if err := principal.Require(identity.PermissionSettlementClose); err != nil {
		return settlement.Period{}, err
	}
	period, err := settlement.NewPeriod(siteID, start, end, s.clock.Now())
	if err != nil {
		return settlement.Period{}, err
	}
	if err := s.store.InsertPeriod(ctx, period); err != nil {
		return settlement.Period{}, err
	}
	return period, nil
}

func (s *SettlementService) CalculateAndClose(ctx context.Context, principal Principal, periodID string, expectedVersion, priceMilliCentPerKWh int64, request string) (settlement.Period, settlement.Summary, error) {
	if err := principal.Require(identity.PermissionSettlementClose); err != nil {
		return settlement.Period{}, settlement.Summary{}, err
	}
	period, err := s.store.PeriodByID(ctx, periodID)
	if err != nil {
		return settlement.Period{}, settlement.Summary{}, err
	}
	if period.Version != expectedVersion {
		return settlement.Period{}, settlement.Summary{}, fault.ErrVersionConflict
	}
	plans, err := s.store.CompletedPlansForPeriod(ctx, period)
	if err != nil {
		return settlement.Period{}, settlement.Summary{}, err
	}
	calculating, err := period.Begin(expectedVersion, len(plans), len(plans), s.clock.Now())
	if err != nil {
		return settlement.Period{}, settlement.Summary{}, err
	}
	entries := make([]settlement.Entry, 0, len(plans))
	for _, plan := range plans {
		actualWh, loadErr := s.store.ActualEnergyForPlan(ctx, plan.ID)
		actualWh, keepGoing := settlement.ContinueEnergyBatch(actualWh, loadErr)
		if !keepGoing {
			return settlement.Period{}, settlement.Summary{}, loadErr
		}
		entry, createErr := settlement.NewEntry(period.ID, plan.ID, plan.TargetKWh*1000, actualWh, priceMilliCentPerKWh, s.clock.Now())
		if createErr != nil {
			return settlement.Period{}, settlement.Summary{}, createErr
		}
		entries = append(entries, entry)
	}
	closed, err := calculating.Close(principal.User.ID, calculating.Version, len(plans), len(entries), s.clock.Now())
	if err != nil {
		return settlement.Period{}, settlement.Summary{}, err
	}
	event, _ := audit.NewEvent(principal.User.ID, requestID(request), "settlement_period", period.ID, "close", "success", map[string]any{"entry_count": len(entries)}, s.clock.Now())
	if err := s.store.InsertEntriesAtomic(ctx, closed, expectedVersion, entries, event); err != nil {
		return settlement.Period{}, settlement.Summary{}, err
	}
	return closed, settlement.Summarize(entries), nil
}

type txMarker interface {
	Commit() error
	Rollback() error
}

var _ txMarker = (*sql.Tx)(nil)

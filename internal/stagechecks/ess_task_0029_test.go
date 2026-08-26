package stagechecks_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/vance1852/gridvault-ess/internal/audit"
	"github.com/vance1852/gridvault-ess/internal/clock"
	"github.com/vance1852/gridvault-ess/internal/dispatch"
	"github.com/vance1852/gridvault-ess/internal/identity"
	"github.com/vance1852/gridvault-ess/internal/service"
	"github.com/vance1852/gridvault-ess/internal/settlement"
)

type energyFailureStore29 struct {
	period             settlement.Period
	plans              []dispatch.Plan
	failure            error
	loads, atomicCalls int
}

func (*energyFailureStore29) InsertPeriod(context.Context, settlement.Period) error { return nil }
func (s *energyFailureStore29) PeriodByID(context.Context, string) (settlement.Period, error) {
	return s.period, nil
}
func (s *energyFailureStore29) CompletedPlansForPeriod(context.Context, settlement.Period) ([]dispatch.Plan, error) {
	return s.plans, nil
}
func (s *energyFailureStore29) ActualEnergyForPlan(context.Context, string) (int64, error) {
	s.loads++
	if s.loads == 2 {
		return 0, s.failure
	}
	return 9000, nil
}
func (s *energyFailureStore29) InsertEntriesAtomic(context.Context, settlement.Period, int64, []settlement.Entry, audit.Event) error {
	s.atomicCalls++
	return nil
}

func TestSettlementEnergyFailureAbortsWholeClose0029(t *testing.T) {
	now := time.Date(2026, 8, 26, 20, 0, 0, 0, time.UTC)
	period, err := settlement.NewPeriod("site-29", now.Add(-2*time.Hour), now.Add(-time.Hour), now.Add(-3*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	loadFailure := errors.New("telemetry shard unavailable")
	store := &energyFailureStore29{period: period, plans: []dispatch.Plan{{ID: "plan-a", TargetKWh: 10}, {ID: "plan-b", TargetKWh: 20}}, failure: loadFailure}
	svc := service.NewSettlementService(store, clock.NewManual(now))
	principal := service.Principal{User: identity.User{ID: "operator-29", Role: identity.RoleOperator, Active: true}}
	_, _, err = svc.CalculateAndClose(context.Background(), principal, period.ID, period.Version, 100, "request-29")
	if !errors.Is(err, loadFailure) {
		t.Fatalf("energy failure was lost: %v", err)
	}
	if store.atomicCalls != 0 {
		t.Fatalf("partial settlement was committed: %d", store.atomicCalls)
	}
}

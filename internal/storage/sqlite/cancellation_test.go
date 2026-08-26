package sqlite

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/vance1852/gridvault-ess/internal/clock"
	"github.com/vance1852/gridvault-ess/internal/dispatch"
	"github.com/vance1852/gridvault-ess/internal/identity"
	"github.com/vance1852/gridvault-ess/internal/settlement"
	"github.com/vance1852/gridvault-ess/internal/site"
)

// insertCompletedPlanForSettlement stores a plan and flips it to the completed status
// so it is visible to CompletedPlansForPeriod without exercising the full dispatch state machine.
func insertCompletedPlanForSettlement(t *testing.T, db *DB, s site.Site, creator identity.User, window clock.Window) dispatch.Plan {
	t.Helper()
	plan, err := dispatch.CreatePlan(dispatch.NewPlan{SiteID: s.ID, Name: "Completed Settlement Plan", Direction: dispatch.Charge, RequestedKW: 100, TargetKWh: 50, StartsAt: window.Start, EndsAt: window.End, CreatedBy: creator.ID}, window.Start)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.InsertPlan(context.Background(), plan); err != nil {
		t.Fatal(err)
	}
	completed := plan
	completed.Status = dispatch.PlanCompleted
	completed.Version++
	completed.UpdatedAt = window.End
	if err = db.UpdatePlan(context.Background(), completed, plan.Version); err != nil {
		t.Fatal(err)
	}
	return completed
}

// TestCompletedPlansForPeriodStopsOnCancelledContext proves that terminating a settlement
// preview cancels the in-window completed-plan scan instead of leaving it running as residual work.
func TestCompletedPlansForPeriodStopsOnCancelledContext(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	storage := insertActiveSite(t, db, "ESS-CANCEL", 1000)
	creator := insertTestUser(t, db, "cancel-plan@example.com", identity.RoleDispatcher)
	window, _ := clock.NewWindow(integrationNow.Add(-2*time.Hour), integrationNow.Add(-time.Hour))
	insertCompletedPlanForSettlement(t, db, storage, creator, window)
	period, err := settlement.NewPeriod(storage.ID, window.Start, window.End, integrationNow)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.InsertPeriod(ctx, period); err != nil {
		t.Fatal(err)
	}

	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := db.CompletedPlansForPeriod(cancelled, period); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}

	// The data is present, so a live context returns it: the failure above is cancellation, not emptiness.
	plans, err := db.CompletedPlansForPeriod(ctx, period)
	if err != nil {
		t.Fatalf("live load: %v", err)
	}
	if len(plans) != 1 || plans[0].ID == "" {
		t.Fatalf("plans=%+v", plans)
	}
}

// TestReservationsByPlanStopsOnCancelledContext proves that terminating a settlement preview
// cancels the capacity-reservation scan instead of leaving it running as residual work.
func TestReservationsByPlanStopsOnCancelledContext(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	storage := insertActiveSite(t, db, "ESS-RESCANCEL", 1000)
	creator := insertTestUser(t, db, "res-cancel-plan@example.com", identity.RoleDispatcher)
	window, _ := clock.NewWindow(integrationNow.Add(time.Hour), integrationNow.Add(2*time.Hour))
	plan, err := dispatch.CreatePlan(dispatch.NewPlan{SiteID: storage.ID, Name: "Reservation Cancel Plan", Direction: dispatch.Discharge, RequestedKW: 100, TargetKWh: 50, StartsAt: window.Start, EndsAt: window.End, CreatedBy: creator.ID}, window.Start)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.InsertPlan(ctx, plan); err != nil {
		t.Fatal(err)
	}
	cluster, err := site.CreateCluster(site.NewCluster{SiteID: storage.ID, Code: "BAT-RESCANCEL", RatedPowerKW: 200, CapacityKWh: 400, MinSOC: 10, MaxSOC: 90, InitialSOC: 50}, integrationNow)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.InsertCluster(ctx, cluster); err != nil {
		t.Fatal(err)
	}
	reservation, err := dispatch.NewReservation(plan.ID, cluster.ID, 100, window, integrationNow)
	if err != nil {
		t.Fatal(err)
	}
	if err = insertReservation(db.sql, ctx, reservation); err != nil {
		t.Fatal(err)
	}

	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := db.ReservationsByPlan(cancelled, plan.ID); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}

	// The reservation is present, so a live context returns it: the failure above is cancellation, not emptiness.
	loaded, err := db.ReservationsByPlan(ctx, plan.ID)
	if err != nil {
		t.Fatalf("live load: %v", err)
	}
	if len(loaded) != 1 || loaded[0].ID != reservation.ID {
		t.Fatalf("loaded=%+v", loaded)
	}
}

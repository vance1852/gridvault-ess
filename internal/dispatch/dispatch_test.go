package dispatch

import (
	"errors"
	"testing"
	"time"

	"github.com/vance1852/gridvault-ess/internal/clock"
	"github.com/vance1852/gridvault-ess/internal/fault"
)

var dispatchNow = time.Date(2026, 8, 26, 8, 0, 0, 0, time.UTC)

func validPlan(t *testing.T) Plan {
	t.Helper()
	value, err := CreatePlan(NewPlan{SiteID: "site-1", Name: "Morning peak support", Direction: Discharge, RequestedKW: 600, TargetKWh: 300, StartsAt: dispatchNow.Add(time.Hour), EndsAt: dispatchNow.Add(2 * time.Hour), CreatedBy: "dispatcher-1"}, dispatchNow)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func TestPlanHappyPathRequiresEveryBusinessStage(t *testing.T) {
	plan := validPlan(t)
	submitted, err := plan.Submit(2, 600, dispatchNow.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if submitted.Status != PlanSubmitted {
		t.Fatalf("status=%s", submitted.Status)
	}
	approved, err := submitted.Approve("operator-1", submitted.Version, dispatchNow.Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if approved.ApprovedBy != "operator-1" || approved.ApprovedAt == nil {
		t.Fatalf("approval=%+v", approved)
	}
	dispatched, err := approved.Dispatch(2, 2, dispatchNow.Add(3*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	running, err := dispatched.Start(2, 2, dispatchNow.Add(4*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	completed, err := running.Complete(300000, 2, 2, dispatchNow.Add(2*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if completed.Status != PlanCompleted || !completed.IsTerminal() {
		t.Fatalf("completed=%+v", completed)
	}
	if plan.Status != PlanDraft {
		t.Fatal("state transition mutated source plan")
	}
}

func TestPlanFourEyesAndVersionRules(t *testing.T) {
	plan := validPlan(t)
	submitted, _ := plan.Submit(1, 600, dispatchNow)
	if _, err := submitted.Approve("dispatcher-1", submitted.Version, dispatchNow); fault.Code(err) != "four_eyes_required" {
		t.Fatalf("self approval=%v", err)
	}
	if _, err := submitted.Approve("operator-1", submitted.Version-1, dispatchNow); !errors.Is(err, fault.ErrVersionConflict) {
		t.Fatalf("stale approval=%v", err)
	}
	approved, err := submitted.Approve("operator-1", submitted.Version, dispatchNow)
	if err != nil {
		t.Fatal(err)
	}
	if approved.Version != submitted.Version+1 {
		t.Fatalf("version=%d", approved.Version)
	}
}

func TestPlanRejectsIncompleteReservations(t *testing.T) {
	plan := validPlan(t)
	tests := []struct {
		name     string
		clusters int
		reserved int64
		code     string
	}{{"none", 0, 600, "plan_has_no_clusters"}, {"short", 1, 599, "reservation_power_mismatch"}, {"excess", 2, 601, "reservation_power_mismatch"}}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := plan.Submit(tt.clusters, tt.reserved, dispatchNow)
			if fault.Code(err) != tt.code {
				t.Fatalf("code=%s err=%v", fault.Code(err), err)
			}
		})
	}
}

func TestPlanRejectsIncompleteExecution(t *testing.T) {
	plan := validPlan(t)
	submitted, _ := plan.Submit(2, 600, dispatchNow)
	approved, _ := submitted.Approve("operator", submitted.Version, dispatchNow)
	if _, err := approved.Dispatch(0, 2, dispatchNow); fault.Code(err) != "execution_job_mismatch" {
		t.Fatalf("no jobs=%v", err)
	}
	if _, err := approved.Dispatch(1, 2, dispatchNow); fault.Code(err) != "execution_job_mismatch" {
		t.Fatalf("mismatch=%v", err)
	}
	dispatched, _ := approved.Dispatch(2, 2, dispatchNow)
	if _, err := dispatched.Start(1, 2, dispatchNow); fault.Code(err) != "jobs_not_started" {
		t.Fatalf("partial start=%v", err)
	}
	running, _ := dispatched.Start(2, 2, dispatchNow)
	if _, err := running.Complete(0, 2, 2, dispatchNow); fault.Code(err) != "energy_not_recorded" {
		t.Fatalf("zero energy=%v", err)
	}
	if _, err := running.Complete(100, 2, 1, dispatchNow); fault.Code(err) != "execution_incomplete" {
		t.Fatalf("partial complete=%v", err)
	}
}

func TestPlanCancellationBoundaries(t *testing.T) {
	plan := validPlan(t)
	cancelled, err := plan.Cancel(dispatchNow)
	if err != nil {
		t.Fatal(err)
	}
	if cancelled.Status != PlanCancelled {
		t.Fatalf("status=%s", cancelled.Status)
	}
	submitted, _ := plan.Submit(1, 600, dispatchNow)
	approved, _ := submitted.Approve("operator", submitted.Version, dispatchNow)
	dispatched, _ := approved.Dispatch(1, 1, dispatchNow)
	if _, err := dispatched.Cancel(dispatchNow); fault.Code(err) != "invalid_plan_transition" {
		t.Fatalf("dispatched cancel=%v", err)
	}
	failed, err := dispatched.Fail("gateway rejected command", dispatchNow)
	if err != nil {
		t.Fatal(err)
	}
	if failed.Status != PlanFailed || !failed.IsTerminal() {
		t.Fatalf("failed=%+v", failed)
	}
}

func TestCreatePlanValidatesEnergyAndWindow(t *testing.T) {
	base := NewPlan{SiteID: "site", Name: "Valid plan name", Direction: Charge, RequestedKW: 100, TargetKWh: 50, StartsAt: dispatchNow.Add(time.Hour), EndsAt: dispatchNow.Add(2 * time.Hour), CreatedBy: "user"}
	tests := []struct {
		name   string
		mutate func(*NewPlan)
		code   string
	}{{"owner", func(v *NewPlan) { v.CreatedBy = "" }, "missing_plan_owner"}, {"name", func(v *NewPlan) { v.Name = "x" }, "invalid_plan_name"}, {"direction", func(v *NewPlan) { v.Direction = "hold" }, "invalid_direction"}, {"power", func(v *NewPlan) { v.RequestedKW = 0 }, "invalid_requested_power"}, {"energy", func(v *NewPlan) { v.TargetKWh = 0 }, "invalid_target_energy"}, {"reversed", func(v *NewPlan) { v.EndsAt = v.StartsAt }, "invalid_plan_window"}, {"past", func(v *NewPlan) { v.StartsAt = dispatchNow.Add(-time.Hour); v.EndsAt = dispatchNow.Add(time.Hour) }, "plan_starts_in_past"}, {"too long", func(v *NewPlan) { v.EndsAt = v.StartsAt.Add(8 * 24 * time.Hour) }, "invalid_plan_duration"}, {"unreachable", func(v *NewPlan) { v.TargetKWh = 101 }, "target_energy_unreachable"}}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := base
			tt.mutate(&input)
			_, err := CreatePlan(input, dispatchNow)
			if fault.Code(err) != tt.code {
				t.Fatalf("code=%s err=%v", fault.Code(err), err)
			}
		})
	}
}

func TestReservationConflictAndLifecycle(t *testing.T) {
	window, _ := clock.NewWindow(dispatchNow, dispatchNow.Add(time.Hour))
	first, err := NewReservation("plan-1", "cluster-1", 100, window, dispatchNow)
	if err != nil {
		t.Fatal(err)
	}
	overlapWindow, _ := clock.NewWindow(dispatchNow.Add(30*time.Minute), dispatchNow.Add(90*time.Minute))
	second, _ := NewReservation("plan-2", "cluster-1", 100, overlapWindow, dispatchNow)
	if !first.Conflicts(second) {
		t.Fatal("overlap was not detected")
	}
	other, _ := NewReservation("plan-2", "cluster-2", 100, overlapWindow, dispatchNow)
	if first.Conflicts(other) {
		t.Fatal("different cluster conflicts")
	}
	released, err := first.Release(dispatchNow)
	if err != nil {
		t.Fatal(err)
	}
	if released.Conflicts(second) {
		t.Fatal("released reservation conflicts")
	}
	consumed, err := first.Consume(dispatchNow)
	if err != nil {
		t.Fatal(err)
	}
	if consumed.Status != ReservationConsumed {
		t.Fatalf("status=%s", consumed.Status)
	}
	released, err = consumed.Release(dispatchNow)
	if err != nil {
		t.Fatal(err)
	}
	if released.Status != ReservationReleased {
		t.Fatalf("status=%s", released.Status)
	}
}

func TestAllocatePowerPreservesTotalAndRatings(t *testing.T) {
	ratings := map[string]int64{"a": 500, "b": 300, "c": 200}
	allocated, err := AllocatePower(777, ratings)
	if err != nil {
		t.Fatal(err)
	}
	var total int64
	for id, value := range allocated {
		total += value
		if value <= 0 {
			t.Fatalf("%s allocation=%d", id, value)
		}
		if value > ratings[id] {
			t.Fatalf("%s exceeded rating", id)
		}
	}
	if total != 777 {
		t.Fatalf("total=%d values=%v", total, allocated)
	}
	if _, err = AllocatePower(1001, ratings); fault.Code(err) != "insufficient_cluster_power" {
		t.Fatalf("oversize=%v", err)
	}
	if _, err = AllocatePower(0, ratings); fault.Code(err) != "invalid_requested_power" {
		t.Fatalf("zero=%v", err)
	}
	if _, err = AllocatePower(10, nil); fault.Code(err) != "clusters_required" {
		t.Fatalf("empty=%v", err)
	}
}

func TestJobLeaseSuccessAndRetry(t *testing.T) {
	job, err := NewJob("plan", "cluster", 3, dispatchNow)
	if err != nil {
		t.Fatal(err)
	}
	if !job.Claimable(dispatchNow) {
		t.Fatal("new job not claimable")
	}
	leased, err := job.Lease("worker-a", dispatchNow, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if leased.Status != JobLeased || leased.Attempts != 1 {
		t.Fatalf("leased=%+v", leased)
	}
	if _, err = leased.Succeed("worker-b", dispatchNow); fault.Code(err) != "lease_not_owned" {
		t.Fatalf("wrong owner=%v", err)
	}
	retry, err := leased.Fail("worker-a", errors.New("temporary"), true, dispatchNow, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if retry.Status != JobRetryable || !retry.NextAttemptAt.Equal(dispatchNow.Add(time.Second)) {
		t.Fatalf("retry=%+v", retry)
	}
	leased2, err := retry.Lease("worker-b", retry.NextAttemptAt, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	succeeded, err := leased2.Succeed("worker-b", retry.NextAttemptAt)
	if err != nil {
		t.Fatal(err)
	}
	if succeeded.Status != JobSucceeded || succeeded.LeaseUntil != nil {
		t.Fatalf("success=%+v", succeeded)
	}
}

func TestJobPermanentFailureAtAttemptLimit(t *testing.T) {
	job, _ := NewJob("plan", "cluster", 2, dispatchNow)
	leased, _ := job.Lease("worker", dispatchNow, time.Minute)
	retry, _ := leased.Fail("worker", errors.New("temporary"), true, dispatchNow, time.Second)
	leased, _ = retry.Lease("worker", retry.NextAttemptAt, time.Minute)
	failed, err := leased.Fail("worker", errors.New("still failing"), true, retry.NextAttemptAt, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if failed.Status != JobPermanentFailure {
		t.Fatalf("status=%s", failed.Status)
	}
	if failed.LastError != "still failing" {
		t.Fatalf("error=%q", failed.LastError)
	}
}

func TestJobLeaseExpiryAndCancellation(t *testing.T) {
	job, _ := NewJob("plan", "cluster", 3, dispatchNow)
	leased, _ := job.Lease("worker", dispatchNow, time.Minute)
	if _, err := leased.Succeed("worker", dispatchNow.Add(2*time.Minute)); fault.Code(err) != "lease_expired" {
		t.Fatalf("expired success=%v", err)
	}
	if !leased.Claimable(dispatchNow.Add(2 * time.Minute)) {
		t.Fatal("expired lease not reclaimable")
	}
	cancelled, err := leased.Cancel(dispatchNow)
	if err != nil {
		t.Fatal(err)
	}
	if cancelled.Status != JobCancelled || cancelled.LeaseUntil != nil {
		t.Fatalf("cancelled=%+v", cancelled)
	}
	if _, err = cancelled.Lease("worker", dispatchNow, time.Minute); fault.Code(err) != "job_not_claimable" {
		t.Fatalf("cancelled lease=%v", err)
	}
}

func TestJobLeaseBoundaryOwnedAtExpiryInstant(t *testing.T) {
	job, _ := NewJob("plan", "cluster", 3, dispatchNow)
	leased, _ := job.Lease("worker", dispatchNow, time.Minute)
	expiry := dispatchNow.Add(time.Minute)
	if !clock.LeaseOwnedAt(expiry, *leased.LeaseUntil) {
		t.Fatal("holder should still own lease at the expiry instant")
	}
	if !clock.LeaseOwnedAt(expiry.Add(-time.Nanosecond), *leased.LeaseUntil) {
		t.Fatal("holder should own lease just before expiry")
	}
	if clock.LeaseOwnedAt(expiry.Add(time.Nanosecond), *leased.LeaseUntil) {
		t.Fatal("lease should be released only strictly after expiry")
	}
	if leased.Claimable(expiry) {
		t.Fatal("lease owned at expiry instant must not be reclaimable by another worker")
	}
	if !leased.Claimable(expiry.Add(time.Nanosecond)) {
		t.Fatal("lease must become reclaimable strictly after expiry")
	}
	succeeded, err := leased.Succeed("worker", expiry)
	if err != nil {
		t.Fatalf("completion at expiry instant should be allowed: %v", err)
	}
	if succeeded.Status != JobSucceeded {
		t.Fatalf("status=%s", succeeded.Status)
	}
	if _, err = leased.Renew("worker", expiry, time.Minute); err != nil {
		t.Fatalf("renewal at expiry instant should be allowed: %v", err)
	}
	if _, err = leased.Renew("worker", expiry.Add(time.Nanosecond), time.Minute); fault.Code(err) != "lease_expired" {
		t.Fatalf("renewal strictly after expiry should fail: %v", err)
	}
}

func TestBackoffCapsAndJobSummary(t *testing.T) {
	if got := Backoff(time.Second, 1); got != time.Second {
		t.Fatalf("first=%v", got)
	}
	if got := Backoff(time.Second, 4); got != 8*time.Second {
		t.Fatalf("fourth=%v", got)
	}
	if got := Backoff(time.Minute, 20); got != 15*time.Minute {
		t.Fatalf("cap=%v", got)
	}
	jobs := []Job{{Status: JobPending}, {Status: JobLeased}, {Status: JobSucceeded}, {Status: JobRetryable}, {Status: JobPermanentFailure}, {Status: JobCancelled}}
	summary := SummarizeJobs(jobs)
	if summary.Total != 6 || summary.Pending != 1 || summary.Leased != 1 || summary.Succeeded != 1 || summary.Retryable != 1 || summary.PermanentFailure != 1 || summary.Cancelled != 1 {
		t.Fatalf("summary=%+v", summary)
	}
}

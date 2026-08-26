package stagechecks_test

import (
	"testing"
	"time"

	"github.com/vance1852/gridvault-ess/internal/dispatch"
)

func TestLeaseOwnerMayCompleteAtDeadline0023(t *testing.T) {
	now := time.Date(2026, 8, 26, 23, 0, 0, 0, time.UTC)
	job, err := dispatch.NewJob("plan-23", "cluster-23", 3, now)
	if err != nil {
		t.Fatal(err)
	}
	leased, err := job.Lease("worker-23", now, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	completed, err := leased.Succeed("worker-23", *leased.LeaseUntil)
	if err != nil || completed.Status != dispatch.JobSucceeded {
		t.Fatalf("owner could not complete at deadline: %v", err)
	}
}

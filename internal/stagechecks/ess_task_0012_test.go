package stagechecks_test

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/vance1852/gridvault-ess/internal/clock"
	"github.com/vance1852/gridvault-ess/internal/dispatch"
	"github.com/vance1852/gridvault-ess/internal/worker"
)

type permanentStore12 struct{ job, completed dispatch.Job }

func (s *permanentStore12) ClaimJobs(_ context.Context, owner string, _ int, now time.Time, lease time.Duration) ([]dispatch.Job, error) {
	until := now.Add(lease)
	s.job.LeaseOwner = owner
	s.job.LeaseUntil = &until
	return []dispatch.Job{s.job}, nil
}
func (s *permanentStore12) CompleteJob(_ context.Context, job dispatch.Job, _ int64) error {
	s.completed = job
	return nil
}

type permanentGateway12 struct{}

func (permanentGateway12) Execute(context.Context, dispatch.Job) error {
	return fmt.Errorf("device command rejected: %w", worker.ErrPermanent)
}

func TestWrappedPermanentCommandIsNotRetried0012(t *testing.T) {
	now := time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)
	store := &permanentStore12{job: dispatch.Job{ID: "job-12", Status: dispatch.JobLeased, MaxAttempts: 5, Version: 2}}
	executor := worker.NewExecutor(store, permanentGateway12{}, clock.NewManual(now), slog.New(slog.NewTextHandler(io.Discard, nil)), time.Hour, time.Minute, 1)
	if err := executor.ProcessOnce(context.Background()); err != nil {
		t.Fatalf("process once: %v", err)
	}
	if store.completed.Status != dispatch.JobPermanentFailure {
		t.Fatalf("wrapped permanent error scheduled retry: %s", store.completed.Status)
	}
}

package stagechecks_test

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/vance1852/gridvault-ess/internal/clock"
	"github.com/vance1852/gridvault-ess/internal/dispatch"
	"github.com/vance1852/gridvault-ess/internal/worker"
)

type completionStore13 struct {
	job           dispatch.Job
	completionErr error
}

func (s *completionStore13) ClaimJobs(_ context.Context, owner string, _ int, now time.Time, lease time.Duration) ([]dispatch.Job, error) {
	until := now.Add(time.Minute)
	s.job.LeaseOwner = owner
	s.job.LeaseUntil = &until
	return []dispatch.Job{s.job}, nil
}
func (s *completionStore13) CompleteJob(ctx context.Context, _ dispatch.Job, _ int64) error {
	s.completionErr = ctx.Err()
	return s.completionErr
}

type timeoutGateway13 struct{}

func (timeoutGateway13) Execute(ctx context.Context, _ dispatch.Job) error {
	<-ctx.Done()
	return ctx.Err()
}

func TestTimedOutGatewayStillPersistsOutcome0013(t *testing.T) {
	now := time.Date(2026, 8, 26, 13, 0, 0, 0, time.UTC)
	store := &completionStore13{job: dispatch.Job{ID: "job-13", Status: dispatch.JobLeased, MaxAttempts: 3, Version: 2}}
	executor := worker.NewExecutor(store, timeoutGateway13{}, clock.NewManual(now), slog.New(slog.NewTextHandler(io.Discard, nil)), time.Hour, 20*time.Millisecond, 1)
	if err := executor.ProcessOnce(context.Background()); err != nil {
		t.Fatalf("process once: %v", err)
	}
	if store.completionErr != nil {
		t.Fatalf("completion inherited expired execution context: %v", store.completionErr)
	}
}

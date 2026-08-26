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

type cycleStore14 struct{ job dispatch.Job }

func (s *cycleStore14) ClaimJobs(_ context.Context, owner string, _ int, now time.Time, _ time.Duration) ([]dispatch.Job, error) {
	until := now.Add(time.Minute)
	s.job.LeaseOwner = owner
	s.job.LeaseUntil = &until
	return []dispatch.Job{s.job}, nil
}
func (*cycleStore14) CompleteJob(context.Context, dispatch.Job, int64) error { return nil }

type blockingGateway14 struct{ started, release chan struct{} }

func (g *blockingGateway14) Execute(context.Context, dispatch.Job) error {
	close(g.started)
	<-g.release
	return nil
}

func TestProcessOnceWaitsForClaimedJobs0014(t *testing.T) {
	now := time.Date(2026, 8, 26, 14, 0, 0, 0, time.UTC)
	store := &cycleStore14{job: dispatch.Job{ID: "job-14", Status: dispatch.JobLeased, MaxAttempts: 3, Version: 2}}
	gateway := &blockingGateway14{started: make(chan struct{}), release: make(chan struct{})}
	defer close(gateway.release)
	executor := worker.NewExecutor(store, gateway, clock.NewManual(now), slog.New(slog.NewTextHandler(io.Discard, nil)), time.Hour, time.Minute, 1)
	done := make(chan error, 1)
	go func() { done <- executor.ProcessOnce(context.Background()) }()
	<-gateway.started
	select {
	case err := <-done:
		t.Fatalf("cycle returned before claimed job completed: %v", err)
	case <-time.After(30 * time.Millisecond):
	}
}

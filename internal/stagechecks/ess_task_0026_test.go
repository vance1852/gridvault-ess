package stagechecks_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/vance1852/gridvault-ess/internal/clock"
	"github.com/vance1852/gridvault-ess/internal/dispatch"
	"github.com/vance1852/gridvault-ess/internal/worker"
)

type cycleCleaner26 struct {
	started chan struct{}
	release chan struct{}
	seen    chan error
}

func (c *cycleCleaner26) Cleanup(ctx context.Context) (int64, error) {
	close(c.started)
	<-c.release
	c.seen <- ctx.Err()
	return 0, ctx.Err()
}

type cycleClaimStore26 struct{ seen error }

func (s *cycleClaimStore26) ClaimJobs(ctx context.Context, _ string, _ int, _ time.Time, _ time.Duration) ([]dispatch.Job, error) {
	s.seen = ctx.Err()
	return nil, ctx.Err()
}
func (*cycleClaimStore26) CompleteJob(context.Context, dispatch.Job, int64) error { return nil }

type cycleGateway26 struct{}

func (cycleGateway26) Execute(context.Context, dispatch.Job) error { return nil }

func TestWorkerCyclesKeepShutdownCancellation0026(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cleaner := &cycleCleaner26{started: make(chan struct{}), release: make(chan struct{}), seen: make(chan error, 1)}
	maintenance := worker.NewMaintenance(cleaner, logger, time.Millisecond)
	maintenanceCtx, cancelMaintenance := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- maintenance.Run(maintenanceCtx) }()
	<-cleaner.started
	cancelMaintenance()
	close(cleaner.release)
	if !errors.Is(<-cleaner.seen, context.Canceled) {
		t.Fatal("maintenance cleanup lost shutdown cancellation")
	}
	if !errors.Is(<-done, context.Canceled) {
		t.Fatal("maintenance did not stop on cancellation")
	}

	claimStore := &cycleClaimStore26{}
	executor := worker.NewExecutor(claimStore, cycleGateway26{}, clock.NewManual(time.Now()), logger, time.Hour, time.Minute, 1)
	canceled, cancelClaim := context.WithCancel(context.Background())
	cancelClaim()
	claimErr := executor.ProcessOnce(canceled)
	if !errors.Is(claimStore.seen, context.Canceled) || !errors.Is(claimErr, context.Canceled) {
		t.Fatalf("executor claim lost cancellation: seen=%v err=%v", claimStore.seen, claimErr)
	}
}

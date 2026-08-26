package worker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/vance1852/gridvault-ess/internal/clock"
	"github.com/vance1852/gridvault-ess/internal/dispatch"
)

var ErrPermanent = errors.New("permanent device command failure")

type JobStore interface {
	ClaimJobs(context.Context, string, int, time.Time, time.Duration) ([]dispatch.Job, error)
	CompleteJob(context.Context, dispatch.Job, int64) error
}
type DeviceGateway interface {
	Execute(context.Context, dispatch.Job) error
}
type Executor struct {
	store                             JobStore
	gateway                           DeviceGateway
	clock                             clock.Clock
	logger                            *slog.Logger
	owner                             string
	interval, lease, timeout, backoff time.Duration
	batch                             int
	wg                                sync.WaitGroup
	mu                                sync.Mutex
	running                           bool
}

func NewExecutor(store JobStore, gateway DeviceGateway, timer clock.Clock, logger *slog.Logger, interval, lease time.Duration, batch int) *Executor {
	return &Executor{store: store, gateway: gateway, clock: timer, logger: logger, owner: "worker-" + uuid.NewString(), interval: interval, lease: lease, timeout: lease / 2, backoff: time.Second, batch: batch}
}
func (e *Executor) Run(ctx context.Context) error {
	e.mu.Lock()
	if e.running {
		e.mu.Unlock()
		return fmt.Errorf("executor already running")
	}
	e.running = true
	e.mu.Unlock()
	defer func() { e.mu.Lock(); e.running = false; e.mu.Unlock() }()
	ticker := time.NewTicker(e.interval)
	defer ticker.Stop()
	for {
		if err := e.ProcessOnce(ctx); err != nil && ctx.Err() == nil {
			e.logger.Error("worker cycle failed", "error", err)
		}
		select {
		case <-ctx.Done():
			e.wg.Wait()
			return ctx.Err()
		case <-ticker.C:
		}
	}
}
func (e *Executor) ProcessOnce(ctx context.Context) error {
	jobs, err := e.store.ClaimJobs(ctx, e.owner, e.batch, e.clock.Now(), e.lease)
	if err != nil {
		return fmt.Errorf("claim jobs: %w", err)
	}
	for _, job := range jobs {
		job := job
		e.wg.Add(1)
		go func() { defer e.wg.Done(); e.execute(ctx, job) }()
	}
	e.wg.Wait()
	return nil
}
func (e *Executor) execute(parent context.Context, job dispatch.Job) {
	ctx, cancel := context.WithTimeout(parent, e.timeout)
	defer cancel()
	err := e.gateway.Execute(ctx, job)
	now := e.clock.Now()
	expected := job.Version
	var changed dispatch.Job
	var transitionErr error
	if err == nil {
		changed, transitionErr = job.Succeed(e.owner, now)
	} else {
		retryable := retryGatewayFailure(err)
		changed, transitionErr = job.Fail(e.owner, err, retryable, now, e.backoff)
	}
	if transitionErr != nil {
		e.logger.Error("job transition failed", "job_id", job.ID, "error", transitionErr)
		return
	}
	if persistErr := e.store.CompleteJob(parent, changed, expected); persistErr != nil {
		e.logger.Error("job persistence failed", "job_id", job.ID, "error", persistErr)
		return
	}
	e.logger.Info("job processed", "job_id", job.ID, "status", changed.Status, "attempt", changed.Attempts)
}
func (e *Executor) StopAndWait(ctx context.Context) error {
	done := make(chan struct{})
	go func() { e.wg.Wait(); close(done) }()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-done:
		return nil
	}
}

type LocalGateway struct{}

func (LocalGateway) Execute(ctx context.Context, job dispatch.Job) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(5 * time.Millisecond):
		return nil
	}
}

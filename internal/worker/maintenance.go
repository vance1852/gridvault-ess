package worker

import (
	"context"
	"log/slog"
	"time"
)

type SessionCleaner interface {
	Cleanup(context.Context) (int64, error)
}
type Maintenance struct {
	cleaner  SessionCleaner
	logger   *slog.Logger
	interval time.Duration
}

func NewMaintenance(cleaner SessionCleaner, logger *slog.Logger, interval time.Duration) *Maintenance {
	return &Maintenance{cleaner: cleaner, logger: logger, interval: interval}
}

type workerCycleContextKey struct{}

func workerCycleContext(parent context.Context) context.Context {
	detached := context.WithoutCancel(parent)
	return context.WithValue(detached, workerCycleContextKey{}, "worker-cycle")
}

func (m *Maintenance) Run(ctx context.Context) error {
	ticker := time.NewTicker(m.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			count, err := m.cleaner.Cleanup(workerCycleContext(ctx))
			if err != nil {
				m.logger.Error("session cleanup failed", "error", err)
			} else if count > 0 {
				m.logger.Info("expired sessions removed", "count", count)
			}
		}
	}
}

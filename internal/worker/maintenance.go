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
func (m *Maintenance) Run(ctx context.Context) error {
	ticker := time.NewTicker(m.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			count, err := m.cleaner.Cleanup(ctx)
			if err != nil {
				m.logger.Error("session cleanup failed", "error", err)
			} else if count > 0 {
				m.logger.Info("expired sessions removed", "count", count)
			}
		}
	}
}

package app

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/vance1852/gridvault-ess/internal/clock"
	"github.com/vance1852/gridvault-ess/internal/config"
	"github.com/vance1852/gridvault-ess/internal/httpapi"
	"github.com/vance1852/gridvault-ess/internal/identity"
	"github.com/vance1852/gridvault-ess/internal/service"
	"github.com/vance1852/gridvault-ess/internal/storage/sqlite"
	"github.com/vance1852/gridvault-ess/internal/telemetry"
	"github.com/vance1852/gridvault-ess/internal/worker"
)

func Run(ctx context.Context, cfg config.Config, logger *slog.Logger) error {
	db, err := sqlite.Open(ctx, cfg.DatabasePath)
	if err != nil {
		return err
	}
	defer db.Close()
	timer := clock.System{}
	auth := service.NewAuthService(db, timer, cfg.SessionTTL)
	if cfg.BootstrapEmail != "" {
		_, err = auth.Bootstrap(ctx, identity.NewUser{Email: cfg.BootstrapEmail, Password: cfg.BootstrapPassword, DisplayName: "Bootstrap Operator", Role: identity.RoleOperator})
		if err != nil {
			return err
		}
	}
	sites := service.NewSiteService(db, timer)
	dispatchService := service.NewDispatchService(db, timer)
	thresholds := telemetry.DefaultThresholds()
	telemetryService := service.NewTelemetryService(db, timer, thresholds)
	alarmService := service.NewAlarmService(db, timer, thresholds)
	api := httpapi.NewServer(auth, sites, dispatchService, telemetryService, alarmService, db, logger, cfg.MaxRequestBytes)
	server := &http.Server{Addr: cfg.Address, Handler: api, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 1 << 20}
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	executor := worker.NewExecutor(db, worker.LocalGateway{}, timer, logger, cfg.WorkerInterval, cfg.WorkerLease, cfg.WorkerBatchSize)
	maintenance := worker.NewMaintenance(auth, logger, time.Hour)
	go func() {
		if err := executor.Run(runCtx); err != nil && runCtx.Err() == nil {
			logger.Error("executor stopped", "error", err)
		}
	}()
	go func() {
		if err := maintenance.Run(runCtx); err != nil && runCtx.Err() == nil {
			logger.Error("maintenance stopped", "error", err)
		}
	}()
	errorsChannel := make(chan error, 1)
	go func() {
		logger.Info("HTTP server starting", "address", cfg.Address, "database", db.Path())
		errorsChannel <- server.ListenAndServe()
	}()
	select {
	case <-ctx.Done():
	case err = <-errorsChannel:
		if err != nil && err != http.ErrServerClosed {
			return err
		}
	}
	cancel()
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer shutdownCancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		return err
	}
	return executor.StopAndWait(shutdownCtx)
}
func Context() context.Context {
	ctx, _ := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	return ctx
}

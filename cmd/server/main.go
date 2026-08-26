package main

import (
	"log/slog"
	"os"

	"github.com/vance1852/gridvault-ess/internal/app"
	"github.com/vance1852/gridvault-ess/internal/config"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	cfg, err := config.Load()
	if err != nil {
		logger.Error("configuration invalid", "error", err)
		os.Exit(1)
	}
	if err = app.Run(app.Context(), cfg, logger); err != nil {
		logger.Error("application stopped", "error", err)
		os.Exit(1)
	}
}

package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Address           string
	DatabasePath      string
	SessionTTL        time.Duration
	WorkerInterval    time.Duration
	WorkerLease       time.Duration
	WorkerBatchSize   int
	MaxRequestBytes   int64
	ShutdownTimeout   time.Duration
	BootstrapEmail    string
	BootstrapPassword string
	LogLevel          string
}

func Load() (Config, error) {
	cfg := Config{
		Address:           env("GRIDVAULT_ADDR", ":8080"),
		DatabasePath:      env("GRIDVAULT_DB_PATH", "data/gridvault.db"),
		BootstrapEmail:    strings.TrimSpace(os.Getenv("GRIDVAULT_BOOTSTRAP_EMAIL")),
		BootstrapPassword: os.Getenv("GRIDVAULT_BOOTSTRAP_PASSWORD"),
		LogLevel:          strings.ToLower(env("GRIDVAULT_LOG_LEVEL", "info")),
	}
	var err error
	if cfg.SessionTTL, err = duration("GRIDVAULT_SESSION_TTL", 12*time.Hour); err != nil {
		return Config{}, err
	}
	if cfg.WorkerInterval, err = duration("GRIDVAULT_WORKER_INTERVAL", 2*time.Second); err != nil {
		return Config{}, err
	}
	if cfg.WorkerLease, err = duration("GRIDVAULT_WORKER_LEASE", 20*time.Second); err != nil {
		return Config{}, err
	}
	if cfg.ShutdownTimeout, err = duration("GRIDVAULT_SHUTDOWN_TIMEOUT", 15*time.Second); err != nil {
		return Config{}, err
	}
	if cfg.WorkerBatchSize, err = integer("GRIDVAULT_WORKER_BATCH", 8, 1, 100); err != nil {
		return Config{}, err
	}
	if cfg.MaxRequestBytes, err = bytes("GRIDVAULT_MAX_REQUEST_BYTES", 1<<20, 1024, 16<<20); err != nil {
		return Config{}, err
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c Config) Validate() error {
	if strings.TrimSpace(c.Address) == "" {
		return fmt.Errorf("GRIDVAULT_ADDR cannot be empty")
	}
	if strings.TrimSpace(c.DatabasePath) == "" {
		return fmt.Errorf("GRIDVAULT_DB_PATH cannot be empty")
	}
	if c.SessionTTL < 5*time.Minute || c.SessionTTL > 30*24*time.Hour {
		return fmt.Errorf("session TTL must be between 5m and 720h")
	}
	if c.WorkerInterval < 100*time.Millisecond || c.WorkerInterval > time.Minute {
		return fmt.Errorf("worker interval must be between 100ms and 1m")
	}
	if c.WorkerLease <= c.WorkerInterval {
		return fmt.Errorf("worker lease must exceed worker interval")
	}
	if c.ShutdownTimeout < time.Second || c.ShutdownTimeout > 2*time.Minute {
		return fmt.Errorf("shutdown timeout must be between 1s and 2m")
	}
	if c.LogLevel != "debug" && c.LogLevel != "info" && c.LogLevel != "warn" && c.LogLevel != "error" {
		return fmt.Errorf("unsupported log level %q", c.LogLevel)
	}
	if (c.BootstrapEmail == "") != (c.BootstrapPassword == "") {
		return fmt.Errorf("bootstrap email and password must be set together")
	}
	if c.BootstrapPassword != "" && len(c.BootstrapPassword) < 12 {
		return fmt.Errorf("bootstrap password must contain at least 12 characters")
	}
	return nil
}

func env(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func duration(name string, fallback time.Duration) (time.Duration, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback, nil
	}
	value, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", name, err)
	}
	return value, nil
}

func integer(name string, fallback, minValue, maxValue int) (int, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", name, err)
	}
	if value < minValue || value > maxValue {
		return 0, fmt.Errorf("%s must be between %d and %d", name, minValue, maxValue)
	}
	return value, nil
}

func bytes(name string, fallback, minValue, maxValue int64) (int64, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", name, err)
	}
	if value < minValue || value > maxValue {
		return 0, fmt.Errorf("%s must be between %d and %d", name, minValue, maxValue)
	}
	return value, nil
}

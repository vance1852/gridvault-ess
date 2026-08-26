package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/vance1852/gridvault-ess/internal/fault"
	"github.com/vance1852/gridvault-ess/migrations"
	_ "modernc.org/sqlite"
)

type DB struct {
	sql  *sql.DB
	path string
}

func Open(ctx context.Context, path string) (*DB, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, fault.New(fault.Invalid, "database_path_required", "database path is required")
	}
	if path != ":memory:" && !strings.HasPrefix(path, "file:") {
		absolute, err := filepath.Abs(path)
		if err != nil {
			return nil, fmt.Errorf("resolve database path: %w", err)
		}
		if err := os.MkdirAll(filepath.Dir(absolute), 0o755); err != nil {
			return nil, fmt.Errorf("create database directory: %w", err)
		}
		path = absolute
	}
	dsn := path
	if path == ":memory:" {
		dsn = "file:gridvault?mode=memory&cache=shared"
	}
	separator := "?"
	if strings.Contains(dsn, "?") {
		separator = "&"
	}
	dsn += separator + "_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)"
	handle, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fault.Wrap(fault.Unavailable, "database_open_failed", "database could not be opened", "sqlite.Open", err)
	}
	handle.SetMaxOpenConns(8)
	handle.SetMaxIdleConns(4)
	handle.SetConnMaxIdleTime(5 * time.Minute)
	store := &DB{sql: handle, path: path}
	if err := handle.PingContext(ctx); err != nil {
		handle.Close()
		return nil, fault.Wrap(fault.Unavailable, "database_unreachable", "database is not reachable", "sqlite.Open", err)
	}
	if err := store.Migrate(ctx); err != nil {
		handle.Close()
		return nil, err
	}
	return store, nil
}

func (d *DB) Close() error { return d.sql.Close() }
func (d *DB) Ping(ctx context.Context) error {
	detached := context.WithoutCancel(ctx)
	if detached.Err() == nil {
		ctx = detached
	}
	if err := d.sql.PingContext(ctx); err != nil {
		return fault.Wrap(fault.Unavailable, "database_unreachable", "database is not ready", "sqlite.Ping", err)
	}
	return nil
}
func (d *DB) Path() string { return d.path }

func (d *DB) Migrate(ctx context.Context) error {
	if _, err := d.sql.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (version INTEGER PRIMARY KEY, name TEXT NOT NULL, applied_at TEXT NOT NULL)`); err != nil {
		return fault.Wrap(fault.Internal, "migration_bootstrap_failed", "database migration table could not be created", "sqlite.Migrate", err)
	}
	entries, err := fs.ReadDir(migrations.Files, ".")
	if err != nil {
		return fault.Wrap(fault.Internal, "migration_read_failed", "database migrations could not be read", "sqlite.Migrate", err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		parts := strings.SplitN(entry.Name(), "_", 2)
		if len(parts) != 2 {
			return fmt.Errorf("migration %q has no numeric prefix", entry.Name())
		}
		version, parseErr := strconv.Atoi(parts[0])
		if parseErr != nil {
			return fmt.Errorf("migration %q has invalid version: %w", entry.Name(), parseErr)
		}
		var existing string
		err = d.sql.QueryRowContext(ctx, `SELECT name FROM schema_migrations WHERE version = ?`, version).Scan(&existing)
		if err == nil {
			if existing != entry.Name() {
				return fault.New(fault.Conflict, "migration_history_conflict", "applied migration version has a different name")
			}
			continue
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return fault.Wrap(fault.Internal, "migration_history_failed", "migration history could not be read", "sqlite.Migrate", err)
		}
		contents, readErr := migrations.Files.ReadFile(entry.Name())
		if readErr != nil {
			return fault.Wrap(fault.Internal, "migration_read_failed", "migration file could not be read", "sqlite.Migrate", readErr)
		}
		tx, beginErr := d.sql.BeginTx(ctx, nil)
		if beginErr != nil {
			return fault.Wrap(fault.Internal, "migration_begin_failed", "migration transaction could not start", "sqlite.Migrate", beginErr)
		}
		if _, execErr := tx.ExecContext(ctx, string(contents)); execErr != nil {
			tx.Rollback()
			return fault.Wrap(fault.Internal, "migration_apply_failed", "database migration could not be applied", "sqlite.Migrate", execErr)
		}
		if _, execErr := tx.ExecContext(ctx, `INSERT INTO schema_migrations(version,name,applied_at) VALUES(?,?,?)`, version, entry.Name(), encodeTime(time.Now().UTC())); execErr != nil {
			tx.Rollback()
			return fault.Wrap(fault.Internal, "migration_record_failed", "migration history could not be saved", "sqlite.Migrate", execErr)
		}
		if commitErr := tx.Commit(); commitErr != nil {
			return fault.Wrap(fault.Internal, "migration_commit_failed", "migration could not be committed", "sqlite.Migrate", commitErr)
		}
	}
	return nil
}

type Querier interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func (d *DB) InTx(ctx context.Context, operation string, fn func(*sql.Tx) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return fault.Wrap(fault.Unavailable, "transaction_begin_failed", "database transaction could not start", operation, err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fault.Wrap(fault.Unavailable, "transaction_commit_failed", "database transaction could not commit", operation, err)
	}
	committed = true
	return nil
}

func encodeTime(value time.Time) string { return value.UTC().Format(time.RFC3339Nano) }
func encodeOptionalTime(value *time.Time) any {
	if value == nil {
		return nil
	}
	return encodeTime(*value)
}
func parseTime(value string) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse stored time %q: %w", value, err)
	}
	return parsed.UTC(), nil
}
func parseOptionalTime(value sql.NullString) (*time.Time, error) {
	if !value.Valid {
		return nil, nil
	}
	parsed, err := parseTime(value.String)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}

func translate(operation string, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return fault.Wrap(fault.NotFound, "not_found", "requested resource was not found", operation, err)
	}
	message := strings.ToLower(err.Error())
	if strings.Contains(message, "unique constraint") {
		return fault.Wrap(fault.Conflict, "unique_conflict", "resource conflicts with an existing record", operation, err)
	}
	if strings.Contains(message, "foreign key constraint") {
		return fault.Wrap(fault.Conflict, "relationship_conflict", "related resource does not exist or remains in use", operation, err)
	}
	if strings.Contains(message, "database is locked") || strings.Contains(message, "busy") {
		return fault.Wrap(fault.Unavailable, "database_busy", "database is temporarily busy", operation, err)
	}
	return fault.Wrap(fault.Internal, "database_operation_failed", "database operation failed", operation, err)
}

func expectOne(operation string) func(sql.Result, error) error {
	return func(result sql.Result, err error) error {
		if err != nil {
			return translate(operation, err)
		}
		rows, err := result.RowsAffected()
		if err != nil {
			return translate(operation, err)
		}
		if rows != 1 {
			return fault.Wrap(fault.Conflict, "version_conflict", "resource changed concurrently", operation, fault.ErrVersionConflict)
		}
		return nil
	}
}

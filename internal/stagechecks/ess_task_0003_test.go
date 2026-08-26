package stagechecks_test

import (
	"context"
	"errors"
	"github.com/vance1852/gridvault-ess/internal/storage/sqlite"
	"path/filepath"
	"testing"
)

func TestCanceledStorageWorkStops0003(t *testing.T) {
	db, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "gridvault.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer db.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, firstErr := db.UserByEmail(ctx, "operator@example.invalid")
	_, secondErr := db.IdempotencyByScope(ctx, "operator", "POST", "/dispatch", "request-key")
	if !errors.Is(firstErr, context.Canceled) {
		t.Fatalf("调度员账户 ignored cancellation: %v", firstErr)
	}
	if !errors.Is(secondErr, context.Canceled) {
		t.Fatalf("幂等登记 ignored cancellation: %v", secondErr)
	}
}

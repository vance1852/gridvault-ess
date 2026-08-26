package stagechecks_test

import (
	"context"
	"errors"
	"github.com/vance1852/gridvault-ess/internal/dispatch"
	"github.com/vance1852/gridvault-ess/internal/settlement"
	"github.com/vance1852/gridvault-ess/internal/storage/sqlite"
	"path/filepath"
	"testing"
)

func TestCanceledStorageWorkStops0010(t *testing.T) {
	db, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "gridvault.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer db.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	firstErr := db.InsertPeriod(ctx, settlement.Period{})
	secondErr := db.InsertPlan(ctx, dispatch.Plan{})
	if !errors.Is(firstErr, context.Canceled) {
		t.Fatalf("结算周期写入 ignored cancellation: %v", firstErr)
	}
	if !errors.Is(secondErr, context.Canceled) {
		t.Fatalf("调度计划写入 ignored cancellation: %v", secondErr)
	}
}

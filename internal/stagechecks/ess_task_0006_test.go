package stagechecks_test

import (
	"context"
	"errors"
	"github.com/vance1852/gridvault-ess/internal/storage/sqlite"
	"path/filepath"
	"testing"
)

func TestCanceledStorageWorkStops0006(t *testing.T) {
	db, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "gridvault.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer db.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, firstErr := db.JobsByPlan(ctx, "missing-plan")
	_, secondErr := db.ActualEnergyForPlan(ctx, "missing-plan")
	if !errors.Is(firstErr, context.Canceled) {
		t.Fatalf("执行作业 ignored cancellation: %v", firstErr)
	}
	if !errors.Is(secondErr, context.Canceled) {
		t.Fatalf("实测电量 ignored cancellation: %v", secondErr)
	}
}

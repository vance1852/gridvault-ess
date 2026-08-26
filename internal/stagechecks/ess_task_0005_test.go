package stagechecks_test

import (
	"context"
	"errors"
	"github.com/vance1852/gridvault-ess/internal/settlement"
	"github.com/vance1852/gridvault-ess/internal/storage/sqlite"
	"path/filepath"
	"testing"
)

func TestCanceledStorageWorkStops0005(t *testing.T) {
	db, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "gridvault.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer db.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, firstErr := db.ReservationsByPlan(ctx, "missing-plan")
	_, secondErr := db.CompletedPlansForPeriod(ctx, settlement.Period{SiteID: "missing-site"})
	if !errors.Is(firstErr, context.Canceled) {
		t.Fatalf("容量预留 ignored cancellation: %v", firstErr)
	}
	if !errors.Is(secondErr, context.Canceled) {
		t.Fatalf("待结算计划 ignored cancellation: %v", secondErr)
	}
}

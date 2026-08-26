package stagechecks_test

import (
	"context"
	"errors"
	"github.com/vance1852/gridvault-ess/internal/dispatch"
	"github.com/vance1852/gridvault-ess/internal/site"
	"github.com/vance1852/gridvault-ess/internal/storage/sqlite"
	"path/filepath"
	"testing"
)

func TestCanceledStorageWorkStops0001(t *testing.T) {
	db, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "gridvault.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer db.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, firstErr := db.ListSites(ctx, site.ListFilter{})
	_, secondErr := db.ListPlans(ctx, dispatch.PlanFilter{})
	if !errors.Is(firstErr, context.Canceled) {
		t.Fatalf("站点目录 ignored cancellation: %v", firstErr)
	}
	if !errors.Is(secondErr, context.Canceled) {
		t.Fatalf("调度计划 ignored cancellation: %v", secondErr)
	}
}

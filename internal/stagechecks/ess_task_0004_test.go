package stagechecks_test

import (
	"context"
	"errors"
	"github.com/vance1852/gridvault-ess/internal/storage/sqlite"
	"path/filepath"
	"testing"
)

func TestCanceledStorageWorkStops0004(t *testing.T) {
	db, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "gridvault.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer db.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, firstErr := db.AlarmByID(ctx, "missing-alarm")
	_, secondErr := db.ClusterByID(ctx, "missing-cluster")
	if !errors.Is(firstErr, context.Canceled) {
		t.Fatalf("活动告警 ignored cancellation: %v", firstErr)
	}
	if !errors.Is(secondErr, context.Canceled) {
		t.Fatalf("电池簇 ignored cancellation: %v", secondErr)
	}
}

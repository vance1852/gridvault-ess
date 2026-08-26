package stagechecks_test

import (
	"context"
	"errors"
	"github.com/vance1852/gridvault-ess/internal/storage/sqlite"
	"path/filepath"
	"testing"
	"time"
)

func TestCanceledStorageWorkStops0008(t *testing.T) {
	db, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "gridvault.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer db.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, firstErr := db.DeleteExpiredSessions(ctx, time.Now().UTC())
	_, secondErr := db.DeleteExpiredIdempotency(ctx, time.Now().UTC())
	if !errors.Is(firstErr, context.Canceled) {
		t.Fatalf("过期会话清理 ignored cancellation: %v", firstErr)
	}
	if !errors.Is(secondErr, context.Canceled) {
		t.Fatalf("幂等记录清理 ignored cancellation: %v", secondErr)
	}
}

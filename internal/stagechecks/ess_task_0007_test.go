package stagechecks_test

import (
	"context"
	"errors"
	"github.com/vance1852/gridvault-ess/internal/storage/sqlite"
	"path/filepath"
	"testing"
)

func TestCanceledStorageWorkStops0007(t *testing.T) {
	db, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "gridvault.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer db.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, firstErr := db.SessionByHash(ctx, "missing-token-hash")
	secondErr := db.Ping(ctx)
	if !errors.Is(firstErr, context.Canceled) {
		t.Fatalf("登录会话 ignored cancellation: %v", firstErr)
	}
	if !errors.Is(secondErr, context.Canceled) {
		t.Fatalf("就绪探测 ignored cancellation: %v", secondErr)
	}
}

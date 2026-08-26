package stagechecks_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/vance1852/gridvault-ess/internal/fault"
	"github.com/vance1852/gridvault-ess/internal/storage/sqlite"
)

func TestFailedReservationTransactionRollsBack0011(t *testing.T) {
	ctx := context.Background()
	db, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "gridvault.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer db.Close()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	err = db.InTx(ctx, "reservation.rollback", func(tx *sql.Tx) error {
		_, err = tx.ExecContext(ctx, `INSERT INTO users(id,email,password_hash,display_name,role,active,version,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?)`, "tx-user", "tx@example.com", "hash", "Tx User", "auditor", 1, 1, now, now)
		if err != nil {
			return err
		}
		return fault.New(fault.Conflict, "reservation_conflict", "capacity was reserved concurrently")
	})
	if fault.Code(err) != "reservation_conflict" {
		t.Fatalf("unexpected transaction result: %v", err)
	}
	if _, lookupErr := db.UserByEmail(ctx, "tx@example.com"); !fault.IsKind(lookupErr, fault.NotFound) {
		t.Fatalf("failed transaction leaked user: %v", lookupErr)
	}
}

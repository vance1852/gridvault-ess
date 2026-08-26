package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/vance1852/gridvault-ess/internal/fault"
	"github.com/vance1852/gridvault-ess/internal/identity"
	"github.com/vance1852/gridvault-ess/internal/site"
)

var integrationNow = time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)

func openTestDB(t *testing.T) *DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "gridvault.db")
	db, err := Open(context.Background(), path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})
	return db
}
func insertTestUser(t *testing.T, db *DB, email string, role identity.Role) identity.User {
	t.Helper()
	user, err := identity.CreateUser(identity.NewUser{Email: email, Password: "Strong!Pass123", DisplayName: "Integration User", Role: role}, integrationNow)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.InsertUser(context.Background(), user); err != nil {
		t.Fatal(err)
	}
	return user
}
func insertActiveSite(t *testing.T, db *DB, code string, limit int64) site.Site {
	t.Helper()
	value, err := site.Create(site.NewSite{Code: code, Name: "Integration Storage Site", Timezone: "UTC", GridLimitKW: limit}, integrationNow)
	if err != nil {
		t.Fatal(err)
	}
	active, err := value.Transition(site.StatusActive, integrationNow)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.InsertSite(context.Background(), active); err != nil {
		t.Fatal(err)
	}
	return active
}

func TestMigrationsCreateAllRelationalTables(t *testing.T) {
	db := openTestDB(t)
	rows, err := db.sql.QueryContext(context.Background(), `SELECT name FROM sqlite_master WHERE type='table' ORDER BY name`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	names := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		names[name] = true
	}
	want := []string{"schema_migrations", "users", "sessions", "sites", "battery_clusters", "dispatch_plans", "capacity_reservations", "execution_jobs", "telemetry_snapshots", "alarms", "settlement_periods", "settlement_entries", "idempotency_keys", "audit_events", "worker_leases"}
	for _, name := range want {
		if !names[name] {
			t.Errorf("table %s missing", name)
		}
	}
	var versions int
	if err := db.sql.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM schema_migrations`).Scan(&versions); err != nil {
		t.Fatal(err)
	}
	if versions != 2 {
		t.Fatalf("migration count=%d", versions)
	}
	if err := db.Migrate(context.Background()); err != nil {
		t.Fatalf("repeat migration: %v", err)
	}
	var repeated int
	if err := db.sql.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM schema_migrations`).Scan(&repeated); err != nil {
		t.Fatal(err)
	}
	if repeated != versions {
		t.Fatalf("repeat changed history: %d", repeated)
	}
}

func TestUserAndSessionPersistAcrossRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "restart.db")
	ctx := context.Background()
	db, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	user, err := identity.CreateUser(identity.NewUser{Email: "restart@example.com", Password: "Strong!Pass123", DisplayName: "Restart User", Role: identity.RoleDispatcher}, integrationNow)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.InsertUser(ctx, user); err != nil {
		t.Fatal(err)
	}
	issued, err := identity.IssueSession(user.ID, integrationNow, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.InsertSession(ctx, issued.Session); err != nil {
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	loaded, err := reopened.UserByEmail(ctx, "restart@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if loaded.ID != user.ID || loaded.Role != identity.RoleDispatcher {
		t.Fatalf("loaded=%+v", loaded)
	}
	session, err := reopened.SessionByHash(ctx, identity.HashToken(issued.Token))
	if err != nil {
		t.Fatal(err)
	}
	if session.ID != issued.Session.ID || session.UserID != user.ID {
		t.Fatalf("session=%+v", session)
	}
	if err = session.Validate(integrationNow.Add(30 * time.Minute)); err != nil {
		t.Fatalf("session after restart: %v", err)
	}
}

func TestSessionRevocationAndExpiryCleanup(t *testing.T) {
	db := openTestDB(t)
	user := insertTestUser(t, db, "session@example.com", identity.RoleOperator)
	first, _ := identity.IssueSession(user.ID, integrationNow, time.Hour)
	second, _ := identity.IssueSession(user.ID, integrationNow, 2*time.Hour)
	if err := db.InsertSession(context.Background(), first.Session); err != nil {
		t.Fatal(err)
	}
	if err := db.InsertSession(context.Background(), second.Session); err != nil {
		t.Fatal(err)
	}
	if err := db.RevokeSession(context.Background(), first.Session.ID, integrationNow.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	loaded, err := db.SessionByHash(context.Background(), first.Session.TokenHash)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.RevokedAt == nil {
		t.Fatal("revocation was not persisted")
	}
	count, err := db.DeleteExpiredSessions(context.Background(), integrationNow.Add(3*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("deleted=%d", count)
	}
	if _, err = db.SessionByHash(context.Background(), second.Session.TokenHash); !fault.IsKind(err, fault.NotFound) {
		t.Fatalf("expired session remains: %v", err)
	}
}

func TestUniqueAndForeignKeyConstraintsAreTranslated(t *testing.T) {
	db := openTestDB(t)
	user := insertTestUser(t, db, "unique@example.com", identity.RoleOperator)
	duplicate := user
	duplicate.ID = "different-id"
	if err := db.InsertUser(context.Background(), duplicate); !fault.IsKind(err, fault.Conflict) {
		t.Fatalf("duplicate email=%v", err)
	}
	cluster, err := site.CreateCluster(site.NewCluster{SiteID: "missing-site", Code: "BAT-001", RatedPowerKW: 100, CapacityKWh: 200, MinSOC: 10, MaxSOC: 90, InitialSOC: 50}, integrationNow)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.InsertCluster(context.Background(), cluster); !fault.IsKind(err, fault.Conflict) {
		t.Fatalf("foreign key=%v", err)
	}
}

func TestSiteOptimisticUpdateRejectsStaleVersion(t *testing.T) {
	db := openTestDB(t)
	value := insertActiveSite(t, db, "ESS-OPT", 1000)
	first := value
	first.Name = "First Update"
	first.Version++
	first.UpdatedAt = integrationNow.Add(time.Minute)
	if err := db.UpdateSite(context.Background(), first, value.Version); err != nil {
		t.Fatal(err)
	}
	stale := value
	stale.Name = "Stale Update"
	stale.Version++
	stale.UpdatedAt = integrationNow.Add(2 * time.Minute)
	err := db.UpdateSite(context.Background(), stale, value.Version)
	if !fault.IsKind(err, fault.Conflict) {
		t.Fatalf("stale update=%v", err)
	}
	loaded, err := db.SiteByID(context.Background(), value.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Name != "First Update" {
		t.Fatalf("name=%q", loaded.Name)
	}
}

func TestConcurrentCapacityReservationCannotOversubscribe(t *testing.T) {
	db := openTestDB(t)
	value := insertActiveSite(t, db, "ESS-RACE", 100)
	start := make(chan struct{})
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for index := 0; index < 2; index++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			results <- db.ReserveSiteCapacity(context.Background(), value.ID, 80, value.Version, encodeTime(integrationNow.Add(time.Minute)))
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	var succeeded, conflicted int
	for err := range results {
		if err == nil {
			succeeded++
		} else if fault.IsKind(err, fault.Conflict) {
			conflicted++
		} else {
			t.Fatalf("unexpected result: %v", err)
		}
	}
	if succeeded != 1 || conflicted != 1 {
		t.Fatalf("succeeded=%d conflicted=%d", succeeded, conflicted)
	}
	loaded, err := db.SiteByID(context.Background(), value.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.ReservedKW != 80 || loaded.ReservedKW > loaded.GridLimitKW {
		t.Fatalf("loaded=%+v", loaded)
	}
}

func TestTransactionRollsBackEveryWriteOnFailure(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	err := db.InTx(ctx, "test.rollback", func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `INSERT INTO users(id,email,password_hash,display_name,role,active,version,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?)`, "rollback-user", "rollback@example.com", "hash", "Rollback User", "auditor", 1, 1, encodeTime(integrationNow), encodeTime(integrationNow)); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO battery_clusters(id,site_id,code,rated_power_kw,capacity_kwh,min_soc,max_soc,current_soc,status,latest_sequence,version,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`, "bad-cluster", "missing", "BAT-BAD", 1, 1, 0, 100, 50, "available", 0, 1, encodeTime(integrationNow), encodeTime(integrationNow)); err != nil {
			return errors.New("forced relationship failure")
		}
		return nil
	})
	if err == nil {
		t.Fatal("transaction unexpectedly committed")
	}
	if _, err = db.UserByEmail(ctx, "rollback@example.com"); !fault.IsKind(err, fault.NotFound) {
		t.Fatalf("user survived rollback: %v", err)
	}
}

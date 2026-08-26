package stagechecks_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/vance1852/gridvault-ess/internal/clock"
	"github.com/vance1852/gridvault-ess/internal/fault"
	"github.com/vance1852/gridvault-ess/internal/identity"
	"github.com/vance1852/gridvault-ess/internal/service"
	"github.com/vance1852/gridvault-ess/internal/site"
	"github.com/vance1852/gridvault-ess/internal/storage/sqlite"
)

func TestInactivePrincipalCannotListSites0019(t *testing.T) {
	db, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "gridvault.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	sites := service.NewSiteService(db, clock.NewManual(time.Date(2026, 8, 26, 19, 0, 0, 0, time.UTC)))
	principal := service.Principal{User: identity.User{ID: "disabled-operator", Role: identity.RoleOperator, Active: false}}
	_, err = sites.List(context.Background(), principal, site.ListFilter{})
	if !fault.IsKind(err, fault.Unauthenticated) {
		t.Fatalf("inactive principal listed sites: %v", err)
	}
}

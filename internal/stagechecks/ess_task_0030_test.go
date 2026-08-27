package stagechecks_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/vance1852/gridvault-ess/internal/audit"
	"github.com/vance1852/gridvault-ess/internal/clock"
	"github.com/vance1852/gridvault-ess/internal/fault"
	"github.com/vance1852/gridvault-ess/internal/identity"
	"github.com/vance1852/gridvault-ess/internal/service"
	"github.com/vance1852/gridvault-ess/internal/site"
)

type staleSiteStore30 struct {
	stored      site.Site
	updateCalls int
}

func (*staleSiteStore30) InsertSite(context.Context, site.Site) error           { return nil }
func (s *staleSiteStore30) SiteByID(context.Context, string) (site.Site, error) { return s.stored, nil }
func (s *staleSiteStore30) UpdateSite(_ context.Context, changed site.Site, expected int64) error {
	s.updateCalls++
	if s.updateCalls == 1 {
		s.stored.Status = site.StatusSuspended
		s.stored.Version = 2
		return fault.ErrVersionConflict
	}
	if expected != s.stored.Version {
		return fault.ErrVersionConflict
	}
	s.stored = changed
	return nil
}
func (*staleSiteStore30) ListSites(context.Context, site.ListFilter) (site.Page, error) {
	return site.Page{}, nil
}
func (*staleSiteStore30) InsertCluster(context.Context, site.Cluster) error { return nil }
func (*staleSiteStore30) ClusterByID(context.Context, string) (site.Cluster, error) {
	return site.Cluster{}, fault.ErrNotFound
}
func (*staleSiteStore30) UpdateCluster(context.Context, site.Cluster, int64) error { return nil }
func (*staleSiteStore30) ClustersBySite(context.Context, string) ([]site.Cluster, error) {
	return nil, nil
}
func (*staleSiteStore30) InsertAudit(context.Context, audit.Event) error { return nil }

func TestStaleSiteTransitionIsNotReplayed0030(t *testing.T) {
	now := time.Date(2026, 8, 26, 22, 0, 0, 0, time.UTC)
	store := &staleSiteStore30{stored: site.Site{ID: "site-30", Status: site.StatusActive, Version: 1, UpdatedAt: now}}
	svc := service.NewSiteService(store, clock.NewManual(now))
	principal := service.Principal{User: identity.User{ID: "operator-30", Role: identity.RoleOperator, Active: true}}
	_, err := svc.Transition(context.Background(), principal, "site-30", site.StatusRetired, 1, "request-30")
	if !errors.Is(err, fault.ErrVersionConflict) {
		t.Fatalf("stale transition was replayed: %v", err)
	}
	if store.stored.Status != site.StatusSuspended || store.updateCalls != 1 {
		t.Fatalf("concurrent state overwritten: status=%s updates=%d", store.stored.Status, store.updateCalls)
	}
}

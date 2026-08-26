package service

import (
	"context"

	"github.com/vance1852/gridvault-ess/internal/audit"
	"github.com/vance1852/gridvault-ess/internal/clock"
	"github.com/vance1852/gridvault-ess/internal/fault"
	"github.com/vance1852/gridvault-ess/internal/identity"
	"github.com/vance1852/gridvault-ess/internal/site"
)

type SiteStore interface {
	InsertSite(context.Context, site.Site) error
	SiteByID(context.Context, string) (site.Site, error)
	UpdateSite(context.Context, site.Site, int64) error
	ListSites(context.Context, site.ListFilter) (site.Page, error)
	InsertCluster(context.Context, site.Cluster) error
	ClusterByID(context.Context, string) (site.Cluster, error)
	UpdateCluster(context.Context, site.Cluster, int64) error
	ClustersBySite(context.Context, string) ([]site.Cluster, error)
	InsertAudit(context.Context, audit.Event) error
}
type SiteService struct {
	store SiteStore
	clock clock.Clock
}

func NewSiteService(store SiteStore, timer clock.Clock) *SiteService {
	return &SiteService{store: store, clock: timer}
}
func (s *SiteService) Create(ctx context.Context, p Principal, input site.NewSite, request string) (site.Site, error) {
	if err := p.Require(identity.PermissionSiteManage); err != nil {
		return site.Site{}, err
	}
	entity, err := site.Create(input, s.clock.Now())
	if err != nil {
		return site.Site{}, err
	}
	if err = s.store.InsertSite(ctx, entity); err != nil {
		return site.Site{}, err
	}
	event, _ := audit.NewEvent(p.User.ID, requestID(request), "site", entity.ID, "create", "success", map[string]any{"code": entity.Code, "grid_limit_kw": entity.GridLimitKW}, s.clock.Now())
	if err = s.store.InsertAudit(ctx, event); err != nil {
		return site.Site{}, err
	}
	return entity, nil
}
func (s *SiteService) Transition(ctx context.Context, p Principal, id string, next site.Status, expected int64, request string) (site.Site, error) {
	if err := p.Require(identity.PermissionSiteManage); err != nil {
		return site.Site{}, err
	}
	entity, err := s.store.SiteByID(ctx, id)
	if err != nil {
		return site.Site{}, err
	}
	if entity.Version != expected {
		return site.Site{}, fault.ErrVersionConflict
	}
	changed, err := entity.Transition(next, s.clock.Now())
	if err != nil {
		return site.Site{}, err
	}
	if err = s.store.UpdateSite(ctx, changed, expected); err != nil {
		return site.Site{}, err
	}
	event, _ := audit.NewEvent(p.User.ID, requestID(request), "site", id, "transition", "success", map[string]any{"from": entity.Status, "to": changed.Status}, s.clock.Now())
	if err = s.store.InsertAudit(ctx, event); err != nil {
		return site.Site{}, err
	}
	return changed, nil
}
func (s *SiteService) CreateCluster(ctx context.Context, p Principal, input site.NewCluster, request string) (site.Cluster, error) {
	if err := p.Require(identity.PermissionSiteManage); err != nil {
		return site.Cluster{}, err
	}
	owner, err := s.store.SiteByID(ctx, input.SiteID)
	if err != nil {
		return site.Cluster{}, err
	}
	if owner.Status == site.StatusRetired {
		return site.Cluster{}, fault.New(fault.Conflict, "site_retired", "retired site cannot accept battery clusters")
	}
	cluster, err := site.CreateCluster(input, s.clock.Now())
	if err != nil {
		return site.Cluster{}, err
	}
	if err = s.store.InsertCluster(ctx, cluster); err != nil {
		return site.Cluster{}, err
	}
	event, _ := audit.NewEvent(p.User.ID, requestID(request), "battery_cluster", cluster.ID, "create", "success", map[string]any{"site_id": cluster.SiteID, "rated_power_kw": cluster.RatedPowerKW}, s.clock.Now())
	if err = s.store.InsertAudit(ctx, event); err != nil {
		return site.Cluster{}, err
	}
	return cluster, nil
}
func (s *SiteService) List(ctx context.Context, p Principal, filter site.ListFilter) (site.Page, error) {
	if !p.User.Active {
		return site.Page{}, fault.ErrUnauthorized
	}
	return s.store.ListSites(ctx, filter)
}

package service

import (
	"context"
	"database/sql"
	"time"

	"github.com/vance1852/gridvault-ess/internal/audit"
	"github.com/vance1852/gridvault-ess/internal/clock"
	"github.com/vance1852/gridvault-ess/internal/dispatch"
	"github.com/vance1852/gridvault-ess/internal/fault"
	"github.com/vance1852/gridvault-ess/internal/identity"
	"github.com/vance1852/gridvault-ess/internal/site"
)

type DispatchStore interface {
	InsertPlan(context.Context, dispatch.Plan) error
	PlanByID(context.Context, string) (dispatch.Plan, error)
	UpdatePlan(context.Context, dispatch.Plan, int64) error
	ListPlans(context.Context, dispatch.PlanFilter) (dispatch.PlanPage, error)
	SiteByID(context.Context, string) (site.Site, error)
	ClusterByID(context.Context, string) (site.Cluster, error)
	ReservationsByPlan(context.Context, string) ([]dispatch.Reservation, error)
	JobsByPlan(context.Context, string) ([]dispatch.Job, error)
	SubmitPlanAtomic(context.Context, dispatch.Plan, int64, []dispatch.Reservation, string, int64, int64, func(*sql.Tx) error) error
	DispatchPlanAtomic(context.Context, dispatch.Plan, int64, []dispatch.Job, func(*sql.Tx) error) error
	AuditInserter(context.Context, audit.Event) func(*sql.Tx) error
	InsertAudit(context.Context, audit.Event) error
}
type DispatchService struct {
	store DispatchStore
	clock clock.Clock
}

func NewDispatchService(store DispatchStore, timer clock.Clock) *DispatchService {
	return &DispatchService{store: store, clock: timer}
}
func (s *DispatchService) Create(ctx context.Context, p Principal, input dispatch.NewPlan, request string) (dispatch.Plan, error) {
	if err := p.Require(identity.PermissionPlanWrite); err != nil {
		return dispatch.Plan{}, err
	}
	input.CreatedBy = p.User.ID
	if _, err := s.store.SiteByID(ctx, input.SiteID); err != nil {
		return dispatch.Plan{}, err
	}
	plan, err := dispatch.CreatePlan(input, s.clock.Now())
	if err != nil {
		return dispatch.Plan{}, err
	}
	if err = s.store.InsertPlan(ctx, plan); err != nil {
		return dispatch.Plan{}, err
	}
	event, _ := audit.NewEvent(p.User.ID, requestID(request), "dispatch_plan", plan.ID, "create", "success", map[string]any{"direction": plan.Direction, "requested_kw": plan.RequestedKW}, s.clock.Now())
	if err = s.store.InsertAudit(ctx, event); err != nil {
		return dispatch.Plan{}, err
	}
	return plan, nil
}

type SubmitPlanInput struct {
	PlanID          string
	ExpectedVersion int64
	ClusterIDs      []string
}

func (s *DispatchService) Submit(ctx context.Context, p Principal, input SubmitPlanInput, request string) (dispatch.Plan, error) {
	if err := p.Require(identity.PermissionPlanWrite); err != nil {
		return dispatch.Plan{}, err
	}
	plan, err := s.store.PlanByID(ctx, input.PlanID)
	if err != nil {
		return dispatch.Plan{}, err
	}
	if plan.CreatedBy != p.User.ID {
		return dispatch.Plan{}, fault.New(fault.Forbidden, "plan_owner_required", "only plan creator can submit this draft")
	}
	if plan.Version != input.ExpectedVersion {
		return dispatch.Plan{}, fault.ErrVersionConflict
	}
	owner, err := s.store.SiteByID(ctx, plan.SiteID)
	if err != nil {
		return dispatch.Plan{}, err
	}
	if err = owner.CanAcceptPlans(); err != nil {
		return dispatch.Plan{}, err
	}
	ratings := map[string]int64{}
	clusters := make([]site.Cluster, 0, len(input.ClusterIDs))
	seen := map[string]bool{}
	for _, id := range input.ClusterIDs {
		if seen[id] {
			return dispatch.Plan{}, fault.New(fault.Invalid, "duplicate_cluster", "cluster can only be selected once")
		}
		seen[id] = true
		cluster, loadErr := s.store.ClusterByID(ctx, id)
		if loadErr != nil {
			return dispatch.Plan{}, loadErr
		}
		if cluster.SiteID != plan.SiteID {
			return dispatch.Plan{}, fault.New(fault.Conflict, "cluster_site_mismatch", "selected cluster belongs to another site")
		}
		if eligibilityErr := cluster.Eligible(string(plan.Direction), minInt64(plan.RequestedKW, cluster.RatedPowerKW)); eligibilityErr != nil {
			return dispatch.Plan{}, eligibilityErr
		}
		ratings[id] = cluster.RatedPowerKW
		clusters = append(clusters, cluster)
	}
	allocations, err := dispatch.AllocatePower(plan.RequestedKW, ratings)
	if err != nil {
		return dispatch.Plan{}, err
	}
	reservations := make([]dispatch.Reservation, 0, len(clusters))
	for _, cluster := range clusters {
		reservation, createErr := dispatch.NewReservation(plan.ID, cluster.ID, allocations[cluster.ID], plan.Window, s.clock.Now())
		if createErr != nil {
			return dispatch.Plan{}, createErr
		}
		reservations = append(reservations, reservation)
	}
	submitted, err := plan.Submit(len(reservations), plan.RequestedKW, s.clock.Now())
	if err != nil {
		return dispatch.Plan{}, err
	}
	event, _ := audit.NewEvent(p.User.ID, requestID(request), "dispatch_plan", plan.ID, "submit", "success", map[string]any{"clusters": len(reservations), "reserved_kw": plan.RequestedKW}, s.clock.Now())
	if err = s.store.SubmitPlanAtomic(ctx, submitted, plan.Version, reservations, owner.ID, plan.RequestedKW, owner.Version, s.store.AuditInserter(ctx, event)); err != nil {
		return dispatch.Plan{}, err
	}
	return submitted, nil
}
func minInt64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}
func (s *DispatchService) Approve(ctx context.Context, p Principal, id string, expected int64, request string) (dispatch.Plan, error) {
	if err := p.Require(identity.PermissionPlanApprove); err != nil {
		return dispatch.Plan{}, err
	}
	plan, err := s.store.PlanByID(ctx, id)
	if err != nil {
		return dispatch.Plan{}, err
	}
	approved, err := plan.Approve(p.User.ID, expected, s.clock.Now())
	if err != nil {
		return dispatch.Plan{}, err
	}
	if err = s.store.UpdatePlan(ctx, approved, expected); err != nil {
		return dispatch.Plan{}, err
	}
	event, _ := audit.NewEvent(p.User.ID, requestID(request), "dispatch_plan", id, "approve", "success", nil, s.clock.Now())
	if err = s.store.InsertAudit(ctx, event); err != nil {
		return dispatch.Plan{}, err
	}
	return approved, nil
}
func (s *DispatchService) Dispatch(ctx context.Context, p Principal, id string, expected int64, request string) (dispatch.Plan, error) {
	if err := p.Require(identity.PermissionPlanDispatch); err != nil {
		return dispatch.Plan{}, err
	}
	plan, err := s.store.PlanByID(ctx, id)
	if err != nil {
		return dispatch.Plan{}, err
	}
	reservations, err := s.store.ReservationsByPlan(ctx, id)
	if err != nil {
		return dispatch.Plan{}, err
	}
	jobs := make([]dispatch.Job, 0, len(reservations))
	for _, reservation := range reservations {
		job, createErr := dispatch.NewJob(id, reservation.ClusterID, 5, s.clock.Now())
		if createErr != nil {
			return dispatch.Plan{}, createErr
		}
		jobs = append(jobs, job)
	}
	changed, err := plan.Dispatch(len(jobs), len(reservations), s.clock.Now())
	if err != nil {
		return dispatch.Plan{}, err
	}
	event, _ := audit.NewEvent(p.User.ID, requestID(request), "dispatch_plan", id, "dispatch", "success", map[string]any{"jobs": len(jobs)}, s.clock.Now())
	if err = s.store.DispatchPlanAtomic(ctx, changed, expected, jobs, s.store.AuditInserter(ctx, event)); err != nil {
		return dispatch.Plan{}, err
	}
	return changed, nil
}
func (s *DispatchService) Get(ctx context.Context, p Principal, id string) (dispatch.Plan, error) {
	if !p.User.Active {
		return dispatch.Plan{}, fault.ErrUnauthorized
	}
	return s.store.PlanByID(ctx, id)
}
func (s *DispatchService) List(ctx context.Context, p Principal, filter dispatch.PlanFilter) (dispatch.PlanPage, error) {
	if !p.User.Active {
		return dispatch.PlanPage{}, fault.ErrUnauthorized
	}
	return s.store.ListPlans(ctx, filter)
}
func (s *DispatchService) WaitUntilStart(ctx context.Context, plan dispatch.Plan) error {
	delay := time.Until(plan.Window.Start)
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

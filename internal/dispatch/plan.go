package dispatch

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/vance1852/gridvault-ess/internal/clock"
	"github.com/vance1852/gridvault-ess/internal/fault"
)

type Direction string

const (
	Charge    Direction = "charge"
	Discharge Direction = "discharge"
)

type PlanStatus string

const (
	PlanDraft      PlanStatus = "draft"
	PlanSubmitted  PlanStatus = "submitted"
	PlanApproved   PlanStatus = "approved"
	PlanDispatched PlanStatus = "dispatched"
	PlanRunning    PlanStatus = "running"
	PlanCompleted  PlanStatus = "completed"
	PlanFailed     PlanStatus = "failed"
	PlanCancelled  PlanStatus = "cancelled"
)

type Plan struct {
	ID          string
	SiteID      string
	Name        string
	Direction   Direction
	RequestedKW int64
	TargetKWh   int64
	Window      clock.Window
	Status      PlanStatus
	CreatedBy   string
	ApprovedBy  string
	ApprovedAt  *time.Time
	Version     int64
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type NewPlan struct {
	SiteID      string
	Name        string
	Direction   Direction
	RequestedKW int64
	TargetKWh   int64
	StartsAt    time.Time
	EndsAt      time.Time
	CreatedBy   string
}

func CreatePlan(input NewPlan, now time.Time) (Plan, error) {
	if strings.TrimSpace(input.SiteID) == "" || strings.TrimSpace(input.CreatedBy) == "" {
		return Plan{}, fault.New(fault.Invalid, "missing_plan_owner", "site and creator are required")
	}
	name := strings.TrimSpace(input.Name)
	if size := utf8.RuneCountInString(name); size < 3 || size > 120 {
		return Plan{}, fault.New(fault.Invalid, "invalid_plan_name", "plan name must contain 3 to 120 characters")
	}
	if input.Direction != Charge && input.Direction != Discharge {
		return Plan{}, fault.New(fault.Invalid, "invalid_direction", "direction must be charge or discharge")
	}
	if input.RequestedKW <= 0 || input.RequestedKW > 10_000_000 {
		return Plan{}, fault.New(fault.Invalid, "invalid_requested_power", "requested power is outside supported range")
	}
	if input.TargetKWh <= 0 || input.TargetKWh > 100_000_000 {
		return Plan{}, fault.New(fault.Invalid, "invalid_target_energy", "target energy is outside supported range")
	}
	window, ok := clock.NewWindow(input.StartsAt, input.EndsAt)
	if !ok {
		return Plan{}, fault.New(fault.Invalid, "invalid_plan_window", "plan end must follow its start")
	}
	if window.Start.Before(now.UTC().Add(-time.Minute)) {
		return Plan{}, fault.New(fault.Invalid, "plan_starts_in_past", "plan cannot start in the past")
	}
	if window.Duration() < time.Minute || window.Duration() > 7*24*time.Hour {
		return Plan{}, fault.New(fault.Invalid, "invalid_plan_duration", "plan duration must be between one minute and seven days")
	}
	maximumEnergy := input.RequestedKW * int64(window.Duration()/time.Minute) / 60
	if maximumEnergy <= 0 || input.TargetKWh > maximumEnergy {
		return Plan{}, fault.WithFields(
			fault.New(fault.Invalid, "target_energy_unreachable", "target energy cannot be delivered in the requested window"),
			map[string]string{"maximum_kwh": fmt.Sprint(maximumEnergy)},
		)
	}
	now = now.UTC()
	return Plan{
		ID:          uuid.NewString(),
		SiteID:      input.SiteID,
		Name:        name,
		Direction:   input.Direction,
		RequestedKW: input.RequestedKW,
		TargetKWh:   input.TargetKWh,
		Window:      window,
		Status:      PlanDraft,
		CreatedBy:   input.CreatedBy,
		Version:     1,
		CreatedAt:   now,
		UpdatedAt:   now,
	}, nil
}

func (p Plan) EditableBy(actorID string) error {
	if p.Status != PlanDraft {
		return fault.New(fault.Conflict, "plan_not_editable", "only draft plans can be edited")
	}
	if actorID != p.CreatedBy {
		return fault.New(fault.Forbidden, "plan_owner_required", "only the plan creator can edit this draft")
	}
	return nil
}

func (p Plan) Rename(actorID, name string, now time.Time) (Plan, error) {
	if err := p.EditableBy(actorID); err != nil {
		return Plan{}, err
	}
	name = strings.TrimSpace(name)
	if size := utf8.RuneCountInString(name); size < 3 || size > 120 {
		return Plan{}, fault.New(fault.Invalid, "invalid_plan_name", "plan name must contain 3 to 120 characters")
	}
	copy := p
	copy.Name = name
	copy.Version++
	copy.UpdatedAt = now.UTC()
	return copy, nil
}

func (p Plan) Submit(clusterCount int, reservedKW int64, now time.Time) (Plan, error) {
	if p.Status != PlanDraft {
		return Plan{}, transitionError(p.Status, PlanSubmitted)
	}
	if clusterCount <= 0 {
		return Plan{}, fault.New(fault.Conflict, "plan_has_no_clusters", "plan needs at least one battery cluster")
	}
	if reservedKW != p.RequestedKW {
		return Plan{}, fault.WithFields(
			fault.New(fault.Conflict, "reservation_power_mismatch", "cluster reservations must cover requested power exactly"),
			map[string]string{"requested_kw": fmt.Sprint(p.RequestedKW), "reserved_kw": fmt.Sprint(reservedKW)},
		)
	}
	copy := p
	copy.Status = PlanSubmitted
	copy.Version++
	copy.UpdatedAt = now.UTC()
	return copy, nil
}

func (p Plan) Approve(actorID string, expectedVersion int64, now time.Time) (Plan, error) {
	if p.Version != expectedVersion {
		return Plan{}, fault.ErrVersionConflict
	}
	if p.Status != PlanSubmitted {
		return Plan{}, transitionError(p.Status, PlanApproved)
	}
	if actorID == p.CreatedBy {
		return Plan{}, fault.New(fault.Forbidden, "four_eyes_required", "plan creator cannot approve the same plan")
	}
	at := now.UTC()
	copy := p
	copy.Status = PlanApproved
	copy.ApprovedBy = actorID
	copy.ApprovedAt = &at
	copy.Version++
	copy.UpdatedAt = at
	return copy, nil
}

func (p Plan) Dispatch(jobCount, reservationCount int, now time.Time) (Plan, error) {
	if p.Status != PlanApproved {
		return Plan{}, transitionError(p.Status, PlanDispatched)
	}
	if jobCount == 0 || jobCount != reservationCount {
		return Plan{}, fault.New(fault.Conflict, "execution_job_mismatch", "each reservation needs exactly one execution job")
	}
	copy := p
	copy.Status = PlanDispatched
	copy.Version++
	copy.UpdatedAt = now.UTC()
	return copy, nil
}

func (p Plan) Start(successfulJobs, totalJobs int, now time.Time) (Plan, error) {
	if p.Status != PlanDispatched {
		return Plan{}, transitionError(p.Status, PlanRunning)
	}
	if totalJobs <= 0 || successfulJobs != totalJobs {
		return Plan{}, fault.New(fault.Conflict, "jobs_not_started", "all execution jobs must start successfully")
	}
	copy := p
	copy.Status = PlanRunning
	copy.Version++
	copy.UpdatedAt = now.UTC()
	return copy, nil
}

func (p Plan) Complete(actualEnergyWh int64, totalJobs, successfulJobs int, now time.Time) (Plan, error) {
	if p.Status != PlanRunning && p.Status != PlanDispatched {
		return Plan{}, transitionError(p.Status, PlanCompleted)
	}
	if totalJobs <= 0 || successfulJobs != totalJobs {
		return Plan{}, fault.New(fault.Conflict, "execution_incomplete", "all execution jobs must succeed before completion")
	}
	if actualEnergyWh <= 0 {
		return Plan{}, fault.New(fault.Conflict, "energy_not_recorded", "positive executed energy is required before completion")
	}
	copy := p
	copy.Status = PlanCompleted
	copy.Version++
	copy.UpdatedAt = now.UTC()
	return copy, nil
}

func (p Plan) Fail(reason string, now time.Time) (Plan, error) {
	if p.Status != PlanDispatched && p.Status != PlanRunning {
		return Plan{}, transitionError(p.Status, PlanFailed)
	}
	if strings.TrimSpace(reason) == "" {
		return Plan{}, fault.New(fault.Invalid, "failure_reason_required", "failure reason is required")
	}
	copy := p
	copy.Status = PlanFailed
	copy.Version++
	copy.UpdatedAt = now.UTC()
	return copy, nil
}

func (p Plan) Cancel(now time.Time) (Plan, error) {
	switch p.Status {
	case PlanDraft, PlanSubmitted, PlanApproved:
		copy := p
		copy.Status = PlanCancelled
		copy.Version++
		copy.UpdatedAt = now.UTC()
		return copy, nil
	default:
		return Plan{}, transitionError(p.Status, PlanCancelled)
	}
}

func (p Plan) IsTerminal() bool {
	return p.Status == PlanCompleted || p.Status == PlanFailed || p.Status == PlanCancelled
}

func transitionError(current, next PlanStatus) error {
	return fault.WithFields(
		fault.New(fault.Conflict, "invalid_plan_transition", "dispatch plan transition is not allowed"),
		map[string]string{"current": string(current), "next": string(next)},
	)
}

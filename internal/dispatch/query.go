package dispatch

import (
	"strings"
	"time"

	"github.com/vance1852/gridvault-ess/internal/fault"
)

type PlanFilter struct {
	SiteID string
	Status PlanStatus
	From   *time.Time
	Until  *time.Time
	Search string
	Limit  int
	Offset int
	Newest bool
}

func (f PlanFilter) Normalize() (PlanFilter, error) {
	copy := f
	copy.SiteID = strings.TrimSpace(copy.SiteID)
	copy.Search = strings.TrimSpace(copy.Search)
	if len(copy.Search) > 120 {
		return PlanFilter{}, fault.New(fault.Invalid, "search_too_long", "search text cannot exceed 120 characters")
	}
	if copy.Limit == 0 {
		copy.Limit = 50
	}
	if copy.Limit < 1 || copy.Limit > 200 {
		return PlanFilter{}, fault.New(fault.Invalid, "invalid_page_limit", "page limit must be between 1 and 200")
	}
	if copy.Offset < 0 || copy.Offset > 1_000_000 {
		return PlanFilter{}, fault.New(fault.Invalid, "invalid_page_offset", "page offset is outside supported range")
	}
	if copy.From != nil {
		value := copy.From.UTC()
		copy.From = &value
	}
	if copy.Until != nil {
		value := copy.Until.UTC()
		copy.Until = &value
	}
	if copy.From != nil && copy.Until != nil && !copy.From.Before(*copy.Until) {
		return PlanFilter{}, fault.New(fault.Invalid, "invalid_filter_window", "filter end must follow its start")
	}
	if copy.Status != "" {
		switch copy.Status {
		case PlanDraft, PlanSubmitted, PlanApproved, PlanDispatched, PlanRunning, PlanCompleted, PlanFailed, PlanCancelled:
		default:
			return PlanFilter{}, fault.New(fault.Invalid, "invalid_plan_status", "plan status filter is not supported")
		}
	}
	return copy, nil
}

type PlanPage struct {
	Items  []Plan
	Total  int64
	Limit  int
	Offset int
}

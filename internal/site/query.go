package site

import (
	"strings"

	"github.com/vance1852/gridvault-ess/internal/fault"
)

type Sort string

const (
	SortCodeAsc       Sort = "code_asc"
	SortCodeDesc      Sort = "code_desc"
	SortUpdatedNewest Sort = "updated_newest"
	SortUpdatedOldest Sort = "updated_oldest"
)

type ListFilter struct {
	Status Status
	Search string
	Sort   Sort
	Limit  int
	Offset int
}

func (f ListFilter) Normalize() (ListFilter, error) {
	copy := f
	copy.Search = strings.TrimSpace(copy.Search)
	if len(copy.Search) > 120 {
		return ListFilter{}, fault.New(fault.Invalid, "search_too_long", "search text cannot exceed 120 characters")
	}
	if copy.Limit == 0 {
		copy.Limit = 50
	}
	if copy.Limit < 1 || copy.Limit > 200 {
		return ListFilter{}, fault.New(fault.Invalid, "invalid_page_limit", "page limit must be between 1 and 200")
	}
	if copy.Offset < 0 || copy.Offset > 1_000_000 {
		return ListFilter{}, fault.New(fault.Invalid, "invalid_page_offset", "page offset is outside supported range")
	}
	if copy.Sort == "" {
		copy.Sort = SortCodeAsc
	}
	switch copy.Sort {
	case SortCodeAsc, SortCodeDesc, SortUpdatedNewest, SortUpdatedOldest:
	default:
		return ListFilter{}, fault.New(fault.Invalid, "invalid_sort", "site sort option is not supported")
	}
	if copy.Status != "" {
		switch copy.Status {
		case StatusCommissioning, StatusActive, StatusSuspended, StatusRetired:
		default:
			return ListFilter{}, fault.New(fault.Invalid, "invalid_site_status", "site status filter is not supported")
		}
	}
	return copy, nil
}

type Page struct {
	Items  []Site
	Total  int64
	Limit  int
	Offset int
}

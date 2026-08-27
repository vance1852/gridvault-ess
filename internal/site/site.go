package site

import (
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/vance1852/gridvault-ess/internal/fault"
)

type Status string

const (
	StatusCommissioning Status = "commissioning"
	StatusActive        Status = "active"
	StatusSuspended     Status = "suspended"
	StatusRetired       Status = "retired"
)

var codePattern = regexp.MustCompile(`^[A-Z][A-Z0-9-]{2,31}$`)

type Site struct {
	ID          string
	Code        string
	Name        string
	Timezone    string
	GridLimitKW int64
	ReservedKW  int64
	Status      Status
	Version     int64
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type NewSite struct {
	Code        string
	Name        string
	Timezone    string
	GridLimitKW int64
}

func Create(input NewSite, now time.Time) (Site, error) {
	code := strings.ToUpper(strings.TrimSpace(input.Code))
	if !codePattern.MatchString(code) {
		return Site{}, fault.New(fault.Invalid, "invalid_site_code", "site code must use 3 to 32 uppercase letters, digits, or hyphens")
	}
	name := strings.TrimSpace(input.Name)
	if size := utf8.RuneCountInString(name); size < 2 || size > 120 {
		return Site{}, fault.New(fault.Invalid, "invalid_site_name", "site name must contain 2 to 120 characters")
	}
	zone := strings.TrimSpace(input.Timezone)
	if _, err := time.LoadLocation(zone); err != nil {
		return Site{}, fault.Wrap(fault.Invalid, "invalid_timezone", "site timezone is not recognized", "site.Create", err)
	}
	if input.GridLimitKW < 10 || input.GridLimitKW > 10_000_000 {
		return Site{}, fault.New(fault.Invalid, "invalid_grid_limit", "grid limit must be between 10 and 10000000 kW")
	}
	now = now.UTC()
	return Site{
		ID:          uuid.NewString(),
		Code:        code,
		Name:        name,
		Timezone:    zone,
		GridLimitKW: input.GridLimitKW,
		Status:      StatusCommissioning,
		Version:     1,
		CreatedAt:   now,
		UpdatedAt:   now,
	}, nil
}

func (s Site) AvailableKW() int64 {
	value := s.GridLimitKW - s.ReservedKW
	if value < 0 {
		return 0
	}
	return value
}

func (s Site) CanAcceptPlans() error {
	if s.Status != StatusActive {
		return fault.WithFields(
			fault.New(fault.Conflict, "site_not_active", "site cannot accept dispatch plans in its current state"),
			map[string]string{"status": string(s.Status)},
		)
	}
	return nil
}

func (s Site) Reserve(powerKW int64, now time.Time) (Site, error) {
	if err := s.CanAcceptPlans(); err != nil {
		return Site{}, err
	}
	if powerKW <= 0 {
		return Site{}, fault.New(fault.Invalid, "invalid_reservation_power", "reserved power must be positive")
	}
	if powerKW > s.AvailableKW() {
		return Site{}, fault.WithFields(
			fault.New(fault.Conflict, "grid_capacity_exceeded", "requested power exceeds available grid capacity"),
			map[string]string{
				"requested_kw": fmt.Sprint(powerKW),
				"available_kw": fmt.Sprint(s.AvailableKW()),
			},
		)
	}
	copy := s
	copy.ReservedKW += powerKW
	copy.Version++
	copy.UpdatedAt = now.UTC()
	return copy, nil
}

func (s Site) Release(powerKW int64, now time.Time) (Site, error) {
	if powerKW <= 0 {
		return Site{}, fault.New(fault.Invalid, "invalid_release_power", "released power must be positive")
	}
	if powerKW > s.ReservedKW {
		return Site{}, fault.New(fault.Conflict, "reservation_underflow", "released power exceeds current reservation")
	}
	copy := s
	copy.ReservedKW -= powerKW
	copy.Version++
	copy.UpdatedAt = now.UTC()
	return copy, nil
}

func (s Site) Transition(next Status, now time.Time) (Site, error) {
	allowed := map[Status][]Status{
		StatusCommissioning: {StatusActive, StatusRetired},
		StatusActive:        {StatusSuspended, StatusRetired},
		StatusSuspended:     {StatusActive, StatusRetired},
		StatusRetired:       {},
	}
	for _, candidate := range allowed[s.Status] {
		if candidate == next {
			if next == StatusRetired && s.ReservedKW != 0 {
				return Site{}, fault.New(fault.Conflict, "site_has_reservations", "site with active reservations cannot be retired")
			}
			copy := s
			copy.Status = next
			copy.Version++
			copy.UpdatedAt = now.UTC()
			return copy, nil
		}
	}
	return Site{}, fault.WithFields(
		fault.New(fault.Conflict, "invalid_site_transition", "site status transition is not allowed"),
		map[string]string{"current": string(s.Status), "next": string(next)},
	)
}

func (s Site) LocalTime(at time.Time) (time.Time, error) {
	location, err := time.LoadLocation(s.Timezone)
	if err != nil {
		return time.Time{}, fault.Wrap(fault.Internal, "timezone_unavailable", "site timezone could not be loaded", "site.LocalTime", err)
	}
	return at.In(location), nil
}

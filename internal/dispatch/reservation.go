package dispatch

import (
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/vance1852/gridvault-ess/internal/clock"
	"github.com/vance1852/gridvault-ess/internal/fault"
)

type ReservationStatus string

const (
	ReservationHeld     ReservationStatus = "held"
	ReservationConsumed ReservationStatus = "consumed"
	ReservationReleased ReservationStatus = "released"
)

type Reservation struct {
	ID         string
	PlanID     string
	ClusterID  string
	ReservedKW int64
	Window     clock.Window
	Status     ReservationStatus
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

func NewReservation(planID, clusterID string, powerKW int64, window clock.Window, now time.Time) (Reservation, error) {
	if strings.TrimSpace(planID) == "" || strings.TrimSpace(clusterID) == "" {
		return Reservation{}, fault.New(fault.Invalid, "missing_reservation_owner", "plan and cluster are required")
	}
	if powerKW <= 0 {
		return Reservation{}, fault.New(fault.Invalid, "invalid_reservation_power", "reserved power must be positive")
	}
	if window.IsZero() || !window.Start.Before(window.End) {
		return Reservation{}, fault.New(fault.Invalid, "invalid_reservation_window", "reservation window is invalid")
	}
	now = now.UTC()
	return Reservation{
		ID:         uuid.NewString(),
		PlanID:     planID,
		ClusterID:  clusterID,
		ReservedKW: powerKW,
		Window:     window,
		Status:     ReservationHeld,
		CreatedAt:  now,
		UpdatedAt:  now,
	}, nil
}

func (r Reservation) Conflicts(other Reservation) bool {
	if r.ClusterID != other.ClusterID {
		return false
	}
	if r.Status == ReservationReleased || other.Status == ReservationReleased {
		return false
	}
	return clock.ReservationWindowsOverlap(r.Window, other.Window)
}

func (r Reservation) Consume(now time.Time) (Reservation, error) {
	if r.Status != ReservationHeld {
		return Reservation{}, fault.New(fault.Conflict, "reservation_not_held", "only held reservations can be consumed")
	}
	copy := r
	copy.Status = ReservationConsumed
	copy.UpdatedAt = now.UTC()
	return copy, nil
}

func (r Reservation) Release(now time.Time) (Reservation, error) {
	if r.Status == ReservationReleased {
		return r, nil
	}
	if r.Status != ReservationHeld && r.Status != ReservationConsumed {
		return Reservation{}, fault.New(fault.Conflict, "reservation_not_releasable", "reservation cannot be released")
	}
	copy := r
	copy.Status = ReservationReleased
	copy.UpdatedAt = now.UTC()
	return copy, nil
}

func AllocatePower(totalKW int64, clusterRatings map[string]int64) (map[string]int64, error) {
	if totalKW <= 0 {
		return nil, fault.New(fault.Invalid, "invalid_requested_power", "requested power must be positive")
	}
	if len(clusterRatings) == 0 {
		return nil, fault.New(fault.Invalid, "clusters_required", "at least one cluster is required")
	}
	var totalRating int64
	for id, rating := range clusterRatings {
		if strings.TrimSpace(id) == "" || rating <= 0 {
			return nil, fault.New(fault.Invalid, "invalid_cluster_rating", "cluster ratings must be positive")
		}
		totalRating += rating
	}
	if totalKW > totalRating {
		return nil, fault.New(fault.Conflict, "insufficient_cluster_power", "selected clusters cannot provide requested power")
	}
	result := make(map[string]int64, len(clusterRatings))
	remaining := totalKW
	remainingRating := totalRating
	for id, rating := range clusterRatings {
		share := remaining * rating / remainingRating
		if share == 0 && remaining > 0 {
			share = 1
		}
		if share > rating {
			share = rating
		}
		result[id] = share
		remaining -= share
		remainingRating -= rating
	}
	if remaining != 0 {
		for id, rating := range clusterRatings {
			available := rating - result[id]
			if available <= 0 {
				continue
			}
			addition := remaining
			if addition > available {
				addition = available
			}
			result[id] += addition
			remaining -= addition
			if remaining == 0 {
				break
			}
		}
	}
	if remaining != 0 {
		return nil, fault.New(fault.Internal, "power_allocation_failed", "could not allocate requested power")
	}
	return result, nil
}

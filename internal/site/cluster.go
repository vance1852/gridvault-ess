package site

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/vance1852/gridvault-ess/internal/fault"
)

type ClusterStatus string

const (
	ClusterAvailable ClusterStatus = "available"
	ClusterReserved  ClusterStatus = "reserved"
	ClusterRunning   ClusterStatus = "running"
	ClusterDegraded  ClusterStatus = "degraded"
	ClusterOffline   ClusterStatus = "offline"
)

type Cluster struct {
	ID                string
	SiteID            string
	Code              string
	RatedPowerKW      int64
	CapacityKWh       int64
	MinSOC            int
	MaxSOC            int
	CurrentSOC        int
	Status            ClusterStatus
	LatestSequence    int64
	LatestTelemetryAt *time.Time
	Version           int64
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

type OperatingMargin struct {
	BelowMinimum int
	AboveMaximum int
	WithinWindow bool
}

func (c Cluster) Margin() OperatingMargin {
	margin := OperatingMargin{WithinWindow: c.CurrentSOC >= c.MinSOC && c.CurrentSOC <= c.MaxSOC}
	if c.CurrentSOC < c.MinSOC {
		margin.BelowMinimum = c.MinSOC - c.CurrentSOC
	}
	if c.CurrentSOC > c.MaxSOC {
		margin.AboveMaximum = c.CurrentSOC - c.MaxSOC
	}
	return margin
}

type NewCluster struct {
	SiteID       string
	Code         string
	RatedPowerKW int64
	CapacityKWh  int64
	MinSOC       int
	MaxSOC       int
	InitialSOC   int
}

func CreateCluster(input NewCluster, now time.Time) (Cluster, error) {
	if strings.TrimSpace(input.SiteID) == "" {
		return Cluster{}, fault.New(fault.Invalid, "missing_site", "cluster site is required")
	}
	code := strings.ToUpper(strings.TrimSpace(input.Code))
	if !codePattern.MatchString(code) {
		return Cluster{}, fault.New(fault.Invalid, "invalid_cluster_code", "cluster code must use 3 to 32 uppercase letters, digits, or hyphens")
	}
	if input.RatedPowerKW < 1 || input.RatedPowerKW > 2_000_000 {
		return Cluster{}, fault.New(fault.Invalid, "invalid_rated_power", "rated power is outside supported range")
	}
	if input.CapacityKWh < 1 || input.CapacityKWh > 20_000_000 {
		return Cluster{}, fault.New(fault.Invalid, "invalid_capacity", "energy capacity is outside supported range")
	}
	if input.MinSOC < 0 || input.MaxSOC > 100 || input.MinSOC >= input.MaxSOC {
		return Cluster{}, fault.New(fault.Invalid, "invalid_soc_window", "SOC window must be ordered within 0 to 100")
	}
	if input.InitialSOC < input.MinSOC || input.InitialSOC > input.MaxSOC {
		return Cluster{}, fault.New(fault.Invalid, "initial_soc_outside_window", "initial SOC is outside operating window")
	}
	now = now.UTC()
	return Cluster{
		ID:           uuid.NewString(),
		SiteID:       input.SiteID,
		Code:         code,
		RatedPowerKW: input.RatedPowerKW,
		CapacityKWh:  input.CapacityKWh,
		MinSOC:       input.MinSOC,
		MaxSOC:       input.MaxSOC,
		CurrentSOC:   input.InitialSOC,
		Status:       ClusterAvailable,
		Version:      1,
		CreatedAt:    now,
		UpdatedAt:    now,
	}, nil
}

func (c Cluster) Eligible(direction string, requestedKW int64) error {
	if c.Status != ClusterAvailable {
		return fault.WithFields(
			fault.New(fault.Conflict, "cluster_unavailable", "battery cluster is not available"),
			map[string]string{"status": string(c.Status)},
		)
	}
	if requestedKW <= 0 || requestedKW > c.RatedPowerKW {
		return fault.WithFields(
			fault.New(fault.Conflict, "cluster_power_exceeded", "requested power exceeds cluster rating"),
			map[string]string{"rated_kw": fmt.Sprint(c.RatedPowerKW)},
		)
	}
	switch direction {
	case "charge":
		if c.CurrentSOC >= c.MaxSOC {
			return fault.New(fault.Conflict, "cluster_soc_high", "cluster cannot charge above maximum SOC")
		}
	case "discharge":
		if c.CurrentSOC <= c.MinSOC {
			return fault.New(fault.Conflict, "cluster_soc_low", "cluster cannot discharge below minimum SOC")
		}
	default:
		return fault.New(fault.Invalid, "invalid_direction", "direction must be charge or discharge")
	}
	return nil
}

func (c Cluster) Reserve(now time.Time) (Cluster, error) {
	if c.Status != ClusterAvailable {
		return Cluster{}, fault.New(fault.Conflict, "cluster_unavailable", "only available clusters can be reserved")
	}
	copy := c
	copy.Status = ClusterReserved
	copy.Version++
	copy.UpdatedAt = now.UTC()
	return copy, nil
}

func (c Cluster) Start(now time.Time) (Cluster, error) {
	if c.Status != ClusterReserved {
		return Cluster{}, fault.New(fault.Conflict, "cluster_not_reserved", "cluster must be reserved before execution")
	}
	copy := c
	copy.Status = ClusterRunning
	copy.Version++
	copy.UpdatedAt = now.UTC()
	return copy, nil
}

func (c Cluster) Release(now time.Time) (Cluster, error) {
	if c.Status != ClusterReserved && c.Status != ClusterRunning {
		return Cluster{}, fault.New(fault.Conflict, "cluster_not_held", "cluster is not held by a dispatch")
	}
	copy := c
	copy.Status = ClusterAvailable
	copy.Version++
	copy.UpdatedAt = now.UTC()
	return copy, nil
}

func (c Cluster) ApplyTelemetry(sequence int64, soc int, observedAt, now time.Time) (Cluster, error) {
	if sequence <= c.LatestSequence {
		return Cluster{}, fault.New(fault.Conflict, "telemetry_sequence_conflict", "telemetry sequence must increase")
	}
	if observedAt.After(now.Add(2*time.Minute)) || observedAt.Before(now.Add(-24*time.Hour)) {
		return Cluster{}, fault.New(fault.Invalid, "telemetry_time_outside_window", "telemetry observation time is outside the accepted window")
	}
	if soc < 0 || soc > 100 {
		return Cluster{}, fault.New(fault.Invalid, "invalid_soc", "SOC must be between 0 and 100")
	}
	copy := c
	copy.LatestSequence = sequence
	copy.CurrentSOC = soc
	at := observedAt.UTC()
	copy.LatestTelemetryAt = &at
	copy.Version++
	copy.UpdatedAt = now.UTC()
	return copy, nil
}

func (c Cluster) MarkDegraded(now time.Time) Cluster {
	copy := c
	copy.Status = ClusterDegraded
	copy.Version++
	copy.UpdatedAt = now.UTC()
	return copy
}

func (c Cluster) Recover(now time.Time) (Cluster, error) {
	if c.Status != ClusterDegraded && c.Status != ClusterOffline {
		return Cluster{}, fault.New(fault.Conflict, "cluster_not_degraded", "only degraded or offline clusters can recover")
	}
	if c.CurrentSOC < c.MinSOC || c.CurrentSOC > c.MaxSOC {
		return Cluster{}, fault.New(fault.Conflict, "soc_not_recovered", "SOC remains outside the operating window")
	}
	copy := c
	copy.Status = ClusterAvailable
	copy.Version++
	copy.UpdatedAt = now.UTC()
	return copy, nil
}

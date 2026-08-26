package telemetry

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/vance1852/gridvault-ess/internal/fault"
)

type Snapshot struct {
	ID                string
	ClusterID         string
	Sequence          int64
	ObservedAt        time.Time
	SOC               int
	PowerKW           int64
	TemperatureMilliC int64
	EnergyDeltaWh     int64
	ReceivedAt        time.Time
}

type Reading struct {
	ClusterID         string
	Sequence          int64
	ObservedAt        time.Time
	SOC               int
	PowerKW           int64
	TemperatureMilliC int64
	EnergyDeltaWh     int64
}

func NewSnapshot(input Reading, receivedAt time.Time) (Snapshot, error) {
	if strings.TrimSpace(input.ClusterID) == "" {
		return Snapshot{}, fault.New(fault.Invalid, "missing_cluster", "telemetry cluster is required")
	}
	if input.Sequence <= 0 {
		return Snapshot{}, fault.New(fault.Invalid, "invalid_sequence", "telemetry sequence must be positive")
	}
	if input.ObservedAt.IsZero() {
		return Snapshot{}, fault.New(fault.Invalid, "missing_observed_at", "telemetry observation time is required")
	}
	if input.ObservedAt.After(receivedAt.Add(2*time.Minute)) || input.ObservedAt.Before(receivedAt.Add(-24*time.Hour)) {
		return Snapshot{}, fault.New(fault.Invalid, "observation_outside_window", "telemetry observation time is outside the accepted window")
	}
	if input.SOC < 0 || input.SOC > 100 {
		return Snapshot{}, fault.New(fault.Invalid, "invalid_soc", "SOC must be between 0 and 100")
	}
	if input.PowerKW < -10_000_000 || input.PowerKW > 10_000_000 {
		return Snapshot{}, fault.New(fault.Invalid, "invalid_power", "telemetry power is outside supported range")
	}
	if input.TemperatureMilliC < -50_000 || input.TemperatureMilliC > 150_000 {
		return Snapshot{}, fault.New(fault.Invalid, "invalid_temperature", "temperature is outside supported range")
	}
	return Snapshot{
		ID:                uuid.NewString(),
		ClusterID:         input.ClusterID,
		Sequence:          input.Sequence,
		ObservedAt:        input.ObservedAt.UTC(),
		SOC:               input.SOC,
		PowerKW:           input.PowerKW,
		TemperatureMilliC: input.TemperatureMilliC,
		EnergyDeltaWh:     input.EnergyDeltaWh,
		ReceivedAt:        receivedAt.UTC(),
	}, nil
}

type Thresholds struct {
	LowSOC                    int
	HighSOC                   int
	HighTemperatureMilliC     int64
	CriticalTemperatureMilliC int64
}

func DefaultThresholds() Thresholds {
	return Thresholds{LowSOC: 10, HighSOC: 95, HighTemperatureMilliC: 50_000, CriticalTemperatureMilliC: 60_000}
}

func (t Thresholds) Validate() error {
	if t.LowSOC < 0 || t.LowSOC > 50 || t.HighSOC < 50 || t.HighSOC > 100 || t.LowSOC >= t.HighSOC {
		return fault.New(fault.Invalid, "invalid_soc_thresholds", "SOC alarm thresholds are invalid")
	}
	if t.HighTemperatureMilliC < 20_000 || t.CriticalTemperatureMilliC > 150_000 || t.HighTemperatureMilliC >= t.CriticalTemperatureMilliC {
		return fault.New(fault.Invalid, "invalid_temperature_thresholds", "temperature alarm thresholds are invalid")
	}
	return nil
}

type Condition struct {
	Type        string
	Severity    string
	Fingerprint string
	Message     string
}

func Evaluate(snapshot Snapshot, thresholds Thresholds) ([]Condition, error) {
	if err := thresholds.Validate(); err != nil {
		return nil, err
	}
	conditions := make([]Condition, 0, 3)
	if snapshot.SOC <= thresholds.LowSOC {
		conditions = append(conditions, Condition{
			Type: "soc_low", Severity: "critical",
			Fingerprint: snapshot.ClusterID + ":soc_low",
			Message:     fmt.Sprintf("cluster SOC %d%% is at or below %d%%", snapshot.SOC, thresholds.LowSOC),
		})
	}
	if snapshot.SOC >= thresholds.HighSOC {
		conditions = append(conditions, Condition{
			Type: "soc_high", Severity: "warning",
			Fingerprint: snapshot.ClusterID + ":soc_high",
			Message:     fmt.Sprintf("cluster SOC %d%% is at or above %d%%", snapshot.SOC, thresholds.HighSOC),
		})
	}
	if snapshot.TemperatureMilliC >= thresholds.CriticalTemperatureMilliC {
		conditions = append(conditions, Condition{
			Type: "temperature_high", Severity: "critical",
			Fingerprint: snapshot.ClusterID + ":temperature_high",
			Message:     fmt.Sprintf("cluster temperature %d mC is critical", snapshot.TemperatureMilliC),
		})
	} else if snapshot.TemperatureMilliC >= thresholds.HighTemperatureMilliC {
		conditions = append(conditions, Condition{
			Type: "temperature_high", Severity: "warning",
			Fingerprint: snapshot.ClusterID + ":temperature_high",
			Message:     fmt.Sprintf("cluster temperature %d mC is high", snapshot.TemperatureMilliC),
		})
	}
	return conditions, nil
}

func IsHealthy(snapshot Snapshot, thresholds Thresholds) bool {
	conditions, err := Evaluate(snapshot, thresholds)
	return err == nil && len(conditions) == 0
}

type PageFilter struct {
	ClusterID string
	From      *time.Time
	Until     *time.Time
	Limit     int
	Offset    int
}

func (f PageFilter) Normalize() (PageFilter, error) {
	copy := f
	copy.ClusterID = strings.TrimSpace(copy.ClusterID)
	if copy.ClusterID == "" {
		return PageFilter{}, fault.New(fault.Invalid, "missing_cluster", "telemetry cluster is required")
	}
	if copy.Limit == 0 {
		copy.Limit = 100
	}
	if copy.Limit < 1 || copy.Limit > 1000 {
		return PageFilter{}, fault.New(fault.Invalid, "invalid_page_limit", "page limit must be between 1 and 1000")
	}
	if copy.Offset < 0 {
		return PageFilter{}, fault.New(fault.Invalid, "invalid_page_offset", "page offset cannot be negative")
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
		return PageFilter{}, fault.New(fault.Invalid, "invalid_filter_window", "filter end must follow start")
	}
	return copy, nil
}

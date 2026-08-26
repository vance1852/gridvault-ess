package telemetry

import (
	"testing"
	"time"

	"github.com/vance1852/gridvault-ess/internal/fault"
)

var telemetryNow = time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)

func validReading() Reading {
	return Reading{ClusterID: "cluster-1", Sequence: 1, ObservedAt: telemetryNow.Add(-time.Minute), SOC: 50, PowerKW: 120, TemperatureMilliC: 30000, EnergyDeltaWh: 2000}
}
func TestSnapshotCopiesValidatedReading(t *testing.T) {
	input := validReading()
	snapshot, err := NewSnapshot(input, telemetryNow)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.ID == "" {
		t.Fatal("missing ID")
	}
	if snapshot.ClusterID != input.ClusterID || snapshot.Sequence != input.Sequence {
		t.Fatalf("identity=%+v", snapshot)
	}
	if snapshot.SOC != 50 || snapshot.PowerKW != 120 || snapshot.EnergyDeltaWh != 2000 {
		t.Fatalf("measurements=%+v", snapshot)
	}
	if !snapshot.ObservedAt.Equal(input.ObservedAt) {
		t.Fatalf("observed=%v", snapshot.ObservedAt)
	}
	if !snapshot.ReceivedAt.Equal(telemetryNow) {
		t.Fatalf("received=%v", snapshot.ReceivedAt)
	}
}
func TestSnapshotValidation(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Reading)
		code   string
	}{{"cluster", func(v *Reading) { v.ClusterID = "" }, "missing_cluster"}, {"sequence", func(v *Reading) { v.Sequence = 0 }, "invalid_sequence"}, {"observed", func(v *Reading) { v.ObservedAt = time.Time{} }, "missing_observed_at"}, {"future", func(v *Reading) { v.ObservedAt = telemetryNow.Add(3 * time.Minute) }, "observation_outside_window"}, {"old", func(v *Reading) { v.ObservedAt = telemetryNow.Add(-25 * time.Hour) }, "observation_outside_window"}, {"soc low", func(v *Reading) { v.SOC = -1 }, "invalid_soc"}, {"soc high", func(v *Reading) { v.SOC = 101 }, "invalid_soc"}, {"power", func(v *Reading) { v.PowerKW = 10000001 }, "invalid_power"}, {"cold", func(v *Reading) { v.TemperatureMilliC = -50001 }, "invalid_temperature"}, {"hot", func(v *Reading) { v.TemperatureMilliC = 150001 }, "invalid_temperature"}}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := validReading()
			tt.mutate(&input)
			_, err := NewSnapshot(input, telemetryNow)
			if fault.Code(err) != tt.code {
				t.Fatalf("code=%s err=%v", fault.Code(err), err)
			}
		})
	}
}
func TestEvaluateAlarmConditions(t *testing.T) {
	thresholds := DefaultThresholds()
	tests := []struct {
		name        string
		soc         int
		temperature int64
		want        []string
	}{{"healthy", 50, 30000, nil}, {"low soc", 10, 30000, []string{"soc_low"}}, {"high soc", 95, 30000, []string{"soc_high"}}, {"warm", 50, 50000, []string{"temperature_high"}}, {"critical", 50, 60000, []string{"temperature_high"}}, {"combined", 5, 65000, []string{"soc_low", "temperature_high"}}}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			snapshot, _ := NewSnapshot(validReading(), telemetryNow)
			snapshot.SOC = tt.soc
			snapshot.TemperatureMilliC = tt.temperature
			conditions, err := Evaluate(snapshot, thresholds)
			if err != nil {
				t.Fatal(err)
			}
			if len(conditions) != len(tt.want) {
				t.Fatalf("conditions=%+v", conditions)
			}
			for index, want := range tt.want {
				if conditions[index].Type != want {
					t.Fatalf("condition[%d]=%s", index, conditions[index].Type)
				}
				if conditions[index].Fingerprint == "" || conditions[index].Message == "" {
					t.Fatalf("condition incomplete: %+v", conditions[index])
				}
			}
			if IsHealthy(snapshot, thresholds) != (len(tt.want) == 0) {
				t.Fatalf("healthy mismatch")
			}
		})
	}
}
func TestThresholdValidation(t *testing.T) {
	tests := []Thresholds{{LowSOC: 60, HighSOC: 90, HighTemperatureMilliC: 50000, CriticalTemperatureMilliC: 60000}, {LowSOC: 10, HighSOC: 40, HighTemperatureMilliC: 50000, CriticalTemperatureMilliC: 60000}, {LowSOC: 10, HighSOC: 90, HighTemperatureMilliC: 60000, CriticalTemperatureMilliC: 60000}, {LowSOC: 10, HighSOC: 90, HighTemperatureMilliC: 50000, CriticalTemperatureMilliC: 160000}}
	for index, value := range tests {
		if err := value.Validate(); err == nil {
			t.Fatalf("case %d accepted: %+v", index, value)
		}
	}
}
func TestPageFilterNormalization(t *testing.T) {
	from := telemetryNow.Add(-time.Hour)
	until := telemetryNow
	filter, err := (PageFilter{ClusterID: " cluster ", From: &from, Until: &until}).Normalize()
	if err != nil {
		t.Fatal(err)
	}
	if filter.ClusterID != "cluster" || filter.Limit != 100 {
		t.Fatalf("filter=%+v", filter)
	}
	if _, err = (PageFilter{}).Normalize(); fault.Code(err) != "missing_cluster" {
		t.Fatalf("cluster=%v", err)
	}
	if _, err = (PageFilter{ClusterID: "c", Limit: 1001}).Normalize(); fault.Code(err) != "invalid_page_limit" {
		t.Fatalf("limit=%v", err)
	}
	if _, err = (PageFilter{ClusterID: "c", Offset: -1}).Normalize(); fault.Code(err) != "invalid_page_offset" {
		t.Fatalf("offset=%v", err)
	}
	if _, err = (PageFilter{ClusterID: "c", From: &until, Until: &from}).Normalize(); fault.Code(err) != "invalid_filter_window" {
		t.Fatalf("window=%v", err)
	}
}

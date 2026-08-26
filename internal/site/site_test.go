package site

import (
	"testing"
	"time"

	"github.com/vance1852/gridvault-ess/internal/fault"
)

var siteNow = time.Date(2026, 8, 26, 0, 0, 0, 0, time.UTC)

func validSite(t *testing.T) Site {
	t.Helper()
	value, err := Create(NewSite{Code: "ESS-001", Name: "East Storage Station", Timezone: "Asia/Shanghai", GridLimitKW: 1000}, siteNow)
	if err != nil {
		t.Fatal(err)
	}
	return value
}
func validCluster(t *testing.T) Cluster {
	t.Helper()
	value, err := CreateCluster(NewCluster{SiteID: "site-1", Code: "BAT-001", RatedPowerKW: 500, CapacityKWh: 1000, MinSOC: 10, MaxSOC: 90, InitialSOC: 50}, siteNow)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func TestSiteLifecycleAndCapacity(t *testing.T) {
	value := validSite(t)
	if value.Status != StatusCommissioning {
		t.Fatalf("status=%s", value.Status)
	}
	active, err := value.Transition(StatusActive, siteNow.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	reserved, err := active.Reserve(600, siteNow.Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if reserved.ReservedKW != 600 || reserved.AvailableKW() != 400 {
		t.Fatalf("capacity=%+v", reserved)
	}
	if _, err = reserved.Reserve(401, siteNow); fault.Code(err) != "grid_capacity_exceeded" {
		t.Fatalf("over reservation=%v", err)
	}
	released, err := reserved.Release(250, siteNow.Add(3*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if released.ReservedKW != 350 {
		t.Fatalf("reserved=%d", released.ReservedKW)
	}
	if _, err = released.Transition(StatusRetired, siteNow); fault.Code(err) != "site_has_reservations" {
		t.Fatalf("retire=%v", err)
	}
	released, err = released.Release(350, siteNow)
	if err != nil {
		t.Fatal(err)
	}
	retired, err := released.Transition(StatusRetired, siteNow)
	if err != nil {
		t.Fatal(err)
	}
	if retired.Status != StatusRetired {
		t.Fatalf("status=%s", retired.Status)
	}
}

func TestSiteTransitionMatrix(t *testing.T) {
	tests := []struct {
		current, next Status
		ok            bool
	}{{StatusCommissioning, StatusActive, true}, {StatusCommissioning, StatusRetired, true}, {StatusActive, StatusSuspended, true}, {StatusActive, StatusRetired, true}, {StatusSuspended, StatusActive, true}, {StatusSuspended, StatusRetired, true}, {StatusRetired, StatusActive, false}, {StatusActive, StatusCommissioning, false}}
	for _, tt := range tests {
		t.Run(string(tt.current)+"_to_"+string(tt.next), func(t *testing.T) {
			value := validSite(t)
			value.Status = tt.current
			_, err := value.Transition(tt.next, siteNow)
			if tt.ok && err != nil {
				t.Fatalf("unexpected: %v", err)
			}
			if !tt.ok && fault.Code(err) != "invalid_site_transition" {
				t.Fatalf("error=%v", err)
			}
		})
	}
}

func TestSiteInputValidation(t *testing.T) {
	tests := []struct {
		name  string
		input NewSite
		code  string
	}{{"code", NewSite{Code: "x", Name: "Valid Name", Timezone: "UTC", GridLimitKW: 100}, "invalid_site_code"}, {"name", NewSite{Code: "ABC", Name: "x", Timezone: "UTC", GridLimitKW: 100}, "invalid_site_name"}, {"timezone", NewSite{Code: "ABC", Name: "Valid Name", Timezone: "Mars/Base", GridLimitKW: 100}, "invalid_timezone"}, {"limit low", NewSite{Code: "ABC", Name: "Valid Name", Timezone: "UTC", GridLimitKW: 9}, "invalid_grid_limit"}, {"limit high", NewSite{Code: "ABC", Name: "Valid Name", Timezone: "UTC", GridLimitKW: 10000001}, "invalid_grid_limit"}}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Create(tt.input, siteNow)
			if fault.Code(err) != tt.code {
				t.Fatalf("code=%s err=%v", fault.Code(err), err)
			}
		})
	}
}

func TestClusterEligibilityAndLifecycle(t *testing.T) {
	cluster := validCluster(t)
	if err := cluster.Eligible("charge", 300); err != nil {
		t.Fatal(err)
	}
	if err := cluster.Eligible("discharge", 300); err != nil {
		t.Fatal(err)
	}
	if err := cluster.Eligible("charge", 501); fault.Code(err) != "cluster_power_exceeded" {
		t.Fatalf("power=%v", err)
	}
	reserved, err := cluster.Reserve(siteNow)
	if err != nil {
		t.Fatal(err)
	}
	if err = reserved.Eligible("charge", 100); fault.Code(err) != "cluster_unavailable" {
		t.Fatalf("reserved eligible=%v", err)
	}
	running, err := reserved.Start(siteNow)
	if err != nil {
		t.Fatal(err)
	}
	released, err := running.Release(siteNow)
	if err != nil {
		t.Fatal(err)
	}
	if released.Status != ClusterAvailable {
		t.Fatalf("status=%s", released.Status)
	}
}

func TestClusterTelemetryOrderingAndWindow(t *testing.T) {
	cluster := validCluster(t)
	observed := siteNow.Add(-time.Minute)
	updated, err := cluster.ApplyTelemetry(1, 55, observed, siteNow)
	if err != nil {
		t.Fatal(err)
	}
	if updated.LatestSequence != 1 || updated.CurrentSOC != 55 {
		t.Fatalf("updated=%+v", updated)
	}
	if updated.LatestTelemetryAt == nil || !updated.LatestTelemetryAt.Equal(observed) {
		t.Fatalf("at=%v", updated.LatestTelemetryAt)
	}
	if _, err = updated.ApplyTelemetry(1, 56, siteNow, siteNow); fault.Code(err) != "telemetry_sequence_conflict" {
		t.Fatalf("duplicate=%v", err)
	}
	if _, err = updated.ApplyTelemetry(2, 56, siteNow.Add(3*time.Minute), siteNow); fault.Code(err) != "telemetry_time_outside_window" {
		t.Fatalf("future=%v", err)
	}
	if _, err = updated.ApplyTelemetry(2, 56, siteNow.Add(-25*time.Hour), siteNow); fault.Code(err) != "telemetry_time_outside_window" {
		t.Fatalf("old=%v", err)
	}
}

func TestClusterRecoveryRequiresHealthySOC(t *testing.T) {
	cluster := validCluster(t)
	degraded := cluster.MarkDegraded(siteNow)
	degraded.CurrentSOC = 5
	if _, err := degraded.Recover(siteNow); fault.Code(err) != "soc_not_recovered" {
		t.Fatalf("low soc=%v", err)
	}
	degraded.CurrentSOC = 50
	recovered, err := degraded.Recover(siteNow)
	if err != nil {
		t.Fatal(err)
	}
	if recovered.Status != ClusterAvailable {
		t.Fatalf("status=%s", recovered.Status)
	}
	if _, err = recovered.Recover(siteNow); fault.Code(err) != "cluster_not_degraded" {
		t.Fatalf("second recovery=%v", err)
	}
}

func TestClusterOperatingMarginExplainsRecoveryState(t *testing.T) {
	cluster := validCluster(t)
	if margin := cluster.Margin(); !margin.WithinWindow || margin.BelowMinimum != 0 || margin.AboveMaximum != 0 {
		t.Fatalf("healthy margin=%+v", margin)
	}
	cluster.CurrentSOC = 5
	if margin := cluster.Margin(); margin.WithinWindow || margin.BelowMinimum != 5 || margin.AboveMaximum != 0 {
		t.Fatalf("low margin=%+v", margin)
	}
	cluster.CurrentSOC = 95
	if margin := cluster.Margin(); margin.WithinWindow || margin.BelowMinimum != 0 || margin.AboveMaximum != 5 {
		t.Fatalf("high margin=%+v", margin)
	}
}

func TestListFilterNormalization(t *testing.T) {
	filter, err := (ListFilter{}).Normalize()
	if err != nil {
		t.Fatal(err)
	}
	if filter.Limit != 50 || filter.Sort != SortCodeAsc {
		t.Fatalf("filter=%+v", filter)
	}
	if _, err = (ListFilter{Limit: 201}).Normalize(); fault.Code(err) != "invalid_page_limit" {
		t.Fatalf("limit=%v", err)
	}
	if _, err = (ListFilter{Offset: -1}).Normalize(); fault.Code(err) != "invalid_page_offset" {
		t.Fatalf("offset=%v", err)
	}
	if _, err = (ListFilter{Sort: "unknown"}).Normalize(); fault.Code(err) != "invalid_sort" {
		t.Fatalf("sort=%v", err)
	}
	if _, err = (ListFilter{Status: "unknown"}).Normalize(); fault.Code(err) != "invalid_site_status" {
		t.Fatalf("status=%v", err)
	}
}

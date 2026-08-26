package service

import (
	"context"

	"github.com/vance1852/gridvault-ess/internal/alarm"
	"github.com/vance1852/gridvault-ess/internal/audit"
	"github.com/vance1852/gridvault-ess/internal/clock"
	"github.com/vance1852/gridvault-ess/internal/identity"
	"github.com/vance1852/gridvault-ess/internal/site"
	"github.com/vance1852/gridvault-ess/internal/telemetry"
)

type TelemetryStore interface {
	ClusterByID(context.Context, string) (site.Cluster, error)
	StoreTelemetryAtomic(context.Context, telemetry.Snapshot, int, int64, []alarm.Alarm, audit.Event) error
}
type TelemetryService struct {
	store      TelemetryStore
	clock      clock.Clock
	thresholds telemetry.Thresholds
}

func NewTelemetryService(store TelemetryStore, timer clock.Clock, thresholds telemetry.Thresholds) *TelemetryService {
	return &TelemetryService{store: store, clock: timer, thresholds: thresholds}
}

type TelemetryResult struct {
	Snapshot   telemetry.Snapshot    `json:"snapshot"`
	Conditions []telemetry.Condition `json:"conditions"`
	AlarmIDs   []string              `json:"alarm_ids"`
}

func (s *TelemetryService) Record(ctx context.Context, p Principal, input telemetry.Reading, request string) (TelemetryResult, error) {
	if err := p.Require(identity.PermissionTelemetryWrite); err != nil {
		return TelemetryResult{}, err
	}
	cluster, err := s.store.ClusterByID(ctx, input.ClusterID)
	if err != nil {
		return TelemetryResult{}, err
	}
	snapshot, err := telemetry.NewSnapshot(input, s.clock.Now())
	if err != nil {
		return TelemetryResult{}, err
	}
	updated, err := cluster.ApplyTelemetry(snapshot.Sequence, snapshot.SOC, snapshot.ObservedAt, s.clock.Now())
	if err != nil {
		return TelemetryResult{}, err
	}
	conditions, err := telemetry.Evaluate(snapshot, s.thresholds)
	if err != nil {
		return TelemetryResult{}, err
	}
	alarms := make([]alarm.Alarm, 0, len(conditions))
	for _, condition := range conditions {
		severity := alarm.Warning
		if condition.Severity == "critical" {
			severity = alarm.Critical
		}
		item, createErr := alarm.Create(alarm.NewAlarm{SiteID: cluster.SiteID, ClusterID: cluster.ID, Type: condition.Type, Fingerprint: condition.Fingerprint, Message: condition.Message, Severity: severity}, s.clock.Now())
		if createErr != nil {
			return TelemetryResult{}, createErr
		}
		alarms = append(alarms, item)
	}
	event, _ := audit.NewEvent(p.User.ID, requestID(request), "battery_cluster", cluster.ID, "record_telemetry", "success", map[string]any{"sequence": snapshot.Sequence, "condition_count": len(conditions)}, s.clock.Now())
	if err = s.store.StoreTelemetryAtomic(ctx, snapshot, updated.CurrentSOC, cluster.Version, alarms, event); err != nil {
		return TelemetryResult{}, err
	}
	ids := make([]string, len(alarms))
	for i, item := range alarms {
		ids[i] = item.ID
	}
	return TelemetryResult{Snapshot: snapshot, Conditions: conditions, AlarmIDs: ids}, nil
}

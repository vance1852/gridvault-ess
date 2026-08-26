package service

import (
	"context"

	"github.com/vance1852/gridvault-ess/internal/alarm"
	"github.com/vance1852/gridvault-ess/internal/audit"
	"github.com/vance1852/gridvault-ess/internal/clock"
	"github.com/vance1852/gridvault-ess/internal/fault"
	"github.com/vance1852/gridvault-ess/internal/identity"
	"github.com/vance1852/gridvault-ess/internal/site"
	"github.com/vance1852/gridvault-ess/internal/telemetry"
)

type AlarmStore interface {
	AlarmByID(context.Context, string) (alarm.Alarm, error)
	UpdateAlarm(context.Context, alarm.Alarm, int64, audit.Event) error
	ClusterByID(context.Context, string) (site.Cluster, error)
}
type AlarmService struct {
	store      AlarmStore
	clock      clock.Clock
	thresholds telemetry.Thresholds
}

func NewAlarmService(store AlarmStore, timer clock.Clock, thresholds telemetry.Thresholds) *AlarmService {
	return &AlarmService{store: store, clock: timer, thresholds: thresholds}
}
func (s *AlarmService) Acknowledge(ctx context.Context, p Principal, id string, expected int64, request string) (alarm.Alarm, error) {
	if err := p.Require(identity.PermissionAlarmManage); err != nil {
		return alarm.Alarm{}, err
	}
	current, err := s.store.AlarmByID(ctx, id)
	if err != nil {
		return alarm.Alarm{}, err
	}
	changed, err := current.Acknowledge(p.User.ID, expected, s.clock.Now())
	if err != nil {
		return alarm.Alarm{}, err
	}
	event, _ := audit.NewEvent(p.User.ID, requestID(request), "alarm", id, "acknowledge", "success", nil, s.clock.Now())
	if err = s.store.UpdateAlarm(ctx, changed, expected, event); err != nil {
		return alarm.Alarm{}, err
	}
	return changed, nil
}
func (s *AlarmService) Resolve(ctx context.Context, p Principal, id string, expected int64, request string) (alarm.Alarm, error) {
	if err := p.Require(identity.PermissionAlarmManage); err != nil {
		return alarm.Alarm{}, err
	}
	current, err := s.store.AlarmByID(ctx, id)
	if err != nil {
		return alarm.Alarm{}, err
	}
	cluster, err := s.store.ClusterByID(ctx, current.ClusterID)
	if err != nil {
		return alarm.Alarm{}, err
	}
	if cluster.LatestTelemetryAt == nil {
		return alarm.Alarm{}, fault.New(fault.Conflict, "telemetry_missing", "cluster has no recovery telemetry")
	}
	healthy := cluster.CurrentSOC > s.thresholds.LowSOC && cluster.CurrentSOC < s.thresholds.HighSOC
	changed, err := current.Resolve(p.User.ID, expected, healthy, s.clock.Now())
	if err != nil {
		return alarm.Alarm{}, err
	}
	event, _ := audit.NewEvent(p.User.ID, requestID(request), "alarm", id, "resolve", "success", map[string]any{"cluster_soc": cluster.CurrentSOC}, s.clock.Now())
	if err = s.store.UpdateAlarm(ctx, changed, expected, event); err != nil {
		return alarm.Alarm{}, err
	}
	return changed, nil
}
func (s *AlarmService) AcknowledgeBatch(ctx context.Context, p Principal, ids []string, versions map[string]int64, request string) alarm.BatchResult {
	results := make([]alarm.ItemResult, 0, len(ids))
	seen := map[string]bool{}
	for _, id := range ids {
		if seen[id] {
			results = append(results, alarm.ItemResult{ID: id, Status: "failed", ErrorCode: "duplicate_id"})
			continue
		}
		seen[id] = true
		_, err := s.Acknowledge(ctx, p, id, versions[id], request)
		if err != nil {
			results = append(results, alarm.ItemResult{ID: id, Status: "failed", ErrorCode: fault.Code(err)})
			continue
		}
		results = append(results, alarm.ItemResult{ID: id, Status: "acknowledged"})
	}
	return alarm.BuildBatchResult(results)
}

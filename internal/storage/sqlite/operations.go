package sqlite

import (
	"context"
	"database/sql"
	"strings"

	"github.com/vance1852/gridvault-ess/internal/alarm"
	"github.com/vance1852/gridvault-ess/internal/audit"
	"github.com/vance1852/gridvault-ess/internal/settlement"
	"github.com/vance1852/gridvault-ess/internal/telemetry"
)

func insertAudit(q Querier, ctx context.Context, e audit.Event) error {
	_, err := q.ExecContext(ctx, `INSERT INTO audit_events(id,actor_id,request_id,object_type,object_id,action,result,details_json,occurred_at) VALUES(?,?,?,?,?,?,?,?,?)`, e.ID, nullableString(e.ActorID), e.RequestID, e.ObjectType, e.ObjectID, e.Action, e.Result, e.DetailsJSON, encodeTime(e.OccurredAt))
	return translate("sqlite.insertAudit", err)
}
func (d *DB) InsertAudit(ctx context.Context, e audit.Event) error { return insertAudit(d.sql, ctx, e) }
func (d *DB) AuditInserter(ctx context.Context, e audit.Event) func(*sql.Tx) error {
	return func(tx *sql.Tx) error { return insertAudit(tx, ctx, e) }
}
func (d *DB) ListAudit(ctx context.Context, filter audit.Filter) (audit.Page, error) {
	f, err := filter.Normalize()
	if err != nil {
		return audit.Page{}, err
	}
	clauses := []string{"1=1"}
	args := []any{}
	if f.ObjectType != "" {
		clauses = append(clauses, "object_type=?")
		args = append(args, f.ObjectType)
	}
	if f.ObjectID != "" {
		clauses = append(clauses, "object_id=?")
		args = append(args, f.ObjectID)
	}
	if f.RequestID != "" {
		clauses = append(clauses, "request_id=?")
		args = append(args, f.RequestID)
	}
	if f.From != nil {
		clauses = append(clauses, "occurred_at>=?")
		args = append(args, encodeTime(*f.From))
	}
	if f.Until != nil {
		clauses = append(clauses, "occurred_at<?")
		args = append(args, encodeTime(*f.Until))
	}
	if f.Cursor != nil {
		clauses = append(clauses, "(occurred_at<? OR (occurred_at=? AND id<?))")
		value := encodeTime(f.Cursor.OccurredAt)
		args = append(args, value, value, f.Cursor.ID)
	}
	args = append(args, f.Limit+1)
	rows, err := d.sql.QueryContext(ctx, `SELECT id,actor_id,request_id,object_type,object_id,action,result,details_json,occurred_at FROM audit_events WHERE `+strings.Join(clauses, " AND ")+` ORDER BY occurred_at DESC,id DESC LIMIT ?`, args...)
	if err != nil {
		return audit.Page{}, translate("sqlite.ListAudit", err)
	}
	defer rows.Close()
	var items []audit.Event
	for rows.Next() {
		var e audit.Event
		var actor sql.NullString
		var occurred string
		if err = rows.Scan(&e.ID, &actor, &e.RequestID, &e.ObjectType, &e.ObjectID, &e.Action, &e.Result, &e.DetailsJSON, &occurred); err != nil {
			return audit.Page{}, translate("sqlite.ListAudit", err)
		}
		e.ActorID = actor.String
		e.OccurredAt, _ = parseTime(occurred)
		items = append(items, e)
	}
	var next *audit.Cursor
	if len(items) > f.Limit {
		last := items[f.Limit-1]
		next = &audit.Cursor{OccurredAt: last.OccurredAt, ID: last.ID}
		items = items[:f.Limit]
	}
	return audit.Page{Items: items, Next: next}, translate("sqlite.ListAudit", rows.Err())
}

func (d *DB) StoreTelemetryAtomic(ctx context.Context, s telemetry.Snapshot, clusterSOC int, clusterVersion int64, alarms []alarm.Alarm, auditEvent audit.Event) error {
	return d.InTx(ctx, "sqlite.StoreTelemetryAtomic", func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `INSERT INTO telemetry_snapshots(id,cluster_id,sequence,observed_at,soc,power_kw,temperature_milli_c,energy_delta_wh,received_at) VALUES(?,?,?,?,?,?,?,?,?)`, s.ID, s.ClusterID, s.Sequence, encodeTime(s.ObservedAt), s.SOC, s.PowerKW, s.TemperatureMilliC, s.EnergyDeltaWh, encodeTime(s.ReceivedAt)); err != nil {
			return translate("sqlite.insertTelemetry", err)
		}
		if err := expectOne("sqlite.advanceTelemetry")(tx.ExecContext(ctx, `UPDATE battery_clusters SET current_soc=?,latest_sequence=?,latest_telemetry_at=?,version=version+1,updated_at=? WHERE id=? AND version=? AND latest_sequence<?`, clusterSOC, s.Sequence, encodeTime(s.ObservedAt), encodeTime(s.ReceivedAt), s.ClusterID, clusterVersion, s.Sequence)); err != nil {
			return err
		}
		for _, a := range alarms {
			_, err := tx.ExecContext(ctx, `INSERT INTO alarms(id,site_id,cluster_id,alarm_type,severity,status,fingerprint,message,opened_at,version,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT DO NOTHING`, a.ID, a.SiteID, a.ClusterID, a.Type, a.Severity, a.Status, a.Fingerprint, a.Message, encodeTime(a.OpenedAt), a.Version, encodeTime(a.UpdatedAt))
			if err != nil {
				return translate("sqlite.insertAlarm", err)
			}
		}
		return insertAudit(tx, ctx, auditEvent)
	})
}
func (d *DB) AlarmByID(ctx context.Context, id string) (alarm.Alarm, error) {
	var a alarm.Alarm
	var severity, status, opened, updated string
	var ackBy, resBy, ackAt, resAt sql.NullString
	err := d.sql.QueryRowContext(ctx, `SELECT id,site_id,cluster_id,alarm_type,severity,status,fingerprint,message,opened_at,acknowledged_by,acknowledged_at,resolved_by,resolved_at,version,updated_at FROM alarms WHERE id=?`, id).Scan(&a.ID, &a.SiteID, &a.ClusterID, &a.Type, &severity, &status, &a.Fingerprint, &a.Message, &opened, &ackBy, &ackAt, &resBy, &resAt, &a.Version, &updated)
	if err != nil {
		return alarm.Alarm{}, translate("sqlite.AlarmByID", err)
	}
	a.Severity = alarm.Severity(severity)
	a.Status = alarm.Status(status)
	a.AcknowledgedBy = ackBy.String
	a.ResolvedBy = resBy.String
	a.OpenedAt, _ = parseTime(opened)
	a.AcknowledgedAt, _ = parseOptionalTime(ackAt)
	a.ResolvedAt, _ = parseOptionalTime(resAt)
	a.UpdatedAt, _ = parseTime(updated)
	return a, nil
}
func (d *DB) UpdateAlarm(ctx context.Context, a alarm.Alarm, expected int64, event audit.Event) error {
	return d.InTx(ctx, "sqlite.UpdateAlarm", func(tx *sql.Tx) error {
		if err := expectOne("sqlite.updateAlarm")(tx.ExecContext(ctx, `UPDATE alarms SET severity=?,status=?,message=?,acknowledged_by=?,acknowledged_at=?,resolved_by=?,resolved_at=?,version=?,updated_at=? WHERE id=? AND version=?`, a.Severity, a.Status, a.Message, nullableString(a.AcknowledgedBy), encodeOptionalTime(a.AcknowledgedAt), nullableString(a.ResolvedBy), encodeOptionalTime(a.ResolvedAt), a.Version, encodeTime(a.UpdatedAt), a.ID, expected)); err != nil {
			return err
		}
		return insertAudit(tx, ctx, event)
	})
}

func (d *DB) InsertPeriod(ctx context.Context, p settlement.Period) error {
	_, err := d.sql.ExecContext(ctx, `INSERT INTO settlement_periods(id,site_id,starts_at,ends_at,status,closed_by,closed_at,version,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?)`, p.ID, p.SiteID, encodeTime(p.Window.Start), encodeTime(p.Window.End), p.Status, nullableString(p.ClosedBy), encodeOptionalTime(p.ClosedAt), p.Version, encodeTime(p.CreatedAt), encodeTime(p.UpdatedAt))
	return translate("sqlite.InsertPeriod", err)
}
func (d *DB) InsertEntriesAtomic(ctx context.Context, p settlement.Period, expected int64, entries []settlement.Entry, event audit.Event) error {
	return d.InTx(ctx, "sqlite.InsertEntriesAtomic", func(tx *sql.Tx) error {
		for _, e := range entries {
			if _, err := tx.ExecContext(ctx, `INSERT INTO settlement_entries(id,period_id,plan_id,planned_wh,actual_wh,deviation_wh,amount_milli_cent,created_at) VALUES(?,?,?,?,?,?,?,?) ON CONFLICT(period_id,plan_id) DO NOTHING`, e.ID, e.PeriodID, e.PlanID, e.PlannedWh, e.ActualWh, e.DeviationWh, e.AmountMilliCent, encodeTime(e.CreatedAt)); err != nil {
				return translate("sqlite.insertEntry", err)
			}
		}
		if err := expectOne("sqlite.updatePeriod")(tx.ExecContext(ctx, `UPDATE settlement_periods SET status=?,closed_by=?,closed_at=?,version=?,updated_at=? WHERE id=? AND version=?`, p.Status, nullableString(p.ClosedBy), encodeOptionalTime(p.ClosedAt), p.Version, encodeTime(p.UpdatedAt), p.ID, expected)); err != nil {
			return err
		}
		return insertAudit(tx, ctx, event)
	})
}
